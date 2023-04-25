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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
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
}

func TestCollectNodeState(t *testing.T) {
	tests := []struct {
		name            string
		fakeNodeState   readPacemakerNodeState
		wantValues      []int
		wantMetricCount int
	}{
		{
			name: "StateUncleanAndShutdown",
			fakeNodeState: func() (map[string]string, error) {
				return map[string]string{
					"test-instance-1": "unclean",
					"test-instance-2": "shutdown",
				}, nil
			},
			wantValues:      []int{nodeUnclean, nodeShutdown},
			wantMetricCount: 2,
		},
		{
			name: "StateStandbyAndOnlineUnknown",
			fakeNodeState: func() (map[string]string, error) {
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
			name: "PacemekerReadFailure",
			fakeNodeState: func() (map[string]string, error) {
				return nil, cmpopts.AnyError
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues := collectNodeState(defaultInstanceProperties, test.fakeNodeState)
			diff := cmp.Diff(test.wantValues, gotValues, cmpopts.SortSlices(func(x, y int) bool { return x < y }))
			if diff != "" {
				t.Errorf("collectNodeState() returned unexpected diff (-want,+got): %s\n", diff)
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
		fakeResourceState readPacemakerResourceState
		wantValues        []int
		wantMetricCount   int
	}{
		{
			name: "SuccessStartedStartingMasterSlave",
			fakeResourceState: func() ([]pacemaker.Resource, error) {
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
			name: "SuccessStoppedFailedUnknown",
			fakeResourceState: func() ([]pacemaker.Resource, error) {
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
			name: "SuccessDuplicateResources",
			fakeResourceState: func() ([]pacemaker.Resource, error) {
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
			name: "PacemakerReadFailure",
			fakeResourceState: func() ([]pacemaker.Resource, error) {
				return nil, cmpopts.AnyError
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues := collectResourceState(defaultInstanceProperties, test.fakeResourceState)

			if diff := cmp.Diff(test.wantValues, gotValues); diff != "" {
				t.Errorf("resourceState() returned unexpected diff (-want,+got): %s\n", diff)
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
		name string
		want []*mrpb.TimeSeries
	}{
		{
			name: "EmptyMetricsWhenNoPacmaker",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := defaultInstanceProperties.Collect(context.Background())
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Collect() returned unexpected diff (-want,+got): %s\n", diff)
			}
		})
	}
}

func TestCollectFailCount(t *testing.T) {
	tests := []struct {
		name              string
		fakeReadFailCount readPacemakerFailCount
		wantValues        []int
		wantMetricCount   int
	}{
		{
			name: "SuccessOneResource",
			fakeReadFailCount: func(commandlineexecutor.CommandRunner) ([]pacemaker.ResourceFailCount, error) {
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
			name: "MultipleFailedResources",
			fakeReadFailCount: func(commandlineexecutor.CommandRunner) ([]pacemaker.ResourceFailCount, error) {
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
			name: "ReadFailCountFailure",
			fakeReadFailCount: func(commandlineexecutor.CommandRunner) ([]pacemaker.ResourceFailCount, error) {
				return nil, cmpopts.AnyError
			},
		},
		{
			name: "NoFailedResource",
			fakeReadFailCount: func(commandlineexecutor.CommandRunner) ([]pacemaker.ResourceFailCount, error) {
				return nil, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMetrics, gotValues := collectFailCount(defaultInstanceProperties, test.fakeReadFailCount)
			diff := cmp.Diff(test.wantValues, gotValues, cmpopts.SortSlices(func(x, y int) bool { return x < y }))
			if diff != "" {
				t.Errorf("collectFailCount() returned unexpected diff (-want,+got): %s\n", diff)
			}
			if len(gotMetrics) != test.wantMetricCount {
				t.Errorf("collectFailCount() returned unexpected number of metrics. got: %d, want: %d. Got Metrics: %v",
					len(gotMetrics), test.wantMetricCount, gotMetrics)
			}
		})
	}
}
