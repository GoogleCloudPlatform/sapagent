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
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"flag"
	store "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/iam"
	"github.com/GoogleCloudPlatform/sapagent/shared/iam"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/statushelper"

	arpb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/status"
)

const (
	defaultBucketName = "fake-bucket"
)

var (
	defaultStorageClient = func(ctx context.Context, opts ...option.ClientOption) (*store.Client, error) {
		return fakeServer(defaultBucketName).Client(), nil
	}
)

type mockArtifactRegistryClient struct {
	Versions []*arpb.Version
}

type mockVersionIterator struct {
	Versions []*arpb.Version
}

func (m *mockArtifactRegistryClient) ListVersions(ctx context.Context, req *arpb.ListVersionsRequest, opts ...gax.CallOption) statushelper.VersionIterator {
	return &mockVersionIterator{Versions: m.Versions}
}

func (m *mockVersionIterator) Next() (*arpb.Version, error) {
	if len(m.Versions) == 0 {
		return nil, iterator.Done
	}
	version := m.Versions[0]
	m.Versions = m.Versions[1:]
	return version, nil
}

func fakeArtifactRegistryClient(versions []string) statushelper.ARClientInterface {
	var arVersions []*arpb.Version
	for _, version := range versions {
		arVersions = append(arVersions, &arpb.Version{Name: version})
	}
	return &mockArtifactRegistryClient{Versions: arVersions}
}

type mockFileInfo struct{ perm os.FileMode }

func (m *mockFileInfo) Name() string       { return "mock_file" }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return m.perm }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }

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

func httpGetFailure(url string) (*http.Response, error) {
	return nil, fmt.Errorf("endpoint failure")
}

func httpGetError(url string) (*http.Response, error) {
	return &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("internal error")),
	}, nil
}

func httpGetSuccess(url string) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader("{\"metrics\": [\"test\"]}")),
	}, nil
}

func dbConnectorSuccess(ctx context.Context, p databaseconnector.Params) (*databaseconnector.DBHandle, error) {
	return &databaseconnector.DBHandle{}, nil
}

func dbConnectorFailure(ctx context.Context, p databaseconnector.Params) (*databaseconnector.DBHandle, error) {
	return nil, fmt.Errorf("connection failure")
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
	flags := []string{"config", "c", "backint", "b", "compact", "h"}
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
				&iipb.CloudProperties{},
			},
		},
		{
			name: "FailedToCreateClient",
			s:    Status{},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&iipb.CloudProperties{},
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

func TestStartStatusCollection(t *testing.T) {
	s := Status{
		iamService: &iam.IAM{},
		permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
			return nil, nil
		},
		config:     &cpb.Configuration{},
		CloudProps: &iipb.CloudProperties{},
	}
	if !s.StartStatusCollection(context.Background()) {
		t.Errorf("StartStatusCollection()=false, want true")
	}
}

func TestCollectAndSendStatus(t *testing.T) {
	tests := []struct {
		name string
		s    Status
		want error
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
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0077}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return []fs.FileInfo{
						&mockFileInfo{perm: 0400},
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					Scopes: []string{requiredScope},
					Zone:   "us-central1-a",
					Region: "test-region",
				},
				httpGet:        httpGetSuccess,
				createDBHandle: dbConnectorSuccess,
				iamService:     &iam.IAM{},
				arClient:       fakeArtifactRegistryClient([]string{""}),
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				WLMService: &fake.TestWLM{
					WriteInsightArgs: []fake.WriteInsightArgs{},
					WriteInsightErrs: []error{nil},
				},
			},
			want: nil,
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
    "enabled": true,
		"hana_instances": [
			{
				"name": "instance1",
				"user": "user1",
				"host": "host1",
				"port": "1234",
				"secret_name": "secret1"
			}
		]
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
				CloudProps: &iipb.CloudProperties{
					Scopes: []string{},
					Zone:   "us-central1-a",
					Region: "test-region",
				},
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0400}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return []fs.FileInfo{
						&mockFileInfo{perm: 0400},
					}, nil
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
				arClient:              fakeArtifactRegistryClient([]string{"fake-version/0:1.10-1", "fake-version/0:1.1-2", "fake-version/0:1.2-3"}),
				httpGet:               httpGetSuccess,
				createDBHandle:        dbConnectorSuccess,
				iamService:            &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				WLMService: &fake.TestWLM{
					WriteInsightArgs: []fake.WriteInsightArgs{},
					WriteInsightErrs: []error{nil},
				},
			},
			want: nil,
		},
		{
			name: "StatusStructNotInitialized",
			s:    Status{},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToWriteInsight",
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
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0077}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return []fs.FileInfo{
						&mockFileInfo{perm: 0400},
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					Scopes: []string{requiredScope},
					Zone:   "us-central1-a",
					Region: "test-region",
				},
				httpGet:        httpGetSuccess,
				createDBHandle: dbConnectorSuccess,
				iamService:     &iam.IAM{},
				arClient:       fakeArtifactRegistryClient([]string{""}),
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				WLMService: &fake.TestWLM{
					WriteInsightArgs: []fake.WriteInsightArgs{},
					WriteInsightErrs: []error{cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := test.s.collectAndSendStatus(context.Background())
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("statusHandler()=%v want %v", got, test.want)
			}
		})
	}
}

