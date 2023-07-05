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

package config

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
	defaultParameters = &Parameters{
		User:      "testUser",
		ParamFile: "testParamsFile",
		Function:  "backup",
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
		name   string
		params *Parameters
		read   ReadConfigFile
		want   *bpb.BackintConfiguration
		wantOk bool
	}{
		{
			name: "NoUser",
			params: &Parameters{
				User: "",
			},
			wantOk: false,
		},
		{
			name: "NoParamsFile",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "",
			},
			wantOk: false,
		},
		{
			name: "NoFunction",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile",
				Function:  "",
			},
			wantOk: false,
		},
		{
			name: "InvalidFunction",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile",
				Function:  "testFunction",
			},
			wantOk: false,
		},
		{
			name:   "ParametersReadFileError",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read:   readConfigFileError,
			wantOk: false,
		},
		{
			name:   "ParametersFileEmpty",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read:   defaultReadConfigFile,
			wantOk: false,
		},
		{
			name:   "ParametersFileMalformed",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`test`), nil
			},
			wantOk: false,
		},
		{
			name:   "NoBucket",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": ""}`), nil
			},
			wantOk: false,
		},
		{
			name:   "EncyptionKeyAndKmsKeyDefined",
			params: defaultParameters,
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
			name:   "CompressedParallelBackup",
			params: defaultParameters,
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
			name:   "EncyptedParallelBackupEncryptionKey",
			params: defaultParameters,
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
			name:   "EncyptedParallelBackupKmsKey",
			params: defaultParameters,
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
			name:   "SuccessfullyParseWithDefaultsApplied",
			params: defaultParameters,
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
				InputFile:         "/dev/stdin",
				OutputFile:        "/dev/stdout",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "rate_limit_mb": -1}`), nil
			},
			wantOk: true,
		},
		{
			name: "SuccessfullyParseNoDefaults",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
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
				InputFile:         "/input.txt",
				OutputFile:        "/output.txt",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "kms_key": "testKey", "compress": true, "parallel_streams": 2, "buffer_size_mb": 200, "file_read_timeout_ms": 2000, "parallel_size_mb": 228, "retries": 25, "threads": 2, "rate_limit_mb": 200}`), nil
			},
			wantOk: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotOk := test.params.ParseArgsAndValidateConfig(test.read)
			if gotOk != test.wantOk {
				t.Errorf("%#v.ParseArgsAndValidateConfig() = %v, want %v", test.params, gotOk, test.wantOk)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%#v.ParseArgsAndValidateConfig() had unexpected diff (-want +got):\n%s", test.params, diff)
			}
		})
	}
}

func TestApplyDefaultMaxThreads(t *testing.T) {
	params := Parameters{Config: &bpb.BackintConfiguration{}}
	want := &bpb.BackintConfiguration{
		ParallelStreams:   1,
		BufferSizeMb:      100,
		FileReadTimeoutMs: 1000,
		ParallelSizeMb:    128,
		Retries:           5,
		Threads:           64,
		InputFile:         "/dev/stdin",
		OutputFile:        "/dev/stdout",
	}
	params.applyDefaults(65)
	got := params.Config
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("%#v.applyDefaults(65) had unexpected diff (-want +got):\n%s", params, diff)
	}
}
