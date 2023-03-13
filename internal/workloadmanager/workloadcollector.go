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
	"os"
	"strconv"
	"strings"
	"time"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
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

/*
OSStatReader abstracts os.FileInfo reading. OSStatReader Example usage:

	OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
*/
type OSStatReader func(string) (os.FileInfo, error)

// DefaultTokenGetter obtains a "default" oauth2 token source within the getDefaultBearerToken function.
type DefaultTokenGetter func(context.Context, ...string) (oauth2.TokenSource, error)

// JSONCredentialsGetter obtains a JSON oauth2 google credentials within the getJSONBearerToken function.
type JSONCredentialsGetter func(context.Context, []byte, ...string) (*google.Credentials, error)

/*
WorkloadMetrics is a container for monitoring TimeSeries metrics.
*/
type WorkloadMetrics struct {
	Metrics []*monitoringresourcespb.TimeSeries
}

// metricEmitter is a container for constructing metrics from an override configuration file
type metricEmitter struct {
	scanner       *bufio.Scanner
	tmpMetricName string
}

/*
Parameters holds the parameters for all of the Collect* function calls.
*/
type Parameters struct {
	Config                *cnfpb.Configuration
	Remote                bool
	ConfigFileReader      ConfigFileReader
	OSStatReader          OSStatReader
	CommandRunner         commandlineexecutor.CommandRunner
	CommandRunnerNoSpace  commandlineexecutor.CommandRunnerNoSpace
	CommandExistsRunner   commandlineexecutor.CommandExistsRunner
	InstanceInfoReader    instanceinfo.Reader
	TimeSeriesCreator     cloudmonitoring.TimeSeriesCreator
	DefaultTokenGetter    DefaultTokenGetter
	JSONCredentialsGetter JSONCredentialsGetter
	OSType                string
	BackOffs              *cloudmonitoring.BackOffIntervals
	HeartbeatSpec         *heartbeat.Spec
}

var (
	now = currentTime
)

const metricOverridePath = "/etc/google-cloud-sap-agent/wlmmetricoverride.yaml"
const metricTypePrefix = "workload.googleapis.com/sap/validation/"

func currentTime() int64 {
	return time.Now().Unix()
}

func start(ctx context.Context, params Parameters, tsc cloudmonitoring.TimeSeriesCreator) {
	log.Logger.Info("Starting collection of Workload Manager metrics")
	st := time.Duration(params.Config.GetCollectionConfiguration().GetWorkloadValidationMetricsFrequency()) * time.Second
	if st <= 0 {
		// default back to 5 minutes
		st = time.Duration(5) * time.Minute
	}
	if params.Remote {
		// NOMUTANTS -- tested that when remote is set this is called, but cannot kill mutant
		for {
			log.Logger.Info("Collecting metrics from remote instances")
			collectAndSendRemoteMetrics(ctx, params)
			log.Logger.Debugw("Sleeping", "duration", st)
			time.Sleep(st)
		}
		return
	}

	projectID := params.Config.GetCloudProperties().GetProjectId()
	heartbeatTicker := params.HeartbeatSpec.CreateTicker()
	collectTicker := time.NewTicker(st)
	defer heartbeatTicker.Stop()
	defer collectTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Logger.Debug("cancellation requested")
			return
		case <-heartbeatTicker.C:
			params.HeartbeatSpec.Beat()
		case <-collectTicker.C:
			log.Logger.Info("collecting metrics from this instance")
			params.HeartbeatSpec.Beat()
			metrics := collectMetrics(ctx, params, metricOverridePath)
			sendMetrics(ctx, metrics, projectID, &tsc, params.BackOffs)
		}
	}
}

// StartMetricsCollection continuously collects Workload Manager metrics for SAP workloads.
// Returns true if the collection goroutine is started, and false otherwise.
func StartMetricsCollection(ctx context.Context, params Parameters) bool {
	if (params.Config.GetCollectionConfiguration() == nil || params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection() == nil) &&
		!params.Config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics() {
		log.Logger.Info("Not collecting Workload Manager metrics")
		return false
	}
	if params.OSType == "windows" {
		log.Logger.Info("Workload Manager metrics collection is not supported for windows platform.")
		return false
	}
	go start(ctx, params, params.TimeSeriesCreator)
	return true
}

