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
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
)

type fakeToken struct {
	T *oauth2.Token
}

func (ft fakeToken) Token() (*oauth2.Token, error) {
	return ft.T, nil
}

var (
	//go:embed test_data/credentials.json
	defaultCredentials string
	//go:embed test_data/pacemaker-clone.xml
	pacemakerCloneXML string

	defaultExec = func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "",
			StdErr: "",
		}
	}
	defaultExists      = func(string) bool { return true }
	defaultTokenGetter = func(context.Context, ...string) (oauth2.TokenSource, error) {
		return fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}}, nil
	}
	defaultCredGetter = func(context.Context, []byte, ...string) (*google.Credentials, error) {
		return &google.Credentials{
			TokenSource: fakeToken{T: &oauth2.Token{AccessToken: defaultCredentials}},
			JSON:        []byte{},
		}, nil
	}
)

func wantErrorPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-region-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*mrpb.Point{{
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

func wantDefaultPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"fence_agent":                      "fence_gce",
					"fence_agent_compute_api_access":   "false",
					"fence_agent_logging_api_access":   "false",
					"location_preference_set":          "false",
					"maintenance_mode_active":          "true",
					"migration_threshold":              "5000",
					"pcmk_delay_max":                   "test-instance-name=30",
					"pcmk_monitor_retries":             "4",
					"pcmk_reboot_timeout":              "300",
					"resource_stickiness":              "1000",
					"saphana_demote_timeout":           "3600",
					"saphana_promote_timeout":          "3600",
					"saphana_start_timeout":            "3600",
					"saphana_stop_timeout":             "3600",
					"saphanatopology_monitor_interval": "10",
					"saphanatopology_monitor_timeout":  "600",
					"saphanatopology_start_timeout":    "600",
					"saphanatopology_stop_timeout":     "300",
					"ascs_instance":                    "",
					"ers_instance":                     "",
					"enqueue_server":                   "",
					"ascs_failure_timeout":             "60",
					"ascs_migration_threshold":         "3",
					"ascs_resource_stickiness":         "5000",
					"is_ers":                           "true",
					"op_timeout":                       "600",
					"stonith_enabled":                  "true",
					"stonith_timeout":                  "300",
					"saphana_notify":                   "true",
					"saphana_clone_max":                "2",
					"saphana_clone_node_max":           "1",
					"saphana_interleave":               "true",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-region-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*mrpb.Point{{
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

func TestCollectPacemakerMetricsFromConfig(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name                 string
		exec                 commandlineexecutor.Execute
		wantPacemakerExists  float64
		wantPacemakerMetrics func(*timestamppb.Timestamp, float64, string) WorkloadMetrics
	}{
		{
			name: "ZeroValue",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{} // Empty Pacemaker XML
			},
			wantPacemakerExists:  float64(0.0),
			wantPacemakerMetrics: wantErrorPacemakerMetrics,
		},
		{
			name: "CollectPacemakerMetricsSuccess",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerCloneXML,
				}
			},
			wantPacemakerExists:  float64(1.0),
			wantPacemakerMetrics: wantDefaultPacemakerMetrics,
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
			defaultIIR.Read(context.Background(), defaultConfiguration, defaultMapperFunc)
			want := test.wantPacemakerMetrics(nts, test.wantPacemakerExists, "test-os-version")

			p := Parameters{
				Config:                defaultConfiguration,
				Execute:               test.exec,
				Exists:                defaultExists,
				ConfigFileReader:      defaultFileReader,
				DefaultTokenGetter:    defaultTokenGetter,
				JSONCredentialsGetter: defaultCredGetter,
				WorkloadConfig:        collectionDefinition.GetWorkloadValidation(),
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
