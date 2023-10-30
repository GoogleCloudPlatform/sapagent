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

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (
	// InstanceProperties struct has necessary context for Metrics collection.
	InstanceProperties struct {
		SAPInstance     *sapb.SAPInstance
		Config          *cnfpb.Configuration
		Client          cloudmonitoring.TimeSeriesCreator
		SkippedMetrics  map[string]bool
		PMBackoffPolicy backoff.BackOffContext
	}
)

// Netweaver system availability.
const (
	systemAtLeastOneProcessNotGreen = 0
	systemAllProcessesGreen         = 1
)

const (
	metricURL                  = "workload.googleapis.com"
	nwServicePath              = "/sap/nw/service"
	nwICMRCodePath             = "/sap/nw/icm/rcode"
	nwICMRTimePath             = "/sap/nw/icm/rtime"
	nwMSResponseCodePath       = "/sap/nw/ms/rcode"
	nwMSResponseTimePath       = "/sap/nw/ms/rtime"
	nwMSWorkProcessesPath      = "/sap/nw/ms/wp"
	nwABAPProcBusyPath         = "/sap/nw/abap/proc/busy"
	nwABAPProcCountPath        = "/sap/nw/abap/proc/count"
	nwABAPProcUtilPath         = "/sap/nw/abap/proc/utilization"
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
// Collect method keeps on collecting all the metrics it can, logs errors if it encounters
// any and returns the collected metrics with the last error encountered while collecting metrics.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	scc := sapcontrolclient.New(p.SAPInstance.GetInstanceNumber())
	var metricsCollectionError error
	metrics, err := collectNetWeaverMetrics(ctx, p, scc)
	if err != nil {
		metricsCollectionError = err
	}

	httpMetrics, err := collectHTTPMetrics(ctx, p)
	if err != nil {
		metricsCollectionError = err
	}
	if httpMetrics != nil {
		metrics = append(metrics, httpMetrics...)
	}

	abapProcessStatusMetrics, err := collectABAPProcessStatus(ctx, p, scc)
	if err != nil {
		metricsCollectionError = err
	}
	if abapProcessStatusMetrics != nil {
		metrics = append(metrics, abapProcessStatusMetrics...)
	}

	abapQueueStats, err := collectABAPQueueStats(ctx, p, scc)
	if err != nil {
		metricsCollectionError = err
	}
	if abapQueueStats != nil {
		metrics = append(metrics, abapQueueStats...)
	}

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
	abapSessionStats, err := collectABAPSessionStats(ctx, p, commandlineexecutor.ExecuteCommand, abapSessionParams)
	if err != nil {
		metricsCollectionError = err
	}
	if abapSessionStats != nil {
		metrics = append(metrics, abapSessionStats...)
	}

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

	rffcConnectionsMetric, err := collectRFCConnections(ctx, p, commandlineexecutor.ExecuteCommand, abapRFCParams)
	if err != nil {
		metricsCollectionError = err
	}
	if rffcConnectionsMetric != nil {
		metrics = append(metrics, rffcConnectionsMetric...)
	}

	enqLockParams := commandlineexecutor.Params{
		User:        p.SAPInstance.GetUser(),
		Executable:  p.SAPInstance.GetSapcontrolPath(),
		ArgsToSplit: fmt.Sprintf("-nr %s -function EnqGetLockTable", p.SAPInstance.GetInstanceNumber()),
		Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	enqLockMetrics, err := collectEnqLockMetrics(ctx, p, commandlineexecutor.ExecuteCommand, enqLockParams, scc)
	if err != nil {
		metricsCollectionError = err
	}
	if enqLockMetrics != nil {
		metrics = append(metrics, enqLockMetrics...)
	}

	return metrics, metricsCollectionError
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		log.CtxLogger(ctx).Infof("Attempting collector retry", "attempt", attempt)
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Retry limit exceeded", "InstanceId", p.SAPInstance.GetInstanceId(), "error", err)
	}
	return res, err
}

// collectNetWeaverMetrics builds a slice of SAP metrics containing all relevant NetWeaver metrics
func collectNetWeaverMetrics(ctx context.Context, p *InstanceProperties, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	if _, ok := p.SkippedMetrics[nwServicePath]; ok {
		return nil, nil
	}
	now := tspb.Now()
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	var (
		err   error
		procs map[int]*sapcontrol.ProcessStatus
	)
	procs, err = sc.GetProcessList(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error performing GetProcessList web method", log.Error(err))
		return nil, err
	}
	metrics := collectServiceMetrics(ctx, p, procs, now)
	return metrics, nil
}

