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

package fastmovingmetrics

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

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
			CollectProcessMetrics:   false,
			ProcessMetricsFrequency: 5,
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

	defaultSapControlOutputAppSrvAPI = sapcontrolclienttest.Fake{
		Processes: []sapcontrolclient.OSProcess{
			sapcontrolclient.OSProcess{
				Name:       "msg_server",
				Dispstatus: "SAPControl-GREEN",
				Pid:        111,
			},
			sapcontrolclient.OSProcess{
				Name:       "enserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        222,
			},
			sapcontrolclient.OSProcess{
				Name:       "enrepserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        333,
			},
			sapcontrolclient.OSProcess{
				Name:       "disp+work",
				Dispstatus: "SAPControl-GREEN",
				Pid:        444,
			},
			sapcontrolclient.OSProcess{
				Name:       "gwrd",
				Dispstatus: "SAPControl-GREEN",
				Pid:        555,
			},
			sapcontrolclient.OSProcess{
				Name:       "icman",
				Dispstatus: "SAPControl-GREEN",
				Pid:        666,
			},
		},
	}

	defaultSapControlOutputJavaAPI = sapcontrolclienttest.Fake{
		Processes: []sapcontrolclient.OSProcess{
			sapcontrolclient.OSProcess{
				Name:       "msg_server",
				Dispstatus: "SAPControl-GREEN",
				Pid:        111,
			},
			sapcontrolclient.OSProcess{
				Name:       "enserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        222,
			},
			sapcontrolclient.OSProcess{
				Name:       "enrepserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        333,
			},
			sapcontrolclient.OSProcess{
				Name:       "jstart",
				Dispstatus: "SAPControl-GREEN",
				Pid:        444,
			},
			sapcontrolclient.OSProcess{
				Name:       "jcontrol",
				Dispstatus: "SAPControl-GREEN",
				Pid:        555,
			},
		},
	}
)

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestHaAvailabilityValue(t *testing.T) {
	tests := []struct {
		name              string
		sapControlResult  int64
		replicationStatus int64
		want              int64
	}{
		{
			name:              "PrimaryOnlineReplicationRunning",
			sapControlResult:  sapControlAllProcessesRunning,
			replicationStatus: replicationActive,
			want:              primaryOnlineReplicationRunning,
		},
		{
			name:              "PrimaryOnlineReplicationNotFunctional",
			sapControlResult:  sapControlAllProcessesRunning,
			replicationStatus: replicationOff,
			want:              primaryOnlineReplicationNotFunctional,
		},
		{
			name:              "PrimaryHasError",
			sapControlResult:  sapControlAllProcessesStopped,
			replicationStatus: replicationSyncing,
			want:              primaryHasError,
		},
		{
			name:              "UnknownState",
			sapControlResult:  0,
			replicationStatus: 0,
			want:              unknownState,
		},
		{
			name:              "ReplicationActivePrimaryHasError",
			sapControlResult:  sapControlAllProcessesStopped,
			replicationStatus: replicationActive,
			want:              primaryHasError,
		},
		{
			name:              "ReplicationUnknownPrimaryOnline",
			sapControlResult:  sapControlAllProcessesRunning,
			replicationStatus: replicationUnknown,
			want:              primaryOnlineReplicationNotFunctional,
		},
		{
			name:              "ReplicationUnknownPrimaryError",
			sapControlResult:  sapControlAllProcessesStopped,
			replicationStatus: replicationUnknown,
			want:              primaryHasError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			got := haAvailabilityValue(defaultInstanceProperties, test.sapControlResult, test.replicationStatus)
			if got != test.want {
				t.Errorf("haAvailabilityValue(), got: %d want: %d.", got, test.want)
			}
		})
	}
}

func TestHAAvailabilityValueSecondary(t *testing.T) {
	p := &InstanceProperties{
		Config: defaultConfig,
		SAPInstance: &sapb.SAPInstance{
			Sapsid:         "TST",
			InstanceNumber: "00",
			Type:           sapb.InstanceType_HANA,
			Site:           sapb.InstanceSite_HANA_SECONDARY,
			HanaHaMembers:  []string{"test-instance-1", "test-instance-2"},
		},
	}
	got := haAvailabilityValue(p, sapControlAllProcessesStopped, replicationUnknown)
	want := currentNodeSecondary
	if got != want {
		t.Errorf("haAvailabilityValue(), got: %d want: %d.", got, want)
	}
}

