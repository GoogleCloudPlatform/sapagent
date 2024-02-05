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

// Package workloadmanager collects workload manager metrics and sends them to Cloud Monitoring
package workloadmanager

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"

	workloadmanager "google.golang.org/api/workloadmanager/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/recovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

/*
ConfigFileReader abstracts loading and reading files into an io.ReadCloser object. ConfigFileReader Example usage:

	ConfigFileReader(func(path string) (io.ReadCloser, error) {
			file, err := os.Open(path)
			var f io.ReadCloser = file
			return f, err
		})
*/
type ConfigFileReader func(string) (io.ReadCloser, error)

// WorkloadMetrics is a container for monitoring TimeSeries metrics.
type WorkloadMetrics struct {
	Metrics []*mrpb.TimeSeries
}

// metricEmitter is a container for constructing metrics from an override configuration file
type metricEmitter struct {
	scanner       *bufio.Scanner
	tmpMetricName string
}

type wlmInterface interface {
	WriteInsight(project, location string, writeInsightRequest *workloadmanager.WriteInsightRequest) error
}

// sendMetricsParams defines the set of parameters required to call sendMetrics
type sendMetricsParams struct {
	wm                    WorkloadMetrics
	cp                    *ipb.CloudProperties
	bareMetal             bool
	sendToCloudMonitoring bool
	timeSeriesCreator     cloudmonitoring.TimeSeriesCreator
	backOffIntervals      *cloudmonitoring.BackOffIntervals
	wlmService            wlmInterface
}

var (
	now               = currentTime
	dailyUsageRoutine *recovery.RecoverableRoutine
	collectRoutine    *recovery.RecoverableRoutine
)

const metricOverridePath = "/etc/google-cloud-sap-agent/wlmmetricoverride.yaml"
const metricTypePrefix = "workload.googleapis.com/sap/validation/"

func currentTime() int64 {
	return time.Now().Unix()
}

func start(ctx context.Context, a any) {
	var params Parameters
	if v, ok := a.(Parameters); ok {
		params = v
	} else {
		log.CtxLogger(ctx).Info("Cannot collect Workload Manager metrics, no collection configuration detected")
		return
	}
	log.CtxLogger(ctx).Infow("Starting collection of Workload Manager metrics", "definitionVersion", params.WorkloadConfig.GetVersion())
	cmf := time.Duration(params.Config.GetCollectionConfiguration().GetWorkloadValidationMetricsFrequency()) * time.Second
	if cmf <= 0 {
		// default it to 5 minutes
		cmf = time.Duration(5) * time.Minute
	}
	configurableMetricsTicker := time.NewTicker(cmf)
	defer configurableMetricsTicker.Stop()

	dbmf := time.Duration(params.Config.GetCollectionConfiguration().GetWorkloadValidationDbMetricsFrequency()) * time.Second
	if dbmf <= 0 {
		// default it to 1 hour
		dbmf = time.Duration(3600) * time.Second
	}
	databaseMetricTicker := time.NewTicker(dbmf)
	defer databaseMetricTicker.Stop()

	heartbeatTicker := params.HeartbeatSpec.CreateTicker()
	defer heartbeatTicker.Stop()

	// Do not wait for the first tick and start metric collection immediately.
	select {
	case <-ctx.Done():
		log.CtxLogger(ctx).Debug("Workload metrics cancellation requested")
		return
	default:
		collectWorkloadMetricsOnce(ctx, params)
		collectDBMetricsOnce(ctx, params)
	}

	for {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debug("Workload metrics cancellation requested")
			return
		case cd := <-params.WorkloadConfigCh:
			params.WorkloadConfig = cd.GetWorkloadValidation()
			log.CtxLogger(ctx).Info("Received updated workload collection configuration", "version", params.WorkloadConfig.GetVersion())
		case <-heartbeatTicker.C:
			params.HeartbeatSpec.Beat()
		case <-configurableMetricsTicker.C:
			collectWorkloadMetricsOnce(ctx, params)
		case <-databaseMetricTicker.C:
			collectDBMetricsOnce(ctx, params)
		}
	}
}

