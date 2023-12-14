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

package collectiondefinition

import (
	"context"
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	//go:embed test_data/test_collectiondefinition1.json
	testCollectionDefinition1 []byte

	//go:embed test_data/test_collectiondefinition2.json
	testCollectionDefinition2 []byte

	//go:embed test_data/invalid_collectiondefinition.json
	invalidCollectionDefinition []byte

	//go:embed test_data/collectiondefinition_with_unknown_fields.json
	collectionDefinitionWithUnknownFields []byte

	createEvalMetric = func(metricType, label, contains string) *cmpb.EvalMetric {
		return &cmpb.EvalMetric{
			MetricInfo: &cmpb.MetricInfo{
				Type:  metricType,
				Label: label,
			},
			EvalRuleTypes: &cmpb.EvalMetric_OrEvalRules{
				OrEvalRules: &cmpb.OrEvalMetricRule{
					OrEvalRules: []*cmpb.EvalMetricRule{
						&cmpb.EvalMetricRule{
							EvalRules: []*cmpb.EvalRule{
								&cmpb.EvalRule{
									EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: contains},
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
			},
		}
	}
	createOSCommandMetric = func(metricType, label, command string, vendor cmpb.OSVendor) *cmpb.OSCommandMetric {
		return createOSCommandMetricWithVersion(metricType, label, command, vendor, "")
	}

	createOSCommandMetricWithVersion = func(metricType, label, command string, vendor cmpb.OSVendor, version string) *cmpb.OSCommandMetric {
		metricInfo := &cmpb.MetricInfo{Type: metricType, Label: label, MinVersion: version}
		return &cmpb.OSCommandMetric{
			MetricInfo: metricInfo,
			OsVendor:   vendor,
			Command:    command,
			Args:       []string{"-v"},
			EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							OutputSource:  cmpb.OutputSource_STDOUT,
							EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "Contains Text"},
						},
					},
					IfTrue: &cmpb.EvalResult{
						OutputSource:    cmpb.OutputSource_STDOUT,
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
					},
					IfFalse: &cmpb.EvalResult{
						OutputSource:    cmpb.OutputSource_STDOUT,
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
					},
				},
			},
		}
	}

	disableFetchConfig = &cpb.CollectionConfiguration{
		WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
			FetchLatestConfig: wpb.Bool(false),
		},
	}
)

func TestStart(t *testing.T) {
	wantCollectionDefinition, err := unmarshal(configuration.DefaultCollectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name string
		opts StartOptions
		want *cdpb.CollectionDefinition
	}{
		{
			name: "NoServicesEnabledReturnsNil",
			opts: StartOptions{
				LoadOptions: LoadOptions{
					CollectionConfig: &cpb.CollectionConfiguration{},
				},
			},
			want: nil,
		},
		{
			name: "InitialLoadReturnsNil",
			opts: StartOptions{
				LoadOptions: LoadOptions{
					CollectionConfig: &cpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
						WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
							FetchLatestConfig: wpb.Bool(false),
						},
					},
					ReadFile: func(s string) ([]byte, error) { return invalidCollectionDefinition, nil },
					OSType:   "linux",
					Version:  configuration.AgentVersion,
				},
			},
			want: nil,
		},
		{
			name: "InitialLoadReturnsCollectionDefinition",
			opts: StartOptions{
				LoadOptions: LoadOptions{
					CollectionConfig: &cpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
						WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
							FetchLatestConfig: wpb.Bool(false),
						},
					},
					ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
					OSType:   "linux",
					Version:  configuration.AgentVersion,
				},
			},
			want: wantCollectionDefinition,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Start(context.Background(), []chan<- *cdpb.CollectionDefinition{}, test.opts)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("Start() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestStart_HeartbeatSpec(t *testing.T) {
	tests := []struct {
		name         string
		beatInterval time.Duration
		timeout      time.Duration
		want         int
	}{
		{
			name:         "CancelBeforeHeartbeat",
			beatInterval: time.Second,
			timeout:      time.Second * 0,
			want:         0,
		},
		{
			name:         "CancelAfterOneBeat",
			beatInterval: time.Millisecond * 150,
			timeout:      time.Millisecond * 250,
			want:         1,
		},
		{
			name:         "CancelAfterTwoBeats",
			beatInterval: time.Millisecond * 150,
			timeout:      time.Millisecond * 400,
			want:         2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := 0
			lock := sync.Mutex{}
			ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
			defer cancel()
			opts := StartOptions{
				HeartbeatSpec: &heartbeat.Spec{
					BeatFunc: func() {
						lock.Lock()
						defer lock.Unlock()
						got++
					},
					Interval: test.beatInterval,
				},
				LoadOptions: LoadOptions{
					CollectionConfig: &cpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
						WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
							FetchLatestConfig:       wpb.Bool(true),
							ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
						},
					},
					FetchOptions: FetchOptions{
						OSType: "linux",
						Env:    cpb.TargetEnvironment_DEVELOPMENT,
						Client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
							return nil, errors.New("client create error")
						},
						CreateTemp: os.CreateTemp,
						Execute:    defaultExec,
					},
					ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
					OSType:   "linux",
					Version:  configuration.AgentVersion,
				},
			}

			Start(ctx, []chan<- *cdpb.CollectionDefinition{}, opts)
			<-ctx.Done()
			lock.Lock()
			defer lock.Unlock()
			if got != test.want {
				t.Errorf("Start() heartbeat mismatch got %d, want %d", got, test.want)
			}
		})
	}
}

