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
	"testing"

	apb "google.golang.org/protobuf/types/known/anypb"
	client "github.com/GoogleCloudPlatform/agentcommunication_client"
	acpb "github.com/GoogleCloudPlatform/agentcommunication_client/gapic/agentcommunicationpb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
)

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
			got := sendStatusMessage(ctx, "msgID", "body", "status", conn)
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

	var want *client.Connection = nil
	got := establishConnection(ctx, "", "")

	if got != want {
		t.Errorf("Logger.log() expected error mismatch. got: %v want: %v", got, want)
	} else {
		t.Logf("Logger.log() expected error MATCH. got: %v want: %v", got, want)
	}
}
