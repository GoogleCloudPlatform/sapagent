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
	mrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tpb "google.golang.org/protobuf/types/known/timestamppb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// CloudProperties has the necessary data to create a timeseries points.
type CloudProperties struct {
	ProjectID        string
	InstanceID       string
	Zone             string
	InstanceName     string
	Image            string
	NumericProjectID string
	Region           string
}

// Params has the necessary data to create a timeseries points.
type Params struct {
	BareMetal    bool
	CloudProp    *CloudProperties
	MetricType   string
	MetricLabels map[string]string
	MetricKind   mpb.MetricDescriptor_MetricKind
	StartTime    *tpb.Timestamp
	Timestamp    *tpb.Timestamp
	Int64Value   int64
	Float64Value float64
	BoolValue    bool
}

// ConvertCloudProperties converts Cloud Properties proto to CloudProperties struct.
func ConvertCloudProperties(cp *ipb.CloudProperties) *CloudProperties {
	return &CloudProperties{
		ProjectID:        cp.GetProjectId(),
		InstanceID:       cp.GetInstanceId(),
		Zone:             cp.GetZone(),
		InstanceName:     cp.GetInstanceName(),
		Image:            cp.GetImage(),
		NumericProjectID: cp.GetNumericProjectId(),
		Region:           cp.GetRegion(),
	}
}

// BuildInt builds a cloudmonitoring timeseries with Int64 point.
func BuildInt(p Params) *mrpb.TimeSeries {
	ts := buildTimeSeries(p)
	if p.StartTime == nil {
		p.StartTime = p.Timestamp
	}
	ts.Points = []*mrpb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.StartTime,
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
func BuildBool(p Params) *mrpb.TimeSeries {
	ts := buildTimeSeries(p)
	if p.StartTime == nil {
		p.StartTime = p.Timestamp
	}
	ts.Points = []*mrpb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.StartTime,
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
func BuildFloat64(p Params) *mrpb.TimeSeries {
	ts := buildTimeSeries(p)
	if p.StartTime == nil {
		p.StartTime = p.Timestamp
	}
	ts.Points = []*mrpb.Point{{
		Interval: &cpb.TimeInterval{
			StartTime: p.StartTime,
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

func buildTimeSeries(p Params) *mrpb.TimeSeries {
	if p.MetricKind == mpb.MetricDescriptor_METRIC_KIND_UNSPECIFIED {
		p.MetricKind = mpb.MetricDescriptor_GAUGE
	}
	return &mrpb.TimeSeries{
		Metric: &mpb.Metric{
			Type:   p.MetricType,
			Labels: p.MetricLabels,
		},
		MetricKind: p.MetricKind,
		Resource:   monitoredResource(p.CloudProp, p.BareMetal),
	}
}

func monitoredResource(cp *CloudProperties, bareMetal bool) *mrespb.MonitoredResource {
	if bareMetal {
		return &mrespb.MonitoredResource{
			Type: "generic_node",
			Labels: map[string]string{
				"project_id": cp.ProjectID,
				"location":   cp.Region,
				"namespace":  cp.InstanceName,
				"node_id":    cp.InstanceName,
			},
		}
	}
	return &mrespb.MonitoredResource{
		Type: "gce_instance",
		Labels: map[string]string{
			"project_id":  cp.ProjectID,
			"zone":        cp.Zone,
			"instance_id": cp.InstanceID,
		},
	}
}
