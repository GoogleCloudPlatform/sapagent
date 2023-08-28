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

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

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
	haReplicationPath    = "/sap/hana/ha/replication"
	availabilityPath     = "/sap/hana/availability"
	haAvailabilityPath   = "/sap/hana/ha/availability"
	queryStatePath       = "/sap/hana/query/state"
	queryOverallTimePath = "/sap/hana/query/overalltime"
	queryServerTimePath  = "/sap/hana/query/servertime"
	hanaQuery            = "select * from dummy"
)

var (
	queryOverallTime = regexp.MustCompile("overall time ([0-9]+) usec")
	queryServerTime  = regexp.MustCompile("server time ([0-9]+) usec")
)

// Collect is HANA implementation of Collector interface from processmetrics.go.
// Returns a list of HANA related metrics.
func (p *InstanceProperties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	scc := sapcontrolclient.New(p.SAPInstance.GetInstanceNumber())

	metrics := collectReplicationHA(ctx, p, scc)

	// Collect DB Query metrics only if credentials are set and NOT a HANA secondary.
	if p.SAPInstance.GetHanaDbUser() != "" && p.SAPInstance.GetHanaDbPassword() != "" && p.SAPInstance.GetSite() != sapb.InstanceSite_HANA_SECONDARY {
		queryMetrics := collectHANAQueryMetrics(ctx, p, commandlineexecutor.ExecuteCommand)
		metrics = append(metrics, queryMetrics...)
	}

	return metrics
}

func collectReplicationHA(ctx context.Context, ip *InstanceProperties, scc sapcontrol.ClientInterface) []*mrpb.TimeSeries {
	log.Logger.Debugw("Collecting HANA Replication HA metrics for instance", "instanceid", ip.SAPInstance.GetInstanceId())

	now := tspb.Now()
	sc := &sapcontrol.Properties{ip.SAPInstance}
	var (
		err               error
		processes         map[int]*sapcontrol.ProcessStatus
		metrics           []*mrpb.TimeSeries
	)
	processes, err = sc.GetProcessList(scc)
	if err != nil {
		log.Logger.Errorw("Error executing GetProcessList SAPControl API", log.Error(err))
	}

	if err == nil {
		// If GetProcessList via command line or API didn't return an error.
		metrics = collectHANAServiceMetrics(ip, processes, now)
	}

	haReplicationValue := refreshHAReplicationConfig(ctx, ip)
	extraLabels := map[string]string{
		"ha_members": strings.Join(ip.SAPInstance.GetHanaHaMembers(), ","),
	}
	metrics = append(metrics, createMetrics(ip, haReplicationPath, extraLabels, now, haReplicationValue))

	log.Logger.Debugw("Time taken to collect metrics in CollectReplicationHA()", "duration", time.Since(now.AsTime()))
	return metrics
}

// collectHANAServiceMetrics creates metrics for each of the services in
// a HANA instance. Also returns availabilityValue a signal that depends
// on the status of the HANA services.
func collectHANAServiceMetrics(p *InstanceProperties, processes map[int]*sapcontrol.ProcessStatus, now *tspb.Timestamp) (metrics []*mrpb.TimeSeries) {
	if len(processes) == 0 {
		return nil
	}

	for _, process := range processes {
		extraLabels := map[string]string{
			"service_name": process.Name,
			"pid":          process.PID,
		}
		metrics = append(metrics, createMetrics(p, servicePath, extraLabels, now, boolToInt64(process.IsGreen)))
	}
	return metrics
}

