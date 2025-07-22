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
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	mockPubSubClient struct {
		mockSubscription *mockPubSubSubscription
		mockTopic        *mockPubSubTopic
	}

	mockPubSubSubscription struct {
		exists       bool
		existsError  error
		receiveError error
	}

	mockPubSubTopic struct {
		topic         *pubsub.Topic
		publishResult *mockPubSubResult
		exists        bool
		existsError   error
	}

	mockPubSubResult struct {
		result      string
		resultError error
	}
)

func (m *mockPubSubClient) Subscription(subID string) PubSubSubscriptionIface {
	return m.mockSubscription
}

func (m *mockPubSubClient) Topic(topicID string) PubSubTopicIface {
	return m.mockTopic
}

func (m *mockPubSubClient) Close() error {
	return nil
}

func (m *mockPubSubSubscription) Exists(ctx context.Context) (bool, error) {
	return m.exists, m.existsError
}

func (m *mockPubSubSubscription) Receive(ctx context.Context, f func(context.Context, *pubsub.Message)) error {
	return m.receiveError
}

func (m *mockPubSubSubscription) SetMaxExtension(d time.Duration) {
	return
}

func (m *mockPubSubTopic) Publish(ctx context.Context, msg *pubsub.Message) PubSubResultIface {
	return m.publishResult
}

func (m *mockPubSubTopic) Exists(ctx context.Context) (bool, error) {
	return m.exists, m.existsError
}

func (m *mockPubSubTopic) GetTopic() *pubsub.Topic {
	return m.topic
}

func (m *mockPubSubResult) Get(ctx context.Context) (string, error) {
	return m.result, m.resultError
}

var (
	sampleActionMessage = ActionMessage{
		EventType: "LOG_COLLECTION",
		GCEDetails: GCEDetails{
			InstanceID: "test-instance",
			GCSBucket:  "test-bucket",
		},
		SAPDetails: SAPDetails{
			SID:          "test-sid",
			Hostname:     "test-hostname",
			InstanceNums: "00",
		},
	}
)

func TestCreateClient(t *testing.T) {
	ctx := context.Background()
	projectID := "test-project"
	_, err := createClient(ctx, projectID)
	wantErr := cmpopts.AnyError

	if !cmp.Equal(err, wantErr, cmpopts.EquateErrors()) {
		t.Errorf("createClient(%v, %v) returned error: %v, want error: %v", ctx, projectID, err, wantErr)
	}
}

func TestGetSubscription(t *testing.T) {
	tests := []struct {
		name    string
		client  PubSubClientIface
		subID   string
		wantErr error
	}{
		{
			name: "SubscriptionExists",
			client: &mockPubSubClient{
				mockSubscription: &mockPubSubSubscription{
					exists:      true,
					existsError: nil,
				},
			},
			subID:   "test-subscription",
			wantErr: nil,
		},
		{
			name: "SubscriptionFailure",
			client: &mockPubSubClient{
				mockSubscription: &mockPubSubSubscription{
					exists:      false,
					existsError: cmpopts.AnyError,
				},
			},
			subID:   "test-subscription",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SubscriptionDoesNotExist",
			client: &mockPubSubClient{
				mockSubscription: &mockPubSubSubscription{
					exists:      false,
					existsError: nil,
				},
			},
			subID:   "test-subscription",
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := getSubscription(ctx, tc.client, tc.subID)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getSubscription(%v, %v, %v) returned error: %v, want error: %v", ctx, tc.client, tc.subID, err, tc.wantErr)
			}
		})
	}
}

func TestGetTopic(t *testing.T) {
	tests := []struct {
		name    string
		client  PubSubClientIface
		topicID string
		wantErr error
	}{
		{
			name: "TopicFailure",
			client: &mockPubSubClient{
				mockTopic: &mockPubSubTopic{
					exists:      false,
					existsError: cmpopts.AnyError,
				},
			},
			topicID: "test-topic",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TopicDoesNotExist",
			client: &mockPubSubClient{
				mockTopic: &mockPubSubTopic{
					exists:      false,
					existsError: nil,
				},
			},
			topicID: "test-topic",
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := getTopic(ctx, tc.client, tc.topicID)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getTopic(%v, %v, %v) returned error: %v, want error: %v", ctx, tc.client, tc.topicID, err, tc.wantErr)
			}
		})
	}
}

