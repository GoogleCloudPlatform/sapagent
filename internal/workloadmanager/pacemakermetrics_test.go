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
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
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
	//go:embed test_data/pacemaker.xml
	pacemakerXML string
	//go:embed test_data/pacemaker-serviceaccount.xml
	pacemakerServiceAccountXML string
	//go:embed test_data/pacemaker-notype.xml
	pacemakerNoTypeXML string
	//go:embed test_data/pacemaker-fencegce.xml
	pacemakerFenceGCEXML string
	//go:embed test_data/pacemaker-clone.xml
	pacemakerCloneXML string
	//go:embed test_data/pacemaker-cliprefer.xml
	pacemakerClipReferXML string

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

func wantServiceAccountErrorPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"pcmk_delay_max": "instance-name-1=45",
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

func wantDefaultPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"pcmk_delay_max":                 "instance-name-1=45",
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

func wantCustomWorkloadConfigMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
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

func wantCLIPreferPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"pcmk_delay_max":                   "instance-name-1=30",
					"migration_threshold":              "5000",
					"fence_agent_compute_api_access":   "false",
					"fence_agent_logging_api_access":   "false",
					"location_preference_set":          locationPref,
					"maintenance_mode_active":          "true",
					"resource_stickiness":              "1000",
					"saphana_demote_timeout":           "3600",
					"saphana_promote_timeout":          "3600",
					"saphana_start_timeout":            "3600",
					"saphana_stop_timeout":             "3600",
					"saphanatopology_monitor_interval": "10",
					"saphanatopology_monitor_timeout":  "600",
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

func wantNoPropertiesPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
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
					"instance_id": "",
					"zone":        "",
					"project_id":  "",
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

func wantSuccessfulAccessPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/pacemaker",
				Labels: map[string]string{
					"pcmk_delay_max":                 "instance-name-1=45",
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
			wantPacemakerMetrics: wantServiceAccountErrorPacemakerMetrics,
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
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
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
