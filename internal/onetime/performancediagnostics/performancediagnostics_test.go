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

package performancediagnostics

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	s "cloud.google.com/go/storage"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	fakeServer = fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "object.txt",
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				// The backup object name is in the format <userID>/<fileName>/<externalBackupID>.bak
				Name: "test@TST/object.txt/12345.bak",
			},
			Content: []byte("test content"),
		},
	})
	defaultStorageClient = func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
		return fakeServer.Client(), nil
	}
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:    "default-project",
		InstanceName: "default-instance",
	}
)

type fakeBucketHandle struct {
	attrs *s.BucketAttrs
	err   error
}

func fakeExecForErr(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecForSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{ExitCode: 0, StdErr: "success", Error: nil}
}

func (f *fakeBucketHandle) Attrs(ctx context.Context) (*s.BucketAttrs, error) {
	return f.attrs, f.err
}

func defaultParametersFile(t *testing.T) *os.File {
	filePath := t.TempDir() + "/parameters.json"
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("os.Create(%v) failed: %v", filePath, err)
	}
	f.WriteString(`{
		"bucket": "test-bucket",
		"retries": 5,
		"parallel_streams": 2,
		"buffer_size_mb": 100,
		"encryption_key": "",
		"compress": false,
		"kms_key": "",
		"service_account_key": "",
		"rate_limit_mb": 0,
		"file_read_timeout_ms": 1000,
		"dump_data": false,
		"log_level": "INFO",
		"log_delay_sec": 3
	}`)
	return f
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailParseAndValidateConfig",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForAgentVersion",
			d: &Diagnose{
				version: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			d: &Diagnose{
				help: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, tc.args...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Execute() returned an unexpected diff (-want +got): %v", diff)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	c := &Diagnose{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	c.SetFlags(fs)

	flags := []string{
		"scope", "test-bucket", "param-file", "result-bucket", "bundle-name",
		"hyper-threading", "path", "loglevel", "help", "h", "version", "v",
	}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		want bool
	}{
		{
			name: "InvalidScope",
			d: &Diagnose{
				scope: "nw",
			},
			want: false,
		},
		{
			name: "NoParamFileAndTestBucket1",
			d: &Diagnose{
				scope: "all",
			},
			want: false,
		},
		{
			name: "NoParamFileAndTestBucket2",
			d: &Diagnose{
				scope: "backup",
			},
			want: false,
		},
		{
			name: "ParamFilePresent",
			d: &Diagnose{
				scope:     "all",
				paramFile: "/tmp/param_file.txt",
			},
			want: true,
		},
		{
			name: "TestBucketPresent",
			d: &Diagnose{
				scope:      "all",
				testBucket: "test_bucket",
			},
			want: true,
		},
		{
			name: "Valid1",
			d: &Diagnose{
				scope:      "io",
				path:       "/tmp/path.txt",
				bundleName: "test_bundle",
			},
			want: true,
		},
		{
			name: "Valid2",
			d: &Diagnose{
				scope:      "backup, io",
				testBucket: "test_bucket",
				path:       "/tmp/path.txt",
				bundleName: "test_bundle",
			},
			want: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.validateParams(ctx, &flag.FlagSet{Usage: func() { return }})
			if got != tc.want {
				t.Errorf("validateParams(ctx) = %v, want: %v", got, tc.want)
			}
		})
	}
}

