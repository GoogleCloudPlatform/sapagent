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

package workloadmanager

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/zieckey/goini"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
)

type (
	mockReadCloser struct{}
	fakeToken      struct {
		T *oauth2.Token
	}
	fakeErrorToken struct{}
)

func (ft fakeToken) Token() (*oauth2.Token, error) {
	return ft.T, nil
}

func (ft fakeErrorToken) Token() (*oauth2.Token, error) {
	return nil, errors.New("Could not generate token")
}

func (m mockReadCloser) Read(p []byte) (n int, err error) {
	return 0, errors.New("Stream error")
}

func (m mockReadCloser) Close() error {
	return nil
}

var (

	//go:embed test_data/credentials.json
	defaultCredentials string

	defaultPacemakerConfig = &cnfpb.Configuration{
		BareMetal: false,
		CloudProperties: &instancepb.CloudProperties{
			InstanceId:   "test-instance-id",
			ProjectId:    "test-project-id",
			Zone:         "test-zone",
			InstanceName: "test-instance-name",
		},
		AgentProperties: &cnfpb.AgentProperties{Version: "1.0"},
	}

	defaultPacemakerConfigNoCloudProperties = &cnfpb.Configuration{
		BareMetal: false,
	}

	jsonResponseError = `
{
	"error": {
		"code": "1",
		"message": "generic error message"
	}
}
`
	jsonHealthyResponse = `
{
	"error": null
}
`
)

func wantErrorPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: pacemakerExists,
					},
				},
			}},
		}},
	}
}

func wantDefaultPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"fence_agent_compute_api_access": "false",
					"fence_agent_logging_api_access": "false",
					"location_preference_set":        locationPref,
					"maintenance_mode_active":        "true",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: pacemakerExists,
					},
				},
			}},
		}},
	}
}

func wantCLIPreferPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"migration_threshold":            "5000",
					"fence_agent_compute_api_access": "false",
					"fence_agent_logging_api_access": "false",
					"location_preference_set":        locationPref,
					"maintenance_mode_active":        "true",
					"resource_stickiness":            "1000",
					"saphana_demote_timeout":         "3600",
					"saphana_promote_timeout":        "3600",
					"saphana_start_timeout":          "3600",
					"saphana_stop_timeout":           "3600",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: pacemakerExists,
					},
				},
			}},
		}},
	}
}

func wantNoPropertiesPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "",
					"zone":        "",
					"project_id":  "",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: pacemakerExists,
					},
				},
			}},
		}},
	}
}

func wantSuccessfulAccessPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"fence_agent_compute_api_access": "true",
					"fence_agent_logging_api_access": "true",
					"location_preference_set":        locationPref,
					"maintenance_mode_active":        "true",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: pacemakerExists,
					},
				},
			}},
		}},
	}
}

