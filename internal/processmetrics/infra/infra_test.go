/*
Copyright 2023 Google LLC

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

package infra

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v0.alpha"
	"google.golang.org/api/googleapi"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcealpha/fakegcealpha"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultProperties = &Properties{
		Config: &cpb.Configuration{
			CloudProperties: defaultCloudProperties,
		},
	}
	defaultCloudProperties = &iipb.CloudProperties{
		InstanceName: "test-instance-name",
		Zone:         "test-zone",
		ProjectId:    "test-project",
	}
	soleTenantGCE = &fakegcealpha.TestGCE{
		Project: "test-project",
		Zone:    "test-zone",
		Instances: []*compute.Instance{{
			Name: "test-instance-name",
			Scheduling: &compute.Scheduling{
				NodeAffinities: []*compute.SchedulingNodeAffinity{{
					Key:      "compute.googleapis.com/node-group-name",
					Operator: "IN",
					Values:   []string{"test-node-group"},
				}},
			},
			SelfLink: defaultInstanceLink,
		}},
		NodeGroups: []*compute.NodeGroup{{
			Name: "test-node-group",
			Zone: "test-zone",
		}},
		NodeGroupNodes: &compute.NodeGroupsListNodes{
			Items: []*compute.NodeGroupNode{{
				Instances:           []string{defaultInstanceLink},
				UpcomingMaintenance: sampleUpcomingMaintenance,
			}},
		},
	}
	defaultInstanceLink       = "https://www.googleapis.com/compute/v1/projects/test-project-id/zones/test-zone/instances/test-instance-id"
	sampleUpcomingMaintenance = &compute.UpcomingMaintenance{
		CanReschedule:         true,
		WindowStartTime:       "2023-06-21T15:57:53Z",
		WindowEndTime:         "2023-06-21T23:57:53Z",
		LatestWindowStartTime: "2023-06-21T15:57:53Z",
		MaintenanceStatus:     "PENDING",
		Type:                  "SCHEDULED",
	}
	cmpCodeOnly = cmpopts.IgnoreFields(
		googleapi.Error{},
		"Body",
		"Header",
		"err",
	)
)

func TestCollect(t *testing.T) {
	tests := []struct {
		name                   string
		properties             *Properties
		fakeMetadataServerCall func() (string, error)
		gceService             *fakegcealpha.TestGCE
		wantCount              int
	}{
		{
			name:       "bareMetal",
			properties: &Properties{Config: &cpb.Configuration{BareMetal: true}},
			wantCount:  0,
		},
		{
			name:                   "GCE",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return "", nil },
			gceService: &fakegcealpha.TestGCE{
				Instances: []*compute.Instance{},
			},
			wantCount: 1,
		},
		{
			name:                   "soleTenant",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return "", nil },
			gceService:             soleTenantGCE,
			wantCount:              7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadataServerCall = test.fakeMetadataServerCall
			p := test.properties
			if test.gceService != nil {
				p.gceAlphaService = test.gceService
			}
			p.gceAlphaService = test.gceService
			got := p.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() returned unexpected metric count: got=%+v, want=%d", got, test.wantCount)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricCount(t *testing.T) {
	tests := []struct {
		name                   string
		properties             *Properties
		fakeMetadataServerCall func() (string, error)
		wantCount              int
	}{
		{
			name:                   "noMigration",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return "NONE", nil },
			wantCount:              1,
		},
		{
			name:                   "scheduledMigration",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return metadataMigrationResponse, nil },
			wantCount:              1,
		},
		{
			name:                   "error",
			properties:             defaultProperties,
			fakeMetadataServerCall: func() (string, error) { return "", errors.New("Error") },
			wantCount:              0,
		},
		{
			name: "MetricsSkipped",
			properties: &Properties{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{migrationPath},
					},
				},
				skippedMetrics: map[string]bool{migrationPath: true},
			},
			fakeMetadataServerCall: func() (string, error) { return metadataMigrationResponse, nil },
			wantCount:              0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectScheduledMigration(test.properties, test.fakeMetadataServerCall)
			// Test one metric is exported in case of successful call to the metadata server.
			if len(got) != test.wantCount {
				t.Errorf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricValue(t *testing.T) {
	tests := []struct {
		name                   string
		fakeMetadataServerCall func() (string, error)
		wantValue              int64
	}{
		{
			name:                   "noMigration",
			fakeMetadataServerCall: func() (string, error) { return "NONE", nil },
			wantValue:              0,
		},
		{
			name:                   "scheduledMigration",
			fakeMetadataServerCall: func() (string, error) { return metadataMigrationResponse, nil },
			wantValue:              1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectScheduledMigration(defaultProperties, test.fakeMetadataServerCall)
			if len(got) != 1 {
				t.Fatalf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), 1)
			}
			gotPointsCount := len(got[0].Points)
			if gotPointsCount != 1 {
				t.Fatalf("collectScheduledMigration() returned unexpected metric.points count: got=%d, want=%d", gotPointsCount, 1)
			}
			// Test exported metric value.
			gotValue := got[0].Points[0].GetValue().GetInt64Value()
			if gotValue != test.wantValue {
				t.Errorf("collectScheduledMigration() returned unexpected metric.point value: got=%d, want=%d", gotValue, test.wantValue)
			}
		})
	}
}

func TestCollectScheduledMigration_MetricType(t *testing.T) {
	want := "workload.googleapis.com/sap/infra/migration"
	got := collectScheduledMigration(defaultProperties, func() (string, error) { return "", nil })
	if len(got) != 1 {
		t.Fatalf("collectScheduledMigration() returned unexpected metric count: got=%d, want=%d", len(got), 1)
	}
	if got[0].Metric == nil {
		t.Fatal("collectScheduledMigration() returned unexpected metric.metric: nil")
	}
	// Test exported metric type.
	gotType := got[0].Metric.GetType()
	if gotType != want {
		t.Errorf("collectScheduledMigration() returned unexpected metric type: got=%s, want=%s", gotType, want)
	}
}

// metricValues converts a slice of metric protobufs into a map of metric types and string values.
func metricValues(metrics []*mrpb.TimeSeries) (map[string]string, error) {
	out := make(map[string]string)
	for _, m := range metrics {
		for _, v := range m.GetPoints() {
			proto := v.GetValue().ProtoReflect()
			protoFields := proto.Descriptor().Fields()
			if proto.Has(protoFields.ByName("bool_value")) {
				out[m.GetMetric().GetType()] = strconv.FormatBool(v.GetValue().GetBoolValue())
			} else if proto.Has(protoFields.ByName("int64_value")) {
				out[m.GetMetric().GetType()] = strconv.FormatInt(v.GetValue().GetInt64Value(), 10)
			} else {
				return nil, fmt.Errorf("Unsupported data type for metric %+v", protoFields)
			}
		}
	}
	return out, nil
}

func TestCollectUpcomingMaintenance(t *testing.T) {
	tests := []struct {
		name                string
		cloudProperties     *iipb.CloudProperties
		gceService          *fakegcealpha.TestGCE
		upcomingMaintenance *compute.UpcomingMaintenance
		skipMetrics         map[string]bool
		wantValues          map[string]string
		wantErr             error
	}{{
		name:            "UpcomingMaint",
		cloudProperties: defaultCloudProperties,
		gceService:      soleTenantGCE,
		upcomingMaintenance: &compute.UpcomingMaintenance{
			CanReschedule:         true,
			WindowStartTime:       "2023-06-21T15:57:53Z",
			WindowEndTime:         "2023-06-21T23:57:53Z",
			LatestWindowStartTime: "2023-06-21T15:57:53Z",
			MaintenanceStatus:     "PENDING",
			Type:                  "SCHEDULED",
		},
		wantValues: map[string]string{
			metricURL + maintPath + "/can_reschedule":           "true",
			metricURL + maintPath + "/window_start_time":        "1687363073",
			metricURL + maintPath + "/window_end_time":          "1687391873",
			metricURL + maintPath + "/latest_window_start_time": "1687363073",
			metricURL + maintPath + "/maintenance_status":       "1",
			metricURL + maintPath + "/type":                     "1",
		},
	},
		{
			name:            "CanRescheduleSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/can_reschedule": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/window_start_time":        "1687363073",
				metricURL + maintPath + "/window_end_time":          "1687391873",
				metricURL + maintPath + "/latest_window_start_time": "1687363073",
				metricURL + maintPath + "/maintenance_status":       "1",
				metricURL + maintPath + "/type":                     "1",
			},
		},
		{
			name:            "WindowStartTimeSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/window_start_time": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":           "true",
				metricURL + maintPath + "/window_end_time":          "1687391873",
				metricURL + maintPath + "/latest_window_start_time": "1687363073",
				metricURL + maintPath + "/maintenance_status":       "1",
				metricURL + maintPath + "/type":                     "1",
			},
		},
		{
			name:            "WindowEndTimeSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/window_end_time": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":           "true",
				metricURL + maintPath + "/window_start_time":        "1687363073",
				metricURL + maintPath + "/latest_window_start_time": "1687363073",
				metricURL + maintPath + "/maintenance_status":       "1",
				metricURL + maintPath + "/type":                     "1",
			},
		},
		{
			name:            "LatestWindowStartTimeSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/latest_window_start_time": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":     "true",
				metricURL + maintPath + "/window_start_time":  "1687363073",
				metricURL + maintPath + "/window_end_time":    "1687391873",
				metricURL + maintPath + "/maintenance_status": "1",
				metricURL + maintPath + "/type":               "1",
			},
		},
		{
			name:            "MaintenanceStatusSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/maintenance_status": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":           "true",
				metricURL + maintPath + "/window_start_time":        "1687363073",
				metricURL + maintPath + "/window_end_time":          "1687391873",
				metricURL + maintPath + "/latest_window_start_time": "1687363073",
				metricURL + maintPath + "/type":                     "1",
			},
		},
		{
			name:            "TypeSkipped",
			cloudProperties: defaultCloudProperties,
			gceService:      soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{
				CanReschedule:         true,
				WindowStartTime:       "2023-06-21T15:57:53Z",
				WindowEndTime:         "2023-06-21T23:57:53Z",
				LatestWindowStartTime: "2023-06-21T15:57:53Z",
				MaintenanceStatus:     "PENDING",
				Type:                  "SCHEDULED",
			},
			skipMetrics: map[string]bool{maintPath + "/type": true},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":           "true",
				metricURL + maintPath + "/window_start_time":        "1687363073",
				metricURL + maintPath + "/window_end_time":          "1687391873",
				metricURL + maintPath + "/latest_window_start_time": "1687363073",
				metricURL + maintPath + "/maintenance_status":       "1",
			},
		},
		{
			name:                "NoMaint",
			cloudProperties:     defaultCloudProperties,
			gceService:          soleTenantGCE,
			upcomingMaintenance: nil,
			wantValues:          map[string]string{},
		}, {
			name:            "NotSoleTenant",
			cloudProperties: defaultCloudProperties,
			gceService: &fakegcealpha.TestGCE{
				Project: "test-project",
				Zone:    "test-zone",
				Instances: []*compute.Instance{{
					Name: "test-instance-name",
				}},
			},
			wantErr:    cmpopts.AnyError,
			wantValues: map[string]string{},
		}, {
			name:                "errParseError",
			cloudProperties:     defaultCloudProperties,
			gceService:          soleTenantGCE,
			upcomingMaintenance: &compute.UpcomingMaintenance{WindowStartTime: "bogus"},
			wantValues: map[string]string{
				metricURL + maintPath + "/can_reschedule":           "false",
				metricURL + maintPath + "/window_start_time":        "0",
				metricURL + maintPath + "/window_end_time":          "0",
				metricURL + maintPath + "/latest_window_start_time": "0",
				metricURL + maintPath + "/maintenance_status":       "0",
				metricURL + maintPath + "/type":                     "0",
			},
		}, {
			name:            "errGetInstance",
			cloudProperties: defaultCloudProperties,
			gceService:      &fakegcealpha.TestGCE{},
			wantValues:      map[string]string{},
			wantErr:         cmpopts.AnyError,
		}, {
			name:            "errResolveNodeGroup",
			cloudProperties: defaultCloudProperties,
			gceService: &fakegcealpha.TestGCE{
				Instances: []*compute.Instance{{
					Scheduling: &compute.Scheduling{
						NodeAffinities: []*compute.SchedulingNodeAffinity{{
							Key:      "compute.googleapis.com/node-group-name",
							Operator: "IN",
							Values:   []string{"test-node-group"},
						}},
					},
					SelfLink: defaultInstanceLink,
				}},
			},
			wantValues: map[string]string{},
			wantErr:    cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		if tc.gceService != nil && tc.gceService.NodeGroupNodes != nil {
			tc.gceService.NodeGroupNodes.Items[0].UpcomingMaintenance = tc.upcomingMaintenance
		}
		p := New(&cpb.Configuration{CloudProperties: tc.cloudProperties}, nil, tc.gceService, tc.skipMetrics)
		m, err := p.collectUpcomingMaintenance()
		if d := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); d != "" {
			t.Errorf("collectUpcomingMaintenance(%s) error mismatch (-want, +got):\n%s", tc.name, d)
		}
		got, err := metricValues(m)
		if err != nil {
			t.Errorf("collectUpcomingMaintenance(%s) unparseable metrics: %+v", m, err)
		}
		if d := cmp.Diff(tc.wantValues, got); d != "" {
			t.Errorf("collectUpcomingMaintenance(%s) mismatch (-want, +got):\n%s", tc.name, d)
		}
	}
}

// TestCollectUpcomingMaintenanceNotInitialized is not implemented as a test case of
// TestCollectUpcomingMaintenance because the interface indirection of gceAlphaInterface results
// in a valid struct pointer to a nil value, which does _not_ equal nil nor is safely comparable.
// Further background: https://groups.google.com/g/golang-nuts/c/wnH302gBa4I
func TestCollectUpcomingMaintenanceNotInitialized(t *testing.T) {
	p := New(&cpb.Configuration{}, nil, nil, nil)
	_, err := p.collectUpcomingMaintenance()
	if err == nil {
		t.Error("collectUpcomingMaintenance(NotInitialized) got success expected error")
	}
}

func TestResolveNodeGroup(t *testing.T) {
	tests := []struct {
		name                string
		project             string
		zone                string
		instanceLink        string
		gceService          *fakegcealpha.TestGCE
		upcomingMaintenance *compute.UpcomingMaintenance
		wantNode            *compute.NodeGroupNode
		wantErr             error
	}{{
		name:                "success",
		project:             "test-project",
		zone:                "test-zone",
		instanceLink:        defaultInstanceLink,
		gceService:          soleTenantGCE,
		upcomingMaintenance: sampleUpcomingMaintenance,
		wantNode: &compute.NodeGroupNode{
			Instances:           []string{defaultInstanceLink},
			UpcomingMaintenance: sampleUpcomingMaintenance,
		},
	}, {
		name:         "errListModeGroups",
		project:      "test-project",
		zone:         "test-zone",
		instanceLink: defaultInstanceLink,
		gceService:   &fakegcealpha.TestGCE{},
		wantErr:      cmpopts.AnyError,
	}, {
		name:         "errListModeGroupNodes",
		project:      "test-project",
		zone:         "test-zone",
		instanceLink: defaultInstanceLink,
		gceService: &fakegcealpha.TestGCE{
			NodeGroups: []*compute.NodeGroup{{
				Name: "test-node-group-name",
			}},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name:         "errNoMatchingGroup",
		project:      "test-project",
		zone:         "test-zone",
		instanceLink: "non-matching-instance-link",
		gceService:   soleTenantGCE,
		wantErr:      cmpopts.AnyError,
	},
	}
	for _, tc := range tests {
		if tc.upcomingMaintenance != nil {
			tc.gceService.NodeGroupNodes.Items[0].UpcomingMaintenance = tc.upcomingMaintenance
		}
		p := New(nil, nil, tc.gceService, nil)
		got, err := p.resolveNodeGroup(tc.project, tc.zone, tc.instanceLink)
		if diff := cmp.Diff(tc.wantErr, err, cmpCodeOnly, cmpopts.EquateErrors()); diff != "" {
			t.Errorf("ListNodeGroups(%s) returned an unexpected error (-want +got): %v", tc.name, diff)
		}
		if d := cmp.Diff(tc.wantNode, got, protocmp.Transform()); d != "" {
			t.Errorf("resolveNodeGroup(%s) mismatch (-want, +got):\n%s", tc.name, d)
		}
	}
}

func TestRfc3339ToUnix(t *testing.T) {
	tests := []struct {
		rfc3339 string
		want    int64
	}{{
		rfc3339: "2023-06-21T15:57:53Z",
		want:    1687363073,
	}, {
		rfc3339: "bogus",
		want:    0,
	}, {
		rfc3339: "",
		want:    0,
	}}

	for _, tc := range tests {
		got := rfc3339ToUnix(tc.rfc3339)
		if got != tc.want {
			t.Errorf("rfc3339ToUnix(%v) = %v, want: %v", tc.rfc3339, got, tc.want)
		}
	}
}

func TestEnumToInt(t *testing.T) {
	tests := []struct {
		s    string
		m    map[string]int64
		want int64
	}{{
		s:    "item2",
		m:    map[string]int64{"item1": 1, "item2": 2},
		want: 2,
	}, {
		s:    "nonexistant",
		m:    map[string]int64{"item1": 1, "item2": 2},
		want: 0,
	}}

	for _, tc := range tests {
		got := enumToInt(tc.s, tc.m)
		if got != tc.want {
			t.Errorf("enumToInt(%v, %v) = %v, want: %v", tc.s, tc.m, got, tc.want)
		}
	}
}
