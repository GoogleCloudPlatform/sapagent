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

package workloadmanager

import (
	"context"
	"os"
	"testing"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
)

func TestProcessInsights(t *testing.T) {
	params := Parameters{
		Config:            defaultConfigurationDBMetrics,
		TimeSeriesCreator: &fake.TimeSeriesCreator{},
		BackOffs:          defaultBackOffIntervals,
		WLMService:        &testWLMInterface{},
	}

	insights := make(ruleengine.Insights)
	insights["rule_id"] = []ruleengine.ValidationResult{
		ruleengine.ValidationResult{
			RecommendationID: "recommendation_1",
		},
		ruleengine.ValidationResult{
			RecommendationID: "recommendation_2",
			Result:           true,
		},
	}

	want := WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/hanasecurity",
				Labels: map[string]string{
					"rule_id_recommendation_1": "false",
					"rule_id_recommendation_2": "true",
				},
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
			Points: []*mrpb.Point{{
				Interval: &cpb.TimeInterval{},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: 1,
					},
				},
			}},
		}},
	}

	got := processInsights(context.Background(), params, insights)
	if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
		t.Errorf("processInsights() failure diff (-want +got):\n%s", diff)
	}
}

func TestCollectDBMetricsOnce(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   error
	}{
		{
			name:   "HANAMetricsConfigNotSet",
			params: Parameters{},
			want:   cmpopts.AnyError,
		},
		{
			name:   "NoHANAInsightsRules",
			params: Parameters{HANAInsightRules: []*rpb.Rule{}},
			want:   cmpopts.AnyError,
		},
		{
			name: "HANAMetricsConfigSetMetricOverride",
			params: Parameters{
				Config: defaultConfigurationDBMetrics,
				HANAInsightRules: []*rpb.Rule{
					&rpb.Rule{},
				},
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				BackOffs:          defaultBackOffIntervals,
				WLMService:        &testWLMInterface{},
				OSStatReader: func(string) (os.FileInfo, error) {
					f, err := testFS.Open("test_data/metricoverride.yaml")
					if err != nil {
						return nil, err
					}
					return f.Stat()
				},
			},
		},
		{
			name: "HANAMetricsConfigSetNoOverride",
			params: Parameters{
				Config: defaultConfigurationDBMetrics,
				HANAInsightRules: []*rpb.Rule{
					&rpb.Rule{},
				},
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				BackOffs:          defaultBackOffIntervals,
				WLMService:        &testWLMInterface{},
				OSStatReader: func(data string) (os.FileInfo, error) {
					return nil, cmpopts.AnyError
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectDBMetricsOnce(context.Background(), test.params)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectDBMetricsOnce(%v)=%v, want: %v", test.params, got, test.want)
			}
		})
	}
}
