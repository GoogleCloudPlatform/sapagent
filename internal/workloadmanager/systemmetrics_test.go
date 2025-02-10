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
	_ "embed"
	"errors"
	"net"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/configurablemetrics"
)

var (
	cnf = &cnfpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			InstanceName: "test-instance-name",
			InstanceId:   "test-instance-id",
			Zone:         "test-region-zone",
			ProjectId:    "test-project-id",
		},
		AgentProperties: &cnfpb.AgentProperties{Name: "sapagent", Version: "1.0"},
	}

	collectionConfigVersion = "22"
)

func wantSystemMetrics(ts *timestamppb.Timestamp, labels map[string]string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/system",
				Labels: labels,
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-region-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*mrpb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: 1,
					},
				},
			}},
		}},
	}
}

func TestCollectSystemMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	systemMetricOSNameVersion := &wlmpb.WorkloadValidation{
		ValidationSystem: &wlmpb.ValidationSystem{
			SystemMetrics: []*wlmpb.SystemMetric{
				&wlmpb.SystemMetric{
					MetricInfo: &cmpb.MetricInfo{
						Type:  "workload.googleapis.com/sap/validation/system",
						Label: "os",
					},
					Value: wlmpb.SystemVariable_OS_NAME_VERSION,
				},
			},
		},
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
				osVendorID:     "debian",
				osVersion:      "11",
				InterfaceAddrsGetter: func() ([]net.Addr, error) {
					ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
					ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
					return []net.Addr{ip1, ip2}, nil
				},
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					if params.Executable == "gcloud" {
						return commandlineexecutor.Result{
							StdOut: "Google Cloud SDK 393.0.0",
							StdErr: "",
						}
					}
					if params.Executable == "gsutil" {
						return commandlineexecutor.Result{
							StdOut: "gsutil version 5.10",
							StdErr: "",
						}
					}
					if params.Executable == "systemctl" {
						return commandlineexecutor.Result{
							StdOut: "active",
							StdErr: "",
						}
					}
					if params.Executable == "sh" {
						return commandlineexecutor.Result{
							StdOut: "true",
							StdErr: "",
						}
					}
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
					}
				},
			},
			wantLabels: map[string]string{
				"instance_name":             "test-instance-name",
				"os":                        "debian-11",
				"agent":                     "sapagent",
				"agent_version":             "1.0",
				"network_ips":               "192.168.0.1,192.168.0.2",
				"gcloud":                    "true",
				"gsutil":                    "true",
				"agent_state":               "running",
				"os_settings":               "",
				"uefi_enabled":              "true",
				"collection_config_version": collectionConfigVersion,
			},
		},
		{
			name: "SystemValidationMetricsEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{},
			},
			wantLabels: map[string]string{},
		},
		{
			name: "SystemVariableUnknown",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "foo",
								},
							},
						},
					},
				},
			},
			wantLabels: map[string]string{
				"foo": "",
			},
		},
		{
			name: "OSNameVersionEmpty",
			params: Parameters{
				Config:         defaultConfiguration,
				WorkloadConfig: systemMetricOSNameVersion,
				osVendorID:     "",
				osVersion:      "",
			},
			wantLabels: map[string]string{
				"os": "-",
			},
		},
		{
			name: "InterfaceAddrsError",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "network_ips",
								},
								Value: wlmpb.SystemVariable_NETWORK_IPS,
							},
						},
					},
				},
				InterfaceAddrsGetter: func() ([]net.Addr, error) {
					return nil, errors.New("Interface Addrs Error")
				},
			},
			wantLabels: map[string]string{
				"network_ips": "",
			},
		},
		{
			name: "OSCommandMetrics_EmptyLabel",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_RHEL,
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputEquals{OutputEquals: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
										},
										IfFalse: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
										},
									},
								},
							},
						},
					},
				},
				osVendorID: "sles",
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "foobar",
						StdErr: "",
					}
				},
			},
			wantLabels: map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := wantSystemMetrics(&timestamppb.Timestamp{Seconds: time.Now().Unix()}, test.wantLabels)
			got := CollectSystemMetricsFromConfig(context.Background(), test.params)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectSystemMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}
