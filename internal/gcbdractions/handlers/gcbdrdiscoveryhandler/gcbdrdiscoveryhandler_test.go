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

package gcbdrdiscoveryhandler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	apb "google.golang.org/protobuf/types/known/anypb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	hdpb "github.com/GoogleCloudPlatform/sapagent/protos/gcbdrhanadiscovery"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

var realanyNew = anyNew

func emptyAnyGCBDRDiscoveryResponse() *apb.Any {
	anyGCBDRDiscoveryResponse, _ := anyNew(&gpb.CommandResult{})
	return anyGCBDRDiscoveryResponse
}

func TestGCBDRDiscoveryHandler(t *testing.T) {
	tests := []struct {
		name                string
		command             *gpb.Command
		cp                  *metadataserver.CloudProperties
		mockExecutor        func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		wantResult          *gpb.CommandResult
		checkPayloadIsNil   bool
		checkStderrContains string
		expectedPayload     *apb.Any
		mockanyNew          func(proto.Message) (*apb.Any, error)
	}{
		{
			name: "ExitFailure",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{},
					},
				},
			},
			cp: &metadataserver.CloudProperties{},
			mockExecutor: func(_ context.Context, _ commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{ExitCode: 127, Error: fmt.Errorf("mock command execution failed"), StdErr: "mock command execution failed"}
			},
			wantResult: &gpb.CommandResult{
				ExitCode: 127,
			},
			checkPayloadIsNil:   true,
			checkStderrContains: "mock command execution failed",
			mockanyNew:          nil,
		},
		{
			name: "ExitSuccess",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{
						Parameters: map[string]string{
							"discover": "all",
						},
					},
				},
			},
			cp: &metadataserver.CloudProperties{},
			mockExecutor: func(_ context.Context, _ commandlineexecutor.Params) commandlineexecutor.Result {

				return commandlineexecutor.Result{ExitCode: 0, StdOut: "Script executed successfully."}
			},
			wantResult: &gpb.CommandResult{
				ExitCode: 0,
			},
			checkPayloadIsNil:   false,
			checkStderrContains: "Could not read the file for HANA discovery. file: /etc/google-cloud-sap-agent/gcbdr/SAPHANA.xml",
			expectedPayload:     mustNewAny(&hdpb.ApplicationsList{}),
			mockanyNew:          nil,
		},
		{
			name: "MarshalToAnyFails",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{Parameters: map[string]string{"discover": "all"}},
				},
			},
			cp: &metadataserver.CloudProperties{ProjectID: "test-project"},
			mockExecutor: func(_ context.Context, _ commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{ExitCode: 0, StdOut: "Script executed successfully."}
			},
			wantResult: &gpb.CommandResult{
				ExitCode: 0,
			},
			checkPayloadIsNil:   true,
			checkStderrContains: "Failed to marshal response to any. Error: forced anyNew error",
			expectedPayload:     nil,
			mockanyNew: func(m proto.Message) (*apb.Any, error) {
				return nil, fmt.Errorf("forced anyNew error")
			},
		},
		{
			name: "DefaultExecutorPath",
			command: &gpb.Command{
				CommandType: &gpb.Command_AgentCommand{
					AgentCommand: &gpb.AgentCommand{Parameters: map[string]string{"discover": "all"}},
				},
			},
			cp:           &metadataserver.CloudProperties{ProjectID: "test-project"},
			mockExecutor: nil,
			wantResult: &gpb.CommandResult{
				ExitCode: 127,
			},
			checkPayloadIsNil:   true,
			checkStderrContains: "/etc/google-cloud-sap-agent/gcbdr/discoverySAP.sh: No such file or directory",
			expectedPayload:     nil,
			mockanyNew:          nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalGlobalanyNew := anyNew
			defer func() { anyNew = originalGlobalanyNew }()

			if tc.mockanyNew != nil {
				anyNew = tc.mockanyNew
			} else {
				anyNew = realanyNew
			}

			originalHandlerExec := gcbdrDiscoveryHandlerExec
			defer func() { gcbdrDiscoveryHandlerExec = originalHandlerExec }()
			if tc.mockExecutor != nil {
				gcbdrDiscoveryHandlerExec = func() commandlineexecutor.Execute {
					return tc.mockExecutor
				}
			}
			res := GCBDRDiscoveryHandler(context.Background(), tc.command, tc.cp)
			expectedResult := proto.Clone(tc.wantResult).(*gpb.CommandResult)
			expectedResult.Command = tc.command

			cmpOpts := []cmp.Option{
				protocmp.Transform(),
				protocmp.IgnoreFields(&gpb.CommandResult{}, "payload", "stdout"),
			}
			if tc.checkStderrContains != "" {
				cmpOpts = append(cmpOpts, protocmp.IgnoreFields(&gpb.CommandResult{}, "stderr"))
			}

			if diff := cmp.Diff(expectedResult, res, cmpOpts...); diff != "" {
				t.Errorf("GCBDRDiscoveryHandler(%s) returned diff (-want +got):\n%s", tc.name, diff)
			}

			if tc.checkPayloadIsNil {
				if res.Payload != nil {
					t.Errorf("GCBDRDiscoveryHandler(%s): Payload is non-nil, want nil. Got: %v", tc.name, res.Payload)
				}
			} else {
				if res.Payload == nil {
					t.Errorf("GCBDRDiscoveryHandler(%s): Payload is nil, want non-nil", tc.name)
				} else if tc.expectedPayload != nil {
					if diff := cmp.Diff(tc.expectedPayload, res.Payload, protocmp.Transform()); diff != "" {
						t.Errorf("GCBDRDiscoveryHandler(%s) payload diff (-want +got):\n%s", tc.name, diff)
					}
				}
			}
			if tc.checkStderrContains != "" {
				if !strings.Contains(res.Stderr, tc.checkStderrContains) {
					t.Errorf("GCBDRDiscoveryHandler(%s): res.Stderr = %q, want to contain %q", tc.name, res.Stderr, tc.checkStderrContains)
				}
			} else if res.Stderr != tc.wantResult.GetStderr() {
				t.Errorf("GCBDRDiscoveryHandler(%s): res.Stderr = %q, want %q", tc.name, res.Stderr, tc.wantResult.GetStderr())
			}
		})
	}
}

func mustNewAny(m proto.Message) *apb.Any {
	anyVal, err := anyNew(m)
	if err != nil {
		panic(fmt.Sprintf("apb.New failed: %v", err))
	}
	return anyVal
}
func TestMain(m *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(m.Run())
}
