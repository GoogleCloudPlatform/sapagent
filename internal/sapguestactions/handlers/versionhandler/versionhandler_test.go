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

// Package versionhandler implements the handler for the version command.
package versionhandler

import (
	"context"
	"testing"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

func TestVersionHandler(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.Command
		want    *gpb.CommandResult
	}{
		{
			name:    "Success",
			command: &gpb.Command{},
			want: &gpb.CommandResult{
				Command:  &gpb.Command{},
				Stdout:   onetime.GetAgentVersion(),
				ExitCode: int32(subcommands.ExitSuccess),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := VersionHandler(context.Background(), tc.command, nil)
			if got.GetExitCode() != tc.want.GetExitCode() {
				t.Errorf("VersionHandler(%v) got exit code: %d, want: %d", tc.command, got.GetExitCode(), tc.want.GetExitCode())
			}
			if got.GetStdout() != tc.want.GetStdout() {
				t.Errorf("VersionHandler(%v) got stdout: %s, want: %s", tc.command, got.GetStdout(), tc.want.GetStdout())
			}
		})
	}
}