func TestPublishMessage(t *testing.T) {
	tests := []struct {
		name    string
		topic   PubSubTopicIface
		topicID string
		data    []byte
		wantErr error
	}{
		{
			name: "PublishMessageFailure",
			topic: &mockPubSubTopic{
				publishResult: &mockPubSubResult{
					result:      "",
					resultError: cmpopts.AnyError,
				},
			},
			topicID: "test-topic",
			data:    []byte("test-data"),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "PublishMessageSuccess",
			topic: &mockPubSubTopic{
				topic: &pubsub.Topic{},
				publishResult: &mockPubSubResult{
					result:      "test-result",
					resultError: nil,
				},
			},
			topicID: "test-topic",
			data:    []byte("test-data"),
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			err := publishMessage(ctx, tc.topic, tc.topicID, tc.data)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("publishMessage(%v, %v, %v, %v) returned error: %v, want error: %v", ctx, tc.topic, tc.topicID, tc.data, err, tc.wantErr)
			}
		})
	}
}

func TestFetchSAPDetails(t *testing.T) {
	tests := []struct {
		name       string
		logMessage ActionMessage
		want1      string
		want2      string
		want3      string
	}{
		{
			name: "FetchSAPDetailsSuccess",
			logMessage: ActionMessage{
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want1: "test-sid",
			want2: "test-hostname",
			want3: "00",
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var lc LogCollector
			got1, got2, got3 := lc.fetchSAPDetails(ctx, tc.logMessage)
			if got1 != tc.want1 {
				t.Errorf("fetchSAPDetails(%v) = %q, want: %q", tc.logMessage, got1, tc.want1)
			}
			if got2 != tc.want2 {
				t.Errorf("fetchSAPDetails(%v) = %q, want: %q", tc.logMessage, got2, tc.want2)
			}
			if got3 != tc.want3 {
				t.Errorf("fetchSAPDetails(%v) = %q, want: %q", tc.logMessage, got3, tc.want3)
			}
		})
	}
}

func TestValidateMessage(t *testing.T) {
	tests := []struct {
		name       string
		logMessage ActionMessage
		want       bool
	}{
		{
			name: "ValidMessage",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want: true,
		},
		{
			name: "InvalidEventType",
			logMessage: ActionMessage{
				EventType: "INVALID_EVENT",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want: false,
		},
		{
			name: "InvalidInstanceID",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "invalid-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want: false,
		},
		{
			name: "MissingGCSBucket",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
				},
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want: false,
		},
		{
			name: "MissingSID",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					Hostname:     "test-hostname",
					InstanceNums: "00",
				},
			},
			want: false,
		},
		{
			name: "MissingHostname",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					SID:          "test-sid",
					InstanceNums: "00",
				},
			},
			want: false,
		},
		{
			name: "MissingInstanceNums",
			logMessage: ActionMessage{
				EventType: "LOG_COLLECTION",
				GCEDetails: GCEDetails{
					InstanceID: "test-instance",
					GCSBucket:  "test-bucket",
				},
				SAPDetails: SAPDetails{
					SID:      "test-sid",
					Hostname: "test-hostname",
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			lc := &LogCollector{
				CloudProperties: &iipb.CloudProperties{InstanceId: "test-instance"},
			}
			got := lc.validateMessage(ctx, tc.logMessage)
			if got != tc.want {
				t.Errorf("validateMessage(%v, %v) = %v, want: %v", ctx, tc.logMessage, got, tc.want)
			}
		})
	}
}

