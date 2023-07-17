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

package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/gammazero/workerpool"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
)

var (
	defaultBucketHandle = fakeServer("test-bucket").Client().Bucket("test-bucket")
	fakeFile            = func() *os.File {
		f, _ := os.Open("fake-file.txt")
		return f
	}
	defaultParameters = parameters{
		bucketHandle: defaultBucketHandle,
		fileName:     "/object.txt",
		fileType:     "#FILE",
		config: &bpb.BackintConfiguration{
			UserId:          "test@TST",
			ParallelStreams: 2,
		},
		extension:        ".bak",
		externalBackupID: "12345",
		output:           bytes.NewBufferString(""),
		wp:               workerpool.New(2),
		mu:               &sync.Mutex{},
		stat:             os.Stat,
	}
)

// fakeServer creates a new server with objects in the bucketName.
// Deleting objects from the same bucket can cause tests to fail when
// run in parallel. Ensure deletions will happen in a confined
// environment by providing separate bucketNames.
func fakeServer(bucketName string) *fakestorage.Server {
	return fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				Name:       "object.txt",
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				// The backup object name is in the format <userID>/<fileName>/<externalBackupID>.bak
				Name: "test@TST/object.txt/12345.bak",
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				// Chunk objects have a suffix for the chunk ID
				Name: "test@TST/object.txt/12345.bak0",
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: bucketName,
				// Chunk objects have a suffix for the chunk ID
				Name: "test@TST/object.txt/12345.bak1",
			},
			Content: []byte("test content"),
		},
	})
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
			name:  "FormattedFileNoSize",
			input: bytes.NewBufferString("#FILE /test.txt"),
			want:  nil,
		},
		{
			name:  "EmptyInput",
			input: bytes.NewBufferString(""),
			want:  nil,
		},
		{
			name:  "NoSpecifiedPrefix",
			input: bytes.NewBufferString("#TEST"),
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

func TestBackupParallelSuccess(t *testing.T) {
	input := bytes.NewBufferString("#FILE /object.txt 12345")
	config := &bpb.BackintConfiguration{ParallelStreams: 2}
	got := backup(context.Background(), config, defaultBucketHandle, input, bytes.NewBufferString(""))
	if got != nil {
		t.Errorf("backup() = %v, want <nil>", got)
	}
}

func TestBackupFile(t *testing.T) {
	tests := []struct {
		name string
		p    parameters
		want string
	}{
		{
			name: "ErrorOpeningFile",
			want: "#ERROR",
		},
		{
			name: "UploadError",
			p: parameters{
				fileName: t.TempDir() + "/UploadError.txt",
				copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
					return 0, errors.New("copy error")
				},
			},
			want: "#ERROR",
		},
		{
			name: "UploadSuccess",
			p: parameters{
				bucketHandle: defaultBucketHandle,
				fileName:     t.TempDir() + "/UploadSuccess.txt",
				copier:       io.Copy,
			},
			want: "#SAVED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.p.fileName != "" {
				f, err := os.Create(test.p.fileName)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.p.fileName, err)
				}
				defer f.Close()
			}

			got := backupFile(context.Background(), test.p)
			if !strings.HasPrefix(got, test.want) {
				t.Errorf("backupFile(%s) = %v, want prefix %v", test.p.fileName, got, test.want)
			}
		})
	}
}

func TestBackupFileParallel(t *testing.T) {
	tests := []struct {
		name      string
		p         parameters
		fileName  string
		want      string
		wantError error
	}{
		{
			name:      "ErrorOpeningFile",
			p:         parameters{wp: workerpool.New(1)},
			want:      "#ERROR",
			wantError: cmpopts.AnyError,
		},
		{
			name: "ErrorStatingFile",
			p: parameters{
				fileSize: 0,
				fileName: t.TempDir() + "/object.txt",
				stat:     func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist },
				wp:       workerpool.New(1),
			},
			want:      "#ERROR",
			wantError: cmpopts.AnyError,
		},
		{
			name: "ErrorChunkUpload",
			p: parameters{
				bucketHandle: fakeServer("parallel-failed").Client().Bucket("parallel-failed"),
				fileName:     t.TempDir() + "/object.txt",
				config:       &bpb.BackintConfiguration{ParallelStreams: 2},
				wp:           workerpool.New(2),
				mu:           &sync.Mutex{},
				output:       bytes.NewBufferString(""),
				fileSize:     12345,
				copier:       func(dst io.Writer, src io.Reader) (written int64, err error) { return 0, errors.New("copy error") },
			},
		},
		{
			name: "Success",
			p: parameters{
				bucketHandle: fakeServer("parallel-success").Client().Bucket("parallel-success"),
				fileName:     t.TempDir() + "/object.txt",
				config:       &bpb.BackintConfiguration{ParallelStreams: 2},
				wp:           workerpool.New(2),
				mu:           &sync.Mutex{},
				output:       bytes.NewBufferString(""),
				stat:         os.Stat,
				copier:       io.Copy,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.p.fileName != "" {
				f, err := os.Create(test.p.fileName)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.p.fileName, err)
				}
				defer f.Close()
			}

			got, gotError := backupFileParallel(context.Background(), test.p)
			test.p.wp.StopWait()
			if !strings.HasPrefix(got, test.want) {
				t.Errorf("backupFileParallel(%s) = %v, want prefix %v", test.p.fileName, got, test.want)
			}
			if !cmp.Equal(gotError, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("backupFileParallel(%s) = %v, want %v", test.p.fileName, gotError, test.wantError)
			}
		})
	}
}

func TestComposeChunks(t *testing.T) {
	tests := []struct {
		name       string
		p          parameters
		bucketName string
		chunkError bool
		want       string
	}{
		{
			name: "FailNoChunks",
			p:    parameters{bucketHandle: defaultBucketHandle},
			want: "#ERROR",
		},
		{
			name: "FailComposingAndDeleting",
			p: parameters{
				bucketHandle: defaultBucketHandle,
				config:       &bpb.BackintConfiguration{ParallelStreams: 2},
			},
			want: "#ERROR",
		},
		{
			name:       "ComposeSuccessWithPreviousError",
			p:          defaultParameters,
			bucketName: "previous-error",
			chunkError: true,
			want:       "#ERROR",
		},
		{
			name:       "ComposeSuccess",
			p:          defaultParameters,
			bucketName: "success",
			want:       "#SAVED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.bucketName != "" {
				test.p.bucketHandle = fakeServer(test.bucketName).Client().Bucket(test.bucketName)
			}

			got := composeChunks(context.Background(), test.p, test.chunkError)
			if !strings.HasPrefix(got, test.want) {
				t.Errorf("composeChunks(%#v, %v) = %v, want prefix %v", test.p, test.chunkError, got, test.want)
			}
		})
	}
}
