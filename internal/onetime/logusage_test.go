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

package onetime

import (
	"context"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestLogUsageHandler(t *testing.T) {
	// Prevent requests to the compute endpoint during test execution
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		LogUsageMetrics: false,
	})

	tests := []struct {
		name     string
		logUsage *LogUsage
		want     subcommands.ExitStatus
	}{
		{
			name: "EmptyUsageStatus",
			logUsage: &LogUsage{
				status: "",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "AgentUpdatedWithEmptyPriorVersion",
			logUsage: &LogUsage{
				status:       "UPDATED",
				priorVersion: "",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "ErrorWithInvalidErrorCode",
			logUsage: &LogUsage{
				status:     "ERROR",
				usageError: 0,
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "ErrorWithValidErrorCode",
			logUsage: &LogUsage{
				status:     "ERROR",
				usageError: 1,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "ActionWithEmptyActionCode",
			logUsage: &LogUsage{
				status: "ACTION",
				action: 0,
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "Success",
			logUsage: &LogUsage{
				status: "ACTION",
				action: 1,
			},
			want: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.logUsage.logUsageHandler(&ipb.CloudProperties{})
			if got != test.want {
				t.Errorf("logUsageHandler(%v) got: %v, want: %v", test.logUsage, got, test.want)
			}
		})

	}
}

func TestLogUsageStatus(t *testing.T) {
	// Prevent requests to the compute endpoint during test execution
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		LogUsageMetrics: false,
	})

	tests := []struct {
		name     string
		status   string
		actionID int
		errorID  int
		want     error
	}{
		{
			name:   "Running",
			status: "RUNNING",
			want:   nil,
		},
		{
			name:   "Started",
			status: "STARTED",
			want:   nil,
		},
		{
			name:   "Stopped",
			status: "STOPPED",
			want:   nil,
		},
		{
			name:   "Configured",
			status: "CONFIGURED",
			want:   nil,
		},
		{
			name:   "Misconfigured",
			status: "MISCONFIGURED",
			want:   nil,
		},
		{
			name:    "Error",
			status:  "ERROR",
			errorID: 1,
			want:    nil,
		},
		{
			name:   "Installed",
			status: "INSTALLED",
			want:   nil,
		},
		{
			name:   "Updated",
			status: "UPDATED",
			want:   nil,
		},
		{
			name:   "Uninstalled",
			status: "UNINSTALLED",
			want:   nil,
		},
		{
			name:     "Action",
			status:   "ACTION",
			actionID: 1,
			want:     nil,
		},
		{
			name:   "InvalidStatusReturnsError",
			status: "INVALID",
			want:   cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l := &LogUsage{
				status:     test.status,
				action:     test.actionID,
				usageError: test.errorID,
			}
			got := l.logUsageStatus(&ipb.CloudProperties{})
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("logUsageStatus(%q, %q, %q) got: %v, want nil", test.status, test.actionID, test.errorID, got)
			}
		})
	}
}

func TestExecuteLogUsage(t *testing.T) {
	tests := []struct {
		name     string
		logUsage *LogUsage
		want     subcommands.ExitStatus
		args     []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			logUsage: &LogUsage{
				status: "ACTION",
				action: 1,
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
			got := test.logUsage.Execute(context.Background(), &flag.FlagSet{}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.logUsage, test.args, got, test.want)
			}
		})
	}
}
