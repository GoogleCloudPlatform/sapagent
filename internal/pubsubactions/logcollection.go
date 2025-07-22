/*
Copyright 2025 Google LLC

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

package pubsubactions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/subcommands" // LogCollector is responsible for collecting logs from the SAP system and uploading them to the customer GCS bucket and push to pubsub.
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics" // GCEDetails represents the structure for gce_details
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// LogCollector is responsible for collecting logs from the SAP system and uploading them to the customer GCS bucket and push to pubsub.
	LogCollector struct {
		Config          *cpb.Configuration
		CloudProperties *iipb.CloudProperties
	}

	// GCEDetails represents the structure for gce_details
	GCEDetails struct {
		ProjectID    string `json:"project_id"`
		Zone         string `json:"zone"`
		InstanceID   string `json:"instance_id"`
		InstanceName string `json:"instance_name"`
		GCSBucket    string `json:"gcs_bucket"`
	}

	// SAPDetails represents the structure for sap_details
	SAPDetails struct {
		SID           string `json:"sid"`
		Hostname      string `json:"hostname"`
		InstanceNums  string `json:"instance_nums"`
		DetectedIssue string `json:"detected_issue"`
	}

	// ActionMessage represents the top-level structure of the JSON
	ActionMessage struct {
		AlertID        string     `json:"alert_id"`
		EventType      string     `json:"event_type"`
		EventTimestamp time.Time  `json:"event_timestamp"`
		EventSource    string     `json:"event_source"`
		Description    string     `json:"description"`
		GCEDetails     GCEDetails `json:"gce_details"`
		SAPDetails     SAPDetails `json:"sap_details"`
		Agents         []string   `json:"agents"`
		ActionScript   []string   `json:"action_script"`
	}

	// EventTopicMessage represents the structure for the event topic message
	EventTopicMessage struct {
		AlertID        string     `json:"alert_id"`
		EventType      string     `json:"event_type"`
		EventTimestamp time.Time  `json:"event_timestamp"`
		EventSource    string     `json:"event_source"`
		Description    string     `json:"description"`
		GCEDetails     GCEDetails `json:"gce_details"`
		SAPDetails     SAPDetails `json:"sap_details"`
		LogPath        string     `json:"log_path"`
	}

	// LogCollectionParameters represents the parameters for the log collection routine.
	LogCollectionParameters struct {
		ActionsSubID string
		TopicID      string
	}
)

type (
	// PubSubClientIface is an interface for the pubsub client for testing purposes.
	PubSubClientIface interface {
		Subscription(string) PubSubSubscriptionIface
		Topic(string) PubSubTopicIface
		Close() error
	}

	// PubSubSubscriptionIface is an interface for the pubsub subscription for testing purposes.
	PubSubSubscriptionIface interface {
		Exists(context.Context) (bool, error)
		Receive(context.Context, func(context.Context, *pubsub.Message)) error
		SetMaxExtension(time.Duration)
	}

	// PubSubTopicIface is an interface for the pubsub topic for testing purposes.
	PubSubTopicIface interface {
		Publish(context.Context, *pubsub.Message) PubSubResultIface
		Exists(context.Context) (bool, error)
		GetTopic() *pubsub.Topic
	}

	// PubSubResultIface is an interface for the pubsub publish result for testing purposes.
	PubSubResultIface interface {
		Get(context.Context) (string, error)
	}

	pubsubClient struct {
		client *pubsub.Client
	}

	pubsubSubscription struct {
		subscription *pubsub.Subscription
	}

	pubsubTopic struct {
		topic *pubsub.Topic
	}

	// CreateClient is a function that creates a new pubsub client.
	CreateClient func(ctx context.Context, projectID string) (PubSubClientIface, error)
)

// Subscription returns a pubsub subscription.
func (p pubsubClient) Subscription(subID string) PubSubSubscriptionIface {
	return pubsubSubscription{subscription: p.client.Subscription(subID)}
}

// Topic returns a pubsub topic.
func (p pubsubClient) Topic(topicID string) PubSubTopicIface {
	return pubsubTopic{topic: p.client.Topic(topicID)}
}

// Close closes the pubsub client.
func (p pubsubClient) Close() error {
	return p.client.Close()
}

// Exists checks if the pubsub subscription exists.
func (p pubsubSubscription) Exists(ctx context.Context) (bool, error) {
	return p.subscription.Exists(ctx)
}

// Receive receives messages from the pubsub subscription.
func (p pubsubSubscription) Receive(ctx context.Context, f func(context.Context, *pubsub.Message)) error {
	return p.subscription.Receive(ctx, f)
}

// SetMaxExtension sets the maximum extension for the pubsub subscription.
func (p pubsubSubscription) SetMaxExtension(d time.Duration) {
	p.subscription.ReceiveSettings.MaxExtension = d
}

// Publish publishes a message to the pubsub topic.
func (p pubsubTopic) Publish(ctx context.Context, msg *pubsub.Message) PubSubResultIface {
	return p.topic.Publish(ctx, msg)
}

// Exists checks if the topic exists.
func (p pubsubTopic) Exists(ctx context.Context) (bool, error) {
	return p.topic.Exists(ctx)
}

// GetTopic returns the pubsub topic.
func (p pubsubTopic) GetTopic() *pubsub.Topic {
	return p.topic
}

// LogCollectionHandler is the handler for the log collection pubsub subscription.
func (lc *LogCollector) LogCollectionHandler(ctx context.Context, actionsSubID, topicID string, createClient CreateClient) error {
	usagemetrics.Action(usagemetrics.LogCollectionStarted)
	client, err := createClient(ctx, lc.CloudProperties.GetProjectId())
	if err != nil {
		return err
	}
	defer client.Close()

	actionsSub, err := getSubscription(ctx, client, actionsSubID)
	if err != nil {
		return err
	}

	log.CtxLogger(ctx).Infow("Listening for messages on subscription", "subscription", actionsSubID)
	pullCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	err = actionsSub.Receive(pullCtx, func(_ context.Context, msg *pubsub.Message) {
		var logMessage ActionMessage
		err := json.Unmarshal(msg.Data, &logMessage)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to unmarshal Pub/Sub message data into PubSubLogMessage",
				"error", err, "messageID", msg.ID, "raw_data", string(msg.Data))
			msg.Nack()
			return
		}

		if !lc.validateMessage(ctx, logMessage) {
			log.CtxLogger(ctx).Debugw("Invalid message", "messageID", msg.ID)
			msg.Nack()
			return
		}

		var bundlePath string
		if bundlePath, err = lc.bundleCollection(ctx, logMessage); err != nil {
			log.CtxLogger(ctx).Errorw("Failed to collect bundle", "error", err)
			msg.Nack()
			return
		}

		log.CtxLogger(ctx).Infow("Successfully collected bundle, acknowledging message", "bundlePath", bundlePath)
		msg.Ack()

		if err = lc.publishEvent(ctx, bundlePath, topicID, logMessage, createClient); err != nil {
			log.CtxLogger(ctx).Errorw("Failed to publish event", "error", err)
			msg.Nack()
			return
		}
		log.CtxLogger(ctx).Infow("Successfully processed message", "messageID", msg.ID)
	})

	if err != nil && err != context.Canceled {
		log.CtxLogger(ctx).Errorw("Failed to receive messages on subscription", "error", err)
		return err
	}
	return nil
}

// bundleCollection collects the support bundle for the SAP system.
//
// It returns the path of the collected bundle and an error.
// The path of the collected bundle is in the format gs://<bucket>/supportbundle/<bundleName>.zip.
func (lc *LogCollector) bundleCollection(ctx context.Context, logMessage ActionMessage) (string, error) {
	log.CtxLogger(ctx).Info("Collecting bundle")
	sid, hostname, instanceNums := lc.fetchSAPDetails(ctx, logMessage)
	bundleName := fmt.Sprintf("logcollection-supportbundle-%s", strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1))
	supportbundleArgs := supportbundle.SupportBundle{
		Sid:          sid,
		Hostname:     hostname,
		InstanceNums: instanceNums,
		ResultBucket: logMessage.GCEDetails.GCSBucket,
		Metrics:      true,
		BundleName:   bundleName,
		IIOTEParams: &onetime.InternallyInvokedOTE{
			InvokedBy: "logcollection",
			Cp:        lc.CloudProperties,
			Lp:        log.Parameters{},
		},
	}

	if status := supportbundleArgs.Execute(ctx, nil, nil); status != subcommands.ExitSuccess {
		return "", fmt.Errorf("failed to collect support bundle: %v", status)
	}
	bundlePath := fmt.Sprintf("gs://%s/supportbundle/%s.zip", logMessage.GCEDetails.GCSBucket, bundleName)
	return bundlePath, nil
}

// publishEvent publishes the event to the event topic.
func (lc *LogCollector) publishEvent(ctx context.Context, bundlePath, topicID string, action ActionMessage, createClient CreateClient) error {
	log.CtxLogger(ctx).Infow("Publishing event to event topic", "topicID", topicID)
	client, err := createClient(ctx, lc.CloudProperties.GetProjectId())
	if err != nil {
		return err
	}
	defer client.Close()

	event := EventTopicMessage{
		AlertID:        action.AlertID,
		EventType:      action.EventType,
		EventTimestamp: action.EventTimestamp,
		EventSource:    action.EventSource,
		Description:    action.Description,
		GCEDetails:     action.GCEDetails,
		SAPDetails:     action.SAPDetails,
		LogPath:        bundlePath,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	topic, err := getTopic(ctx, client, topicID)
	if err != nil {
		return err
	}

	if err := publishMessage(ctx, topic, topicID, data); err != nil {
		return err
	}
	return nil
}

// validateMessage validates the received message.
//
// It checks if the event type is "LOG_COLLECTION", if the instance ID matches the current instance ID,
// if the GCS bucket is not empty, and if the SAP details are not empty.
func (lc *LogCollector) validateMessage(ctx context.Context, logMessage ActionMessage) bool {
	if logMessage.EventType != "LOG_COLLECTION" || logMessage.GCEDetails.InstanceID != lc.CloudProperties.GetInstanceId() {
		log.CtxLogger(ctx).Infow("Invalid message", "eventType", logMessage.EventType, "instanceID", logMessage.GCEDetails.InstanceID, "currentInstanceID", lc.CloudProperties.GetInstanceId())
		return false
	}
	if logMessage.GCEDetails.GCSBucket == "" {
		log.CtxLogger(ctx).Infow("Invalid message", "gcsBucket", logMessage.GCEDetails.GCSBucket)
		return false
	}
	if logMessage.SAPDetails.SID == "" || logMessage.SAPDetails.Hostname == "" || logMessage.SAPDetails.InstanceNums == "" {
		log.CtxLogger(ctx).Infow("Invalid message", "sid", logMessage.SAPDetails.SID, "hostname", logMessage.SAPDetails.Hostname, "instanceNums", logMessage.SAPDetails.InstanceNums)
		return false
	}
	return true
}

// fetchSAPDetails fetches the SAP details from the ActionMessage: SID, hostname, instance numbers.
func (lc *LogCollector) fetchSAPDetails(ctx context.Context, logMessage ActionMessage) (string, string, string) {
	return logMessage.SAPDetails.SID, logMessage.SAPDetails.Hostname, logMessage.SAPDetails.InstanceNums
}

// createClient creates a new pubsub client.
func createClient(ctx context.Context, projectID string) (PubSubClientIface, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return pubsubClient{client: client}, nil
}

// getSubscription creates a reference to a pubsub subscription and sets the maximum extension for the subscription to 15 minutes.
func getSubscription(ctx context.Context, client PubSubClientIface, subID string) (PubSubSubscriptionIface, error) {
	sub := client.Subscription(subID)
	ok, err := sub.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if subscription %s exists: %v", subID, err)
	}
	if !ok {
		return nil, fmt.Errorf("subscription %s does not exist", subID)
	}
	sub.SetMaxExtension(15 * time.Minute)
	return sub, nil
}

// getTopic creates a reference to a pubsub topic.
func getTopic(ctx context.Context, client PubSubClientIface, topicID string) (PubSubTopicIface, error) {
	topic := client.Topic(topicID)
	ok, err := topic.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if topic %s exists: %v", topicID, err)
	}
	if !ok {
		return nil, fmt.Errorf("topic %s does not exist", topicID)
	}
	return pubsubTopic{topic: topic.GetTopic()}, nil
}

// publishMessage publishes a message to the pubsub topic.
func publishMessage(ctx context.Context, topic PubSubTopicIface, topicID string, data []byte) error {
	if topic.GetTopic() == nil {
		return fmt.Errorf("topic is nil")
	}
	result := topic.Publish(ctx, &pubsub.Message{Data: data})
	_, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message to topic %s: %v", topicID, err)
	}
	return nil
}
