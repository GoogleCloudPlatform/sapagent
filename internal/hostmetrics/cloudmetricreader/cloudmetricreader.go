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

// Package cloudmetricreader provides functionality for interfacing with the cloud monitoring API.
package cloudmetricreader

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/iterator"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"

	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	metricspb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
)

// QueryClient is a mockable wrapper around the cloud monitoring API Go client.
//
// See [QueryClient] for the real client.
//
// [QueryClient]: https://pkg.go.dev/cloud.google.com/go/monitoring/apiv3/v2
type QueryClient struct {
	Client *monitoring.QueryClient
}

// QueryTimeSeries fetches time series data that matches a query.
func (c *QueryClient) QueryTimeSeries(ctx context.Context, req *mpb.QueryTimeSeriesRequest, opts ...gax.CallOption) ([]*mrpb.TimeSeriesData, error) {
	it := c.Client.QueryTimeSeries(ctx, req, opts...)
	data := []*mrpb.TimeSeriesData{}
	for timeseries, err := it.Next(); err != iterator.Done; timeseries, err = it.Next() {
		if err != nil {
			return nil, err
		}
		data = append(data, timeseries)
	}
	return data, nil
}

type queryCallOptions struct {
	filter          string
	start           time.Time
	end             time.Time
	alignmentPeriod string
	alignment       string
	aggregation     string
}

// CloudMetricReader handles how metrics will be read and returned.
type CloudMetricReader struct {
	QueryClient cloudmonitoring.TimeSeriesQuerier
	BackOffs    *cloudmonitoring.BackOffIntervals
}

// Read reads metrics from the cloud monitoring API and returns a MetricsCollection.
func (r *CloudMetricReader) Read(ctx context.Context, config *configpb.Configuration, ip *instancepb.InstanceProperties, at agenttime.AgentTime) *metricspb.MetricsCollection {
	log.CtxLogger(ctx).Debug("Getting metrics from cloud monitoring...")
	return r.readQueryTimeSeries(ctx, config, ip, at)
}

// readQueryTimeSeries reads metrics from the cloud monitoring API and returns a MetricsCollection.
func (r *CloudMetricReader) readQueryTimeSeries(ctx context.Context, config *configpb.Configuration, ip *instancepb.InstanceProperties, at agenttime.AgentTime) *metricspb.MetricsCollection {
	// We do not collect any metrics from cloud monitoring on Bare Metal.
	if config.GetBareMetal() == true {
		return &metricspb.MetricsCollection{}
	}

	var (
		metrics []*metricspb.Metric
		refresh = at.CloudMetricRefresh()
	)

	log.CtxLogger(ctx).Info("Collecting metrics via QueryTimeSeries.")

	metrics = append(metrics, r.createPassthroughMetrics(ctx, config, refresh)...)
	metrics = append(metrics, r.createNetworkMetrics(ctx, ip.GetNetworkAdapters(), config, refresh)...)
	metrics = append(metrics, r.createDiskMetrics(ctx, ip.GetDisks(), config, refresh)...)

	return &metricspb.MetricsCollection{Metrics: metrics}
}

// createPassthroughMetrics generates metrics associated with the overall state of the system.
func (r *CloudMetricReader) createPassthroughMetrics(ctx context.Context, config *configpb.Configuration, refresh time.Time) []*metricspb.Metric {
	passthroughMetrics := []sapMetricKey{metricCPUUtilization}

	var data []*mrpb.TimeSeriesData
	cp := config.GetCloudProperties()
	if cp != nil {
		data = r.queryTimeSeriesData(ctx, passthroughMetrics, cp.GetProjectId(), queryCallOptions{
			filter:          fmt.Sprintf("resource.instance_id='%s'", cp.GetInstanceId()),
			start:           refresh.Add(-3 * time.Minute),
			end:             refresh,
			alignmentPeriod: "60s",
			alignment:       "mean",
		})
	}
	metricValues := parseTimeSeriesData(ctx, passthroughMetrics, data)
	log.CtxLogger(ctx).Debugw("Metric values", "values", metricValues)

	var metrics []*metricspb.Metric
	for k, v := range metricValues {
		metrics = append(metrics, buildMetric(ctx, k, v, refresh, ""))
	}
	return metrics
}

