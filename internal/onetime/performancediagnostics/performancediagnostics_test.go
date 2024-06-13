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
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	s "cloud.google.com/go/storage"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

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

	defaultBackintFileJSON = `{
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
	}
	`
)

type fakeBucketHandle struct {
	attrs *s.BucketAttrs
	err   error
}

type mockedZipper struct {
	fileInfoErr     error
	createHeaderErr error
}

type mockedWriter struct {
	err error
}

type mockedFileInfo struct {
	FileName    string
	FileSize    int64
	FileMode    fs.FileMode
	FileModTime time.Time
	Dir         bool
	System      any
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

func (w mockedWriter) Write([]byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return 10, nil
}

func (mz mockedZipper) NewWriter(w io.Writer) *zip.Writer {
	return &zip.Writer{}
}

func (mz mockedZipper) FileInfoHeader(fi fs.FileInfo) (*zip.FileHeader, error) {
	if mz.fileInfoErr != nil {
		return nil, mz.fileInfoErr
	}
	return &zip.FileHeader{}, nil
}

func (mz mockedZipper) CreateHeader(w *zip.Writer, fh *zip.FileHeader) (io.Writer, error) {
	if mz.createHeaderErr != nil {
		return nil, mz.createHeaderErr
	}
	return mockedWriter{err: nil}, nil
}

func (mz mockedZipper) Close(w *zip.Writer) error {
	if w == nil {
		return cmpopts.AnyError
	}
	return nil
}

func (mfi mockedFileInfo) Name() string {
	return mfi.FileName
}

func (mfi mockedFileInfo) Size() int64 {
	return mfi.FileSize
}

func (mfi mockedFileInfo) Mode() fs.FileMode {
	return mfi.FileMode
}

func (mfi mockedFileInfo) ModTime() time.Time {
	return mfi.FileModTime
}

func (mfi mockedFileInfo) IsDir() bool {
	return mfi.Dir
}

func (mfi mockedFileInfo) Sys() any {
	return mfi.System
}

func (mfi mockedFileInfo) Write([]byte) (int, error) {
	return 0, nil
}

type fakeReadWriter struct {
	err error
}

func (f *fakeReadWriter) Upload(ctx context.Context) (int64, error) {
	return 0, f.err
}

func defaultParametersFile(t *testing.T) *os.File {
	filePath := t.TempDir() + "/parameters.json"
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("os.Create(%v) failed: %v", filePath, err)
	}
	f.WriteString(defaultBackintFileJSON)
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
		name    string
		d       *Diagnose
		wantErr bool
	}{
		{
			name: "InvalidScope",
			d: &Diagnose{
				Scope: "nw",
			},
			wantErr: true,
		},
		{
			name: "NoParamFileAndTestBucket1",
			d: &Diagnose{
				Scope: "all",
			},
			wantErr: true,
		},
		{
			name: "NoParamFileAndTestBucket2",
			d: &Diagnose{
				Scope: "backup",
			},
			wantErr: true,
		},
		{
			name: "ParamFilePresent",
			d: &Diagnose{
				Scope:     "all",
				ParamFile: "/tmp/param_file.txt",
			},
			wantErr: false,
		},
		{
			name: "TestBucketPresent",
			d: &Diagnose{
				Scope:      "all",
				TestBucket: "test_bucket",
			},
			wantErr: false,
		},
		{
			name: "Valid1",
			d: &Diagnose{
				Scope:      "io",
				Path:       "/tmp/path.txt",
				BundleName: "test_bundle",
			},
			wantErr: false,
		},
		{
			name: "Valid2",
			d: &Diagnose{
				Scope:      "backup, io",
				TestBucket: "test_bucket",
				Path:       "/tmp/path.txt",
				BundleName: "test_bundle",
			},
			wantErr: false,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.validateParams(ctx, &flag.FlagSet{Usage: func() { return }})
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("validateParams(ctx) = %v, want error presence = %v", err, tc.wantErr)
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
				TestBucket: "test-bucket-1",
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
				TestBucket: "test-bucket-1",
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
				TestBucket: "test-bucket-1",
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
				ParamFile: "/tmp/param_file.json",
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
				ParamFile:  "/tmp/param_file.json",
				TestBucket: "test-bucket-1",
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
				ParamFile: "/tmp/sample_param_file.txt",
			},
			want: "sample_param_file.txt",
		},
		{
			name: "SampleParamFile2",
			d: &Diagnose{
				ParamFile: "sample_param_file.txt",
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

func TestSetBackintConfig(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		read       ReadConfigFile
		wantConfig *bpb.BackintConfiguration
		wantErr    error
	}{
		{
			name: "NoTestBucketAndParamFileProvided",
			d:    &Diagnose{},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "TestBucketError",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				ParamFile:  "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return []byte{}, fmt.Errorf("error")
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "UnmarshalError",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return []byte{}, fmt.Errorf("error")
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "EmptyConfigWithNoTestBucket",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "MalformedConfig",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
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
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			wantConfig: &bpb.BackintConfiguration{
				Bucket:                  "test_bucket",
				BufferSizeMb:            100,
				FileReadTimeoutMs:       60000,
				InputFile:               "/dev/stdin",
				LogToCloud:              &wpb.BoolValue{Value: true},
				OutputFile:              "backup/backint-output.log",
				ParallelStreams:         1,
				Retries:                 5,
				SendMetricsToMonitoring: &wpb.BoolValue{Value: true},
				DiagnoseFileMaxSizeGb:   1,
				StorageClass:            bpb.StorageClass_STANDARD,
				Threads:                 int64(runtime.NumCPU()),
			},
		},
	}

	ctx := context.Background()

	fs := &fake.FileSystem{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.d.setBackintConfig(ctx, fs, tc.read)
			if diff := cmp.Diff(tc.wantConfig, got, protocmp.Transform()); diff != "" {
				t.Errorf("unmarshalBackintConfig(%v) returned an unexpected diff (-want +got): %v", tc.read, diff)
			}
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("unmarshalBackintConfig(%v) returned an unexpected error: %v", tc.read, err)
			}
		})
	}
}

