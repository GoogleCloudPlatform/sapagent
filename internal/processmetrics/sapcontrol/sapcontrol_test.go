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

package sapcontrol

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultProcessListOutput = `OK
		0 name: hdbdaemon
		0 dispstatus: GREEN
		0 pid: 111
		1 name: hdbcompileserver
		1 dispstatus: GREEN
		1 pid: 222
		2 name: hdbindexserver
		2 dispstatus: GREEN
		2 pid: 333`

	defaultEnqTableOutput = `
	OK
lock_name, lock_arg, lock_mode, owner, owner_vb, use_count_owner, use_count_owner_vb, client, user, transaction, object, backup
USR04, 000DDIC, E, 20230424073648639586000402dnwh75ldbci....................., 20230424073648639586000402dnwh75ldbci....................., 0, 1, 000, SAP*, SU01, E_USR04, FALSE
	`
	multilineEnqTableOutput = `
	09.05.2023 15:23:12
	EnqGetLockTable
	OK
	lock_name, lock_arg, lock_mode, owner, owner_vb, use_count_owner, use_count_owner_vb, client, user, transaction, object, backup
	USR04, 001BASIS, E, 20230509120639629460000602dnwh75ldbci....................., 20230509120639629460000602dnwh75ldbci....................., 0, 1, 001, DDIC, SU01, E_USR04, FALSE
	USR04, 001CALM_USER, E, 20230509120620130684000702dnwh75ldbci....................., 20230509120620130684000702dnwh75ldbci....................., 0, 1, 001, DDIC, SU01, E_USR04, FALSE`
)

type fakeRunner struct {
	stdOut, stdErr string
	exitCode       int
	err            error
}

func (f *fakeRunner) RunWithEnv() (string, string, int, error) {
	return f.stdOut, f.stdErr, f.exitCode, f.err
}

func TestGetProcessList(t *testing.T) {
	tests := []struct {
		name           string
		respProcesses  []sapcontrolclient.OSProcess
		wantProcStatus map[int]*ProcessStatus
		wantErr        error
	}{
		{
			name: "SucceedsAllProcesses",
			respProcesses: []sapcontrolclient.OSProcess{
				{"hdbdaemon", "SAPControl-GREEN", 9609},
				{"hdbcompileserver", "SAPControl-GREEN", 9972},
				{"hdbindexserver", "SAPControl-GREEN", 10013},
				{"hdbnameserver", "SAPControl-GREEN", 9642},
				{"hdbpreprocessor", "SAPControl-GREEN", 9975},
			},
			wantProcStatus: map[int]*ProcessStatus{
				0: &ProcessStatus{Name: "hdbdaemon", DisplayStatus: "GREEN", IsGreen: true, PID: "9609"},
				1: &ProcessStatus{Name: "hdbcompileserver", DisplayStatus: "GREEN", IsGreen: true, PID: "9972"},
				2: &ProcessStatus{Name: "hdbindexserver", DisplayStatus: "GREEN", IsGreen: true, PID: "10013"},
				3: &ProcessStatus{Name: "hdbnameserver", DisplayStatus: "GREEN", IsGreen: true, PID: "9642"},
				4: &ProcessStatus{Name: "hdbpreprocessor", DisplayStatus: "GREEN", IsGreen: true, PID: "9975"},
			},
			wantErr: nil,
		},
		{
			name: "NoNameForProcess",
			respProcesses: []sapcontrolclient.OSProcess{
				{"", "SAPControl-GREEN", 9609},
				{"hdbcompileserver", "SAPControl-GREEN", 9972},
			},
			wantProcStatus: map[int]*ProcessStatus{
				1: &ProcessStatus{Name: "hdbcompileserver", DisplayStatus: "GREEN", IsGreen: true, PID: "9972"},
			},
			wantErr: nil,
		},
		{
			name: "NoPIDForProcess",
			respProcesses: []sapcontrolclient.OSProcess{
				{"hdbdaemon", "SAPControl-GREEN", 9609},
				{"hdbcompileserver", "SAPControl-GREEN", 0},
			},
			wantProcStatus: map[int]*ProcessStatus{
				0: &ProcessStatus{Name: "hdbdaemon", DisplayStatus: "GREEN", IsGreen: true, PID: "9609"},
			},
			wantErr: nil,
		},
		{
			name: "NoDispstatus",
			respProcesses: []sapcontrolclient.OSProcess{
				{"hdbdaemon", "SAPControl-GREEN", 9609},
				{"hdbcompileserver", "", 9972},
			},
			wantProcStatus: map[int]*ProcessStatus{
				0: &ProcessStatus{Name: "hdbdaemon", DisplayStatus: "GREEN", IsGreen: true, PID: "9609"},
			},
			wantErr: nil,
		},
		{
			name: "WrongFormatDispstatus",
			respProcesses: []sapcontrolclient.OSProcess{
				{"hdbdaemon", "SAP-Control-GREEN", 9609},
				{"hdbcompileserver", "SAPControl-GREEN", 9972},
			},
			wantProcStatus: map[int]*ProcessStatus{
				1: &ProcessStatus{Name: "hdbcompileserver", DisplayStatus: "GREEN", IsGreen: true, PID: "9972"},
			},
			wantErr: nil,
		},
		{
			name:           "Error",
			respProcesses:  nil,
			wantProcStatus: nil,
			wantErr:        cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			var respErr error = nil
			if test.respProcesses == nil {
				respErr = cmpopts.AnyError
			}
			fakeSAPClient := sapcontrolclienttest.Fake{Processes: test.respProcesses, ErrGetProcessList: respErr}
			gotProcStatus, gotErr := p.GetProcessList(context.Background(), fakeSAPClient)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("GetProcessList(%v), gotErr: %v wantErr: %v.", fakeSAPClient, gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantProcStatus, gotProcStatus); diff != "" {
				t.Errorf("Properties.GetProcessList(%v) returned unexpected diff (-want +got):\n%v \ngot : %v", fakeSAPClient, diff, gotProcStatus)
			}
		})
	}
}