// createNetworkMetrics generates metrics associated with each of the network adapters in use in the system.
func (r *CloudMetricReader) createNetworkMetrics(ctx context.Context, adapters []*instancepb.NetworkAdapter, config *configpb.Configuration, refresh time.Time) []*metricspb.Metric {
	var metrics []*metricspb.Metric
	if len(adapters) == 0 {
		return metrics
	}

	networkMetrics := []sapMetricKey{metricRXBytesCount, metricTXBytesCount}

	var data []*mrpb.TimeSeriesData
	cp := config.GetCloudProperties()
	if cp != nil {
		data = r.queryTimeSeriesData(ctx, networkMetrics, cp.GetProjectId(), queryCallOptions{
			filter:          fmt.Sprintf("resource.instance_id='%s'", cp.GetInstanceId()),
			start:           refresh.Add(-3 * time.Minute),
			end:             refresh,
			alignmentPeriod: "60s",
			alignment:       "delta",
		})
	}
	metricValues := parseTimeSeriesData(ctx, networkMetrics, data)
	log.CtxLogger(ctx).Debugw("Network metric values", "values", metricValues)

	// Since device is shared across adapters, it is expected that the metrics returned will be the same for each.
	for _, adapter := range adapters {
		for _, k := range networkMetrics {
			if v, ok := metricValues[k]; ok {
				metrics = append(metrics, buildMetric(ctx, k, v, refresh, adapter.GetName()))
			}
		}
	}
	return metrics
}

// createDiskMetrics generates metrics associated with each of the disks in use in the system.
func (r *CloudMetricReader) createDiskMetrics(ctx context.Context, disks []*instancepb.Disk, config *configpb.Configuration, refresh time.Time) []*metricspb.Metric {
	var metrics []*metricspb.Metric
	if len(disks) == 0 {
		return metrics
	}

	var deviceNames, deviceNamesFilter []string
	for _, disk := range disks {
		name := disk.GetDiskName()
		if name == "" {
			name = disk.GetDeviceName()
		}
		deviceNames = append(deviceNames, name)
		deviceNamesFilter = append(deviceNamesFilter, fmt.Sprintf("metric.device_name='%s'", name))
	}

	diskDeltaMetrics := []sapMetricKey{metricDiskReadBytesCount, metricDiskWriteBytesCount, metricDiskReadOpsCount, metricDiskWriteOpsCount}
	diskRateMetrics := []sapMetricKey{metricDiskReadOpsCountRate, metricDiskWriteOpsCountRate}
	diskMaxMetrics := []sapMetricKey{metricDiskMaxReadOpsCount, metricDiskMaxWriteOpsCount}

	var diskDeltaData []*mrpb.TimeSeriesData
	var diskRateData []*mrpb.TimeSeriesData
	var diskMaxData []*mrpb.TimeSeriesData
	cp := config.GetCloudProperties()
	if cp != nil {
		filter := fmt.Sprintf("resource.instance_id='%s' && (%s)", cp.GetInstanceId(), strings.Join(deviceNamesFilter, " || "))
		diskDeltaData = r.queryTimeSeriesData(ctx, diskDeltaMetrics, cp.GetProjectId(), queryCallOptions{
			filter:          filter,
			start:           refresh.Add(-3 * time.Minute),
			end:             refresh,
			alignmentPeriod: "60s",
			alignment:       "delta",
		})
		diskRateData = r.queryTimeSeriesData(ctx, diskRateMetrics, cp.GetProjectId(), queryCallOptions{
			filter:          filter,
			start:           refresh.Add(-3 * time.Minute),
			end:             refresh,
			alignmentPeriod: "60s",
			alignment:       "rate",
		})
		diskMaxData = r.queryTimeSeriesData(ctx, diskMaxMetrics, cp.GetProjectId(), queryCallOptions{
			filter:          filter,
			start:           refresh.Add(-3 * time.Minute),
			end:             refresh,
			alignmentPeriod: "60s",
			aggregation:     "max",
		})
	}
	diskDeltaMetricValues := parseTimeSeriesDataByDisk(ctx, deviceNames, diskDeltaMetrics, diskDeltaData)
	log.CtxLogger(ctx).Debugw("Disk delta metric values", "values", diskDeltaMetricValues)
	diskRateMetricValues := parseTimeSeriesDataByDisk(ctx, deviceNames, diskRateMetrics, diskRateData)
	log.CtxLogger(ctx).Debugw("Disk rate metric values", "values", diskRateMetricValues)
	diskMaxMetricValues := parseTimeSeriesDataByDisk(ctx, deviceNames, diskMaxMetrics, diskMaxData)
	log.CtxLogger(ctx).Debugw("Disk max metric values", "values", diskMaxMetricValues)

	for _, deviceID := range deviceNames {
		for _, k := range diskDeltaMetrics {
			if v, ok := diskDeltaMetricValues[deviceID][k]; ok {
				metrics = append(metrics, buildMetric(ctx, k, v, refresh, deviceID))
			}
		}
		readOpsRate := diskRateMetricValues[deviceID][metricDiskReadOpsCountRate]
		writeOpsRate := diskRateMetricValues[deviceID][metricDiskWriteOpsCountRate]
		maxReadOps := diskMaxMetricValues[deviceID][metricDiskMaxReadOpsCount]
		maxWriteOps := diskMaxMetricValues[deviceID][metricDiskMaxWriteOpsCount]
		metrics = append(metrics, buildVolumeUtilization(readOpsRate, writeOpsRate, maxReadOps, maxWriteOps, refresh, deviceID))
	}
	return metrics
}

