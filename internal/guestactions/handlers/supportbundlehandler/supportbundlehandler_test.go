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

package supportbundlehandler

import (
	"context"
	"testing"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestSupportBundleHandler(t *testing.T) {
	fakeExec := func(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{ExitCode: 0}
	}
	tests := []struct {
		name           string
		command        *gpb.Command
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name: "FailureCollectingErrors",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"sid":              "test-sid",
							"hostname":         "test-hostname",
							"instance-numbers": "00",
						},
					},
				},
			},
			wantExitStatus: subcommands.ExitFailure,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := runCommand(context.Background(), tc.command, nil, fakeExec)
			if res.ExitCode != int32(tc.wantExitStatus) {
				t.Errorf("runCommand(%v) = %q, want: %q", tc.command, res.ExitCode, tc.wantExitStatus)
			}
		})
	}
}
