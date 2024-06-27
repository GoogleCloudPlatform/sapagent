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

package configureinstancehandler

import (
	"context"
	"testing"

	"github.com/google/subcommands"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestConfigureInstanceHandler(t *testing.T) {
	tests := []struct {
		name            string
		command         *gpb.Command
		cloudProperties *ipb.CloudProperties
		wantExitStatus  subcommands.ExitStatus
	}{
		{
			name: "FailureForInvalidParameters",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{},
					},
				},
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "FailureForLatestVersion",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":           "true",
							"overrideVersion": "latest",
							"hyperThreading":  "off",
							"overrideType":    "x4",
						},
					},
				},
			},
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "FailureFor3.3Version",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":           "true",
							"overrideVersion": "3.3",
							"hyperThreading":  "off",
						},
					},
				},
			},
			cloudProperties: &ipb.CloudProperties{
				MachineType: "x4",
			},
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "FailureFor3.4Version",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":           "true",
							"overrideVersion": "3.4",
							"hyperThreading":  "off",
						},
					},
				},
			},
			cloudProperties: &ipb.CloudProperties{
				MachineType: "x4",
			},
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "FailureForUnsupportedMachine",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":           "true",
							"overrideVersion": "3.4",
							"hyperThreading":  "off",
						},
					},
				},
			},
			cloudProperties: &ipb.CloudProperties{
				MachineType: "unsupported-machine",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "FailureForUnsupportedVersion",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":           "true",
							"overrideVersion": "unsupported-version",
							"hyperThreading":  "off",
						},
					},
				},
			},
			cloudProperties: &ipb.CloudProperties{
				MachineType: "x4",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := ConfigureInstanceHandler(context.Background(), tc.command, tc.cloudProperties)
			if result.ExitCode != int32(tc.wantExitStatus) {
				t.Errorf("ConfigureInstanceHandler(%v) = %v, want: %v", tc.command, result.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