// collectServiceMetrics collects NetWeaver "service" metrics describing Netweaver service
// processes as managed by the sapcontrol program.
func collectServiceMetrics(ctx context.Context, p *InstanceProperties, procs map[int]*sapcontrol.ProcessStatus, now *tspb.Timestamp) (metrics []*mrpb.TimeSeries) {
	start := tspb.Now()

	for _, proc := range procs {
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

		log.CtxLogger(ctx).Debugw("Creating metrics for process",
			"metric", nwServicePath, "process", proc.Name, "instanceid", p.SAPInstance.GetInstanceId(), "value", value)
		metrics = append(metrics, createMetrics(p, nwServicePath, extraLabels, now, value))
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectServiceMetrics()", "time", time.Since(start.AsTime()))
	return metrics
}

// collectHTTPMetrics collects the HTTP health check metrics for different types of
// Netweaver instances based on their types.
func collectHTTPMetrics(ctx context.Context, p *InstanceProperties) ([]*mrpb.TimeSeries, error) {
	url := p.SAPInstance.GetNetweaverHealthCheckUrl()
	if url == "" {
		log.CtxLogger(ctx).Debugw("SAP Instance HTTP health check URL is empty", "instanceid", p.SAPInstance.GetInstanceId(), "url", url)
		return nil, fmt.Errorf("SAP Instance HTTP health check URL is empty %s", p.SAPInstance.GetInstanceId())
	}
	log.CtxLogger(ctx).Debugw("SAP Instance HTTP health check", "instanceid", p.SAPInstance.GetInstanceId(), "url", url)

	switch p.SAPInstance.GetServiceName() {
	case "SAP-ICM-ABAP", "SAP-ICM-JAVA":
		return collectICMMetrics(ctx, p, url)
	case "SAP-CS":
		return collectMessageServerMetrics(ctx, p, url)
	default:
		log.CtxLogger(ctx).Debugw("unsupported service name: %s", p.SAPInstance.GetServiceName())
		return nil, fmt.Errorf("unsupported service name: %s", p.SAPInstance.GetServiceName())
	}
}

// collectICMMetrics makes HTTP GET request on given URL.
// Returns metrics built using:
//   - HTTP response code.
//   - Total time taken by the request.
func collectICMMetrics(ctx context.Context, p *InstanceProperties, url string) ([]*mrpb.TimeSeries, error) {
	// Since these metrics are derived from the same operation, even if one of the metric is skipped the whole group will be skipped from collection.
	if _, ok := p.SkippedMetrics[nwICMRCodePath]; ok {
		return nil, nil
	}
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.CtxLogger(ctx).Debugw("HTTP GET failed", "instanceid", p.SAPInstance.GetInstanceId(), "url", url, "error", err)
		return nil, err
	}
	defer response.Body.Close()

	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectICMMetrics", "time", time.Since(now.AsTime()))
	return []*mrpb.TimeSeries{
		createMetrics(p, nwICMRCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwICMRTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}, nil
}

// collectMessageServerMetrics uses HTTP GET on given URL to collect message server metrics.
// Returns
//   - Two metrics - HTTP response code and response time for all HTTP status codes.
//   - Additional work process count as reported by the message server info page on StatusOK(200).
//   - A nil in case of errors in HTTP GET request failures.
func collectMessageServerMetrics(ctx context.Context, p *InstanceProperties, url string) ([]*mrpb.TimeSeries, error) {
	// Since these metrics are derived from the same operation, even if one of the metric is skipped the whole group will be skipped from collection.
	if _, ok := p.SkippedMetrics[nwMSResponseCodePath]; ok {
		return nil, nil
	}
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.CtxLogger(ctx).Debugw("HTTP GET failed", "instanceid", p.SAPInstance.GetInstanceId(), "url", url, "error", err)
		return nil, err
	}
	defer response.Body.Close()

	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	metrics := []*mrpb.TimeSeries{
		createMetrics(p, nwMSResponseCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwMSResponseTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}

	if response.StatusCode != http.StatusOK {
		log.CtxLogger(ctx).Debugw("HTTP GET failed", "statuscode", response.StatusCode, "response", response)
		log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectMessageServerMetrics()", "time", time.Since(now.AsTime()))
		return nil, fmt.Errorf("HTTP GET failed code: %d", response.StatusCode)
	}

	workProcessCount, err := parseWorkProcessCount(response.Body)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Reading work process count from message server info page failed", "error", err)
		return nil, err
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectMessageServerMetrics()", "time", time.Since(now.AsTime()))

	return append(metrics, createMetrics(p, nwMSWorkProcessesPath, extraLabels, now, int64(workProcessCount))), nil
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
func collectABAPProcessStatus(ctx context.Context, p *InstanceProperties, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	now := tspb.Now()
	// Since these metrics are derived from the same operation, even if one of the metric is skipped the whole group will be skipped from collection.
	skipABAPProcessStatus := p.SkippedMetrics[nwABAPProcCountPath] || p.SkippedMetrics[nwABAPProcBusyPath] || p.SkippedMetrics[nwABAPProcUtilPath]
	if skipABAPProcessStatus {
		return nil, nil
	}
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	var (
		err              error
		processCount     map[string]int
		busyProcessCount map[string]int
		busyPercentage   map[string]int
	)
	wpDetails, err := sc.ABAPGetWPTable(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Sapcontrol web method failed", "error", err)
		return nil, err
	}
	processCount = wpDetails.Processes
	busyProcessCount = wpDetails.BusyProcesses
	busyPercentage = wpDetails.BusyProcessPercentage

	var metrics []*mrpb.TimeSeries
	for k, v := range processCount {
		extraLabels := map[string]string{"abap_process": k}
		log.CtxLogger(ctx).Debugw("Creating metric for abap_process",
			"metric", nwABAPProcCountPath, "abapprocess", k, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPProcCountPath, extraLabels, now, int64(v)))
	}

	for k, v := range busyProcessCount {
		extraLabels := map[string]string{"abap_process": k}
		log.CtxLogger(ctx).Debugw("Creating metric for abap_process",
			"metric", nwABAPProcBusyPath, "abapprocess", k, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPProcBusyPath, extraLabels, now, int64(v)))
	}

	for k, v := range busyPercentage {
		extraLabels := map[string]string{"abap_process": k}
		log.CtxLogger(ctx).Debugw("Creating metric for abap_process",
			"metric", nwABAPProcUtilPath, "abapprocess", k, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPProcUtilPath, extraLabels, now, int64(v)))
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectABAPProcessStatus()", "time", time.Since(now.AsTime()))
	return metrics, nil
}

