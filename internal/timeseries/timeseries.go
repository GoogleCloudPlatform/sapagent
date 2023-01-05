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

// Package timeseries has cloudmonitoring timeseries related helper functions.
package timeseries

import (
	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	tpb "google.golang.org/protobuf/types/known/timestamppb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// Params has the necessary data to create a timeseries points.
type Params struct {
	BareMetal    bool
	CloudProp    *ipb.CloudProperties
	MetricType   string
	MetricLabels map[string]string
	Timestamp    *tpb.Timestamp
	Int64Value   int64
	Float64Value float64
	BoolValue    bool
}

// BuildInt builds a cloudmonitoring timeseries with Int64 point.
func BuildInt(p Params) *monitoringresourcespb.TimeSeries {
	ts := buildTimeSeries(p)
	ts.Points = []*monitoringresourcespb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.Timestamp,
			EndTime:   p.Timestamp,
		},
		Value: &cpb.TypedValue{
			Value: &cpb.TypedValue_Int64Value{
				Int64Value: p.Int64Value,
			},
		},
	}}
	return ts
}

// BuildBool builds a cloudmonitoring timeseries with boolean point.
func BuildBool(p Params) *monitoringresourcespb.TimeSeries {
	ts := buildTimeSeries(p)
	ts.Points = []*monitoringresourcespb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.Timestamp,
			EndTime:   p.Timestamp,
		},
		Value: &cpb.TypedValue{
			Value: &cpb.TypedValue_BoolValue{
				BoolValue: p.BoolValue,
			},
		},
	}}
	return ts
}

// BuildFloat64 builds a cloudmonitoring timeseries with float64 point.
func BuildFloat64(p Params) *monitoringresourcespb.TimeSeries {
	ts := buildTimeSeries(p)
	ts.Points = []*monitoringresourcespb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.Timestamp,
			EndTime:   p.Timestamp,
		},
		Value: &cpb.TypedValue{
			Value: &cpb.TypedValue_DoubleValue{
				DoubleValue: p.Float64Value,
			},
		},
	}}
	return ts
}

func buildTimeSeries(p Params) *monitoringresourcespb.TimeSeries {
	return &monitoringresourcespb.TimeSeries{
		Metric: &mpb.Metric{
			Type:   p.MetricType,
			Labels: p.MetricLabels,
		},
		MetricKind: mpb.MetricDescriptor_GAUGE,
		Resource:   monitoredResource(p.CloudProp, p.BareMetal),
	}
}

func monitoredResource(cp *ipb.CloudProperties, bareMetal bool) *mrpb.MonitoredResource {
	if bareMetal {
		return &mrpb.MonitoredResource{
			Type: "generic_node",
			Labels: map[string]string{
				"project_id": cp.GetProjectId(),
				"location":   cp.GetRegion(),
				"namespace":  cp.GetInstanceId(),
				"node_id":    cp.GetInstanceId(),
			},
		}
	}
	return &mrpb.MonitoredResource{
		Type: "gce_instance",
		Labels: map[string]string{
			"project_id":  cp.GetProjectId(),
			"zone":        cp.GetZone(),
			"instance_id": cp.GetInstanceId(),
		},
	}
}
