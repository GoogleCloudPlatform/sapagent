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

package hanadiskbackuphandler

import (
	"context"
	"testing"

	"github.com/google/subcommands"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestHANADiskBackupHandler(t *testing.T) {
	tests := []struct {
		name           string
		command        *gpb.AgentCommand
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name: "InvalidParameters",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{},
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "FailureForCloudMonitoring",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"sid":              "test-sid",
					"hdbuserstore-key": "test-hdbuserstore-key",
					"project":          "test-project",
					"source-disk-zone": "test-zone",
				},
			},
			wantExitStatus: subcommands.ExitFailure,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, exitStatus, _ := HANADiskBackupHandler(context.Background(), tc.command, nil)
			if exitStatus != tc.wantExitStatus {
				t.Errorf("HANADiskBackupHandler(%v) = %q, want: %q", tc.command, exitStatus, tc.wantExitStatus)
			}
		})
	}
}
