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

	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	dwpb "github.com/GoogleCloudPlatform/sapagent/protos/datawarehouse"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wlmfake "github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
)

var (
	defaultRemoteCollectionStdout = `{"metric":{"type":"workload.googleapis.com/sap/validation/system","labels":{"agent":"sapagent","instance_name":"test-instance","os":"sles-15"}},"points":[{"interval":{},"value":{"double_value":1}}],"resource":{"type":"gce_instance","labels":{"instance_id":"instanceId","project_id":"projectId","zone":"some-region-zone"}},"metricKind":"GAUGE"}`
	defaultRemoteInstance         = &cfgpb.RemoteCollectionInstance{
		ProjectId:    "projectId",
		Zone:         "some-region-zone",
		InstanceId:   "instanceId",
		InstanceName: "instanceName",
	}
	defaultTimeSeries = createTimeSeries("workload.googleapis.com/sap/validation/system", map[string]string{"agent": "sapagent", "instance_name": "test-instance", "os": "sles-15"}, 1, &cfgpb.Configuration{
		BareMetal: false,
		CloudProperties: &iipb.CloudProperties{
			ProjectId:    "projectId",
			Zone:         "some-region-zone",
			Region:       "some-region",
			InstanceId:   "instanceId",
			InstanceName: "instanceName",
		},
	})
	defaultWLMInterface = func() *wlmfake.TestWLM {
		return &wlmfake.TestWLM{
			WriteInsightArgs: []wlmfake.WriteInsightArgs{{
				Project:  "projectId",
				Location: "some-region",
				Req: &dwpb.WriteInsightRequest{
					Insight: &dwpb.Insight{
						InstanceId: "instanceId",
						SapValidation: &dwpb.SapValidation{
							ProjectId: "projectId",
							Zone:      "some-region-zone",
							ValidationDetails: []*dwpb.SapValidation_ValidationDetail{{
								Details: map[string]string{
									"agent":         "sapagent",
									"instance_name": "test-instance",
									"os":            "sles-15",
								},
								SapValidationType: dwpb.SapValidation_SYSTEM,
								IsPresent:         true,
							}},
						},
					},
				},
			}},
			WriteInsightErrs: []error{nil},
		}
	}
)

func TestCollectMetricsToJSON(t *testing.T) {
	c := &cfgpb.Configuration{
		CollectionConfiguration: &cfgpb.CollectionConfiguration{
			CollectWorkloadValidationMetrics: wpb.Bool(true),
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
		Discovery: &fakeDiscoveryInterface{
			instances: &sapb.SAPInstances{Instances: []*sapb.SAPInstance{}},
		},
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
		wlmInterface *wlmfake.TestWLM
		want         int
	}{
		{
			name: "returnsZeroWhenNotConfigured",
			config: &cfgpb.Configuration{
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: wpb.Bool(false),
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						ConcurrentCollections: 1,
					},
				},
			},
			wlmInterface: defaultWLMInterface(),
			want:         0,
		},
		{
			name: "returnsMetricsSentWhenConfiguredGcloud",
			config: &cfgpb.Configuration{
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: wpb.Bool(false),
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						RemoteCollectionGcloud:    &cfgpb.RemoteCollectionGcloud{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{defaultRemoteInstance},
						ConcurrentCollections:     1,
					},
				},
			},
			wlmInterface: defaultWLMInterface(),
			want:         1,
		},
		{
			name: "returnsMetricsSentWhenConfiguredSSH",
			config: &cfgpb.Configuration{
				CollectionConfiguration: &cfgpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: wpb.Bool(false),
					WorkloadValidationRemoteCollection: &cfgpb.WorkloadValidationRemoteCollection{
						RemoteCollectionSsh:       &cfgpb.RemoteCollectionSsh{},
						RemoteCollectionInstances: []*cfgpb.RemoteCollectionInstance{defaultRemoteInstance},
						ConcurrentCollections:     1,
					},
				},
			},
			wlmInterface: defaultWLMInterface(),
			want:         1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.wlmInterface.T = t
			p := Parameters{
				Config: test.config,
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: defaultRemoteCollectionStdout,
						StdErr: "",
						Error:  nil,
					}
				},
				Exists:            func(string) bool { return true },
				ConfigFileReader:  func(data string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil },
				OSStatReader:      func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				BackOffs:          defaultBackOffIntervals,
				WLMService:        test.wlmInterface,
			}
			got := collectAndSendRemoteMetrics(context.Background(), p)
			if got != test.want {
				t.Errorf("collectAndSendRemoteMetrics() unexpected metrics sent, got %d want %d", got, test.want)
			}
		})
	}
}

