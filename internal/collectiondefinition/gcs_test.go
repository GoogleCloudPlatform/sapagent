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
	"errors"
	"os"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	invalidJSON = fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{
			BucketName: "sapagent-collection-definition-dev",
			Name:       "sapagent/collection-definition/collection_definition.json",
		},
		Content: []byte("invalid json"),
	}
	validJSON = fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{
			BucketName: "sapagent-collection-definition-dev",
			Name:       "sapagent/collection-definition/collection_definition.json",
		},
		Content: []byte(`{"workload_validation":{}}`),
	}
	validSignature = fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{
			BucketName: "sapagent-collection-definition-dev",
			Name:       "sapagent/collection-definition/collection_definition.signature",
		},
		Content: []byte("test signature"),
	}
	fakeStorageClient = func(objects []fakestorage.Object) func(context.Context, ...option.ClientOption) (*storage.Client, error) {
		fakeServer := fakestorage.NewServer(objects)
		return func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
			return fakeServer.Client(), nil
		}
	}
	defaultExec = func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "Verified OK",
		}
	}
)

func TestFetchFromGCS(t *testing.T) {
	tests := []struct {
		name string
		opts FetchOptions
		want *cdpb.CollectionDefinition
	}{
		{
			name: "ConnectToBucket_CreateClientFailure",
			opts: FetchOptions{
				OSType: "linux",
				Env:    cpb.TargetEnvironment_DEVELOPMENT,
				Client: func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
					return nil, errors.New("client create error")
				},
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: nil,
		},
		{
			name: "CreateTemp_JSON_Failure",
			opts: FetchOptions{
				OSType: "linux",
				Env:    cpb.TargetEnvironment_DEVELOPMENT,
				Client: fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: func(dir, pattern string) (*os.File, error) {
					if pattern == "collection-definition.*.json" {
						return nil, errors.New("temp file create error")
					}
					return os.CreateTemp(dir, pattern)
				},
				Execute: defaultExec,
			},
			want: nil,
		},
		{
			name: "Download_JSON_ObjectDoesNotExist",
			opts: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: nil,
		},
		{
			name: "CreateTemp_Signature_Failure",
			opts: FetchOptions{
				OSType: "linux",
				Env:    cpb.TargetEnvironment_DEVELOPMENT,
				Client: fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: func(dir, pattern string) (*os.File, error) {
					if pattern == "collection-definition.*.signature" {
						return nil, errors.New("temp file create error")
					}
					return os.CreateTemp(dir, pattern)
				},
				Execute: defaultExec,
			},
			want: nil,
		},
		{
			name: "Download_Signature_ObjectDoesNotExist",
			opts: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validJSON}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: nil,
		},
		{
			name: "CreateTemp_PublicKey_Failure",
			opts: FetchOptions{
				OSType: "linux",
				Env:    cpb.TargetEnvironment_DEVELOPMENT,
				Client: fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: func(dir, pattern string) (*os.File, error) {
					if pattern == "public.*.pem" {
						return nil, errors.New("temp file create error")
					}
					return os.CreateTemp(dir, pattern)
				},
				Execute: defaultExec,
			},
			want: nil,
		},
		{
			name: "OSType_windows",
			opts: FetchOptions{
				OSType:     "windows",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: nil,
		},
		{
			name: "Execute_Failure",
			opts: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{StdOut: "Verification failure"}
				},
			},
			want: nil,
		},
		{
			name: "Unmarshal_Failure",
			opts: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{invalidJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: nil,
		},
		{
			name: "Success",
			opts: FetchOptions{
				OSType:     "linux",
				Env:        cpb.TargetEnvironment_DEVELOPMENT,
				Client:     fakeStorageClient([]fakestorage.Object{validJSON, validSignature}),
				CreateTemp: os.CreateTemp,
				Execute:    defaultExec,
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := fetchFromGCS(context.Background(), test.opts)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("fetchFromGCS() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
