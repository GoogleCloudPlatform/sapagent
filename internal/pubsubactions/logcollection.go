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

// LogCollectionHandler is the handler for the log collection pubsub subscription.
func (lc *LogCollector) LogCollectionHandler(ctx context.Context, actionsSubID, topicID string) error {
	usagemetrics.Action(usagemetrics.LogCollectionStarted)
	client, err := pubsub.NewClient(ctx, lc.CloudProperties.GetProjectId())
	if err != nil {
		return err
	}
	defer client.Close()

	actionsSub := client.Subscription(actionsSubID)
	ok, err := actionsSub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subscription %s exists: %v", actionsSubID, err)
	}
	if !ok {
		return fmt.Errorf("subscription %s does not exist", actionsSubID)
	}
	actionsSub.ReceiveSettings.MaxExtension = 15 * time.Minute

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

		if err = lc.publishEvent(ctx, bundlePath, topicID, logMessage); err != nil {
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

func (lc *LogCollector) publishEvent(ctx context.Context, bundlePath, topicID string, action ActionMessage) error {
	log.CtxLogger(ctx).Infow("Publishing event to event topic", "topicID", topicID)
	client, err := pubsub.NewClient(ctx, lc.CloudProperties.GetProjectId())
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
	topic := client.Topic(topicID)
	result := topic.Publish(ctx, &pubsub.Message{Data: data})

	_, err = result.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish event back to event topic: %v", err)
	}
	return nil
}

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

// fetchSAPDetails fetches the SAP details from the SAP system: SID, hostname, instance numbers.
func (lc *LogCollector) fetchSAPDetails(ctx context.Context, logMessage ActionMessage) (string, string, string) {
	return logMessage.SAPDetails.SID, logMessage.SAPDetails.Hostname, logMessage.SAPDetails.InstanceNums
}
