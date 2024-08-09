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
	"path"
	"regexp"
	"strconv"
	"strings"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Properties struct contains the parameters necessary for networkstats package common methods.
type (
	Properties struct {
		Executor        commandlineexecutor.Execute
		Config          *cnfpb.Configuration
		Client          cloudmonitoring.TimeSeriesCreator
		PMBackoffPolicy backoff.BackOffContext
		SkippedMetrics  map[string]bool
	}

	metricVal struct {
		val  any
		Type string
	}
)

var (
	metricRe             = regexp.MustCompile(`(\w*:[a-zA-Z\d,\/.]*)|(\w+\s\d+[a-zA-Z.\d]*)`)
	requiredFloatMetrics = []string{"rtt", "rcv_rtt"}
	requiredIntMetrics   = []string{"rto", "bytes_acked", "bytes_received", "lastsnd", "lastrcv"}
)

const (
	metricURL   = "workload.googleapis.com"
	nwStatsPath = "/sap/networkstats"
)

/*
Collect is an implementation of Collector interface defined in processmetrics.go.
Collect method collects network metrics, logs errors if it encounters
any and returns the collected metrics with the last error encountered while collecting metrics.
*/
func (p *Properties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var floatMetrics, intMetrics []string
	for _, metric := range requiredFloatMetrics {
		if p.SkippedMetrics[path.Join(nwStatsPath, metric)] {
			log.CtxLogger(ctx).Debug("Skipping collection of networkstats metric:", metric)
			continue
		}
		floatMetrics = append(floatMetrics, metric)
	}
	for _, metric := range requiredIntMetrics {
		if p.SkippedMetrics[path.Join(nwStatsPath, metric)] {
			log.CtxLogger(ctx).Debug("Skipping collection of networkstats metric:", metric)
			continue
		}
		intMetrics = append(intMetrics, metric)
	}
	if len(floatMetrics) == 0 && len(intMetrics) == 0 {
		log.CtxLogger(ctx).Debug("Skipping collection of all networkstats metrics")
		return nil, nil
	}

	log.CtxLogger(ctx).Debug("Collecting networkstats metrics")
	pid := p.fetchPID(ctx)
	if pid == "" {
		return nil, fmt.Errorf("could not fetch pid of hdbnameserver")
	}
	socket, err := p.fetchHDBSocket(ctx)
	if err != nil {
		return nil, err
	}
	socket = strings.ReplaceAll(socket, "0.0.0.0", "*")
	out := p.fetchSSOutput(ctx, socket)
	log.CtxLogger(ctx).Debug("Socket: ", socket, "Output: ", out, "pid: ", pid)

	metricList := parseSSOutput(ctx, out)

	if len(metricList) == 0 {
		return nil, fmt.Errorf("could not fetch TCP connection Metrics")
	}

	ssValues := mapValues(metricList)

	floats, err := p.createTSList(ctx, pid, floatMetrics, ssValues, "float64")
	if err != nil {
		return nil, err
	}

	ints, err := p.createTSList(ctx, pid, intMetrics, ssValues, "int64")
	if err != nil {
		return nil, err
	}

	return append(floats, ints...), nil
}

// fetchPID fetches the pid of hdbnameserver.
func (p *Properties) fetchPID(ctx context.Context) string {
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "pidof",
		ArgsToSplit: "hdbnameserver",
	})
	return result.StdOut
}

// fetchHDBSocket fetches the listening socket opened by hdbnameserver.
func (p *Properties) fetchHDBSocket(ctx context.Context) (string, error) {
	// This is a single line command to fetch LISTENING ports used by hdbnameserver
	// using lsof command
	// Note: Backticks in grep -Eo `<regularExp>` get replaced by single quotes
	// as explained in commandlineexecutor.go
	argsToSplit := "-c 'sudo lsof -nP -p $(pidof hdbnameserver) | grep LISTEN | grep -v 127.0.0.1 | grep -Eo `(([0-9]{1,3}\\.){1,3}[0-9]{1,3})|(\\*)\\:[0-9]{3,5}`'"
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: argsToSplit,
	})

	if !strings.Contains(result.StdErr, "command not found") {
		log.CtxLogger(ctx).Debug("Fetched listening sockets for hdbnameserver using lsof command")
		return strings.Split(result.StdOut, "\n")[0], nil
	}

	// This is a single line command to fetch LISTENING ports used by hdbnameserver
	// using ss command
	argsToSplit = "-c 'sudo ss -lp | grep $(pidof hdbnameserver) | grep -v 127.0.0.1 | grep -Eo `((([0-9]{1,3}\\.){1,3}[0-9]{1,3})|(\\*))\\:[0-9]{3,5}`'"
	result = p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: argsToSplit,
	})
	if !strings.Contains(result.StdErr, "command not found") {
		socket := strings.Split(result.StdOut, "\n")[0]
		// Replacing '0.0.0.0' with wildcard '*' as '*' listens to all available
		// interfaces on both ipv6 and ipv4 addresses, whereas 0.0.0.0 listens to
		// all interfaces on ipv4 addresses.
		// 'lsof' command is more generic than 'ss'. Generally, 0.0.0.0 and * are equivalent,
		// leading to higher chances of TCP metrics being fetched.
		socket = strings.ReplaceAll(socket, "0.0.0.0", "*")

		log.CtxLogger(ctx).Debug("Fetched listening sockets for hdbnameserver using ss command")
		return socket, nil
	}

	log.CtxLogger(ctx).Debugw("Could not fetch listening sockets for hdbnameserver", "error", result.StdErr)
	return "", result.Error
}