// collectWorkloadMetricsOnce issues a heartbeat and initiates one round of metric collection.
func collectWorkloadMetricsOnce(ctx context.Context, params Parameters) {
	params.HeartbeatSpec.Beat()
	if params.Remote {
		log.CtxLogger(ctx).Info("Collecting metrics from remote instances")
		collectAndSendRemoteMetrics(ctx, params)
		return
	}
	log.CtxLogger(ctx).Info("Collecting metrics from this instance")
	metrics := collectMetricsFromConfig(ctx, params, metricOverridePath)
	sendMetrics(ctx, sendMetricsParams{
		wm:                    metrics,
		cp:                    params.Config.GetCloudProperties(),
		bareMetal:             params.Config.GetBareMetal(),
		sendToCloudMonitoring: params.Config.GetSupportConfiguration().GetSendWorkloadValidationMetricsToCloudMonitoring().GetValue(),
		timeSeriesCreator:     params.TimeSeriesCreator,
		backOffIntervals:      params.BackOffs,
		wlmService:            params.WLMService,
	})
}

// StartMetricsCollection continuously collects Workload Manager metrics for SAP workloads.
// Returns true if the collection goroutine is started, and false otherwise.
func StartMetricsCollection(ctx context.Context, params Parameters) bool {
	if params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection() == nil &&
		!params.Config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics().GetValue() {
		log.CtxLogger(ctx).Info("Not collecting Workload Manager metrics")
		return false
	}
	if params.OSType == "windows" {
		log.CtxLogger(ctx).Info("Workload Manager metrics collection is not supported for windows platform.")
		return false
	}
	if params.WorkloadConfig == nil {
		log.CtxLogger(ctx).Info("Cannot collect Workload Manager metrics, no collection configuration detected")
		return false
	}
	dailyUsageRoutine = &recovery.RecoverableRoutine{
		Routine: func(_ context.Context, a any) {
			if v, ok := a.(int); ok {
				usagemetrics.LogActionDaily(v)
			}
		},
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		ExpectedMinDuration: 24 * time.Hour,
	}
	if params.Remote {
		dailyUsageRoutine.RoutineArg = usagemetrics.RemoteWLMMetricsCollection
	} else {
		dailyUsageRoutine.RoutineArg = usagemetrics.CollectWLMMetrics
	}
	dailyUsageRoutine.StartRoutine(ctx)
	collectRoutine = &recovery.RecoverableRoutine{
		Routine:             start,
		RoutineArg:          params,
		ErrorCode:           usagemetrics.WLMCollectionRoutineFailure,
		ExpectedMinDuration: 10 * time.Second,
	}
	collectRoutine.StartRoutine(ctx)
	return true
}

