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

package hana

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var (
	defaultSAPInstance = &sapb.SAPInstance{
		Sapsid:         "TST",
		InstanceNumber: "00",
		ServiceName:    "test-service",
		Type:           sapb.InstanceType_HANA,
		Site:           sapb.InstanceSite_HANA_PRIMARY,
		HanaHaMembers:  []string{"test-instance-1", "test-instance-2"},
		HanaDbUser:     "test-user",
		HanaDbPassword: "test-pass",
	}

	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       false,
			ProcessMetricsFrequency:     5,
			ProcessMetricsSendFrequency: 60,
		},
		CloudProperties: &iipb.CloudProperties{
			ProjectId:        "test-project",
			InstanceId:       "test-instance",
			Zone:             "test-zone",
			InstanceName:     "test-instance",
			Image:            "test-image",
			NumericProjectId: "123456",
		},
	}

	defaultInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultSAPInstance,
	}

	defaultAPIInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultSAPInstance,
	}

	defaultSapControlOutput = `OK
		0 name: hdbdaemon
		0 dispstatus: GREEN
		0 pid: 111
		1 name: hdbcompileserver
		1 dispstatus: GREEN
		1 pid: 222
		2 name: hdbindexserver
		2 dispstatus: GREEN
		2 pid: 333
		3 name: hdbnameserver
		3 dispstatus: GREEN
		3 pid: 444
		4 name: hdbpreprocessor
		4 dispstatus: GREEN
		4 pid: 555
		5 name: hdbwebdispatcher
		5 dispstatus: GREEN
		5 pid: 666
		6 name: hdbxsengine
		6 dispstatus: GREEN
		6 pid: 777`
)

type fakeRunner struct {
	stdOut, stdErr string
	exitCode       int
	err            error
}

func (f *fakeRunner) RunWithEnv() (string, string, int, error) {
	return f.stdOut, f.stdErr, f.exitCode, f.err
}

func TestCollectHANAServiceMetrics(t *testing.T) {
	tests := []struct {
		name               string
		fakeClient         sapcontrolclienttest.Fake
		wantMetricCount    int
		wantErr            error
		instanceProperties *InstanceProperties
	}{
		{
			name: "SuccessWebmethod",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbdaemon", "SAPControl-GREEN", 9609},
					{"hdbcompileserver", "SAPControl-GREEN", 9972},
					{"hdbindexserver", "SAPControl-GREEN", 10013},
					{"hdbnameserver", "SAPControl-GREEN", 9642},
					{"hdbpreprocessor", "SAPControl-GREEN", 9975},
					{"hdbwebdispatcher", "SAPControl-GREEN", 666},
					{"hdbxsengine", "SAPControl-GREEN", 777},
				},
			},
			wantMetricCount:    7,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name:               "FailureWebmethodGetProcessList",
			fakeClient:         sapcontrolclienttest.Fake{ErrGetProcessList: cmpopts.AnyError},
			wantMetricCount:    0,
			wantErr:            cmpopts.AnyError,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "EmptyProcessList",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{},
			},
			wantMetricCount:    0,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "MetricsSkipped",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbdaemon", "SAPControl-GREEN", 9609},
					{"hdbcompileserver", "SAPControl-GREEN", 9972},
					{"hdbindexserver", "SAPControl-GREEN", 10013},
					{"hdbnameserver", "SAPControl-GREEN", 9642},
					{"hdbpreprocessor", "SAPControl-GREEN", 9975},
					{"hdbwebdispatcher", "SAPControl-GREEN", 666},
				},
			},
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultSAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{servicePath},
					},
				},
				SkippedMetrics: map[string]bool{servicePath: true},
			},
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics, err := collectHANAServiceMetrics(context.Background(), test.instanceProperties, test.fakeClient)
			if len(metrics) != test.wantMetricCount {
				t.Errorf("collectHANAServiceMetrics() metric count mismatch, got: %v want: %v.", len(metrics), test.wantMetricCount)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectHANAServiceMetrics() gotErr: %v wantErr: %v.", err, test.wantErr)
			}
		})
	}
}

