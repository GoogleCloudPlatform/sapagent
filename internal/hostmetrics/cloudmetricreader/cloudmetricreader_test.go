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

package cloudmetricreader

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"

	commonpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	configpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
	mpb "github.com/GoogleCloudPlatform/sap-agent/protos/metrics"
)

var (
	at                        = agenttime.New(agenttime.Clock{})
	adapter1, adapter2        = "networkAdapter1", "networkAdapter2"
	disk1, disk2              = "devicename", "diskname"
	wantUnavailable           = strconv.FormatFloat(metricsformatter.Unavailable, 'f', 1, 64)
	defaultInstanceProperties = &instancepb.InstanceProperties{
		Disks: []*instancepb.Disk{
			&instancepb.Disk{DeviceName: disk1},
			&instancepb.Disk{DiskName: disk2},
		},
		NetworkAdapters: []*instancepb.NetworkAdapter{
			&instancepb.NetworkAdapter{Name: adapter1},
			&instancepb.NetworkAdapter{Name: adapter2},
		},
	}
	emptyInstanceProperties = &instancepb.InstanceProperties{}
	defaultCloudProperties  = &instancepb.CloudProperties{
		ProjectId:  "testproject",
		InstanceId: "testinstance",
	}
	defaultConfig = &configpb.Configuration{
		CloudProperties: defaultCloudProperties,
	}
	defaultMetrics = map[string]*mpb.Metric{
		"cpuUtilization": &mpb.Metric{
			Context:         sapMetrics[metricCPUUtilization].context,
			Category:        sapMetrics[metricCPUUtilization].category,
			Type:            sapMetrics[metricCPUUtilization].metricType,
			Name:            sapMetrics[metricCPUUtilization].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricCPUUtilization].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			Value:           wantUnavailable,
		},
		"network1ReadThroughput": &mpb.Metric{
			Context:         sapMetrics[metricRXBytesCount].context,
			Category:        sapMetrics[metricRXBytesCount].category,
			Type:            sapMetrics[metricRXBytesCount].metricType,
			Name:            sapMetrics[metricRXBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricRXBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        adapter1,
			Value:           wantUnavailable,
		},
		"network1WriteThroughput": &mpb.Metric{
			Context:         sapMetrics[metricTXBytesCount].context,
			Category:        sapMetrics[metricTXBytesCount].category,
			Type:            sapMetrics[metricTXBytesCount].metricType,
			Name:            sapMetrics[metricTXBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricTXBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        adapter1,
			Value:           wantUnavailable,
		},
		"network2ReadThroughput": &mpb.Metric{
			Context:         sapMetrics[metricRXBytesCount].context,
			Category:        sapMetrics[metricRXBytesCount].category,
			Type:            sapMetrics[metricRXBytesCount].metricType,
			Name:            sapMetrics[metricRXBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricRXBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        adapter2,
			Value:           wantUnavailable,
		},
		"network2WriteThroughput": &mpb.Metric{
			Context:         sapMetrics[metricTXBytesCount].context,
			Category:        sapMetrics[metricTXBytesCount].category,
			Type:            sapMetrics[metricTXBytesCount].metricType,
			Name:            sapMetrics[metricTXBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricTXBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        adapter2,
			Value:           wantUnavailable,
		},
		"disk1VolumeReadThroughput": &mpb.Metric{
			Context:         sapMetrics[metricDiskReadBytesCount].context,
			Category:        sapMetrics[metricDiskReadBytesCount].category,
			Type:            sapMetrics[metricDiskReadBytesCount].metricType,
			Name:            sapMetrics[metricDiskReadBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskReadBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk1,
			Value:           wantUnavailable,
		},
		"disk1VolumeWriteThroughput": &mpb.Metric{
			Context:         sapMetrics[metricDiskWriteBytesCount].context,
			Category:        sapMetrics[metricDiskWriteBytesCount].category,
			Type:            sapMetrics[metricDiskWriteBytesCount].metricType,
			Name:            sapMetrics[metricDiskWriteBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskWriteBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk1,
			Value:           wantUnavailable,
		},
		"disk1VolumeReadOps": &mpb.Metric{
			Context:         sapMetrics[metricDiskReadOpsCount].context,
			Category:        sapMetrics[metricDiskReadOpsCount].category,
			Type:            sapMetrics[metricDiskReadOpsCount].metricType,
			Name:            sapMetrics[metricDiskReadOpsCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskReadOpsCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk1,
			Value:           wantUnavailable,
		},
		"disk1VolumeWriteOps": &mpb.Metric{
			Context:         sapMetrics[metricDiskWriteOpsCount].context,
			Category:        sapMetrics[metricDiskWriteOpsCount].category,
			Type:            sapMetrics[metricDiskWriteOpsCount].metricType,
			Name:            sapMetrics[metricDiskWriteOpsCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskWriteOpsCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk1,
			Value:           wantUnavailable,
		},
		"disk1VolumeUtilization": &mpb.Metric{
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_DISK,
			Type:            mpb.Type_TYPE_DOUBLE,
			Name:            "Volume Utilization",
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            mpb.Unit_UNIT_PERCENT,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk1,
			Value:           "0.0",
		},
		"disk2VolumeReadThroughput": &mpb.Metric{
			Context:         sapMetrics[metricDiskReadBytesCount].context,
			Category:        sapMetrics[metricDiskReadBytesCount].category,
			Type:            sapMetrics[metricDiskReadBytesCount].metricType,
			Name:            sapMetrics[metricDiskReadBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskReadBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk2,
			Value:           wantUnavailable,
		},
		"disk2VolumeWriteThroughput": &mpb.Metric{
			Context:         sapMetrics[metricDiskWriteBytesCount].context,
			Category:        sapMetrics[metricDiskWriteBytesCount].category,
			Type:            sapMetrics[metricDiskWriteBytesCount].metricType,
			Name:            sapMetrics[metricDiskWriteBytesCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskWriteBytesCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk2,
			Value:           wantUnavailable,
		},
		"disk2VolumeReadOps": &mpb.Metric{
			Context:         sapMetrics[metricDiskReadOpsCount].context,
			Category:        sapMetrics[metricDiskReadOpsCount].category,
			Type:            sapMetrics[metricDiskReadOpsCount].metricType,
			Name:            sapMetrics[metricDiskReadOpsCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskReadOpsCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk2,
			Value:           wantUnavailable,
		},
		"disk2VolumeWriteOps": &mpb.Metric{
			Context:         sapMetrics[metricDiskWriteOpsCount].context,
			Category:        sapMetrics[metricDiskWriteOpsCount].category,
			Type:            sapMetrics[metricDiskWriteOpsCount].metricType,
			Name:            sapMetrics[metricDiskWriteOpsCount].sapName,
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            sapMetrics[metricDiskWriteOpsCount].unit,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk2,
			Value:           wantUnavailable,
		},
		"disk2VolumeUtilization": &mpb.Metric{
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_DISK,
			Type:            mpb.Type_TYPE_DOUBLE,
			Name:            "Volume Utilization",
			LastRefresh:     at.CloudMetricRefresh().Unix(),
			Unit:            mpb.Unit_UNIT_PERCENT,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        disk2,
			Value:           "0.0",
		},
	}
	// When calling QueryTimeSeries(), the resulting number of time series data entries, or values
	// in each time series data point, may differ based on the query in a way that is non-trivial
	// to detect with a fake implementation. Instead, return a variety of sample data that satisfies
	// all possible queries.
	defaultTimeSeriesData = []*mrpb.TimeSeriesData{
		{
			PointData: []*mrpb.TimeSeriesData_PointData{
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{0.12345}},
						{Value: &commonpb.TypedValue_DoubleValue{1}},
						{Value: &commonpb.TypedValue_DoubleValue{60}},
						{Value: &commonpb.TypedValue_DoubleValue{90}},
					},
				},
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{0.23456}},
						{Value: &commonpb.TypedValue_DoubleValue{2}},
						{Value: &commonpb.TypedValue_DoubleValue{120}},
						{Value: &commonpb.TypedValue_DoubleValue{180}},
					},
				},
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{0.34567}},
						{Value: &commonpb.TypedValue_DoubleValue{3}},
						{Value: &commonpb.TypedValue_DoubleValue{180}},
						{Value: &commonpb.TypedValue_DoubleValue{360}},
					},
				},
			},
		},
		{
			PointData: []*mrpb.TimeSeriesData_PointData{
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{100}},
						{Value: &commonpb.TypedValue_DoubleValue{200}},
						{Value: &commonpb.TypedValue_DoubleValue{300}},
						{Value: &commonpb.TypedValue_DoubleValue{400}},
					},
				},
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{500}},
						{Value: &commonpb.TypedValue_DoubleValue{600}},
						{Value: &commonpb.TypedValue_DoubleValue{700}},
						{Value: &commonpb.TypedValue_DoubleValue{800}},
					},
				},
				{
					Values: []*commonpb.TypedValue{
						{Value: &commonpb.TypedValue_DoubleValue{900}},
						{Value: &commonpb.TypedValue_DoubleValue{1000}},
						{Value: &commonpb.TypedValue_DoubleValue{1100}},
						{Value: &commonpb.TypedValue_DoubleValue{1200}},
					},
				},
			},
		},
	}
	metricValues = func(c *mpb.MetricsCollection, values []string) *mpb.MetricsCollection {
		copy := proto.Clone(c).(*mpb.MetricsCollection)
		metrics := copy.GetMetrics()
		for i, v := range values {
			if i < len(metrics) {
				metrics[i].Value = v
			}
		}
		return copy
	}
)

