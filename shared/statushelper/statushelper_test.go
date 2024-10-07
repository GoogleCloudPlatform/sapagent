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
	"testing"

	"github.com/google/go-cmp/cmp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
)

func TestGetCurrentAndLatestVersion(t *testing.T) {
	// Implement unit tests for GetCurrentAndLatestVersion.
}

func TestCheckIAMRoles(t *testing.T) {
	// Implement unit tests for CheckIAMRoles.
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
    Systemd Service Enabled: False
    Systemd Service Running: False
    Configuration File: 
    Configuration Valid: False
        


`,
		},
		{
			name: "fullStatusAllSuccess",
			status: &spb.AgentStatus{
				AgentName:                 "Agent for SAP",
				InstalledVersion:          "3.6",
				AvailableVersion:          "3.6",
				SystemdServiceEnabled:     true,
				SystemdServiceRunning:     true,
				ConfigurationFilePath:     "/etc/google-cloud-sap-agent/configuration.json",
				ConfigurationValid:        true,
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
								Granted: true,
							},
							{
								Name:    "Monitoring Viewer",
								Role:    "roles/monitoring.viewer",
								Granted: true,
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
				SystemdServiceEnabled:     false,
				SystemdServiceRunning:     false,
				ConfigurationFilePath:     "/etc/google-cloud-sap-agent/configuration.json",
				ConfigurationValid:        false,
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
								Granted: true,
							},
							{
								Name:    "Monitoring Viewer",
								Role:    "roles/monitoring.viewer",
								Granted: false,
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
    Monitoring Viewer (roles/monitoring.viewer): False
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
			// Monkey patch the print functions to check the output.
			var buf bytes.Buffer
			writeToBuf := func(format string, args ...any) {
				buf.WriteString(fmt.Sprintf(format, args...))
			}
			defer func(infoOld func(format string, a ...any), successOld func(format string, a ...any), failureOld func(format string, a ...any), faintOld func(format string, a ...any), hyperlinkOld func(format string, a ...any)) {
				info = infoOld
				success = successOld
				failure = failureOld
				faint = faintOld
				hyperlink = hyperlinkOld
			}(info, success, failure, faint, hyperlink)
			info = writeToBuf
			success = writeToBuf
			failure = writeToBuf
			faint = writeToBuf
			hyperlink = writeToBuf

			PrintStatus(context.Background(), tc.status)
			got := buf.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("PrintStatus(%v) had unexpected diff (-want +got):\n%s", tc.status, diff)
			}
		})
	}
}