func TestRemoteCollectGcloud(t *testing.T) {
	tests := []struct {
		name       string
		cmdExists  func(string) bool
		cmdExecute func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		config     *cfgpb.WorkloadValidationRemoteCollection
		instance   *cfgpb.RemoteCollectionInstance
		want       WorkloadMetrics
	}{
		{
			name:      "gcloudCommandNotExists",
			cmdExists: func(string) bool { return false },
			cmdExecute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "SCPWorkloadValidationConfigError",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "/tmp/workload-validation.json") {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("SCP error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "SCPAgentBinaryError",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, agentBinary) {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("SCP error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "RemoteCollectionError",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "--command") {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("sapagent error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "RemoteCollectionOutputError",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "--command") {
					return commandlineexecutor.Result{
						StdOut: "ERROR: remote collection error",
						StdErr: "",
						Error:  nil,
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "RemoteCollectionOutputInvalid",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "--command") {
					return commandlineexecutor.Result{
						StdOut: "Invalid output",
						StdErr: "",
						Error:  nil,
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections:  1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name:      "Success",
			cmdExists: func(string) bool { return true },
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionGcloud: &cfgpb.RemoteCollectionGcloud{
					SshUsername:      "username",
					TunnelThroughIap: true,
					UseInternalIp:    true,
					GcloudArgs:       "--args",
				},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{Metrics: defaultTimeSeries},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch := make(chan WorkloadMetrics)
			opts := collectOptions{
				exists:     test.cmdExists,
				execute:    test.cmdExecute,
				configPath: "/tmp/workload-validation.json",
				rc:         test.config,
				i:          test.instance,
				wm:         ch,
			}
			go collectRemoteGcloud(context.Background(), opts)
			got := <-ch
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("collectRemoteGcloud() unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemoteCollectSSH(t *testing.T) {
	tests := []struct {
		name       string
		cmdExecute func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		config     *cfgpb.WorkloadValidationRemoteCollection
		instance   *cfgpb.RemoteCollectionInstance
		want       WorkloadMetrics
	}{
		{
			name: "SCPWorkloadValidationConfigError",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "/tmp/workload-validation.json") {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("SCP error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name: "SCPAgentBinaryError",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, agentBinary) {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("SCP error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name: "RemoteCollectionError",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "remote") {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
						Error:  errors.New("sapagent error"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name: "RemoteCollectionOutputError",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if slices.Contains(params.Args, "remote") {
					return commandlineexecutor.Result{
						StdOut: "ERROR: remote collection error",
						StdErr: "",
						Error:  nil,
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name: "RemoteCollectionOutputInvalid",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "Invalid output",
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{},
		},
		{
			name: "Success",
			cmdExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultRemoteCollectionStdout,
					StdErr: "",
					Error:  nil,
				}
			},
			config: &cfgpb.WorkloadValidationRemoteCollection{
				ConcurrentCollections: 1,
				RemoteCollectionSsh:   &cfgpb.RemoteCollectionSsh{},
			},
			instance: defaultRemoteInstance,
			want:     WorkloadMetrics{Metrics: defaultTimeSeries},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch := make(chan WorkloadMetrics)
			opts := collectOptions{
				execute:    test.cmdExecute,
				configPath: "/tmp/workload-validation.json",
				rc:         test.config,
				i:          test.instance,
				wm:         ch,
			}
			go collectRemoteSSH(context.Background(), opts)
			got := <-ch
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("collectRemoteSSH() unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
