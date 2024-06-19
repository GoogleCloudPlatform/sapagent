/*
Copyright 2024 Google LLC

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

// Package metricoverrides contains functions to override metrics.
package metricoverrides

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tpb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

type (
	// DemoMetricsReaderFunc provides a definition for a function that reads
	// the demo metrics override file.
	DemoMetricsReaderFunc func(string) (io.ReadCloser, error)

	// OSStatReader is a testable version of os.Stat.
	OSStatReader func(string) (os.FileInfo, error)

	// DemoInstanceProperties is a struct for Demo metric collection.
	DemoInstanceProperties struct {
		Config           *cpb.Configuration
		Reader           DemoMetricsReaderFunc
		DemoMetricPath   string
		MetricTypePrefix string
	}
	// metricEmitter is a struct for constructing metrics from
	// an override configuration file.
	metricEmitter struct {
		scanner       *bufio.Scanner
		tmpMetricName string
	}

	// DemoMetricData stores the metric values and labels read from
	// the override configuration file.
	demoMetricData struct {
		name         string
		int64Value   int64
		float64Value float64
		boolValue    bool
		labels       map[string]string
		metricType   mpb.MetricDescriptor_MetricKind
		isFloat      bool
		isBool       bool
	}
)

// DemoMetricsReader is a function to read demo metrics from the override file.
var DemoMetricsReader = DemoMetricsReaderFunc(func(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	var f io.ReadCloser = file
	return f, err
})

// CollectWithRetry implements the Collector interface for demo metric
// collection. We don't need to retry for demo metrics so it simply calls
// Collect().
func (ip *DemoInstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	return ip.Collect(ctx)
}

// Collect implements the Collector interface for Demo metric collection.
func (ip *DemoInstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	file, err := ip.Reader(ip.DemoMetricPath)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Could not read the metric override file", "error", err)
		return metrics, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	metricEmitter := metricEmitter{scanner, ""}
	for {
		metricData, last := metricEmitter.getMetric(ctx)
		if metricData.name != "" {
			metrics = append(metrics, metricData.createTimeSeries(ip)...)
		}
		if last {
			break
		}
	}
	return metrics, nil
}

// getMetric parses all labels for a metric type in the override file.
// Returns true if done with parsing the file, false otherwise.
func (e *metricEmitter) getMetric(ctx context.Context) (*demoMetricData, bool) {
	metricName := e.tmpMetricName
	metricIntValue := int64(0)
	metricFloatValue := float64(0.0)
	metricBoolValue := false
	isFloat := false
	isBool := false
	metricType := mpb.MetricDescriptor_GAUGE
	labels := make(map[string]string)
	var err error

	for e.scanner.Scan() {
		key, value, found := parseScannedText(ctx, e.scanner.Text())
		if !found {
			continue
		}

		switch key {
		case "metric":
			if metricName != "" {
				e.tmpMetricName = value
				return &demoMetricData{metricName, metricIntValue, metricFloatValue, metricBoolValue, labels, metricType, isFloat, isBool}, false
			}
			metricName = value
		case "metric_value":
			if metricIntValue, err = strconv.ParseInt(value, 10, 64); err == nil {
				isFloat = false
				isBool = false
			} else if metricFloatValue, err = strconv.ParseFloat(value, 64); err == nil {
				isFloat = true
				isBool = false
			} else if metricBoolValue, err = strconv.ParseBool(value); err == nil {
				isFloat = false
				isBool = true
			} else {
				log.CtxLogger(ctx).Warnw("Could not parse metric value as int or float", "value", value, "error", err)
			}
		case "metric_type":
			if value == "METRIC_CUMULATIVE" {
				metricType = mpb.MetricDescriptor_CUMULATIVE
			} else {
				metricType = mpb.MetricDescriptor_GAUGE
			}
		default:
			labels[key] = strings.TrimSpace(value)
		}
	}

	if err = e.scanner.Err(); err != nil {
		log.CtxLogger(ctx).Warnw("Could not read from the override metrics file", "error", err)
	}
	return &demoMetricData{metricName, metricIntValue, metricFloatValue, metricBoolValue, labels, metricType, isFloat, isBool}, true
}

// parseScannedText extracts a key and value pair from a scanned line of text.
// The expected format for the text string is: '<key>: <value>'.
func parseScannedText(ctx context.Context, text string) (key, value string, found bool) {
	// Ignore empty lines and comments.
	if text == "" || strings.HasPrefix(text, "#") {
		return "", "", false
	}

	key, value, found = strings.Cut(text, ":")
	if !found {
		log.CtxLogger(ctx).Warnw("Could not parse key, value pair. Expected format: '<key>: <value>'", "text", text)
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), found
}

// createTimeSeries creates a time series for the Demo metric data.
func (metricData *demoMetricData) createTimeSeries(ip *DemoInstanceProperties) []*mrpb.TimeSeries {
	timestamp := tpb.Now()
	startTimestamp := timestamp
	if metricData.metricType == mpb.MetricDescriptor_CUMULATIVE {
		startTimestamp = tpb.New(timestamp.AsTime().Add(-1 * time.Second))
	}
	tsParams := timeseries.Params{
		CloudProp:    ip.Config.CloudProperties,
		MetricType:   ip.MetricTypePrefix + metricData.name,
		MetricLabels: metricData.labels,
		Timestamp:    timestamp,
		StartTime:    startTimestamp,
		Int64Value:   metricData.int64Value,
		Float64Value: metricData.float64Value,
		BoolValue:    metricData.boolValue,
		BareMetal:    ip.Config.BareMetal,
		MetricKind:   metricData.metricType,
	}
	if metricData.isFloat {
		return []*mrpb.TimeSeries{timeseries.BuildFloat64(tsParams)}
	}
	if metricData.isBool {
		return []*mrpb.TimeSeries{timeseries.BuildBool(tsParams)}
	}
	return []*mrpb.TimeSeries{timeseries.BuildInt(tsParams)}
}
