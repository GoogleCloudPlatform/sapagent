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

package instancemetadatahandler

import (
	"context"
	"testing"

	apb "google.golang.org/protobuf/types/known/anypb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	impb "github.com/GoogleCloudPlatform/sapagent/protos/instancemetadata"
)

func emptyAnyInstanceMetadataResponse() *apb.Any {
	anyInstanceMetadataResponse, _ := apb.New(&impb.Metadata{})
	return anyInstanceMetadataResponse
}

func TestInstanceMetadataHandler(t *testing.T) {
	tests := []struct {
		name        string
		command     *gpb.Command
		cp          *ipb.CloudProperties
		want        *gpb.CommandResult
		wantRestart bool
	}{
		{
			name: "NegativeTestCase",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Command:    "instancemetadata",
						Parameters: map[string]string{},
					},
				},
			},
			cp: &ipb.CloudProperties{},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_AgentCommand{
						AgentCommand: &gpb.AgentCommand{
							Command: "instancemetadata",
						},
					},
				},
				Payload:  emptyAnyInstanceMetadataResponse(),
				Stdout:   "could not read OS release info, error: both ConfigFileReader and OSReleaseFilePath must be set",
				ExitCode: int32(subcommands.ExitFailure),
			},
			wantRestart: false,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotRestart := InstanceMetadataHandler(ctx, tc.command, tc.cp)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("InstanceMetadataHandler(%v, %v) returned an unexpected diff (-want +got): %v", tc.command, tc.cp, diff)
			}
			if gotRestart != tc.wantRestart {
				t.Errorf("InstanceMetadataHandler(%v, %v) = %v, want: %v", tc.command, tc.cp, gotRestart, tc.wantRestart)
			}
		})
	}
}
