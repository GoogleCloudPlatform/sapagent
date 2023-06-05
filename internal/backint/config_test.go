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

package backint

import (
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

var (
	defaultReadConfigFile = func(p string) ([]byte, error) {
		return nil, nil
	}
	readConfigFileError = func(p string) ([]byte, error) {
		return nil, cmpopts.AnyError
	}
	defaultBackint = &Backint{
		user:      "testUser",
		paramFile: "testParamsFile",
		function:  "backup",
	}
	defaultConfigArgsParsed = &bpb.BackintConfiguration{
		UserId:    "testUser",
		Function:  bpb.Function_BACKUP,
		ParamFile: "testParamsFile",
	}
	defaultThreads = func() int64 {
		numThreads := int64(runtime.NumCPU())
		if numThreads > 64 {
			numThreads = 64
		}
		return numThreads
	}
)

func TestParseArgsAndValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		backint *Backint
		read    ReadConfigFile
		want    *bpb.BackintConfiguration
		wantOk  bool
	}{
		{
			name: "NoUser",
			backint: &Backint{
				user: "",
			},
			wantOk: false,
		},
		{
			name: "NoParamsFile",
			backint: &Backint{
				user:      "testUser",
				paramFile: "",
			},
			wantOk: false,
		},
		{
			name: "NoFunction",
			backint: &Backint{
				user:      "testUser",
				paramFile: "testParamsFile",
				function:  "",
			},
			wantOk: false,
		},
		{
			name: "InvalidFunction",
			backint: &Backint{
				user:      "testUser",
				paramFile: "testParamsFile",
				function:  "testFunction",
			},
			wantOk: false,
		},
		{
			name:    "ParametersReadFileError",
			backint: defaultBackint,
			want:    defaultConfigArgsParsed,
			read:    readConfigFileError,
			wantOk:  false,
		},
		{
			name:    "ParametersFileEmpty",
			backint: defaultBackint,
			want:    defaultConfigArgsParsed,
			read:    defaultReadConfigFile,
			wantOk:  false,
		},
		{
			name:    "ParametersFileMalformed",
			backint: defaultBackint,
			want:    defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`test`), nil
			},
			wantOk: false,
		},
		{
			name:    "NoBucket",
			backint: defaultBackint,
			want:    defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": ""}`), nil
			},
			wantOk: false,
		},
		{
			name:    "EncyptionKeyAndKmsKeyDefined",
			backint: defaultBackint,
			want: &bpb.BackintConfiguration{
				UserId:        "testUser",
				Function:      bpb.Function_BACKUP,
				ParamFile:     "testParamsFile",
				Bucket:        "testBucket",
				EncryptionKey: "testKey",
				KmsKey:        "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "encryption_key": "testKey", "kms_key": "testKey"}`), nil
			},
			wantOk: false,
		},
		{
			name:    "CompressedParallelBackup",
			backint: defaultBackint,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				Compress:        true,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "compress": true}`), nil
			},
			wantOk: false,
		},
		{
			name:    "EncyptedParallelBackupEncryptionKey",
			backint: defaultBackint,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				EncryptionKey:   "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "encryption_key": "testKey"}`), nil
			},
			wantOk: false,
		},
		{
			name:    "EncyptedParallelBackupKmsKey",
			backint: defaultBackint,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				KmsKey:          "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "kms_key": "testKey"}`), nil
			},
			wantOk: false,
		},
		{
			name:    "SuccessfullyParseWithDefaultsApplied",
			backint: defaultBackint,
			want: &bpb.BackintConfiguration{
				UserId:            "testUser",
				Function:          bpb.Function_BACKUP,
				ParamFile:         "testParamsFile",
				Bucket:            "testBucket",
				ParallelStreams:   1,
				BufferSizeMb:      100,
				FileReadTimeoutMs: 1000,
				ParallelSizeMb:    128,
				Retries:           5,
				Threads:           defaultThreads(),
				RateLimitMb:       0,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "rate_limit_mb": -1}`), nil
			},
			wantOk: true,
		},
		{
			name: "SuccessfullyParseNoDefaults",
			backint: &Backint{
				user:      "testUser",
				paramFile: "testParamsFile",
				function:  "restore",
			},
			want: &bpb.BackintConfiguration{
				UserId:            "testUser",
				Function:          bpb.Function_RESTORE,
				ParamFile:         "testParamsFile",
				Bucket:            "testBucket",
				ParallelStreams:   2,
				BufferSizeMb:      200,
				FileReadTimeoutMs: 2000,
				ParallelSizeMb:    228,
				Retries:           25,
				Threads:           2,
				RateLimitMb:       200,
				Compress:          true,
				KmsKey:            "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "kms_key": "testKey", "compress": true, "parallel_streams": 2, "buffer_size_mb": 200, "file_read_timeout_ms": 2000, "parallel_size_mb": 228, "retries": 25, "threads": 2, "rate_limit_mb": 200}`), nil
			},
			wantOk: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotOk := test.backint.ParseArgsAndValidateConfig(test.read)
			got := test.backint.config
			if gotOk != test.wantOk {
				t.Errorf("%#v.ParseArgsAndValidateConfig() = %v, want %v", test.backint, gotOk, test.wantOk)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%#v.ParseArgsAndValidateConfig() had unexpected diff (-want +got):\n%s", test.backint, diff)
			}
		})
	}
}

func TestApplyDefaultMaxThreads(t *testing.T) {
	backint := Backint{config: &bpb.BackintConfiguration{}}
	want := &bpb.BackintConfiguration{
		ParallelStreams:   1,
		BufferSizeMb:      100,
		FileReadTimeoutMs: 1000,
		ParallelSizeMb:    128,
		Retries:           5,
		Threads:           64,
	}
	backint.applyDefaults(65)
	got := backint.config
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("%#v.applyDefaults(65) had unexpected diff (-want +got):\n%s", backint, diff)
	}
}