// queryTimeSeriesData builds and executes a query to fetch time series data for a series of metrics.
//
// The query is constructed using [Monitoring Query Language] syntax reference documentation.
//
// [Monitoring Query Language]: https://cloud.google.com/monitoring/mql/reference
func (r *CloudMetricReader) queryTimeSeriesData(ctx context.Context, metrics []sapMetricKey, projectID string, opts queryCallOptions) []*mrpb.TimeSeriesData {
	var fetch []string
	for _, m := range metrics {
		fetch = append(fetch, fmt.Sprintf("fetch gce_instance :: %s | value cast_double(val())", sapMetrics[m].name))
	}
	b := new(strings.Builder)
	fmt.Fprintf(b, "{%s}", strings.Join(fetch, "; "))
	if opts.filter != "" {
		fmt.Fprintf(b, " | filter %s", opts.filter)
	}
	if !opts.start.IsZero() && !opts.end.IsZero() {
		fmt.Fprintf(b, " | within d'%s', d'%s'", formatTime(opts.start), formatTime(opts.end))
	}
	if opts.alignmentPeriod != "" {
		fmt.Fprintf(b, " | every %s", opts.alignmentPeriod)
	}
	if opts.alignment != "" {
		fmt.Fprintf(b, " | %s", opts.alignment)
	}
	if opts.aggregation != "" {
		fmt.Fprintf(b, " | %s", opts.aggregation)
	}
	if len(fetch) > 1 {
		fmt.Fprintf(b, " | join")
	}
	req := &mpb.QueryTimeSeriesRequest{
		Name:  fmt.Sprintf("projects/%s", projectID),
		Query: b.String(),
	}
	log.CtxLogger(ctx).Debugw("QueryTimeSeries request", "request", b.String())
	data, err := cloudmonitoring.QueryTimeSeriesWithRetry(ctx, r.QueryClient, req, r.BackOffs)
	log.CtxLogger(ctx).Debugw("QueryTimeSeries response", "response", data, "error", err)

	// Log any error that is encountered but allow the collection of metrics to proceed with a zero time series.
	if err != nil {
		log.CtxLogger(ctx).Errorw("The QueryTimeSeries request for metrics has failed", "metrics", metrics, "error", err)
	}
	return data
}

// buildMetric returns a formatted Metric proto containing the time series data value found for a given metric key and refresh time.
func buildMetric(ctx context.Context, key sapMetricKey, value float64, refresh time.Time, deviceID string) *metricspb.Metric {
	log.CtxLogger(ctx).Debugw("Building metric with value", "metric", key, "value", value)
	sap := sapMetrics[key]
	metric := &metricspb.Metric{
		Context:         sap.context,
		Category:        sap.category,
		Type:            sap.metricType,
		Name:            sap.sapName,
		LastRefresh:     refresh.Unix(),
		Unit:            sap.unit,
		RefreshInterval: metricspb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
	}

	if deviceID != "" {
		metric.DeviceId = deviceID
	}

	switch {
	case value == metricsformatter.Unavailable:
		metric.Value = strconv.FormatFloat(value, 'f', 1, 64)
	case sap == sapMetrics[metricDiskReadBytesCount] || sap == sapMetrics[metricDiskWriteBytesCount]:
		metric.Value = strconv.FormatInt(metricsformatter.PerMinuteToPerSec(value), 10)
	case sap == sapMetrics[metricDiskReadOpsCount] || sap == sapMetrics[metricDiskWriteOpsCount]:
		metric.Value = strconv.FormatInt(metricsformatter.PerMinuteToPerSec(value), 10)
	case metric.GetType() == metricspb.Type_TYPE_INT64:
		metric.Value = strconv.FormatInt(int64(value), 10)
	case metric.GetUnit() == metricspb.Unit_UNIT_PERCENT:
		metric.Value = strconv.FormatFloat(metricsformatter.ToPercentage(math.Min(value, 1), 3), 'f', 1, 64)
	default:
		metric.Value = strconv.FormatFloat(value, 'f', -1, 64)
	}

	return metric
}

