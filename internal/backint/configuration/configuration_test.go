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

package configuration

import (
	"os"
	"runtime"
	"testing"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"go.uber.org/zap/zapcore"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultReadConfigFile = func(p string) ([]byte, error) {
		return nil, nil
	}
	readConfigFileError = func(p string) ([]byte, error) {
		return nil, cmpopts.AnyError
	}
	defaultParameters = &Parameters{
		User:      "testUser",
		ParamFile: "testParamsFile.json",
		Function:  "backup",
	}
	defaultLegacyParameters = &Parameters{
		User:      "testUser",
		ParamFile: "testParamsFile.txt",
		Function:  "backup",
	}
	defaultConfigArgsParsed = &bpb.BackintConfiguration{
		UserId:    "testUser",
		Function:  bpb.Function_BACKUP,
		ParamFile: "testParamsFile.json",
	}
	defaultLegacyConfigArgsParsed = &bpb.BackintConfiguration{
		UserId:    "testUser",
		Function:  bpb.Function_BACKUP,
		ParamFile: "testParamsFile.txt",
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
		name              string
		params            *Parameters
		read              ReadConfigFile
		readEncryptionKey ReadConfigFile
		want              *bpb.BackintConfiguration
		wantErr           error
	}{
		{
			name: "NoUser",
			params: &Parameters{
				User: "",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoParamsFile",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoFunction",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "InvalidFunction",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "testFunction",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ParametersReadFileError",
			params:  defaultParameters,
			want:    defaultConfigArgsParsed,
			read:    readConfigFileError,
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ParametersFileEmpty",
			params:  defaultParameters,
			want:    defaultConfigArgsParsed,
			read:    defaultReadConfigFile,
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "ParametersFileMalformed",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`test`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "NoBucket",
			params: defaultParameters,
			want:   defaultConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": ""}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "BucketWithForwardSlash",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:    "testUser",
				Function:  bpb.Function_BACKUP,
				ParamFile: "testParamsFile.json",
				Bucket:    "//test-bucket",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "//test-bucket"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "BucketWithGsPrefix",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:    "testUser",
				Function:  bpb.Function_BACKUP,
				ParamFile: "testParamsFile.json",
				Bucket:    "gs:test-bucket",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "gs:test-bucket"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "RecoveryBucketWithForwardSlash",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:         "testUser",
				Function:       bpb.Function_BACKUP,
				ParamFile:      "testParamsFile.json",
				Bucket:         "test-bucket",
				RecoveryBucket: "//test-bucket",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "test-bucket", "recovery_bucket": "//test-bucket"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "RecoveryBucketWithGsPrefix",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:         "testUser",
				Function:       bpb.Function_BACKUP,
				ParamFile:      "testParamsFile.json",
				Bucket:         "test-bucket",
				RecoveryBucket: "gs:test-bucket",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "test-bucket", "recovery_bucket": "gs:test-bucket"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptionKeyAndKmsKeyDefined",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:        "testUser",
				Function:      bpb.Function_BACKUP,
				ParamFile:     "testParamsFile.json",
				Bucket:        "testBucket",
				EncryptionKey: "testKey",
				KmsKey:        "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "encryption_key": "testKey", "kms_key": "testKey"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "CompressedParallelBackup",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile.json",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				Compress:        true,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "compress": true}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptedParallelBackupEncryptionKey",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile.json",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				EncryptionKey:   "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "encryption_key": "testKey"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptedParallelBackupKmsKey",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:          "testUser",
				Function:        bpb.Function_BACKUP,
				ParamFile:       "testParamsFile.json",
				Bucket:          "testBucket",
				ParallelStreams: 2,
				KmsKey:          "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_streams": 2, "kms_key": "testKey"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "CompressedXMLMultipartBackup",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:             "testUser",
				Function:           bpb.Function_BACKUP,
				ParamFile:          "testParamsFile.json",
				Bucket:             "testBucket",
				XmlMultipartUpload: true,
				Compress:           true,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "xml_multipart_upload": true, "compress": true}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptedXMLMultipartBackupEncryptionKey",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:             "testUser",
				Function:           bpb.Function_BACKUP,
				ParamFile:          "testParamsFile.json",
				Bucket:             "testBucket",
				XmlMultipartUpload: true,
				EncryptionKey:      "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "xml_multipart_upload": true, "encryption_key": "testKey"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptedXMLMultipartBackupKmsKey",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:             "testUser",
				Function:           bpb.Function_BACKUP,
				ParamFile:          "testParamsFile.json",
				Bucket:             "testBucket",
				XmlMultipartUpload: true,
				KmsKey:             "testKey",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "xml_multipart_upload": true, "kms_key": "testKey"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CompressedParallelRestore",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
			},
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_RESTORE,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				ParallelRecoveryStreams: 2,
				Compress:                true,
				InputFile:               "/input.txt",
				OutputFile:              "/output.txt",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "parallel_recovery_streams": 2, "compress": true}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "DefaultParallelStreamsForXMLMultipart",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_BACKUP,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				ParallelStreams:         16,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				Threads:                 defaultThreads(),
				InputFile:               "/dev/stdin",
				OutputFile:              "/dev/stdout",
				LogToCloud:              wpb.Bool(true),
				SendMetricsToMonitoring: wpb.Bool(true),
				XmlMultipartUpload:      true,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "xml_multipart_upload": true}`), nil
			},
			wantErr: nil,
		},
		{
			name:   "IncorrectRetentionMode",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:              "testUser",
				Function:            bpb.Function_BACKUP,
				ParamFile:           "testParamsFile.json",
				Bucket:              "testBucket",
				ObjectRetentionMode: "LOCKED",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "object_retention_mode": "LOCKED"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "RetentionModeWithoutTime",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:              "testUser",
				Function:            bpb.Function_BACKUP,
				ParamFile:           "testParamsFile.json",
				Bucket:              "testBucket",
				ObjectRetentionMode: "Locked",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "object_retention_mode": "Locked"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "RetentionTimeWithoutMode",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:              "testUser",
				Function:            bpb.Function_BACKUP,
				ParamFile:           "testParamsFile.json",
				Bucket:              "testBucket",
				ObjectRetentionTime: "UTCNow+7d",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "object_retention_time": "UTCNow+7d"}`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "SuccessfullyParseWithDefaultsApplied",
			params: defaultParameters,
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_BACKUP,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				RecoveryBucket:          "recoveryBucket",
				ParallelStreams:         1,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				Threads:                 defaultThreads(),
				RateLimitMb:             0,
				InputFile:               "/dev/stdin",
				OutputFile:              "/dev/stdout",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "test/1/2/3/",
				SendMetricsToMonitoring: wpb.Bool(true),
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "recovery_bucket": "recoveryBucket", "rate_limit_mb": -1, "folder_prefix": "test/1/2/3"}`), nil
			},
			wantErr: nil,
		},
		{
			name: "SuccessfullyParseNoDefaults",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
			},
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_RESTORE,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "recoveryBucket",
				RecoveryBucket:          "recoveryBucket",
				ParallelStreams:         32,
				BufferSizeMb:            250,
				FileReadTimeoutMs:       2000,
				Retries:                 25,
				StorageClass:            bpb.StorageClass_COLDLINE,
				Threads:                 64,
				RateLimitMb:             200,
				Compress:                true,
				KmsKey:                  "testKey",
				InputFile:               "/input.txt",
				OutputFile:              "/output.txt",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "test/1/2/3/",
				RecoveryFolderPrefix:    "test/1/2/3/",
				SendMetricsToMonitoring: wpb.Bool(false),
				ObjectRetentionMode:     "Unlocked",
				ObjectRetentionTime:     "UTCNow+7d",
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "recovery_bucket": "recoveryBucket", "kms_key": "testKey", "compress": true, "parallel_streams": 33, "buffer_size_mb": 300, "file_read_timeout_ms": 2000, "retries": 25, "threads": 200, "rate_limit_mb": 200, "folder_prefix": "test/1/2/3/", "recovery_folder_prefix": "test/1/2/3/", "storage_class": "COLDLINE", "send_metrics_to_monitoring": false, "object_retention_mode": "Unlocked", "object_retention_time": "UTCNow+7d"}`), nil
			},
			wantErr: nil,
		},
		{
			name: "SuccessfullyParseRecoveryParametersNoFolder",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
			},
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_RESTORE,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "recoveryBucket",
				RecoveryBucket:          "recoveryBucket",
				ParallelStreams:         1,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				Threads:                 defaultThreads(),
				RateLimitMb:             0,
				InputFile:               "/input.txt",
				OutputFile:              "/output.txt",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "",
				SendMetricsToMonitoring: wpb.Bool(true),
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "recovery_bucket": "recoveryBucket", "folder_prefix": "test/1/2/3", "recovery_folder_prefix": ""}`), nil
			},
			wantErr: nil,
		},
		{
			name: "SuccessfullyParseRecoveryParametersNoBucket",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.json",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
			},
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_RESTORE,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				ParallelStreams:         1,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				Threads:                 defaultThreads(),
				RateLimitMb:             0,
				InputFile:               "/input.txt",
				OutputFile:              "/output.txt",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "test/recovery/1/2/3/",
				RecoveryFolderPrefix:    "test/recovery/1/2/3/",
				SendMetricsToMonitoring: wpb.Bool(true),
			},
			read: func(p string) ([]byte, error) {
				return []byte(`{"bucket": "testBucket", "recovery_bucket": "", "folder_prefix": "test/1/2/3", "recovery_folder_prefix": "test/recovery/1/2/3/"}`), nil
			},
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.params.ParseArgsAndValidateConfig(test.read, test.readEncryptionKey)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("%#v.ParseArgsAndValidateConfig() = %v, want %v", test.params, gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%#v.ParseArgsAndValidateConfig() had unexpected diff (-want +got):\n%s", test.params, diff)
			}
		})
	}
}

func TestConfigToPrint(t *testing.T) {
	tests := []struct {
		name  string
		input *bpb.BackintConfiguration
		want  *bpb.BackintConfiguration
	}{
		{
			name: "EmptyConfig",
		},
		{
			name: "AllFields",
			input: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_BACKUP,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				RecoveryBucket:          "recoveryBucket",
				BackupId:                "123",
				DatabaseObjectCount:     1,
				BackupLevel:             "FULL",
				ParallelStreams:         1,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				StorageClass:            bpb.StorageClass_STANDARD,
				Compress:                true,
				DumpData:                true,
				EncryptionKey:           "testKey",
				KmsKey:                  "testKey",
				LogLevel:                bpb.LogLevel_DEBUG,
				ServiceAccountKey:       "testAccount",
				Threads:                 32,
				RateLimitMb:             0,
				InputFile:               "/dev/stdin",
				OutputFile:              "/dev/stdout",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "test/1/2/3/",
				RecoveryFolderPrefix:    "test/1/2/3/",
				SendMetricsToMonitoring: wpb.Bool(true),
			},
			// want: `user_id: testUser, function: BACKUP, input_file: /dev/stdin, output_file: /dev/stdout, param_file: testParamsFile.json, backup_id: 123, database_object_count: 1, backup_level: FULL, bucket: testBucket, folder_prefix: test/1/2/3/, storage_class: STANDARD, compress: true, log_to_cloud: true, send_metrics_to_monitoring: true, log_level: DEBUG, retries: 5, parallel_streams: 1, threads: 32, buffer_size_mb: 100, file_read_timeout_ms: 60000, recovery_bucket: recoveryBucket, recovery_folder_prefix: test/1/2/3/`,
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_BACKUP,
				ParamFile:               "testParamsFile.json",
				Bucket:                  "testBucket",
				RecoveryBucket:          "recoveryBucket",
				BackupId:                "123",
				DatabaseObjectCount:     1,
				BackupLevel:             "FULL",
				ParallelStreams:         1,
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				Retries:                 5,
				StorageClass:            bpb.StorageClass_STANDARD,
				Compress:                true,
				DumpData:                true,
				EncryptionKey:           "***",
				KmsKey:                  "***",
				LogLevel:                bpb.LogLevel_DEBUG,
				ServiceAccountKey:       "***",
				Threads:                 32,
				RateLimitMb:             0,
				InputFile:               "/dev/stdin",
				OutputFile:              "/dev/stdout",
				LogToCloud:              wpb.Bool(true),
				FolderPrefix:            "test/1/2/3/",
				RecoveryFolderPrefix:    "test/1/2/3/",
				SendMetricsToMonitoring: wpb.Bool(true),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ConfigToPrint(test.input)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ConfigToPrint(%v) had unexpected diff (-want +got):\n%s", test.input, diff)
			}
		})
	}
}

func TestLegacyParameters(t *testing.T) {
	tests := []struct {
		name              string
		params            *Parameters
		read              ReadConfigFile
		readEncryptionKey ReadConfigFile
		want              *bpb.BackintConfiguration
		wantErr           error
	}{
		{
			name:   "EmptyValueForParameter",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#BUCKET`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseReadIdleTimeout",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#READ_IDLE_TIMEOUT abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseChunkSize",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#CHUNK_SIZE_MB abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseRateLimit",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#RATE_LIMIT_MB abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseRetries",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#MAX_GCS_RETRY abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseParallelFactor",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#PARALLEL_FACTOR abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "FailedToParseThreads",
			params: defaultLegacyParameters,
			want:   defaultLegacyConfigArgsParsed,
			read: func(p string) ([]byte, error) {
				return []byte(`#THREADS abc`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:   "EncryptionKeyAndKmsKeyDefined",
			params: defaultLegacyParameters,
			want: &bpb.BackintConfiguration{
				UserId:        "testUser",
				Function:      bpb.Function_BACKUP,
				ParamFile:     "testParamsFile.txt",
				Bucket:        "testBucket",
				EncryptionKey: "testKey",
				KmsKey:        "testKey",
				LogToCloud:    wpb.Bool(true),
				Compress:      true,
			},
			read: func(p string) ([]byte, error) {
				return []byte(`#BUCKET testBucket
#ENCRYPTION_KEY testKey
#KMS_KEY_NAME testKey`), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SuccessfullyParseAllArgs",
			params: &Parameters{
				User:      "testUser",
				ParamFile: "testParamsFile.txt",
				Function:  "restore",
				InFile:    "/input.txt",
				OutFile:   "/output.txt",
			},
			want: &bpb.BackintConfiguration{
				UserId:                  "testUser",
				Function:                bpb.Function_RESTORE,
				ParamFile:               "testParamsFile.txt",
				Bucket:                  "testBucket",
				ParallelStreams:         32,
				BufferSizeMb:            250,
				FileReadTimeoutMs:       2000,
				Retries:                 25,
				Threads:                 64,
				RateLimitMb:             200,
				Compress:                false,
				DumpData:                true,
				EncryptionKey:           "base64EncodedTestKey",
				InputFile:               "/input.txt",
				OutputFile:              "/output.txt",
				LogToCloud:              wpb.Bool(false),
				ServiceAccountKey:       "testAccount",
				LogLevel:                bpb.LogLevel_DEBUG,
				SendMetricsToMonitoring: wpb.Bool(true),
			},
			read: func(p string) ([]byte, error) {
				return []byte(`#DISABLE_COMPRESSION
#DISABLE_CLOUD_LOGGING
#DUMP_DATA
#BUCKET testBucket
#SERVICE_ACCOUNT testAccount
#ENCRYPTION_KEY testKey
#LOG_LEVEL DEBUG
#READ_IDLE_TIMEOUT 2000
#CHUNK_SIZE_MB 250
#RATE_LIMIT_MB 200
#MAX_GCS_RETRY 25
#PARALLEL_FACTOR 32
#THREADS 64
#PARALLEL_PART_SIZE_MB 150
#FAKE_PARAMETER 123
#ENCRYPTION_KEY /tmp/testKey.txt
`), nil
			},
			readEncryptionKey: func(p string) ([]byte, error) {
				return []byte("base64EncodedTestKey"), nil
			},
			wantErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.params.ParseArgsAndValidateConfig(test.read, test.readEncryptionKey)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("%#v.ParseArgsAndValidateConfig() = %v, want %v", test.params, gotErr, test.wantErr)
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
		LogToCloud:              wpb.Bool(true),
		ParallelStreams:         1,
		BufferSizeMb:            100,
		FileReadTimeoutMs:       60000,
		Retries:                 5,
		Threads:                 64,
		InputFile:               "/dev/stdin",
		OutputFile:              "/dev/stdout",
		SendMetricsToMonitoring: wpb.Bool(true),
	}
	params.ApplyDefaults(65)
	got := params.Config
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("%#v.applyDefaults(65) had unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestLogLevelToZapcore(t *testing.T) {
	tests := []struct {
		name  string
		level bpb.LogLevel
		want  zapcore.Level
	}{
		{
			name:  "INFO",
			level: bpb.LogLevel_INFO,
			want:  zapcore.InfoLevel,
		},
		{
			name:  "DEBUG",
			level: bpb.LogLevel_DEBUG,
			want:  zapcore.DebugLevel,
		},
		{
			name:  "WARNING",
			level: bpb.LogLevel_WARNING,
			want:  zapcore.WarnLevel,
		},
		{
			name:  "ERROR",
			level: bpb.LogLevel_ERROR,
			want:  zapcore.ErrorLevel,
		},
		{
			name:  "UNKNOWN",
			level: bpb.LogLevel_LOG_LEVEL_UNSPECIFIED,
			want:  zapcore.InfoLevel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := LogLevelToZapcore(test.level)
			if got != test.want {
				t.Errorf("LogLevelToZapcore(%v) = %v, want: %v", test.level, got, test.want)
			}
		})
	}
}
