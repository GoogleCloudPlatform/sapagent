/*
Copyright 2022 Google LLC

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

package cluster

import (
	"context"
	"testing"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var defaultInstanceProperties = &InstanceProperties{
	Config: &cpb.Configuration{
		CloudProperties: &ipb.CloudProperties{
			ProjectId:  "test-project",
			Zone:       "test-zone",
			InstanceId: "test-instance",
		},
	},
	PMBackoffPolicy: defaultBOPolicy(context.Background()),
}

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestCollectNodeState(t *testing.T) {
	tests := []struct {
		name            string
		properties      *InstanceProperties
		fakeNodeState   readPacemakerNodeState
		wantValues      []int
		wantMetricCount int
		wantErr         error
	}{
		{
			name:       "StateUncleanAndShutdown",
			properties: defaultInstanceProperties,
			fakeNodeState: func(crm *pacemaker.CRMMon) (map[string]string, error) {
				return map[string]string{
					"test-instance-1": "unclean",
					"test-instance-2": "shutdown",
				}, nil
			},
			wantValues:      []int{nodeUnclean, nodeShutdown},
			wantMetricCount: 2,
		},
		{
			name:       "StateStandbyAndOnlineUnknown",
			properties: defaultInstanceProperties,
			fakeNodeState: func(crm *pacemaker.CRMMon) (map[string]string, error) {
				return map[string]string{
					"test-instance-1": "standby",
					"test-instance-2": "online",
					"test-instance-3": "seeking",
				}, nil
			},
			wantValues:      []int{nodeStandby, stateUnknown, nodeOnline},
			wantMetricCount: 3,
		},
		{
			name:       "PacemekerReadFailure",
			properties: defaultInstanceProperties,
			fakeNodeState: func(crm *pacemaker.CRMMon) (map[string]string, error) {
				return nil, cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MetricSkipped",
			properties: &InstanceProperties{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{
							nodesPath,
						},
					},
				},
				SkippedMetrics: map[string]bool{
					nodesPath: true,
				},
			},
			fakeNodeState: func(crm *pacemaker.CRMMon) (map[string]string, error) {
				return nil, nil
			},
			wantMetricCount: 0,
			wantValues:      nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues, gotErr := collectNodeState(context.Background(), test.properties, test.fakeNodeState, nil)
			diff := cmp.Diff(test.wantValues, gotValues, cmpopts.SortSlices(func(x, y int) bool { return x < y }))
			if diff != "" {
				t.Errorf("collectNodeState() returned unexpected diff (-want,+got): %s\n", diff)
			}

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectNodeState() returned unexpected error. got: %v, want: %v", gotErr, test.wantErr)
			}

			if len(gotMetrics) != test.wantMetricCount {
				t.Errorf("collectNodeState() returned unexpected number of metrics. got: %d, want: %d",
					len(gotMetrics), test.wantMetricCount)
			}
		})
	}
}

func TestCollectResourceState(t *testing.T) {
	tests := []struct {
		name              string
		properties        *InstanceProperties
		fakeResourceState readPacemakerResourceState
		wantValues        []int
		wantMetricCount   int
		wantErr           error
	}{
		{
			name:       "SuccessStartedStartingMasterSlave",
			properties: defaultInstanceProperties,
			fakeResourceState: func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error) {
				rs := []pacemaker.Resource{
					{
						Name: "resource1",
						Role: "Started",
						Node: "test-instance-1",
					},
					{
						Name: "resource2",
						Role: "Starting",
						Node: "test-instance-2",
					},
					{
						Name: "resource3",
						Role: "Master",
						Node: "test-instance-1",
					},
					{
						Name: "resource4",
						Role: "Slave",
						Node: "test-instance-2",
					},
				}
				return rs, nil
			},
			wantValues:      []int{resourceStarted, resourceStarting, resourceStarted, resourceStarted},
			wantMetricCount: 4,
		},
		{
			name:       "SuccessStoppedFailedUnknown",
			properties: defaultInstanceProperties,
			fakeResourceState: func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error) {
				rs := []pacemaker.Resource{
					{
						Name: "resource1",
						Role: "Stopped",
						Node: "test-instance-1",
					},
					{
						Name: "resource2",
						Role: "Failed",
						Node: "test-instance-2",
					},
					{
						Name: "resource3",
						Role: "Seek",
						Node: "test-instance-3",
					},
				}
				return rs, nil
			},
			wantValues:      []int{resourceStopped, resourceFailed, stateUnknown},
			wantMetricCount: 3,
		},
		{
			name:       "SuccessDuplicateResources",
			properties: defaultInstanceProperties,
			fakeResourceState: func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error) {
				rs := []pacemaker.Resource{
					{
						Name: "resource1",
						Role: "Stopped",
						Node: "test-instance-1",
					},
					{
						Name: "resource1",
						Role: "Stopped",
						Node: "test-instance-1",
					},
				}
				return rs, nil
			},
			wantValues:      []int{resourceStopped},
			wantMetricCount: 1,
		},
		{
			name:       "PacemakerReadFailure",
			properties: defaultInstanceProperties,
			fakeResourceState: func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error) {
				return nil, cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MetricSkipped",
			properties: &InstanceProperties{
				SAPInstance: &sapb.SAPInstance{Sapsid: "TST", Type: sapb.InstanceType_HANA},
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{resourcesPath},
					},
				},
				SkippedMetrics: map[string]bool{resourcesPath: true},
			},
			fakeResourceState: func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error) {
				rs := []pacemaker.Resource{
					{
						Name: "resource1",
						Role: "Stopped",
						Node: "test-instance-1",
					},
					{
						Name: "resource1",
						Role: "Stopped",
						Node: "test-instance-1",
					},
				}
				return rs, nil
			},
			wantValues:      nil,
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues, gotErr := collectResourceState(context.Background(), test.properties, test.fakeResourceState, nil)

			if diff := cmp.Diff(test.wantValues, gotValues); diff != "" {
				t.Errorf("resourceState() returned unexpected diff (-want,+got): %s\n", diff)
			}

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("resourceState() returned unexpected error. got: %v, want: %v", gotErr, test.wantErr)
			}

			if len(gotMetrics) != test.wantMetricCount {
				t.Errorf("resourceState() returned unexpected number of metrics. got: %d, want: %d",
					len(gotMetrics), test.wantMetricCount)
			}
		})
	}
}

func TestMetricLabels(t *testing.T) {
	tests := []struct {
		name        string
		extraLabels map[string]string
		want        map[string]string
	}{
		{
			name: "AddNodeLabel",
			extraLabels: map[string]string{
				"node": "test-instance-1",
			},
			want: map[string]string{
				"sid":  "TST",
				"type": "HANA",
				"node": "test-instance-1",
			},
		},
		{
			name: "NoExtraLabels",
			want: map[string]string{
				"sid":  "TST",
				"type": "HANA",
			},
		},
	}

	p := &InstanceProperties{
		SAPInstance: &sapb.SAPInstance{
			Sapsid: "TST",
			Type:   sapb.InstanceType_HANA,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := metricLabels(p, test.extraLabels)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("metricLabels() returned unexpected diff (-want,+got): %s\n", diff)
			}
		})
	}
}

// TestCollect is a negative test that checks Collect() returns empty
// metrics if pacemaker is not installed. In production control should
// never reach here.
func TestCollect(t *testing.T) {
	tests := []struct {
		name      string
		ip        *InstanceProperties
		wantCount int
		wantErr   error
	}{
		{
			name:      "EmptyMetricsWhenNoPacmaker",
			ip:        defaultInstanceProperties,
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.ip.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() = %v, want %v", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Collect() returned unexpected error. got: %v, want: %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollectFailCount(t *testing.T) {
	tests := []struct {
		name              string
		properties        *InstanceProperties
		fakeReadFailCount readPacemakerFailCount
		wantValues        []int
		wantMetricCount   int
		wantErr           error
	}{
		{
			name:       "SuccessOneResource",
			properties: defaultInstanceProperties,
			fakeReadFailCount: func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error) {
				return []pacemaker.ResourceFailCount{
					{
						Node:         "test-instance-1",
						ResourceName: "resource-1",
						FailCount:    1,
					},
				}, nil
			},
			wantValues:      []int{1},
			wantMetricCount: 1,
		},
		{
			name:       "MultipleFailedResources",
			properties: defaultInstanceProperties,
			fakeReadFailCount: func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error) {
				return []pacemaker.ResourceFailCount{
					{
						Node:         "test-instance-1",
						ResourceName: "resource-1",
						FailCount:    1,
					}, {
						Node:         "test-instance-2",
						ResourceName: "resource-2",
						FailCount:    2,
					},
				}, nil
			},
			wantValues:      []int{2, 1},
			wantMetricCount: 2,
		},
		{
			name:       "ReadFailCountFailure",
			properties: defaultInstanceProperties,
			fakeReadFailCount: func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error) {
				return nil, cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "NoFailedResource",
			properties: defaultInstanceProperties,
			fakeReadFailCount: func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error) {
				return nil, nil
			},
		},
		{
			name: "MetricsSkipped",
			properties: &InstanceProperties{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{
							failCountsPath,
						},
					},
				},
				SkippedMetrics: map[string]bool{
					failCountsPath: true,
				},
			},
			fakeReadFailCount: func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error) {
				return []pacemaker.ResourceFailCount{
					{
						Node:         "test-instance-1",
						ResourceName: "resource-1",
						FailCount:    1,
					}, {
						Node:         "test-instance-2",
						ResourceName: "resource-2",
						FailCount:    2,
					},
				}, nil
			},
			wantValues:      nil,
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues, gotErr := collectFailCount(context.Background(), test.properties, test.fakeReadFailCount, nil)
			diff := cmp.Diff(test.wantValues, gotValues, cmpopts.SortSlices(func(x, y int) bool { return x < y }))
			if diff != "" {
				t.Errorf("collectFailCount() returned unexpected diff (-want,+got): %s\n", diff)
			}
			if len(gotMetrics) != test.wantMetricCount {
				t.Errorf("collectFailCount() returned unexpected number of metrics. got: %d, want: %d. Got Metrics: %v",
					len(gotMetrics), test.wantMetricCount, gotMetrics)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectFailCount() returned unexpected error. got: %v, want: %v", gotErr, test.wantErr)
			}
		})
	}
}

// In Non Production setup CollectWithRetry should keep on retrying till the limit is reached.
func TestCollectWithRetry(t *testing.T) {
	_, err := defaultInstanceProperties.CollectWithRetry(context.Background())
	if err == nil {
		t.Errorf("CollectWithRetry() = nil, want error")
	}
}
