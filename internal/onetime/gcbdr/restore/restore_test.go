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

// Package restore is the module containing one time execution for HANA GCBDR restore.
package restore

import (
	"context"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

func fakeExecError(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 1,
		StdErr:   "error",
		Error:    cmpopts.AnyError,
	}
}

func fakeExecSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		ExitCode: 0,
		StdOut:   "success",
	}
}

func TestSynopsis(t *testing.T) {
	r := &Restore{}
	want := "invoke GCBDR CoreAPP restore script"
	if got := r.Synopsis(); got != want {
		t.Errorf("Synopsis() = %q, want %q", got, want)
	}
}

func TestUsage(t *testing.T) {
	r := Restore{}
	want := `gcbdr-restore -operation-type=<restore|restore-log>
	-database_sid=<HANA-sid> -hana_version=<HANA-version> -hdbuserstore_key=<userstore-key>
	[-source_data_volume=<source-data-volume>] [-source_log_volume=<source-log-volume>]
	[-new_target=<true|false>] [-data_vg_name=<data-vg-name>] [-log_vg_name=<log-vg-name>]
	[-data_mnt=<data-mnt>] [-log_backup_mnt=<log-backup-mnt>] [-loglevel=<debug|info|warn|error>]
	[-log-path=<log-path>]` + "\n"
	got := r.Usage()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Usage() returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		r    Restore
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			r:    Restore{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			r:    Restore{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			r:    Restore{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			r: Restore{
				LogLevel: "error",
			},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "RunSuccessLogsToStdout",
			r: Restore{
				OperationType: "restore-preflight",
				LogLevel:      "info",
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
			got := test.r.Execute(context.Background(), flag.NewFlagSet("test", flag.ContinueOnError), test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.r, test.args, got, test.want)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	tests := []struct {
		name       string
		flagName   string
		flagValue  string
		checkField func(res *Restore) bool
		expected   any
	}{
		{
			name:      "OperationType",
			flagName:  "operation-type",
			flagValue: "restore",
			checkField: func(res *Restore) bool {
				return res.OperationType == "restore"
			},
			expected: "restore",
		},
		{
			name:      "SID",
			flagName:  "database_sid",
			flagValue: "HXE",
			checkField: func(res *Restore) bool {
				return res.SID == "HXE"
			},
			expected: "HXE",
		},
		{
			name:      "HANAVersion",
			flagName:  "hana_version",
			flagValue: "2.00.059.00",
			checkField: func(res *Restore) bool {
				return res.HANAVersion == "2.00.059.00"
			},
			expected: "2.00.059.00",
		},
		{
			name:      "HDBUserstoreKey",
			flagName:  "hdbuserstore_key",
			flagValue: "SYSTEM_KEY",
			checkField: func(res *Restore) bool {
				return res.HDBUserstoreKey == "SYSTEM_KEY"
			},
			expected: "SYSTEM_KEY",
		},
		{
			name:      "SourceDataVolume",
			flagName:  "source_data_volume",
			flagValue: "/hana/data/HXE/mnt00001",
			checkField: func(res *Restore) bool {
				return res.SourceDataVolume == "/hana/data/HXE/mnt00001"
			},
			expected: "/hana/data/HXE/mnt00001",
		},
		{
			name:      "SourceLogVolume",
			flagName:  "source_log_volume",
			flagValue: "/hana/log/HXE/mnt00001",
			checkField: func(res *Restore) bool {
				return res.SourceLogVolume == "/hana/log/HXE/mnt00001"
			},
			expected: "/hana/log/HXE/mnt00001",
		},
		{
			name:      "NewTargetTrue",
			flagName:  "new_target",
			flagValue: "true",
			checkField: func(res *Restore) bool {
				return res.NewTarget == true
			},
			expected: true,
		},
		{
			name:      "NewTargetFalse",
			flagName:  "new_target",
			flagValue: "false",
			checkField: func(res *Restore) bool {
				return res.NewTarget == false
			},
			expected: false,
		},
		{
			name:      "DataVGName",
			flagName:  "data_vg_name",
			flagValue: "vg_hana_data_hxe",
			checkField: func(res *Restore) bool {
				return res.DataVGName == "vg_hana_data_hxe"
			},
			expected: "vg_hana_data_hxe",
		},
		{
			name:      "LogVGName",
			flagName:  "log_vg_name",
			flagValue: "vg_hana_log_hxe",
			checkField: func(res *Restore) bool {
				return res.LogVGName == "vg_hana_log_hxe"
			},
			expected: "vg_hana_log_hxe",
		},
		{
			name:      "DataMnt",
			flagName:  "data_mnt",
			flagValue: "/hana/data/HXE",
			checkField: func(res *Restore) bool {
				return res.DataMnt == "/hana/data/HXE"
			},
			expected: "/hana/data/HXE",
		},
		{
			name:      "LogBackupMnt",
			flagName:  "log_backup_mnt",
			flagValue: "/hanabackup/log/HXE",
			checkField: func(res *Restore) bool {
				return res.LogBackupMnt == "/hanabackup/log/HXE"
			},
			expected: "/hanabackup/log/HXE",
		},
		{
			name:      "LogLevelDebug",
			flagName:  "loglevel",
			flagValue: "debug",
			checkField: func(res *Restore) bool {
				return res.LogLevel == "debug"
			},
			expected: "debug",
		},
		{
			name:      "LogPath",
			flagName:  "log-path",
			flagValue: "/tmp/gcbdr.log",
			checkField: func(res *Restore) bool {
				return res.LogPath == "/tmp/gcbdr.log"
			},
			expected: "/tmp/gcbdr.log",
		},
		{
			name:      "HelpShort",
			flagName:  "h",
			flagValue: "true",
			checkField: func(res *Restore) bool {
				return res.help == true
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			currentRestore := &Restore{}
			currentFs := flag.NewFlagSet(tc.name, flag.ContinueOnError)
			currentRestore.SetFlags(currentFs)

			err := currentFs.Set(tc.flagName, tc.flagValue)
			if err != nil {
				t.Fatalf("Set(%q, %q) failed: %v", tc.flagName, tc.flagValue, err)
			}

			if !tc.checkField(currentRestore) {
				t.Errorf("Flag %q with value %q did not set field correctly. Got: %+v, Expected related field to be: %v", tc.flagName, tc.flagValue, currentRestore, tc.expected)
			}
		})
	}
}

func TestSetFlags_LogLevelDefault(t *testing.T) {
	defaultRestore := Restore{}
	defaultFs := flag.NewFlagSet("defaultLogLevel", flag.ContinueOnError)
	defaultRestore.SetFlags(defaultFs)
	if err := defaultFs.Parse([]string{}); err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if defaultRestore.LogLevel != "info" {
		t.Errorf("Default LogLevel got %q, want %q", defaultRestore.LogLevel, "info")
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name               string
		r                  Restore
		exec               commandlineexecutor.Execute
		wantResult         *gpb.CommandResult
		wantResponseStatus bool
	}{
		{
			name: "MissingOperationType",
			r: Restore{
				OperationType:   "invalid-operation",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecError,
			wantResult: &gpb.CommandResult{
				ExitCode: -1,
				Stderr:   "invalid opration type: invalid-operation",
				Stdout:   "invalid opration type: invalid-operation",
			},
		},
		{
			name: "Success",
			r: Restore{
				OperationType:   "restore-preflight",
				SID:             "sid",
				HDBUserstoreKey: "userstorekey",
			},
			exec: fakeExecSuccess,
			wantResult: &gpb.CommandResult{
				ExitCode: 0,
				Stdout:   "GCBDR CoreAPP script for preflight operation executed successfully",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Run(context.Background(), test.exec, onetime.CreateRunOptions(nil, false))
			if diff := cmp.Diff(test.wantResult, got, protocmp.Transform()); diff != "" {
				t.Errorf("Run(%v) returned an unexpected diff (-want +got): %v", test.r, diff)
			}
		})
	}
}