// buildVolumeUtilization returns a formatted Metric proto containing the calculated volume utilization.
func buildVolumeUtilization(readOps, writeOps, maxReadOps, maxWriteOps float64, refresh time.Time, deviceID string) *metricspb.Metric {
	var (
		readUtilization  = float64(0)
		writeUtilization = float64(0)
	)

	if readOps > 0 && maxReadOps > 0 {
		readUtilization = math.Min(readOps/maxReadOps, 1)
	}
	if writeOps > 0 && maxWriteOps > 0 {
		writeUtilization = math.Min(writeOps/maxWriteOps, 1)
	}
	volumeUtilization := metricsformatter.ToPercentage((readUtilization+writeUtilization)/2, 3)

	return &metricspb.Metric{
		Context:         metricspb.Context_CONTEXT_VM,
		Category:        metricspb.Category_CATEGORY_DISK,
		Type:            metricspb.Type_TYPE_DOUBLE,
		Name:            "Volume Utilization",
		LastRefresh:     refresh.Unix(),
		Unit:            metricspb.Unit_UNIT_PERCENT,
		RefreshInterval: metricspb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
		DeviceId:        deviceID,
		Value:           strconv.FormatFloat(volumeUtilization, 'f', 1, 64),
	}
}

// formatTime specifies a YYYY/MM/DD-HH:mm timestamp for use in a time series query.
func formatTime(t time.Time) string {
	return fmt.Sprintf("%d/%02d/%02d-%02d:%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
}

// parseTimeSeriesData maps a series of metric keys to the value returned in a time series response.
//
// The structure of the map is as follows:
//
//	metricValues = {
//	  "metricKey1": float64(metricValue1),
//	  "metricKey2": float64(metricValue2),
//	  ...
//	}
func parseTimeSeriesData(ctx context.Context, metrics []sapMetricKey, data []*mrpb.TimeSeriesData) map[sapMetricKey]float64 {
	metricValues := make(map[sapMetricKey]float64)
	// Assign default metric values of unavailable.
	for _, m := range metrics {
		metricValues[m] = metricsformatter.Unavailable
	}

	if len(data) == 0 {
		log.CtxLogger(ctx).Debugw("There is no time series data for metrics", "metrics", metrics)
		return metricValues
	}
	points := data[0].GetPointData()
	if len(points) == 0 {
		log.CtxLogger(ctx).Debugw("There is no point data in the time series for metrics", "metrics", metrics)
		return metricValues
	}
	// In the event of multiple points in the time series, the first entry is the most recent.
	values := points[0].GetValues()
	// MQL reference documentation specifies that values returned can be referenced by index.
	// The exact index position corresponds to the entries in the point descriptors field.
	// The point descriptors field appears to return data in the same order that it was fetched.
	// Therefore, constructing the map based on the metric keys that were fetched should be sufficient.
	// If there are any issues with the ordering of metric data from the time series, this is the likely source of contention.
	for i, v := range values {
		if i < len(metrics) {
			metricValues[metrics[i]] = v.GetDoubleValue()
		}
	}
	return metricValues
}

// parseTimeSeriesDataByDisk maps a series of device names to a nested map of metric keys/values for that device.
//
// The structure of the map is as follows:
//
//	metricValues = {
//	  "deviceName1": {
//	    "metricKey1": float64(metricValue1),
//	    "metricKey2": float64(metricValue2),
//	    ...
//	  },
//	  "deviceName2": {
//	    "metricKey1": float64(metricValue1),
//	    "metricKey2": float64(metricValue2),
//	    ...
//	  },
//	  ...
//	}
func parseTimeSeriesDataByDisk(ctx context.Context, deviceNames []string, metrics []sapMetricKey, data []*mrpb.TimeSeriesData) map[string]map[sapMetricKey]float64 {
	metricValues := make(map[string]map[sapMetricKey]float64)
	// Assign default metric values of unavailable.
	for _, d := range deviceNames {
		metricValues[d] = make(map[sapMetricKey]float64)
		for _, m := range metrics {
			metricValues[d][m] = metricsformatter.Unavailable
		}
	}

	if len(data) == 0 {
		log.CtxLogger(ctx).Debugw("There is no time series data for metrics", "metrics", metrics)
		return metricValues
	}
	for i, d := range data {
		// Each entry for time series data should correspond to a device name supplied in the query filter.
		if i < len(deviceNames) {
			metricValues[deviceNames[i]] = parseTimeSeriesData(ctx, metrics, []*mrpb.TimeSeriesData{d})
		}
	}
	return metricValues
}

// sapMetric defines the format of the metrics that are returned.
type sapMetric struct {
	sapName, name string
	seriesAligner cpb.Aggregation_Aligner
	category      metricspb.Category
	metricType    metricspb.Type
	unit          metricspb.Unit
	context       metricspb.Context
}

type sapMetricKey string

const (
	metricCPUUtilization        sapMetricKey = "CPU_UTILIZATION"
	metricRXBytesCount          sapMetricKey = "RX_BYTES_COUNT"
	metricTXBytesCount          sapMetricKey = "TX_BYTES_COUNT"
	metricDiskReadBytesCount    sapMetricKey = "DISK_READ_BYTES_COUNT"
	metricDiskWriteBytesCount   sapMetricKey = "DISK_WRITE_BYTES_COUNT"
	metricDiskReadOpsCount      sapMetricKey = "DISK_READ_OPS_COUNT"
	metricDiskWriteOpsCount     sapMetricKey = "DISK_WRITE_OPS_COUNT"
	metricDiskWriteOpsCountRate sapMetricKey = "DISK_WRITE_OPS_COUNT_RATE"
	metricDiskMaxWriteOpsCount  sapMetricKey = "DISK_MAX_WRITE_OPS_COUNT"
	metricDiskReadOpsCountRate  sapMetricKey = "DISK_READ_OPS_COUNT_RATE"
	metricDiskMaxReadOpsCount   sapMetricKey = "DISK_MAX_READ_OPS_COUNT"
)

var sapMetrics = map[sapMetricKey]sapMetric{
	metricCPUUtilization: {
		"VM Processing Power Consumption",
		"compute.googleapis.com/instance/cpu/utilization",
		cpb.Aggregation_ALIGN_MEAN,
		metricspb.Category_CATEGORY_CPU,
		metricspb.Type_TYPE_DOUBLE,
		metricspb.Unit_UNIT_PERCENT,
		metricspb.Context_CONTEXT_VM,
	},
	metricRXBytesCount: {
		"Network Read Throughput",
		"compute.googleapis.com/instance/network/received_bytes_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_NETWORK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_BPS,
		metricspb.Context_CONTEXT_VM,
	},
	metricTXBytesCount: {
		"Network Write Throughput",
		"compute.googleapis.com/instance/network/sent_bytes_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_NETWORK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_BPS,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskReadBytesCount: {
		"Volume Read Throughput",
		"compute.googleapis.com/instance/disk/read_bytes_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_BPS,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskWriteBytesCount: {
		"Volume Write Throughput",
		"compute.googleapis.com/instance/disk/write_bytes_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_BPS,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskReadOpsCount: {
		"Volume Read Ops",
		"compute.googleapis.com/instance/disk/read_ops_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskWriteOpsCount: {
		"Volume Write Ops",
		"compute.googleapis.com/instance/disk/write_ops_count",
		cpb.Aggregation_ALIGN_DELTA,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_INT64,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskWriteOpsCountRate: {
		"Write Ops Count Rate",
		"compute.googleapis.com/instance/disk/write_ops_count",
		cpb.Aggregation_ALIGN_RATE,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_DOUBLE,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskMaxWriteOpsCount: {
		"Max Write Ops Count",
		"compute.googleapis.com/instance/disk/max_write_ops_count",
		cpb.Aggregation_ALIGN_MAX,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_DOUBLE,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskReadOpsCountRate: {
		"Read Ops Count Rate",
		"compute.googleapis.com/instance/disk/read_ops_count",
		cpb.Aggregation_ALIGN_RATE,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_DOUBLE,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
	metricDiskMaxReadOpsCount: {
		"Max Read Ops Count",
		"compute.googleapis.com/instance/disk/max_read_ops_count",
		cpb.Aggregation_ALIGN_MAX,
		metricspb.Category_CATEGORY_DISK,
		metricspb.Type_TYPE_DOUBLE,
		metricspb.Unit_UNIT_OPS_PER_SEC,
		metricspb.Context_CONTEXT_VM,
	},
}
