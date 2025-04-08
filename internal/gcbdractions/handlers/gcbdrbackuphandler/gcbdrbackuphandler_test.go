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

package gcbdrbackuphandler

import (
	"context"
	"testing"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

func TestGCBDRBackupHandler(t *testing.T) {
	tests := []struct {
		name           string
		command        *gpb.Command
		wantExitStatus int32
	}{
		{
			name: "InvalidParameters",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{},
					},
				},
			},
			wantExitStatus: -1,
		},
		{
			name: "FailureForPrepare",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"operation-type":   "gcbdr-backup-prepare",
							"sid":              "sid",
							"hdbuserstore-key": "userstorekey",
						},
					},
				},
			},
			wantExitStatus: 127,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := GCBDRBackupHandler(context.Background(), tc.command, nil)
			if res.ExitCode != tc.wantExitStatus {
				t.Errorf("GCBDRBackupHandler(%v) = %v, want: %v", tc.command, res.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