func TestLoadAndBroadcast_Success(t *testing.T) {
	want := &cdpb.CollectionDefinition{WorkloadValidation: &wlmpb.WorkloadValidation{}}
	ch1, ch2 := make(chan *cdpb.CollectionDefinition), make(chan *cdpb.CollectionDefinition)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	opts := StartOptions{
		HeartbeatSpec: &heartbeat.Spec{Interval: time.Second, BeatFunc: func() {}},
		LoadOptions: LoadOptions{
			CollectionConfig: &cpb.CollectionConfiguration{
				WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
					FetchLatestConfig:       wpb.Bool(true),
					ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
				},
			},
			FetchOptions: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
			OSType:   "linux",
			Version:  "1.4",
		},
	}

	go loadAndBroadcast(ctx, []chan<- *cdpb.CollectionDefinition{ch1, ch2}, opts)

	var got1, got2 *cdpb.CollectionDefinition
	for {
		select {
		case got1 = <-ch1:
			t.Log("Received response from channel 1")
		case got2 = <-ch2:
			t.Log("Received response from channel 2")
		case <-ctx.Done():
			if d := cmp.Diff(want, got1, protocmp.Transform()); d != "" {
				t.Errorf("loadAndBroadcast() mismatch (-want, +got):\n%s", d)
			}
			if d := cmp.Diff(want, got2, protocmp.Transform()); d != "" {
				t.Errorf("loadAndBroadcast() mismatch (-want, +got):\n%s", d)
			}
			return
		}
	}
}

func TestLoadAndBroadcast_Failure(t *testing.T) {
	ch1, ch2 := make(chan *cdpb.CollectionDefinition), make(chan *cdpb.CollectionDefinition)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	opts := StartOptions{
		HeartbeatSpec: &heartbeat.Spec{Interval: time.Second, BeatFunc: func() {}},
		LoadOptions: LoadOptions{
			CollectionConfig: &cpb.CollectionConfiguration{
				WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
					FetchLatestConfig:       wpb.Bool(true),
					ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
				},
			},
			FetchOptions: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{invalidJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			ReadFile: func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
			OSType:   "linux",
			Version:  "1.4",
		},
	}

	go loadAndBroadcast(ctx, []chan<- *cdpb.CollectionDefinition{ch1, ch2}, opts)

	var got1, got2 *cdpb.CollectionDefinition
	select {
	case got1 = <-ch1:
		t.Errorf("loadAndBroadcast() channel 1 should not have received a response. got: %v", got1)
	case got2 = <-ch2:
		t.Errorf("loadAndBroadcast() channel 2 should not have received a response. got: %v", got2)
	case <-ctx.Done():
		return
	}
}

