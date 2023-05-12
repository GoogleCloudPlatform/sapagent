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

// Package netweaver contains functions that gather SAP NetWeaver system metrics for the SAP agent.
package netweaver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (
	// InstanceProperties struct has necessary context for Metrics collection.
	InstanceProperties struct {
		SAPInstance *sapb.SAPInstance
		Config      *cnfpb.Configuration
		Client      cloudmonitoring.TimeSeriesCreator
	}
)

// Netweaver system availability.
const (
	systemAtLeastOneProcessNotGreen = 0
	systemAllProcessesGreen         = 1
)

const (
	metricURL                  = "workload.googleapis.com"
	nwAvailabilityPath         = "/sap/nw/availability"
	nwServicePath              = "/sap/nw/service"
	nwICMRCodePath             = "/sap/nw/icm/rcode"
	nwICMRTimePath             = "/sap/nw/icm/rtime"
	nwMSResponseCodePath       = "/sap/nw/ms/rcode"
	nwMSResponseTimePath       = "/sap/nw/ms/rtime"
	nwMSWorkProcessesPath      = "/sap/nw/ms/wp"
	nwABAPProcBusyPath         = "/sap/nw/abap/proc/busy"
	nwABAPProcCountPath        = "/sap/nw/abap/proc/count"
	nwABAPProcQueueCurrentPath = "/sap/nw/abap/queue/current"
	nwABAPProcQueuePeakPath    = "/sap/nw/abap/queue/peak"
	nwABAPSessionsPath         = "/sap/nw/abap/sessions"
	nwABAPRFCPath              = "/sap/nw/abap/rfc"
	nwEnqLocksPath             = "/sap/nw/enq/locks/usercountowner"
)

var (
	msWorkProcess = regexp.MustCompile(`LB=([0-9]+)`)
)

// Collect is Netweaver implementation of Collector interface from processmetrics.go.
// Returns a list of NetWeaver related metrics.
func (p *InstanceProperties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	processListParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  p.SAPInstance.GetSapcontrolPath(),
		ArgsToSplit: fmt.Sprintf("-nr %s -function GetProcessList -format script", p.SAPInstance.GetInstanceNumber()),
		Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics := collectNetWeaverMetrics(p, commandlineexecutor.ExecuteCommand, processListParams)

	metrics = append(metrics, collectHTTPMetrics(p)...)

	abapProcessParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  p.SAPInstance.GetSapcontrolPath(),
		ArgsToSplit: fmt.Sprintf("-nr %s -function ABAPGetWPTable", p.SAPInstance.GetInstanceNumber()),
		Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectABAPProcessStatus(p, commandlineexecutor.ExecuteCommand, abapProcessParams)...)

	abapQueuesParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  p.SAPInstance.GetSapcontrolPath(),
		ArgsToSplit: fmt.Sprintf("-nr %s -function GetQueueStatistic", p.SAPInstance.GetInstanceNumber()),
		Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectABAPQueueStats(p, commandlineexecutor.ExecuteCommand, abapQueuesParams)...)

	dpmonPath := `/usr/sap/` + p.SAPInstance.GetSapsid() + `/SYS/exe/run/dpmon`
	command := `-c 'echo q | %s pf=%s v'`
	abapSessionParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(command, dpmonPath, p.SAPInstance.GetProfilePath()),
		Env: []string{
			"PATH=$PATH:" + p.SAPInstance.GetLdLibraryPath(),
			"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath(),
		},
	}
	metrics = append(metrics, collectABAPSessionStats(p, commandlineexecutor.ExecuteCommand, abapSessionParams)...)

	command = `-c 'echo q | %s pf=%s c'`
	abapRFCParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(command, dpmonPath, p.SAPInstance.GetProfilePath()),
		Env: []string{
			"PATH=$PATH:" + p.SAPInstance.GetLdLibraryPath(),
			"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath(),
		},
	}

	metrics = append(metrics, collectRFCConnections(p, commandlineexecutor.ExecuteCommand, abapRFCParams)...)

	enqLockParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  p.SAPInstance.GetSapcontrolPath(),
		ArgsToSplit: fmt.Sprintf("-nr %s -function EnqGetLockTable", p.SAPInstance.GetInstanceNumber()),
		Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectEnqLockMetrics(p, commandlineexecutor.ExecuteCommand, enqLockParams)...)

	return metrics
}

