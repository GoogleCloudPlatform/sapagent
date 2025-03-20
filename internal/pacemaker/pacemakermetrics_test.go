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

package pacemaker

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wvpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/configurablemetrics"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

type (
	mockReadCloser struct{}
	fakeToken      struct {
		T *oauth2.Token
	}
	fakeErrorToken struct{}
	fakeDiskMapper struct {
		err error
		out string
	}
)

var (
	//go:embed test_data/credentials.json
	defaultCredentials string
	//go:embed test_data/metricoverride.yaml
	testFS embed.FS

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

	defaultFileReader = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(data)), nil
	})
	fileReaderError = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return nil, errors.New("Could not find file")
	})

	defaultDiskMapper = &fakeDiskMapper{err: nil, out: "disk-mapping"}
	defaultMapperFunc = func() (map[instanceinfo.InterfaceName][]instanceinfo.NetworkAddress, error) {
		return map[instanceinfo.InterfaceName][]instanceinfo.NetworkAddress{"lo": []instanceinfo.NetworkAddress{instanceinfo.NetworkAddress(defaultNetworkIP)}}, nil
	}
	defaultNetworkIP = "127.0.0.1"

	defaultGCEService = &fake.TestGCE{
		GetDiskResp: []*compute.Disk{{
			Type: "/some/path/default-disk-type",
		}, {
			Type: "/some/path/default-disk-type",
		}, {
			Type: "/some/path/default-disk-type",
		}},
		GetDiskErr: []error{nil, nil, nil},
		ListDisksResp: []*compute.DiskList{
			{
				Items: []*compute.Disk{
					{
						Name: "disk-name",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "other-disk-device-name",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "hana-disk-name",
						Type: "/some/path/default-disk-type",
					},
				},
			},
		},
		ListDisksErr: []error{nil},
		GetInstanceResp: []*compute.Instance{{
			MachineType:       "test-machine-type",
			CpuPlatform:       "test-cpu-platform",
			CreationTimestamp: "test-creation-timestamp",
			Disks: []*compute.AttachedDisk{
				{
					Source:     "/some/path/disk-name",
					DeviceName: "disk-device-name",
					Type:       "PERSISTENT",
				},
				{
					Source:     "",
					DeviceName: "other-disk-device-name",
					Type:       "SCRATCH",
				},
				{
					Source:     "/some/path/hana-disk-name",
					DeviceName: "sdb",
					Type:       "PERSISTENT",
				},
			},
			NetworkInterfaces: []*compute.NetworkInterface{
				{
					Name:      "network-name",
					Network:   "test-network",
					NetworkIP: defaultNetworkIP,
				},
			},
		},
		},
		GetInstanceErr: []error{nil},
		ListZoneOperationsResp: []*compute.OperationList{{
			Items: []*compute.Operation{
				{
					EndTime: "2022-08-23T12:00:01.000-04:00",
				},
				{
					EndTime: "2022-08-23T12:00:00.000-04:00",
				},
			},
		},
		},
		ListZoneOperationsErr: []error{nil},
	}
	defaultIIR = instanceinfo.New(defaultDiskMapper, defaultGCEService)

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
	defaultConfiguration = &cnfpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			InstanceName: "test-instance-name",
			InstanceId:   "test-instance-id",
			Zone:         "test-region-zone",
			ProjectId:    "test-project-id",
		},
		AgentProperties: &cnfpb.AgentProperties{Name: "sapagent", Version: "1.0"},
		CollectionConfiguration: &cnfpb.CollectionConfiguration{
			CollectWorkloadValidationMetrics: wpb.Bool(true),
		},
		SupportConfiguration: &cnfpb.SupportConfiguration{
			SendWorkloadValidationMetricsToCloudMonitoring: &wpb.BoolValue{Value: true},
		},
	}
)

func (f *fakeDiskMapper) ForDeviceName(ctx context.Context, deviceName string) (string, error) {
	return deviceName, f.err
}

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

func wantErrorPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{}
}

func wantServiceAccountErrorPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"fence_agent":          "fence_gce",
		"pcmk_delay_max":       "test-instance-name=45",
		"pcmk_monitor_retries": "5",
		"pcmk_reboot_timeout":  "200",
	}
}

func wantDefaultPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"fence_agent":                       "fence_gce",
		"pcmk_delay_max":                    "test-instance-name=45",
		"pcmk_monitor_retries":              "5",
		"pcmk_reboot_timeout":               "200",
		"fence_agent_compute_api_access":    "false",
		"fence_agent_logging_api_access":    "false",
		"location_preference_set":           locationPref,
		"maintenance_mode_active":           "true",
		"stonith_enabled":                   "",
		"stonith_timeout":                   "",
		"ascs_instance":                     "",
		"ers_instance":                      "",
		"enqueue_server":                    "",
		"saphana_automated_register":        "",
		"saphana_duplicate_primary_timeout": "",
		"saphana_prefer_site_takeover":      "",
		"saphana_notify":                    "",
		"saphana_clone_max":                 "",
		"saphana_clone_node_max":            "",
		"saphana_interleave":                "",
		"saphanatopology_clone_node_max":    "",
		"saphanatopology_interleave":        "",
		"ascs_automatic_recover":            "",
		"ascs_failure_timeout":              "",
		"ascs_migration_threshold":          "",
		"ascs_resource_stickiness":          "",
		"ascs_monitor_interval":             "",
		"ascs_monitor_timeout":              "",
		"ers_automatic_recover":             "",
		"is_ers":                            "",
		"ers_monitor_interval":              "",
		"ers_monitor_timeout":               "",
		"op_timeout":                        "",
		"healthcheck_monitor_interval":      "",
		"healthcheck_monitor_timeout":       "",
		"ilb_monitor_interval":              "",
		"ilb_monitor_timeout":               "",
		"ascs_healthcheck_monitor_interval": "",
		"ascs_healthcheck_monitor_timeout":  "",
		"ascs_ilb_monitor_interval":         "",
		"ascs_ilb_monitor_timeout":          "",
		"ers_healthcheck_monitor_interval":  "",
		"ers_healthcheck_monitor_timeout":   "",
		"ers_ilb_monitor_interval":          "",
		"ers_ilb_monitor_timeout":           "",
		"has_alias_ip":                      "false",
	}
}

func wantCustomWorkloadConfigMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"location_preference_set": locationPref,
		"foo":                     "true",
	}
}

func wantCLIPreferPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"fence_agent":                        "gcpstonith",
		"pcmk_delay_max":                     "test-instance-name=30",
		"pcmk_monitor_retries":               "4",
		"pcmk_reboot_timeout":                "300",
		"migration_threshold":                "5000",
		"fence_agent_compute_api_access":     "false",
		"fence_agent_logging_api_access":     "false",
		"location_preference_set":            locationPref,
		"maintenance_mode_active":            "true",
		"resource_stickiness":                "1000",
		"saphana_demote_timeout":             "3600",
		"saphana_promote_timeout":            "3600",
		"saphana_start_timeout":              "3600",
		"saphana_stop_timeout":               "3600",
		"saphana_primary_monitor_interval":   "60",
		"saphana_primary_monitor_timeout":    "700",
		"saphana_secondary_monitor_interval": "61",
		"saphana_secondary_monitor_timeout":  "700",
		"saphanatopology_monitor_interval":   "10",
		"saphanatopology_monitor_timeout":    "600",
		"saphanatopology_start_timeout":      "600",
		"saphanatopology_stop_timeout":       "300",
		"ascs_instance":                      "",
		"ers_instance":                       "",
		"enqueue_server":                     "",
		"saphana_automated_register":         "true",
		"saphana_duplicate_primary_timeout":  "7200",
		"saphana_prefer_site_takeover":       "true",
		"saphana_notify":                     "true",
		"saphana_clone_max":                  "2",
		"saphana_clone_node_max":             "1",
		"saphana_interleave":                 "true",
		"saphanatopology_clone_node_max":     "1",
		"saphanatopology_interleave":         "true",
		"ascs_automatic_recover":             "",
		"ascs_failure_timeout":               "",
		"ascs_migration_threshold":           "",
		"ascs_resource_stickiness":           "",
		"ascs_monitor_interval":              "",
		"ascs_monitor_timeout":               "",
		"ers_automatic_recover":              "",
		"is_ers":                             "",
		"ers_monitor_interval":               "",
		"ers_monitor_timeout":                "",
		"op_timeout":                         "600",
		"stonith_enabled":                    "true",
		"stonith_timeout":                    "300",
		"healthcheck_monitor_interval":       "10",
		"healthcheck_monitor_timeout":        "20",
		"ilb_monitor_interval":               "3600",
		"ilb_monitor_timeout":                "60",
		"ascs_healthcheck_monitor_interval":  "",
		"ascs_healthcheck_monitor_timeout":   "",
		"ascs_ilb_monitor_interval":          "",
		"ascs_ilb_monitor_timeout":           "",
		"ers_healthcheck_monitor_interval":   "",
		"ers_healthcheck_monitor_timeout":    "",
		"ers_ilb_monitor_interval":           "",
		"ers_ilb_monitor_timeout":            "",
		"has_alias_ip":                       "false",
	}
}

func wantClonePacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"fence_agent":                        "fence_gce",
		"pcmk_delay_max":                     "rhel-ha1=30",
		"pcmk_monitor_retries":               "4",
		"pcmk_reboot_timeout":                "300",
		"migration_threshold":                "5000",
		"fence_agent_compute_api_access":     "false",
		"fence_agent_logging_api_access":     "false",
		"location_preference_set":            locationPref,
		"maintenance_mode_active":            "true",
		"resource_stickiness":                "1000",
		"saphana_demote_timeout":             "3600",
		"saphana_promote_timeout":            "3600",
		"saphana_start_timeout":              "3600",
		"saphana_stop_timeout":               "3600",
		"saphana_primary_monitor_interval":   "59",
		"saphana_primary_monitor_timeout":    "700",
		"saphana_secondary_monitor_interval": "61",
		"saphana_secondary_monitor_timeout":  "700",
		"saphanatopology_monitor_interval":   "10",
		"saphanatopology_monitor_timeout":    "600",
		"saphanatopology_start_timeout":      "600",
		"saphanatopology_stop_timeout":       "300",
		"ascs_instance":                      "",
		"ers_instance":                       "",
		"enqueue_server":                     "",
		"ascs_automatic_recover":             "false",
		"ascs_failure_timeout":               "60",
		"ascs_migration_threshold":           "3",
		"ascs_resource_stickiness":           "5000",
		"ascs_monitor_interval":              "20",
		"ascs_monitor_timeout":               "60",
		"ers_automatic_recover":              "false",
		"is_ers":                             "true",
		"ers_monitor_interval":               "20",
		"ers_monitor_timeout":                "60",
		"op_timeout":                         "600",
		"stonith_enabled":                    "true",
		"stonith_timeout":                    "300",
		"saphana_automated_register":         "true",
		"saphana_duplicate_primary_timeout":  "7200",
		"saphana_prefer_site_takeover":       "true",
		"saphana_notify":                     "true",
		"saphana_clone_max":                  "2",
		"saphana_clone_node_max":             "1",
		"saphana_interleave":                 "true",
		"saphanatopology_clone_node_max":     "1",
		"saphanatopology_interleave":         "true",
		"healthcheck_monitor_interval":       "",
		"healthcheck_monitor_timeout":        "",
		"ilb_monitor_interval":               "",
		"ilb_monitor_timeout":                "",
		"ascs_healthcheck_monitor_interval":  "10",
		"ascs_healthcheck_monitor_timeout":   "20",
		"ascs_ilb_monitor_interval":          "3600",
		"ascs_ilb_monitor_timeout":           "60",
		"ers_healthcheck_monitor_interval":   "10",
		"ers_healthcheck_monitor_timeout":    "20",
		"ers_ilb_monitor_interval":           "3600",
		"ers_ilb_monitor_timeout":            "60",
		"has_alias_ip":                       "false",
	}
}

func wantNoPropertiesPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{}
}