func TestABAPGetWPTable(t *testing.T) {
	tests := []struct {
		name               string
		wp                 []sapcontrolclient.WorkProcess
		wantProcesses      map[string]int
		wantBusyProcesses  map[string]int
		wantBusyPercentage map[string]int
		wantPIDMap         map[string]string
		wantErr            error
	}{
		{
			name:               "SuccessOneWorkProcess",
			wp:                 []sapcontrolclient.WorkProcess{{0, "DIA", 7488, "Run", "4", ""}},
			wantProcesses:      map[string]int{"DIA": 1, "Total": 1},
			wantBusyProcesses:  map[string]int{"DIA": 1, "Total": 1},
			wantBusyPercentage: map[string]int{"DIA": 100, "Total": 100},
			wantPIDMap:         map[string]string{"7488": "DIA"},
			wantErr:            nil,
		},
		{
			name: "SuccessAllWorkProcess",
			wp: []sapcontrolclient.WorkProcess{
				{0, "DIA", 7488, "Run", "4", ""},
				{1, "BTC", 7489, "Wait", "", ""},
				{2, "SPO", 7490, "Wait", "", ""},
				{3, "DIA", 7491, "Wait", "", ""},
				{4, "DIA", 7492, "Wait", "", ""},
			},
			wantProcesses:      map[string]int{"DIA": 3, "BTC": 1, "SPO": 1, "Total": 5},
			wantBusyProcesses:  map[string]int{"DIA": 1, "Total": 1},
			wantBusyPercentage: map[string]int{"DIA": 33, "BTC": 0, "SPO": 0, "Total": 20},
			wantPIDMap:         map[string]string{"7488": "DIA", "7489": "BTC", "7490": "SPO", "7491": "DIA", "7492": "DIA"},
			wantErr:            nil,
		},
		{
			name:               "Error",
			wp:                 nil,
			wantProcesses:      nil,
			wantBusyProcesses:  nil,
			wantBusyPercentage: nil,
			wantPIDMap:         nil,
			wantErr:            cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			var respErr error = nil
			if test.wp == nil {
				respErr = cmpopts.AnyError
			}
			wantWPDetails := WorkProcessDetails{test.wantProcesses, test.wantBusyProcesses, test.wantBusyPercentage, test.wantPIDMap}
			fakeSAPClient := sapcontrolclienttest.Fake{WorkProcesses: test.wp, ErrABAPGetWPTable: respErr}
			gotWPDetails, err := p.ABAPGetWPTable(context.Background(), fakeSAPClient)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ABAPGetWPTable(%v)=%v, want: %v.", fakeSAPClient, err, test.wantErr)
			}
			if diff := cmp.Diff(wantWPDetails, gotWPDetails); diff != "" {
				t.Errorf("ABAPGetWPTable(%v) work process details mismatch, diff (-want, +got): %v.", fakeSAPClient, diff)
			}
		})
	}
}

