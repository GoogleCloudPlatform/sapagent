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

package configurehandler

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/google/subcommands"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

func TestConfigureHandler(t *testing.T) {
	tests := []struct {
		name           string
		command        *gpb.Command
		createConfig   bool
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name: "FailureForNoConfigurationJSON",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"loglevel": "debug",
						},
					},
				},
			},
			createConfig:   false,
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "UsageFailureForInvalidLogLevel",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"loglevel": "invalid-loglevel",
						},
					},
				},
			},
			createConfig:   true,
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "SuccessForValidLogLevel",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"loglevel": "debug",
						},
					},
				},
			},
			createConfig:   true,
			wantExitStatus: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.createConfig {
				dir := t.TempDir()
				filePath := path.Join(dir, "/configuration.json")
				tc.command.GetAgentCommand().Parameters["path"] = filePath
				f, err := os.Create(filePath)
				if err != nil {
					t.Fatalf("Failed to create configuration file: %v", err)
				}
				if err = os.WriteFile(filePath, []byte("{}"), os.ModePerm); err != nil {
					t.Fatalf("Failed to write to configuration file: %v", err)
				}
				defer os.Remove(f.Name())
			}
			res, _ := ConfigureHandler(context.Background(), tc.command, nil)
			if res.ExitCode != int32(tc.wantExitStatus) {
				t.Errorf("ConfigureHandler(%v) = %q, want: %q", tc.command, res.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