func wantSuccessfulAccessPacemakerMetrics(ts *timestamppb.Timestamp, pacemakerExists float64, os string, locationPref string) map[string]string {
	return map[string]string{
		"fence_agent":                       "fence_gce",
		"pcmk_delay_max":                    "test-instance-name=45",
		"pcmk_monitor_retries":              "5",
		"pcmk_reboot_timeout":               "200",
		"fence_agent_compute_api_access":    "true",
		"fence_agent_logging_api_access":    "true",
		"location_preference_set":           locationPref,
		"maintenance_mode_active":           "true",
		"stonith_enabled":                   "",
		"stonith_timeout":                   "",
		"ascs_instance":                     "",
		"ers_instance":                      "",
		"enqueue_server":                    "",
		"saphana_automated_register":        "",
		"saphana_duplicate_primary_timeout": "",
		"saphana_prefer_site_takeover":      "",
		"saphana_notify":                    "",
		"saphana_clone_max":                 "",
		"saphana_clone_node_max":            "",
		"saphana_interleave":                "",
		"saphanatopology_clone_node_max":    "",
		"saphanatopology_interleave":        "",
		"ascs_automatic_recover":            "",
		"ascs_failure_timeout":              "",
		"ascs_migration_threshold":          "",
		"ascs_resource_stickiness":          "",
		"ascs_monitor_interval":             "",
		"ascs_monitor_timeout":              "",
		"ers_automatic_recover":             "",
		"is_ers":                            "",
		"ers_monitor_interval":              "",
		"ers_monitor_timeout":               "",
		"op_timeout":                        "",
		"healthcheck_monitor_interval":      "",
		"healthcheck_monitor_timeout":       "",
		"ilb_monitor_interval":              "",
		"ilb_monitor_timeout":               "",
		"ascs_healthcheck_monitor_interval": "",
		"ascs_healthcheck_monitor_timeout":  "",
		"ascs_ilb_monitor_interval":         "",
		"ascs_ilb_monitor_timeout":          "",
		"ers_healthcheck_monitor_interval":  "",
		"ers_healthcheck_monitor_timeout":   "",
		"ers_ilb_monitor_interval":          "",
		"ers_ilb_monitor_timeout":           "",
		"has_alias_ip":                      "false",
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
		instances  []string
		resources  Resources
		want       map[string]string
		wantLabels map[string]string
	}{
		{
			name:      "TestSetPacemakerPrimitivesImproperTypes",
			c:         defaultConfiguration,
			instances: []string{"fake-id"},
			resources: Resources{
				Primitives: []PrimitiveClass{
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
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
			},
			wantLabels: map[string]string{},
		},
		{
			name:      "TestSetPacemakerPrimitivesNoMatch",
			c:         defaultConfiguration,
			instances: []string{"fake-id"},
			resources: Resources{
				Primitives: []PrimitiveClass{
					{
						ClassType: "fence_gce",
						InstanceAttributes: ClusterPropertySet{
							ID:      "fake-id",
							NVPairs: []NVPair{},
						},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
			},
			wantLabels: map[string]string{},
		},
		{
			name:      "TestSetPacemakerPrimitivesBasicMatch1",
			c:         defaultConfiguration,
			instances: []string{"instance-name"},
			resources: Resources{
				Primitives: []PrimitiveClass{
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
			},
			want: map[string]string{
				"serviceAccountJsonFile": "external/test/account/path",
			},
			wantLabels: map[string]string{},
		},
		{
			name:      "TestSetPacemakerPrimitivesBasicMatch2",
			c:         defaultConfiguration,
			instances: []string{"instance-name", "fake-id"},
			resources: Resources{
				Primitives: []PrimitiveClass{
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
			},
			want: map[string]string{
				"serviceAccountJsonFile": "external/test/account/path2",
			},
			wantLabels: map[string]string{},
		},
		{
			name:      "pcmkDelayMaxSingleValue",
			c:         defaultConfiguration,
			instances: []string{"instance-name-1", "instance-name-2", "instance-name-3", "instance-name-4"},
			resources: Resources{
				Primitives: []PrimitiveClass{
					{
						ClassType: "stonith",
						ID:        "STONITH-instance-name-1",
						InstanceAttributes: ClusterPropertySet{
							ID:      "STONITH-instance-name-1-instance_attributes",
							NVPairs: []NVPair{},
						},
					},
					{
						ClassType: "stonith",
						ID:        "invalid",
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instance-name-2-instance_attributes",
							NVPairs: []NVPair{
								{ID: "STONITH-instance-name-2-instance_attributes-pcmk_delay_max", Name: "pcmk_delay_max", Value: "60"},
							},
						},
					},
					{
						ClassType: "stonith",
						ID:        "STONITH-instance-name-3",
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instance-name-3-instance_attributes",
							NVPairs: []NVPair{
								{ID: "STONITH-invalid-instance_attributes-pcmk_delay_max", Name: "pcmk_delay_max", Value: "90"},
							},
						},
					},
					{
						ClassType: "stonith",
						ID:        "STONITH-instance-name-4",
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instance-name-4-instance_attributes",
							NVPairs: []NVPair{
								{ID: "STONITH-instance-name-4-instance_attributes-pcmk_delay_max", Name: "pcmk_delay_max", Value: "30"},
							},
						},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
			},
			wantLabels: map[string]string{
				"pcmk_delay_max": "instance-name-4=30",
			},
		},
		{
			name:      "pcmkDelayMaxMultipleValues",
			c:         defaultConfiguration,
			instances: []string{"instance-name-1", "instance-name-2"},
			resources: Resources{
				Primitives: []PrimitiveClass{
					{
						ClassType: "stonith",
						ID:        "STONITH-instance-name-1",
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instance-name-1-instance_attributes",
							NVPairs: []NVPair{
								{ID: "STONITH-instance-name-1-instance_attributes-pcmk_delay_max", Name: "pcmk_delay_max", Value: "60"},
							},
						},
					},
					{
						ClassType: "stonith",
						ID:        "STONITH-instance-name-2",
						InstanceAttributes: ClusterPropertySet{
							ID: "STONITH-instance-name-2-instance_attributes",
							NVPairs: []NVPair{
								{ID: "STONITH-instance-name-2-instance_attributes-pcmk_delay_max", Name: "pcmk_delay_max", Value: "30"},
							},
						},
					},
				},
			},
			want: map[string]string{
				"serviceAccountJsonFile": "",
			},
			wantLabels: map[string]string{
				"pcmk_delay_max": "instance-name-1=60,instance-name-2=30",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotLabels := map[string]string{}
			ctx := context.Background()
			got := setPacemakerPrimitives(ctx, gotLabels, test.resources, test.instances, test.c)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerPrimitives() returned unexpected return map diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantLabels, gotLabels); diff != "" {
				t.Errorf("setPacemakerPrimitives() returned unexpected labels diff (-want +got):\n%s", diff)
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

func TestCollectPacemakerMetrics(t *testing.T) {
	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name string

		exec                commandlineexecutor.Execute
		exists              commandlineexecutor.Exists
		config              *cnfpb.Configuration
		workloadConfig      *wvpb.WorkloadValidation
		fileReader          ConfigFileReader
		credGetter          JSONCredentialsGetter
		tokenGetter         DefaultTokenGetter
		wantPacemakerExists float64
		wantPacemakerLabels func(*timestamppb.Timestamp, float64, string, string) map[string]string
		locationPref        string
	}{
		{
			name:                "XMLNotFound",
			exec:                defaultExec,
			exists:              func(string) bool { return false },
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists: float64(0.0),
			wantPacemakerLabels: wantErrorPacemakerMetrics,
		},
		{
			name: "UnparseableXML",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "Error: Bad XML",
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists: float64(0.0),
			wantPacemakerLabels: wantErrorPacemakerMetrics,
		},
		{
			name: "ServiceAccountReadError",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			fileReader:          fileReaderError,
			wantPacemakerExists: float64(0.0),
			wantPacemakerLabels: wantServiceAccountErrorPacemakerMetrics,
		},
		{
			name: "ServiceAccountReadSuccess",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			fileReader:          defaultFileReader,
			credGetter:          defaultCredGetter,
			wantPacemakerExists: float64(1.0),
			wantPacemakerLabels: wantDefaultPacemakerMetrics,
			locationPref:        "false",
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
			workloadConfig: &wvpb.WorkloadValidation{
				ValidationPacemaker: &wvpb.ValidationPacemaker{
					ConfigMetrics: &wvpb.PacemakerConfigMetrics{
						RscLocationMetrics: []*wvpb.PacemakerRSCLocationMetric{
							{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/pacemaker",
									Label: "location_preference_set",
								},
								Value: wvpb.RSCLocationVariable_LOCATION_PREFERENCE_SET,
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
			fileReader:          defaultFileReader,
			credGetter:          defaultCredGetter,
			wantPacemakerExists: float64(1.0),
			wantPacemakerLabels: wantCustomWorkloadConfigMetrics,
			locationPref:        "false",
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
				return commandlineexecutor.Result{
					StdOut: pacemakerServiceAccountXML,
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			fileReader:          defaultFileReader,
			credGetter:          defaultCredGetter,
			wantPacemakerExists: float64(1.0),
			wantPacemakerLabels: wantSuccessfulAccessPacemakerMetrics,
			locationPref:        "false",
		},
		{
			name: "LocationPref",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerClipReferXML,
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultConfiguration,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			fileReader:          defaultFileReader,
			tokenGetter:         defaultToxenGetter,
			wantPacemakerExists: float64(1.0),
			wantPacemakerLabels: wantCLIPreferPacemakerMetrics,
			locationPref:        "true",
		},
		{
			name: "CloneMetrics",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerCloneXML,
					StdErr: "",
				}
			},
			exists: defaultExists,
			config: &cnfpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					InstanceName: "rhel-ha1",
					InstanceId:   "test-instance-id",
					Zone:         "test-region-zone",
					ProjectId:    "test-project-id",
				},
				AgentProperties: &cnfpb.AgentProperties{Name: "sapagent", Version: "1.0"},
				CollectionConfiguration: &cnfpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: wpb.Bool(true),
				},
				SupportConfiguration: &cnfpb.SupportConfiguration{
					SendWorkloadValidationMetricsToCloudMonitoring: &wpb.BoolValue{Value: true},
				},
			},
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			fileReader:          defaultFileReader,
			tokenGetter:         defaultToxenGetter,
			wantPacemakerExists: float64(1.0),
			wantPacemakerLabels: wantClonePacemakerMetrics,
			locationPref:        "false",
		},
		{
			name: "NilCloudProperties",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: pacemakerCloneXML,
					StdErr: "",
				}
			},
			exists:              defaultExists,
			config:              defaultPacemakerConfigNoCloudProperties,
			workloadConfig:      collectionDefinition.GetWorkloadValidation(),
			wantPacemakerExists: float64(0.0),
			wantPacemakerLabels: wantNoPropertiesPacemakerMetrics,
			locationPref:        "false",
		},
	}

	now := func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaultIIR.Read(context.Background(), test.config, defaultMapperFunc)
			want := test.wantPacemakerLabels(nts, test.wantPacemakerExists, "test-os-version", test.locationPref)

			p := Parameters{
				Config:                test.config,
				Execute:               test.exec,
				Exists:                test.exists,
				ConfigFileReader:      test.fileReader,
				DefaultTokenGetter:    test.tokenGetter,
				JSONCredentialsGetter: test.credGetter,
				WorkloadConfig:        test.workloadConfig,
				OSVendorID:            "rhel",
			}
			val, got := CollectPacemakerMetrics(context.Background(), p)
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("CollectPacemakerMetricsFromConfig() returned unexpected metric labels diff (-want +got):\n%s", diff)
			}
			if val != test.wantPacemakerExists {
				t.Errorf("CollectPacemakerMetricsFromConfig() returned unexpected value: got %v, want %v", val, test.wantPacemakerExists)
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
				{
					Name:     "monitor",
					Role:     "Master",
					Interval: "5",
					Timeout:  "6",
				},
				{
					Name:     "monitor",
					Role:     "Slave",
					Interval: "7",
					Timeout:  "8",
				},
			},
			want: map[string]string{
				"saphana_start_timeout":              "1",
				"saphana_stop_timeout":               "2",
				"saphana_promote_timeout":            "3",
				"saphana_demote_timeout":             "4",
				"saphana_primary_monitor_interval":   "5",
				"saphana_primary_monitor_timeout":    "6",
				"saphana_secondary_monitor_interval": "7",
				"saphana_secondary_monitor_timeout":  "8",
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

func TestPacemakerHanaTopology(t *testing.T) {
	tests := []struct {
		name string
		ops  []Op
		want map[string]string
	}{
		{
			name: "monitorPrimitiveNotFound",
			ops: []Op{
				{
					Name:    "invalid",
					Timeout: "0",
				},
			},
			want: map[string]string{},
		},
		{
			name: "monitorPrimitiveFound",
			ops: []Op{
				{
					Name:    "start",
					Timeout: "1",
				},
				{
					Name:    "stop",
					Timeout: "2",
				},
				{
					Name:     "monitor",
					Timeout:  "600",
					Interval: "30",
				},
			},
			want: map[string]string{
				"saphanatopology_monitor_timeout":  "600",
				"saphanatopology_monitor_interval": "30",
				"saphanatopology_start_timeout":    "1",
				"saphanatopology_stop_timeout":     "2",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}

			pacemakerHanaTopology(got, test.ops)

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("pacemakerHanaTopology() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectASCSInstance(t *testing.T) {
	tests := []struct {
		name             string
		exists           commandlineexecutor.Exists
		exec             commandlineexecutor.Execute
		wantASCSInstance string
		wantERSInstance  string
	}{
		{
			name:   "CommandLineToolNotExists",
			exists: func(string) bool { return false },
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `
Full List of Resources:
  * gce_stonith_sap-posers12    (stonith:fence_gce):     Started sap-posascs11 (unmanaged)
  * gce_stonith_sap-posascs11   (stonith:fence_gce):     Started sap-posers12 (unmanaged)
  * Resource Group: ers_resource_group (unmanaged):
    * gcp_ers_instance  (ocf::heartbeat:SAPInstance):    Started sap-posers12 (unmanaged)
  * Resource Group: ascs_resource_group (unmanaged):
    * gcp_ascs_instance (ocf::heartbeat:SAPInstance):    Started sap-posascs11 (unmanaged)
`,
				}
			},
			wantASCSInstance: "",
			wantERSInstance:  "",
		},
		{
			name:   "CommandLineStatusError",
			exists: defaultExists,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: errors.New("Something went wrong"),
				}
			},
			wantASCSInstance: "",
			wantERSInstance:  "",
		},
		{
			name:   "ResourceGroupNotFound",
			exists: defaultExists,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `
Full List of Resources:
  * gce_stonith_sap-posascs11   (stonith:fence_gce):     Started sap-posers12 (unmanaged)
  * Resource Group: other_resource_group (unmanaged):
    * gcp_ascs_instance (ocf::heartbeat:SAPInstance):    Started sap-posascs11 (unmanaged)
    * gcp_ers_instance  (ocf::heartbeat:SAPInstance):    Started sap-posers12 (unmanaged)
`,
				}
			},
			wantASCSInstance: "",
			wantERSInstance:  "",
		},
		{
			name:   "InstanceNotFound",
			exists: defaultExists,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `
Full List of Resources:
  * gce_stonith_sap-posers12    (stonith:fence_gce):     Started sap-posascs11 (unmanaged)
  * gce_stonith_sap-posascs11   (stonith:fence_gce):     Started sap-posers12 (unmanaged)
  * Resource Group: ascs_resource_group (unmanaged):
    * fs_ascs_POS       (ocf::heartbeat:Filesystem):     Started sap-posascs11 (unmanaged)
    * gcp_ascs_healthcheck      (ocf::heartbeat:anything):       Started sap-posascs11 (unmanaged)
    * gcp_ipaddr2_ascs  (ocf::heartbeat:IPaddr2):        Started sap-posascs11 (unmanaged)
  * Resource Group: ers_resource_group (unmanaged):
    * fs_ers_POS        (ocf::heartbeat:Filesystem):     Started sap-posers12 (unmanaged)
    * gcp_ers_healthcheck       (ocf::heartbeat:anything):       Started sap-posers12 (unmanaged)
    * gcp_ipaddr2_ers   (ocf::heartbeat:IPaddr2):        Started sap-posers12 (unmanaged)
`,
				}
			},
			wantASCSInstance: "",
			wantERSInstance:  "",
		},
		{
			name:   "InstanceParseError",
			exists: defaultExists,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `
Full List of Resources:
  * gce_stonith_sap-posers12    (stonith:fence_gce):     Started sap-posascs11 (unmanaged)
  * gce_stonith_sap-posascs11   (stonith:fence_gce):     Started sap-posers12 (unmanaged)
  * Resource Group: ascs_resource_group (unmanaged):
    * gcp_ascs_instance (ocf::heartbeat:SAPInstance):    Stopped sap-posascs11 (unmanaged)
  * Resource Group: ers_resource_group (unmanaged):
    * gcp_ers_instance  (ocf::heartbeat:SAPInstance):    Stopped sap-posers12 (unmanaged)
`,
				}
			},
			wantASCSInstance: "",
			wantERSInstance:  "",
		},
		{
			name:   "Success",
			exists: defaultExists,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `
Full List of Resources:
  * gce_stonith_sap-posers12    (stonith:fence_gce):     Started sap-posascs11 (unmanaged)
  * gce_stonith_sap-posascs11   (stonith:fence_gce):     Started sap-posers12 (unmanaged)
  * Resource Group: ers_resource_group (unmanaged):
    * gcp_ers_instance  (ocf::heartbeat:SAPInstance):    Started sap-posers12 (unmanaged)
  * Resource Group: ascs_resource_group (unmanaged):
    * gcp_ascs_instance (ocf::heartbeat:SAPInstance):    Started sap-posascs11 (unmanaged)
`,
				}
			},
			wantASCSInstance: "sap-posascs11",
			wantERSInstance:  "sap-posers12",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			collectASCSInstance(context.Background(), got, test.exists, test.exec)
			if got["ascs_instance"] != test.wantASCSInstance {
				t.Errorf("collectASCSInstance() got %v, want %v", got["ascs_instance"], test.wantASCSInstance)
			}
			if got["ers_instance"] != test.wantERSInstance {
				t.Errorf("collectASCSInstance() got %v, want %v", got["ers_instance"], test.wantERSInstance)
			}
		})
	}
}

