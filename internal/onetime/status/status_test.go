/*
Copyright 2024 Google LLC

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

// Package status implements the status subcommand to provide information
// on the agent, configuration, IAM and functional statuses.

package status

import (
	"context"
	"fmt"
	"testing"

	"flag"
	store "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/status"
)

const (
	defaultBucketName = "fake-bucket"
)

var (
	defaultStorageClient = func(ctx context.Context, opts ...option.ClientOption) (*store.Client, error) {
		return fakeServer(defaultBucketName).Client(), nil
	}
	defaultBackintIAMPermissions = []*spb.IAMPermission{
		{Name: "storage.objects.list"},
		{Name: "storage.objects.create"},
		{Name: "storage.objects.get"},
		{Name: "storage.objects.update"},
		{Name: "storage.objects.delete"},
		{Name: "storage.multipartUploads.create"},
		{Name: "storage.multipartUploads.abort"},
	}
)

func fakeServer(bucketName string) *fakestorage.Server {
	return fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object.txt",
			},
			Content: []byte("hello world"),
		},
	})
}

func TestSynopsisForStatus(t *testing.T) {
	want := "get the status of the agent and its services"
	s := Status{}
	got := s.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForStatus(t *testing.T) {
	s := Status{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"config", "c", "v", "h"}
	s.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestExecuteStatus(t *testing.T) {
	tests := []struct {
		name string
		s    Status
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "SuccessForHelp",
			s: Status{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.s, test.args, got, test.want)
			}
		})
	}
}

func TestStatusHandler(t *testing.T) {
	tests := []struct {
		name    string
		s       Status
		want    *spb.AgentStatus
		wantErr error
	}{
		{
			name: "SuccessEmptyConfig",
			s: Status{
				readFile: func(string) ([]byte, error) {
					return nil, nil
				},
				backintReadFile: func(string) ([]byte, error) {
					return nil, nil
				},
				exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{StdOut: "enabled", StdErr: "", ExitCode: 0, Error: nil}
				},
				exists: func(string) bool {
					return true
				},
				cloudProps: &ipb.CloudProperties{
					Scopes: []string{requiredScope},
				},
			},
			want: &spb.AgentStatus{
				AgentName:                       agentPackageName,
				InstalledVersion:                fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
				AvailableVersion:                "enabled",
				SystemdServiceEnabled:           spb.State_SUCCESS_STATE,
				SystemdServiceRunning:           spb.State_SUCCESS_STATE,
				ConfigurationFilePath:           configuration.LinuxConfigPath,
				ConfigurationValid:              spb.State_SUCCESS_STATE,
				CloudApiAccessFullScopesGranted: spb.State_SUCCESS_STATE,
				Services: []*spb.ServiceStatus{
					{
						Name:  "Host Metrics",
						State: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "example.compute.viewer"},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
						},
					},
					{
						Name:           "Process Metrics",
						State:          spb.State_FAILURE_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_process_metrics", Value: "false", IsDefault: true},
							{Name: "process_metrics_frequency", Value: "0", IsDefault: false},
							{Name: "process_metrics_to_skip", Value: "[]", IsDefault: true},
							{Name: "slow_process_metrics_frequency", Value: "0", IsDefault: false},
						},
					},
					{
						Name:           "HANA Monitoring Metrics",
						State:          spb.State_FAILURE_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "connection_timeout", Value: "0", IsDefault: false},
							{Name: "enabled", Value: "false", IsDefault: true},
							{Name: "execution_threads", Value: "0", IsDefault: false},
							{Name: "max_connect_retries", Value: "0", IsDefault: false},
							{Name: "query_timeout_sec", Value: "0", IsDefault: false},
							{Name: "sample_interval_sec", Value: "0", IsDefault: false},
							{Name: "send_query_response_time", Value: "false", IsDefault: true},
						},
					},
					{
						Name:           "System Discovery",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "enable_discovery", Value: "true", IsDefault: true},
							{Name: "enable_workload_discovery", Value: "true", IsDefault: true},
							{Name: "sap_instances_update_frequency", Value: "60", IsDefault: true},
							{Name: "system_discovery_update_frequency", Value: "14400", IsDefault: true},
						},
					},
					{
						Name:                      "Backint",
						State:                     spb.State_UNSPECIFIED_STATE,
						EnabledUnspecifiedMessage: "Backint parameters file not specified / Disabled",
						IamPermissions:            defaultBackintIAMPermissions,
						ConfigValues:              []*spb.ConfigValue{},
					},
					{
						Name:           "Disk Snapshot",
						State:          spb.State_UNSPECIFIED_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues:   []*spb.ConfigValue{},
					},
					{
						Name:           "Workload Manager",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
							{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
							{Name: "fetch_latest_config", Value: "true", IsDefault: true},
							{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
							{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
						},
					},
				},
			},
		},
		{
			name: "SuccessAllEnabled",
			s: Status{
				readFile: func(string) ([]byte, error) {
					return []byte(`
{
  "provide_sap_host_agent_metrics": true,
  "log_level": "INFO",
  "log_to_cloud": true,
  "collection_configuration": {
    "collect_workload_validation_metrics": true,
    "collect_process_metrics": true
  },
  "discovery_configuration": {
    "enable_discovery": true
  },
  "hana_monitoring_configuration": {
    "enabled": true
  }
}
`), nil
				},
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{
  "bucket": "fake-bucket",
  "log_to_cloud": true,
	"parallel_streams": 16,
	"threads": 1,
	"service_account_key": "fake-key"
}
`), nil
				},
				exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{StdOut: "", StdErr: "error", ExitCode: 0, Error: fmt.Errorf("error")}
				},
				exists: func(string) bool {
					return false
				},
				cloudProps: &ipb.CloudProperties{
					Scopes: []string{},
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
			},
			want: &spb.AgentStatus{
				AgentName:                       agentPackageName,
				InstalledVersion:                fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
				AvailableVersion:                fetchLatestVersionError,
				SystemdServiceEnabled:           spb.State_ERROR_STATE,
				SystemdServiceRunning:           spb.State_ERROR_STATE,
				ConfigurationFilePath:           configuration.LinuxConfigPath,
				ConfigurationValid:              spb.State_SUCCESS_STATE,
				CloudApiAccessFullScopesGranted: spb.State_FAILURE_STATE,
				Services: []*spb.ServiceStatus{
					{
						Name:  "Host Metrics",
						State: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "example.compute.viewer"},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
						},
					},
					{
						Name:           "Process Metrics",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_process_metrics", Value: "true", IsDefault: false},
							{Name: "process_metrics_frequency", Value: "5", IsDefault: true},
							{Name: "process_metrics_to_skip", Value: "[]", IsDefault: true},
							{Name: "slow_process_metrics_frequency", Value: "30", IsDefault: true},
						},
					},
					{
						Name:           "HANA Monitoring Metrics",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "connection_timeout", Value: "120", IsDefault: true},
							{Name: "enabled", Value: "true", IsDefault: false},
							{Name: "execution_threads", Value: "10", IsDefault: true},
							{Name: "max_connect_retries", Value: "1", IsDefault: true},
							{Name: "query_timeout_sec", Value: "300", IsDefault: true},
							{Name: "sample_interval_sec", Value: "300", IsDefault: true},
							{Name: "send_query_response_time", Value: "false", IsDefault: true},
						},
					},
					{
						Name:           "System Discovery",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "enable_discovery", Value: "true", IsDefault: true},
							{Name: "enable_workload_discovery", Value: "true", IsDefault: true},
							{Name: "sap_instances_update_frequency", Value: "60", IsDefault: true},
							{Name: "system_discovery_update_frequency", Value: "14400", IsDefault: true},
						},
					},
					{
						Name:            "Backint",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions:  defaultBackintIAMPermissions,
						ConfigValues: []*spb.ConfigValue{
							{Name: "bucket", Value: "fake-bucket", IsDefault: false},
							{Name: "log_to_cloud", Value: "true", IsDefault: true},
							{Name: "param_file", Value: "fake-path/backint-gcs/parameters.json", IsDefault: false},
							{Name: "parallel_streams", Value: "16", IsDefault: false},
							{Name: "service_account_key", Value: "***", IsDefault: false},
							{Name: "threads", Value: "1", IsDefault: false},
						},
					},
					{
						Name:           "Disk Snapshot",
						State:          spb.State_UNSPECIFIED_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues:   []*spb.ConfigValue{},
					},
					{
						Name:           "Workload Manager",
						State:          spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
							{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
							{Name: "fetch_latest_config", Value: "true", IsDefault: true},
							{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
							{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.s.statusHandler(context.Background())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("statusHandler() returned unexpected diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("statusHandler()=%v want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestBackintStatusFailures(t *testing.T) {
	tests := []struct {
		name string
		s    Status
		want *spb.ServiceStatus
	}{
		{
			name: "NoBackintParametersFile",
			s: Status{
				backintReadFile: func(string) ([]byte, error) {
					return nil, nil
				},
			},
			want: &spb.ServiceStatus{
				Name:                      "Backint",
				State:                     spb.State_UNSPECIFIED_STATE,
				EnabledUnspecifiedMessage: "Backint parameters file not specified / Disabled",
				IamPermissions:            defaultBackintIAMPermissions,
				ConfigValues:              []*spb.ConfigValue{},
			},
		},
		{
			name: "FailedToParseBackintParametersFile",
			s: Status{
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{

}
`), nil
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
			},
			want: &spb.ServiceStatus{
				Name:           "Backint",
				State:          spb.State_ERROR_STATE,
				ErrorMessage:   `bucket must be provided`,
				IamPermissions: defaultBackintIAMPermissions,
				ConfigValues:   []*spb.ConfigValue{},
			},
		},
		{
			name: "FailedToConnectToBucket",
			s: Status{
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{
  "bucket": "bucket-does-not-exist",
  "log_to_cloud": true,
	"threads": 1
}
`), nil
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
			},
			want: &spb.ServiceStatus{
				Name:            "Backint",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "Failed to connect to bucket",
				IamPermissions:  defaultBackintIAMPermissions,
				ConfigValues: []*spb.ConfigValue{
					{Name: "bucket", Value: "bucket-does-not-exist", IsDefault: false},
					{Name: "log_to_cloud", Value: "true", IsDefault: true},
					{Name: "param_file", Value: "fake-path/backint-gcs/parameters.json", IsDefault: false},
					{Name: "threads", Value: "1", IsDefault: false},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.backintStatus(context.Background())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("backintStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
