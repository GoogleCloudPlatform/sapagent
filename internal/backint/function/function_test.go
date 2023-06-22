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

package function

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/protobuf/testing/protocmp"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
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
	defaultBucketHandle = fakeServer.Client().Bucket("test-bucket")
	defaultConfig       = &bpb.BackintConfiguration{UserId: "test@TST"}
	fakeFile            = func() *os.File {
		f, _ := os.Open("fake-file.txt")
		return f
	}
)

func TestExecute(t *testing.T) {
	tests := []struct {
		name   string
		config *bpb.BackintConfiguration
		bucket *storage.BucketHandle
		input  string
		want   bool
	}{
		{
			name: "ErrorOpeningInputFile",
			want: false,
		},
		{
			name: "ErrorOpeningOuputFile",
			config: &bpb.BackintConfiguration{
				InputFile: t.TempDir() + "/input.txt",
			},
			want: false,
		},
		{
			name: "UnspecifiedFunction",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_FUNCTION_UNSPECIFIED,
			},
			want: false,
		},
		{
			name: "BackupFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_BACKUP,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "BackupSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_BACKUP,
			},
			input: `#SOFTWAREID "backint 1.50"`,
			want:  true,
		},
		{
			name: "InquireFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_INQUIRE,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "InquireSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_INQUIRE,
			},
			input: `#SOFTWAREID "backint 1.50"`,
			want:  true,
		},
		{
			name: "DeleteFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_DELETE,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "DeleteSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_DELETE,
			},
			bucket: defaultBucketHandle,
			input:  `#SOFTWAREID "backint 1.50"`,
			want:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.config.GetInputFile() != "" {
				f, err := os.Create(test.config.GetInputFile())
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.config.GetInputFile(), err)
				}
				f.WriteString(test.input)
				defer f.Close()
			}

			got := Execute(context.Background(), test.config, test.bucket)
			if got != test.want {
				t.Errorf("Execute(%#v) = %v, want %v", test.config, got, test.want)
			}
		})
	}
}

func TestBackup(t *testing.T) {
	tests := []struct {
		name  string
		input io.Reader
		want  error
	}{
		{
			name:  "ScannerError",
			input: fakeFile(),
			want:  cmpopts.AnyError,
		},
		{
			name:  "MalformedSoftwareID",
			input: bytes.NewBufferString("#SOFTWAREID"),
			want:  cmpopts.AnyError,
		},
		{
			name:  "FormattedSoftwareID",
			input: bytes.NewBufferString(`#SOFTWAREID "backint 1.50"`),
			want:  nil,
		},
		{
			name:  "MalformedPipe",
			input: bytes.NewBufferString("#PIPE"),
			want:  cmpopts.AnyError,
		},
		{
			name:  "FormattedPipe",
			input: bytes.NewBufferString("#PIPE /test.txt 12345"),
			want:  nil,
		},
		{
			name:  "MalformedFile",
			input: bytes.NewBufferString("#FILE"),
			want:  cmpopts.AnyError,
		},
		{
			name:  "FormattedFile",
			input: bytes.NewBufferString("#FILE /test.txt 12345"),
			want:  nil,
		},
		{
			name:  "EmptyInput",
			input: bytes.NewBufferString(""),
			want:  nil,
		},
		{
			name:  "FileSizeConversionError",
			input: bytes.NewBufferString("#PIPE /test.txt 123.45"),
			want:  cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := backup(context.Background(), nil, defaultBucketHandle, test.input, bytes.NewBufferString(""))
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("backup() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBackupFile(t *testing.T) {
	tests := []struct {
		name     string
		bucket   *storage.BucketHandle
		fileType string
		fileName string
		fileSize int64
		want     string
	}{
		{
			name: "ErrorOpeningFile",
			want: "#ERROR",
		},
		{
			name:     "UploadError",
			fileName: t.TempDir() + "/UploadError.txt",
			want:     "#ERROR",
		},
		{
			name:     "UploadSuccess",
			fileName: t.TempDir() + "/UploadSuccess.txt",
			bucket:   defaultBucketHandle,
			want:     "#SAVED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fileName != "" {
				f, err := os.Create(test.fileName)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.fileName, err)
				}
				defer f.Close()
			}

			got := backupFile(context.Background(), nil, test.bucket, test.fileType, test.fileName, test.fileSize)
			if !strings.HasPrefix(got, test.want) {
				t.Errorf("backupFile(%s) = %v, want prefix %v", test.fileName, got, test.want)
			}
		})
	}
}