func TestListOperations(t *testing.T) {
	tests := []struct {
		name       string
		operations []string
		want       map[string]struct{}
	}{
		{
			name:       "InvalidOpPresent",
			operations: []string{"backup", " nw"},
			want: map[string]struct{}{
				"backup": {},
			},
		},
		{
			name:       "AllPresent",
			operations: []string{"all", "backup", "io"},
			want: map[string]struct{}{
				"all": {},
			},
		},
		{
			name:       "AllAbsent",
			operations: []string{"backup", "io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
		{
			name:       "Spaces",
			operations: []string{" backup  ", "  io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
		{
			name:       "DuplicateOps",
			operations: []string{"backup", "backup", "io", "io", " io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := listOperations(ctx, tc.operations)
			if diff := cmp.Diff(tc.want, got, cmpopts.SortMaps(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("listOperations(%v) returned an unexpected diff (-want +got): %v", tc.operations, diff)
			}
		})
	}
}

func TestCheckRetention(t *testing.T) {
	tests := []struct {
		name    string
		d       *Diagnose
		client  storage.Client
		ctb     connectToBucket
		config  *bpb.BackintConfiguration
		wantErr error
	}{
		{
			name:    "NoBucket",
			d:       &Diagnose{},
			config:  &bpb.BackintConfiguration{},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ErrorConnectingToBucket",
			d: &Diagnose{
				testBucket: "test-bucket-1",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				return nil, false
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ConfigBucketHasRetention",
			d:    &Diagnose{},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				fbh := &fakeBucketHandle{
					attrs: &s.BucketAttrs{
						Name: "test-bucket",
						RetentionPolicy: &s.RetentionPolicy{
							RetentionPeriod: time.Nanosecond,
						},
					},
				}
				return fbh, true
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TestBucketPresentHasNoRetention",
			d: &Diagnose{
				testBucket: "test-bucket-1",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				fbh := &fakeBucketHandle{
					attrs: &s.BucketAttrs{
						Name: "test-bucket-1",
					},
				}
				return fbh, true
			},
			wantErr: nil,
		},
		{
			name: "TestBucketPresentHasRetention",
			d: &Diagnose{
				testBucket: "test-bucket-1",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				fbh := &fakeBucketHandle{
					attrs: &s.BucketAttrs{
						Name: "test-bucket-1",
						RetentionPolicy: &s.RetentionPolicy{
							RetentionPeriod: time.Nanosecond,
						},
					},
				}
				return fbh, true
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoRetention1",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				fbh := &fakeBucketHandle{
					attrs: &s.BucketAttrs{
						Name: "test-bucket",
					},
				}
				return fbh, true
			},
			wantErr: nil,
		},
		{
			name: "NoRetention2",
			d: &Diagnose{
				paramFile:  "/tmp/param_file.json",
				testBucket: "test-bucket-1",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test-bucket",
			},
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool) {
				fbh := &fakeBucketHandle{
					attrs: &s.BucketAttrs{
						Name: "test-bucket",
					},
				}
				return fbh, true
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.d.checkRetention(ctx, tc.client, tc.ctb, tc.config)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("checkRetention(%v, %v, %v) returned error: %v, want error: %v", tc.client, tc.ctb, tc.config, gotErr, tc.wantErr)
			}
		})
	}
}

func TestSetBucket(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		config     *bpb.BackintConfiguration
		fs         filesystem.FileSystem
		wantBucket string
		wantErr    error
	}{
		{
			name: "TestBucketEmpty",
			d:    &Diagnose{},
			config: &bpb.BackintConfiguration{
				Bucket: "test_bucket",
			},
			wantErr: nil,
		},
		{
			name: "TestBucketPresent",
			d: &Diagnose{
				testBucket: "test_bucket1",
				path:       "/tmp/",
				paramFile:  "/param_file.json",
			},
			config: &bpb.BackintConfiguration{
				Bucket: "test_bucket",
			},
			fs:         filesystem.Helper{},
			wantBucket: "test_bucket1",
			wantErr:    nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.d.setBucket(ctx, tc.fs, tc.config)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("setBucket(%v, %v) returned error: %v, want error: %v", tc.fs, tc.config, gotErr, tc.wantErr)
			}

			if tc.wantBucket != "" {
				content, err := os.ReadFile(tc.d.paramFile)
				if err != nil {
					t.Fatalf("os.ReadFile(%s) failed: %v", tc.d.paramFile, err)
				}
				config, err := configuration.Unmarshal(tc.d.paramFile, content)
				if err != nil {
					t.Fatalf("configuration.Unmarshal(%s) failed: %v", tc.d.paramFile, err)
				}
				if config.GetBucket() != tc.wantBucket {
					t.Errorf("setBucket(%v, %v) = %v, want %v", tc.fs, tc.config, config.GetBucket(), tc.wantBucket)
				}
			}
		})
	}
}

