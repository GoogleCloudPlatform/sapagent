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

// Package uap provides capability for Google Cloud Agents to communicate with Google Cloud Service Providers.
package uap

import (
	"context"
	"fmt"

	client "github.com/GoogleCloudPlatform/agentcommunication_client"
	"google.golang.org/api/option"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	anypb "google.golang.org/protobuf/types/known/anypb"
	acpb "github.com/GoogleCloudPlatform/agentcommunication_client/gapic/agentcommunicationpb"
)

const (
	succeeded = "SUCCEEDED"
	failed    = "FAILED"
)

var sendMessage = func(c *client.Connection, msg *acpb.MessageBody) error {
	return c.SendMessage(msg)
}

var receive = func(c *client.Connection) (*acpb.MessageBody, error) {
	return c.Receive()
}

var createConnection = func(ctx context.Context, channel string, regional bool, opts ...option.ClientOption) (*client.Connection, error) {
	return client.CreateConnection(ctx, channel, regional, opts...)
}

func sendStatusMessage(ctx context.Context, msgID string, body string, status string, conn *client.Connection) error {
	labels := map[string]string{"uap_message_type": "OPERATION_STATUS", "message_id": msgID, "state": status}
	messageToSend := &acpb.MessageBody{Labels: labels, Body: &anypb.Any{Value: []byte(body)}}
	log.CtxLogger(ctx).Infow("Sending status message to UAP.", "messageToSend", messageToSend)
	if err := sendMessage(conn, messageToSend); err != nil {
		return fmt.Errorf("Error sending status message to UAP: %v", err)
	}
	return nil
}

func listenForMessages(ctx context.Context, conn *client.Connection) *acpb.MessageBody {
	log.CtxLogger(ctx).Info("Listening for messages from UAP Highway.")
	msg, err := receive(conn)
	if err != nil {
		log.CtxLogger(ctx).Error(err)
		return nil
	}
	log.CtxLogger(ctx).Infow("UAP Message received.", "msg", msg)
	return msg
}

func establishConnection(ctx context.Context, endpoint string, channel string) *client.Connection {
	log.CtxLogger(ctx).Infow("Establishing connection with UAP Highway.", "channel", channel)
	opts := []option.ClientOption{}
	if endpoint != "" {
		log.CtxLogger(ctx).Infow("Using non-default endpoint.", "endpoint", endpoint)
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	conn, err := createConnection(ctx, channel, false, opts...)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to establish connection to UAP.", "err", err)
	}
	log.CtxLogger(ctx).Info("Connected to UAP Highway.")
	return conn
}

// CommunicateWithUAP establishes ongoing communication with UAP Highway.
// "endpoint" is the endpoint and will often be an empty string.
// "channel" is the registered channel name to be used for communication
// between the agent and the service provider.
// "messageHandler" is the function that the agent will use to handle incoming messages.
func CommunicateWithUAP(ctx context.Context, endpoint string, channel string, messageHandler func(context.Context, string) (string, error)) {
	conn := establishConnection(ctx, endpoint, channel)
	for {
		// listen for messages
		msg := listenForMessages(ctx, conn)

		// parse message
		if msg.GetLabels() == nil {
			log.CtxLogger(ctx).Warn("Nil labels in message from listenForMessages.")
			continue
		}
		msgID, ok := msg.GetLabels()["message_id"]
		if !ok {
			log.CtxLogger(ctx).Warn("No message_id label in message.")
			continue
		}
		log.CtxLogger(ctx).Infow("Parsed id of message from label.", "msgID", msgID)
		command := string(msg.GetBody().Value[:])
		log.CtxLogger(ctx).Infow("Parsed command of message from body.", "command", command)

		// handle the message
		responseMsg, err := messageHandler(ctx, command)
		statusMsg := succeeded
		if err != nil {
			log.CtxLogger(ctx).Infow("Encountered error during UAP message handling.", "err", err)
			statusMsg = failed
		}
		log.CtxLogger(ctx).Infow("Message handling complete.", "responseMsg", responseMsg, "statusMsg", statusMsg)

		// Send operation status message
		err = sendStatusMessage(ctx, msgID, responseMsg, statusMsg, conn)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Encountered error during sendStatusMessage.", "err", err)
		}
	}
}