func TestInquire(t *testing.T) {
	tests := []struct {
		name       string
		input      io.Reader
		want       error
		wantPrefix string
	}{
		{
			name:  "ScannerError",
			input: fakeFile(),
			want:  cmpopts.AnyError,
		},
		{
			name:  "MalformedSoftwareID",
			input: bytes.NewBufferString("#SOFTWAREID"),
			want:  cmpopts.AnyError,
		},
		{
			name:       "FormattedSoftwareID",
			input:      bytes.NewBufferString(`#SOFTWAREID "backint 1.50"`),
			wantPrefix: "#SOFTWAREID",
			want:       nil,
		},
		{
			name:       "AllObjectsWithPrefix",
			input:      bytes.NewBufferString(`#NULL "/object.txt"`),
			wantPrefix: "#BACKUP",
			want:       nil,
		},
		{
			name:  "MalformedExternalBackupID",
			input: bytes.NewBufferString("#EBID"),
			want:  cmpopts.AnyError,
		},
		{
			name:       "FormattedExternalBackupID",
			input:      bytes.NewBufferString(`#EBID "12345" "/object.txt"`),
			wantPrefix: "#BACKUP",
			want:       nil,
		},
		{
			name:  "EmptyInput",
			input: bytes.NewBufferString(""),
			want:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := bytes.NewBufferString("")
			got := inquire(context.Background(), defaultConfig, defaultBucketHandle, test.input, output)
			if !strings.HasPrefix(output.String(), test.wantPrefix) {
				t.Errorf("inquire() = %s, wantPrefix: %s", output.String(), test.wantPrefix)
			}
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("inquire() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestInquireFiles(t *testing.T) {
	tests := []struct {
		name             string
		bucket           *storage.BucketHandle
		prefix           string
		fileName         string
		externalBackupID string
		wantPrefix       string
	}{
		{
			name:       "NoBucketNoParameters",
			wantPrefix: "#ERROR",
		},
		{
			name:             "NoBucketWithParameters",
			fileName:         "/test.txt",
			externalBackupID: "12345",
			wantPrefix:       "#ERROR",
		},
		{
			name:       "NoObjectsFound",
			bucket:     defaultBucketHandle,
			prefix:     "fake-object.txt",
			wantPrefix: "#NOTFOUND",
		},
		{
			name:       "ObjectFoundBadObjectName",
			bucket:     defaultBucketHandle,
			prefix:     "object.txt",
			wantPrefix: "#ERROR",
		},
		{
			name:       "ObjectFoundNoError",
			bucket:     defaultBucketHandle,
			prefix:     "test@TST/object.txt/12345.bak",
			wantPrefix: "#BACKUP",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := inquireFiles(context.Background(), test.bucket, test.prefix, test.fileName, test.externalBackupID)
			if !strings.HasPrefix(string(got), test.wantPrefix) {
				t.Errorf("inquireFiles(%s, %s, %s) = %s, wantPrefix: %s", test.prefix, test.fileName, test.externalBackupID, got, test.wantPrefix)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name       string
		input      io.Reader
		bucket     *storage.BucketHandle
		want       error
		wantPrefix string
	}{
		{
			name:   "NoBucket",
			bucket: nil,
			want:   cmpopts.AnyError,
		},
		{
			name:   "ScannerError",
			bucket: defaultBucketHandle,
			input:  fakeFile(),
			want:   cmpopts.AnyError,
		},
		{
			name:   "MalformedSoftwareID",
			input:  bytes.NewBufferString("#SOFTWAREID"),
			bucket: defaultBucketHandle,
			want:   cmpopts.AnyError,
		},
		{
			name:       "FormattedSoftwareID",
			input:      bytes.NewBufferString(`#SOFTWAREID "backint 1.50"`),
			bucket:     defaultBucketHandle,
			wantPrefix: "#SOFTWAREID",
			want:       nil,
		},
		{
			name:   "MalformedExternalBackupID",
			input:  bytes.NewBufferString("#EBID"),
			bucket: defaultBucketHandle,
			want:   cmpopts.AnyError,
		},
		{
			name:       "ObjectNotFound",
			input:      bytes.NewBufferString("#EBID 12345 /fake-object.txt"),
			bucket:     defaultBucketHandle,
			wantPrefix: "#NOTFOUND",
			want:       nil,
		},
		{
			name:       "ObjectDeleted",
			input:      bytes.NewBufferString("#EBID 12345 /object.txt"),
			bucket:     defaultBucketHandle,
			wantPrefix: "#DELETED",
			want:       nil,
		},
		{
			name:   "EmptyInput",
			input:  bytes.NewBufferString(""),
			bucket: defaultBucketHandle,
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := bytes.NewBufferString("")
			got := delete(context.Background(), defaultConfig, test.bucket, test.input, output)
			if !strings.HasPrefix(output.String(), test.wantPrefix) {
				t.Errorf("delete() = %s, wantPrefix: %s", output.String(), test.wantPrefix)
			}
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("delete() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []string
	}{
		{
			name: "EmptyString",
			s:    "",
			want: []string{""},
		},
		{
			name: "OneParameter",
			s:    "Hello",
			want: []string{"Hello"},
		},
		{
			name: "MultipleParameters",
			s:    "backup restore inquire delete",
			want: []string{"backup", "restore", "inquire", "delete"},
		},
		{
			name: "ParameterWithSpaces",
			s:    `"Hello World"`,
			want: []string{`"Hello World"`},
		},
		{
			name: "ParameterWithDoubleQuote",
			s:    `"\"Hello"`,
			want: []string{`"\"Hello"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := split(test.s)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("split(%v) had unexpected diff (-want +got):\n%s", test.s, diff)
			}
		})
	}
}