func TestCollectEnqueueServer(t *testing.T) {
	tests := []struct {
		name string
		exec commandlineexecutor.Execute
		want string
	}{
		{
			name: "sapservicesError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "sapservices") {
					return commandlineexecutor.Result{
						Error: errors.New("Something went wrong"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: "enq/replicatorhost = aliders11",
				}
			},
			want: "",
		},
		{
			name: "sapservicesParseError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "sapservices") {
					return commandlineexecutor.Result{
						StdOut: `
#systemctl --no-ask-password start SAPPOS_11 # sapstartsrv pf=/sapmnt/POS/profile/POS_ASCS11_alidascs11
systemctl --no-ask-password start SAPPOS_12 # sapstartsrv pf=/usr/sap/POS/SYS/profile/POS_ERS12_aliders11
`,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "enq/replicatorhost = aliders11",
				}
			},
			want: "",
		},
		{
			name: "replicatorError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "enq/replicator") {
					return commandlineexecutor.Result{
						Error:    errors.New("Something went wrong"),
						ExitCode: 2,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "systemctl --no-ask-password start SAPPOS_11 # sapstartsrv pf=/sapmnt/POS/profile/POS_ASCS11_alidascs11",
				}
			},
			want: "",
		},
		{
			name: "ENSA1",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "enq/replicator") {
					return commandlineexecutor.Result{
						ExitCode: 1,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "systemctl --no-ask-password start SAPPOS_11 # sapstartsrv pf=/sapmnt/POS/profile/POS_ASCS11_alidascs1",
				}
			},
			want: "ENSA1",
		},
		{
			name: "ENSA2",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "enq/replicator") {
					return commandlineexecutor.Result{
						StdOut:   "enq/replicatorhost = aliders11",
						ExitCode: 0,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "systemctl --no-ask-password start SAPPOS_11 # sapstartsrv pf=/sapmnt/POS/profile/POS_ASCS11_alidascs1",
				}
			},
			want: "ENSA2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			collectEnqueueServer(context.Background(), got, test.exec)
			if got["enqueue_server"] != test.want {
				t.Errorf("collectEnqueueServer() got %v, want %v", got["enqueue_server"], test.want)
			}
		})
	}
}