func TestReadQueryTimeSeries(t *testing.T) {
	tests := []struct {
		name               string
		queryClient        *fake.TimeSeriesQuerier
		config             *configpb.Configuration
		instanceProperties *instancepb.InstanceProperties
		want               *mpb.MetricsCollection
	}{
		{
			name:               "returnAllMetrics",
			queryClient:        &fake.TimeSeriesQuerier{TS: defaultTimeSeriesData},
			config:             defaultConfig,
			instanceProperties: defaultInstanceProperties,
			want: metricValues(&mpb.MetricsCollection{
				Metrics: []*mpb.Metric{
					defaultMetrics["cpuUtilization"],
					defaultMetrics["network1ReadThroughput"],
					defaultMetrics["network1WriteThroughput"],
					defaultMetrics["network2ReadThroughput"],
					defaultMetrics["network2WriteThroughput"],
					defaultMetrics["disk1VolumeReadThroughput"],
					defaultMetrics["disk1VolumeWriteThroughput"],
					defaultMetrics["disk1VolumeReadOps"],
					defaultMetrics["disk1VolumeWriteOps"],
					defaultMetrics["disk1VolumeUtilization"],
					defaultMetrics["disk2VolumeReadThroughput"],
					defaultMetrics["disk2VolumeWriteThroughput"],
					defaultMetrics["disk2VolumeReadOps"],
					defaultMetrics["disk2VolumeWriteOps"],
					defaultMetrics["disk2VolumeUtilization"],
				},
			}, []string{"12.3", "0", "1", "0", "1", "0", "0", "1", "2", "100.0", "2", "3", "5", "7", "100.0"}),
		},
		{
			name:        "bareMetalConfiguration",
			queryClient: &fake.TimeSeriesQuerier{TS: defaultTimeSeriesData},
			config: &configpb.Configuration{
				BareMetal:       true,
				CloudProperties: defaultCloudProperties,
			},
			instanceProperties: defaultInstanceProperties,
			want:               &mpb.MetricsCollection{},
		},
		{
			name:               "noDisksNoNetworks",
			queryClient:        &fake.TimeSeriesQuerier{TS: defaultTimeSeriesData},
			config:             defaultConfig,
			instanceProperties: emptyInstanceProperties,
			want: metricValues(&mpb.MetricsCollection{
				Metrics: []*mpb.Metric{defaultMetrics["cpuUtilization"]},
			}, []string{"12.3"}),
		},
		{
			name: "cpuUtilizationCap",
			queryClient: &fake.TimeSeriesQuerier{
				TS: []*mrpb.TimeSeriesData{
					{
						PointData: []*mrpb.TimeSeriesData_PointData{
							{
								Values: []*commonpb.TypedValue{{Value: &commonpb.TypedValue_DoubleValue{5}}},
							},
						},
					},
				},
			},
			config:             defaultConfig,
			instanceProperties: emptyInstanceProperties,
			want: metricValues(&mpb.MetricsCollection{
				Metrics: []*mpb.Metric{defaultMetrics["cpuUtilization"]},
			}, []string{"100.0"}),
		},
		{
			name:               "missingCloudProperties",
			queryClient:        &fake.TimeSeriesQuerier{TS: defaultTimeSeriesData},
			config:             &configpb.Configuration{},
			instanceProperties: defaultInstanceProperties,
			want: &mpb.MetricsCollection{
				Metrics: []*mpb.Metric{
					defaultMetrics["cpuUtilization"],
					defaultMetrics["network1ReadThroughput"],
					defaultMetrics["network1WriteThroughput"],
					defaultMetrics["network2ReadThroughput"],
					defaultMetrics["network2WriteThroughput"],
					defaultMetrics["disk1VolumeReadThroughput"],
					defaultMetrics["disk1VolumeWriteThroughput"],
					defaultMetrics["disk1VolumeReadOps"],
					defaultMetrics["disk1VolumeWriteOps"],
					defaultMetrics["disk1VolumeUtilization"],
					defaultMetrics["disk2VolumeReadThroughput"],
					defaultMetrics["disk2VolumeWriteThroughput"],
					defaultMetrics["disk2VolumeReadOps"],
					defaultMetrics["disk2VolumeWriteOps"],
					defaultMetrics["disk2VolumeUtilization"],
				},
			},
		},
		{
			name: "errQueryTimeSeries",
			queryClient: &fake.TimeSeriesQuerier{
				Err: errors.New("Query Time Series error"),
			},
			config: defaultConfig,
			// TODO(b/254459768): Fully testing all error paths currently causes a timeout.
			// Need to allow backoff interval to be adjusted in test contexts.
			instanceProperties: emptyInstanceProperties,
			want: &mpb.MetricsCollection{
				Metrics: []*mpb.Metric{
					defaultMetrics["cpuUtilization"],
				},
			},
		},
		{
			name: "emptyTimeSeries",
			queryClient: &fake.TimeSeriesQuerier{
				TS: []*mrpb.TimeSeriesData{},
			},
			config:             defaultConfig,
			instanceProperties: defaultInstanceProperties,
			want: &mpb.MetricsCollection{
				Metrics: []*mpb.Metric{
					defaultMetrics["cpuUtilization"],
					defaultMetrics["network1ReadThroughput"],
					defaultMetrics["network1WriteThroughput"],
					defaultMetrics["network2ReadThroughput"],
					defaultMetrics["network2WriteThroughput"],
					defaultMetrics["disk1VolumeReadThroughput"],
					defaultMetrics["disk1VolumeWriteThroughput"],
					defaultMetrics["disk1VolumeReadOps"],
					defaultMetrics["disk1VolumeWriteOps"],
					defaultMetrics["disk1VolumeUtilization"],
					defaultMetrics["disk2VolumeReadThroughput"],
					defaultMetrics["disk2VolumeWriteThroughput"],
					defaultMetrics["disk2VolumeReadOps"],
					defaultMetrics["disk2VolumeWriteOps"],
					defaultMetrics["disk2VolumeUtilization"],
				},
			},
		},
		{
			name: "emptyPointData",
			queryClient: &fake.TimeSeriesQuerier{
				TS: []*mrpb.TimeSeriesData{
					{
						PointData: []*mrpb.TimeSeriesData_PointData{},
					},
				},
			},
			config:             defaultConfig,
			instanceProperties: defaultInstanceProperties,
			want: &mpb.MetricsCollection{
				Metrics: []*mpb.Metric{
					defaultMetrics["cpuUtilization"],
					defaultMetrics["network1ReadThroughput"],
					defaultMetrics["network1WriteThroughput"],
					defaultMetrics["network2ReadThroughput"],
					defaultMetrics["network2WriteThroughput"],
					defaultMetrics["disk1VolumeReadThroughput"],
					defaultMetrics["disk1VolumeWriteThroughput"],
					defaultMetrics["disk1VolumeReadOps"],
					defaultMetrics["disk1VolumeWriteOps"],
					defaultMetrics["disk1VolumeUtilization"],
					defaultMetrics["disk2VolumeReadThroughput"],
					defaultMetrics["disk2VolumeWriteThroughput"],
					defaultMetrics["disk2VolumeReadOps"],
					defaultMetrics["disk2VolumeWriteOps"],
					defaultMetrics["disk2VolumeUtilization"],
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := CloudMetricReader{
				QueryClient: test.queryClient,
			}
			got := r.Read(context.Background(), test.config, test.instanceProperties, *at)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("Read() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestBuildVolumeUtilization(t *testing.T) {
	refresh := time.Now()
	deviceID := "testdevice"
	wantMetric := func(v float64) *mpb.Metric {
		return &mpb.Metric{
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_DISK,
			Type:            mpb.Type_TYPE_DOUBLE,
			Name:            "Volume Utilization",
			LastRefresh:     refresh.Unix(),
			Unit:            mpb.Unit_UNIT_PERCENT,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			DeviceId:        deviceID,
			Value:           strconv.FormatFloat(v, 'f', 1, 64),
		}
	}

	tests := []struct {
		name        string
		readOps     float64
		writeOps    float64
		maxReadOps  float64
		maxWriteOps float64
		want        *mpb.Metric
	}{
		{
			name:        "unavailableOps",
			readOps:     metricsformatter.Unavailable,
			writeOps:    metricsformatter.Unavailable,
			maxReadOps:  1,
			maxWriteOps: 1,
			want:        wantMetric(0),
		},
		{
			name:        "unavailableMaxOps",
			readOps:     1,
			writeOps:    1,
			maxReadOps:  metricsformatter.Unavailable,
			maxWriteOps: metricsformatter.Unavailable,
			want:        wantMetric(0),
		},
		{
			name:        "zeroOps",
			readOps:     0,
			writeOps:    0,
			maxReadOps:  1,
			maxWriteOps: 1,
			want:        wantMetric(0),
		},
		{
			name:        "zeroMaxOps",
			readOps:     1,
			writeOps:    1,
			maxReadOps:  0,
			maxWriteOps: 0,
			want:        wantMetric(0),
		},
		{
			name:        "volumeUtilizationCap",
			readOps:     2,
			writeOps:    2,
			maxReadOps:  1,
			maxWriteOps: 1,
			want:        wantMetric(100),
		},
		{
			name:        "success",
			readOps:     1,
			writeOps:    1,
			maxReadOps:  2,
			maxWriteOps: 2,
			want:        wantMetric(50),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildVolumeUtilization(test.readOps, test.writeOps, test.maxReadOps, test.maxWriteOps, refresh, deviceID)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("buildVolumeUtilization(%g, %g, %g, %g, %s, %s) mismatch (-want, +got):\n%s", test.readOps, test.writeOps, test.maxReadOps, test.maxWriteOps, refresh, deviceID, d)
			}
		})
	}
}