// collectHANAQueryMetrics collects the query state by running a HANA DB query.
//   - state : Value is 0 in case of successful query.
//   - overall time: Overall time taken by query in micro seconds.
//   - servertime: Time spent on the server side in micro seconds.
func collectHANAQueryMetrics(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute) []*mrpb.TimeSeries {
	now := tspb.Now()
	if p.HANAQueryFailCount >= maxHANAQueryFailCount {
		// if HANAQueryFailCount reaches maxHANAQueryFailCount we should not let it
		// query again, because the user can be locked out.
		log.Logger.Debugw("Not queryig for HANAQuery Metrics as failcount has reached max allowed fail count.", "instanceid", p.SAPInstance.GetInstanceId(), "failcount", p.HANAQueryFailCount)
		return nil
	}
	queryState, err := runHANAQuery(ctx, p, exec)
	if err != nil {
		log.Logger.Errorw("Error in running query", log.Error(err))
		// Return a non-zero state in case of query failure.
		return []*mrpb.TimeSeries{createMetrics(p, queryStatePath, nil, now, 1)}
	}

	log.Logger.Debugw("HANA query metrics for instance", "instanceid", p.SAPInstance.GetInstanceId(), "querystate", queryState)
	return []*mrpb.TimeSeries{
		createMetrics(p, queryStatePath, nil, now, queryState.state),
		createMetrics(p, queryOverallTimePath, nil, now, queryState.overallTime),
		createMetrics(p, queryServerTimePath, nil, now, queryState.serverTime),
	}
}

// runHANAQuery runs the hana query and returns the state and time taken in a struct.
// Uses SAP Instance's hana_db_user, hana_db_password for authentication with the DB.
// Returns an error in case of failures.
func runHANAQuery(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute) (queryState, error) {
	port := fmt.Sprintf("3%s15", p.SAPInstance.GetInstanceNumber())
	hdbsql := fmt.Sprintf("/usr/sap/%s/%s/exe/hdbsql", p.SAPInstance.GetSapsid(), p.SAPInstance.GetInstanceId())
	args := fmt.Sprintf("-n localhost:%s -j -u %s -p %s '%s'",
		port, p.SAPInstance.GetHanaDbUser(), p.SAPInstance.GetHanaDbPassword(), hanaQuery)

	result := exec(ctx, commandlineexecutor.Params{
		Executable:  hdbsql,
		ArgsToSplit: args,
		User:        p.SAPInstance.GetUser(),
	})
	log.Logger.Errorw("HANA query command returned", "sql", hdbsql, "stdout", result.StdOut, "stderror", result.StdErr, "state", result.ExitCode, "err", result.Error)
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
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *mrpb.TimeSeries {
	mLabels := appendLabels(p, extraLabels)
	params := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: mLabels,
		Timestamp:    now,
		Int64Value:   val,
		BareMetal:    p.Config.BareMetal,
	}
	log.Logger.Debugw("Create metric for instance", "key", mPath, "value", val, "instanceid", p.SAPInstance.GetInstanceId(), "labels", mLabels)
	return timeseries.BuildInt(params)
}

// appendLabels appends the default SAP Instance labels and extra labels
// to return a consolidated map of metric labels.
func appendLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":         p.SAPInstance.GetSapsid(),
		"instance_nr": p.SAPInstance.GetInstanceNumber(),
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}

// refreshHAReplicationConfig reads the current HA and replication config.
// Updates the Site, HanaHaMembers entries in the SAP instance proto.
// Returns the exit code of systemReplicationStatus.py.
func refreshHAReplicationConfig(ctx context.Context, p *InstanceProperties) int64 {
	mode, haMembers, replicationStatus, err := sapdiscovery.HANAReplicationConfig(
		ctx,
		p.SAPInstance.GetUser(),
		p.SAPInstance.GetSapsid(),
		p.SAPInstance.GetInstanceId())

	//This is not in-band error handling. Metric should be zero in case of failures.
	if err != nil {
		log.Logger.Debugw("Failed to refresh HANA HA Replication config for instance", "instanceid", p.SAPInstance.GetInstanceId())
		return 0
	}

	p.SAPInstance.Site = sapdiscovery.HANASite(mode)
	p.SAPInstance.HanaHaMembers = haMembers
	return int64(replicationStatus)
}

// boolToInt64 converts bool to int64.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
