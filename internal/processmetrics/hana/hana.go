/*
Copyright 2022 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package hana provides functionality to collect SAP HANA metrics.
// The package implements processmetrics.Collector interface to collect HANA metrics.
package hana

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/metricevents"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (
	// Time taken is in microseconds.
	queryState struct {
		state, overallTime, serverTime int64
	}

	// InstanceProperties has necessary context for Metrics collection.
	// InstanceProperties implements Collector interface for HANA.
	InstanceProperties struct {
		SAPInstance        *sapb.SAPInstance
		Config             *cnfpb.Configuration
		Client             cloudmonitoring.TimeSeriesCreator
		HANAQueryFailCount int64
		SkippedMetrics     map[string]bool
		PMBackoffPolicy    backoff.BackOffContext
		ReplicationConfig  sapdiscovery.ReplicationConfig
		SapSystemInterface system.SapSystemDiscoveryInterface
	}
)

// HANA DB locks the user out after 3 failed authentication attempts, so we
// will have only two max fail counts.
const (
	maxHANAQueryFailCount = 2
)

// HANA HA replication: Any code from 10-15 is a valid return code. Anything else needs to be treated as failure.
const (
	replicationOff             int64 = 10
	replicationConnectionError int64 = 11
	replicationUnknown         int64 = 12
	replicationInitialization  int64 = 13
	replicationSyncing         int64 = 14
	replicationActive          int64 = 15
)

// HANA HA availability.
const (
	unknownState                          int64 = 0
	currentNodeSecondary                  int64 = 1
	primaryHasError                       int64 = 2
	primaryOnlineReplicationNotFunctional int64 = 3
	primaryOnlineReplicationRunning       int64 = 4
	currentNodeDRSite                     int64 = 5
)

// SAP control results.
const (
	sapControlAllProcessesRunning = 3
	sapControlAllProcessesStopped = 4
)

// HANA system availability.
const (
	systemAtLeastOneProcessNotGreen = 0
	systemAllProcessesGreen         = 1
)

const (
	metricURL            = "workload.googleapis.com"
	servicePath          = "/sap/hana/service"
	queryStatePath       = "/sap/hana/query/state"
	queryOverallTimePath = "/sap/hana/query/overalltime"
	queryServerTimePath  = "/sap/hana/query/servertime"
	logUtilisationKbPath = "/sap/hana/log/utilisationkb"
	hanaQuery            = "select * from dummy"
)

var (
	queryOverallTime = regexp.MustCompile("overall time ([0-9]+) usec")
	queryServerTime  = regexp.MustCompile("server time ([0-9]+) usec")
)

// Collect is HANA implementation of Collector interface from processmetrics.go.
// Returns a list of HANA related metrics.
// Collect method keeps on collecting all the metrics it can, logs errors if it encounters
// any and returns the collected metrics with the last error encountered while collecting metrics.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	scc := sapcontrolclient.New(p.SAPInstance.GetInstanceNumber())
	var metricsCollectionErr error

	metrics, err := collectHANAServiceMetrics(ctx, p, scc)
	if err != nil {
		metricsCollectionErr = err
	}

	// Collect DB Query metrics only if credentials are set and NOT a HANA secondary.
	if ((p.SAPInstance.GetHanaDbUser() != "" && p.SAPInstance.GetHanaDbPassword() != "") || p.SAPInstance.GetHdbuserstoreKey() != "") && p.SAPInstance.GetSite() != sapb.InstanceSite_HANA_SECONDARY {
		queryMetrics, err := collectHANAQueryMetrics(ctx, p, commandlineexecutor.ExecuteCommand)
		if err != nil {
			metricsCollectionErr = err
		}
		if queryMetrics != nil {
			metrics = append(metrics, queryMetrics...)
		}
	}

	hanaLogUtilisationKbMetrics, err := collectHANALogUtilisationKb(ctx, p, commandlineexecutor.ExecuteCommand)
	metrics = append(metrics, hanaLogUtilisationKbMetrics...)
	if err != nil {
		metricsCollectionErr = err
	}

	return metrics, metricsCollectionErr
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debugw("Context cancelled, exiting CollectWithRetry")
			return nil
		default:
			var err error
			res, err = p.Collect(ctx)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
				attempt++
			}
			return err
		}
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Retry limit exceeded", "InstanceId", p.SAPInstance.GetInstanceId(), "error", err)
	}
	return res, err
}

// collectHANAServiceMetrics creates metrics for each of the services in
// a HANA instance. Also returns availabilityValue a signal that depends
// on the status of the HANA services.
func collectHANAServiceMetrics(ctx context.Context, ip *InstanceProperties, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	log.CtxLogger(ctx).Debugw("Collecting HANA Replication HA metrics for instance", "instanceid", ip.SAPInstance.GetInstanceId())

	now := tspb.Now()
	sc := &sapcontrol.Properties{Instance: ip.SAPInstance}
	var (
		err       error
		processes map[int]*sapcontrol.ProcessStatus
		metrics   []*mrpb.TimeSeries
	)

	if _, ok := ip.SkippedMetrics[servicePath]; !ok {
		processes, err = sc.GetProcessList(ctx, scc)
		if err != nil {
			return nil, err
		}
		// If GetProcessList via command line or API didn't return an error.
		if len(processes) == 0 {
			log.CtxLogger(ctx).Debugw("Empty list of process returned")
			return nil, nil
		}

		for _, process := range processes {
			extraLabels := map[string]string{
				"service_name": process.Name,
				"pid":          process.PID,
			}
			metricevents.AddEvent(ctx, metricevents.Parameters{
				Path:       metricURL + servicePath,
				Message:    fmt.Sprintf("HANA Service Availability for %s", process.Name),
				Value:      strconv.FormatInt(boolToInt64(process.IsGreen), 10),
				Labels:     appendLabels(ip, extraLabels),
				Identifier: process.Name,
			})
			metrics = append(metrics, createMetrics(ip, servicePath, extraLabels, now, boolToInt64(process.IsGreen)))
		}
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in CollectReplicationHA()", "duration", time.Since(now.AsTime()))
	return metrics, nil
}

// collectHANAQueryMetrics collects the query state by running a HANA DB query.
//   - state : Value is 0 in case of successful query.
//   - overall time: Overall time taken by query in micro seconds.
//   - servertime: Time spent on the server side in micro seconds.
func collectHANAQueryMetrics(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute) ([]*mrpb.TimeSeries, error) {
	// Since these metrics are derived from the same operation, even if one of the metric is skipped the whole group will be skipped from collection.
	skipQueryMetrics := p.SkippedMetrics[queryStatePath] || p.SkippedMetrics[queryOverallTimePath] || p.SkippedMetrics[queryServerTimePath]
	if skipQueryMetrics {
		return nil, nil
	}
	now := tspb.Now()
	if p.HANAQueryFailCount >= maxHANAQueryFailCount {
		// if HANAQueryFailCount reaches maxHANAQueryFailCount we should not let it
		// query again, because the user can be locked out.
		log.CtxLogger(ctx).Debugw("Not queryig for HANAQuery Metrics as failcount has reached max allowed fail count.", "instanceid", p.SAPInstance.GetInstanceId(), "failcount", p.HANAQueryFailCount)
		return nil, nil
	}
	queryState, err := runHANAQuery(ctx, p, exec)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error in running query", log.Error(err))
		// Return a non-zero state in case of query failure.
		// Not following the convention of process metrics here because if we return the error here
		// query state metric will never get collected in an error state which can mask failures.
		return []*mrpb.TimeSeries{createMetrics(p, queryStatePath, nil, now, int64(1))}, nil
	}

	log.CtxLogger(ctx).Debugw("HANA query metrics for instance", "instanceid", p.SAPInstance.GetInstanceId(), "querystate", queryState)
	metricevents.AddEvent(ctx, metricevents.Parameters{
		Path:    metricURL + queryStatePath,
		Message: fmt.Sprintf("HANA Query State for instance %s", p.SAPInstance.GetInstanceId()),
		Value:   strconv.FormatInt(queryState.state, 10),
		Labels:  appendLabels(p, nil),
	})
	return []*mrpb.TimeSeries{
		createMetrics(p, queryStatePath, nil, now, queryState.state),
		createMetrics(p, queryOverallTimePath, nil, now, queryState.overallTime),
		createMetrics(p, queryServerTimePath, nil, now, queryState.serverTime),
	}, nil
}

// collectHANALogUtilisationKb collects the HANA log utilisation in Kbytes.
func collectHANALogUtilisationKb(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute) ([]*mrpb.TimeSeries, error) {
	if p.SkippedMetrics[logUtilisationKbPath] {
		return nil, nil
	}
	site, err := fetchHANAReplicationSite(ctx, p)
	if err != nil {
		return nil, err
	}
	if site == sapb.InstanceSite_HANA_STANDALONE {
		log.CtxLogger(ctx).Debugw("Skipping HANA log utilisation metric collection for standalone instance", "instanceid", p.SAPInstance.GetInstanceId())
		return nil, nil
	}
	args := fmt.Sprintf("-sk /hana/log/%s", p.SAPInstance.GetSapsid())
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "du",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command du", "args", args, "error", result.Error)
		return nil, result.Error
	}
	values := strings.Fields(result.StdOut)
	if len(values) < 1 {
		log.CtxLogger(ctx).Debugw("Command du returned no values. Skipping HANA log utilisation metric collection")
		return nil, nil
	}
	logUtilisationKb, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error while converting stdout value to int64", "value", values[0], "error", err)
		return nil, err
	}

	var labels map[string]string
	args = "-k /hana/log"
	result = exec(ctx, commandlineexecutor.Params{
		Executable:  "df",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command df. Skipping HANA log_disk_size label collection", "args", args, "error", result.Error)
		return []*mrpb.TimeSeries{
			createMetrics(p, logUtilisationKbPath, nil, tspb.Now(), logUtilisationKb),
		}, nil
	}
	outputLines := strings.Split(result.StdOut, "\n")
	if len(outputLines) < 2 {
		log.CtxLogger(ctx).Debugw("Command df returned less than expected rows. Skipping HANA log_disk_size label collection", "stdout", result.StdOut)
		return []*mrpb.TimeSeries{
			createMetrics(p, logUtilisationKbPath, nil, tspb.Now(), logUtilisationKb),
		}, nil
	}
	values = strings.Fields(outputLines[1])
	if len(values) >= 2 {
		labels = map[string]string{"hana_log_disk_size_kb": values[1]}
	} else {
		log.CtxLogger(ctx).Debugw("Command df returned less than expected values. Skipping HANA log_disk_size label collection", "stdout", result.StdOut)
	}

	return []*mrpb.TimeSeries{
		createMetrics(p, logUtilisationKbPath, labels, tspb.Now(), logUtilisationKb),
	}, nil
}

// fetchHANAReplicationSite fetches the HANA site using replication config.
func fetchHANAReplicationSite(ctx context.Context, p *InstanceProperties) (sapb.InstanceSite, error) {
	mode, _, _, err := p.ReplicationConfig(
		ctx,
		p.SAPInstance.GetUser(),
		p.SAPInstance.GetSapsid(),
		p.SAPInstance.GetInstanceId(),
		p.SapSystemInterface)

	if err != nil {
		log.CtxLogger(ctx).Debugw("Failed to refresh HANA HA Replication config for instance", "instanceid", p.SAPInstance.GetInstanceId(), "error", err)
		return 0, err
	}
	return sapdiscovery.HANASite(mode), nil
}

// runHANAQuery runs the hana query and returns the state and time taken in a struct.
// Uses SAP Instance's hana_db_user/hana_db_password or hdbuserstore_key for authentication with the DB.
// Returns an error in case of failures.
func runHANAQuery(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute) (queryState, error) {
	port := fmt.Sprintf("3%s15", p.SAPInstance.GetInstanceNumber())
	hdbsql := fmt.Sprintf("/usr/sap/%s/%s/exe/hdbsql", p.SAPInstance.GetSapsid(), p.SAPInstance.GetInstanceId())
	auth := ""
	if p.SAPInstance.GetHdbuserstoreKey() != "" {
		auth = fmt.Sprintf("-U %s", p.SAPInstance.GetHdbuserstoreKey())
	} else {
		auth = fmt.Sprintf("-n localhost:%s -u %s -p %s", port, p.SAPInstance.GetHanaDbUser(), p.SAPInstance.GetHanaDbPassword())
	}
	args := fmt.Sprintf("%s -j '%s'", auth, hanaQuery)

	result := exec(ctx, commandlineexecutor.Params{
		Executable:  hdbsql,
		ArgsToSplit: args,
		User:        p.SAPInstance.GetUser(),
	})
	log.CtxLogger(ctx).Debugw("HANA query command returned", "sql", hdbsql, "stdout", result.StdOut, "stderror", result.StdErr, "state", result.ExitCode, "err", result.Error)
	if strings.Contains(result.StdErr, "authentication failed") {
		p.HANAQueryFailCount++
	}
	// We do not want to check the result Error, error can exist even when the exec was successful.

	overallTime, err := parseQueryOutput(result.StdOut, queryOverallTime)
	if err != nil {
		return queryState{}, err
	}

	serverTime, err := parseQueryOutput(result.StdOut, queryServerTime)
	if err != nil {
		return queryState{}, err
	}

	return queryState{state: int64(result.ExitCode), overallTime: overallTime, serverTime: serverTime}, nil
}

// parseQueryOutput parses a string to get the time taken by the query.
func parseQueryOutput(str string, regex *regexp.Regexp) (int64, error) {
	matches := regex.FindStringSubmatch(str)
	log.Logger.Debugw("Matching substrings for regex", "regex", regex.String(), "matches", matches)
	if len(matches) != 2 {
		return 0, fmt.Errorf("error getting query time from output string: %q", str)
	}

	timeTaken, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}
	return int64(timeTaken), nil
}

// createMetrics - create mrpb.TimeSeries object for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val any) *mrpb.TimeSeries {
	mLabels := appendLabels(p, extraLabels)
	params := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(p.Config.CloudProperties),
		MetricType:   metricURL + mPath,
		MetricLabels: mLabels,
		Timestamp:    now,
		BareMetal:    p.Config.BareMetal,
	}
	switch val.(type) {
	case bool:
		params.BoolValue = val.(bool)
		log.Logger.Debugw("Create metric for instance", "key", mPath, "value", val.(bool), "instanceid", p.SAPInstance.GetInstanceId(), "labels", mLabels)
		return timeseries.BuildBool(params)
	case int64:
		params.Int64Value = val.(int64)
		log.Logger.Debugw("Create metric for instance", "key", mPath, "value", val.(int64), "instanceid", p.SAPInstance.GetInstanceId(), "labels", mLabels)
		return timeseries.BuildInt(params)
	default:
		log.Logger.Debugw("Invalid value type for metric", "metric", mPath, "value", val)
		return nil
	}
}

// appendLabels appends the default SAP Instance labels and extra labels
// to return a consolidated map of metric labels.
func appendLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":           p.SAPInstance.GetSapsid(),
		"instance_nr":   p.SAPInstance.GetInstanceNumber(),
		"instance_name": p.Config.CloudProperties.InstanceName,
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}

// boolToInt64 converts bool to int64.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
