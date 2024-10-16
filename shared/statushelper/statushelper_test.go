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

package statushelper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fatih/color"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
)

type fakeExecutor struct {
	commandlineexecutor.Execute
	commandlineexecutor.Exists

	// fakeCommandRes maps commands to their results.
	fakeCommandRes map[string]commandlineexecutor.Result
}

func (e *fakeExecutor) ExecuteCommand(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	for cmd, res := range e.fakeCommandRes {
		if strings.Contains(params.ArgsToSplit, cmd) {
			return res
		}
	}
	return commandlineexecutor.Result{Error: fmt.Errorf("cannot run command")}
}

func (e *fakeExecutor) CommandExists(cmd string) bool {
	if _, ok := e.fakeCommandRes[cmd]; ok {
		return true
	}
	return false
}

func TestPackageVersionLinux(t *testing.T) {
	tests := []struct {
		name        string
		packageName string
		fakeCommand string
		fakeRes     commandlineexecutor.Result
		wantLatest  string
		wantErr     error
	}{
		{
			name:        "YumSuccess",
			packageName: "foo",
			fakeCommand: "yum",
			fakeRes: commandlineexecutor.Result{
				StdOut: "3.5-671008012 ",
			},
			wantLatest: "3.5-671008012",
		},
		{
			name:        "YumFailure",
			packageName: "foo",
			fakeCommand: "yum",
			fakeRes: commandlineexecutor.Result{
				ExitCode: 1,
				Error:    fmt.Errorf("could not refresh repositories"),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "ZypperSuccess",
			packageName: "foo",
			fakeCommand: "zypper",
			fakeRes: commandlineexecutor.Result{
				StdOut: "3.5-671008012",
			},
			wantLatest: "3.5-671008012",
		},
		{
			name:        "ZypperFailure",
			packageName: "foo",
			fakeCommand: "zypper",
			fakeRes: commandlineexecutor.Result{
				ExitCode: 1,
				Error:    fmt.Errorf("could not refresh repositories"),
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "NoSupportedPackageManager",
			packageName: "foo",
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exec := &fakeExecutor{fakeCommandRes: map[string]commandlineexecutor.Result{test.fakeCommand: test.fakeRes}}
			gotLatest, gotErr := packageVersionLinux(context.Background(), test.packageName, exec.ExecuteCommand, exec.CommandExists)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("packageVersionLinux(%s) returned err: %v, wantErr: %v", test.packageName, gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantLatest, gotLatest); diff != "" {
				t.Errorf("packageVersionLinux(%s) returned unexpected diff (-want +got):\n%s", test.packageName, diff)
			}
		})
	}
}

func TestAgentEnabledAndRunningLinux(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		fakeCommand map[string]commandlineexecutor.Result
		wantEnabled bool
		wantRunning bool
		wantErr     error
	}{
		{
			name:        "ServiceEnabledAndRunning",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "enabled",
					ExitCode: 0,
				},
				"is-active": commandlineexecutor.Result{
					StdOut:   "active",
					ExitCode: 0,
				},
			},
			wantEnabled: true,
			wantRunning: true,
		},
		{
			name:        "ServiceEnabledButNotRunning",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "enabled",
					ExitCode: 0,
				},
				"is-active": commandlineexecutor.Result{
					StdOut:   "inactive",
					ExitCode: 1,
				},
			},
			wantEnabled: true,
			wantRunning: false,
		},
		{
			name:        "ServiceNotEnabledButRunning",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "disabled",
					ExitCode: 0,
				},
				"is-active": commandlineexecutor.Result{
					StdOut:   "active",
					ExitCode: 0,
				},
			},
			wantEnabled: false,
			wantRunning: true,
		},
		{
			name:        "ServiceNotEnabledAndNotRunning",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "disabled",
					ExitCode: 0,
				},
				"is-active": commandlineexecutor.Result{
					StdOut:   "inactive",
					ExitCode: 1,
				},
			},
			wantEnabled: false,
			wantRunning: false,
		},
		{
			name:        "ServiceNotEnabledAndNotRunningDifferentOutput",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "not enabled",
					ExitCode: 1,
				},
				"is-active": commandlineexecutor.Result{
					StdOut:   "inactive",
					ExitCode: 1,
				},
			},
			wantEnabled: false,
			wantRunning: false,
		},
		{
			name:        "ErrorCheckingEnabledStatus",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdErr: "error checking enabled status",
					Error:  fmt.Errorf("error checking enabled status"),
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:        "ErrorCheckingRunningStatus",
			serviceName: "foo",
			fakeCommand: map[string]commandlineexecutor.Result{
				"is-enabled": commandlineexecutor.Result{
					StdOut:   "enabled",
					ExitCode: 0,
				},
				"is-active": commandlineexecutor.Result{
					StdErr: "error checking running status",
					Error:  fmt.Errorf("error checking running status"),
				},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exec := &fakeExecutor{
				fakeCommandRes: test.fakeCommand,
			}
			gotEnabled, gotRunning, err := agentEnabledAndRunningLinux(context.Background(), test.serviceName, exec.ExecuteCommand)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("agentEnabledAndRunningLinux(%s) returned err: %v, wantErr: %v", test.serviceName, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantEnabled, gotEnabled); diff != "" {
				t.Errorf("agentEnabledAndRunningLinux(%s) returned unexpected enabled status diff (-want +got):\n%s", test.serviceName, diff)
			}
			if diff := cmp.Diff(test.wantRunning, gotRunning); diff != "" {
				t.Errorf("agentEnabledAndRunningLinux(%s) returned unexpected running status diff (-want +got):\n%s", test.serviceName, diff)
			}
		})
	}
}