func TestCreateTempParamFile(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		fs         filesystem.FileSystem
		read       ReadConfigFile
		wantConfig *bpb.BackintConfiguration
		wantErr    error
	}{
		{
			name: "unmarshalBackintError",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				ParamFile:  "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return nil, fmt.Errorf("error")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "createError",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				ParamFile:  "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			fs: &fake.FileSystem{
				CreateResp: []*os.File{nil},
				CreateErr:  []error{fmt.Errorf("error")},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				ParamFile:  "/tmp/param_file.json",
				Path:       os.TempDir(),
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			fs: filesystem.Helper{},
			wantConfig: &bpb.BackintConfiguration{
				Bucket: "test_bucket-2",
			},
			wantErr: nil,
		},
		{
			name: "SuccessWithEmptyConfig",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				ParamFile:  "/tmp/param_file.txt",
				Path:       os.TempDir(),
			},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			fs: filesystem.Helper{},
			wantConfig: &bpb.BackintConfiguration{
				Bucket: "test_bucket-2",
			},
			wantErr: nil,
		},
		{
			name: "SuccessWithEmptyParamFile",
			d: &Diagnose{
				TestBucket: "test_bucket-2",
				Path:       os.TempDir(),
			},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			fs: filesystem.Helper{},
			wantConfig: &bpb.BackintConfiguration{
				Bucket: "test_bucket-2",
			},
			wantErr: nil,
		},
	}

	// Creating a backup directory for SuccessWithEmptyParamFile testcase.
	if err := os.MkdirAll(path.Join(os.TempDir(), "backup"), 0777); err != nil {
		t.Fatalf("os.MkdirAll(%v, 0777) returned an unexpected error: %v", os.TempDir(), err)
	}
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.d.createTempParamFile(ctx, tc.fs, tc.read)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createTempParamFile(%v, %v) returned an unexpected error: %v", tc.fs, tc.read, gotErr)
			}

			if tc.wantConfig != nil {
				got, err := tc.d.unmarshalBackintConfig(ctx, os.ReadFile)
				if err != nil {
					t.Errorf("unmarshalBackintConfig(%v, %v) returned an unexpected error: %v", tc.fs, tc.read, err)
				}
				if diff := cmp.Diff(tc.wantConfig, got, protocmp.Transform()); diff != "" {
					t.Errorf("unmarshalBackintConfig(%v, %v) returned an unexpected diff (-want +got): %v", tc.fs, tc.read, diff)
				}
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
			name: "NoParamFile",
			d:    &Diagnose{},
			read: func(string) ([]byte, error) {
				return nil, nil
			},
			wantConfig: nil,
		},
		{
			name: "ErrorRead",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return nil, fmt.Errorf("error")
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "EmptyParamFile",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			wantConfig: nil,
			wantErr:    nil,
		},
		{
			name: "ErrorUnmarshal",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"test_bucket": "test_bucket", "enc": "true"}`
				return []byte(fileContent), nil
			},
			wantConfig: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "ValidConfig",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
			},
			read: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			wantConfig: &bpb.BackintConfiguration{
				Bucket: "test_bucket",
			},
			wantErr: nil,
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
				TestBucket: "test_bucket",
			},
			opts: &options{
				exec: fakeExecForSuccess,
			},
			wantErrCnt: 0,
		},
		{
			name: "TestBucketFromParamFile",
			d: &Diagnose{
				ParamFile: "/tmp/param_file.json",
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
				ParamFile: "/tmp/param_file.json",
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

func TestRunFIOCommands(t *testing.T) {
	tests := []struct {
		name    string
		opts    *options
		wantCnt int
	}{
		{
			name: "ErrorCreatingDir",
			opts: &options{
				exec: fakeExecForErr,
				fs: &fake.FileSystem{
					MkDirErr: []error{fmt.Errorf("error")},
				},
			},
			wantCnt: 1,
		},
		{
			name: "ErrorExecutingCommand",
			opts: &options{
				exec: fakeExecForErr,
				fs: &fake.FileSystem{
					MkDirErr: []error{nil},
				},
			},
			wantCnt: 4,
		},
		{
			name: "Success",
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					MkDirErr:              []error{nil},
					OpenFileResp:          []*os.File{&os.File{}, &os.File{}, &os.File{}, &os.File{}},
					OpenFileErr:           []error{nil, nil, nil, nil},
					WriteStringToFileResp: []int{0, 0, 0, 0},
					WriteStringToFileErr:  []error{nil, nil, nil, nil},
				},
			},
			wantCnt: 0,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Diagnose
			got := d.runFIOCommands(ctx, tc.opts)
			if len(got) != tc.wantCnt {
				t.Errorf("runFIOCommands(%v) returned an unexpected diff (-want +got): %d %d", tc.opts, len(got), tc.wantCnt)
			}
		})
	}
}

func TestExecAndWriteToFile(t *testing.T) {
	tests := []struct {
		name       string
		opFile     string
		targetPath string
		params     commandlineexecutor.Params
		opts       *options
		wantErr    error
	}{
		{
			name:       "CommandFailure",
			opFile:     "test_file.txt",
			targetPath: bundlePath,
			params:     commandlineexecutor.Params{},
			opts: &options{
				exec: fakeExecForErr,
				fs: &fake.FileSystem{
					MkDirErr:     []error{nil},
					OpenFileResp: []*os.File{&os.File{}},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "FileCreationFailure",
			opFile:     "test_file.txt",
			targetPath: bundlePath,
			params:     commandlineexecutor.Params{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					MkDirErr:     []error{nil},
					OpenFileErr:  []error{fmt.Errorf("error")},
					OpenFileResp: []*os.File{&os.File{}},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "FalureWritingToFile",
			opFile:     "test_file.txt",
			targetPath: bundlePath,
			params:     commandlineexecutor.Params{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					MkDirErr:              []error{nil},
					WriteStringToFileResp: []int{0},
					WriteStringToFileErr:  []error{fmt.Errorf("error")},
					OpenFileResp:          []*os.File{&os.File{}},
					OpenFileErr:           []error{nil},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "Success",
			opFile:     "test_file.txt",
			targetPath: bundlePath,
			params:     commandlineexecutor.Params{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					MkDirErr:              []error{nil},
					OpenFileResp:          []*os.File{&os.File{}},
					OpenFileErr:           []error{nil},
					WriteStringToFileResp: []int{0},
					WriteStringToFileErr:  []error{nil},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := execAndWriteToFile(ctx, tc.opFile, tc.targetPath, tc.params, tc.opts)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("execAndWriteToFile(%q, %q, %v, %v) returned error: %v, want error: %v", tc.opFile, tc.targetPath, tc.params, tc.opts, gotErr, tc.wantErr)
			}
		})
	}
}

func TestZipSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		target string
		fu     filesystem.FileSystem
		z      zipper.Zipper
		want   error
	}{
		{
			name:   "CreateError",
			source: "sampleFile",
			target: "failure",
			fu: &fake.FileSystem{
				CreateErr:  []error{fmt.Errorf("create error")},
				CreateResp: []*os.File{nil},
			},
			z:    mockedZipper{},
			want: cmpopts.AnyError,
		},
		{
			name:   "WalkAndZipError",
			source: "failure",
			target: "destFile",
			fu: &fake.FileSystem{
				CreateErr:     []error{nil},
				CreateResp:    []*os.File{&os.File{}},
				WalkAndZipErr: []error{fmt.Errorf("zip error")},
			},
			z:    mockedZipper{},
			want: cmpopts.AnyError,
		},
		{
			name:   "RenameError",
			source: "sampleFile",
			target: "dest",
			fu: &fake.FileSystem{
				CreateErr:     []error{nil},
				CreateResp:    []*os.File{&os.File{}},
				WalkAndZipErr: []error{nil},
				RenameErr:     []error{fmt.Errorf("rename error")},
			},
			z:    mockedZipper{},
			want: cmpopts.AnyError,
		},
		{
			name:   "Success",
			source: "sampleFile",
			target: "destFile",
			fu: &fake.FileSystem{
				CreateErr:     []error{nil},
				CreateResp:    []*os.File{&os.File{}},
				WalkAndZipErr: []error{nil},
				RenameErr:     []error{nil},
			},
			z:    mockedZipper{},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := zipSource(test.source, test.target, test.fu, test.z)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("zipSource(%q, %q) = %v, want %v", test.source, test.target, got, test.want)
			}
		})
	}
}

func TestRemoveDestinationFolder(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		fu      filesystem.FileSystem
		wantErr error
	}{
		{
			name: "RemoveError",
			path: "failure",
			fu: &fake.FileSystem{
				RemoveAllErr: []error{fmt.Errorf("remove error")},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "RemoveSuccess",
			path: "sampleFile",
			fu: &fake.FileSystem{
				RemoveAllErr: []error{nil},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := removeDestinationFolder(context.Background(), tc.path, tc.fu)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("removeDestinationFolder(%q, %v) returned an unexpected diff (-want +got): %v", tc.path, tc.fu, diff)
			}
		})
	}
}

func TestUploadZip(t *testing.T) {
	tests := []struct {
		name          string
		d             *Diagnose
		destFilesPath string
		ctb           storage.BucketConnector
		grw           getReaderWriter
		opts          *options
		wantErr       error
	}{
		{
			name: "OpenFail",
			d: &Diagnose{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "failure",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*s.BucketHandle, bool) {
				return &s.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			opts: &options{
				fs: &fake.FileSystem{
					OpenErr:  []error{fmt.Errorf("error")},
					OpenResp: []*os.File{nil},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "StatFail",
			d: &Diagnose{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*s.BucketHandle, bool) {
				return &s.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			opts: &options{
				fs: &fake.FileSystem{
					OpenErr:  []error{nil},
					OpenResp: []*os.File{&os.File{}},
					StatErr:  []error{fmt.Errorf("error")},
					StatResp: []os.FileInfo{nil},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ConnectToBucketFail",
			d: &Diagnose{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*s.BucketHandle, bool) {
				return nil, false
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			opts: &options{
				fs: &fake.FileSystem{
					OpenErr:  []error{nil},
					OpenResp: []*os.File{&os.File{}},
					StatErr:  []error{nil},
					StatResp: []os.FileInfo{
						mockedFileInfo{FileName: "samplefile", FileMode: 0777},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UploadFail",
			d: &Diagnose{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*s.BucketHandle, bool) {
				return &s.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			opts: &options{
				fs: &fake.FileSystem{
					OpenErr:  []error{nil},
					OpenResp: []*os.File{&os.File{}},
					StatErr:  []error{nil},
					StatResp: []os.FileInfo{
						mockedFileInfo{FileName: "samplefile", FileMode: 0777},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UploadSuccess",
			d: &Diagnose{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*s.BucketHandle, bool) {
				return &s.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{}
			},
			opts: &options{
				fs: &fake.FileSystem{
					OpenErr:  []error{nil},
					OpenResp: []*os.File{&os.File{}},
					StatErr:  []error{nil},
					StatResp: []os.FileInfo{
						mockedFileInfo{FileName: "samplefile", FileMode: 0777},
					},
				},
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.d.uploadZip(ctx, tc.destFilesPath, tc.ctb, tc.grw, tc.opts)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("uploadZip(%q, %v, %v) returned an unexpected error: %v", tc.destFilesPath, tc.ctb, tc.grw, diff)
			}
		})
	}
}

func TestRunConfigureInstanceOTE(t *testing.T) {
	tests := []struct {
		name       string
		opts       *options
		wantStatus subcommands.ExitStatus
	}{
		{
			name: "ErrorWhileExecutingOTE",
			opts: &options{
				exec: fakeExecForErr,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
				},
			},
			wantStatus: subcommands.ExitUsageError,
		},
		{
			name: "ErrorWhileAddingLogsToBundle",
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenResp:   []*os.File{&os.File{}},
					OpenErr:    []error{fmt.Errorf("error")},
					CopyResp:   []int64{0},
					CopyErr:    []error{fmt.Errorf("error")},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{fmt.Errorf("error")},
				},
			},
			wantStatus: subcommands.ExitFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &Diagnose{HyperThreading: "default"}
			if got := d.runConfigureInstanceOTE(context.Background(), &flag.FlagSet{}, test.opts); got != test.wantStatus {
				t.Errorf("Execute(%v) returned status: %v, want status: %v", test.opts, got, test.wantStatus)
			}
		})
	}

}

func TestPerformDiagnosticsOps(t *testing.T) {
	tests := []struct {
		name    string
		d       *Diagnose
		flagSet *flag.FlagSet
		opts    *options
		wantCnt int
	}{
		{
			name: "ErrorInConfigureInstanceWithScopeIO",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "io",
			},
			flagSet: &flag.FlagSet{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
					MkDirErr:   []error{fmt.Errorf("error")},
				},
			},
			wantCnt: 2,
		},
		{
			name: "ErrorInConfigureInstanceWithScopeBackup",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup",
			},
			flagSet: &flag.FlagSet{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
					MkDirErr:   []error{fmt.Errorf("error")},
				},
			},
			wantCnt: 2,
		},
		{
			name: "ErrorInConfigureInstanceWithScopeAll",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "all",
			},
			flagSet: &flag.FlagSet{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
					MkDirErr:   []error{fmt.Errorf("error"), fmt.Errorf("error")},
				},
			},
			wantCnt: 3,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := performDiagnosticsOps(ctx, tc.d, tc.flagSet, tc.opts)
			if len(got) != tc.wantCnt {
				t.Errorf("performDiagnosticsOps(%v, %v, %v) returned an unexpected (-want +got): %v, %v", tc.d, tc.flagSet, tc.opts, len(got), tc.wantCnt)
			}
		})
	}
}

func TestBackup(t *testing.T) {
	tests := []struct {
		name    string
		opts    *options
		d       *Diagnose
		wantCnt int
	}{
		{
			name: "ErrorWhileUnmarshallingBackintParamFile",
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:      []error{nil},
					OpenResp:     []*os.File{&os.File{}},
					CopyResp:     []int64{0},
					CopyErr:      []error{nil},
					CreateResp:   []*os.File{&os.File{}},
					CreateErr:    []error{nil},
					ReadFileResp: [][]byte{[]byte("sample")},
					ReadFileErr:  []error{fmt.Errorf("error")},
					MkDirErr:     []error{fmt.Errorf("error")},
				},
			},
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup",
				ParamFile:      "sampleFile",
			},
			wantCnt: 1,
		},
		{
			name: "MarshaledSuccessfully",
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:      []error{nil},
					OpenResp:     []*os.File{&os.File{}},
					CopyResp:     []int64{0},
					CopyErr:      []error{nil},
					CreateResp:   []*os.File{&os.File{}},
					CreateErr:    []error{nil},
					ReadFileResp: [][]byte{[]byte(defaultBackintFileJSON)},
					ReadFileErr:  []error{nil},
					MkDirErr:     []error{fmt.Errorf("error")},
				},
			},
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup",
				ParamFile:      "sampleFile",
			},
			wantCnt: 1,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.backup(ctx, tc.opts)
			if len(got) != tc.wantCnt {
				t.Errorf("backup(%v) returned an unexpected number of files: %v, want: %v", tc.opts, len(got), tc.wantCnt)
			}
		})
	}
}

func TestDiagnosticsHandler(t *testing.T) {
	tests := []struct {
		name    string
		d       *Diagnose
		flagSet *flag.FlagSet
		opts    *options
		want    subcommands.ExitStatus
	}{
		{
			name: "InvalidParams",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup,disk",
				ParamFile:      "sampleFile",
			},
			flagSet: &flag.FlagSet{
				Usage: func() { return },
			},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
					MkDirErr:   []error{fmt.Errorf("error")},
				},
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "MkDirError",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "io",
				ParamFile:      "sampleFile",
				TestBucket:     "sample",
			},
			flagSet: &flag.FlagSet{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					MkDirErr:   []error{fmt.Errorf("error")},
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "LocalBundleCollection",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "io",
				ParamFile:      "sampleFile",
				TestBucket:     "sample",
			},
			flagSet: &flag.FlagSet{},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:               []error{nil, nil, nil, nil},
					OpenResp:              []*os.File{&os.File{}, &os.File{}, &os.File{}, &os.File{}},
					OpenFileResp:          []*os.File{&os.File{}, &os.File{}, &os.File{}, &os.File{}},
					OpenFileErr:           []error{nil, nil, nil, nil},
					CopyResp:              []int64{0, 0, 0, 0},
					CopyErr:               []error{nil, nil, nil, nil},
					CreateResp:            []*os.File{&os.File{}, &os.File{}, &os.File{}, &os.File{}},
					CreateErr:             []error{nil, nil, nil, nil},
					MkDirErr:              []error{nil, nil},
					WalkAndZipErr:         []error{fmt.Errorf("error")},
					WriteStringToFileResp: []int{0, 0, 0, 0},
					WriteStringToFileErr:  []error{nil, nil, nil, nil},
				},
				z: mockedZipper{},
			},
			want: subcommands.ExitFailure,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := tc.d.diagnosticsHandler(ctx, tc.flagSet, tc.opts)
			if got != tc.want {
				t.Errorf("diagnosticsHandler(%v, %v) returned an unexpected exit status: %v, want: %v", tc.flagSet, tc.opts, got, tc.want)
			}
		})
	}
}

func TestRunBackint(t *testing.T) {
	tests := []struct {
		name    string
		d       *Diagnose
		opts    *options
		wantErr error
	}{
		{
			name: "SetBucketError",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup",
			},
			opts: &options{
				exec: fakeExecForSuccess,
				fs: &fake.FileSystem{
					OpenErr:    []error{nil},
					OpenResp:   []*os.File{&os.File{}},
					CopyResp:   []int64{0},
					CopyErr:    []error{nil},
					CreateResp: []*os.File{&os.File{}},
					CreateErr:  []error{nil},
					MkDirErr:   []error{fmt.Errorf("error")},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "RunBackInt",
			d: &Diagnose{
				HyperThreading: "default",
				Scope:          "backup",
				ParamFile:      "sampleFile",
				TestBucket:     "sample",
			},
			opts: &options{
				exec:   fakeExecForSuccess,
				config: &bpb.BackintConfiguration{Bucket: "sample"},
				fs: &fake.FileSystem{
					OpenErr:    []error{nil, nil},
					OpenResp:   []*os.File{&os.File{}, &os.File{}},
					CopyResp:   []int64{0, 0},
					CopyErr:    []error{nil, nil},
					CreateResp: []*os.File{&os.File{}, &os.File{}},
					CreateErr:  []error{nil, nil},
				},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Diagnose
			gotErr := d.runBackint(ctx, tc.opts)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("runBackint(%v) returned error: %v, want error: %v", tc.opts, gotErr, tc.wantErr)
			}
		})
	}
}