func TestDiskSnapshotStatus(t *testing.T) {
	tests := []struct {
		name   string
		s      Status
		config *cpb.Configuration
		want   *spb.ServiceStatus
	}{
		{
			name: "PermissionsNotGranted",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"compute.disks.createSnapshot": false,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
					Region:    "test-region",
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Disk Snapshot",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "compute.disks.createSnapshot",
						Granted: spb.State_FAILURE_STATE,
					},
				},
				ErrorMessage: "IAM permissions not granted",
			},
		},
		{
			name: "Success",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"compute.disks.createSnapshot": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
					Region:    "test-region",
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Disk Snapshot",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_SUCCESS_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "compute.disks.createSnapshot",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{},
			},
		},
		{
			name: "ErrorGettingPermissions",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, fmt.Errorf("error getting permissions")
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
					Region:    "test-region",
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Disk Snapshot",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_ERROR_STATE,
				IamPermissions:  nil,
				ErrorMessage:    "Error checking IAM permissions: error getting permissions",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.s.diskSnapshotStatus(context.Background(), tc.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("diskSnapshotStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHostMetricsStatus(t *testing.T) {
	tests := []struct {
		name   string
		s      Status
		config *cpb.Configuration
		want   *spb.ServiceStatus
	}{
		{
			name: "HostMetricsDisabled",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, nil
				},
				config: &cpb.Configuration{
					ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: false},
				},
			},
			want: &spb.ServiceStatus{
				Name:  "Host Metrics",
				State: spb.State_FAILURE_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "provide_sap_host_agent_metrics", Value: "false", IsDefault: false},
				},
			},
		},
		{
			name: "PermissionsNotGranted",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": false,
						"monitoring.timeSeries.list":   true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId:  "test-project",
					InstanceId: "test-instance",
					Zone:       "test-zone",
					Region:     "test-region",
					Scopes:     []string{requiredScope},
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Host Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_FAILURE_STATE,
					},
					{
						Name:    "monitoring.timeSeries.list",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
				},
				ErrorMessage: "IAM permissions not granted",
			},
		},
		{
			name: "EndpointFailure",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId:  "test-project",
					InstanceId: "test-instance",
					Zone:       "test-zone",
					Region:     "test-region",
					Scopes:     []string{requiredScope},
				},
				httpGet: httpGetFailure,
			},
			want: &spb.ServiceStatus{
				Name:            "Host Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_ERROR_STATE,
				ErrorMessage:    "Error verifying endpoint: endpoint failure",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
				},
			},
		},
		{
			name: "ErrorResponseFromEndpoint",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId:  "test-project",
					InstanceId: "test-instance",
					Zone:       "test-zone",
					Region:     "test-region",
					Scopes:     []string{requiredScope},
				},
				httpGet: httpGetError,
			},
			want: &spb.ServiceStatus{
				Name:            "Host Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "Endpoint verification failed with code: 500",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
				},
			},
		},
		{
			name: "Success",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId:  "test-project",
					InstanceId: "test-instance",
					Zone:       "test-zone",
					Region:     "test-region",
					Scopes:     []string{requiredScope},
				},
				httpGet: httpGetSuccess,
			},
			want: &spb.ServiceStatus{
				Name:            "Host Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_SUCCESS_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.s.hostMetricsStatus(context.Background(), tc.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("hostMetricsStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProcessMetricsStatus(t *testing.T) {
	tests := []struct {
		name    string
		s       Status
		config  *cpb.Configuration
		want    *spb.ServiceStatus
		wantErr error
	}{
		{
			name: "ProcessMetricsDisabled",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, nil
				},
				config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						CollectProcessMetrics: false,
					},
				},
			},
			want: &spb.ServiceStatus{
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
		},
		{
			name: "PermissionsNotGranted",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       true,
					ProcessMetricsFrequency:     10,
					ProcessMetricsToSkip:        []string{"test1", "test2"},
					SlowProcessMetricsFrequency: 20,
				},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": false,
						"monitoring.timeSeries.list":   true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Process Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_FAILURE_STATE,
					},
					{
						Name:    "monitoring.timeSeries.list",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_process_metrics", Value: "true", IsDefault: false},
					{Name: "process_metrics_frequency", Value: "10", IsDefault: false},
					{Name: "process_metrics_to_skip", Value: "[test1 test2]", IsDefault: false},
					{Name: "slow_process_metrics_frequency", Value: "20", IsDefault: false},
				},
				ErrorMessage: "IAM permissions not granted",
			},
		},
		{
			name: "ProcessMetricsEnabled",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       true,
					ProcessMetricsFrequency:     10,
					ProcessMetricsToSkip:        []string{"test1", "test2"},
					SlowProcessMetricsFrequency: 20,
				},
			},
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Process Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_SUCCESS_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_process_metrics", Value: "true", IsDefault: false},
					{Name: "process_metrics_frequency", Value: "10", IsDefault: false},
					{Name: "process_metrics_to_skip", Value: "[test1 test2]", IsDefault: false},
					{Name: "slow_process_metrics_frequency", Value: "20", IsDefault: false},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.s.processMetricsStatus(context.Background(), tc.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("processMetricsStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHanaMonitoringMetricsStatus(t *testing.T) {
	tests := []struct {
		name    string
		s       Status
		want    *spb.ServiceStatus
		wantErr error
	}{
		{
			name: "HanaMonitoringDisabled",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, nil
				},
				config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled:               false,
						ConnectionTimeout:     &dpb.Duration{Seconds: 120},
						ExecutionThreads:      10,
						MaxConnectRetries:     wpb.Int32(1),
						QueryTimeoutSec:       300,
						SampleIntervalSec:     300,
						SendQueryResponseTime: false,
					},
				},
				createDBHandle: dbConnectorSuccess,
			},
			want: &spb.ServiceStatus{
				Name:  "HANA Monitoring Metrics",
				State: spb.State_FAILURE_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "connection_timeout", Value: "120", IsDefault: true},
					{Name: "enabled", Value: "false", IsDefault: true},
					{Name: "execution_threads", Value: "10", IsDefault: true},
					{Name: "max_connect_retries", Value: "1", IsDefault: true},
					{Name: "query_timeout_sec", Value: "300", IsDefault: true},
					{Name: "sample_interval_sec", Value: "300", IsDefault: true},
					{Name: "send_query_response_time", Value: "false", IsDefault: true},
				},
			},
		},
		{
			name: "PermissionsNotGranted",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.list":   false,
						"monitoring.timeSeries.create": false,
					}, nil
				},
				config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled:               true,
						ConnectionTimeout:     &dpb.Duration{Seconds: 120},
						ExecutionThreads:      10,
						MaxConnectRetries:     wpb.Int32(1),
						QueryTimeoutSec:       300,
						SampleIntervalSec:     300,
						SendQueryResponseTime: false,
						HanaInstances: []*cpb.HANAInstance{
							{
								Name: "instance1",
								User: "user1",
								Host: "host1",
								Port: "1234",
							},
							{
								Name: "instance2",
								User: "user2",
								Host: "host2",
								Port: "5678",
							},
						},
					},
				},
				gceService: &fake.TestGCE{
					GetSecretResp: []string{"password1", "password2"},
					GetSecretErr:  []error{nil, nil},
				},
				createDBHandle: dbConnectorSuccess,
			},
			want: &spb.ServiceStatus{
				Name:            "HANA Monitoring Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "connection_timeout", Value: "120", IsDefault: true},
					{Name: "enabled", Value: "true", IsDefault: false},
					{Name: "execution_threads", Value: "10", IsDefault: true},
					{Name: "max_connect_retries", Value: "1", IsDefault: true},
					{Name: "query_timeout_sec", Value: "300", IsDefault: true},
					{Name: "sample_interval_sec", Value: "300", IsDefault: true},
					{Name: "send_query_response_time", Value: "false", IsDefault: true},
				},
				ErrorMessage: "IAM permissions not granted",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_FAILURE_STATE,
					},
					{
						Name:    "monitoring.timeSeries.list",
						Granted: spb.State_FAILURE_STATE,
					},
				},
			},
		},
		{
			name: "HanaMonitoringEnabled",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled:               true,
						ConnectionTimeout:     &dpb.Duration{Seconds: 120},
						ExecutionThreads:      10,
						MaxConnectRetries:     wpb.Int32(1),
						QueryTimeoutSec:       300,
						SampleIntervalSec:     300,
						SendQueryResponseTime: false,
						HanaInstances: []*cpb.HANAInstance{
							{
								Name: "instance1",
								User: "user1",
								Host: "host1",
								Port: "1234",
							},
							{
								Name: "instance2",
								User: "user2",
								Host: "host2",
								Port: "5678",
							},
						},
					},
				},
				gceService: &fake.TestGCE{
					GetSecretResp: []string{"password1", "password2"},
					GetSecretErr:  []error{nil, nil},
				},
				createDBHandle: dbConnectorSuccess,
			},
			want: &spb.ServiceStatus{
				Name:            "HANA Monitoring Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_SUCCESS_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "connection_timeout", Value: "120", IsDefault: true},
					{Name: "enabled", Value: "true", IsDefault: false},
					{Name: "execution_threads", Value: "10", IsDefault: true},
					{Name: "max_connect_retries", Value: "1", IsDefault: true},
					{Name: "query_timeout_sec", Value: "300", IsDefault: true},
					{Name: "sample_interval_sec", Value: "300", IsDefault: true},
					{Name: "send_query_response_time", Value: "false", IsDefault: true},
				},
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
			},
		},
		{
			name: "HanaMonitoringConnectionFailure",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled:               true,
						ConnectionTimeout:     &dpb.Duration{Seconds: 120},
						ExecutionThreads:      10,
						MaxConnectRetries:     wpb.Int32(1),
						QueryTimeoutSec:       300,
						SampleIntervalSec:     300,
						SendQueryResponseTime: false,
						HanaInstances: []*cpb.HANAInstance{
							{
								Name: "instance1",
								User: "user1",
								Host: "host1",
								Port: "1234",
							},
						},
					},
				},
				gceService: &fake.TestGCE{
					GetSecretResp: []string{"password1"},
					GetSecretErr:  []error{nil},
				},
				createDBHandle: dbConnectorFailure,
			},
			want: &spb.ServiceStatus{
				Name:            "HANA Monitoring Metrics",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "Failed to connect to HANA instances: instance1",
				ConfigValues: []*spb.ConfigValue{
					{Name: "connection_timeout", Value: "120", IsDefault: true},
					{Name: "enabled", Value: "true", IsDefault: false},
					{Name: "execution_threads", Value: "10", IsDefault: true},
					{Name: "max_connect_retries", Value: "1", IsDefault: true},
					{Name: "query_timeout_sec", Value: "300", IsDefault: true},
					{Name: "sample_interval_sec", Value: "300", IsDefault: true},
					{Name: "send_query_response_time", Value: "false", IsDefault: true},
				},
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.s.hanaMonitoringMetricsStatus(context.Background(), tc.s.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("hanaMonitoringMetricsStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWorkloadManagerStatus(t *testing.T) {
	tests := []struct {
		name   string
		s      Status
		config *cpb.Configuration
		want   *spb.ServiceStatus
	}{
		{
			name: "WorkloadManagerDisabled",
			s:    Status{},
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: false},
				},
			},
			want: &spb.ServiceStatus{
				Name:  "Workload Manager Evaluation",
				State: spb.State_FAILURE_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_workload_validation_metrics", Value: "false", IsDefault: false},
					{Name: "config_target_environment", Value: "TARGET_ENVIRONMENT_UNSPECIFIED", IsDefault: false},
					{Name: "fetch_latest_config", Value: "false", IsDefault: false},
					{Name: "workload_validation_db_metrics_frequency", Value: "0", IsDefault: false},
					{Name: "workload_validation_metrics_frequency", Value: "0", IsDefault: false},
				},
			},
		},
		{
			name: "PermissionsNotGranted",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": false,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
				},
			},
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						ConfigTargetEnvironment: cpb.TargetEnvironment_TARGET_ENVIRONMENT_UNSPECIFIED,
						FetchLatestConfig:       &wpb.BoolValue{Value: true},
					},
					WorkloadValidationDbMetricsFrequency: 3600,
					WorkloadValidationMetricsFrequency:   300,
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Workload Manager Evaluation",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_FAILURE_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
					{Name: "config_target_environment", Value: "TARGET_ENVIRONMENT_UNSPECIFIED", IsDefault: false},
					{Name: "fetch_latest_config", Value: "true", IsDefault: true},
					{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
					{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
				},
				ErrorMessage: "IAM permissions not granted",
			},
		},
		{
			name: "Success",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
				},
			},
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						ConfigTargetEnvironment: cpb.TargetEnvironment_PRODUCTION,
						FetchLatestConfig:       &wpb.BoolValue{Value: true},
					},
					WorkloadValidationDbMetricsFrequency: 3600,
					WorkloadValidationMetricsFrequency:   300,
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Workload Manager Evaluation",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_SUCCESS_STATE,
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
					{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
					{Name: "fetch_latest_config", Value: "true", IsDefault: true},
					{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
					{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
				},
			},
		},
		{
			name: "ErrorGettingPermissions",
			s: Status{
				iamService: &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, fmt.Errorf("error getting permissions")
				},
				CloudProps: &iipb.CloudProperties{
					ProjectId: "test-project",
					Scopes:    []string{requiredScope},
				},
			},
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						ConfigTargetEnvironment: cpb.TargetEnvironment_PRODUCTION,
						FetchLatestConfig:       &wpb.BoolValue{Value: true},
					},
					WorkloadValidationDbMetricsFrequency: 3600,
					WorkloadValidationMetricsFrequency:   300,
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Workload Manager Evaluation",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_ERROR_STATE,
				IamPermissions:  nil,
				ErrorMessage:    "Error checking IAM permissions: error getting permissions",
				ConfigValues: []*spb.ConfigValue{
					{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
					{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
					{Name: "fetch_latest_config", Value: "true", IsDefault: true},
					{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
					{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.s.workloadManagerStatus(context.Background(), tc.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("workloadManagerStatus() returned unexpected diff (-want +got):\n%s", diff)
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
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0077}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return []fs.FileInfo{
						&mockFileInfo{perm: 0400},
					}, nil
				},
				CloudProps: &iipb.CloudProperties{
					Scopes: []string{requiredScope},
				},
				httpGet:        httpGetSuccess,
				createDBHandle: dbConnectorSuccess,
				iamService:     &iam.IAM{},
				arClient:       fakeArtifactRegistryClient([]string{""}),
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
			},
			want: &spb.AgentStatus{
				AgentName:                       agentPackageName,
				InstalledVersion:                fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
				AvailableVersion:                "",
				SystemdServiceEnabled:           spb.State_SUCCESS_STATE,
				SystemdServiceRunning:           spb.State_SUCCESS_STATE,
				ConfigurationFilePath:           configuration.LinuxConfigPath,
				ConfigurationValid:              spb.State_SUCCESS_STATE,
				CloudApiAccessFullScopesGranted: spb.State_SUCCESS_STATE,
				KernelVersion:                   &spb.KernelVersion{RawString: "enabled"},
				InstanceUri:                     "projects//zones//instances/",
				Services: []*spb.ServiceStatus{
					{
						Name:            "Host Metrics",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
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
						Name:            "System Discovery",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_FAILURE_STATE,
						ErrorMessage:    "/usr/sap/sapservices has incorrect permissions. Got: 077, want: 0400",
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "enable_discovery", Value: "true", IsDefault: true},
							{Name: "enable_workload_discovery", Value: "true", IsDefault: true},
							{Name: "sap_instances_update_frequency", Value: "60", IsDefault: true},
							{Name: "system_discovery_update_frequency", Value: "14400", IsDefault: true},
						},
					},
					{
						Name:                    "Backint",
						State:                   spb.State_UNSPECIFIED_STATE,
						UnspecifiedStateMessage: "Backint parameters file not specified / Disabled",
						ConfigValues:            []*spb.ConfigValue{},
					},
					{
						Name:            "Disk Snapshot",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{},
					},
					{
						Name:  "Workload Manager Evaluation",
						State: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
							{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
							{Name: "fetch_latest_config", Value: "true", IsDefault: true},
							{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
							{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
						},
						FullyFunctional: spb.State_SUCCESS_STATE,
					},
				},
				References: []*spb.Reference{
					{
						Name: "Release notes",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new",
					},
					{
						Name: "Guides",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/all-guides",
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
    "enabled": true,
		"hana_instances": [
			{
				"name": "instance1",
				"user": "user1",
				"host": "host1",
				"port": "1234",
				"secret_name": "secret1"
			}
		]
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
				CloudProps: &iipb.CloudProperties{
					Scopes:     []string{},
					InstanceId: "instance-id",
					ProjectId:  "project-id",
					Zone:       "zone-id",
				},
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0700}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return []fs.FileInfo{
						&mockFileInfo{perm: 0400},
					}, nil
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
				arClient:              fakeArtifactRegistryClient([]string{"fake-version/0:1.10-1", "fake-version/0:1.1-2", "fake-version/0:1.2-3"}),
				httpGet:               httpGetSuccess,
				createDBHandle:        dbConnectorSuccess,
				iamService:            &iam.IAM{},
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
			},
			want: &spb.AgentStatus{
				AgentName:                       agentPackageName,
				InstalledVersion:                fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
				AvailableVersion:                "1.10-1",
				SystemdServiceEnabled:           spb.State_ERROR_STATE,
				SystemdServiceRunning:           spb.State_ERROR_STATE,
				ConfigurationFilePath:           configuration.LinuxConfigPath,
				ConfigurationValid:              spb.State_SUCCESS_STATE,
				CloudApiAccessFullScopesGranted: spb.State_FAILURE_STATE,
				InstanceUri:                     "projects/project-id/zones/zone-id/instances/instance-id",
				Services: []*spb.ServiceStatus{
					{
						Name:            "Host Metrics",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "provide_sap_host_agent_metrics", Value: "true", IsDefault: true},
						},
					},
					{
						Name:            "Process Metrics",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_process_metrics", Value: "true", IsDefault: false},
							{Name: "process_metrics_frequency", Value: "5", IsDefault: true},
							{Name: "process_metrics_to_skip", Value: "[]", IsDefault: true},
							{Name: "slow_process_metrics_frequency", Value: "30", IsDefault: true},
						},
					},
					{
						Name:            "HANA Monitoring Metrics",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
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
						Name:            "System Discovery",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
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
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
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
						Name:            "Disk Snapshot",
						State:           spb.State_SUCCESS_STATE,
						FullyFunctional: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{},
					},
					{
						Name:  "Workload Manager Evaluation",
						State: spb.State_SUCCESS_STATE,
						IamPermissions: []*spb.IAMPermission{
							{Name: "monitoring.timeSeries.create", Granted: spb.State_SUCCESS_STATE},
						},
						ConfigValues: []*spb.ConfigValue{
							{Name: "collect_workload_validation_metrics", Value: "true", IsDefault: true},
							{Name: "config_target_environment", Value: "PRODUCTION", IsDefault: true},
							{Name: "fetch_latest_config", Value: "true", IsDefault: true},
							{Name: "workload_validation_db_metrics_frequency", Value: "3600", IsDefault: true},
							{Name: "workload_validation_metrics_frequency", Value: "300", IsDefault: true},
						},
						FullyFunctional: spb.State_SUCCESS_STATE,
					},
				},
				References: []*spb.Reference{
					{
						Name: "Release notes",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/whats-new",
					},
					{
						Name: "Guides",
						Url:  "https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/all-guides",
					},
				},
			},
		},
		{
			name:    "StatusStructNotInitialized",
			s:       Status{},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
				Name:                    "Backint",
				State:                   spb.State_UNSPECIFIED_STATE,
				UnspecifiedStateMessage: "Backint parameters file not specified / Disabled",
				ConfigValues:            []*spb.ConfigValue{},
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
				Name:         "Backint",
				State:        spb.State_ERROR_STATE,
				ErrorMessage: `bucket must be provided`,
				ConfigValues: []*spb.ConfigValue{},
			},
		},
		{
			name: "IAMPermissionsNotGranted",
			s: Status{
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{
  "bucket": "fake-bucket",
  "log_to_cloud": true,
	"threads": 1
}
`), nil
				},
				BackintParametersPath: "fake-path/backint-gcs/parameters.json",
				backintClient:         defaultStorageClient,
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"storage.objects.create":          false,
						"storage.objects.delete":          false,
						"storage.multipartUploads.create": false,
					}, nil
				},
			},
			want: &spb.ServiceStatus{
				Name:            "Backint",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "IAM permissions not granted",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "storage.multipartUploads.create",
						Granted: spb.State_FAILURE_STATE,
					},
					{
						Name:    "storage.objects.create",
						Granted: spb.State_FAILURE_STATE,
					},
					{
						Name:    "storage.objects.delete",
						Granted: spb.State_FAILURE_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "bucket", Value: "fake-bucket", IsDefault: false},
					{Name: "log_to_cloud", Value: "true", IsDefault: true},
					{Name: "param_file", Value: "fake-path/backint-gcs/parameters.json", IsDefault: false},
					{Name: "threads", Value: "1", IsDefault: false},
				},
			},
		},
		{
			name: "PermissionsStatusFailure",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, fmt.Errorf("error getting permissions")
				},
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{
  "bucket": "fake-bucket",
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
				FullyFunctional: spb.State_ERROR_STATE,
				ErrorMessage:    "Error checking IAM permissions: error getting permissions",
				IamPermissions:  nil,
				ConfigValues: []*spb.ConfigValue{
					{Name: "bucket", Value: "fake-bucket", IsDefault: false},
					{Name: "log_to_cloud", Value: "true", IsDefault: true},
					{Name: "param_file", Value: "fake-path/backint-gcs/parameters.json", IsDefault: false},
					{Name: "threads", Value: "1", IsDefault: false},
				},
			},
		},
		{
			name: "MultipartIAMPermissionsNotGranted",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					if serviceName == backintMultipartLabel {
						return map[string]bool{
							"storage.multipartUploads.create": false,
						}, nil
					}
					return map[string]bool{
						"storage.objects.create": true,
						"storage.objects.delete": true,
					}, nil
				},
				backintReadFile: func(string) ([]byte, error) {
					return []byte(`
{
  "bucket": "fake-bucket",
  "log_to_cloud": true,
	"threads": 1,
  "xml_multipart_upload": true
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
				ErrorMessage:    "IAM permissions not granted",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "storage.objects.create",
						Granted: spb.State_SUCCESS_STATE,
					},
					{
						Name:    "storage.objects.delete",
						Granted: spb.State_SUCCESS_STATE,
					},
					{
						Name:    "storage.multipartUploads.create",
						Granted: spb.State_FAILURE_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "bucket", Value: "fake-bucket", IsDefault: false},
					{Name: "log_to_cloud", Value: "true", IsDefault: true},
					{Name: "param_file", Value: "fake-path/backint-gcs/parameters.json", IsDefault: false},
					{Name: "parallel_streams", Value: "16", IsDefault: false},
					{Name: "threads", Value: "1", IsDefault: false},
					{Name: "xml_multipart_upload", Value: "true", IsDefault: false},
				},
			},
		},
		{
			name: "FailedToConnectToBucket",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"storage.objects.create":          true,
						"storage.objects.delete":          true,
						"storage.multipartUploads.create": true,
					}, nil
				},
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
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "storage.multipartUploads.create",
						Granted: spb.State_SUCCESS_STATE,
					},
					{
						Name:    "storage.objects.create",
						Granted: spb.State_SUCCESS_STATE,
					},
					{
						Name:    "storage.objects.delete",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
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
			t.Parallel()
			got := test.s.backintStatus(context.Background())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("backintStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSystemDiscoveryStatusFailures(t *testing.T) {
	tests := []struct {
		name   string
		s      Status
		config *cpb.Configuration
		want   *spb.ServiceStatus
	}{
		{
			name: "FailedToStatSapServices",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				stat: func(name string) (os.FileInfo, error) {
					return nil, fmt.Errorf("failed to stat sapservices")
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return nil, nil
				},
			},
			config: &cpb.Configuration{
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: wpb.Bool(true),
				},
			},
			want: &spb.ServiceStatus{
				Name:            "System Discovery",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "failed to stat sapservices",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "enable_discovery", Value: "true", IsDefault: true},
					{Name: "enable_workload_discovery", Value: "false", IsDefault: false},
					{Name: "sap_instances_update_frequency", Value: "0", IsDefault: false},
					{Name: "system_discovery_update_frequency", Value: "0", IsDefault: false},
				},
			},
		},
		{
			name: "IAMPermissionsNotGranted",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": false,
					}, nil
				},
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0400}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return nil, nil
				},
			},
			config: &cpb.Configuration{
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: wpb.Bool(true),
				},
			},
			want: &spb.ServiceStatus{
				Name:            "System Discovery",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "IAM permissions not granted",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_FAILURE_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "enable_discovery", Value: "true", IsDefault: true},
					{Name: "enable_workload_discovery", Value: "false", IsDefault: false},
					{Name: "sap_instances_update_frequency", Value: "0", IsDefault: false},
					{Name: "system_discovery_update_frequency", Value: "0", IsDefault: false},
				},
			},
		},
		{
			name: "IAMPermissionsError",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return nil, fmt.Errorf("error getting permissions")
				},
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0400}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return nil, nil
				},
			},
			config: &cpb.Configuration{
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: wpb.Bool(true),
				},
			},
			want: &spb.ServiceStatus{
				Name:            "System Discovery",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_ERROR_STATE,
				ErrorMessage:    "Error checking IAM permissions: error getting permissions",
				IamPermissions:  nil,
				ConfigValues: []*spb.ConfigValue{
					{Name: "enable_discovery", Value: "true", IsDefault: true},
					{Name: "enable_workload_discovery", Value: "false", IsDefault: false},
					{Name: "sap_instances_update_frequency", Value: "0", IsDefault: false},
					{Name: "system_discovery_update_frequency", Value: "0", IsDefault: false},
				},
			},
		},
		{
			name: "FailedToReadDir",
			s: Status{
				permissionsStatus: func(ctx context.Context, iamService permissions.IAMService, serviceName string, r *permissions.ResourceDetails) (map[string]bool, error) {
					return map[string]bool{
						"monitoring.timeSeries.create": true,
					}, nil
				},
				stat: func(name string) (os.FileInfo, error) {
					return &mockFileInfo{perm: 0400}, nil
				},
				readDir: func(dirname string) ([]fs.FileInfo, error) {
					return nil, fmt.Errorf("failed to read dir")
				},
			},
			config: &cpb.Configuration{
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: wpb.Bool(true),
				},
			},
			want: &spb.ServiceStatus{
				Name:            "System Discovery",
				State:           spb.State_SUCCESS_STATE,
				FullyFunctional: spb.State_FAILURE_STATE,
				ErrorMessage:    "failed to read dir",
				IamPermissions: []*spb.IAMPermission{
					{
						Name:    "monitoring.timeSeries.create",
						Granted: spb.State_SUCCESS_STATE,
					},
				},
				ConfigValues: []*spb.ConfigValue{
					{Name: "enable_discovery", Value: "true", IsDefault: true},
					{Name: "enable_workload_discovery", Value: "false", IsDefault: false},
					{Name: "sap_instances_update_frequency", Value: "0", IsDefault: false},
					{Name: "system_discovery_update_frequency", Value: "0", IsDefault: false},
				},
			},
		},
		{
			name: "SystemDiscoveryDisabled",
			s:    Status{},
			config: &cpb.Configuration{
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery:                wpb.Bool(false),
					EnableWorkloadDiscovery:        wpb.Bool(false),
					SapInstancesUpdateFrequency:    dpb.New(time.Second * 0),
					SystemDiscoveryUpdateFrequency: dpb.New(time.Second * 0),
				},
			},
			want: &spb.ServiceStatus{
				Name:  "System Discovery",
				State: spb.State_FAILURE_STATE,
				ConfigValues: []*spb.ConfigValue{
					{Name: "enable_discovery", Value: "false", IsDefault: false},
					{Name: "enable_workload_discovery", Value: "false", IsDefault: false},
					{Name: "sap_instances_update_frequency", Value: "0", IsDefault: false},
					{Name: "system_discovery_update_frequency", Value: "0", IsDefault: false},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := test.s.systemDiscoveryStatus(context.Background(), test.config)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("systemDiscoveryStatus() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
