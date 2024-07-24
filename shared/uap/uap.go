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
	"time"

	client "github.com/GoogleCloudPlatform/agentcommunication_client"
	backoff "github.com/cenkalti/backoff/v4"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	anypb "google.golang.org/protobuf/types/known/anypb"
	acpb "github.com/GoogleCloudPlatform/agentcommunication_client/gapic/agentcommunicationpb"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	succeeded = "SUCCEEDED"
	failed    = "FAILED"
)

type (
	// MessageHandlerFunc is the function that the agent will use to handle incoming messages.
	MessageHandlerFunc func(context.Context, *gpb.GuestActionRequest, *ipb.CloudProperties) *gpb.GuestActionResponse
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

func sendStatusMessage(ctx context.Context, operationID string, body *anypb.Any, status string, conn *client.Connection) error {
	labels := map[string]string{
		"operation_id": operationID,
		"state":        status,
		"lro_state":    "done",
	}
	messageToSend := &acpb.MessageBody{Labels: labels, Body: body}
	log.CtxLogger(ctx).Debugw("Sending status message to UAP.", "messageToSend", messageToSend)
	if err := sendMessage(conn, messageToSend); err != nil {
		return fmt.Errorf("error sending status message to UAP: %v", err)
	}
	return nil
}

func listenForMessages(ctx context.Context, conn *client.Connection, endpoint string, channel string) *acpb.MessageBody {
	log.CtxLogger(ctx).Debugw("Listening for messages from UAP Highway.", "endpoint", endpoint, "channel", channel)
	msg, err := receive(conn)
	if err != nil {
		log.CtxLogger(ctx).Warn(err)
		return nil
	}
	log.CtxLogger(ctx).Debugw("UAP Message received.", "msg", msg)
	return msg
}

func establishConnection(ctx context.Context, endpoint string, channel string) *client.Connection {
	log.CtxLogger(ctx).Infow("Establishing connection with UAP Highway.", "endpoint", endpoint, "channel", channel)
	opts := []option.ClientOption{}
	if endpoint != "" {
		log.CtxLogger(ctx).Infow("Using non-default endpoint.", "endpoint", endpoint)
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	conn, err := createConnection(ctx, channel, true, opts...)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Failed to establish connection to UAP.", "err", err)
	}
	log.CtxLogger(ctx).Info("Connected to UAP Highway.")
	return conn
}

func setupBackoff() backoff.BackOff {
	b := &backoff.ExponentialBackOff{
		InitialInterval:     2 * time.Second,
		RandomizationFactor: 0,
		Multiplier:          2,
		MaxInterval:         1 * time.Hour,
		MaxElapsedTime:      0,
		Clock:               backoff.SystemClock,
	}
	b.Reset()
	return b
}

func logAndBackoff(ctx context.Context, eBackoff backoff.BackOff, msg string) {
	duration := eBackoff.NextBackOff()
	log.CtxLogger(ctx).Infow(msg, "duration", duration)
	time.Sleep(duration)
}

func anyResponse(ctx context.Context, gar *gpb.GuestActionResponse) *anypb.Any {
	any, err := anypb.New(gar)
	if err != nil {
		log.CtxLogger(ctx).Infow("Failed to marshal response to any.", "err", err)
		any = &anypb.Any{}
	}
	return any
}

func parseRequest(ctx context.Context, msg *anypb.Any) (*gpb.GuestActionRequest, error) {
	gaReq := &gpb.GuestActionRequest{}
	if err := msg.UnmarshalTo(gaReq); err != nil {
		errMsg := fmt.Sprintf("failed to unmarshal message: %v", err)
		return nil, fmt.Errorf(errMsg)
	}
	log.CtxLogger(ctx).Debugw("successfully unmarshalled message.", "gar", prototext.Format(gaReq))
	return gaReq, nil
}

// CommunicateWithUAP establishes ongoing communication with UAP Highway.
// "endpoint" is the endpoint and will often be an empty string.
// "channel" is the registered channel name to be used for communication
// between the agent and the service provider.
// "messageHandler" is the function that the agent will use to handle incoming messages.
func CommunicateWithUAP(ctx context.Context, endpoint string, channel string, messageHandler MessageHandlerFunc, cloudProperties *ipb.CloudProperties) error {
	eBackoff := setupBackoff()
	conn := establishConnection(ctx, endpoint, channel)
	for conn == nil {
		logMsg := fmt.Sprintf("Establishing connection failed. Will backoff and retry.")
		logAndBackoff(ctx, eBackoff, logMsg)
		conn = establishConnection(ctx, endpoint, channel)
	}
	// Reset backoff once we successfully connected.
	eBackoff.Reset()

	// Establish a way for the method to be interrupted for unit testing purposes.
	done := false
	go func() {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("Context is done. Setting done to true.")
			done = true
		}
	}()

	var lastErr error
	for {
		if done {
			log.CtxLogger(ctx).Info("Done is true. Returning.")
			return lastErr
		}
		// listen for messages
		msg := listenForMessages(ctx, conn, endpoint, channel)
		log.CtxLogger(ctx).Infow("ListenForMessages complete.", "msg", prototext.Format(msg))
		// parse message
		if msg.GetLabels() == nil {
			logMsg := fmt.Sprintf("Nil labels in message from listenForMessages. Will backoff and retry with a new connection.")
			logAndBackoff(ctx, eBackoff, logMsg)
			conn = establishConnection(ctx, endpoint, channel)
			lastErr = fmt.Errorf("nil labels in message from listenForMessages")
			continue
		}
		operationID, ok := msg.GetLabels()["operation_id"]
		if !ok {
			logMsg := fmt.Sprintf("No operation_id label in message. Will backoff and retry.")
			logAndBackoff(ctx, eBackoff, logMsg)
			lastErr = fmt.Errorf("no operation_id label in message")
			continue
		}
		// Reset backoff if we successfully parsed the message.
		eBackoff.Reset()

		log.CtxLogger(ctx).Debugw("Parsed operation_id from label.", "operation_id", operationID)

		gaReq, err := parseRequest(ctx, msg.GetBody())
		if err != nil {
			logMsg := fmt.Sprintf("Encountered error during parseRequest. Will backoff and retry. err: %v", err)
			logAndBackoff(ctx, eBackoff, logMsg)
			lastErr = err
			continue
		}
		// handle the message
		gaRes := messageHandler(ctx, gaReq, cloudProperties)
		statusMsg := succeeded
		if gaRes.GetError().GetErrorMessage() != "" {
			log.CtxLogger(ctx).Warnw("Encountered error during UAP message handling.", "err", gaRes.GetError().GetErrorMessage())
			statusMsg = failed
		}
		log.CtxLogger(ctx).Debugw("Message handling complete.", "responseMsg", prototext.Format(gaRes), "statusMsg", statusMsg)
		anyGar := anyResponse(ctx, gaRes)
		// Send operation status message.
		err = sendStatusMessage(ctx, operationID, anyGar, statusMsg, conn)
		if err != nil {
			log.CtxLogger(ctx).Warnw("Encountered error during sendStatusMessage.", "err", err)
			lastErr = err
		}
	}
}
