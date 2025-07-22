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

// Package pubsubactions is responsible for collecting logs from the SAP system and uploading them to the customer GCS bucket and push to pubsub.
package pubsubactions

// TODO: Add unit tests for this package.
import (
	"context"
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// PubSubActions is responsible for collecting logs from the SAP system and uploading them to the customer GCS bucket and push to pubsub.
	PubSubActions struct {
		Config          *cpb.Configuration
		CloudProperties *iipb.CloudProperties
	}
)

// Start starts the actions on pubsub.
func (ps *PubSubActions) Start(ctx context.Context) (err error) {
	actionsSubID := ps.Config.GetPubSubActions().GetActionsSubscriptionId()
	topicID := ps.Config.GetPubSubActions().GetTopicId()
	if actionsSubID == "" || topicID == "" {
		return fmt.Errorf("actions subscription ID or topic ID is not set, cannot start actions on pubsub")
	}

	lc := &LogCollector{
		Config:          ps.Config,
		CloudProperties: ps.CloudProperties,
	}
	lcParams := LogCollectionParameters{ActionsSubID: actionsSubID, TopicID: topicID}

	LogCollectionRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			if _, ok := a.(LogCollectionParameters); ok {
				lc.LogCollectionHandler(ctx, lcParams.ActionsSubID, lcParams.TopicID, createClient)
			}
		},
		RoutineArg:          lcParams,
		ErrorCode:           usagemetrics.LogCollectionFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 1 * time.Minute,
	}
	LogCollectionRoutine.StartRoutine(ctx)
	return nil
}