func TestRunHANAQuery(t *testing.T) {
	successOutput := `| D |
	| - |
	| X |
	1 row selected (overall time 1187 usec; server time 509 usec)`

	tests := []struct {
		name           string
		fakeExec       commandlineexecutor.Execute
		ip             *InstanceProperties
		wantQueryState queryState
		wantErr        error
	}{
		{
			name: "Success",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   successOutput,
					ExitCode: 0,
				}
			},
			ip: defaultInstanceProperties,
			wantQueryState: queryState{
				state:       0,
				overallTime: 1187,
				serverTime:  509,
			},
			wantErr: nil,
		},
		{
			name: "NonZeroState",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   successOutput,
					ExitCode: 100,
				}
			},
			ip: defaultInstanceProperties,
			wantQueryState: queryState{
				state:       100,
				overallTime: 1187,
				serverTime:  509,
			},
			wantErr: nil,
		},
		{
			name: "ExitCodeZeroWithError",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "(overall time 10 usec; server time 10 usec)",
					StdErr:   "Not Found.",
					ExitCode: 0,
					Error:    cmpopts.AnyError,
				}
			},
			ip: defaultInstanceProperties,
			wantQueryState: queryState{
				state:       0,
				overallTime: 10,
				serverTime:  10,
			},
			wantErr: nil,
		},
		{
			name: "ParseOverallTimeFailure",
			ip:   defaultInstanceProperties,
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "(overall time invalid-int; server time 509 usec).",
					ExitCode: 0,
				}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseServerTimeFailure",
			ip:   defaultInstanceProperties,
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "(overall time 1187 usec; server time invalid-int)",
					ExitCode: 128,
				}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "IntegerOverflow",
			ip:   defaultInstanceProperties,
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "(overall time 100000000000000000000 usec; server time 10 usec)",
					ExitCode: 0,
				}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AuthenticationFailed",
			ip:   defaultInstanceProperties,
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdErr:   "* 10: authentication failed SQLSTATE: 28000\n",
					ExitCode: 3,
				}
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotQueryState, gotErr := runHANAQuery(context.Background(), test.ip, test.fakeExec)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("runHANAQuery(), gotErr: %v wantErr: %v.", gotErr, test.wantErr)
			}

			if diff := cmp.Diff(test.wantQueryState, gotQueryState, cmp.AllowUnexported(queryState{})); diff != "" {
				t.Errorf("runHANAQuery(), diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectHANAQueryMetrics(t *testing.T) {
	fakeExec := func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut:   "1 row selected (overall time 1187 usec; server time 509 usec)",
			ExitCode: 0,
		}
	}
	got, _ := collectHANAQueryMetrics(context.Background(), defaultInstanceProperties, fakeExec)
	if len(got) != 3 {
		t.Errorf("collectHANAQueryMetrics(), got: %d want: 3.", len(got))
	}
}

func TestCollectHANAQueryMetricsWithMaxFailCounts(t *testing.T) {
	fakeExec := func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdErr:   "* 10: authentication failed SQLSTATE: 28000\n",
			ExitCode: 3,
		}
	}
	ip := &InstanceProperties{
		Config:             defaultConfig,
		SAPInstance:        defaultSAPInstance,
		HANAQueryFailCount: 0,
	}

	for i := 0; i < 3; i++ {
		got, _ := collectHANAQueryMetrics(context.Background(), ip, fakeExec)
		switch i {
		case 0, 1:
			ts := got[0].GetPoints()[0].GetInterval().GetEndTime()
			want := []*mrpb.TimeSeries{createMetrics(ip, queryStatePath, nil, ts, 1)}
			if cmp.Diff(got[0], want[0], protocmp.Transform()) != "" {
				t.Errorf("collectHANAQueryMetrics(), got: %v want: %v.", got, want)
			}
		default:
			if got != nil {
				t.Errorf("collectHANAQueryMetrics(), got: %v want: nil.", got)
			}
		}
	}
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name       string
		properties *InstanceProperties
		wantCount  int
		wantErr    error
	}{
		{
			name: "MetricCountTest",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
					HanaDbUser:     "test-user",
					HanaDbPassword: "test-pass",
				},
				SkippedMetrics: map[string]bool{
					servicePath: true,
				},
			},
			wantCount: 1, // Without HANA setup in unit test ENV, only query/state metric is generated.
			wantErr:   nil,
		},
		{
			name: "NoHANADBUserAndKey",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
				},
				SkippedMetrics: map[string]bool{
					servicePath: true,
				},
			},
			wantCount: 0, // Query state metric not generated without credentials.
		},
		{
			name: "NoHANADBUser",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
					HanaDbPassword: "test-pass",
				},
				SkippedMetrics: map[string]bool{
					servicePath: true,
				},
			},
			wantCount: 0, // Query state metric not generated without credentials.
		},
		{
			name: "NoHANADBPassword",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
					HanaDbUser:     "test-user",
				},
				SkippedMetrics: map[string]bool{
					servicePath: true,
				},
			},
			wantCount: 0, // Query state metric not generated without credentials.
		},
		{
			name: "HANASecondaryNode",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
					HanaDbUser:     "test-user",
					HanaDbPassword: "test-pass",
					Site:           sapb.InstanceSite_HANA_SECONDARY,
				},
				SkippedMetrics: map[string]bool{
					servicePath: true,
				},
			},
			wantCount: 0, // Query state metric not generated for HANA secondary.
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.properties.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect(), got: %d want: %d.", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Collect(), gotErr: %v wantErr: %v.", gotErr, test.wantErr)
			}
		})
	}
}
