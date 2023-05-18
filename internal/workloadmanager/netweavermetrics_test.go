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
	"context"
	"net"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"

	"github.com/google/go-cmp/cmp"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
)

func wantLinuxNetWeaverMetrics(ts *timestamppb.Timestamp, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/netweaver",
				Labels: map[string]string{},
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
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: 0.0,
					},
				},
			}},
		}},
	}
}

func createNetWeaverWorkloadMetrics(labels map[string]string, value float64) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/netweaver",
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

func TestCollectNetWeaverMetricsFromConfig(t *testing.T) {
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
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{}
				},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "NetWeaverMetricsEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{},
				osVendorID:     "rhel",
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{}
				},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "NetWeaverMetricsNotEmpty",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/netweaver",
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
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "bar",
					}
				},
			},
			wantLabels: map[string]string{
				"foo": "bar",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := createNetWeaverWorkloadMetrics(test.wantLabels, 0)
			got := CollectNetWeaverMetricsFromConfig(context.Background(), test.params)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectNetWeaverMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectNetWeaverMetrics(t *testing.T) {
	duration := time.Now().Add(3 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), duration)

	defer cancel()

	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}

	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1}, nil
	}
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}

	nch := make(chan WorkloadMetrics)
	p := Parameters{
		Config: cnf,
		OSType: "linux",
	}
	go CollectNetWeaverMetrics(p, nch)

	select {
	case got := <-nch:
		want := wantLinuxNetWeaverMetrics(nts, "test-os-version")

		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("CollectNetWeaverMetrics() returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Errorf("CollectNetWeaverMetrics() timed out")
	}

}