func TestSetASCSConfigMetrics(t *testing.T) {
	validInstanceAttributes := ClusterPropertySet{
		NVPairs: []NVPair{
			{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ASCS12_alidascs11"},
			{Name: "AUTOMATIC_RECOVER", Value: "false"},
		},
	}
	validMetaAttributes := ClusterPropertySet{
		NVPairs: []NVPair{
			{Name: "failure-timeout", Value: "60"},
			{Name: "migration-threshold", Value: "3"},
			{Name: "resource-stickiness", Value: "5000"},
		},
	}
	validOperations := []Op{
		{Name: "monitor", Interval: "20", Timeout: "60"},
	}
	wantEmpty := map[string]string{
		"ascs_automatic_recover":   "",
		"ascs_failure_timeout":     "",
		"ascs_migration_threshold": "",
		"ascs_resource_stickiness": "",
		"ascs_monitor_interval":    "",
		"ascs_monitor_timeout":     "",
	}

	tests := []struct {
		name   string
		groups []Group
		want   map[string]string
	}{
		{
			name:   "NoGroups",
			groups: []Group{},
			want:   wantEmpty,
		},
		{
			name: "NoASCSGroup",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ERS10_aliders11"},
									{Name: "AUTOMATIC_RECOVER", Value: "false"},
								},
							},
							MetaAttributes: validMetaAttributes,
							Operations:     validOperations,
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "NoSAPInstanceType",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:          "NotSAPInstance",
							InstanceAttributes: validInstanceAttributes,
							MetaAttributes:     validMetaAttributes,
							Operations:         validOperations,
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "KeyMismatch",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ASCS12_alidascs11"},
									{Name: "not-AUTOMATIC_RECOVER", Value: "false"},
								},
							},
							MetaAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									{Name: "not-failure-timeout", Value: "60"},
									{Name: "not-migration-threshold", Value: "3"},
									{Name: "not-resource-stickiness", Value: "5000"},
								},
							},
							Operations: []Op{
								{Name: "not-monitor", Interval: "20", Timeout: "60"},
							},
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "Success",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:          "SAPInstance",
							InstanceAttributes: validInstanceAttributes,
							MetaAttributes:     validMetaAttributes,
							Operations:         validOperations,
						},
					},
				},
			},
			want: map[string]string{
				"ascs_automatic_recover":   "false",
				"ascs_failure_timeout":     "60",
				"ascs_migration_threshold": "3",
				"ascs_resource_stickiness": "5000",
				"ascs_monitor_interval":    "20",
				"ascs_monitor_timeout":     "60",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setASCSConfigMetrics(context.Background(), got, test.groups)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setASCSConfigMetrics() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetERSConfigMetrics(t *testing.T) {
	validInstanceAttributes := ClusterPropertySet{
		NVPairs: []NVPair{
			{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ERS10_aliders11"},
			{Name: "AUTOMATIC_RECOVER", Value: "false"},
			{Name: "IS_ERS", Value: "true"},
		},
	}
	validOperations := []Op{
		{Name: "monitor", Interval: "20", Timeout: "60"},
	}
	wantEmpty := map[string]string{
		"ers_automatic_recover": "",
		"is_ers":                "",
		"ers_monitor_interval":  "",
		"ers_monitor_timeout":   "",
	}

	tests := []struct {
		name   string
		groups []Group
		want   map[string]string
	}{
		{
			name:   "NoGroups",
			groups: []Group{},
			want:   wantEmpty,
		},
		{
			name: "NoERSGroup",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ASCS12_alidascs11"},
									{Name: "AUTOMATIC_RECOVER", Value: "false"},
								},
							},
							Operations: validOperations,
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "NoSAPInstanceType",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:          "NotSAPInstance",
							InstanceAttributes: validInstanceAttributes,
							Operations:         validOperations,
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "KeyMismatch",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ERS10_aliders11"},
									{Name: "not-AUTOMATIC_RECOVER", Value: "false"},
									{Name: "not-IS_ERS", Value: "true"},
								},
							},
							Operations: []Op{
								{Name: "not-monitor", Interval: "20", Timeout: "60"},
							},
						},
					},
				},
			},
			want: wantEmpty,
		},
		{
			name: "Success",
			groups: []Group{
				{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:          "SAPInstance",
							InstanceAttributes: validInstanceAttributes,
							Operations:         validOperations,
						},
					},
				},
			},
			want: map[string]string{
				"ers_automatic_recover": "false",
				"is_ers":                "true",
				"ers_monitor_interval":  "20",
				"ers_monitor_timeout":   "60",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setERSConfigMetrics(context.Background(), got, test.groups)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setERSConfigMetrics() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetOPOptions(t *testing.T) {
	tests := []struct {
		name      string
		opOptions ClusterPropertySet
		want      map[string]string
	}{
		{
			name: "Success",
			opOptions: ClusterPropertySet{
				ID: "op-options",
				NVPairs: []NVPair{
					{Name: "timeout", Value: "600"},
				},
			},
			want: map[string]string{
				"op_timeout": "600",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setOPOptions(got, test.opOptions)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setOPOptions() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerStonithClusterProperty(t *testing.T) {
	tests := []struct {
		name string
		cps  []ClusterPropertySet
		want map[string]string
	}{
		{
			name: "NoCIBBootstrapOptions",
			cps: []ClusterPropertySet{
				{
					ID: "not-cib-bootstrap-options",
					NVPairs: []NVPair{
						{Name: "stonith-enabled", Value: "true"},
						{Name: "stonith-timeout", Value: "10"},
					},
				},
			},
			want: map[string]string{
				"stonith_enabled": "",
				"stonith_timeout": "",
			},
		},
		{
			name: "Success",
			cps: []ClusterPropertySet{
				{
					ID: "cib-bootstrap-options",
					NVPairs: []NVPair{
						{Name: "stonith-enabled", Value: "true"},
						{Name: "stonith-timeout", Value: "10"},
					},
				},
			},
			want: map[string]string{
				"stonith_enabled": "true",
				"stonith_timeout": "10",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerStonithClusterProperty(got, test.cps)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerStonithClusterProperty() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerHANACloneAttrs(t *testing.T) {
	tests := []struct {
		name   string
		clones []Clone
		want   map[string]string
	}{
		{
			name: "NoMetaAttributes",
			clones: []Clone{
				Clone{
					ID: "NoMetaAttributes",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHana",
						},
					},
				},
			},
			want: map[string]string{
				"saphana_automated_register":        "",
				"saphana_duplicate_primary_timeout": "",
				"saphana_prefer_site_takeover":      "",
				"saphana_notify":                    "",
				"saphana_clone_max":                 "",
				"saphana_clone_node_max":            "",
				"saphana_interleave":                "",
			},
		},
		{
			name: "SuccessRHEL",
			clones: []Clone{
				Clone{
					ID: "SAPHanaTopology",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHanaTopology",
							MetaAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "notify", Value: "false"},
									NVPair{Name: "clone-max", Value: "20"},
									NVPair{Name: "clone-node-max", Value: "10"},
									NVPair{Name: "interleave", Value: "false"},
								},
							},
						},
					},
				},
				Clone{
					ID: "SAPHana",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHana",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "AUTOMATED_REGISTER", Value: "true"},
									NVPair{Name: "DUPLICATE_PRIMARY_TIMEOUT", Value: "7200"},
									NVPair{Name: "PREFER_SITE_TAKEOVER", Value: "true"},
								},
							},
							MetaAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "notify", Value: "true"},
									NVPair{Name: "clone-max", Value: "2"},
									NVPair{Name: "clone-node-max", Value: "1"},
									NVPair{Name: "interleave", Value: "true"},
								},
							},
						},
					},
				},
			},
			want: map[string]string{
				"saphana_automated_register":        "true",
				"saphana_duplicate_primary_timeout": "7200",
				"saphana_prefer_site_takeover":      "true",
				"saphana_notify":                    "true",
				"saphana_clone_max":                 "2",
				"saphana_clone_node_max":            "1",
				"saphana_interleave":                "true",
			},
		},
		{
			name: "SuccessSLES",
			clones: []Clone{
				Clone{
					ID: "SAPHanaTopology",
					Attributes: ClusterPropertySet{
						NVPairs: []NVPair{
							NVPair{Name: "notify", Value: "false"},
							NVPair{Name: "clone-max", Value: "20"},
							NVPair{Name: "clone-node-max", Value: "10"},
							NVPair{Name: "interleave", Value: "false"},
						},
					},
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHanaTopology",
						},
					},
				},
				Clone{
					ID: "SAPHana",
					Attributes: ClusterPropertySet{
						NVPairs: []NVPair{
							NVPair{Name: "notify", Value: "true"},
							NVPair{Name: "clone-max", Value: "2"},
							NVPair{Name: "clone-node-max", Value: "1"},
							NVPair{Name: "interleave", Value: "true"},
						},
					},
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHana",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "AUTOMATED_REGISTER", Value: "true"},
									NVPair{Name: "DUPLICATE_PRIMARY_TIMEOUT", Value: "7200"},
									NVPair{Name: "PREFER_SITE_TAKEOVER", Value: "true"},
								},
							},
						},
					},
				},
			},
			want: map[string]string{
				"saphana_automated_register":        "true",
				"saphana_duplicate_primary_timeout": "7200",
				"saphana_prefer_site_takeover":      "true",
				"saphana_notify":                    "true",
				"saphana_clone_max":                 "2",
				"saphana_clone_node_max":            "1",
				"saphana_interleave":                "true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerHANACloneAttrs(got, test.clones)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerHANACloneAttrs() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetPacemakerHANATopologyCloneAttrs(t *testing.T) {
	tests := []struct {
		name   string
		clones []Clone
		want   map[string]string
	}{
		{
			name: "NoMetaAttributes",
			clones: []Clone{
				Clone{
					ID: "NoMetaAttributes",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHanaTopology",
						},
					},
				},
			},
			want: map[string]string{
				"saphanatopology_clone_node_max": "",
				"saphanatopology_interleave":     "",
			},
		},
		{
			name: "SuccessRHEL",
			clones: []Clone{
				Clone{
					ID: "SAPHana",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHana",
							MetaAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "clone-node-max", Value: "10"},
									NVPair{Name: "interleave", Value: "false"},
								},
							},
						},
					},
				},
				Clone{
					ID: "SAPHanaTopology",
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHanaTopology",
							MetaAttributes: ClusterPropertySet{
								NVPairs: []NVPair{
									NVPair{Name: "clone-node-max", Value: "1"},
									NVPair{Name: "interleave", Value: "true"},
								},
							},
						},
					},
				},
			},
			want: map[string]string{
				"saphanatopology_clone_node_max": "1",
				"saphanatopology_interleave":     "true",
			},
		},
		{
			name: "SuccessSLES",
			clones: []Clone{
				Clone{
					ID: "SAPHana",
					Attributes: ClusterPropertySet{
						NVPairs: []NVPair{
							NVPair{Name: "clone-node-max", Value: "10"},
							NVPair{Name: "interleave", Value: "false"},
						},
					},
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHana",
						},
					},
				},
				Clone{
					ID: "SAPHanaTopology",
					Attributes: ClusterPropertySet{
						NVPairs: []NVPair{
							NVPair{Name: "clone-node-max", Value: "1"},
							NVPair{Name: "interleave", Value: "true"},
						},
					},
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType: "SAPHanaTopology",
						},
					},
				},
			},
			want: map[string]string{
				"saphanatopology_clone_node_max": "1",
				"saphanatopology_interleave":     "true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setPacemakerHANATopologyCloneAttrs(got, test.clones)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setPacemakerHANATopologyCloneAttrs() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetHealthCheckInternalLoadBalancerMetrics(t *testing.T) {
	tests := []struct {
		name   string
		groups []Group
		want   map[string]string
	}{
		{
			name:   "NoGroups",
			groups: []Group{},
			want: map[string]string{
				"healthcheck_monitor_interval":      "",
				"healthcheck_monitor_timeout":       "",
				"ilb_monitor_interval":              "",
				"ilb_monitor_timeout":               "",
				"ascs_healthcheck_monitor_interval": "",
				"ascs_healthcheck_monitor_timeout":  "",
				"ascs_ilb_monitor_interval":         "",
				"ascs_ilb_monitor_timeout":          "",
				"ers_healthcheck_monitor_interval":  "",
				"ers_healthcheck_monitor_timeout":   "",
				"ers_ilb_monitor_interval":          "",
				"ers_ilb_monitor_timeout":           "",
				"has_alias_ip":                      "false",
			},
		},
		{
			name: "DatabaseSettings",
			groups: []Group{
				Group{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:  "haproxy",
							Operations: []Op{{Name: "monitor", Interval: "10s", Timeout: "20s"}},
						},
						PrimitiveClass{
							ClassType: "IPaddr2",
							Provider:  "gcp",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{{Name: "type", Value: "alias"}},
							},
							Operations: []Op{{Name: "monitor", Interval: "3600s", Timeout: "60s"}},
						},
					},
				},
			},
			want: map[string]string{
				"healthcheck_monitor_interval":      "10",
				"healthcheck_monitor_timeout":       "20",
				"ilb_monitor_interval":              "3600",
				"ilb_monitor_timeout":               "60",
				"ascs_healthcheck_monitor_interval": "",
				"ascs_healthcheck_monitor_timeout":  "",
				"ascs_ilb_monitor_interval":         "",
				"ascs_ilb_monitor_timeout":          "",
				"ers_healthcheck_monitor_interval":  "",
				"ers_healthcheck_monitor_timeout":   "",
				"ers_ilb_monitor_interval":          "",
				"ers_ilb_monitor_timeout":           "",
				"has_alias_ip":                      "true",
			},
		},
		{
			name: "CentralServicesSettings",
			groups: []Group{
				Group{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:  "anything",
							Operations: []Op{{Name: "monitor", Interval: "10s", Timeout: "20s"}},
						},
						PrimitiveClass{
							ClassType: "IPaddr2",
							Provider:  "gcp",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{{Name: "type", Value: "alias"}},
							},
							Operations: []Op{{Name: "monitor", Interval: "3600s", Timeout: "60s"}},
						},
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ASCS01_alidascs01"}},
							},
						},
					},
				},
				Group{
					Primitives: []PrimitiveClass{
						PrimitiveClass{
							ClassType:  "anything",
							Operations: []Op{{Name: "monitor", Interval: "10s", Timeout: "20s"}},
						},
						PrimitiveClass{
							ClassType:  "IPaddr2",
							Operations: []Op{{Name: "monitor", Interval: "3600s", Timeout: "60s"}},
						},
						PrimitiveClass{
							ClassType: "SAPInstance",
							InstanceAttributes: ClusterPropertySet{
								NVPairs: []NVPair{{Name: "START_PROFILE", Value: "/sapmnt/NW1/profile/NW1_ERS01_aliders01"}},
							},
						},
					},
				},
			},
			want: map[string]string{
				"healthcheck_monitor_interval":      "",
				"healthcheck_monitor_timeout":       "",
				"ilb_monitor_interval":              "",
				"ilb_monitor_timeout":               "",
				"ascs_healthcheck_monitor_interval": "10",
				"ascs_healthcheck_monitor_timeout":  "20",
				"ascs_ilb_monitor_interval":         "3600",
				"ascs_ilb_monitor_timeout":          "60",
				"ers_healthcheck_monitor_interval":  "10",
				"ers_healthcheck_monitor_timeout":   "20",
				"ers_ilb_monitor_interval":          "3600",
				"ers_ilb_monitor_timeout":           "60",
				"has_alias_ip":                      "true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := map[string]string{}
			setHealthCheckInternalLoadBalancerMetrics(got, test.groups)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setHealthCheckInternalLoadBalancerMetrics() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
