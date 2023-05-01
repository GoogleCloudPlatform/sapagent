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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

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

	defaultEnqTableOutputScript = `OK
	0 lock_name: USR04
	0 lock_arg: 000DDIC
	0 lock_mode: E
	0 owner: 20230424073648639586000402dnwh75ldbci.....................
	0 owner_vb: 20230424073648639586000402dnwh75ldbci.....................
	0 use_count_owner: 0
	0 use_count_owner_vb: 1
	0 client: 000
	0 user: SAP*
	0 transaction: SU01
	0 object: E_USR04
	0 backup: FALSE`

	defaultEnqTableOutput = `
	OK
lock_name, lock_arg, lock_mode, owner, owner_vb, use_count_owner, use_count_owner_vb, client, user, transaction, object, backup
USR04, 000DDIC, E, 20230424073648639586000402dnwh75ldbci....................., 20230424073648639586000402dnwh75ldbci....................., 0, 1, 000, SAP*, SU01, E_USR04, FALSE
	`
)

type fakeRunner struct {
	stdOut, stdErr string
	exitCode       int
	err            error
}

func (f *fakeRunner) RunWithEnv() (string, string, int, error) {
	return f.stdOut, f.stdErr, f.exitCode, f.err
}

func TestProcessList(t *testing.T) {
	tests := []struct {
		name           string
		fRunner        RunnerWithEnv
		wantProcStatus map[int]*ProcessStatus
		wantExitCode   int
		wantErr        error
	}{
		{
			name: "SucceedsOneProcess",
			fRunner: &fakeRunner{
				stdOut: `0 name: hdbdaemon
				0 description: HDB Daemon
				0 dispstatus: GREEN
				0 pid: 1234`,
				exitCode: 3,
			},
			wantProcStatus: map[int]*ProcessStatus{
				0: &ProcessStatus{
					Name:          "hdbdaemon",
					DisplayStatus: "GREEN",
					IsGreen:       true,
					PID:           "1234",
				},
			},
			wantExitCode: 3,
			wantErr:      nil,
		},
		{
			name: "SucceedsAllProcesses",
			fRunner: &fakeRunner{
				stdOut:   defaultProcessListOutput,
				exitCode: 4,
			},
			wantProcStatus: map[int]*ProcessStatus{
				0: &ProcessStatus{
					Name:          "hdbdaemon",
					DisplayStatus: "GREEN",
					IsGreen:       true,
					PID:           "111",
				},
				1: &ProcessStatus{
					Name:          "hdbcompileserver",
					DisplayStatus: "GREEN",
					IsGreen:       true,
					PID:           "222",
				},
				2: &ProcessStatus{
					Name:          "hdbindexserver",
					DisplayStatus: "GREEN",
					IsGreen:       true,
					PID:           "333",
				},
			},
			wantExitCode: 4,
			wantErr:      nil,
		},
		{
			name: "SapControlFails",
			fRunner: &fakeRunner{
				err: cmpopts.AnyError,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SapControlInvalidExitCode",
			fRunner: &fakeRunner{
				exitCode: -1,
			},
			wantErr:      cmpopts.AnyError,
			wantExitCode: -1,
		},
		{
			name: "NameError",
			fRunner: &fakeRunner{
				stdOut: `abc name: msg_server
				0 description: Message Server
				0 dispstatus: GREEN`,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CountMismatch",
			fRunner: &fakeRunner{
				stdOut: `0 name: msg_server
				0 description: Message Server
				0 dispstatus: GREEN
				1 dispstatus: GREEN`,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "PidMismatch",
			fRunner: &fakeRunner{
				stdOut: `0 name: msg_server
				0 description: Message Server
				1 dispstatus: GREEN`,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NameIntegerOverflow",
			fRunner: &fakeRunner{
				stdOut: `1000000000000000000000000 name: msg_server
				0 description: Message Server
				0 dispstatus: GREEN,
				0 pid: 1234`,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DispStatusIntegerOverflow",
			fRunner: &fakeRunner{
				stdOut: `0 name: msg_server
				0 description: Message Server
				1000000000000000000000000 dispstatus: GREEN,
				0 pid: 1234`,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoNameEntryForProcess",
			fRunner: &fakeRunner{
				stdOut: `1 name: hdbdaemon
				0 description: HDB Daemon
				0 dispstatus: GREEN
				0 pid: 1234`,
				exitCode: 3,
			},
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			gotProcStatus, gotExitCode, gotErr := p.ProcessList(test.fRunner)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ProcessList(), gotErr: %v wantErr: %v.",
					gotErr, test.wantErr)
			}

			if gotExitCode != test.wantExitCode {
				t.Errorf("ProcessList(), gotExitCode: %d wantExitCode: %d.",
					gotExitCode, test.wantExitCode)
			}

			diff := cmp.Diff(test.wantProcStatus, gotProcStatus, cmp.AllowUnexported(ProcessStatus{}))
			if diff != "" {
				t.Errorf("ProcessList(), diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseABAPGetWPTable(t *testing.T) {
	tests := []struct {
		name              string
		fRunner           RunnerWithEnv
		wantProcesses     map[string]int
		wantBusyProcesses map[string]int
		wantPIDMap        map[string]string
		wantErr           error
	}{
		{
			name: "Success",
			fRunner: &fakeRunner{
				stdOut: `No, Typ, Pid, Status, Reason, Start, Err, Sem, Cpu, Time, Program, Client, User, Action, Table
				0, DIA, 7488, Wait, , yes, , , 0:24:54,4, , , , ,
				1, BTC, 7489, Wait, , yes, , , 0:33:24, , , , , ,
				2, SPO, 7490, Wait, , yes, , , 0:22:11, , , , , ,
				3, DIA, 7491, Wait, , yes, , , 0:46:38, , , , , ,
				4, DIA, 7492, Wait, , yes, , , 0:37:05, , , , , ,`,
			},
			wantProcesses:     map[string]int{"DIA": 3, "BTC": 1, "SPO": 1},
			wantBusyProcesses: map[string]int{"DIA": 1},
			wantPIDMap:        map[string]string{"7488": "DIA", "7489": "BTC", "7490": "SPO", "7491": "DIA", "7492": "DIA"},
		},
		{
			name:    "Error",
			fRunner: &fakeRunner{err: cmpopts.AnyError},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			gotProcessCount, gotBusyProcessCount, gotPIDMap, err := p.ParseABAPGetWPTable(test.fRunner)

			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ParseABAPGetWPTable(%v)=%v, want: %v.", test.fRunner, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantProcesses, gotProcessCount); diff != "" {
				t.Errorf("ParseABAPGetWPTable(%v)=%v, want: %v.", test.fRunner, gotProcessCount, test.wantProcesses)
			}
			if diff := cmp.Diff(test.wantBusyProcesses, gotBusyProcessCount); diff != "" {
				t.Errorf("ParseABAPGetWPTable(%v)=%v, want: %v.", test.fRunner, gotBusyProcessCount, test.wantBusyProcesses)
			}
			if diff := cmp.Diff(test.wantPIDMap, gotPIDMap); diff != "" {
				t.Errorf("ParseABAPGetWPTable(%v)=%v, want: %v.", test.fRunner, gotPIDMap, test.wantPIDMap)
			}
		})
	}
}

func TestParseQueueStats(t *testing.T) {
	tests := []struct {
		name        string
		fRunner     RunnerWithEnv
		wantCurrent map[string]int
		wantPeak    map[string]int
		wantErr     error
	}{
		{
			name: "Success",
			fRunner: &fakeRunner{
				stdOut: `Typ, Now, High, Max, Writes, Reads
				ABAP/NOWP, 0, 8, 14000, 270537, 270537
				ABAP/DIA, 0, 10, 14000, 534960, 534960
				ICM/Intern, 0, 7, 6000, 184690, 184690`,
			},
			wantCurrent: map[string]int{"ABAP/NOWP": 0, "ABAP/DIA": 0, "ICM/Intern": 0},
			wantPeak:    map[string]int{"ABAP/NOWP": 8, "ABAP/DIA": 10, "ICM/Intern": 7},
		},
		{
			name:    "Error",
			fRunner: &fakeRunner{err: cmpopts.AnyError},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CurrentCountIntegerOverflow",
			fRunner: &fakeRunner{
				stdOut: `ABAP/NOWP, 1000000000000000000000, 8, 14000, 270537, 270537
				ABAP/DIA, 0, 10, 14000, 534960, 534960`,
			},
			wantCurrent: map[string]int{"ABAP/DIA": 0},
			wantPeak:    map[string]int{"ABAP/DIA": 10},
		},
		{
			name: "PeakCountIntegerOverflow",
			fRunner: &fakeRunner{
				stdOut: `ABAP/NOWP, 0, 1000000000000000000000, 14000, 270537, 270537
				ABAP/DIA, 0, 10, 14000, 534960, 534960`,
			},
			wantCurrent: map[string]int{"ABAP/DIA": 0, "ABAP/NOWP": 0},
			wantPeak:    map[string]int{"ABAP/DIA": 10},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			gotCurrentQueueUsage, gotPeakQueueUsage, err := p.ParseQueueStats(test.fRunner)

			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fRunner, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantCurrent, gotCurrentQueueUsage); diff != "" {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fRunner, gotCurrentQueueUsage, test.wantCurrent)
			}
			if diff := cmp.Diff(test.wantPeak, gotPeakQueueUsage); diff != "" {
				t.Errorf("ParseQueueStats(%v)=%v, want: %v.", test.fRunner, gotPeakQueueUsage, test.wantPeak)
			}
		})
	}
}

func TestEnqGetLockTable(t *testing.T) {
	tests := []struct {
		name         string
		fRunner      RunnerWithEnv
		wantEnqLocks []*EnqLock
		wantErr      error
	}{
		{
			name:    "Success",
			fRunner: &fakeRunner{stdOut: defaultEnqTableOutput},
			wantEnqLocks: []*EnqLock{
				{
					LockName:         "USR04",
					LockArg:          "000DDIC",
					LockMode:         "E",
					Owner:            "20230424073648639586000402dnwh75ldbci.....................",
					OwnerVB:          "20230424073648639586000402dnwh75ldbci.....................",
					UserCountOwner:   0,
					UserCountOwnerVB: 1,
					Client:           "000",
					User:             "SAP*",
					Transaction:      "SU01",
					Object:           "E_USR04",
					Backup:           "FALSE",
				},
			},
		},
		{
			name:    "Error",
			fRunner: &fakeRunner{err: cmpopts.AnyError},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ErroneousOwnerCount",
			fRunner: &fakeRunner{stdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1ab0, 1, 000, SAP*, SU01, E_USR04, FALSE"},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ErroneousOwnerCountVB",
			fRunner: &fakeRunner{stdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 10, 1000000000000000000000, 000, SAP*, SU01, E_USR04, FALSE"},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "InvalidStatus",
			fRunner: &fakeRunner{exitCode: -1},
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Properties{}
			gotEnqList, gotErr := p.EnqGetLockTable(test.fRunner)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("EnqGetLockTable(%v)=%v, want: %v.", test.fRunner, gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantEnqLocks, gotEnqList); diff != "" {
				t.Errorf("EnqGetLockTable(%v) mismatch, diff (-want, +got): %v", test.fRunner, diff)
			}
		})
	}
}