func TestAddToBundle(t *testing.T) {
	tests := []struct {
		name    string
		paths   []moveFiles
		fs      filesystem.FileSystem
		wantErr error
	}{
		{
			name:    "Empty",
			wantErr: nil,
		},
		{
			name: "InvalidPaths",
			paths: []moveFiles{
				{
					oldPath: "/tmp/old_path",
				},
				{
					newPath: "/tmp/new_path",
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ValidPaths",
			paths: []moveFiles{
				{
					oldPath: "/tmp/old/path.txt",
					newPath: "/tmp/new/path.txt",
				},
			},
			fs:      filesystem.Helper{},
			wantErr: nil,
		},
	}

	ctx := context.Background()
	if err := os.MkdirAll("/tmp/old", 0777); err != nil {
		fmt.Printf("os.MkdirAll(%s, 0777) failed: %v", "/tmp/old", err)
	}
	if err := os.MkdirAll("/tmp/new", 0777); err != nil {
		fmt.Printf("os.MkdirAll(%s, 0777) failed: %v", "/tmp/new", err)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range tc.paths {
				if path.oldPath == "" || path.newPath == "" {
					continue
				}
				if _, err := tc.fs.Create(path.oldPath); err != nil {
					fmt.Printf("tc.fs.Create(%s) failed, failed to create temp file at oldPath: %v", path.oldPath, err)
				}
			}
			gotErr := addToBundle(ctx, tc.paths, tc.fs)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("addToBundle(%v, %v) returned error: %v, want error: %v", tc.paths, tc.fs, gotErr, tc.wantErr)
			}

			for _, path := range tc.paths {
				if path.oldPath == "" || path.newPath == "" {
					continue
				}
				if _, err := tc.fs.Stat(path.newPath); os.IsNotExist(err) {
					t.Errorf("addToBundle(%v, %v) did not create new file: %s", tc.paths, tc.fs, path.newPath)
				}
			}
		})
	}
}

func TestGetParamFileName(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		want string
	}{
		{
			name: "Empty",
			d:    &Diagnose{},
		},
		{
			name: "SampleParamFile1",
			d: &Diagnose{
				paramFile: "/tmp/sample_param_file.txt",
			},
			want: "sample_param_file.txt",
		},
		{
			name: "SampleParamFile2",
			d: &Diagnose{
				paramFile: "sample_param_file.txt",
			},
			want: "sample_param_file.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.d.getParamFileName()
			if got != test.want {
				t.Errorf("getParamFileName(%#v) = %s, want %s", test.d, got, test.want)
			}
		})
	}
}

func TestUnmarshalBackintConfig(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		read       ReadConfigFile
		wantConfig *bpb.BackintConfiguration
		wantErr    error
	}{
		{
			name: "ErrorRead",
			d:    &Diagnose{},
			read: func(string) ([]byte, error) {
				return nil, fmt.Errorf("error")
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "EmptyParamFile",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MalformedConfig",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"test_bucket": "test_bucket", "enc": "true"}`
				return []byte(fileContent), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ValidConfig",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			wantConfig: &bpb.BackintConfiguration{
				Bucket: "test_bucket",
			},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.unmarshalBackintConfig(ctx, tc.read)
			if diff := cmp.Diff(tc.wantConfig, got, protocmp.Transform()); diff != "" {
				t.Errorf("unmarshalBackintConfig(%v) returned an unexpected diff (-want +got): %v", tc.read, diff)
			}
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("unmarshalBackintConfig(%v) returned an unexpected error: %v", tc.read, err)
			}
		})
	}
}

func TestRunPerfDiag(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		opts       *options
		wantErrCnt int
	}{
		{
			name: "TestBucketFromFlag",
			d: &Diagnose{
				testBucket: "test_bucket",
			},
			opts: &options{
				exec: fakeExecForSuccess,
			},
			wantErrCnt: 0,
		},
		{
			name: "TestBucketFromParamFile",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			opts: &options{
				config: &bpb.BackintConfiguration{
					Bucket: "test_bucket",
				},
				exec: fakeExecForSuccess,
			},
			wantErrCnt: 0,
		},
		{
			name: "ErrorInCommandExecution",
			d: &Diagnose{
				paramFile: "/tmp/param_file.json",
			},
			opts: &options{
				config: &bpb.BackintConfiguration{
					Bucket: "test_bucket",
				},
				exec: fakeExecForErr,
			},
			wantErrCnt: 4,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Diagnose
			got := d.runPerfDiag(ctx, tc.opts)
			if len(got) != tc.wantErrCnt {
				t.Errorf("runPerfDiag(%v) returned an unexpected diff (-want +got): %d, %d", tc.opts, tc.wantErrCnt, got)
			}
		})
	}
}