func TestPublishEvent(t *testing.T) {
	tests := []struct {
		name         string
		client       PubSubClientIface
		bundlePath   string
		topicID      string
		action       ActionMessage
		createClient CreateClient
		wantErr      error
	}{
		{
			name:       "CreateClientFailure",
			bundlePath: "test-bundle-path",
			topicID:    "test-topic",
			action:     sampleActionMessage,
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return nil, errors.New("create client error")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "TopicDoesNotExist",
			bundlePath: "test-bundle-path",
			topicID:    "test-topic",
			action:     sampleActionMessage,
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return &mockPubSubClient{
					mockTopic: &mockPubSubTopic{
						exists:      false,
						existsError: errors.New("topic does not exist"),
					},
				}, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "PublishEvent",
			client: &mockPubSubClient{
				mockTopic: &mockPubSubTopic{
					exists:      true,
					existsError: nil,
					publishResult: &mockPubSubResult{
						result:      "test-result",
						resultError: nil,
					},
				},
			},
			bundlePath: "test-bundle-path",
			topicID:    "test-topic",
			action:     sampleActionMessage,
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return &mockPubSubClient{
					mockTopic: &mockPubSubTopic{
						exists: true,
						publishResult: &mockPubSubResult{
							result: "test-result",
						},
					},
				}, nil
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			lc := &LogCollector{
				CloudProperties: &iipb.CloudProperties{ProjectId: "test-project"},
			}

			err := lc.publishEvent(ctx, tc.bundlePath, tc.topicID, tc.action, tc.createClient)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("publishEvent(%v, %v, %v, %v) returned error: %v, want error: %v", ctx, tc.bundlePath, tc.topicID, tc.action, err, tc.wantErr)
			}
		})
	}
}

func TestBundleCollection(t *testing.T) {
	tests := []struct {
		name           string
		logMessage     ActionMessage
		wantBundlePath string
		wantErr        error
	}{
		{
			name:           "BundleCollectionFailure",
			logMessage:     sampleActionMessage,
			wantBundlePath: "",
			wantErr:        cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			lc := &LogCollector{
				CloudProperties: &iipb.CloudProperties{ProjectId: "test-project"},
			}
			got, err := lc.bundleCollection(ctx, tc.logMessage)
			if got != tc.wantBundlePath {
				t.Errorf("bundleCollection(%v, %v) = %q, want: %q", ctx, tc.logMessage, got, tc.wantBundlePath)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("bundleCollection(%v, %v) returned error: %v, want error: %v", ctx, tc.logMessage, err, tc.wantErr)
			}
		})
	}
}

func TestLogCollectionHandler(t *testing.T) {
	tests := []struct {
		name         string
		client       PubSubClientIface
		actionsSubID string
		topicID      string
		createClient CreateClient
		wantErr      error
	}{
		{
			name:         "CreateClientFailure",
			actionsSubID: "test-actions-sub",
			topicID:      "test-topic",
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return nil, errors.New("create client error")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:         "GetSubscriptionFailure",
			actionsSubID: "test-actions-sub",
			topicID:      "test-topic",
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return &mockPubSubClient{
					mockSubscription: &mockPubSubSubscription{
						exists:      false,
						existsError: errors.New("subscription does not exist"),
					},
				}, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:         "ReceiveMessageFailure",
			actionsSubID: "test-actions-sub",
			topicID:      "test-topic",
			createClient: func(ctx context.Context, projectID string) (PubSubClientIface, error) {
				return &mockPubSubClient{
					mockSubscription: &mockPubSubSubscription{
						exists:       true,
						receiveError: errors.New("receive message error"),
					},
				}, nil
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			lc := &LogCollector{
				CloudProperties: &iipb.CloudProperties{ProjectId: "test-project"},
			}
			err := lc.LogCollectionHandler(ctx, tc.actionsSubID, tc.topicID, tc.createClient)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("LogCollectionHandler(%v, %v, %v) returned error: %v, want error: %v", ctx, tc.actionsSubID, tc.topicID, err, tc.wantErr)
			}
		})
	}
}
