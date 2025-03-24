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

package guestactions

import (
	"context"
	"errors"
	"fmt"
	"testing"

	anypb "google.golang.org/protobuf/types/known/anypb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

func TestAnyResponse(t *testing.T) {
	tests := []struct {
		name    string
		gar     *gpb.GuestActionResponse
		want    *anypb.Any
		wantErr bool
	}{
		{
			name: "Standard",
			gar: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_AgentCommand{
								AgentCommand: &gpb.AgentCommand{Command: "version"},
							},
						},
						Stdout:   fmt.Sprintf("Google Cloud Agent for SAP version %s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
						Stderr:   "",
						ExitCode: 0,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			want: &anypb.Any{
				TypeUrl: "type.googleapis.com/workloadagentplatform.sharedprotos.guestactions.GuestActionResponse",
			},
		},
		{
			name: "Nil",
			gar:  nil,
			want: &anypb.Any{
				TypeUrl: "type.googleapis.com/workloadagentplatform.sharedprotos.guestactions.GuestActionResponse",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			got := anyResponse(ctx, test.gar)
			if diff := cmp.Diff(test.want.GetTypeUrl(), got.GetTypeUrl()); diff != "" {
				t.Errorf("anyResponse(%v) returned diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name    string
		msg     *anypb.Any
		want    *gpb.GuestActionRequest
		wantErr bool
	}{
		{
			name: "Success",
			msg: &anypb.Any{
				TypeUrl: "type.googleapis.com/workloadagentplatform.sharedprotos.guestactions.GuestActionRequest",
				Value:   []byte{},
			},
			want:    &gpb.GuestActionRequest{},
			wantErr: false,
		},
		{
			name: "InvalidMsgValue",
			msg: &anypb.Any{
				TypeUrl: "type.googleapis.com/workloadagentplatform.sharedprotos.guestactions.GuestActionRequest",
				Value:   []byte("invalid"),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "InvalidMsgType",
			msg: &anypb.Any{
				TypeUrl: "type.googleapis.com/workloadagentplatform.invalid.proto.GuestActionRequest",
				Value:   []byte{},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := parseRequest(ctx, test.msg)
			if test.wantErr && err == nil {
				t.Errorf("parseRequest(%v) returned nil error, want error", test.name)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("parseRequest(%v) returned diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestHandleShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.Command
		want    *gpb.CommandResult
		execute commandlineexecutor.Execute
	}{
		{
			name: "ShellCommandError",
			command: &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
				},
			},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_ShellCommand{
						ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
					},
				},
				Stdout:   "",
				Stderr:   "Command executable: \"eecho\" not found.",
				ExitCode: 1,
			},
			execute: commandlineexecutor.ExecuteCommand,
		},
		{
			name: "ShellCommandErrorNonZeroStatus",
			command: &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
				},
			},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_ShellCommand{
						ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
					},
				},
				Stdout:   "",
				Stderr:   "",
				ExitCode: 3,
			},
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 3,
				}
			},
		},
		{
			name: "ShellCommandErrorZeroStatus",
			command: &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
				},
			},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_ShellCommand{
						ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
					},
				},
				Stdout:   "",
				Stderr:   "",
				ExitCode: 1,
			},
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error:    errors.New("command executable: \"eecho\" not found"),
					ExitCode: 0,
				}
			},
		},
		{
			name: "ShellCommandStderrZeroStatus",
			command: &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
				},
			},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_ShellCommand{
						ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
					},
				},
				Stdout:   "",
				Stderr:   "Command executable: \"eecho\" not found.",
				ExitCode: 1,
			},
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdErr:   "Command executable: \"eecho\" not found.",
					ExitCode: 0,
				}
			},
		},
		{
			name: "ShellCommandSuccess",
			command: &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
				},
			},
			want: &gpb.CommandResult{
				Command: &gpb.Command{
					CommandType: &gpb.Command_ShellCommand{
						ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
					},
				},
				Stdout:   "Hello World!\n",
				Stderr:   "",
				ExitCode: 0,
			},
			execute: commandlineexecutor.ExecuteCommand,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			got := handleShellCommand(ctx, test.command, test.execute)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("handleShellCommand(%v) returned diff (-want +got):\n%s", test.command, diff)
			}
		})
	}
}

