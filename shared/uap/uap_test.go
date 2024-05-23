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

package uap

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	anypb "google.golang.org/protobuf/types/known/anypb"
	apb "google.golang.org/protobuf/types/known/anypb"
	client "github.com/GoogleCloudPlatform/agentcommunication_client"
	acpb "github.com/GoogleCloudPlatform/agentcommunication_client/gapic/agentcommunicationpb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func wrapAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	any, err := anypb.New(m)
	if err != nil {
		t.Fatalf("anypb.New(%v) returned an unexpected error: %v", m, err)
	}
	return any
}

func TestListenForMessages(t *testing.T) {
	tests := []struct {
		name    string
		want    *acpb.MessageBody
		receive func(c *client.Connection) (*acpb.MessageBody, error)
	}{
		{
			name: "typical",
			want: &acpb.MessageBody{
				Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
				Body:   &apb.Any{Value: []byte("typical test body")},
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body:   &apb.Any{Value: []byte("typical test body")},
				}, nil
			},
		},
		{
			name: "emptyBody",
			want: &acpb.MessageBody{
				Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
				Body:   &apb.Any{},
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body:   &apb.Any{},
				}, nil
			},
		},
		{
			name: "noBody",
			want: &acpb.MessageBody{
				Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
				Body:   nil,
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body:   nil,
				}, nil
			},
		},
		{
			name: "emptyLabels",
			want: &acpb.MessageBody{
				Labels: map[string]string{},
				Body:   &apb.Any{Value: []byte("emptyLabels test body")},
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{},
					Body:   &apb.Any{Value: []byte("emptyLabels test body")},
				}, nil
			},
		},
		{
			name: "noLabels",
			want: &acpb.MessageBody{
				Body: &apb.Any{Value: []byte("noLabels test body")},
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Body: &apb.Any{Value: []byte("noLabels test body")},
				}, nil
			},
		},
		{
			name: "noLabels",
			want: &acpb.MessageBody{
				Body: &apb.Any{Value: []byte("noLabels test body")},
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Body: &apb.Any{Value: []byte("noLabels test body")},
				}, nil
			},
		},
		{
			name: "errorReceivingMessages",
			want: nil,
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return nil, fmt.Errorf("test error")
			},
		},
	}

	ctx := context.Background()
	conn := &client.Connection{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receive = test.receive
			got := listenForMessages(ctx, conn)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("listenForMessages() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSendStatusMessage(t *testing.T) {
	tests := []struct {
		name        string
		want        error
		sendMessage func(c *client.Connection, msg *acpb.MessageBody) error
	}{
		{
			name: "typical",
			want: nil,
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return nil
			},
		},
		{
			name: "errorSendingMessage",
			want: cmpopts.AnyError,
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return fmt.Errorf("test error")
			},
		},
	}

	ctx := context.Background()
	conn := &client.Connection{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sendMessage = test.sendMessage
			got := sendStatusMessage(ctx, "msgID", &apb.Any{Value: []byte("test status body")}, "status", conn)
			if diff := cmp.Diff(test.want, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("sendStatusMessage() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEstablishConnection(t *testing.T) {
	tests := []struct {
		name             string
		want             *client.Connection
		createConnection func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error)
	}{
		{
			name: "typical",
			want: &client.Connection{},
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
		},
		{
			name: "error",
			want: nil,
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return nil, cmpopts.AnyError
			},
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			createConnection = test.createConnection
			got := establishConnection(ctx, "endpoint", "channel")
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.IgnoreUnexported(client.Connection{})); diff != "" {
				t.Errorf("establishConnection() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommunicateWithUAP(t *testing.T) {
	tests := []struct {
		name             string
		want             string
		createConnection func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error)
		sendMessage      func(c *client.Connection, msg *acpb.MessageBody) error
		receive          func(c *client.Connection) (*acpb.MessageBody, error)
	}{
		{
			name: "typical",
			want: "",
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return nil
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body: wrapAny(t, &gpb.GuestActionRequest{
						Commands: []*gpb.Command{
							{
								CommandType: &gpb.Command_ShellCommand{
									ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
								},
							},
						},
					}),
				}, nil
			},
		},
		{
			name: "parseBadRequest",
			want: "failed to unmarshal message",
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return nil
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body:   &apb.Any{Value: []byte("bad request")},
				}, nil
			},
		},
		{
			name: "sendMessageError",
			want: "sendMessage error",
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return fmt.Errorf("sendMessage error")
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": "test message_id", "state": succeeded},
					Body: wrapAny(t, &gpb.GuestActionRequest{
						Commands: []*gpb.Command{
							{
								CommandType: &gpb.Command_ShellCommand{
									ShellCommand: &gpb.ShellCommand{Command: "echo", Args: "Hello World!"},
								},
							},
						},
					}),
				}, nil
			},
		},
		{
			name: "receiveError",
			want: "nil labels in message from listenForMessages",
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return nil
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return nil, fmt.Errorf("receive error")
			},
		},
		{
			name: "receiveMessageIdError",
			want: "no message_id label",
			createConnection: func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
				return &client.Connection{}, nil
			},
			sendMessage: func(c *client.Connection, msg *acpb.MessageBody) error {
				return nil
			},
			receive: func(c *client.Connection) (*acpb.MessageBody, error) {
				return &acpb.MessageBody{
					Labels: map[string]string{"uap_message_type": "OPERATION_STATUS", "state": succeeded},
				}, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			createConnection = test.createConnection
			sendMessage = test.sendMessage
			receive = test.receive
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			got := CommunicateWithUAP(ctx, "endpoint", "channel", func(context.Context, *gpb.GuestActionRequest) (*gpb.GuestActionResponse, bool) { return nil, false }, func(context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess })
			if got == nil {
				if test.want != "" {
					t.Errorf("CommunicateWithUAP() returned nil error, want error: %s", test.want)
				}
				return
			}
			if test.want == "" {
				if got.Error() != "" {
					t.Errorf("CommunicateWithUAP() returned error: %s, want nil error", got.Error())
				}
				return
			}
			gotStr := strings.ReplaceAll(got.Error(), "\u00a0", " ")
			// Check if the desired error string is contained in the actual error string.
			if strings.Contains(gotStr, test.want) {
				return
			}
			// If the desired error substring is not present, give an error showing the diff.
			if diff := cmp.Diff(test.want, gotStr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("CommunicateWithUAP() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
