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
	"io"
	"os"
	"strings"
	"testing"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/slices"
	workloadmanager "google.golang.org/api/workloadmanager/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

func TestCollectMetricsToJSON(t *testing.T) {
	c := &cfgpb.Configuration{
		CollectionConfiguration: &cfgpb.CollectionConfiguration{
			CollectWorkloadValidationMetrics: false,
		},
	}
	p := Parameters{
		Config: c,
		Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		ConfigFileReader: func(data string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil },
		OSStatReader:     func(data string) (os.FileInfo, error) { return nil, nil },
		BackOffs:         defaultBackOffIntervals,
		osVendorID:       "test-os",
		osVersion:        "version",
		sapApplications:  &sapb.SAPInstances{Instances: []*sapb.SAPInstance{}},
	}
	got := strings.TrimSpace(CollectMetricsToJSON(context.Background(), p))
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Errorf("CollectMetricsToJSON returned incorrect JSON, does not start with '{' and end with '}' got: %s", got)
	}
}

func TestParseRemoteJSON(t *testing.T) {
	tests := []struct {
		name        string
		want        []*mrpb.TimeSeries
		output      string
		expectError bool
	}{
		{
			name: "succeedsWithValidOutput",
			want: append([]*mrpb.TimeSeries{}, &mrpb.TimeSeries{
				Metric: &mpb.Metric{
					Type:   "workload.googleapis.com/sap/validation/system",
					Labels: map[string]string{"agent": "gcagent", "instance_name": "test-instance", "os": "\"sles\"-\"15\""},
				},
				Resource: &mrespb.MonitoredResource{
					Type:   "gce_instance",
					Labels: map[string]string{"instance_id": "5555"},
				},
				MetricKind: mpb.MetricDescriptor_GAUGE,
			}),
			output:      `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			expectError: false,
		},
		{
			name:        "succeedsWithEmpty",
			want:        []*mrpb.TimeSeries{},
			output:      "",
			expectError: false,
		},
		{
			name:        "failsWithBadInput",
			want:        []*mrpb.TimeSeries{},
			output:      "somebadstuff",
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := []*mrpb.TimeSeries{}
			err := parseRemoteJSON(test.output, &got)
			if !test.expectError && err != nil {
				t.Errorf("parseRemoteJSON returned an error: %s", err)
			}
			diff := cmp.Diff(test.want, got, protocmp.Transform())
			if !test.expectError && diff != "" {
				t.Errorf("parseRemoteJSON did not return the expected values (-want +got):\n%s", diff)
			}
		})
	}

}

func TestAppendCommonGcloudArgs(t *testing.T) {
	tests := []struct {
		name       string
		wantInArgs []string
	}{
		{
			name:       "appendsProjectAndZone",
			wantInArgs: []string{"--project", "--zone", "--tunnel-through-iap", "--internal-ip", "additionalargs"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := []string{}
			rc := &cfgpb.WorkloadValidationRemoteCollection{
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{
					UseInternalIp:    true,
					TunnelThroughIap: true,
					GcloudArgs:       "additionalargs",
				},
			}
			got := appendCommonGcloudArgs(args, rc, &cfgpb.RemoteCollectionInstance{
				ProjectId: "projectId",
				Zone:      "some-region-zone",
			})
			for _, want := range test.wantInArgs {
				if !slices.Contains(got, want) {
					t.Errorf("Did not get all of the args expected, want: %v, got: %v", want, got)
				}
			}
		})
	}
}

func TestAppendSSHArgs(t *testing.T) {
	tests := []struct {
		name       string
		isScp      bool
		wantInArgs []string
	}{
		{
			name:       "appendsSSHArgumentsScp",
			isScp:      true,
			wantInArgs: []string{"-i", "keypath", "/usr/bin/google_cloud_sap_agent", "username" + "@" + "sshHostAddress" + ":" + "/tmp/google_cloud_sap_agent"},
		},
		{
			name:       "appendsSSHArgumentsSsh",
			isScp:      false,
			wantInArgs: []string{"-i", "keypath", "username" + "@" + "sshHostAddress"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := []string{}
			rc := &cfgpb.WorkloadValidationRemoteCollection{
				RemoteCollectionSsh: &cfgpb.RemoteCollectionSsh{
					SshUsername:       "username",
					SshPrivateKeyPath: "keypath",
				},
			}
			instance := &cfgpb.RemoteCollectionInstance{
				ProjectId:      "projectId",
				InstanceId:     "instanceId",
				InstanceName:   "instanceName",
				Zone:           "some-region-zone",
				SshHostAddress: "sshHostAddress",
			}

			got := appendSSHArgs(args, rc, instance, test.isScp)
			for _, want := range test.wantInArgs {
				if !slices.Contains(got, want) {
					t.Errorf("Did not get all of the args expected, want: %v, got: %v", want, got)
				}
			}
		})
	}
}

func TestCollectAndSendRemoteMetrics(t *testing.T) {
	tests := []struct {
		name         string
		config       *cfgpb.Configuration
		execOutput   string
		wlmInterface *testWLMInterface
		wantCount    int
		execError    error
		cmdExists    bool
	}{
		{
			name: "returnsZeroWhenNotConfigured",
			config: &cfgpb.Configuration{
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections: 1,
					},
				},
			},
			execOutput:   "",
			wlmInterface: &testWLMInterface{},
			wantCount:    0,
			execError:    nil,
			cmdExists:    true,
		},
		{
			name: "returnsSentWhenConfigured",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections:  1,
						RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput: `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			wlmInterface: &testWLMInterface{
				WriteInsightArgs: []WriteInsightArgs{{
					Project:  "projectId",
					Location: "some-region",
					Req: &workloadmanager.WriteInsightRequest{
						Insight: &workloadmanager.Insight{
							InstanceId: "instanceId",
							SapValidation: &workloadmanager.SapValidation{
								ValidationDetails: []*workloadmanager.SapValidationValidationDetail{{
									Details: map[string]string{
										"agent":         "gcagent",
										"instance_name": "test-instance",
										"os":            `"sles"-"15"`,
									},
									SapValidationType: "SYSTEM",
								}},
							},
						},
					},
				}},
				WriteInsightErrs: []error{nil},
			},
			wantCount: 1,
			execError: nil,
			cmdExists: true,
		},
		{
			name: "returnsZeroWithErrorFromRemote",
			config: &cfgpb.Configuration{
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections:  1,
						RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput:   "ERROR something did not work",
			wlmInterface: &testWLMInterface{},
			wantCount:    0,
			execError:    nil,
			cmdExists:    true,
		},
		{
			name: "returnsZeroWithErrorExec",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections:  1,
						RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput:   `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			wlmInterface: &testWLMInterface{},
			wantCount:    0,
			execError:    errors.New("Error executing"),
			cmdExists:    true,
		},
		{
			name: "gcloudNotFound",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections:  1,
						RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput:   `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			wlmInterface: &testWLMInterface{},
			wantCount:    0,
			execError:    nil,
			cmdExists:    false,
		},
		{
			name: "returnsSentWhenConfiguredSSH",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections: 1,
						RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput: `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			wlmInterface: &testWLMInterface{
				WriteInsightArgs: []WriteInsightArgs{{
					Project:  "projectId",
					Location: "some-region",
					Req: &workloadmanager.WriteInsightRequest{
						Insight: &workloadmanager.Insight{
							InstanceId: "instanceId",
							SapValidation: &workloadmanager.SapValidation{
								ValidationDetails: []*workloadmanager.SapValidationValidationDetail{{
									SapValidationType: "SYSTEM",
									Details: map[string]string{
										"agent":         "gcagent",
										"instance_name": "test-instance",
										"os":            `"sles"-"15"`,
									},
								}},
							},
						},
					},
				}},
				WriteInsightErrs: []error{nil},
			},
			wantCount: 1,
			execError: nil,
		},
		{
			name: "returnsZeroWithErrorFromRemote",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections: 1,
						RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput: "ERROR something did not work",
			wlmInterface: &testWLMInterface{
				WriteInsightArgs: []WriteInsightArgs{{
					Project:  "projectId",
					Location: "some-region",
					Req: &workloadmanager.WriteInsightRequest{
						Insight: &workloadmanager.Insight{
							InstanceId: "instanceId",
							SapValidation: &workloadmanager.SapValidation{
								ValidationDetails: []*workloadmanager.SapValidationValidationDetail{{
									SapValidationType: "SYSTEM",
									Details:           map[string]string{},
								}},
							},
						},
					},
				}},
				WriteInsightErrs: []error{nil},
			},
			wantCount: 0,
			execError: nil,
		},
		{
			name: "returnsZeroWithErrorExec",
			config: &cfgpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					Zone: "some-region-zone",
				},
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: false,
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections: 1,
						RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{
							&cfgpb.RemoteCollectionInstance{
								ProjectId:    "projectId",
								Zone:         "some-region-zone",
								InstanceId:   "instanceId",
								InstanceName: "instanceName",
							},
						},
					},
				},
			},
			execOutput:   `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"gcagent","instance_name":"test-instance","os":"\"sles\"-\"15\""}},"resource":{"type":"gce_instance","labels":{"instance_id":"5555"}},"metricKind":"GAUGE"}`,
			wlmInterface: &testWLMInterface{},
			wantCount:    0,
			execError:    errors.New("Error executing"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.wlmInterface.T = t
			p := Parameters{
				Config: test.config,
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: test.execOutput,
						StdErr: "",
						Error:  test.execError,
					}
				},
				Exists:            func(string) bool { return test.cmdExists },
				ConfigFileReader:  func(data string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil },
				OSStatReader:      func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				BackOffs:          defaultBackOffIntervals,
				WLMService:        test.wlmInterface,
			}

			want := test.wantCount
			got := collectAndSendRemoteMetrics(context.Background(), p)
			if got != want {
				t.Errorf("Did not collect and send the expected number of metrics, want: %d, got: %d", want, got)
			}

		})
	}
}