func TestMessageHandler(t *testing.T) {
	tests := []struct {
		name    string
		message *gpb.GuestActionRequest
		anyMsg  *anypb.Any
		want    *gpb.GuestActionResponse
		wantErr bool
	}{
		{
			name:    "NoCommands",
			message: &gpb.GuestActionRequest{},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{},
				Error:          &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: false,
		},
		{
			name:    "BadRequest",
			message: &gpb.GuestActionRequest{},
			anyMsg:  &anypb.Any{TypeUrl: "type.googleapis.com/workloadagentplatform.sharedprotos.guestactions.GuestActionRequest", Value: []byte("invalid")},
			want:    &gpb.GuestActionResponse{},
			wantErr: true,
		},
		{
			name: "UnknownCommandType",
			message: &gpb.GuestActionRequest{
				Commands: []*gpb.Command{{}},
			},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					&gpb.CommandResult{
						Command:  nil,
						Stdout:   "received unknown command: ",
						Stderr:   "received unknown command: ",
						ExitCode: 1,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: true,
		},
		{
			name: "AgentCommand",
			message: &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_AgentCommand{
							AgentCommand: &gpb.AgentCommand{Command: "version"},
						},
					},
				},
			},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_AgentCommand{
								AgentCommand: &gpb.AgentCommand{Command: "version"},
							},
						},
						Stdout:   fmt.Sprintf("Google Cloud Agent for SAP version %s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
						Stderr:   "",
						ExitCode: 0,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: false,
		},
		{
			name: "UnknownAgentCommand",
			message: &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_AgentCommand{
							AgentCommand: &gpb.AgentCommand{Command: "unknown_command"},
						},
					},
				},
			},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_AgentCommand{
								AgentCommand: &gpb.AgentCommand{Command: "unknown_command"},
							},
						},
						Stdout: fmt.Sprintf("received unknown agent command: %s", prototext.Format(&gpb.Command{
							CommandType: &gpb.Command_AgentCommand{
								AgentCommand: &gpb.AgentCommand{Command: "unknown_command"},
							},
						})),
						Stderr: fmt.Sprintf("received unknown agent command: %s", prototext.Format(&gpb.Command{
							CommandType: &gpb.Command_AgentCommand{
								AgentCommand: &gpb.AgentCommand{Command: "unknown_command"},
							},
						})),
						ExitCode: 1,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: true,
		},
		{
			name: "ShellCommandError",
			message: &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_ShellCommand{
							ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
						},
					},
				},
			},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_ShellCommand{
								ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
							},
						},
						Stdout:   "",
						Stderr:   "Command executable: \"eecho\" not found.",
						ExitCode: 1,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: true,
		},
		{
			name: "ShellCommandSuccess",
			message: &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_ShellCommand{
							ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
						},
					},
				},
			},
			want: &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_ShellCommand{
								ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
							},
						},
						Stdout:   "Hello World!\n",
						Stderr:   "",
						ExitCode: 0,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			},
			wantErr: false,
		},
	}

	ga := &GuestActions{
		CancelFunc: func() {},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			msg, _ := anypb.New(test.message)
			if test.anyMsg != nil {
				msg = test.anyMsg
			}
			got, err := ga.messageHandler(ctx, msg, nil)
			wantAny, _ := anypb.New(test.want)
			if test.wantErr {
				if err == nil {
					t.Errorf("messageHandler(%v) returned nil error, want error", test.name)
				}
			} else if diff := cmp.Diff(wantAny, got, protocmp.Transform(), protocmp.IgnoreFields(&gpb.GuestActionError{}, "error_message")); diff != "" {
				t.Errorf("messageHandler(%v) returned diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestStartACSCommunication(t *testing.T) {
	tests := []struct {
		name   string
		config *cpb.Configuration
		want   bool
	}{
		{
			name:   "Default",
			config: &cpb.Configuration{},
			want:   false,
		},
		{
			name: "Disabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled: &wpb.BoolValue{Value: false},
				},
			},
			want: false,
		},
		{
			name: "Enabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled: &wpb.BoolValue{Value: true},
				},
			},
			want: true,
		},
		{
			name: "TestChannelEnabled",
			config: &cpb.Configuration{
				UapConfiguration: &cpb.UAPConfiguration{
					Enabled:            &wpb.BoolValue{Value: true},
					TestChannelEnabled: &wpb.BoolValue{Value: true},
				},
			},
			want: true,
		},
	}

	ctx := context.Background()
	ga := &GuestActions{
		CancelFunc: func() {},
	}

	for _, tc := range tests {
		if got := ga.StartACSCommunication(ctx, tc.config); got != tc.want {
			t.Errorf("StartACSCommunication(%v) = %v, want: %v", tc.config, got, tc.want)
		}
	}
}