// collectABAPQueueStats collects ABAP Queue utilization metrics using dpmon tool.
func collectABAPQueueStats(ctx context.Context, p *InstanceProperties, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	now := tspb.Now()
	// Since these metrics are derived from the same operation, even if one of the metric is skipped the whole group will be skipped from collection.
	skipABAPQueue := p.SkippedMetrics[nwABAPProcQueueCurrentPath] || p.SkippedMetrics[nwABAPProcQueuePeakPath]
	if skipABAPQueue {
		return nil, nil
	}
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	var (
		err               error
		currentQueueUsage map[string]int64
		peakQueueUsage    map[string]int64
	)
	currentQueueUsage, peakQueueUsage, err = sc.GetQueueStatistic(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Sapcontrol web method failed", "error", err)
		return nil, err
	}

	var metrics []*mrpb.TimeSeries
	for k, v := range currentQueueUsage {
		extraLabels := map[string]string{"abap_queue": k}
		log.CtxLogger(ctx).Debugw("Creating metric with labels",
			"metric", nwABAPProcQueueCurrentPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueueCurrentPath, extraLabels, now, int64(v)))
	}

	for k, v := range peakQueueUsage {
		extraLabels := map[string]string{"abap_queue_peak": k}
		log.CtxLogger(ctx).Debugw("Creating metric with labels",
			"metric", nwABAPProcQueueCurrentPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueuePeakPath, extraLabels, now, int64(v)))
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectABAPQueueStats()", "time", time.Since(now.AsTime()))
	return metrics, nil
}

