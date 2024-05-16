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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"

	anypb "google.golang.org/protobuf/types/known/anypb"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func wrapAny(t *testing.T, m proto.Message) *anypb.Any {
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) returned an unexpected error: %v", m, err)
	}
	return any
}

func testHandleShellCommand(ctx context.Context, command *gpb.ShellCommand) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		StdOut:   "Hello World!",
		StdErr:   "",
		ExitCode: 0,
		Error:    nil,
	}
}

func TestMessageHandler(t *testing.T) {
	tests := []struct {
		name               string
		message            *anypb.Any
		handleShellCommand func(ctx context.Context, command *gpb.ShellCommand) commandlineexecutor.Result
		want               *anypb.Any
		wantErr            error
	}{
		{
			name: "UnmarshalError",
			message: wrapAny(t, &gpb.Command{
				CommandType: &gpb.Command_ShellCommand{
					ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
				},
			}),
			handleShellCommand: testHandleShellCommand,
			want:               nil,
			wantErr:            cmpopts.AnyError,
		},
		{
			name:               "NoCommands",
			message:            wrapAny(t, &gpb.GuestActionRequest{}),
			handleShellCommand: testHandleShellCommand,
			want: wrapAny(t, &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{},
				Error:          &gpb.GuestActionError{ErrorMessage: ""},
			}),
			wantErr: nil,
		},
		{
			name: "UnknownCommandType",
			message: wrapAny(t, &gpb.GuestActionRequest{
				Commands: []*gpb.Command{{}},
			}),
			handleShellCommand: testHandleShellCommand,
			want: wrapAny(t, &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{},
				Error:          &gpb.GuestActionError{ErrorMessage: ""},
			}),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AgentCommandNotImplemented",
			message: wrapAny(t, &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_AgentCommand{
							AgentCommand: &gpb.AgentCommand{Command: "version"},
						},
					},
				},
			}),
			handleShellCommand: testHandleShellCommand,
			want: wrapAny(t, &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{},
				Error:          &gpb.GuestActionError{ErrorMessage: ""},
			}),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ShellCommandError",
			message: wrapAny(t, &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_ShellCommand{
							ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
						},
					},
				},
			}),
			handleShellCommand: func(ctx context.Context, command *gpb.ShellCommand) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "Error: command not found",
					ExitCode: 1,
					Error:    errors.New("Error: command not found"),
				}
			},
			want: wrapAny(t, &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_ShellCommand{
								ShellCommand: &gpb.ShellCommand{Command: "eecho", Args: "Hello World!"},
							},
						},
						Stdout:   "",
						Stderr:   "Error: command not found",
						ExitCode: 1,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			}),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ShellCommandSuccess",
			message: wrapAny(t, &gpb.GuestActionRequest{
				Commands: []*gpb.Command{
					{
						CommandType: &gpb.Command_ShellCommand{
							ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
						},
					},
				},
			}),
			handleShellCommand: testHandleShellCommand,
			want: wrapAny(t, &gpb.GuestActionResponse{
				CommandResults: []*gpb.CommandResult{
					{
						Command: &gpb.Command{
							CommandType: &gpb.Command_ShellCommand{
								ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
							},
						},
						Stdout:   "Hello World!",
						Stderr:   "",
						ExitCode: 0,
					},
				},
				Error: &gpb.GuestActionError{ErrorMessage: ""},
			}),
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			// TODO: Allow commandlineexecutor to be mocked.
			tmp := handleShellCommand
			handleShellCommand = test.handleShellCommand
			defer func(tmp func(ctx context.Context, command *gpb.ShellCommand) commandlineexecutor.Result) {
				handleShellCommand = tmp
			}(tmp)
			got, err := messageHandler(ctx, test.message)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("messageHandler(%v) returned got error = %v, want %v", test.message, err, test.wantErr)
			}
			if diff := cmp.Diff(got, test.want, protocmp.Transform(), protocmp.IgnoreFields(&gpb.GuestActionError{}, "error_message")); diff != "" {
				t.Errorf("messageHandler(%v) returned diff (-want +got):\n%s", test.message, diff)
			}
		})
	}
}
