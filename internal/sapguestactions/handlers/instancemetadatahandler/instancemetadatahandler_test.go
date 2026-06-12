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
	"fmt"
	"io"
	"testing"

	apb "google.golang.org/protobuf/types/known/anypb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/instancemetadata"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	impb "github.com/GoogleCloudPlatform/sapagent/protos/instancemetadata"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

func emptyAnyInstanceMetadataResponse() *apb.Any {
	anyInstanceMetadataResponse, _ := apb.New(&impb.Metadata{})
	return anyInstanceMetadataResponse
}

func fakeReadCloserError(path string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("error while reading file")
}

func TestInstanceMetadataHandlerHelper(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.Command
		cp      *ipb.CloudProperties
		trc     instancemetadata.ReadCloser
		want    *gpb.CommandResult
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
			cp:  &ipb.CloudProperties{},
			trc: fakeReadCloserError,
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_AgentCommand{
						AgentCommand: &gpb.AgentCommand{
							Command: "instancemetadata",
						},
					},
				},
				Payload:  emptyAnyInstanceMetadataResponse(),
				Stdout:   "could not read OS release info, error: error while reading file",
				ExitCode: int32(subcommands.ExitFailure),
			},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := instanceMetadataHandlerHelper(ctx, tc.command, protostruct.ConvertCloudPropertiesToStruct(tc.cp), tc.trc)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("InstanceMetadataHandler(%v, %v) returned an unexpected diff (-want +got): %v", tc.command, tc.cp, diff)
			}
		})
	}
}

func TestInstanceMetadataHandler(t *testing.T) {
	ctx := context.Background()
	command := &gpb.Command{
		CommandType: &gpb.Command_AgentCommand{
			AgentCommand: &gpb.AgentCommand{
				Command:    "instancemetadata",
				Parameters: map[string]string{},
			},
		},
	}
	cp := &metadataserver.CloudProperties{}

	got := InstanceMetadataHandler(ctx, command, cp)
	if got == nil {
		t.Errorf("InstanceMetadataHandler(%v, %v) returned nil", command, cp)
	}
}

func TestInstanceMetadataHandler_MarshalError(t *testing.T) {
	ctx := context.Background()
	command := &gpb.Command{
		CommandType: &gpb.Command_AgentCommand{
			AgentCommand: &gpb.AgentCommand{
				Command:    "instancemetadata",
				Parameters: map[string]string{},
			},
		},
	}
	cp := &metadataserver.CloudProperties{}

	// Stub apbNew to force a marshaling error
	originalApbNew := apbNew
	apbNew = func(src proto.Message) (*apb.Any, error) {
		return nil, fmt.Errorf("forced marshaling error")
	}
	defer func() { apbNew = originalApbNew }()

	got := instanceMetadataHandlerHelper(ctx, command, cp, nil)
	if got.ExitCode != int32(subcommands.ExitFailure) {
		t.Errorf("expected exit code %d, got %d", subcommands.ExitFailure, got.ExitCode)
	}
	if got.Stderr == "" {
		t.Error("expected non-empty Stderr for marshaling error")
	}
}