func TestPrintStatus(t *testing.T) {
	tests := []struct {
		name   string
		status *spb.AgentStatus
		want   string
	}{
		{
			name:   "emptyStatus",
			status: &spb.AgentStatus{},
			want: `--------------------------------------------------------------------------------
|                                    Status                                    |
--------------------------------------------------------------------------------
Agent Status:
    Installed Version: 
    Available Version: 
    Systemd Service Enabled: Error: could not determine status
    Systemd Service Running: Error: could not determine status
    Configuration File: 
    Configuration Valid: Error: could not determine status
        


`,
		},
		{
			name: "fullStatusAllSuccess",
			status: &spb.AgentStatus{
				AgentName:                 "Agent for SAP",
				InstalledVersion:          "3.6",
				AvailableVersion:          "3.6",
				SystemdServiceEnabled:     spb.State_SUCCESS_STATE,
				SystemdServiceRunning:     spb.State_SUCCESS_STATE,
				ConfigurationFilePath:     "/etc/google-cloud-sap-agent/configuration.json",
				ConfigurationValid:        spb.State_SUCCESS_STATE,
				ConfigurationErrorMessage: "error: proto: (line 6:44): invalid value for bool field value: 2",
				Services: []*spb.ServiceStatus{
					{
						Name:            "Process Metrics",
						Enabled:         true,
						FullyFunctional: true,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						IamRoles: []*spb.IAMRole{
							{
								Name:    "Compute Viewer",
								Role:    "roles/compute.viewer",
								Granted: spb.State_SUCCESS_STATE,
							},
							{
								Name:    "Monitoring Viewer",
								Role:    "roles/monitoring.viewer",
								Granted: spb.State_SUCCESS_STATE,
							},
						},
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "collect_process_metrics",
								Value:     "True",
								IsDefault: true,
							},
							{
								Name:      "process_metrics_frequency",
								Value:     "5",
								IsDefault: true,
							},
						},
					},
					{
						Name:            "Host Metrics",
						Enabled:         true,
						FullyFunctional: true,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "Hello",
								Value:     "World",
								IsDefault: true,
							},
						},
					},
					{
						Name:            "Backint",
						Enabled:         false,
						FullyFunctional: false,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "fake_config",
								Value:     "5",
								IsDefault: true,
							},
						},
					},
				},
				References: []*spb.Reference{
					{
						Name: "IAM Permissions",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/planning#required_iam_roles",
					},
					{
						Name: "What's New",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new",
					},
				},
			},
			want: `--------------------------------------------------------------------------------
|                             Agent for SAP Status                             |
--------------------------------------------------------------------------------
Agent Status:
    Installed Version: 3.6
    Available Version: 3.6
    Systemd Service Enabled: True
    Systemd Service Running: True
    Configuration File: /etc/google-cloud-sap-agent/configuration.json
    Configuration Valid: True
--------------------------------------------------------------------------------
Process Metrics: Enabled
    Status: Fully Functional
    Compute Viewer (roles/compute.viewer): True
    Monitoring Viewer (roles/monitoring.viewer): True
    collect_process_metrics: True (default)
    process_metrics_frequency: 5 (default)
--------------------------------------------------------------------------------
Host Metrics: Enabled
    Status: Fully Functional
    Hello: World (default)
--------------------------------------------------------------------------------
Backint: Disabled
--------------------------------------------------------------------------------
References:
IAM Permissions: https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/planning#required_iam_roles
What's New: https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new


`,
		},
		{
			name: "fullStatusWithFailures",
			status: &spb.AgentStatus{
				AgentName:                 "Agent for SAP",
				InstalledVersion:          "3.5",
				AvailableVersion:          "3.6",
				SystemdServiceEnabled:     spb.State_FAILURE_STATE,
				SystemdServiceRunning:     spb.State_FAILURE_STATE,
				ConfigurationFilePath:     "/etc/google-cloud-sap-agent/configuration.json",
				ConfigurationValid:        spb.State_FAILURE_STATE,
				ConfigurationErrorMessage: "error: proto: (line 6:44): invalid value for bool field value: 2",
				Services: []*spb.ServiceStatus{
					{
						Name:            "Process Metrics",
						Enabled:         true,
						FullyFunctional: false,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						IamRoles: []*spb.IAMRole{
							{
								Name:    "Compute Viewer",
								Role:    "roles/compute.viewer",
								Granted: spb.State_SUCCESS_STATE,
							},
							{
								Name:    "Monitoring Viewer",
								Role:    "roles/monitoring.viewer",
								Granted: spb.State_ERROR_STATE,
							},
						},
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "collect_process_metrics",
								Value:     "True",
								IsDefault: false,
							},
							{
								Name:      "process_metrics_frequency",
								Value:     "",
								IsDefault: true,
							},
						},
					},
					{
						Name:            "Host Metrics",
						Enabled:         true,
						FullyFunctional: true,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "Hello",
								Value:     "World",
								IsDefault: true,
							},
						},
					},
					{
						Name:            "Backint",
						Enabled:         false,
						FullyFunctional: false,
						ErrorMessage:    "Cannot write to Cloud Monitoring, check IAM permissions",
						ConfigValues: []*spb.ConfigValue{
							{
								Name:      "fake_config",
								Value:     "5",
								IsDefault: true,
							},
						},
					},
				},
				References: []*spb.Reference{
					{
						Name: "IAM Permissions",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/planning#required_iam_roles",
					},
					{
						Name: "What's New",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new",
					},
				},
			},
			want: `--------------------------------------------------------------------------------
|                             Agent for SAP Status                             |
--------------------------------------------------------------------------------
Agent Status:
    Installed Version: 3.5
    Available Version: 3.6
    Systemd Service Enabled: False
    Systemd Service Running: False
    Configuration File: /etc/google-cloud-sap-agent/configuration.json
    Configuration Valid: False
        error: proto: (line 6:44): invalid value for bool field value: 2
--------------------------------------------------------------------------------
Process Metrics: Enabled
    Status: Error: Cannot write to Cloud Monitoring, check IAM permissions
    Compute Viewer (roles/compute.viewer): True
    Monitoring Viewer (roles/monitoring.viewer): Error: could not determine status
    collect_process_metrics: True (configuration file)
    process_metrics_frequency: nil (default)
--------------------------------------------------------------------------------
Host Metrics: Enabled
    Status: Fully Functional
    Hello: World (default)
--------------------------------------------------------------------------------
Backint: Disabled
--------------------------------------------------------------------------------
References:
IAM Permissions: https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/planning#required_iam_roles
What's New: https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new


`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Monkey patch stdout to check the output.
			defer func(oldStdout *os.File) {
				os.Stdout = oldStdout
				color.Output = oldStdout
			}(os.Stdout)
			r, w, _ := os.Pipe()
			os.Stdout = w
			color.Output = w
			PrintStatus(context.Background(), tc.status)

			w.Close()
			var buf bytes.Buffer
			io.Copy(&buf, r)

			// NOTE: The //third_party/golang/fatihcolor/color package does some
			// helpful tricks to detect it's not able to support colors in the go
			// test environment so the text here has no special characters
			got := buf.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("PrintStatus(%v) had unexpected diff (-want +got):\n%s", tc.status, diff)
			}
		})
	}
}
