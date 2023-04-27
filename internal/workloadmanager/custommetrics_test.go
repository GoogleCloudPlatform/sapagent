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

package workloadmanager

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

func createCustomWorkloadMetrics(labels map[string]string, value float64) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/custom",
				Labels: labels,
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				// We are choosing to ignore these timestamp values when performing a comparison via cmp.Diff().
				Interval: &cpb.TimeInterval{
					StartTime: &timestamppb.Timestamp{Seconds: time.Now().Unix()},
					EndTime:   &timestamppb.Timestamp{Seconds: time.Now().Unix()},
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: value,
					},
				},
			}},
		}},
	}
}

func TestCollectCustomMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name       string
		params     Parameters
		wantLabels map[string]string
	}{
		{
			name: "DefaultCollectionDefinition",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: collectionDefinition.GetWorkloadValidation(),
				osVendorID:     "rhel",
				CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) {
					return "", "", nil
				},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "CustomMetricsEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{},
				osVendorID:     "rhel",
				CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) {
					return "", "", nil
				},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "CustomMetricsNotEmpty",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "bar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromOutput{ValueFromOutput: true},
										},
									},
								},
							},
						},
					},
				},
				osVendorID: "rhel",
				CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) {
					return "bar", "", nil
				},
			},
			wantLabels: map[string]string{
				"foo": "bar",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := createCustomWorkloadMetrics(test.wantLabels, 1)
			got := CollectCustomMetricsFromConfig(test.params)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectCustomMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}