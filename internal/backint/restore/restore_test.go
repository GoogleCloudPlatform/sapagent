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

package restore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	s "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
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
				Name:       "test@TST/file-object.txt/12345.bak",
				Metadata:   map[string]string{"X-Backup-Type": "FILE"},
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				// The backup object name is in the format <userID>/<fileName>/<externalBackupID>.bak
				Name:    "test@TST/object.txt/12345.bak",
				Created: time.UnixMilli(12345),
			},
			Content: []byte("test content 1"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "test@TST/object.txt/1234567890.bak",
				Created:    time.UnixMilli(1234567890),
			},
			Content: []byte("test content 2"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "test@TST/object.txt_3/123456789012345.bak",
				Created:    time.UnixMilli(123456789012345),
			},
			Content: []byte("test content 3"),
		},
	})
	defaultBucketHandle      = fakeServer.Client().Bucket("test-bucket")
	defaultConnectParameters = &storage.ConnectParameters{
		StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
			return fakeServer.Client(), nil
		},
		BucketName: "test-bucket",
	}
	defaultConfig = &bpb.BackintConfiguration{UserId: "test@TST", FileReadTimeoutMs: 100}
	fakeFile      = func() *os.File {
		f, _ := os.Open("fake-file.txt")
		return f
	}
)

func TestRestore(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		destName  string
		wantError error
		want      string
	}{
		{
			name:      "MalformedSoftwareID",
			input:     "#SOFTWAREID",
			wantError: cmpopts.AnyError,
		},
		{
			name:      "FormattedSoftwareID",
			input:     `#SOFTWAREID "backint 1.50"`,
			want:      fmt.Sprintf(`#SOFTWAREID "backint 1.50" "Google %s %s"`+"\n", configuration.AgentName, configuration.AgentVersion),
			wantError: nil,
		},
		{
			name:      "MalformedNull",
			input:     "#NULL",
			wantError: cmpopts.AnyError,
		},
		// There are 2 objects in the fake bucket with the same user ID and filename.
		// This test case will restore the latest created.
		{
			name:      "FormattedNull",
			input:     `#NULL "/object.txt"`,
			destName:  t.TempDir() + "/object.txt",
			want:      `#RESTORED "1234567890" "/object.txt"` + "\n",
			wantError: nil,
		},
		// There was a previous bug where both object.txt and object.txt_3 would
		// have been returned in this case, and object.txt_3 would have been used.
		{
			name:      "SimilarFileNameReturned",
			input:     `#NULL "/object.txt"`,
			destName:  t.TempDir() + "/object.txt",
			want:      `#RESTORED "1234567890" "/object.txt"` + "\n",
			wantError: nil,
		},
		{
			name:      "MalformedExternalBackupID",
			input:     "#EBID",
			wantError: cmpopts.AnyError,
		},
		{
			name:      "FormattedExternalBackupID",
			input:     `#EBID "12345" "/object.txt"`,
			destName:  t.TempDir() + "/object.txt",
			want:      `#RESTORED "12345" "/object.txt"` + "\n",
			wantError: nil,
		},
		{
			name:      "EmptyInput",
			input:     "",
			wantError: nil,
		},
		{
			name:      "NoSpecifiedPrefix",
			input:     "#TEST",
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.destName != "" {
				f, err := os.Create(test.destName)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.destName, err)
				}
				defer f.Close()
				// Add the destName to the input so Backint can restore to the correct file.
				test.input += fmt.Sprintf(" %s", test.destName)
			}

			input := bytes.NewBufferString(test.input)
			output := bytes.NewBufferString("")
			got := restore(context.Background(), defaultConfig, defaultConnectParameters, input, output)
			if output.String() != test.want {
				t.Errorf("restore() = %s, want: %s", output.String(), test.want)
			}
			if !cmp.Equal(got, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("restore() = %v, want: %v", got, test.wantError)
			}
		})
	}
}

func TestRestoreScannerError(t *testing.T) {
	want := cmpopts.AnyError
	got := restore(context.Background(), defaultConfig, defaultConnectParameters, fakeFile(), bytes.NewBufferString(""))
	if !cmp.Equal(got, want, cmpopts.EquateErrors()) {
		t.Errorf("restore() = %v, want: %v", got, want)
	}
}

// Pipe types will be created beforehand by the database.
func TestRestoreFileBackupTypePipe(t *testing.T) {
	tests := []struct {
		name             string
		bucket           *s.BucketHandle
		copier           storage.IOFileCopier
		fileName         string
		destName         string
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
			fileName:   "fake-object.txt",
			wantPrefix: "#NOTFOUND",
		},
		{
			name:       "ErrorOpeningDest",
			bucket:     defaultBucketHandle,
			fileName:   "/object.txt",
			wantPrefix: "#ERROR",
		},
		{
			name:   "DownloadFail",
			bucket: defaultBucketHandle,
			copier: func(dst io.Writer, src io.Reader) (written int64, err error) {
				return 0, errors.New("copy error")
			},
			fileName:   "/object.txt",
			destName:   t.TempDir() + "/object.txt",
			wantPrefix: "#ERROR",
		},
		{
			name:       "SuccessfulRestore",
			bucket:     defaultBucketHandle,
			copier:     io.Copy,
			fileName:   "/object.txt",
			destName:   t.TempDir() + "/object.txt",
			wantPrefix: "#RESTORED",
		},
		{
			name:             "SuccessfulRestoreWithEBID",
			bucket:           defaultBucketHandle,
			copier:           io.Copy,
			externalBackupID: "12345",
			fileName:         "/object.txt",
			destName:         t.TempDir() + "/object.txt",
			wantPrefix:       "#RESTORED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.destName != "" {
				f, err := os.Create(test.destName)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.destName, err)
				}
				defer f.Close()
			}

			got := restoreFile(context.Background(), defaultConfig, test.bucket, test.copier, test.fileName, test.destName, test.externalBackupID)
			if !strings.HasPrefix(string(got), test.wantPrefix) {
				t.Errorf("restoreFile(%s, %s) = %s, wantPrefix: %s", test.fileName, test.externalBackupID, got, test.wantPrefix)
			}
		})
	}
}

// File types will be created during execution by the agent.
func TestRestoreFileBackupTypeFile(t *testing.T) {
	externalBackupID := "12345"
	fileName := "/file-object.txt"
	destName := t.TempDir() + "/file-object.txt"
	wantPrefix := "#RESTORED"

	got := restoreFile(context.Background(), defaultConfig, defaultBucketHandle, io.Copy, fileName, destName, externalBackupID)
	if !strings.HasPrefix(string(got), wantPrefix) {
		t.Errorf("restoreFile(%s, %s) = %s, wantPrefix: %s", fileName, externalBackupID, got, wantPrefix)
	}
}
