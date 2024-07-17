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

package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"flag"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestDisableService(t *testing.T) {
	tests := []struct {
		name string
		s    Service
		exec func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test1",
				"test2",
				"test3",
			},
		},
		{
			name: "SuccessForHelp",
			s:    Service{help: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"h",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "FailEnableAndDisable",
			s:    Service{disable: true, enable: true},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "SuccessForDisable",
			s:    Service{disable: true},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "FailureForDisable",
			s:    Service{disable: true},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: errors.New("test error")}
			},
			want: subcommands.ExitFailure,
		},
	}
	defer func(f func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result) {
		executeCommand = f
	}(executeCommand)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executeCommand = test.exec
			got := test.s.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.s, test.args, got, test.want)
			}
		})
	}
}

func TestEnableeService(t *testing.T) {
	tests := []struct {
		name string
		s    Service
		exec func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "SuccessForEnable",
			s:    Service{enable: true},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "FailureForEnable",
			s:    Service{enable: true},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: errors.New("test error")}
			},
			want: subcommands.ExitFailure,
		},
	}
	defer func(f func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result) {
		executeCommand = f
	}(executeCommand)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executeCommand = test.exec
			got := test.s.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.s, test.args, got, test.want)
			}
		})
	}
}
