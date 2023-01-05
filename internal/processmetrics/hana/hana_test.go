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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
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

func TestCollectHANAServiceMetrics(t *testing.T) {
	tests := []struct {
		name            string
		testProcesses   map[int]*sapcontrol.ProcessStatus
		wantMetricCount int
		wantValue       int64
	}{
		{
			name: "AllProcessesGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbdaemon", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
			},
			wantMetricCount: 3,
			wantValue:       systemAllProcessesGreen,
		},
		{
			name: "ThreeProcessGreenOneProcessNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbpreprosessor", IsGreen: true},
				3: &sapcontrol.ProcessStatus{Name: "hdbwebdispatcher", IsGreen: false},
			},
			wantMetricCount: 4,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerAndNameServerGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
				2: &sapcontrol.ProcessStatus{Name: "hdbxsengine", IsGreen: false},
			},
			wantMetricCount: 3,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: true},
			},
			wantMetricCount: 2,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
		{
			name: "NameServerNotGreen",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: true},
			},
			wantMetricCount: 2,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
		{
			name:            "ProcessMapEmpty",
			testProcesses:   map[int]*sapcontrol.ProcessStatus{},
			wantMetricCount: 0,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
		{
			name: "IndexServerAndNameServerRED",
			testProcesses: map[int]*sapcontrol.ProcessStatus{
				0: &sapcontrol.ProcessStatus{Name: "hdbindexserver", IsGreen: false},
				1: &sapcontrol.ProcessStatus{Name: "hdbnameserver", IsGreen: false},
				2: &sapcontrol.ProcessStatus{Name: "hdbxsengine", IsGreen: true},
			},
			wantMetricCount: 3,
			wantValue:       systemAtLeastOneProcessNotGreen,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics, got := collectHANAServiceMetrics(defaultInstanceProperties, test.testProcesses, tspb.Now())
			if got != test.wantValue {
				t.Errorf("collectHANAServiceMetrics() returned unexpected value, got=%d, want=%d",
					got, test.wantValue)
			}
			if len(metrics) != test.wantMetricCount {
				t.Errorf("collectHANAServiceMetrics() returned unexpected metric count, got=%d, want=%d",
					len(metrics), test.wantMetricCount)
			}
		})
	}
}

type fakeRunner struct {
	stdOut, stdErr string
	exitCode       int
	err            error
}

func (f *fakeRunner) RunWithEnv() (string, string, int, error) {
	return f.stdOut, f.stdErr, f.exitCode, f.err
}

func TestCollectReplicationHA(t *testing.T) {
	fRunner := &fakeRunner{stdOut: defaultSapControlOutput}

	got := collectReplicationHA(defaultInstanceProperties, fRunner)
	if len(got) != 10 {
		t.Errorf("collectReplicationHA(), got: %d want: 10.", len(got))
	}

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

func TestRunHANAQuery(t *testing.T) {
	successOutput := `| D |
	| - |
	| X |
	1 row selected (overall time 1187 usec; server time 509 usec)`

	tests := []struct {
		name           string
		fakeRun        runCmdAsUserExitCode
		wantQueryState queryState
		wantErr        error
	}{
		{
			name: "Success",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return successOutput, "", 0, nil
			},
			wantQueryState: queryState{
				state:       0,
				overallTime: 1187,
				serverTime:  509,
			},
			wantErr: nil,
		},
		{
			name: "NonZeroState",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return successOutput, "", 100, nil
			},
			wantQueryState: queryState{
				state:       100,
				overallTime: 1187,
				serverTime:  509,
			},
			wantErr: nil,
		},
		{
			name: "QueryFailure",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return "(overall time 10 usec; server time 10 usec)", "Not Found.", 0, cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseOverallTimeFailure",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return "(overall time invalid-int; server time 509 usec).", "", 0, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseServerTimeFailure",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return "(overall time 1187 usec; server time invalid-int)", "", 128, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "IntegerOverflow",
			fakeRun: func(string, string, string) (string, string, int64, error) {
				return "(overall time 100000000000000000000 usec; server time 10 usec)", "Not Found.", 0, nil
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotQueryState, gotErr := runHANAQuery(defaultInstanceProperties, test.fakeRun)

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
	fakeRun := func(string, string, string) (string, string, int64, error) {
		return "1 row selected (overall time 1187 usec; server time 509 usec)", "", 0, nil
	}

	got := collectHANAQueryMetrics(defaultInstanceProperties, fakeRun)
	if len(got) != 3 {
		t.Errorf("collectHANAQueryMetrics(), got: %d want: 3.", len(got))
	}
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name       string
		properties *InstanceProperties
		wantCount  int
	}{
		{
			name:       "MetricCountTest",
			properties: defaultInstanceProperties,
			wantCount:  1, // Without HANA setup in unit test ENV, only query/state metric is generated.
		},
		{
			name: "NoHANADBUserAndKey",
			properties: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					Sapsid:         "TST",
					InstanceNumber: "00",
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
			},
			wantCount: 0, // Query state metric not generated for HANA secondary.
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCount := len(test.properties.Collect())
			if gotCount != test.wantCount {
				t.Errorf("Collect(), got: %d want: %d.", gotCount, test.wantCount)
			}
		})
	}
}
