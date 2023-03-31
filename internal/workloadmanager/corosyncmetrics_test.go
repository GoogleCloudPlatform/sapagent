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
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	defaultConfiguration = &cnfpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			InstanceName: "test-instance-name",
			InstanceId:   "test-instance-id",
			Zone:         "test-zone",
			ProjectId:    "test-project-id",
		},
		AgentProperties: &cnfpb.AgentProperties{Version: "1.0"},
	}
	defaultFileReader = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(data)), nil
	})
	fileReaderError = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return nil, errors.New("Could not find file")
	})
	validCSConfigFile = `totem {
	version: 2
	cluster_name: hacluster
	transport: knet
	join: 60
	consensus: 1200
	fail_recv_const: 2500
	max_messages: 20
	token: 20000
	token_retransmits_before_loss_const: 10
	crypto_cipher: aes256
	crypto_hash: sha256
}
quorum {
	provider: corosync_votequorum
	two_node: 1
}`
	invalidCSConfigFile = `totem {
	transport knet
	join:60
	"fail_recv_const": 2500
	# max_messages: 20
	token:
	token_retransmits_before_loss_const: 1 2 3
}`
	defaultCommandRunner = func(cmd string, args ...string) (string, string, error) {
		if len(args) < 2 {
			return "", "", errors.New("not enough arguments")
		}
		cases := map[string]string{
			"totem.token_retransmits_before_loss_const": "5",
			"totem.token":           "10000",
			"totem.consensus":       "600",
			"totem.join":            "30",
			"totem.max_messages":    "10",
			"totem.transport":       "udp",
			"totem.fail_recv_const": "1250",
			"quorum.two_node":       "2",
		}
		return cases[args[1]], "", nil
	}
	commandRunnerError = func(cmd string, args ...string) (string, string, error) {
		cases := map[string]string{
			"totem.token_retransmits_before_loss_const": "1 2 3",
			"totem.token":        "Can't get key",
			"totem.consensus":    " Can't get key",
			"totem.join":         " ",
			"totem.max_messages": "",
		}
		v, ok := cases[args[1]]
		if !ok {
			return "", "", fmt.Errorf("Failed to get value for %s", args[1])
		}
		return v, "", nil
	}
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
	err := protojson.Unmarshal(defaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}
	collectionDefinition.GetWorkloadValidation().GetValidationCorosync().ConfigPath = validCSConfigFile

	tests := []struct {
		name       string
		params     Parameters
		wantLabels map[string]string
	}{
		{
			name: "DefaultCollectionDefinition",
			params: Parameters{
				Config:           defaultConfiguration,
				WorkloadConfig:   collectionDefinition.GetWorkloadValidation(),
				ConfigFileReader: defaultFileReader,
				osVendorID:       "rhel",
				CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) {
					field := args[len(args)-1]
					return fmt.Sprintf("%s = 999", field), "", nil
				},
			},
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
			wantLabels: map[string]string{},
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
				CommandRunnerNoSpace: func(cmd string, args ...string) (string, string, error) {
					return "foobar", "", nil
				},
			},
			wantLabels: map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := createWorkloadMetrics(test.wantLabels, 1.0)
			got := CollectCorosyncMetricsFromConfig(test.params)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectCorosyncMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectCorosyncMetrics(t *testing.T) {
	tests := []struct {
		name       string
		params     Parameters
		csConfig   string
		wantLabels map[string]string
		wantValue  float64
	}{
		{
			name: "windows",
			params: Parameters{
				OSType: "windows",
				Config: defaultConfiguration,
			},
			wantLabels: map[string]string{},
			wantValue:  0.0,
		},
		{
			name: "linux",
			params: Parameters{
				OSType:               "linux",
				Config:               defaultConfiguration,
				ConfigFileReader:     defaultFileReader,
				CommandRunnerNoSpace: defaultCommandRunner,
			},
			csConfig: validCSConfigFile,
			wantLabels: map[string]string{
				"token":                               "20000",
				"token_runtime":                       "10000",
				"token_retransmits_before_loss_const": "10",
				"token_retransmits_before_loss_const_runtime": "5",
				"consensus":               "1200",
				"consensus_runtime":       "600",
				"join":                    "60",
				"join_runtime":            "30",
				"max_messages":            "20",
				"max_messages_runtime":    "10",
				"transport":               "knet",
				"transport_runtime":       "udp",
				"fail_recv_const":         "2500",
				"fail_recv_const_runtime": "1250",
				"two_node":                "1",
				"two_node_runtime":        "2",
			},
			wantValue: 1.0,
		},
		{
			name: "linuxFileReaderError",
			params: Parameters{
				OSType:               "linux",
				Config:               defaultConfiguration,
				ConfigFileReader:     fileReaderError,
				CommandRunnerNoSpace: defaultCommandRunner,
			},
			csConfig: validCSConfigFile,
			wantLabels: map[string]string{
				"token":                               "",
				"token_runtime":                       "10000",
				"token_retransmits_before_loss_const": "",
				"token_retransmits_before_loss_const_runtime": "5",
				"consensus":               "",
				"consensus_runtime":       "600",
				"join":                    "",
				"join_runtime":            "30",
				"max_messages":            "",
				"max_messages_runtime":    "10",
				"transport":               "",
				"transport_runtime":       "udp",
				"fail_recv_const":         "",
				"fail_recv_const_runtime": "1250",
				"two_node":                "",
				"two_node_runtime":        "2",
			},
			wantValue: 1.0,
		},
		{
			name: "linuxFileReaderParseErrors",
			params: Parameters{
				OSType:               "linux",
				Config:               defaultConfiguration,
				ConfigFileReader:     defaultFileReader,
				CommandRunnerNoSpace: defaultCommandRunner,
			},
			csConfig: invalidCSConfigFile,
			wantLabels: map[string]string{
				"token":                               "",
				"token_runtime":                       "10000",
				"token_retransmits_before_loss_const": "1",
				"token_retransmits_before_loss_const_runtime": "5",
				"consensus":               "",
				"consensus_runtime":       "600",
				"join":                    "",
				"join_runtime":            "30",
				"max_messages":            "",
				"max_messages_runtime":    "10",
				"transport":               "",
				"transport_runtime":       "udp",
				"fail_recv_const":         "",
				"fail_recv_const_runtime": "1250",
				"two_node":                "",
				"two_node_runtime":        "2",
			},
			wantValue: 1.0,
		},
		{
			name: "linuxCommandRunnerErrors",
			params: Parameters{
				OSType:               "linux",
				Config:               defaultConfiguration,
				ConfigFileReader:     defaultFileReader,
				CommandRunnerNoSpace: commandRunnerError,
			},
			csConfig: validCSConfigFile,
			wantLabels: map[string]string{
				"token":                               "20000",
				"token_runtime":                       "",
				"token_retransmits_before_loss_const": "10",
				"token_retransmits_before_loss_const_runtime": "3",
				"consensus":               "1200",
				"consensus_runtime":       "",
				"join":                    "60",
				"join_runtime":            "",
				"max_messages":            "20",
				"max_messages_runtime":    "",
				"transport":               "knet",
				"transport_runtime":       "",
				"fail_recv_const":         "2500",
				"fail_recv_const_runtime": "",
				"two_node":                "1",
				"two_node_runtime":        "",
			},
			wantValue: 1.0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cch := make(chan WorkloadMetrics)
			want := createWorkloadMetrics(test.wantLabels, test.wantValue)
			go CollectCorosyncMetrics(test.params, cch, test.csConfig)
			got := <-cch
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&timestamppb.Timestamp{}, "seconds")); diff != "" {
				t.Errorf("CollectCorosyncMetrics() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
