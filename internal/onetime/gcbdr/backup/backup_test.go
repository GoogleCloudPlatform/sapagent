/*
Copyright 2024 Google LLC

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

package backup

import (
	"context"
	"strings"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func fakeExecSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 0,
		StdOut:   "success",
	}
}

func fakeExecError(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 1,
		StdErr:   "error",
		Error:    cmpopts.AnyError,
	}
}

func fakeExecHANAVersion(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 0,
		StdOut:   "HDB version info: version: 2.00.063.00.1655123455",
	}
}

func fakeExecHANAVersionWrongFormat(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 0,
		StdOut:   "HDB version info:",
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			b:    Backup{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			b:    Backup{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			b:    Backup{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			b:    Backup{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			b: Backup{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.b, test.args, got, test.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name               string
		b                  Backup
		exec               commandlineexecutor.Execute
		wantExitStatus     subcommands.ExitStatus
		wantResponseStatus bool
	}{
		{
			name:           "InvalidParamsMissingOperationType",
			b:              Backup{},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "InvalidParamsMissingSID",
			b: Backup{
				OperationType: "prepare",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "InvalidParamsMissingHDBUserstoreKey",
			b: Backup{
				OperationType: "prepare",
				SID:           "sid",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "InvalidOperationType",
			b: Backup{
				OperationType:   "invalid-operation",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "SuccessForPrepare",
			b: Backup{
				OperationType:   "prepare",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec:               fakeExecSuccess,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
		{
			name: "SuccessForFreeze",
			b: Backup{
				OperationType:   "freeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec:               fakeExecHANAVersion,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
		{
			name: "SuccessForUnfreeze",
			b: Backup{
				OperationType:   "unfreeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				JobName:         "jobname",
			},
			exec:               fakeExecHANAVersion,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
		{
			name: "SuccessForLogbackup",
			b: Backup{
				OperationType:   "logbackup",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec:               fakeExecSuccess,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
		{
			name: "SuccessForLogpurge",
			b: Backup{
				OperationType:   "logpurge",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				LogBackupEndPIT: "2024-01-01 00:00:00",
			},
			exec:               fakeExecHANAVersion,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
		{
			name: "CheckForCaseInsensitiveOperationType",
			b: Backup{
				OperationType:   "PREPARE",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec:               fakeExecSuccess,
			wantExitStatus:     subcommands.ExitSuccess,
			wantResponseStatus: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response, _, exitStatus := tc.b.Run(context.Background(), tc.exec, onetime.CreateRunOptions(nil, false))
			if exitStatus != tc.wantExitStatus {
				t.Errorf("Run(%v) = %v; want %v", tc.b, exitStatus, tc.wantExitStatus)
			}
			if response != nil && response.Status.GetValue() != tc.wantResponseStatus {
				t.Errorf("Run(%v) = %v; want %v", tc.b, response.Status, tc.wantResponseStatus)
			}
		})
	}
}

func TestPrepareHandler(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		exec commandlineexecutor.Execute
		want subcommands.ExitStatus
	}{
		{
			name: "ScriptError",
			b: Backup{
				OperationType:   "prepare",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecError,
			want: subcommands.ExitFailure,
		},
		{
			name: "Success",
			b: Backup{
				OperationType:   "prepare",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecSuccess,
			want: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := tc.b.prepareHandler(context.Background(), tc.exec)
			if got != tc.want {
				t.Errorf("prepareHandler(%v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestFreezeHandler(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		exec commandlineexecutor.Execute
		want subcommands.ExitStatus
	}{
		{
			name: "VersionFailure",
			b: Backup{
				OperationType:   "freeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecError,
			want: subcommands.ExitFailure,
		},
		{
			name: "VersionSuccessButFreezeFailure",
			b: Backup{
				OperationType:   "freeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: func(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(p.ArgsToSplit, "version") {
					return fakeExecHANAVersion(ctx, p)
				}
				return fakeExecError(ctx, p)
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "FreezeSuccess",
			b: Backup{
				OperationType:   "freeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecHANAVersion,
			want: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := tc.b.freezeHandler(context.Background(), tc.exec)
			if got != tc.want {
				t.Errorf("freezeHandler(%v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestUnfreezeHandler(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		exec commandlineexecutor.Execute
		want subcommands.ExitStatus
	}{
		{
			name: "InvalidParamsMissingJobName",
			b: Backup{
				OperationType:   "unfreeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "VersionFailure",
			b: Backup{
				OperationType:   "unfreeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				JobName:         "jobname",
			},
			exec: fakeExecError,
			want: subcommands.ExitFailure,
		},
		{
			name: "VersionSuccessButUnfreezeFailure",
			b: Backup{
				OperationType:   "unfreeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				JobName:         "jobname",
			},
			exec: func(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(p.ArgsToSplit, "version") {
					return fakeExecHANAVersion(ctx, p)
				}
				return fakeExecError(ctx, p)
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "UnfreezeSuccess",
			b: Backup{
				OperationType:   "unfreeze",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				JobName:         "jobname",
				SnapshotStatus:  "UNSUCCESSFUL",
				SnapshotType:    "PD",
			},
			exec: fakeExecHANAVersion,
			want: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := tc.b.unfreezeHandler(context.Background(), tc.exec)
			if got != tc.want {
				t.Errorf("unfreezeHandler(%v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestLogbackupHandler(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		exec commandlineexecutor.Execute
		want subcommands.ExitStatus
	}{
		{
			name: "ScriptError",
			b: Backup{
				OperationType:   "logbackup",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecError,
			want: subcommands.ExitFailure,
		},
		{
			name: "Success",
			b: Backup{
				OperationType:   "logbackup",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecSuccess,
			want: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := tc.b.logbackupHandler(context.Background(), tc.exec); got != tc.want {
				t.Errorf("logbackupHandler(%v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestLogpurgeHandler(t *testing.T) {
	tests := []struct {
		name string
		b    Backup
		exec commandlineexecutor.Execute
		want subcommands.ExitStatus
	}{
		{
			name: "InvalidParamsMissingLogBackupEndPIT",
			b: Backup{
				OperationType:   "logpurge",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "ScriptError",
			b: Backup{
				OperationType:   "logpurge",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				LogBackupEndPIT: "2024-01-01 00:00:00",
			},
			exec: fakeExecError,
			want: subcommands.ExitFailure,
		},
		{
			name: "VersionSuccessButLogpurgeFailure",
			b: Backup{
				OperationType:   "logpurge",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				LogBackupEndPIT: "2024-01-01 00:00:00",
			},
			exec: func(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(p.ArgsToSplit, "version") {
					return fakeExecHANAVersion(ctx, p)
				}
				return fakeExecError(ctx, p)
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "Success",
			b: Backup{
				OperationType:   "logpurge",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
				LogBackupEndPIT: "2024-01-01 00:00:00",
			},
			exec: fakeExecHANAVersion,
			want: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := tc.b.logpurgeHandler(context.Background(), tc.exec); got != tc.want {
				t.Errorf("logpurgeHandler(%v) = %v, want %v", tc.b, got, tc.want)
			}
		})
	}
}

func TestExtractHANAVersion(t *testing.T) {
	tests := []struct {
		name        string
		b           Backup
		exec        commandlineexecutor.Execute
		wantVersion string
		wantErr     error
	}{
		{
			name: "CommandError",
			b: Backup{
				SID: "sid",
			},
			exec:        fakeExecError,
			wantVersion: "",
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "UnexpectedVersionOutput",
			b: Backup{
				SID: "sid",
			},
			exec:        fakeExecHANAVersionWrongFormat,
			wantVersion: "",
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "ExpectedVersionOutput",
			b: Backup{
				SID: "sid",
			},
			exec:        fakeExecHANAVersion,
			wantVersion: "2.00.063.00.1655123455",
			wantErr:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.b.extractHANAVersion(context.Background(), tc.exec)
			if !cmp.Equal(got, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("extractHANAVersion(%v) = %v, want %v", tc.b, got, tc.wantErr)
			}
			if tc.b.hanaVersion != tc.wantVersion {
				t.Errorf("%v: got version %v, want version %v", tc.b, tc.b.hanaVersion, tc.wantVersion)
			}
		})
	}
}