// collectNetWeaverMetrics builds a slice of SAP metrics containing all relevant NetWeaver metrics
func collectNetWeaverMetrics(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {

	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	procs, _, err := sc.ProcessList(exec, params)
	if err != nil {
		log.Logger.Errorw("Error getting ProcessList", log.Error(err))
		return nil
	}

	metrics, availabilityValue := collectServiceMetrics(p, procs, now)
	metrics = append(metrics, createMetrics(p, nwAvailabilityPath, nil, now, availabilityValue))

	return metrics
}

// collectServiceMetrics collects NetWeaver "service" metrics describing Netweaver service
// processes as managed by the sapcontrol program.
func collectServiceMetrics(p *InstanceProperties, procs map[int]*sapcontrol.ProcessStatus, now *tspb.Timestamp) (metrics []*mrpb.TimeSeries, availabilityValue int64) {
	start := tspb.Now()
	availabilityValue = systemAllProcessesGreen

	processNames := []string{"msg_server", "enserver", "enrepserver", "disp+work", "gwrd", "icman", "jstart", "jcontrol", "enq_replicator", "enq_server", "sapwebdisp"}
	for _, proc := range procs {
		if contains(processNames, proc.Name) && !proc.IsGreen {
			availabilityValue = systemAtLeastOneProcessNotGreen
		}

		instanceType := p.SAPInstance.GetKind().String()
		switch proc.Name {
		case "gwrd", "sapwebdisp":
			// For gwrd or web dispatcher process running on another instance, categorize it as "APP"
			instanceType = sapb.InstanceKind_APP.String()
		}
		extraLabels := map[string]string{
			"service_name":  proc.Name,
			"instance_type": instanceType,
		}
		value := boolToInt64(proc.IsGreen)

		log.Logger.Debugw("Creating metrics for process",
			"metric", nwServicePath, "process", proc.Name, "instanceid", p.SAPInstance.GetInstanceId(), "value", value)
		metrics = append(metrics, createMetrics(p, nwServicePath, extraLabels, now, value))
	}
	log.Logger.Debugw("Time taken to collect metrics in collectServiceMetrics()", "time", time.Since(start.AsTime()))
	return metrics, availabilityValue
}

// collectHTTPMetrics collects the HTTP health check metrics for different types of
// Netweaver instances based on their types.
func collectHTTPMetrics(p *InstanceProperties) []*mrpb.TimeSeries {
	url := p.SAPInstance.GetNetweaverHealthCheckUrl()
	if url == "" {
		return nil
	}
	log.Logger.Debugw("SAP Instance HTTP health check", "instanceid", p.SAPInstance.GetInstanceId(), "url", url)

	switch p.SAPInstance.GetServiceName() {
	case "SAP-ICM-ABAP", "SAP-ICM-JAVA":
		return collectICMMetrics(p, url)
	case "SAP-CS":
		return collectMessageServerMetrics(p, url)
	default:
		return nil
	}
}

// collectICMMetrics makes HTTP GET request on given URL.
// Returns metrics built using:
//   - HTTP response code.
//   - Total time taken by the request.
func collectICMMetrics(p *InstanceProperties, url string) []*mrpb.TimeSeries {
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.Logger.Debugw("HTTP GET failed", "instanceid", p.SAPInstance.GetInstanceId(), "url", url, "error", err)
		return nil
	}
	defer response.Body.Close()

	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	log.Logger.Debugw("Time taken to collect metrics in collectICMMetrics", "time", time.Since(now.AsTime()))
	return []*mrpb.TimeSeries{
		createMetrics(p, nwICMRCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwICMRTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}
}

// collectMessageServerMetrics uses HTTP GET on given URL to collect message server metrics.
// Returns
//   - Two metrics - HTTP response code and response time for all HTTP status codes.
//   - Additional work process count as reported by the message server info page on StatusOK(200).
//   - A nil in case of errors in HTTP GET request failures.
func collectMessageServerMetrics(p *InstanceProperties, url string) []*mrpb.TimeSeries {
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.Logger.Debugw("HTTP GET failed", "instanceid", p.SAPInstance.GetInstanceId(), "url", url, "error", err)
		return nil
	}
	defer response.Body.Close()

	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	metrics := []*mrpb.TimeSeries{
		createMetrics(p, nwMSResponseCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwMSResponseTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}

	if response.StatusCode != http.StatusOK {
		log.Logger.Debugw("HTTP GET failed", "statuscode", response.StatusCode, "response", response)
		log.Logger.Debugw("Time taken to collect metrics in collectMessageServerMetrics()", "time", time.Since(now.AsTime()))
		return metrics
	}

	workProcessCount, err := parseWorkProcessCount(response.Body)
	if err != nil {
		log.Logger.Debugw("Reading work process count from message server info page failed", "error", err)
		return metrics
	}
	log.Logger.Debugw("Time taken to collect metrics in collectMessageServerMetrics()", "time", time.Since(now.AsTime()))

	return append(metrics, createMetrics(p, nwMSWorkProcessesPath, extraLabels, now, int64(workProcessCount)))
}

// parseWorkProcessCount processes the HTTP text/plain response body one line at a time
// to find the message server work process count.
// Returns the work process count on success, an error on failures.
func parseWorkProcessCount(r io.ReadCloser) (count int, err error) {
	scanner := bufio.NewScanner(r)
	// NOMUTANTS--cannot test if text is or is not read one line at a time.
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		if match := msWorkProcess.FindStringSubmatch(scanner.Text()); len(match) == 2 {
			if count, err = strconv.Atoi(match[1]); err != nil {
				return 0, err
			}
			return count, nil
		}
	}
	return 0, fmt.Errorf("work process count not found")
}