func TestNWAvailabilityValue(t *testing.T) {
	tests := []struct {
		name             string
		fakeClient       sapcontrolclienttest.Fake
		wantAvailability int64
	}{
		{
			name: "SapControlFailsTwoProcesses",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "msg_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
					sapcontrolclient.OSProcess{
						Name:       "enserver",
						Dispstatus: "SAPControl-RED",
						Pid:        222,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name:             "SapControlSucceedsAppSrv",
			fakeClient:       defaultSapControlOutputAppSrvAPI,
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name:             "SapControlSucceedsJava",
			fakeClient:       defaultSapControlOutputJavaAPI,
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSuccessMsg",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "msg_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsEnServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enserver",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlFailEnRepServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enrepserver",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSuccessEnRepServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enrepserver",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsAppSrv",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "gwrd",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlFailsJava",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "jcontrol",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSuccessJava",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "jstart",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSuccessAppSrv",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "icman",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "InvalidProcess",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "invalidproc",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSuccessEnqReplicator",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_replicator",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsEnqReplicator",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_replicator",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSuccessEnqServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsEnqServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_server",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "WebDispatctherGrey",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "sapwebdisp",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "gwrdGrey",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "gwrd",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sc := &sapcontrol.Properties{
				Instance: defaultSAPInstance,
			}
			procs, err := sc.GetProcessList(context.Background(), test.fakeClient)
			if err != nil {
				t.Errorf("ProcessList() failed with: %v.", err)
			}
			gotAvailability := collectNWAvailability(defaultInstanceProperties, procs)
			if gotAvailability != test.wantAvailability {
				t.Errorf("Failure in readNetWeaverProcessStatus(), gotAvailability: %d wantAvailability: %d.",
					gotAvailability, test.wantAvailability)
			}
		})
	}
}

func TestHANAAvailabilityMetrics(t *testing.T) {
	tests := []struct {
		name          string
		testProcesses map[int]*sapcontrol.ProcessStatus
		wantValue     int64
	}{
		{
			name: "AllProcessesGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbdaemon", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
			},
			wantValue: systemAllProcessesGreen,
		},
		{
			name: "ThreeProcessGreenOneProcessNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbpreprosessor", IsGreen: true},
				3: &sapcontrol.ProcessStatus{Name: "hdbwebdispatcher", IsGreen: false},
			},
			wantValue: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerAndNameServerGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbxsengine", IsGreen: false},
			},
			wantValue: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
			},
			wantValue: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "NameServerNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
			},
			wantValue: systemAtLeastOneProcessNotGreen,
		},
		{
			name:          "ProcessMapEmpty",
			testProcesses: map[int]*sapcontrol.ProcessStatus{},
			wantValue:     systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerAndNameServerRED",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: false},
				2: &sapcontrol.ProcessStatus{Name: "hdbxsengine", IsGreen: true},
			},
			wantValue: systemAtLeastOneProcessNotGreen,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hanaAvailability(defaultInstanceProperties, test.testProcesses)
			if got != test.wantValue {
				t.Errorf("hanaAvailability() returned unexpected value, got=%d, want=%d",
					got, test.wantValue)
			}
		})
	}
}

func TestCollectHANAAvailabilityMetrics(t *testing.T) {
	tests := []struct {
		name       string
		ip         *InstanceProperties
		exec       commandlineexecutor.Execute
		fakeClient sapcontrolclienttest.Fake
		wantCount  int
		wantErr    error
	}{
		{
			name: "ErrorInMetricsCollection",
			ip:   defaultInstanceProperties,
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			fakeClient: sapcontrolclienttest.Fake{
				ErrGetProcessList: cmpopts.AnyError,
			},
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
		},
		{
			name: "SuccessHANAAvailability",
			ip: &InstanceProperties{SAPInstance: defaultSAPInstance, Config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{ProcessMetricsToSkip: []string{pmHAAvailabilityPath}},
				CloudProperties:         defaultConfig.GetCloudProperties()},
				SkippedMetrics: map[string]bool{pmHAAvailabilityPath: true},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{ExitCode: 1, Error: cmpopts.AnyError}
			},
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{Name: "hdbdaemon", Dispstatus: "SAPControl-GREEN", Pid: 111},
			}},
			wantCount: 1,
		},
		{
			name: "SkipMetrics",
			ip: &InstanceProperties{SAPInstance: defaultSAPInstance, Config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{ProcessMetricsToSkip: []string{pmHAAvailabilityPath, pmHANAAvailabilityPath}},
				CloudProperties:         defaultConfig.GetCloudProperties(),
			}, SkippedMetrics: map[string]bool{pmHAAvailabilityPath: true, pmHANAAvailabilityPath: true}},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
				}
			},
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{
					Name:       "hdbdaemon",
					Dispstatus: "SAPControl-GREEN",
					Pid:        111,
				},
			},
			},
			wantCount: 0,
		},
		{
			name: "SkipMetricsHAReplication",
			ip: &InstanceProperties{SAPInstance: defaultSAPInstance, Config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{ProcessMetricsToSkip: []string{pmHAAvailabilityPath, pmHANAAvailabilityPath}},
				CloudProperties:         defaultConfig.GetCloudProperties(),
			}, SkippedMetrics: map[string]bool{pmHAAvailabilityPath: true, pmHAReplicationPath: true}},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
				}
			},
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{
					Name:       "hdbdaemon",
					Dispstatus: "SAPControl-GREEN",
					Pid:        111,
				},
			},
			},
			wantCount: 1,
		},
		{
			name: "SuccessHANAAvailabilityReliability",
			ip: &InstanceProperties{SAPInstance: defaultSAPInstance, ReliabilityMetric: true, Config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{ProcessMetricsToSkip: []string{pmHAAvailabilityPath}},
				CloudProperties:         defaultConfig.GetCloudProperties()},
				SkippedMetrics: map[string]bool{pmHAAvailabilityPath: true},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{ExitCode: 1, Error: cmpopts.AnyError}
			},
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{Name: "hdbdaemon", Dispstatus: "SAPControl-GREEN", Pid: 111},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := collectHANAAvailabilityMetrics(context.Background(), test.ip, test.exec, commandlineexecutor.Params{}, test.fakeClient)
			if len(got) != test.wantCount {
				t.Errorf("collectHANAAvailabilityMetrics() returned unexpected value, got=%d, want=%d",
					len(got), test.wantCount)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectHANAAvailabilityMetrics() returned unexpected error, got=%v, want=%v",
					err, test.wantErr)
			}
		})
	}
}

