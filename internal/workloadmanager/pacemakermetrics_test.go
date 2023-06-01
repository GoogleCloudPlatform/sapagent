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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
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

	defaultPacemakerConfigNoCloudProperties = &cnfpb.Configuration{
		BareMetal: false,
	}

	defaultExec = func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "",
			StdErr: "",
		}
	}
	defaultExists      = func(string) bool { return true }
	defaultToxenGetter = func(context.Context, ...string) (oauth2.TokenSource, error) {
		return fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}}, nil
	}
	defaultCredGetter = func(context.Context, []byte, ...string) (*google.Credentials, error) {
		return &google.Credentials{
			TokenSource: fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}},
			JSON:        []byte{},
		}, nil
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

func wantCustomWorkloadConfigMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"location_preference_set": locationPref,
					"foo":                     "true",
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
		exec    commandlineexecutor.Execute
		args    []string
		want    bool
		wantErr error
	}{
		{
			name: "CheckAPIAccessCurlError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
					Error:  errors.New("Could not resolve URL"),
				}
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CheckAPIAccessInvalidJSON",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "<http>Error 403</http>",
					StdErr: "",
				}
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CheckAPIAccessValidJSONResponseError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: jsonResponseError,
					StdErr: "",
				}
			},
			args:    []string{},
			want:    false,
			wantErr: nil,
		},
		{
			name: "CheckAPIAccessValidJSON",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: jsonHealthyResponse,
					StdErr: "",
				}
			},
			args:    []string{},
			want:    true,
			wantErr: nil,
		},
		{
			name: "CheckAPIAccessValidJSONButWithError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: jsonHealthyResponse,
					StdErr: "",
					Error:  errors.New("Could not resolve URL"),
				}
			},
			args:    []string{},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := checkAPIAccess(context.Background(), test.exec, test.args...)

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
		name string
		exec commandlineexecutor.Execute
		want map[string]string
	}{
		{
			name: "TestAccessFailures",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: jsonResponseError,
					StdErr: "",
				}
			},
			want: map[string]string{
				"fence_agent_compute_api_access": "false",
				"fence_agent_logging_api_access": "false",
			},
		},
		{
			name: "TestAccessErrors",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
					Error:  errors.New("Could not resolve URL"),
				}
			},
			want: map[string]string{
				"fence_agent_compute_api_access": "false",
				"fence_agent_logging_api_access": "false",
			},
		},
		{
			name: "TestAccessSuccessful",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: jsonHealthyResponse,
					StdErr: "",
				}
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
			setPacemakerAPIAccess(context.Background(), got, "", "", test.exec)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerAPIAccess() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerMaintenanceMode(t *testing.T) {
	tests := []struct {
		name         string
		exec         commandlineexecutor.Execute
		crmAvailable bool
		want         map[string]string
	}{
		{
			name: "TestMaintenanceModeCRMAvailable",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "Maintenance mode ready",
					StdErr: "",
				}
			},
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "true"},
		},
		{
			name: "TestMaintenanceModeNotCRMUnavailable",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "Maintenance mode ready",
					StdErr: "",
				}
			},
			crmAvailable: false,
			want:         map[string]string{"maintenance_mode_active": "true"},
		},
		{
			name: "TestMaintenanceModeError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
					Error:  errors.New("cannot run sh, access denied"),
				}
			},
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "false"},
		},
		{
			name: "TestMaintenanceModeNotEnabled",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			crmAvailable: true,
			want:         map[string]string{"maintenance_mode_active": "false"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerMaintenanceMode(context.Background(), got, test.crmAvailable, test.exec)

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
						Name:  "pcmk_delay_base",
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
				"pcmk_delay_base": "0",
			},
		},
		{
			name: "TestIteratePrimitiveChildFenceGCENoPortMatch",
			attribute: ClusterPropertySet{ID: "Test",
				NVPairs: []NVPair{
					NVPair{
						Name:  "pcmk_delay_base",
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
						Name:  "pcmk_delay_base",
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
				"pcmk_delay_base": "0",
				"fence_agent":     "fake-type",
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
			c:    defaultConfiguration,
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
			want: map[string]string{
				"serviceAccountJsonFile": "",
				"pcmk_delay_max":         "",
			},
		},
		{
			name: "TestSetPacemakerPrimitivesNoMatch",
			c:    defaultConfiguration,
			primitives: []PrimitiveClass{
				{
					ClassType: "fence_gce",
					InstanceAttributes: ClusterPropertySet{
						ID:      "fake-id",
						NVPairs: []NVPair{},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
				"pcmk_delay_max":         "",
			},
		},
		{
			name: "TestSetPacemakerPrimitivesBasicMatch1",
			c:    defaultConfiguration,
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
			want: map[string]string{
				"serviceAccountJsonFile": "external/test/account/path",
				"pcmk_delay_max":         "",
			},
		},
		{
			name: "TestSetPacemakerPrimitivesBasicMatch2",
			c:    defaultConfiguration,
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
			want: map[string]string{
				"serviceAccountJsonFile": "external/test/account/path2",
				"pcmk_delay_max":         "",
			},
		},
		{
			name: "pcmkDelayMaxNoValue",
			c:    defaultConfiguration,
			primitives: []PrimitiveClass{
				{
					ClassType: "fence_gce",
					InstanceAttributes: ClusterPropertySet{
						ID:      "test-instance-name-instance_attributes",
						NVPairs: []NVPair{},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
				"pcmk_delay_max":         "",
			},
		},
		{
			name: "pcmkDelayMaxSingleValue",
			c:    defaultConfiguration,
			primitives: []PrimitiveClass{
				{
					ClassType: "stonith",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-1-instance_attributes",
						NVPairs: []NVPair{
							{Name: "instance_name", Value: "test-instance-name-1"},
						},
					},
				},
				{
					ClassType: "stonith",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-2-instance_attributes",
						NVPairs: []NVPair{
							{Name: "pcmk_delay_max", Value: "60"},
						},
					},
				},
				{
					ClassType: "stonith",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-3-instance_attributes",
						NVPairs: []NVPair{
							{Name: "instance_name", Value: "test-instance-name-3"},
							{Name: "pcmk_delay_max", Value: "30"},
						},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
				"pcmk_delay_max":         "test-instance-name-3=30",
			},
		},
		{
			name: "pcmkDelayMaxMultipleValues",
			c:    defaultConfiguration,
			primitives: []PrimitiveClass{
				{
					ClassType: "stonith",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-instance_attributes",
						NVPairs: []NVPair{
							{Name: "instance_name", Value: "test-instance-name"},
							{Name: "pcmk_delay_max", Value: "60"},
						},
					},
				},
				{
					ClassType: "stonith",
					InstanceAttributes: ClusterPropertySet{
						ID: "test-instance-name-2-instance_attributes",
						NVPairs: []NVPair{
							{Name: "instance_name", Value: "test-instance-name-2"},
							{Name: "pcmk_delay_max", Value: "30"},
						},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
				"pcmk_delay_max":         "test-instance-name=60,test-instance-name-2=30",
			},
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
			name:       "GetJSONBearerTokenTestFileReaderError3",
			ctx:        context.Background(),
			file:       "",
			fileReader: defaultFileReader,
			credGetter: func(context.Context, []byte, ...string) (*google.Credentials, error) {
				return nil, errors.New("Could not build credentials")
			},
			want:    "",
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "GetJSONBearerTokenTestFileReaderError4",
			ctx:        context.Background(),
			file:       "",
			fileReader: defaultFileReader,
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
			name:       "GetJSONBearerTokenTestFileReaderError5",
			ctx:        context.Background(),
			file:       "",
			fileReader: defaultFileReader,
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
			name:       "GetJSONBearerTokenTestAccessTokens",
			ctx:        context.Background(),
			file:       "",
			fileReader: defaultFileReader,
			credGetter: defaultCredGetter,
			want:       defaultCredentials,
			wantErr:    nil,
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
			tokenGetter:            defaultToxenGetter,
			want:                   defaultCredentials,
			wantErr:                nil,
		},
		{
			name:                   "DefaultTokenTest",
			serviceAccountJSONFile: "/etc/jsoncreds.json",
			fileReader:             defaultFileReader,
			tokenGetter: func(context.Context, ...string) (oauth2.TokenSource, error) {
				return fakeToken{T: &oauth2.Token{AccessToken: "fake token"}}, nil
			},
			credGetter: defaultCredGetter,
			want:       defaultCredentials,
			wantErr:    nil,
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

func TestCollectPacemakerMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name string

		exec                 commandlineexecutor.Execute
		exists               commandlineexecutor.Exists
		config               *cnfpb.Configuration
		osStatReader         OSStatReader
		workloadConfig       *wpb.WorkloadValidation
		fileReader           ConfigFileReader
		credGetter           JSONCredentialsGetter
		tokenGetter          DefaultTokenGetter
		wantPacemakerExists  float64
		wantPacemakerMetrics func(*timestamppb.Timestamp, float64, string, string) WorkloadMetrics
		locationPref         string
	}{
		{
			name:                 "XMLNotFound",
			exec:                 defaultExec,
			exists:               func(string) bool { return false },
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name: "UnparseableXML",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "Error: Bad XML",
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name: "ServiceAccountReadError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			fileReader:           fileReaderError,
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name: "ServiceAccountReadSuccess",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			fileReader:           defaultFileReader,
			credGetter:           defaultCredGetter,
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantDefaultPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name: "CustomWorkloadConfig",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "cibadmin" {
					return commandlineexecutor.Result{
						StdOut: pacemakerServiceAccountXML,
						// StdOut: "foobar",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					// StdOut: pacemakerServiceAccountXML,
					StdOut: "foobar",
					StdErr: "",
				}
			},
			exists: defaultExists,
			config: defaultConfiguration,
			workloadConfig: &wpb.WorkloadValidation{
				ValidationPacemaker: &wpb.ValidationPacemaker{
					ConfigMetrics: &wpb.PacemakerConfigMetrics{
						RscLocationMetrics: []*wpb.PacemakerRSCLocationMetric{
							{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/pacemaker",
									Label: "location_preference_set",
								},
								Value: wpb.RSCLocationVariable_LOCATION_PREFERENCE_SET,
							},
						},
					},
					OsCommandMetrics: []*cmpb.OSCommandMetric{
						{
							MetricInfo: &cmpb.MetricInfo{
								Type:  "workload.googleapis.com/sap/validation/corosync",
								Label: "foo",
							},
							OsVendor: cmpb.OSVendor_RHEL,
							EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
								AndEvalRules: &cmpb.EvalMetricRule{
									EvalRules: []*cmpb.EvalRule{
										&cmpb.EvalRule{
											OutputSource:  cmpb.OutputSource_STDOUT,
											EvalRuleTypes: &cmpb.EvalRule_OutputEquals{OutputEquals: "foobar"},
										},
									},
									IfTrue: &cmpb.EvalResult{
										EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
									},
									IfFalse: &cmpb.EvalResult{
										EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
									},
								},
							},
						},
						{
							OsVendor: cmpb.OSVendor_OS_VENDOR_UNSPECIFIED,
						},
					},
				},
			},
			fileReader:           defaultFileReader,
			credGetter:           defaultCredGetter,
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantCustomWorkloadConfigMetrics,
			locationPref:         "false",
		},
		{
			name: "ProjectID",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "curl" {
					if params.Args[2] == "https://compute.googleapis.com/compute/v1/projects/core-connect-dev?fields=id" {
						return commandlineexecutor.Result{
							StdOut: jsonHealthyResponse,
							StdErr: "",
						}
					} else if params.Args[8] == fmt.Sprintf(`{"dryRun": true, "entries": [{"logName": "projects/%s`, "core-connect-dev")+
						`/logs/test-log", "resource": {"type": "gce_instance"}, "textPayload": "foo"}]}"` {
						return commandlineexecutor.Result{
							StdOut: jsonHealthyResponse,
							StdErr: "",
						}
					}
				}
				// TODO I think this will break
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			fileReader:           defaultFileReader,
			credGetter:           defaultCredGetter,
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantSuccessfulAccessPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name: "LocationPref",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerClipReferXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			fileReader:           defaultFileReader,
			tokenGetter:          defaultToxenGetter,
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantCLIPreferPacemakerMetrics,
			locationPref:         "true",
		},
		{
			name: "CloneMetrics",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerCloneXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultConfiguration,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			fileReader:           defaultFileReader,
			tokenGetter:          defaultToxenGetter,
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantCLIPreferPacemakerMetrics,
			locationPref:         "false",
		},
		{
			name: "NilCloudProperties",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerCloneXML,
					StdErr: "",
				}
			},
			exists:               defaultExists,
			config:               defaultPacemakerConfigNoCloudProperties,
			workloadConfig:       collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantNoPropertiesPacemakerMetrics,
			locationPref:         "false",
		},
	}

	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaultIIR.Read(context.Background(), test.config, defaultMapperFunc)
			want := test.wantPacemakerMetrics(nts, test.wantPacemakerExists, "test-os-version", test.locationPref)

			p := Parameters{
				Config:                test.config,
				Execute:               test.exec,
				Exists:                test.exists,
				ConfigFileReader:      test.fileReader,
				DefaultTokenGetter:    test.tokenGetter,
				JSONCredentialsGetter: test.credGetter,
				WorkloadConfig:        test.workloadConfig,
				OSType:                "linux",
				osVendorID:            "rhel",
			}
			got := CollectPacemakerMetricsFromConfig(context.Background(), p)
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("CollectPacemakerMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
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