// collectABAPSessionStats collects ABAP session related metrics using dpmon tool.
func collectABAPSessionStats(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) ([]*mrpb.TimeSeries, error) {
	now := tspb.Now()
	if _, ok := p.SkippedMetrics[nwABAPSessionsPath]; ok {
		return nil, nil
	}
	results := exec(ctx, params)
	log.CtxLogger(ctx).Debugw("DPMON for sessionStat output", "stdout", results.StdOut, "stderr", results.StdErr, "exitcode", results.ExitCode, "error", results.Error)
	if results.Error != nil {
		log.CtxLogger(ctx).Debugw("DPMON failed", log.Error(results.Error))
		return nil, results.Error
	}

	var metrics []*mrpb.TimeSeries
	sessionCounts, totalCount, err := parseABAPSessionStats(results.StdOut)
	if err != nil {
		log.CtxLogger(ctx).Debugw("DPMON ran successfully, but no ABAP session currently active", log.Error(err))
		return nil, err
	}

	for k, v := range sessionCounts {
		extraLabels := map[string]string{"abap_session_type": k}
		log.CtxLogger(ctx).Debugw("Creating metric with labels",
			"metric", nwABAPSessionsPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)

		metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(v)))
	}

	extraLabels := map[string]string{"abap_session_type": "total_count"}
	log.CtxLogger(ctx).Debugw("Creating metric with labels",
		"metric", nwABAPSessionsPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", totalCount)

	metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(totalCount)))

	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectABAPSessionStats()", "time", time.Since(now.AsTime()))
	return metrics, nil
}

// collectRFCConnections collects the ABAP RFC connection metrics using dpmon tool.
func collectRFCConnections(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params) ([]*mrpb.TimeSeries, error) {
	now := tspb.Now()
	if _, ok := p.SkippedMetrics[nwABAPRFCPath]; ok {
		return nil, nil
	}
	result := exec(ctx, params)
	log.CtxLogger(ctx).Debugw("DPMON for RFC output", "stdout", result.StdOut, "stderr", result.StdErr, "exitcode", result.ExitCode, "error", result.Error)
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("DPMON for RFC failed", log.Error(result.Error))
		return nil, result.Error
	}

	var metrics []*mrpb.TimeSeries
	rfcStateCount := parseRFCStats(result.StdOut)
	for k, v := range rfcStateCount {
		extraLabels := map[string]string{"abap_rfc_conn": k}
		log.CtxLogger(ctx).Debugw("Creating metric with labels",
			"metric", nwABAPRFCPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", v)
		metrics = append(metrics, createMetrics(p, nwABAPRFCPath, extraLabels, now, int64(v)))
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectRFCConnections()", "time", time.Since(now.AsTime()))
	return metrics, nil
}

// collectEnqLockMetrics builds Enq Locks for SAP Netweaver ASCS instances.
func collectEnqLockMetrics(ctx context.Context, p *InstanceProperties, exec commandlineexecutor.Execute, params commandlineexecutor.Params, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	if _, ok := p.SkippedMetrics[nwEnqLocksPath]; ok {
		return nil, nil
	}
	instance := p.SAPInstance.GetInstanceId()
	if !strings.HasPrefix(instance, "ASCS") && !strings.HasPrefix(instance, "ERS") {
		log.CtxLogger(ctx).Debugw("The Enq Lock metric is only applicable for application type: ASCS.", "InstanceID", p.SAPInstance.InstanceId)
		return nil, nil
	}
	now := tspb.Now()
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	var enqLocks []*sapcontrol.EnqLock
	var err error

	enqLocks, err = sc.EnqGetLockTable(ctx, scc)
	if err != nil {
		return nil, err
	}

	var metrics []*mrpb.TimeSeries
	for _, lock := range enqLocks {
		extraLabels := map[string]string{
			"lock_name":           lock.LockName,
			"lock_arg":            lock.LockArg,
			"lock_mode":           lock.LockMode,
			"owner":               lock.Owner,
			"owner_vb":            lock.OwnerVB,
			"user_count_owner":    fmt.Sprintf("%d", lock.UserCountOwner),
			"user_count_owner_vb": fmt.Sprintf("%d", lock.UserCountOwnerVB),
			"client":              lock.Client,
			"user":                lock.User,
			"transaction":         lock.Transaction,
			"object":              lock.Object,
			"backup":              lock.Backup,
		}
		log.CtxLogger(ctx).Debugw("Creating metric with labels",
			"metric", nwEnqLocksPath, "labels", extraLabels, "instancenumber", p.SAPInstance.GetInstanceNumber(), "value", lock.UserCountOwner)
		metrics = append(metrics, createMetrics(p, nwEnqLocksPath, extraLabels, now, lock.UserCountOwner))

	}
	return metrics, nil
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