func TestCollectNetWeaverMetrics(t *testing.T) {
	tests := []struct {
		name       string
		ip         *InstanceProperties
		fakeClient sapcontrolclienttest.Fake
		wantCount  int
		wantErr    error
	}{
		{
			name: "ErrorInMetricsCollection",
			ip:   defaultInstanceProperties,
			fakeClient: sapcontrolclienttest.Fake{
				ErrGetProcessList: cmpopts.AnyError,
			},
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
		},
		{
			name: "SuccessNetWeaverMetrics",
			ip:   defaultInstanceProperties,
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{Name: "hdbdaemon", Dispstatus: "SAPControl-GREEN", Pid: 111},
			}},
			wantCount: 1,
		},
		{
			name: "SkipNetweaverMetrics",
			ip: &InstanceProperties{
				SAPInstance: defaultSAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{pmNWAvailabilityPath},
					},
				},
				SkippedMetrics: map[string]bool{
					pmNWAvailabilityPath: true,
				},
			},
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				sapcontrolclient.OSProcess{Name: "hdbdaemon", Dispstatus: "SAPControl-GREEN", Pid: 111},
			}},
			wantCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := collectNetWeaverMetrics(context.Background(), test.ip, test.fakeClient)
			if len(got) != test.wantCount {
				t.Errorf("collectNetWeaverMetrics() returned unexpected value, got=%d, want=%d",
					len(got), test.wantCount)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectNetWeaverMetrics() returned unexpected error, got=%v, want=%v",
					err, cmpopts.AnyError)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name string
		list []string
		item string
		want bool
	}{
		{
			name: "ReturnsTrue",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "hdbdaemon",
			want: true,
		},
		{name: "ReturnsFalse",
			list: []string{"hdbdaemon", "hdbnameserver"},
			item: "random",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := contains(test.list, test.item)
			if got != test.want {
				t.Errorf("contains(%v, %v) returned unexpected value, got=%t, want=%t", test.list, test.item, got, test.want)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	c := context.Background()
	p := &InstanceProperties{SAPInstance: defaultSAPInstance, Config: defaultConfig, PMBackoffPolicy: defaultBOPolicy(c)}
	got, _ := p.CollectWithRetry(c)
	want := 2
	if len(got) != want {
		t.Errorf("CollectWithRetry() returned unexpected value, got=%d, want=%d", len(got), want)
	}
}

func TestRefreshHAReplicationConfig(t *testing.T) {
	tests := []struct {
		name              string
		instance          *sapb.SAPInstance
		replicationConfig sapdiscovery.ReplicationConfig
		wantStatus        int64
		wantErr           error
	}{{
		name: "success",
		instance: &sapb.SAPInstance{
			User:       "test",
			Sapsid:     "test",
			InstanceId: "test",
		},
		replicationConfig: func(ctx context.Context, user, sid, instID string) (int, []string, int64, *sapb.HANAReplicaSite, error) {
			return 1, []string{"test"}, 1, &sapb.HANAReplicaSite{}, nil
		},
		wantStatus: 1,
	}, {
		name: "error",
		instance: &sapb.SAPInstance{
			User:       "test",
			Sapsid:     "test",
			InstanceId: "test",
		},
		replicationConfig: func(ctx context.Context, user, sid, instID string) (int, []string, int64, *sapb.HANAReplicaSite, error) {
			return 1, []string{"test"}, 1, &sapb.HANAReplicaSite{}, errors.New("replication read error")
		},
		wantStatus: 0,
		wantErr:    cmpopts.AnyError,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &InstanceProperties{
				SAPInstance:       tc.instance,
				Config:            &cpb.Configuration{},
				ReplicationConfig: tc.replicationConfig,
			}
			got, err := refreshHAReplicationConfig(ctx, p)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Fatalf("refreshHAReplicationConfig() returned an unexpected error: %v, diff: %v", err, diff)
			}

			if got != tc.wantStatus {
				t.Errorf("refreshHAReplicationConfig() = %v, want: %v", got, tc.wantStatus)
			}
		})
	}
}
