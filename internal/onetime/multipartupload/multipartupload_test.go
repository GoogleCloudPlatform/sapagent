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

package multipartupload

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"flag"
	s "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultServer = fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "object.txt",
			},
			Content: []byte("test content"),
		},
	})
	defaultClient = func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
		return defaultServer.Client(), nil
	}
)

func TestExecuteMultipartUpload(t *testing.T) {
	tests := []struct {
		name string
		m    MultipartUpload
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
			m: MultipartUpload{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "NoSourceFile",
			want: subcommands.ExitUsageError,
			m: MultipartUpload{
				SourceFile: "",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "NoTargetPath",
			want: subcommands.ExitUsageError,
			m: MultipartUpload{
				SourceFile: "test.txt",
				TargetPath: "",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "MissingBucketPrefix",
			want: subcommands.ExitUsageError,
			m: MultipartUpload{
				SourceFile: "test.txt",
				TargetPath: "test-bucket",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "ImproperBucketPath",
			want: subcommands.ExitUsageError,
			m: MultipartUpload{
				SourceFile: "test.txt",
				TargetPath: "gs://test-bucket",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "FailConnectToBucket",
			want: subcommands.ExitFailure,
			m: MultipartUpload{
				SourceFile: "test.txt",
				TargetPath: "gs://test-bucket/object.txt",
				client: func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
					return nil, errors.New("failed to connect to bucket")
				},
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "FailOpenFile",
			want: subcommands.ExitFailure,
			m: MultipartUpload{
				SourceFile: "test.txt",
				TargetPath: "gs://test-bucket/object.txt",
				client:     defaultClient,
				fileOpener: func(string, int, os.FileMode) (*os.File, error) {
					return nil, errors.New("failed to open file")
				},
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.m.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.m, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForMultipartUpload(t *testing.T) {
	want := "upload a file to a GCS bucket using XML Multipart API"
	m := MultipartUpload{}
	got := m.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForMultipartUpload(t *testing.T) {
	m := MultipartUpload{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"source-file", "s", "target-path", "t", "parallel-threads", "p", "h"}
	m.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestMultipartUploadHandler(t *testing.T) {
	tests := []struct {
		name    string
		m       MultipartUpload
		want    subcommands.ExitStatus
		wantErr error
	}{

		{
			name: "FailOpenFile",
			m: MultipartUpload{
				bucketName: "test-bucket",
				objectName: "object.txt",
				client:     defaultClient,
				fileOpener: func(string, int, os.FileMode) (*os.File, error) {
					return nil, errors.New("failed to open file")
				},
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FailUpload",
			m: MultipartUpload{
				bucketName: "test-bucket",
				objectName: "object.txt",
				client:     defaultClient,
				fileOpener: func(string, int, os.FileMode) (*os.File, error) {
					return os.Create(t.TempDir() + "object.txt")
				},
				copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("failed to copy file")
				},
			},
			want:    subcommands.ExitFailure,
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		test.m.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.m.multipartUploadHandler(context.Background())
			if got != test.want {
				t.Errorf("multipartUploadHandler()=%v, want %v", got, test.want)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("multipartUploadHandler()=%v want %v", gotErr, test.wantErr)
			}
		})
	}
}