func TestParseQueueStats(t *testing.T) {
	tests := []struct {
		name        string
		fakeExec    commandlineexecutor.Execute
		wantCurrent map[string]int
		wantPeak    map[string]int
		wantErr     error
	}{
		{
			name: "Success",
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `Typ, Now, High, Max, Writes, Reads
					ABAP/NOWP, 0, 8, 14000, 270537, 270537
					ABAP/DIA, 0, 10, 14000, 534960, 534960
					ICM/Intern, 0, 7, 6000, 184690, 184690`,
				}
			},
			wantCurrent: map[string]int{"ABAP/NOWP": 0, "ABAP/DIA": 0, "ICM/Intern": 0},
			wantPeak:    map[string]int{"ABAP/NOWP": 8, "ABAP/DIA": 10, "ICM/Intern": 7},
		},
		{
			name: "Error",
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: cmpopts.AnyError,
				}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CurrentCountIntegerOverflow",
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `ABAP/NOWP, 1000000000000000000000, 8, 14000, 270537, 270537
					ABAP/DIA, 0, 10, 14000, 534960, 534960`,
				}
			},
			wantCurrent: map[string]int{"ABAP/DIA": 0},
			wantPeak:    map[string]int{"ABAP/DIA": 10},
		},
		{
			name: "PeakCountIntegerOverflow",
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `ABAP/NOWP, 0, 1000000000000000000000, 14000, 270537, 270537
					ABAP/DIA, 0, 10, 14000, 534960, 534960`,
				}
			},
			wantCurrent: map[string]int{"ABAP/DIA": 0, "ABAP/NOWP": 0},
			wantPeak:    map[string]int{"ABAP/DIA": 10},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			gotCurrentQueueUsage, gotPeakQueueUsage, err := p.ParseQueueStats(context.Background(), test.fakeExec, commandlineexecutor.Params{})

			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fakeExec, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantCurrent, gotCurrentQueueUsage); diff != "" {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fakeExec, gotCurrentQueueUsage, test.wantCurrent)
			}
			if diff := cmp.Diff(test.wantPeak, gotPeakQueueUsage); diff != "" {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fakeExec, gotPeakQueueUsage, test.wantPeak)
			}
		})
	}
}

func TestGetQueueStatistic(t *testing.T) {
	tests := []struct {
		name        string
		taskQueues  []sapcontrolclient.TaskHandlerQueue
		wantCurrent map[string]int64
		wantPeak    map[string]int64
		wantErr     error
	}{
		{
			name: "Success",
			taskQueues: []sapcontrolclient.TaskHandlerQueue{
				{"ABAP/NOWP", 0, 8}, {"ABAP/DIA", 0, 10}, {"ICM/Intern", 0, 7},
			},
			wantCurrent: map[string]int64{"ABAP/NOWP": 0, "ABAP/DIA": 0, "ICM/Intern": 0},
			wantPeak:    map[string]int64{"ABAP/NOWP": 8, "ABAP/DIA": 10, "ICM/Intern": 7},
		},
		{
			name:        "Error",
			taskQueues:  nil,
			wantCurrent: nil,
			wantPeak:    nil,
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			var respErr error = nil
			if test.taskQueues == nil {
				respErr = cmpopts.AnyError
			}
			fakeSAPClient := sapcontrolclienttest.Fake{TaskQueues: test.taskQueues, ErrGetQueueStatistic: respErr}
			gotCurrentQueueUsage, gotPeakQueueUsage, err := p.GetQueueStatistic(context.Background(), fakeSAPClient)

			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("GetQueueStatistic(%v)=%v, want: %v.", fakeSAPClient, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantCurrent, gotCurrentQueueUsage); diff != "" {
				t.Errorf("GetQueueStatistic(%v) Current queue usage mismatch, diff (-want, +got): %v", fakeSAPClient, diff)
			}
			if diff := cmp.Diff(test.wantPeak, gotPeakQueueUsage); diff != "" {
				t.Errorf("GetQueueStatistic(%v) Peak queue usage mismatch, diff (-want, +got): %v", fakeSAPClient, diff)
			}
		})
	}
}

func TestEnqGetLockTable(t *testing.T) {
	tests := []struct {
		name    string
		c       ClientInterface
		want    []*EnqLock
		wantErr error
	}{
		{
			name: "Success",
			c: sapcontrolclienttest.Fake{
				EnqLocks: []sapcontrolclient.EnqLock{
					{
						LockName:        "USR04",
						LockArg:         "001BASIS",
						LockMode:        "E",
						Owner:           "20230509120639629460000602dnwh75ldbci.....................",
						OwnerVB:         "20230509120639629460000602dnwh75ldbci.....................",
						UseCountOwnerVB: 1,
					},
				},
				ErrEnqGetLockTable: nil,
			},
			want: []*EnqLock{
				{
					LockName:         "USR04",
					LockArg:          "001BASIS",
					LockMode:         "E",
					Owner:            "20230509120639629460000602dnwh75ldbci.....................",
					OwnerVB:          "20230509120639629460000602dnwh75ldbci.....................",
					UserCountOwnerVB: 1,
				}},
			wantErr: nil,
		},
		{
			name: "Error",
			c: sapcontrolclienttest.Fake{
				ErrEnqGetLockTable: cmpopts.AnyError,
				EnqLocks:           nil,
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p Properties
			got, err := p.EnqGetLockTable(context.Background(), tc.c)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("EnqGetLockTable(%v)=%v, want %v", tc.c, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("EnqGetLockTable(%v) returned an unexpected diff (-want +got): %v", tc.c, diff)
			}
		})
	}
}
