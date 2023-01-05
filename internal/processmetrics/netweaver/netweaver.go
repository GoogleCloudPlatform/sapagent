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
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
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
)

var (
	msWorkProcess = regexp.MustCompile(`LB=([0-9]+)`)
)

// Collect is Netweaver implementation of Collector interface from processmetrics.go.
// Returns a list of NetWeaver related metrics.
func (p *InstanceProperties) Collect() []*sapdiscovery.Metrics {
	processListRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: p.SAPInstance.GetSapcontrolPath(),
		Args:       fmt.Sprintf("-nr %s -function GetProcessList -format script", p.SAPInstance.GetInstanceNumber()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics := collectNetWeaverMetrics(p, processListRunner)

	metrics = append(metrics, collectHTTPMetrics(p)...)

	abapProcessRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: p.SAPInstance.GetSapcontrolPath(),
		Args:       fmt.Sprintf("-nr %s -function ABAPGetWPTable", p.SAPInstance.GetInstanceNumber()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectABAPProcessStatus(p, abapProcessRunner)...)

	abapQueuesRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: p.SAPInstance.GetSapcontrolPath(),
		Args:       fmt.Sprintf("-nr %s -function GetQueueStatistic", p.SAPInstance.GetInstanceNumber()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectABAPQueueStats(p, abapQueuesRunner)...)

	// TODO(b/259208470): Explore using DB queries for DPMON instead of interactive command line tool.
	command := `-c 'echo q | dpmon pf=%s v'`
	abapSessionRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: "bash",
		Args:       fmt.Sprintf(command, p.SAPInstance.GetProfilePath()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}
	metrics = append(metrics, collectABAPSessionStats(p, abapSessionRunner)...)

	command = `-c 'echo q | dpmon pf=%s c'`
	abapRFCRunner := &commandlineexecutor.Runner{
		User:       p.SAPInstance.GetUser(),
		Executable: "bash",
		Args:       fmt.Sprintf(command, p.SAPInstance.GetProfilePath()),
		Env:        []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
	}

	metrics = append(metrics, collectRFCConnections(p, abapRFCRunner)...)

	return metrics
}

// collectNetWeaverMetrics builds a slice of SAP metrics containing all relevant NetWeaver metrics
func collectNetWeaverMetrics(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {

	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	procs, _, err := sc.ProcessList(r)
	if err != nil {
		log.Logger.Error("Error getting ProcessList", log.Error(err))
		return nil
	}

	metrics, availabilityValue := collectServiceMetrics(p, procs, now)
	metrics = append(metrics, createMetrics(p, nwAvailabilityPath, nil, now, availabilityValue))

	return metrics
}

// collectServiceMetrics collects NetWeaver "service" metrics describing Netweaver service
// processes as managed by the sapcontrol program.
func collectServiceMetrics(p *InstanceProperties, procs map[int]*sapcontrol.ProcessStatus, now *tspb.Timestamp) (metrics []*sapdiscovery.Metrics, availabilityValue int64) {
	start := tspb.Now()
	availabilityValue = systemAllProcessesGreen

	processNames := []string{"msg_server", "enserver", "enrepserver", "disp+work", "gwrd", "icman", "jstart", "jcontrol"}
	for _, proc := range procs {
		if contains(processNames, proc.Name) && !proc.IsGreen {
			availabilityValue = systemAtLeastOneProcessNotGreen
		}

		extraLabels := map[string]string{
			"service_name": proc.Name,
		}
		value := boolToInt64(proc.IsGreen)

		log.Logger.Debugf("Create %q metric for process %q on instance %q with value: %d.",
			nwServicePath, proc.Name, p.SAPInstance.GetInstanceId(), value)
		metrics = append(metrics, createMetrics(p, nwServicePath, extraLabels, now, value))
	}
	log.Logger.Debugf("Time taken to collect metrics in collectServiceMetrics(): %v.", time.Since(start.AsTime()))
	return metrics, availabilityValue
}

// collectHTTPMetrics collects the HTTP health check metrics for different types of
// Netweaver instances based on their types.
func collectHTTPMetrics(p *InstanceProperties) []*sapdiscovery.Metrics {
	url := p.SAPInstance.GetNetweaverHealthCheckUrl()
	if url == "" {
		return nil
	}
	log.Logger.Debugf("SAP Instance %q has HTTP health check URL: %q.", p.SAPInstance.GetInstanceId(), url)

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
func collectICMMetrics(p *InstanceProperties, url string) []*sapdiscovery.Metrics {
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.Logger.Debugf("HTTP GET failed for instance: %q URL: %q error: %v", p.SAPInstance.GetInstanceId(), url, err)
		return nil
	}
	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	log.Logger.Debugf("Time taken to collect metrics in collectICMMetrics(): %v.", time.Since(now.AsTime()))
	return []*sapdiscovery.Metrics{
		createMetrics(p, nwICMRCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwICMRTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}
}

// collectMessageServerMetrics uses HTTP GET on given URL to collect message server metrics.
// Returns
//   - Two metrics - HTTP response code and response time for all HTTP status codes.
//   - Additional work process count as reported by the message server info page on StatusOK(200).
//   - A nil in case of errors in HTTP GET request failures.
func collectMessageServerMetrics(p *InstanceProperties, url string) []*sapdiscovery.Metrics {
	now := tspb.Now()
	response, err := http.Get(url)
	timeTaken := time.Since(now.AsTime())
	if err != nil {
		log.Logger.Debugf("HTTP GET failed for instance: %q URL: %q error: %v.", p.SAPInstance.GetInstanceId(), url, err)
		return nil
	}
	defer response.Body.Close()

	extraLabels := map[string]string{"service_name": p.SAPInstance.GetServiceName()}

	metrics := []*sapdiscovery.Metrics{
		createMetrics(p, nwMSResponseCodePath, extraLabels, now, int64(response.StatusCode)),
		createMetrics(p, nwMSResponseTimePath, extraLabels, now, timeTaken.Milliseconds()),
	}

	if response.StatusCode != http.StatusOK {
		log.Logger.Debugf("HTTP GET failed with status code: %q.", response.StatusCode)
		log.Logger.Debugf("Time taken to collect metrics in collectMessageServerMetrics(): %v.", time.Since(now.AsTime()))
		return metrics
	}

	workProcessCount, err := parseWorkProcessCount(response.Body)
	if err != nil {
		log.Logger.Debugf("Reading work process count from message server info page failed: %v.", err)
		return metrics
	}
	log.Logger.Debugf("Time taken to collect metrics in collectMessageServerMetrics(): %v.", time.Since(now.AsTime()))

	return append(metrics, createMetrics(p, nwMSWorkProcessesPath, extraLabels, now, int64(workProcessCount)))
}

// parseWorkProcessCount processes the HTTP text/plain response body one line at a time
// to find the message server work process count.
// Returns the work process count on succcess, an error on failures.
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
func collectABAPProcessStatus(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {
	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	processCount, busyProcessCount, err := sc.ParseABAPGetWPTable(r)
	if err != nil {
		log.Logger.Debugf("Command sapcontrol failed with error: %v.", err)
		return nil
	}

	var metrics []*sapdiscovery.Metrics
	for k, v := range processCount {
		extraLabels := map[string]string{"abap_process": k}
		log.Logger.Debugf("Create %q metric for abap_process %q on instance %q with value: %d.",
			nwABAPProcCountPath, k, p.SAPInstance.GetInstanceNumber(), v)
		metrics = append(metrics, createMetrics(p, nwABAPProcCountPath, extraLabels, now, int64(v)))
	}

	for k, v := range busyProcessCount {
		extraLabels := map[string]string{"abap_process": k}
		log.Logger.Debugf("Create %q metric for abap_process %q on instance %q with value: %d.",
			nwABAPProcBusyPath, k, p.SAPInstance.GetInstanceNumber(), v)
		metrics = append(metrics, createMetrics(p, nwABAPProcBusyPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugf("Time taken to collect metrics in collectABAPProcessStatus(): %v.", time.Since(now.AsTime()))
	return metrics
}

// collectABAPQueueStats collects ABAP Queue utilization metrics using dpmon tool.
func collectABAPQueueStats(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {
	now := tspb.Now()
	sc := &sapcontrol.Properties{p.SAPInstance}
	currentQueueUsage, peakQueueUsage, err := sc.ParseQueueStats(r)
	if err != nil {
		log.Logger.Debugf("Command sapcontrol failed with error: %v.", err)
		return nil
	}

	var metrics []*sapdiscovery.Metrics
	for k, v := range currentQueueUsage {
		extraLabels := map[string]string{"abap_queue": k}
		log.Logger.Debugf("Create %q metric with labels %v on instance %q with value: %d.",
			nwABAPProcQueueCurrentPath, extraLabels, p.SAPInstance.GetInstanceNumber(), v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueueCurrentPath, extraLabels, now, int64(v)))
	}

	for k, v := range peakQueueUsage {
		extraLabels := map[string]string{"abap_queue_peak": k}
		log.Logger.Debugf("Create %q metric with labels %v on instance %q with value: %d.",
			nwABAPProcQueuePeakPath, extraLabels, p.SAPInstance.GetInstanceNumber(), v)

		metrics = append(metrics, createMetrics(p, nwABAPProcQueuePeakPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugf("Time taken to collect metrics in collectABAPQueueStats(): %v.", time.Since(now.AsTime()))
	return metrics
}

// collectABAPSessionStats collects ABAP session realted metrics using dpmon tool.
func collectABAPSessionStats(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {
	now := tspb.Now()

	stdOut, stdErr, code, err := r.RunWithEnv()
	log.Logger.Debugf("DPMON for sessionStats returned StdOut %q StdErr: %q, return code: %d, Error: %v.", stdOut, stdErr, code, err)
	if err != nil {
		log.Logger.Debug("DPMON failed", log.Error(err))
		return nil
	}

	var metrics []*sapdiscovery.Metrics
	sessionCounts, totalCount, err := parseABAPSessionStats(stdOut)
	if err != nil {
		log.Logger.Debug("DPMON ran successfully, but no ABAP session currently active", log.Error(err))
		return nil
	}

	for k, v := range sessionCounts {
		extraLabels := map[string]string{"abap_session_type": k}
		log.Logger.Debugf("Create %q metric with labels %v on instance %q with value: %d.",
			nwABAPSessionsPath, extraLabels, p.SAPInstance.GetInstanceNumber(), v)

		metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(v)))
	}

	extraLabels := map[string]string{"abap_session_type": "total_count"}
	log.Logger.Debugf("Create %q metric with labels %v on instance %q with value: %d.",
		nwABAPSessionsPath, extraLabels, p.SAPInstance.GetInstanceNumber(), totalCount)

	metrics = append(metrics, createMetrics(p, nwABAPSessionsPath, extraLabels, now, int64(totalCount)))

	log.Logger.Debugf("Time taken to collect metrics in collectABAPSessionStats(): %v.", time.Since(now.AsTime()))
	return metrics
}

// collectRFCConnections collects the ABAP RFC connection metrics using dpmon tool.
func collectRFCConnections(p *InstanceProperties, r sapcontrol.RunnerWithEnv) []*sapdiscovery.Metrics {
	now := tspb.Now()

	stdOut, stdErr, code, err := r.RunWithEnv()
	log.Logger.Debug(fmt.Sprintf("DPMON for RFC returned StdOut %q StdErr: %q, return code: %d", stdOut, stdErr, code), log.Error(err))
	if err != nil {
		log.Logger.Debug("DPMON for RFC failed", log.Error(err))
		return nil
	}

	var metrics []*sapdiscovery.Metrics
	rfcStateCount := parseRFCStats(stdOut)
	for k, v := range rfcStateCount {
		extraLabels := map[string]string{"abap_rfc_conn": k}
		log.Logger.Debugf("Create %q metric with labels %v on instance %q with value: %d.",
			nwABAPRFCPath, extraLabels, p.SAPInstance.GetInstanceNumber(), v)
		metrics = append(metrics, createMetrics(p, nwABAPRFCPath, extraLabels, now, int64(v)))
	}
	log.Logger.Debugf("Time taken to collect metrics in collectRFCConnections(): %v.", time.Since(now.AsTime()))
	return metrics
}

// createMetrics - create sapdiscovery.Metrics object for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *sapdiscovery.Metrics {
	params := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: metricLabels(p, extraLabels),
		Timestamp:    now,
		Int64Value:   val,
		BareMetal:    p.Config.BareMetal,
	}
	return &sapdiscovery.Metrics{TimeSeries: timeseries.BuildInt(params)}
}

// metricLabels appends the default SAP Instance labels and extra labels
// to return a consilidated map of metric labels.
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
