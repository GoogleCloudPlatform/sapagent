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

package delete

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

func TestDelete(t *testing.T) {
	tests := []struct {
		name       string
		input      io.Reader
		bucket     *storage.BucketHandle
		want       error
		wantPrefix string
	}{
		{
			name:       "NoBucket",
			input:      bytes.NewBufferString("#EBID 12345 /object.txt"),
			bucket:     nil,
			wantPrefix: "#ERROR",
			want:       nil,
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
		{
			name:   "NoSpecifiedPrefix",
			input:  bytes.NewBufferString("#TEST"),
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
