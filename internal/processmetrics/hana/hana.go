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
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (
	runCmdAsUserExitCode func(string, string, string) (string, string, int64, error)

	// Time taken is in microseconds.
	queryState struct {
		state, overallTime, serverTime int64
	}

	// InstanceProperties has necessary context for Metrics collection.
	// InstanceProperties implements Collector interface for HANA.
	InstanceProperties struct {
		SAPInstance *sapb.SAPInstance
		Config      *cnfpb.Configuration
		Client      cloudmonitoring.TimeSeriesCreator
	}
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
func (p *InstanceProperties) Collect(ctx context.Context) []*sapdiscovery.Metrics {
	processListRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: p.SAPInstance.GetSapcontrolPath(),
		Args:       fmt.Sprintf("-nr %s -function GetProcessList -format script", p.SAPInstance.GetInstanceNumber()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics := collectReplicationHA(p, processListRunner)

	// Collect DB Query metrics only if credentials are set and NOT a HANA secondary.
	if p.SAPInstance.GetHanaDbUser() != "" && p.SAPInstance.GetHanaDbPassword() != "" && p.SAPInstance.GetSite() != sapb.InstanceSite_HANA_SECONDARY {
		queryMetrics := collectHANAQueryMetrics(p, commandlineexecutor.ExpandAndExecuteCommandAsUserExitCode)
		metrics = append(metrics, queryMetrics...)
	}

	return metrics
}

func collectReplicationHA(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {
	log.Logger.Debugw("Collecting HANA Replication HA metrics for instance", "instanceid", p.SAPInstance.GetInstanceId())

	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	processes, sapControlResult, err := sc.ProcessList(r)
	if err != nil {
		log.Logger.Errorw("Error getting ProcessList", log.Error(err))
		return nil
	}
	metrics, availabilityValue := collectHANAServiceMetrics(p, processes, now)
	metrics = append(metrics, createMetrics(p, availabilityPath, nil, now, availabilityValue))

	haReplicationValue := refreshHAReplicationConfig(p)
	extraLabels := map[string]string{
		"ha_members": strings.Join(p.SAPInstance.GetHanaHaMembers(), ","),
	}
	metrics = append(metrics, createMetrics(p, haReplicationPath, extraLabels, now, haReplicationValue))

	haAvailabilityValue := haAvailabilityValue(p, int64(sapControlResult), haReplicationValue)
	metrics = append(metrics, createMetrics(p, haAvailabilityPath, nil, now, haAvailabilityValue))

	log.Logger.Debugw("Time taken to collect metrics in CollectReplicationHA()", "duration", time.Since(now.AsTime()))
	return metrics
}

// collectHANAServiceMetrics creates metrics for each of the services in
// a HANA instance. Also returns availabilityValue a signal that depends
// on the status of the HANA services.
func collectHANAServiceMetrics(p *InstanceProperties, processes map[int]*sapcontrol.ProcessStatus, now *tspb.Timestamp) (metrics []*sapdiscovery.Metrics, availabilityValue int64) {
	if len(processes) == 0 {
		return nil, 0
	}

	availabilityValue = systemAllProcessesGreen
	for _, process := range processes {
		if !process.IsGreen {
			availabilityValue = systemAtLeastOneProcessNotGreen
		}
		extraLabels := map[string]string{
			"service_name": process.Name,
			"pid":          process.PID,
		}
		metrics = append(metrics, createMetrics(p, servicePath, extraLabels, now, boolToInt64(process.IsGreen)))
	}
	return metrics, availabilityValue
}

// collectHANAQueryMetrics collects the query state by running a HANA DB query.
//   - state : Value is 0 in case of successful query.
//   - overalltime: Overall time taken by query in micro seconds.
//   - servertime: Time spent on the server side in micro seconds.
func collectHANAQueryMetrics(p *InstanceProperties, run runCmdAsUserExitCode) []*sapdiscovery.Metrics {
	now := tspb.Now()
	queryState, err := runHANAQuery(p, run)
	if err != nil {
		log.Logger.Errorw("Error in running query", log.Error(err))
		// Return a non-zero state in case of query failure.
		return []*sapdiscovery.Metrics{createMetrics(p, queryStatePath, nil, now, 1)}
	}

	log.Logger.Debugw("HANA query metrics for instance", "instanceid", p.SAPInstance.GetInstanceId(), "querystate", queryState)
	return []*sapdiscovery.Metrics{
		createMetrics(p, queryStatePath, nil, now, queryState.state),
		createMetrics(p, queryOverallTimePath, nil, now, queryState.overallTime),
		createMetrics(p, queryServerTimePath, nil, now, queryState.serverTime),
	}
}

// runHANAQuery runs the hana query and returns the state and time taken in a struct.
// Uses SAP Instance's hana_db_user, hana_db_password for authentication with the DB.
// Returns an error in case of failures.
func runHANAQuery(p *InstanceProperties, run runCmdAsUserExitCode) (queryState, error) {
	port := fmt.Sprintf("3%s15", p.SAPInstance.GetInstanceNumber())
	hdbsql := fmt.Sprintf("/usr/sap/%s/%s/exe/hdbsql", p.SAPInstance.GetSapsid(), p.SAPInstance.GetInstanceId())
	args := fmt.Sprintf("-n localhost:%s -j -u %s -p %s '%s'",
		port, p.SAPInstance.GetHanaDbUser(), p.SAPInstance.GetHanaDbPassword(), hanaQuery)

	stdOut, stdErr, state, err := run(p.SAPInstance.GetUser(), hdbsql, args)
	log.Logger.Debugw("HANA query command returned", "sql", hdbsql, "stdout", stdOut, "stderror", stdErr, "state", state, "err", err)
	if err != nil {
		return queryState{}, err
	}

	overallTime, err := parseQueryOutput(stdOut, queryOverallTime)
	if err != nil {
		return queryState{}, err
	}

	serverTime, err := parseQueryOutput(stdOut, queryServerTime)
	if err != nil {
		return queryState{}, err
	}

	return queryState{state: state, overallTime: overallTime, serverTime: serverTime}, nil
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

// createMetrics - create sapdiscovery.Metrics  object for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *sapdiscovery.Metrics {
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
	return &sapdiscovery.Metrics{TimeSeries: timeseries.BuildInt(params)}
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

func haAvailabilityValue(p *InstanceProperties, sapControlResult int64, replicationStatus int64) int64 {
	var haAvVal int64 = unknownState
	switch replicationStatus {
	case replicationActive:
		if sapControlResult == sapControlAllProcessesRunning {
			haAvVal = primaryOnlineReplicationRunning
		} else {
			haAvVal = primaryHasError
		}
	case replicationUnknown:
		if p.SAPInstance.GetSite() == sapb.InstanceSite_HANA_PRIMARY {
			if sapControlResult == sapControlAllProcessesRunning {
				haAvVal = primaryOnlineReplicationNotFunctional
			} else {
				haAvVal = primaryHasError
			}
		} else {
			haAvVal = currentNodeSecondary
		}
	case replicationOff, replicationConnectionError, replicationInitialization, replicationSyncing:
		if sapControlResult == sapControlAllProcessesRunning {
			haAvVal = primaryOnlineReplicationNotFunctional
		} else {
			haAvVal = primaryHasError
		}
	default:
		log.Logger.Warn("HANA HA availability state unknown.")
	}
	log.Logger.Debugw("HANA HA availability for sapcontrol",
		"availability", haAvVal, "returncode", sapControlResult, "replicationcode", replicationStatus)
	return haAvVal
}

// refreshHAReplicationConfig reads the current HA and replication config.
// Updates the Site, HanaHaMembers entries in the SAP instance proto.
// Returns the exit code of systemReplicationStatus.py.
func refreshHAReplicationConfig(p *InstanceProperties) int64 {
	mode, haMembers, replicationStatus, err := sapdiscovery.HANAReplicationConfig(
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
