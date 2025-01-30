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
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
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
			name: "FailureForUnsupportedMachine",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"check":          "true",
							"hyperThreading": "off",
						},
					},
				},
			},
			cloudProperties: &ipb.CloudProperties{
				MachineType: "unsupported-machine",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := ConfigureInstanceHandler(context.Background(), tc.command, protostruct.ConvertCloudPropertiesToStruct(tc.cloudProperties))
			if result.ExitCode != int32(tc.wantExitStatus) {
				t.Errorf("ConfigureInstanceHandler(%v) = %v, want: %v", tc.command, result.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