func TestCheckAPIAccess(t *testing.T) {
	tests := []struct {
		name    string
		runner  commandlineexecutor.CommandRunnerNoSpace
		args    []string
		want    bool
		wantErr error
	}{
		{
			name: "CheckAPIAccessCurlError",
			runner: func(string, ...string) (string, string, error) {
				return "", "", errors.New("Could not resolve URL")
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CheckAPIAccessInvalidJSON",
			runner: func(string, ...string) (string, string, error) {
				return "<http>Error 403</http>", "", nil
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CheckAPIAccessValidJSONResponseError",
			runner: func(string, ...string) (string, string, error) {
				return jsonResponseError, "", nil
			},
			args:    []string{},
			want:    false,
			wantErr: nil,
		},
		{
			name: "CheckAPIAccessValidJSON",
			runner: func(string, ...string) (string, string, error) {
				return jsonHealthyResponse, "", nil
			},
			args:    []string{},
			want:    true,
			wantErr: nil,
		},
		{
			name: "CheckAPIAccessValidJSONButWithError",
			runner: func(string, ...string) (string, string, error) {
				return jsonHealthyResponse, "", errors.New("Could not resolve URL")
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := checkAPIAccess(test.runner, test.args...)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("checkAPIAccess() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}

			if !cmp.Equal(test.wantErr, gotErr, cmpopts.EquateErrors()) {
				t.Errorf("checkAPIAccess got error %v, want error %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestSetPacemakerAPIAccess(t *testing.T) {
	tests := []struct {
		name   string
		runner commandlineexecutor.CommandRunnerNoSpace
		want   map[string]string
	}{
		{
			name: "TestAccessFailures",
			runner: func(string, ...string) (string, string, error) {
				return jsonResponseError, "", nil
			},
			want: map[string]string{
				"fence_agent_compute_api_access": "false",
				"fence_agent_logging_api_access": "false",
			},
		},
		{
			name: "TestAccessErrors",
			runner: func(string, ...string) (string, string, error) {
				return "", "", errors.New("Could not resolve URL")
			},
			want: map[string]string{
				"fence_agent_compute_api_access": "false",
				"fence_agent_logging_api_access": "false",
			},
		},
		{
			name: "TestAccessSuccessful",
			runner: func(string, ...string) (string, string, error) {
				return jsonHealthyResponse, "", nil
			},
			want: map[string]string{
				"fence_agent_compute_api_access": "true",
				"fence_agent_logging_api_access": "true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerAPIAccess(got, "", "", test.runner)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerAPIAccess() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerMaintenanceMode(t *testing.T) {
	tests := []struct {
		name         string
		runner       commandlineexecutor.CommandRunner
		crmAvailable bool
		want         map[string]string
	}{
		{
			name:         "TestMaintenanceModeCRMAvailable",
			runner:       func(string, string) (string, string, error) { return "Maintenance mode ready", "", nil },
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "true"},
		},
		{
			name:         "TestMaintenanceModeNotCRMUnavailable",
			runner:       func(string, string) (string, string, error) { return "Maintenance mode ready", "", nil },
			crmAvailable: false,
			want:         map[string]string{"maintenance_mode_active": "true"},
		},
		{
			name: "TestMaintenanceModeError",
			runner: func(string, string) (string, string, error) {
				return "", "", errors.New("cannot run sh, access denied")
			},
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "false"},
		},
		{
			name: "TestMaintenanceModeNotEnabled",
			runner: func(string, string) (string, string, error) {
				return "", "", nil
			},
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "false"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerMaintenanceMode(got, test.crmAvailable, test.runner)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerMaintenanceMode() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPacemakerSetLabelsForRscNvPairs(t *testing.T) {
	tests := []struct {
		name       string
		nvPairs    []NVPair
		nameToFind string
		want       map[string]string
	}{
		{
			name:       "TestRSCNVPairsEmptyArray",
			nvPairs:    []NVPair{},
			nameToFind: "TestValue",
			want:       map[string]string{},
		},
		{
			name: "TestRSCNVPairsMatchedValues",
			nvPairs: []NVPair{
				NVPair{
					Name:  "TestValue",
					Value: "TestMappingForValue",
				},
				NVPair{
					Name:  "TestValue",
					Value: "SecondMapping",
				},
				NVPair{
					Name:  "SecondValue",
					Value: "ThirdMapping",
				},
			},
			nameToFind: "TestValue",
			want:       map[string]string{"TestValue": "SecondMapping"},
		},
		{
			name: "TestRSCNVPairsNoMatch",
			nvPairs: []NVPair{
				NVPair{
					Name:  "Test-Value",
					Value: "TestMappingForValue",
				},
			},
			nameToFind: "Test-Value",
			want:       map[string]string{"Test_Value": "TestMappingForValue"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setLabelsForRSCNVPairs(got, test.nvPairs, test.nameToFind)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setLabelsForRscNvPairs() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIteratePrimitiveChild(t *testing.T) {
	tests := []struct {
		name         string
		attribute    ClusterPropertySet
		classNode    string
		typeNode     string
		idNode       string
		returnMap    map[string]string
		instanceName string
		want         string
		wantLabels   map[string]string
	}{
		{
			name: "TestIteratePrimitiveChildNoMatches",
			attribute: ClusterPropertySet{
				ID: "Test",
				NVPairs: []NVPair{
					{
						Name:  "TestValue",
						Value: "TestMappingForValue",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fake-type",
			idNode:       "asdf",
			returnMap:    map[string]string{},
			instanceName: "instance-name",
			want:         "",
			wantLabels:   map[string]string{},
		},
		{
			name: "TestIteratePrimitiveChildFenceKeys",
			attribute: ClusterPropertySet{
				ID: "Test",
				NVPairs: []NVPair{
					{
						Name:  "pcmk_delay_max",
						Value: "0",
					},
					{
						Name:  "pcmk_delay_base",
						Value: "2",
					},
					{
						Name:  "pcmk_reboot_timeout",
						Value: "1",
					},
					{
						Name:  "pcmk_monitor_retries",
						Value: "3",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fake-type",
			idNode:       "instance_node_1",
			returnMap:    map[string]string{},
			instanceName: "instance_node_1",
			want:         "",
			wantLabels: map[string]string{
				"pcmk_delay_max":       "0",
				"pcmk_delay_base":      "2",
				"pcmk_reboot_timeout":  "1",
				"pcmk_monitor_retries": "3",
			},
		},
		{
			name: "TestIteratePrimitiveChildServicePath",
			attribute: ClusterPropertySet{ID: "Test",
				NVPairs: []NVPair{
					NVPair{
						Name:  "serviceaccount",
						Value: "test/account/path",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fake-type",
			idNode:       "instance_node_1",
			returnMap:    map[string]string{},
			instanceName: "instance_node_1",
			want:         "test/account/path",
			wantLabels:   map[string]string{},
		},
		{
			name: "TestIteratePrimitiveChildFenceGCE",
			attribute: ClusterPropertySet{ID: "Test",
				NVPairs: []NVPair{
					NVPair{
						Name:  "pcmk_delay_max",
						Value: "0",
					},
					NVPair{
						Name:  "port",
						Value: "instance_node_1",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fence_gce",
			idNode:       "fake_node",
			returnMap:    map[string]string{},
			instanceName: "instance_node_1",
			want:         "",
			wantLabels: map[string]string{
				"pcmk_delay_max": "0",
			},
		},
		{
			name: "TestIteratePrimitiveChildFenceGCENoPortMatch",
			attribute: ClusterPropertySet{ID: "Test",
				NVPairs: []NVPair{
					NVPair{
						Name:  "pcmk_delay_max",
						Value: "0",
					},
					NVPair{
						Name:  "port",
						Value: "instance_node_2",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fence_gce",
			idNode:       "fake_node",
			returnMap:    map[string]string{},
			instanceName: "instance_node_1",
			want:         "",
			wantLabels:   map[string]string{},
		},
		{
			name: "TestIteratePrimitiveChildStonith",
			attribute: ClusterPropertySet{ID: "Test",
				NVPairs: []NVPair{
					NVPair{
						Name:  "pcmk_delay_max",
						Value: "0",
					},
					NVPair{
						Name:  "serviceaccount",
						Value: "external/test/account/path",
					},
				},
			},
			classNode:    "stonith",
			typeNode:     "external/fake-type",
			idNode:       "instance_node_1",
			returnMap:    map[string]string{},
			instanceName: "instance_node_1",
			want:         "external/test/account/path",
			wantLabels: map[string]string{
				"pcmk_delay_max": "0",
				"fence_agent":    "fake-type",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotLabels := map[string]string{}
			got := iteratePrimitiveChild(gotLabels, test.attribute, test.classNode, test.typeNode, test.idNode, test.returnMap, test.instanceName)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("iteratePrimitiveChild() returned unexpected account path value (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(test.wantLabels, gotLabels); diff != "" {
				t.Errorf("iteratePrimitiveChild() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIteratePrimitiveChildReturnMap(t *testing.T) {
	tests := []struct {
		name         string
		attribute    ClusterPropertySet
		classNode    string
		typeNode     string
		idNode       string
		instanceName string
		wantMap      map[string]string
	}{
		{
			name: "TestIteratePrimitiveChildNoMatches",
			attribute: ClusterPropertySet{
				ID: "Test",
				NVPairs: []NVPair{
					{
						Name:  "projectId",
						Value: "test-project-id",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fake-type",
			idNode:       "asdf",
			instanceName: "instance-name",
			wantMap:      map[string]string{},
		},
		{
			name: "TestIteratePrimitiveChildNoMatches",
			attribute: ClusterPropertySet{
				ID: "Test",
				NVPairs: []NVPair{
					{
						Name:  "project",
						Value: "test-project-id",
					},
				},
			},
			classNode:    "fake-class",
			typeNode:     "fake-type",
			idNode:       "asdf",
			instanceName: "instance-name",
			wantMap: map[string]string{
				"projectId": "test-project-id",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMap := map[string]string{}
			iteratePrimitiveChild(map[string]string{}, test.attribute, test.classNode, test.typeNode, test.idNode, gotMap, test.instanceName)

			if diff := cmp.Diff(test.wantMap, gotMap); diff != "" {
				t.Errorf("iteratePrimitiveChild() returned unexpected return map diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerPrimitives(t *testing.T) {
	tests := []struct {
		name       string
		c          *cnfpb.Configuration
		primitives []PrimitiveClass
		want       map[string]string
	}{
		{
			name: "TestSetPacemakerPrimitivesImproperTypes",
			c:    defaultPacemakerConfig,
			primitives: []PrimitiveClass{
				{
					ClassType: "fake-type",
					InstanceAttributes: ClusterPropertySet{
						ID: "fake-id",
						NVPairs: []NVPair{
							NVPair{
								Name:  "serviceaccount",
								Value: "external/test/account/path",
							},
						},
					},
				},
			},
			want: map[string]string{"serviceAccountJsonFile": ""},
		},
		{
			name: "TestSetPacemakerPrimitivesNoMatch",
			c:    defaultPacemakerConfig,
			primitives: []PrimitiveClass{
				{
					ClassType: "fence_gce",
					InstanceAttributes: ClusterPropertySet{
						ID:      "fake-id",
						NVPairs: []NVPair{},
					},
				},
			},
			want: map[string]string{"serviceAccountJsonFile": ""},
		},
		{
			name: "TestSetPacemakerPrimitivesBasicMatch1",
			c:    defaultPacemakerConfig,
			primitives: []PrimitiveClass{
				{
					ClassType: "fake-type",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-instance_attributes",
						NVPairs: []NVPair{
							NVPair{
								Name:  "serviceaccount",
								Value: "external/test/account/path",
							},
						},
					},
				},
			},
			want: map[string]string{"serviceAccountJsonFile": "external/test/account/path"},
		},
		{
			name: "TestSetPacemakerPrimitivesBasicMatch2",
			c:    defaultPacemakerConfig,
			primitives: []PrimitiveClass{
				{
					ClassType: "fake-type",
					InstanceAttributes: ClusterPropertySet{
						ID:      "test-instance-name-instance_attributes",
						NVPairs: []NVPair{},
					},
				},
				{
					ClassType: "fence_gce",
					InstanceAttributes: ClusterPropertySet{
						ID: "fake-id",
						NVPairs: []NVPair{
							NVPair{
								Name:  "serviceaccount",
								Value: "external/test/account/path2",
							},
						},
					},
				},
			},
			want: map[string]string{"serviceAccountJsonFile": "external/test/account/path2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := setPacemakerPrimitives(map[string]string{}, test.primitives, test.c)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerPrimitives() returned unexpected return map diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetDefaultBearerToken(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		file        string
		tokenGetter DefaultTokenGetter
		want        string
		wantErr     error
	}{
		{
			name: "GetDefaultBearerTokenTest",
			ctx:  context.Background(),
			file: defaultCredentials,
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				ts := fakeToken{
					T: &oauth2.Token{
						AccessToken: defaultCredentials,
					},
				}
				return ts, nil
			},
			want:    defaultCredentials,
			wantErr: nil,
		},
		{
			name: "GetDefaultBearerTokenTestBadTokenFile",
			ctx:  context.Background(),
			file: "{}",
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: nil}, errors.New("Could not build token")
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GetDefaultBearerTokenTestBadTokenGenerator",
			ctx:  context.Background(),
			file: "",
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeErrorToken{}, nil
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			got, err := getDefaultBearerToken(test.ctx, test.tokenGetter)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("getDefaultBearerToken() returned unexpected diff (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(test.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("getDefaultBearerToken() returned unexpected error diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetJSONBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		file       string
		fileReader ConfigFileReader
		credGetter JSONCredentialsGetter
		want       string
		wantErr    error
	}{
		{
			name:       "GetJSONBearerTokenTestFileReaderError",
			ctx:        context.Background(),
			file:       "",
			fileReader: func(string) (io.ReadCloser, error) { return nil, errors.New("Failed to read file") },
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) { return nil, nil },
			want:       "",
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "GetJSONBearerTokenTestFileReaderError2",
			ctx:  context.Background(),
			file: "",
			fileReader: func(string) (io.ReadCloser, error) {
				return mockReadCloser{}, nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) { return nil, nil },
			want:       "",
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "GetJSONBearerTokenTestFileReaderError3",
			ctx:  context.Background(),
			file: "",
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return nil, errors.New("Could not build credentials")
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GetJSONBearerTokenTestFileReaderError4",
			ctx:  context.Background(),
			file: "",
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeErrorToken{},
					JSON:        []byte{},
				}, nil
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GetJSONBearerTokenTestFileReaderError5",
			ctx:  context.Background(),
			file: "",
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeErrorToken{},
					JSON:        nil,
				}, nil
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GetJSONBearerTokenTestAccessTokens",
			ctx:  context.Background(),
			file: "",
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeToken{T: &oauth2.Token{
						AccessToken: defaultCredentials,
					},
					},
					JSON: []byte{},
				}, nil
			},
			want:    defaultCredentials,
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := getJSONBearerToken(test.ctx, "", test.fileReader, test.credGetter)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("getJSONBearerToken() returned unexpected diff (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(test.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("getJSONBearerToken() returned unexpected error diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name                   string
		serviceAccountJSONFile string
		fileReader             ConfigFileReader
		tokenGetter            DefaultTokenGetter
		credGetter             JSONCredentialsGetter
		want                   string
		wantErr                error
	}{
		{
			name:                   "DefaultTokenTest",
			serviceAccountJSONFile: "",
			fileReader:             func(string) (io.ReadCloser, error) { return nil, nil },
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}}, nil
			},
			want:    defaultCredentials,
			wantErr: nil,
		},
		{
			name:                   "DefaultTokenTest",
			serviceAccountJSONFile: "/etc/jsoncreds.json",
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: &oauth2.Token{AccessToken: "fake token"}}, nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeToken{T: &oauth2.Token{
						AccessToken: defaultCredentials,
					},
					},
					JSON: []byte{},
				}, nil
			},
			want:    defaultCredentials,
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := getBearerToken(context.Background(), test.serviceAccountJSONFile, test.fileReader, test.credGetter, test.tokenGetter)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("getBearerToken() returned unexpected diff (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(test.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("getBearerToken() returned unexpected error diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectPacemakerMetrics(t *testing.T) {
	tests := []struct {
		name                 string
		runtimeOS            string
		runner               commandlineexecutor.CommandRunner
		runnerNoSpace        commandlineexecutor.CommandRunnerNoSpace
		exists               commandlineexecutor.CommandExistsRunner
		iir                  *instanceinfo.Reader
		config               *cnfpb.Configuration
		mapper               instanceinfo.NetworkInterfaceAddressMapper
		osStatReader         OSStatReader
		wantOsVersion        string
		wantPacemakerExists  float64
		wantPacemakerMetrics func(*timestamppb.Timestamp, float64, string, string) WorkloadMetrics
		fileReader           ConfigFileReader
		credGetter           JSONCredentialsGetter
		tokenGetter          DefaultTokenGetter
		locationPref         string
	}{
		{
			name:                 "TestCollectPacemakerMetricsXMLNotFound",
			runtimeOS:            "linux",
			runner:               func(string, string) (string, string, error) { return "", "", nil },
			runnerNoSpace:        func(string, ...string) (string, string, error) { return "", "", nil },
			exists:               func(string) bool { return false },
			iir:                  defaultIIR,
			config:               defaultPacemakerConfig,
			mapper:               defaultMapperFunc,
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name:                 "TestCollectPacemakerMetricsUnparseableXML",
			runtimeOS:            "linux",
			runner:               func(string, string) (string, string, error) { return "Error: Bad XML", "", nil },
			runnerNoSpace:        func(string, ...string) (string, string, error) { return "", "", nil },
			exists:               func(string) bool { return true },
			iir:                  defaultIIR,
			config:               defaultPacemakerConfig,
			mapper:               defaultMapperFunc,
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name:                 "TestCollectPacemakerMetricsServiceAccountReadError",
			runtimeOS:            "linux",
			runner:               func(string, string) (string, string, error) { return pacemakerServiceAccountXML, "", nil },
			runnerNoSpace:        func(string, ...string) (string, string, error) { return "", "", nil },
			exists:               func(string) bool { return true },
			iir:                  defaultIIR,
			config:               defaultPacemakerConfig,
			mapper:               defaultMapperFunc,
			fileReader:           func(string) (io.ReadCloser, error) { return nil, errors.New("Failed to read file") },
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name:          "TestCollectPacemakerMetricsServiceAccount",
			runtimeOS:     "linux",
			runner:        func(string, string) (string, string, error) { return pacemakerServiceAccountXML, "", nil },
			runnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
			exists:        func(string) bool { return true },
			iir:           defaultIIR,
			config:        defaultPacemakerConfig,
			mapper:        defaultMapperFunc,
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeToken{T: &oauth2.Token{
						AccessToken: defaultCredentials,
					},
					},
					JSON: []byte{},
				}, nil
			},
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantDefaultPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name:      "TestCollectPacemakerMetricsProjectID",
			runtimeOS: "linux",
			runner:    func(string, string) (string, string, error) { return pacemakerServiceAccountXML, "", nil },
			runnerNoSpace: func(cmd string, args ...string) (string, string, error) {
				if cmd == "curl" {
					if args[2] == "https://compute.googleapis.com/compute/v1/projects/core-connect-dev?fields=id" {
						return jsonHealthyResponse, "", nil
					} else if args[8] == fmt.Sprintf(`{"dryRun": true, "entries": [{"logName": "projects/%s`, "core-connect-dev")+
						`/logs/test-log", "resource": {"type": "gce_instance"}, "textPayload": "foo"}]}"` {
						return jsonHealthyResponse, "", nil
					}
				}
				return "", "", nil
			},
			exists: func(string) bool { return true },
			iir:    defaultIIR,
			config: defaultPacemakerConfig,
			mapper: defaultMapperFunc,
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return &google.Credentials{
					TokenSource: fakeToken{T: &oauth2.Token{
						AccessToken: defaultCredentials,
					},
					},
					JSON: []byte{},
				}, nil
			},
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantSuccessfulAccessPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name:          "TestCollectPacemakerMetricsLocationPref",
			runtimeOS:     "linux",
			runner:        func(string, string) (string, string, error) { return pacemakerClipReferXML, "", nil },
			runnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
			exists:        func(string) bool { return true },
			iir:           defaultIIR,
			config:        defaultPacemakerConfig,
			mapper:        defaultMapperFunc,
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}}, nil
			},
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantCLIPreferPacemakerMetrics,
			locationPref:         "true",
		},
		{
			name:          "TestCollectPacemakerMetricsCloneMetrics",
			runtimeOS:     "linux",
			runner:        func(string, string) (string, string, error) { return pacemakerCloneXML, "", nil },
			runnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
			exists:        func(string) bool { return true },
			iir:           defaultIIR,
			config:        defaultPacemakerConfig,
			mapper:        defaultMapperFunc,
			fileReader: func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("test")), nil
			},
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}}, nil
			},
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantCLIPreferPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name:                 "TestCollectPacemakerMetricsNilCloudProperties",
			runtimeOS:            "linux",
			runner:               func(string, string) (string, string, error) { return pacemakerCloneXML, "", nil },
			runnerNoSpace:        func(string, ...string) (string, string, error) { return "", "", nil },
			exists:               func(string) bool { return true },
			iir:                  defaultIIR,
			config:               defaultPacemakerConfigNoCloudProperties,
			mapper:               defaultMapperFunc,
			wantOsVersion:        "test-os-version",
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantNoPropertiesPacemakerMetrics,
			locationPref:         "false",
		},
	}

	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1, ip2}, nil
	}
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}
	osCaptionExecute = func() (string, string, error) {
		return "\n\nCaption=Microsoft Windows Server 2019 Datacenter \n   \n    \n", "", nil
	}
	osVersionExecute = func() (string, string, error) {
		return "\n Version=10.0.17763  \n\n", "", nil
	}
	cmdExists = func(c string) bool {
		return true
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(test.config, test.mapper)
			want := test.wantPacemakerMetrics(nts, test.wantPacemakerExists, test.wantOsVersion, test.locationPref)

			pch := make(chan WorkloadMetrics)
			p := Parameters{
				Config:                test.config,
				CommandRunner:         test.runner,
				CommandRunnerNoSpace:  test.runnerNoSpace,
				CommandExistsRunner:   test.exists,
				ConfigFileReader:      test.fileReader,
				DefaultTokenGetter:    test.tokenGetter,
				JSONCredentialsGetter: test.credGetter,
				OSType:                test.runtimeOS,
			}
			go CollectPacemakerMetrics(context.Background(), p, pch)
			got := <-pch
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("CollectPacemakerMetrics returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFilterPrimitiveOpsByType(t *testing.T) {
	tests := []struct {
		name          string
		primitives    []PrimitiveClass
		primitiveType string
		want          []Op
	}{
		{
			name: "TestFilterPrimitiveOpsByTypeNoMatches",
			primitives: []PrimitiveClass{
				{
					ClassType: "Test",
					Operations: []Op{
						{ID: "1", Interval: "2", Timeout: "3", Name: "4"},
					},
				},
			},
			primitiveType: "TestMatch",
			want:          []Op{},
		},
		{
			name: "TestFilterPrimitiveOpsByTypeSomeMatches",
			primitives: []PrimitiveClass{
				{
					ClassType: "TestMatch",
					Operations: []Op{
						{ID: "1", Interval: "2", Timeout: "3", Name: "4"},
						{ID: "5", Interval: "6", Timeout: "7", Name: "8"},
					},
				},
				{
					ClassType: "TestDudMatch",
					Operations: []Op{
						{ID: "A", Interval: "B", Timeout: "C", Name: "D"},
					},
				},
			},
			primitiveType: "TestMatch",
			want: []Op{
				{ID: "1", Interval: "2", Timeout: "3", Name: "4"},
				{ID: "5", Interval: "6", Timeout: "7", Name: "8"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := filterPrimitiveOpsByType(test.primitives, test.primitiveType)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("filterPrimitiveOpsByType() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerHanaOperations(t *testing.T) {
	tests := []struct {
		name string
		ops  []Op
		want map[string]string
	}{
		{
			name: "TestSetPacemakerHanaOperationsNoMatch",
			ops: []Op{
				{
					Name:    "dud",
					Timeout: "0",
				},
			},
			want: map[string]string{},
		},
		{
			name: "TestSetPacemakerHanaOperationsMatches",
			ops: []Op{
				{
					Name:    "dud",
					Timeout: "0",
				},
				{
					Name:    "start",
					Timeout: "1",
				},
				{
					Name:    "stop",
					Timeout: "2",
				},
				{
					Name:    "promote",
					Timeout: "3",
				},
				{
					Name:    "demote",
					Timeout: "4",
				},
			},
			want: map[string]string{
				"saphana_start_timeout":   "1",
				"saphana_stop_timeout":    "2",
				"saphana_promote_timeout": "3",
				"saphana_demote_timeout":  "4",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}

			setPacemakerHanaOperations(got, test.ops)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerHanaOperations() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