// collectMetricsFromConfig returns the result of metric collection using the
// collection definition configuration supplied to the agent.
//
// The results of this function can be overridden using a metricOverride file.
func collectMetricsFromConfig(ctx context.Context, params Parameters, metricOverride string) WorkloadMetrics {
	log.CtxLogger(ctx).Info("Collecting Workload Manager metrics...")
	if fileInfo, err := params.OSStatReader(metricOverride); fileInfo != nil && err == nil {
		log.CtxLogger(ctx).Info("Using override metrics from yaml file")
		return collectOverrideMetrics(params.Config, params.ConfigFileReader, metricOverride)
	}

	// Read the latest instance info for this system.
	params.InstanceInfoReader.Read(ctx, params.Config, instanceinfo.NetworkInterfaceAddressMap)

	// Collect all metrics specified by the WLM Validation config.
	var system, corosync, hana, netweaver, pacemaker, custom WorkloadMetrics
	var wg sync.WaitGroup
	wg.Add(5)
	systemMetricsRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			defer wg.Done()
			system = CollectSystemMetricsFromConfig(ctx, params)
		},
		ErrorCode:           usagemetrics.WLMCollectionSystemRoutineFailure,
		ExpectedMinDuration: 5 * time.Second,
	}
	systemMetricsRoutine.StartRoutine(ctx)
	hanaMetricsRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			defer wg.Done()
			hana = CollectHANAMetricsFromConfig(ctx, params)
		},
		ErrorCode:           usagemetrics.WLMCollectionHANARoutineFailure,
		ExpectedMinDuration: 5 * time.Second,
	}
	hanaMetricsRoutine.StartRoutine(ctx)
	netweaverMetricsRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			defer wg.Done()
			netweaver = CollectNetWeaverMetricsFromConfig(ctx, params)
		},
		ErrorCode:           usagemetrics.WLMCollectionNetweaverRoutineFailure,
		ExpectedMinDuration: 5 * time.Second,
	}
	netweaverMetricsRoutine.StartRoutine(ctx)
	pacemakerMetricsRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			defer wg.Done()
			pacemaker = CollectPacemakerMetricsFromConfig(ctx, params)
			v := 0.0
			if len(pacemaker.Metrics) > 0 && len(pacemaker.Metrics[0].Points) > 0 {
				v = pacemaker.Metrics[0].Points[0].GetValue().GetDoubleValue()
			}
			corosync = CollectCorosyncMetricsFromConfig(ctx, params, v)
		},
		ErrorCode:           usagemetrics.WLMCollectionPacemakerRoutineFailure,
		ExpectedMinDuration: 5 * time.Second,
	}
	pacemakerMetricsRoutine.StartRoutine(ctx)
	customMetricsRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			defer wg.Done()
			custom = CollectCustomMetricsFromConfig(ctx, params)
		},
		ErrorCode:           usagemetrics.WLMCollectionCustomRoutineFailure,
		ExpectedMinDuration: 5 * time.Second,
	}
	customMetricsRoutine.StartRoutine(ctx)
	wg.Wait()

	// Append the system metrics to all other metrics.
	systemLabels := system.Metrics[0].Metric.Labels
	appendLabels(corosync.Metrics[0].Metric.Labels, systemLabels)
	appendLabels(hana.Metrics[0].Metric.Labels, systemLabels)
	appendLabels(netweaver.Metrics[0].Metric.Labels, systemLabels)
	appendLabels(pacemaker.Metrics[0].Metric.Labels, systemLabels)
	appendLabels(custom.Metrics[0].Metric.Labels, systemLabels)

	// Concatenate all of the metrics together.
	allMetrics := []*mrpb.TimeSeries{}
	allMetrics = append(allMetrics, system.Metrics...)
	allMetrics = append(allMetrics, corosync.Metrics...)
	allMetrics = append(allMetrics, hana.Metrics...)
	allMetrics = append(allMetrics, netweaver.Metrics...)
	allMetrics = append(allMetrics, pacemaker.Metrics...)
	allMetrics = append(allMetrics, custom.Metrics...)

	return WorkloadMetrics{Metrics: allMetrics}
}

