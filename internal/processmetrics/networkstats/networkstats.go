/*
Copyright 2023 Google LLC

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

// Package networkstats is responsible for collection of TCP network stats
// under /sap/networkstats/
package networkstats

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Properties   struct contains the parameters necessary for networkstats package common methods.
type Properties struct {
	Executor        commandlineexecutor.Execute
	Config          *cnfpb.Configuration
	Client          cloudmonitoring.TimeSeriesCreator
	CommandParams   commandlineexecutor.Params
	PMBackoffPolicy backoff.BackOffContext
	SkippedMetrics  map[string]bool
}

var (
	pidRe    = regexp.MustCompile(`\d+`)
	metricRe = regexp.MustCompile(`(\w*:[a-zA-Z\d,\/.]*)|(\w+\s\d+[a-zA-Z.\d]*)`)
)

const (
	metricURL   = "workload.googleapis.com"
	nwStatsPath = "/sap/networkstats"
)

// Collect is an implementation of Collector interface defined in processmetrics.go.
// Collect method collects network metrics, logs errors if it encounters
// any and returns the collected metrics with the last error encountered while collecting metrics.
func (p *Properties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	if p.SkippedMetrics[nwStatsPath] {
		log.CtxLogger(ctx).Infow("Skipped collection of networkstats metrics")
		return nil, nil
	}
	log.CtxLogger(ctx).Info("Collecting networkstats metrics")
	cmd := p.CommandParams.Executable
	argsToSplit := p.CommandParams.ArgsToSplit
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  cmd,
		ArgsToSplit: argsToSplit,
	})

	if strings.Contains(result.StdErr, "command not found") {
		return nil, fmt.Errorf("could not execute command: %s", result.StdErr)
	}
	pid, metricList := parseSSOutput(ctx, result.StdOut)

	if len(metricList) == 0 {
		return nil, fmt.Errorf("could not fetch TCP connection Metrics")
	}

	ssValues := mapValues(metricList)

	reqMetrics := []string{"rtt"}
	return p.createTSList(ctx, pid, reqMetrics, ssValues)
}

// mapValues creates a map of values from given metric list.
// Sample input:
// metricList: ["wscale:7,7", "rto:204", "rtt:0.017/0.008", "send 154202352941bps", "lastsnd:28", "lastrcv:28", "lastack:28", "pacing_rate 306153576640bps"]
// Sample output:
// ssMap: map["wscale": "7", "rto": "204", "rtt": "0.017/0.008", "send": "154202352941bps", "lastsnd": "28", "lastrcv": "28", "lastack": "28", "pacing_rate": "306153576640bps"]
func mapValues(metrics []string) map[string]string {
	ssValues := make(map[string]string)

	for _, metric := range metrics {
		k, v, ok := strings.Cut(metric, ":")
		if !ok {
			k, v, ok = strings.Cut(metric, " ")
			if !ok {
				log.Logger.Debugw("Could not find a whitespace or colon separator in ss metrics received for metric:", metric)
				continue
			}
		}
		ssValues[k] = v
	}
	return ssValues
}

// createTSList creates a slice of timeseries metrics according to the required metric values
// It returns this slice along with an error which could possibly be non-nil.
// Some error could occur in collection of one individual metric.
func (p *Properties) createTSList(ctx context.Context, pid string, reqMetrics []string, ssMap map[string]string) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	var metricsCollectionErr error

	for _, metric := range reqMetrics {
		val, err := strconv.ParseFloat(strings.Split(ssMap[metric], "/")[0], 64)
		if err != nil {
			metricsCollectionErr = err
			log.CtxLogger(ctx).Infow("could not convert string to float for metric ", metric, "err: ", err)
			continue
		}

		ssMetrics := p.collectTCPMetrics(ctx, metric, pid, val)
		if ssMetrics != nil {
			metrics = append(metrics, ssMetrics...)
		}
	}

	return metrics, metricsCollectionErr
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *Properties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	attempt := 1
	var res []*mrpb.TimeSeries
	err := backoff.Retry(func() error {
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Infow("Retry limit exceeded", "error", err)
	}
	return res, err
}

// collectTCPMetrics collects TCP connection metrics.
func (p *Properties) collectTCPMetrics(ctx context.Context, metric, pid string, rtt float64) []*mrpb.TimeSeries {
	labels := map[string]string{
		"name":    metric,
		"process": "hdbnameserver",
		"pid":     pid,
	}

	return []*mrpb.TimeSeries{p.createMetric(labels, rtt)}
}

// createMetric creates a TimeSeries metric with given labels and values.
func (p *Properties) createMetric(labels map[string]string, val float64) *mrpb.TimeSeries {
	ts := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + nwStatsPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		Float64Value: val,
		BareMetal:    p.Config.BareMetal,
	}

	log.Logger.Debug("Creating metric for instance", "metric", nwStatsPath, "value", val, "labels", labels)
	return timeseries.BuildFloat64(ts)
}

// parseSSOutput parses given SSOutput for PID value of concerned processes and a list of TCP connection metrics.
// Example ssOutput:
// 20210
// State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port
// ESTAB    0         0                127.0.0.1:30013          127.0.0.1:55494
// \t cubic wscale:7,7 rto:204 rtt:0.017/0.008 send 154202352941bps lastsnd:28 lastrcv:28 lastack:28 pacing_rate 306153576640bps delivered:3 app_limited rcv_space:65483 minrtt:0.015
// \n
//
// This function returns PID and a string slice containing these metrics.
// PID: 20210
// metricList: ["wscale:7,7", "rto:204", "rtt:0.017/0.008", "send 154202352941bps", "lastsnd:28", "lastrcv:28", "lastack:28", "pacing_rate 306153576640bps", "delivered:3", "rcv_space:65483", "minrtt:0.015"]
func parseSSOutput(ctx context.Context, ssOutput string) (string, []string) {
	var metrics []string
	out := strings.Split(ssOutput, "\n")

	// Checking if process ID has been received or not
	if m := pidRe.FindAllString(out[0], -1); len(m) == 0 || m[0] != out[0] {
		log.CtxLogger(ctx).Debug("Process ID not found")
		return "", metrics
	}
	// Checking if TCP connection metrics has been found or not
	if len(out) <= 4 {
		log.CtxLogger(ctx).Debug("TCP connection metrics not received")
		return "", metrics
	}

	pid := out[0]
	metricString := out[3]

	// TCP SS metrics received using ss command presents the metrics in two ways:
	// 	rtt:0.027/0.23, OR
	// 	pacing_rate 306153576640bps
	// metricRe matches for substrings following either of these display patterns
	// A string slice of these metrics gets generated: ["rtt:0.026,0.23", "pacing_rate 306153576640bps"]
	metrics = metricRe.FindAllString(metricString, -1)
	log.CtxLogger(ctx).Debug("Metric List: ", metrics)

	return pid, metrics
}
