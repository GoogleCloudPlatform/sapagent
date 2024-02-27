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
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var defaultInstanceProperties = &InstanceProperties{
	Config: &cgpb.Configuration{
		CloudProperties: &ipb.CloudProperties{
			ProjectId:  "test-project",
			Zone:       "test-zone",
			InstanceId: "test-instance",
		},
	},
	PMBackoffPolicy: defaultBOPolicy(context.Background()),
}

func defaultResource() *monitoredresourcepb.MonitoredResource {
	return &monitoredresourcepb.MonitoredResource{
		Type: "gce_instance",
		Labels: map[string]string{
			"instance_id": "test-instance",
			"project_id":  "test-project",
			"zone":        "test-zone",
		},
	}
}

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestCollectNodeState(t *testing.T) {
	tests := []struct {
		name            string
		properties      *InstanceProperties
		fakeNodeState   readPacemakerNodeState
		wantMetrics     []*mrpb.TimeSeries
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/nodes",
						Labels: map[string]string{
							"sid":  "",
							"type": "INSTANCE_TYPE_UNDEFINED",
							"node": "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: nodeUnclean,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/nodes",
						Labels: map[string]string{
							"sid":  "",
							"type": "INSTANCE_TYPE_UNDEFINED",
							"node": "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: nodeShutdown,
								},
							},
						},
					},
				},
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/nodes",
						Labels: map[string]string{
							"sid":  "",
							"type": "INSTANCE_TYPE_UNDEFINED",
							"node": "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: nodeStandby,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/nodes",
						Labels: map[string]string{
							"sid":  "",
							"type": "INSTANCE_TYPE_UNDEFINED",
							"node": "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: nodeOnline,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/nodes",
						Labels: map[string]string{
							"sid":  "",
							"type": "INSTANCE_TYPE_UNDEFINED",
							"node": "test-instance-3",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: stateUnknown,
								},
							},
						},
					},
				},
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
				Config: &cgpb.Configuration{
					CollectionConfiguration: &cgpb.CollectionConfiguration{
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

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				cmpopts.SortSlices(func(x, y int) bool { return x < y }),
			}

			if diff := cmp.Diff(test.wantMetrics, gotMetrics, cmpOpts...); diff != "" {
				t.Errorf("collectNodeState() returned unexpected diff (-want,+got): %s\n", diff)
			}

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
		wantMetrics       []*mrpb.TimeSeries
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource1",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStarted,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource2",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStarting,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource3",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStarted,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource4",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStarted,
								},
							},
						},
					},
				},
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource1",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStopped,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource2",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceFailed,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource3",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-3",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: stateUnknown,
								},
							},
						},
					},
				},
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/resources",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource1",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: resourceStopped,
								},
							},
						},
					},
				},
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
				Config: &cgpb.Configuration{
					CollectionConfiguration: &cgpb.CollectionConfiguration{
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

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				cmpopts.SortSlices(func(x, y int) bool { return x < y }),
			}

			if diff := cmp.Diff(test.wantMetrics, gotMetrics, cmpOpts...); diff != "" {
				t.Errorf("resourceState() returned unexpected diff (-want,+got): %s\n", diff)
			}

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
		wantMetrics       []*mrpb.TimeSeries
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/failcounts",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource-1",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 1,
								},
							},
						},
					},
				},
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
			wantMetrics: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/failcounts",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource-1",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-1",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 1,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/cluster/failcounts",
						Labels: map[string]string{
							"sid":      "",
							"resource": "resource-2",
							"type":     "INSTANCE_TYPE_UNDEFINED",
							"node":     "test-instance-2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource:   defaultResource(),
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 2,
								},
							},
						},
					},
				},
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
				Config: &cgpb.Configuration{
					CollectionConfiguration: &cgpb.CollectionConfiguration{
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

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				cmpopts.SortSlices(func(x, y int) bool { return x < y }),
			}

			if diff := cmp.Diff(test.wantMetrics, gotMetrics, cmpOpts...); diff != "" {
				t.Errorf("collectFailCount() returned unexpected diff (-want,+got): %s\n", diff)
			}

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