// collectABAPProcessStatus collects the ABAP worker process status metrics.
func collectABAPProcessStatus(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {
	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	processCount, busyProcessCount, _, err := sc.ParseABAPGetWPTable(exec, params)
	if err != nil {
		log.Logger.Debugw("Command sapcontrol failed", "error", err)
		return nil
	}

	var metrics []*mrpb.TimeSeries
	for k, v := range processCount {
		extraLabels := map[string]string{"abap_process": k}
		log.Logger.Debugw("Creating metric for abap_process",
			"metric", nwABAPProcCountPath, "abapprocess", k, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPProcCountPath, extraLabels, now, int64(v)))
	}

	for k, v := range busyProcessCount {
		extraLabels := map[string]string{"abap_process": k}
		log.Logger.Debugw("Creating metric for abap_process",
			"metric", nwABAPProcCountPath, "abapprocess", k, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPProcBusyPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugw("Time taken to collect metrics in collectABAPProcessStatus()", "time", time.Since(now.AsTime()))
	return metrics
}

// collectABAPQueueStats collects ABAP Queue utilization metrics using dpmon tool.
func collectABAPQueueStats(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {
	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	currentQueueUsage, peakQueueUsage, err := sc.ParseQueueStats(exec, params)
	if err != nil {
		log.Logger.Debugw("Command sapcontrol failed", "error", err)
		return nil
	}

	var metrics []*mrpb.TimeSeries
	for k, v := range currentQueueUsage {
		extraLabels := map[string]string{"abap_queue": k}
		log.Logger.Debugw("Creating metric with labels",
			"metric", nwABAPProcQueueCurrentPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueueCurrentPath, extraLabels, now, int64(v)))
	}

	for k, v := range peakQueueUsage {
		extraLabels := map[string]string{"abap_queue_peak": k}
		log.Logger.Debugw("Creating metric with labels",
			"metric", nwABAPProcQueueCurrentPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueuePeakPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugw("Time taken to collect metrics in collectABAPQueueStats()", "time", time.Since(now.AsTime()))
	return metrics
}

// collectABAPSessionStats collects ABAP session related metrics using dpmon tool.
func collectABAPSessionStats(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {
	now := tspb.Now()

	results := exec(params)
	log.Logger.Debugw("DPMON for sessionStat output", "stdout", results.StdOut, "stderr", results.StdErr, "exitcode", results.ExitCode, "error", results.Error)
	if results.Error != nil {
		log.Logger.Debugw("DPMON failed", log.Error(results.Error))
		return nil
	}

	var metrics []*mrpb.TimeSeries
	sessionCounts, totalCount, err := parseABAPSessionStats(results.StdOut)
	if err != nil {
		log.Logger.Debugw("DPMON ran successfully, but no ABAP session currently active", log.Error(err))
		return nil
	}

	for k, v := range sessionCounts {
		extraLabels := map[string]string{"abap_session_type": k}
		log.Logger.Debugw("Creating metric with labels",
			"metric", nwABAPSessionsPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(v)))
	}

	extraLabels := map[string]string{"abap_session_type": "total_count"}
	log.Logger.Debugw("Creating metric with labels",
		"metric", nwABAPSessionsPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", totalCount)

	metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(totalCount)))

	log.Logger.Debugw("Time taken to collect metrics in collectABAPSessionStats()", "time", time.Since(now.AsTime()))
	return metrics
}

// collectRFCConnections collects the ABAP RFC connection metrics using dpmon tool.
func collectRFCConnections(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {
	now := tspb.Now()

	result := exec(params)
	log.Logger.Debugw("DPMON for RFC output", "stdout", result.StdOut, "stderr", result.StdErr, "exitcode", result.ExitCode, "error", result.Error)
	if result.Error != nil {
		log.Logger.Debugw("DPMON for RFC failed", log.Error(result.Error))
		return nil
	}

	var metrics []*mrpb.TimeSeries
	rfcStateCount := parseRFCStats(result.StdOut)
	for k, v := range rfcStateCount {
		extraLabels := map[string]string{"abap_rfc_conn": k}
		log.Logger.Debugw("Creating metric with labels",
			"metric", nwABAPRFCPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPRFCPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugw("Time taken to collect metrics in collectRFCConnections()", "time", time.Since(now.AsTime()))
	return metrics
}

// collectEnqLockMetrics builds Enq Locks for SAP Netweaver ASCS instances.
func collectEnqLockMetrics(p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) []*mrpb.TimeSeries {

	instance := p.SAPInstance.GetInstanceId()
	if !strings.HasPrefix(instance, "ASCS") && !strings.HasPrefix(instance, "ERS") {
		log.Logger.Debugw("The Enq Lock metric is only applicable for application type: ASCS.", "InstanceID", p.SAPInstance.InstanceId)
		return nil
	}
	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	enqLocks, error := sc.EnqGetLockTable(exec, params)
	if error != nil {
		return nil
	}

	var metrics []*mrpb.TimeSeries
	for _, lock := range enqLocks {
		extraLabels := map[string]string{
			"lock_name":           lock.LockName,
			"lock_mode":           lock.LockMode,
			"user_count_owner":    fmt.Sprintf("%d", lock.UserCountOwner),
			"user_count_owner_vb": fmt.Sprintf("%d", lock.UserCountOwnerVB),
			"transaction":         lock.Transaction,
			"object":              lock.Object,
		}
		log.Logger.Debugw("Creating metric with labels",
			"metric", nwEnqLocksPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", lock.UserCountOwner)
		metrics = append(metrics, createMetrics(p, nwEnqLocksPath, extraLabels, now, lock.UserCountOwner))

	}
	return metrics
}

// createMetrics - create mrpb.TimeSeries object for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: metricLabels(p, extraLabels),
		Timestamp:    now,
		Int64Value:   val,
		BareMetal:    p.Config.BareMetal,
	}
	return timeseries.BuildInt(params)
}

// metricLabels appends the default SAP Instance labels and extra labels
// to return a consolidated map of metric labels.
func metricLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":         p.SAPInstance.GetSapsid(),
		"instance_nr": p.SAPInstance.GetInstanceNumber(),
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}

// contains checks if a string slice contains a given string.
func contains(list []string, s string) bool {
	for _, t := range list {
		if s == t {
			return true
		}
	}
	return false
}

// boolToInt64 converts bool to int64.
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