func collectMetrics(ctx context.Context, params Parameters, metricOverride string) WorkloadMetrics {
	log.Logger.Info("Collecting Workload Manager metrics...")
	usagemetrics.Action(usagemetrics.CollectWLMMetrics) // Collecting WLM metrics
	if fileInfo, err := params.OSStatReader(metricOverride); fileInfo != nil && err == nil {
		log.Logger.Info("Using override metrics from yaml file")
		return collectOverrideMetrics(params.Config, params.ConfigFileReader, metricOverride)
	}
	// read the instance info
	params.InstanceInfoReader.Read(params.Config, instanceinfo.NetworkInterfaceAddressMap)

	sch := make(chan WorkloadMetrics)
	go CollectSystemMetrics(params, sch)
	cch := make(chan WorkloadMetrics)
	go CollectCorosyncMetrics(params, cch, csConfigPath)
	hch := make(chan WorkloadMetrics)
	go CollectHanaMetrics(params, hch)
	nch := make(chan WorkloadMetrics)
	go CollectNetWeaverMetrics(params, nch)
	pch := make(chan WorkloadMetrics)
	go CollectPacemakerMetrics(ctx, params, pch)

	sm := <-sch
	cm := <-cch
	hm := <-hch
	nm := <-nch
	pm := <-pch

	// Append the system metrics to all other metrics
	appendLabels(cm.Metrics[0].Metric.Labels, sm.Metrics[0].Metric.Labels)
	appendLabels(hm.Metrics[0].Metric.Labels, sm.Metrics[0].Metric.Labels)
	appendLabels(nm.Metrics[0].Metric.Labels, sm.Metrics[0].Metric.Labels)
	appendLabels(pm.Metrics[0].Metric.Labels, sm.Metrics[0].Metric.Labels)

	// concatenate all of the metrics together
	allMetrics := []*monitoringresourcespb.TimeSeries{}
	allMetrics = append(allMetrics, sm.Metrics...)
	allMetrics = append(allMetrics, cm.Metrics...)
	allMetrics = append(allMetrics, hm.Metrics...)
	allMetrics = append(allMetrics, nm.Metrics...)
	allMetrics = append(allMetrics, pm.Metrics...)

	return WorkloadMetrics{Metrics: allMetrics}
}

func appendLabels(dst map[string]string, src map[string]string) {
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
		if strings.HasPrefix(e.scanner.Text(), "#") {
			// ignore comments
			continue
		}
		key, value, found := strings.Cut(e.scanner.Text(), ":")
		if !found {
			if e.scanner.Text() != "" {
				log.Logger.Warnw("Could not parse key, value pair. Expected format: '<key>: <value>'", "text", e.scanner.Text())
			}
			continue
		}
		value = strings.TrimSpace(value)
		switch key := strings.TrimSpace(key); key {
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

func sendMetrics(ctx context.Context, wm WorkloadMetrics, p string, mc *cloudmonitoring.TimeSeriesCreator, bo *cloudmonitoring.BackOffIntervals) int {
	if wm.Metrics == nil || len(wm.Metrics) == 0 {
		log.Logger.Info("No metrics to send to Cloud Monitoring")
		return 0
	}
	// debugging metric data being sent
	for _, m := range wm.Metrics {
		if len(m.GetPoints()) == 0 {
			log.Logger.Debugw("  Metric has no point data", "metric", m.GetMetric().GetType())
			continue
		}
		log.Logger.Debugw("  Metric", "metric", m.GetMetric().GetType(), "value", m.GetPoints()[0].GetValue().GetDoubleValue())
		for k, v := range m.GetMetric().GetLabels() {
			log.Logger.Debugw("    Label", "key", k, "value", v)
		}
	}
	log.Logger.Infow("Sending metrics to Cloud Monitoring...", "number", len(wm.Metrics))
	request := monitoringpb.CreateTimeSeriesRequest{
		Name:       fmt.Sprintf("projects/%s", p),
		TimeSeries: wm.Metrics}
	if err := cloudmonitoring.CreateTimeSeriesWithRetry(ctx, *mc, &request, bo); err != nil {
		log.Logger.Errorw("Failed to send metrics to Cloud Monitoring", "error", err)
		usagemetrics.Error(usagemetrics.WLMMetricCollectionFailure) // Workload metrics collection failure
		return 0
	}
	log.Logger.Infow("Sent metrics to Cloud Monitoring.", "number", len(wm.Metrics))
	return len(wm.Metrics)
}

func createTimeSeries(t string, l map[string]string, v float64, c *cnfpb.Configuration) []*monitoringresourcespb.TimeSeries {
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
	return []*monitoringresourcespb.TimeSeries{timeseries.BuildFloat64(p)}
}
