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
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
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

var (
	defaultFileReader = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(data)), nil
	})
	fileReaderError = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return nil, errors.New("Could not find file")
	})
	validCSConfigFile = `totem {
	version:                             2
	cluster_name:                        hacluster
	transport:                           knet
	join:                                60
	consensus:                           1200
	fail_recv_const:                     2500
	max_messages:                        20
	token:                               20000
	token_retransmits_before_loss_const: 10
	crypto_cipher:                       aes256
	crypto_hash:                         sha256
}
quorum {
	provider: corosync_votequorum
	two_node: 1
}`
	createWorkloadMetrics = func(labels map[string]string, value float64) WorkloadMetrics {
		return WorkloadMetrics{
			Metrics: []*monitoringresourcepb.TimeSeries{{
				Metric: &metricpb.Metric{
					Type:   "workload.googleapis.com/sap/validation/corosync",
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
)

func TestCollectCorosyncMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}
	collectionDefinition.GetWorkloadValidation().GetValidationCorosync().ConfigPath = validCSConfigFile

	tests := []struct {
		name         string
		params       Parameters
		pacemakerVal float64
		wantLabels   map[string]string
	}{
		{
			name: "DefaultCollectionDefinition",
			params: Parameters{
				Config:           defaultConfiguration,
				WorkloadConfig:   collectionDefinition.GetWorkloadValidation(),
				ConfigFileReader: defaultFileReader,
				osVendorID:       "rhel",
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					field := params.Args[len(params.Args)-1]
					return commandlineexecutor.Result{
						StdOut: fmt.Sprintf("%s = 999", field),
						StdErr: "",
					}
				},
			},
			pacemakerVal: 1.0,
			wantLabels: map[string]string{
				"token":                               "20000",
				"token_retransmits_before_loss_const": "10",
				"consensus":                           "1200",
				"join":                                "60",
				"max_messages":                        "20",
				"transport":                           "knet",
				"fail_recv_const":                     "2500",
				"two_node":                            "1",
				"token_runtime":                       "999",
				"token_retransmits_before_loss_const_runtime": "999",
				"consensus_runtime":                           "999",
				"join_runtime":                                "999",
				"max_messages_runtime":                        "999",
				"transport_runtime":                           "999",
				"fail_recv_const_runtime":                     "999",
				"two_node_runtime":                            "999",
			},
		},
		{
			name: "CorosyncMetricsEmpty",
			params: Parameters{
				Config:           defaultConfiguration,
				WorkloadConfig:   &wlmpb.WorkloadValidation{},
				ConfigFileReader: defaultFileReader,
			},
			pacemakerVal: 1.0,
			wantLabels:   map[string]string{},
		},
		{
			name: "CorosyncConfigFileReadError",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigPath:    validCSConfigFile,
						ConfigMetrics: collectionDefinition.GetWorkloadValidation().GetValidationCorosync().GetConfigMetrics(),
					},
				},
				ConfigFileReader: fileReaderError,
			},
			pacemakerVal: 1.0,
			wantLabels: map[string]string{
				"token":                               "",
				"token_retransmits_before_loss_const": "",
				"consensus":                           "",
				"join":                                "",
				"max_messages":                        "",
				"transport":                           "",
				"fail_recv_const":                     "",
				"two_node":                            "",
			},
		},
		{
			name: "OSCommandMetrics_EmptyLabel",
			params: Parameters{
				Config: cnf,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/corosync",
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
			},
			pacemakerVal: 1.0,
			wantLabels:   map[string]string{},
		},
		{
			name: "PacemakerInactive",
			params: Parameters{
				Config:           defaultConfiguration,
				WorkloadConfig:   collectionDefinition.GetWorkloadValidation(),
				ConfigFileReader: defaultFileReader,
				osVendorID:       "rhel",
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					field := params.Args[len(params.Args)-1]
					return commandlineexecutor.Result{
						StdOut: fmt.Sprintf("%s = 999", field),
						StdErr: "",
					}
				},
			},
			pacemakerVal: 0.0,
			wantLabels:   map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := createWorkloadMetrics(test.wantLabels, test.pacemakerVal)
			got := CollectCorosyncMetricsFromConfig(context.Background(), test.params, test.pacemakerVal)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectCorosyncMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}
