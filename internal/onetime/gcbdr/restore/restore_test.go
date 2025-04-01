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
			r:    Restore{},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			r: Restore{
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
			got := test.r.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.r, test.args, got, test.want)
			}
		})
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