func appendLabels(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

/*
Utilize an override configuration file to collect metrics for testing purposes. This allows
sending WLM metrics without creating specific SAP setups. The override file will contain the metric
type followed by all metric labels associated with that type. Example override file contents:

metric: system

	  metric_value: 1
		os: rhel-8.4
		agent_version: 2.6
		gcloud: true

metric: hana

	metric_value: 0
	disk_log_mount: /hana/log
*/
func collectOverrideMetrics(config *cnfpb.Configuration, reader ConfigFileReader, metricOverride string) WorkloadMetrics {
	file, err := reader(metricOverride)
	if err != nil {
		log.Logger.Warnw("Could not read the metric override file", "error", err)
		return WorkloadMetrics{}
	}
	defer file.Close()

	wm := WorkloadMetrics{}
	scanner := bufio.NewScanner(file)
	metricEmitter := metricEmitter{scanner, ""}
	for {
		metricName, metricValue, labels, last := metricEmitter.getMetric()
		if metricName != "" {
			wm.Metrics = append(wm.Metrics, createTimeSeries(metricTypePrefix+metricName, labels, metricValue, config)...)
		}
		if last {
			break
		}
	}
	return wm
}

// Reads all metric labels for a type. Returns false if there is more content in the scanner, true otherwise.
func (e *metricEmitter) getMetric() (string, float64, map[string]string, bool) {
	metricName := e.tmpMetricName
	metricValue := 0.0
	labels := make(map[string]string)
	var err error
	for e.scanner.Scan() {
		key, value, found := parseScannedText(e.scanner.Text())
		if !found {
			continue
		}
		switch key {
		case "metric":
			if metricName != "" {
				e.tmpMetricName = value
				return metricName, metricValue, labels, false
			}
			metricName = value
		case "metric_value":
			if metricValue, err = strconv.ParseFloat(value, 64); err != nil {
				log.Logger.Warnw("Failed to parse float", "value", value, "error", err)
			}
		default:
			labels[key] = strings.TrimSpace(value)
		}
	}
	if err = e.scanner.Err(); err != nil {
		log.Logger.Warnw("Could not read from the override metrics file", "error", err)
	}
	return metricName, metricValue, labels, true
}

// parseScannedText extracts a key and value pair from a scanned line of text.
//
// The expected format for the text string is: '<key>: <value>'.
func parseScannedText(text string) (key, value string, found bool) {
	// Ignore empty lines and comments.
	if text == "" || strings.HasPrefix(text, "#") {
		return "", "", false
	}
	key, value, found = strings.Cut(text, ":")
	if !found {
		log.Logger.Warnw("Could not parse key, value pair. Expected format: '<key>: <value>'", "text", text)
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), found
}

// sendMetrics performs the request(s) to write collected metric data.
//
// There are two workflows for sending metric data, determined by the
// send_to_cloud_monitoring configuration flag. The default workflow involves
// sending a write request to Data Warehouse. The legacy workflow involves
// storing metrics as time series data in Cloud Monitoring, with a secondary
// write to Data Warehouse that is not taken into consideration for the
// overall success or failure of the function.
func sendMetrics(ctx context.Context, params sendMetricsParams) int {
	if len(params.wm.Metrics) == 0 {
		log.CtxLogger(ctx).Info("No Workload Manager metrics to send.")
		return 0
	}

	logMetricData(ctx, params.wm.Metrics)

	if params.sendToCloudMonitoring {
		sentMetrics := len(params.wm.Metrics)
		log.CtxLogger(ctx).Infow("Sending metrics to Cloud Monitoring...", "number", len(params.wm.Metrics))
		request := mpb.CreateTimeSeriesRequest{
			Name:       fmt.Sprintf("projects/%s", params.cp.GetProjectId()),
			TimeSeries: params.wm.Metrics,
		}
		if err := cloudmonitoring.CreateTimeSeriesWithRetry(ctx, params.timeSeriesCreator, &request, params.backOffIntervals); err != nil {
			log.CtxLogger(ctx).Warnw("Failed to send metrics to Cloud Monitoring", "error", err)
			usagemetrics.Error(usagemetrics.WLMMetricCollectionFailure)
			sentMetrics = 0
		} else {
			log.CtxLogger(ctx).Infow("Sent metrics to Cloud Monitoring.", "number", len(params.wm.Metrics))
		}

		sendMetricsToDataWarehouseSecondary(ctx, params)

		return sentMetrics
	}

	return sendMetricsToDataWarehouse(ctx, params)
}

// sendMetricsToDataWarehouse initiates the Data Warehouse write insight request.
func sendMetricsToDataWarehouse(ctx context.Context, params sendMetricsParams) int {
	sentMetrics := len(params.wm.Metrics)
	log.CtxLogger(ctx).Infow("Sending metrics to Data Warehouse...", "number", len(params.wm.Metrics))
	// Send request to regional endpoint. Ex: "us-central1-a" -> "us-central1"
	location := params.cp.GetZone()
	if params.bareMetal {
		location = params.cp.GetRegion()
	}
	locationParts := strings.Split(location, "-")
	location = strings.Join([]string{locationParts[0], locationParts[1]}, "-")
	req := createWriteInsightRequest(params.wm, params.cp)
	if err := params.wlmService.WriteInsight(params.cp.GetProjectId(), location, req); err != nil {
		log.CtxLogger(ctx).Warnw("Failed to send metrics to Data Warehouse", "error", err)
		usagemetrics.Error(usagemetrics.WLMMetricCollectionFailure)
		sentMetrics = 0
	} else {
		log.CtxLogger(ctx).Infow("Sent metrics to Data Warehouse.", "number", len(params.wm.Metrics))
	}
	return sentMetrics
}

// sendMetricsToDataWarehouseSecondary initiates the Data Warehouse write
// insight request. However, the log level has been lowered to DEBUG, and there
// is no usagemetrics logging if the request fails.
func sendMetricsToDataWarehouseSecondary(ctx context.Context, params sendMetricsParams) int {
	sentMetrics := len(params.wm.Metrics)
	log.CtxLogger(ctx).Debugw("Sending metrics to Data Warehouse...", "number", len(params.wm.Metrics))
	// Send request to regional endpoint. Ex: "us-central1-a" -> "us-central1"
	location := params.cp.GetZone()
	if params.bareMetal {
		location = params.cp.GetRegion()
	}
	locationParts := strings.Split(location, "-")
	location = strings.Join([]string{locationParts[0], locationParts[1]}, "-")
	req := createWriteInsightRequest(params.wm, params.cp)
	if err := params.wlmService.WriteInsight(params.cp.GetProjectId(), location, req); err != nil {
		log.CtxLogger(ctx).Debugw("Failed to send metrics to Data Warehouse", "error", err)
		sentMetrics = 0
	} else {
		log.CtxLogger(ctx).Debugw("Sent metrics to Data Warehouse.", "number", len(params.wm.Metrics))
	}
	return sentMetrics
}

func createTimeSeries(t string, l map[string]string, v float64, c *cnfpb.Configuration) []*mrpb.TimeSeries {
	now := &timestamppb.Timestamp{
		Seconds: now(),
	}

	p := timeseries.Params{
		BareMetal:    c.BareMetal,
		CloudProp:    c.CloudProperties,
		MetricType:   t,
		MetricLabels: l,
		Timestamp:    now,
		Float64Value: v,
	}
	return []*mrpb.TimeSeries{timeseries.BuildFloat64(p)}
}

// createWriteInsightRequest converts a WorkloadMetrics time series into a WriteInsightRequest.
func createWriteInsightRequest(wm WorkloadMetrics, cp *ipb.CloudProperties) *workloadmanager.WriteInsightRequest {
	validations := []*workloadmanager.SapValidationValidationDetail{}
	for _, m := range wm.Metrics {
		t := "SAP_VALIDATION_TYPE_UNSPECIFIED"
		switch m.GetMetric().GetType() {
		case sapValidationSystem:
			t = "SYSTEM"
		case sapValidationCorosync:
			t = "COROSYNC"
		case sapValidationHANA:
			t = "HANA"
		case sapValidationNetweaver:
			t = "NETWEAVER"
		case sapValidationPacemaker:
			t = "PACEMAKER"
		case sapValidationHANASecurity:
			t = "HANA_SECURITY"
		case sapValidationCustom:
			t = "CUSTOM"
		}
		v := false
		if len(m.GetPoints()) > 0 {
			v = m.GetPoints()[0].GetValue().GetDoubleValue() > 0
		}
		validations = append(validations, &workloadmanager.SapValidationValidationDetail{
			SapValidationType: t,
			IsPresent:         v,
			Details:           m.GetMetric().GetLabels(),
		})
	}

	return &workloadmanager.WriteInsightRequest{
		Insight: &workloadmanager.Insight{
			InstanceId: cp.GetInstanceId(),
			SapValidation: &workloadmanager.SapValidation{
				ProjectId:         cp.GetProjectId(),
				Zone:              cp.GetZone(),
				ValidationDetails: validations,
			},
		},
	}
}

// logMetricData prints collected metric information to the agent log file.
func logMetricData(ctx context.Context, metrics []*mrpb.TimeSeries) {
	for _, m := range metrics {
		if len(m.GetPoints()) == 0 {
			log.CtxLogger(ctx).Debugw("Metric has no point data", "metric", m.GetMetric().GetType())
			continue
		}
		log.CtxLogger(ctx).Debugw("Metric", "metric", m.GetMetric().GetType(), "value", m.GetPoints()[0].GetValue().GetDoubleValue())
		for k, v := range m.GetMetric().GetLabels() {
			log.CtxLogger(ctx).Debugw("  Label", "key", k, "value", v)
		}
	}
}
