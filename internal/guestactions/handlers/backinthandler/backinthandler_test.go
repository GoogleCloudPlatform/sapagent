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

package backinthandler

import (
	"context"
	"testing"

	"github.com/google/subcommands"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

func TestBackintHandler(t *testing.T) {
	tests := []struct {
		name           string
		command        *gpb.Command
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name: "UsageFaliureForNoUser",
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
			name: "UsageFaliureForNoParamFile",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"user": "test-user",
						},
					},
				},
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "UsageFaliureForNoFunction",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"user":      "test-user",
							"paramfile": "test-paramfile",
						},
					},
				},
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := BackintHandler(context.Background(), tc.command, nil)
			if res.ExitCode != int32(tc.wantExitStatus) {
				t.Errorf("BackintHandler(%v) = %q, want: %q", tc.command, res.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