func TestFromJSONFile(t *testing.T) {
	wantCollectionDefinition1, err := unmarshal(testCollectionDefinition1)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name    string
		reader  func(string) ([]byte, error)
		path    string
		want    *cdpb.CollectionDefinition
		wantErr error
	}{
		{
			name: "FileNotFound",
			reader: func(string) ([]byte, error) {
				return nil, fs.ErrNotExist
			},
			path: "not-found",
		},
		{
			name:    "FileReadError",
			reader:  func(string) ([]byte, error) { return nil, errors.New("ReadFile Error") },
			path:    "error",
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "UnmarshalError",
			reader:  func(string) ([]byte, error) { return []byte("Not JSON"), nil },
			path:    "invalid",
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "Success",
			reader: func(string) ([]byte, error) { return testCollectionDefinition1, nil },
			path:   LinuxConfigPath,
			want:   wantCollectionDefinition1,
		},
		{
			name:   "IgnoreUnknownFields",
			reader: func(string) ([]byte, error) { return collectionDefinitionWithUnknownFields, nil },
			path:   LinuxConfigPath,
			want:   wantCollectionDefinition1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := FromJSONFile(context.Background(), test.reader, test.path)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("FromJSONFile() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("FromJSONFile() got %v want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	wantCollectionDefinition, err := unmarshal(testCollectionDefinition2)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name    string
		opts    LoadOptions
		want    *cdpb.CollectionDefinition
		wantErr error
	}{
		{
			name: "FetchErrorNoFallback",
			opts: LoadOptions{
				CollectionConfig: &cpb.CollectionConfiguration{
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						FetchLatestConfig:       wpb.Bool(true),
						ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
					},
				},
				FetchOptions: FetchOptions{
					OSType: "linux",
					Env:    cpb.TargetEnvironment_DEVELOPMENT,
					Client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
						return nil, errors.New("client create error")
					},
					CreateTemp: os.CreateTemp,
					Execute:    defaultExec,
				},
				OSType:          "linux",
				Version:         configuration.AgentVersion,
				DisableFallback: true,
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "LocalReadFileError",
			opts: LoadOptions{
				CollectionConfig: disableFetchConfig,
				ReadFile:         func(s string) ([]byte, error) { return nil, errors.New("ReadFile Error") },
				OSType:           "windows",
				Version:          configuration.AgentVersion,
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ValidationError",
			opts: LoadOptions{
				CollectionConfig: disableFetchConfig,
				ReadFile:         func(s string) ([]byte, error) { return invalidCollectionDefinition, nil },
				OSType:           "linux",
				Version:          configuration.AgentVersion,
			},
			want:    nil,
			wantErr: ValidationError{FailureCount: 1},
		},
		{
			name: "SuccessDisableFetch",
			opts: LoadOptions{
				CollectionConfig: disableFetchConfig,
				ReadFile:         func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
				OSType:           "linux",
				Version:          configuration.AgentVersion,
			},
			want:    wantCollectionDefinition,
			wantErr: nil,
		},
		{
			name: "SuccessWithFetch",
			opts: LoadOptions{
				CollectionConfig: &cpb.CollectionConfiguration{
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						FetchLatestConfig:       wpb.Bool(true),
						ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
					},
				},
				FetchOptions: FetchOptions{
					OSType:     "linux",
					Env:        cpb.TargetEnvironment_DEVELOPMENT,
					Client:     fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
					CreateTemp: os.CreateTemp,
					Execute:    defaultExec,
				},
				ReadFile:        func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
				OSType:          "linux",
				Version:         configuration.AgentVersion,
				DisableFallback: true,
			},
			want:    &cdpb.CollectionDefinition{WorkloadValidation: &wlmpb.WorkloadValidation{}},
			wantErr: nil,
		},
		{
			name: "SuccessWithFallback",
			opts: LoadOptions{
				CollectionConfig: &cpb.CollectionConfiguration{
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						FetchLatestConfig:       wpb.Bool(true),
						ConfigTargetEnvironment: cpb.TargetEnvironment_DEVELOPMENT,
					},
				},
				FetchOptions: FetchOptions{
					OSType: "linux",
					Env:    cpb.TargetEnvironment_DEVELOPMENT,
					Client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
						return nil, errors.New("client create error")
					},
					CreateTemp: os.CreateTemp,
					Execute:    defaultExec,
				},
				ReadFile:        func(s string) ([]byte, error) { return nil, fs.ErrNotExist },
				OSType:          "linux",
				Version:         configuration.AgentVersion,
				DisableFallback: false,
			},
			want:    wantCollectionDefinition,
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := Load(context.Background(), test.opts)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Load() got %v want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	defaultPrimaryDefinition, err := unmarshal(testCollectionDefinition1)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}
	all := cmpb.OSVendor_ALL

	tests := []struct {
		name      string
		primary   *cdpb.CollectionDefinition
		secondary *cdpb.CollectionDefinition
		want      *cdpb.CollectionDefinition
	}{
		{
			name:      "WorkloadValidation_NoSecondaryDefinition",
			primary:   defaultPrimaryDefinition,
			secondary: nil,
			want:      defaultPrimaryDefinition,
		},
		{
			name: "WorkloadValidation_ValidationSystem_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationSystem_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_ConfigMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token2", "token2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token2", "token2"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_ConfigMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_GlobalINIMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana2", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
							createEvalMetric("workload.googleapis.com/sap/validation/hana2", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_GlobalINIMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "dog", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationNetweaver_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationNetweaver_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_HasAll",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_IsAll",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_SLES),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_SLES),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_HasVendor",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_UNSPECIFIED",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Invalid_Version",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "invalid_version"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Primary_Too_New",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "9000.1"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.1"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.1"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Secondary_Too_New",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "9000.1"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Both_Too_New",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "9000.1"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "9000.1"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Valid_Version_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.1"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.6"),
							createOSCommandMetricWithVersion("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, "1.1"),
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Merge(test.primary, test.secondary)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("Merge() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}