// fetchSSOutput fetches the output of ss command for the given socket.
func (p *Properties) fetchSSOutput(ctx context.Context, socket string) string {
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: "-c 'echo ss -tin src " + socket + " | sh'",
	})
	return result.StdOut
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
		if len(v) == 0 {
			log.Logger.Debugw("Empty value for metric:", metric)
			continue
		}
		ssValues[k] = v
	}
	return ssValues
}

// createTSList creates a slice of timeseries metrics according to the required metric values
// It returns this slice along with an error which could possibly be non-nil.
// Some error could occur in collection of one individual metric.
func (p *Properties) createTSList(ctx context.Context, pid string, reqMetrics []string, ssMap map[string]string, t string) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries

	for _, metric := range reqMetrics {
		if _, ok := ssMap[metric]; !ok {
			log.CtxLogger(ctx).Debug("Metric skipped, could not find metric: ", metric)
			continue
		}

		var val any
		var err error
		if t == "float64" {
			if metric == "rtt" {
				val, err = strconv.ParseFloat(strings.Split(ssMap[metric], "/")[0], 64)
			} else {
				val, err = strconv.ParseFloat(ssMap[metric], 64)
			}
		} else {
			val, err = strconv.ParseInt(ssMap[metric], 10, 64)
		}
		if err != nil {
			log.CtxLogger(ctx).Debugw("error in parsing value", "could not convert value to type:", t, "metric:", metric, "Val: ", ssMap[metric], "err: ", err)
			return nil, err
		}

		ssMetrics := p.collectTCPMetrics(ctx, metric, pid, metricVal{val, t})
		if ssMetrics != nil {
			metrics = append(metrics, ssMetrics...)
		}
	}

	return metrics, nil
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *Properties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	attempt := 1
	var res []*mrpb.TimeSeries
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
		log.CtxLogger(ctx).Infow("Retry limit exceeded", "error", err)
	}
	return res, err
}

// collectTCPMetrics collects TCP connection metrics.
func (p *Properties) collectTCPMetrics(ctx context.Context, metric, pid string, data metricVal) []*mrpb.TimeSeries {
	labels := map[string]string{
		"name":    metric,
		"process": "hdbnameserver",
		"pid":     pid,
	}

	return []*mrpb.TimeSeries{p.createMetric(labels, data)}
}

// createMetric creates a TimeSeries metric with given labels and values.
func (p *Properties) createMetric(labels map[string]string, data metricVal) *mrpb.TimeSeries {
	metricPath := path.Join(nwStatsPath, labels["name"])
	log.Logger.Debugw("Creating metric for instance", "metric", metricPath, "value", data.val, "labels", labels)

	ts := timeseries.Params{
		CloudProp:    timeseries.ConvertCloudProperties(p.Config.CloudProperties),
		MetricType:   metricURL + metricPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		BareMetal:    p.Config.BareMetal,
	}

	switch data.Type {
	case "float64":
		ts.Float64Value = data.val.(float64)
		return timeseries.BuildFloat64(ts)
	default:
		ts.Int64Value = data.val.(int64)
		return timeseries.BuildInt(ts)
	}
}

// parseSSOutput parses given SSOutput for a list of TCP connection metrics.
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
func parseSSOutput(ctx context.Context, ssOutput string) []string {
	var metrics []string
	out := strings.Split(ssOutput, "\n")

	// Checking if TCP connection metrics has been found or not
	if len(out) <= 3 {
		log.CtxLogger(ctx).Debug("TCP connection metrics not received")
		return metrics
	}

	metricString := out[2]

	// TCP SS metrics received using ss command presents the metrics in two ways:
	// 	rtt:0.027/0.23, OR
	// 	pacing_rate 306153576640bps
	// metricRe matches for substrings following either of these display patterns
	// A string slice of these metrics gets generated: ["rtt:0.026,0.23", "pacing_rate 306153576640bps"]
	metrics = metricRe.FindAllString(metricString, -1)
	log.CtxLogger(ctx).Debug("Metric List: ", metrics)

	return metrics
}
