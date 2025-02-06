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

package inquire

import (
	"bytes"
	"context"
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
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/parse"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"
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
				Name:    "test@TST/object.txt/12345.bak",
				Created: time.UnixMilli(12345),
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       `test@TST/object'with"special\characters.txt/12345.bak`,
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket-folder-prefix",
				Name:       `folder/prefix/test@TST/object.txt/12345.bak`,
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket-shorten-path",
				Name:       `test@TST/tenant_db/object.txt/12345.bak`,
			},
			Content: []byte("test content"),
		},
	})
	defaultBucketHandle      = fakeServer.Client().Bucket("test-bucket")
	defaultConnectParameters = &storage.ConnectParameters{
		StorageClient: func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
			return fakeServer.Client(), nil
		},
		BucketName: "test-bucket",
	}
	defaultConfig = &bpb.BackintConfiguration{UserId: "test@TST"}
	fakeFile      = func() *os.File {
		f, _ := os.Open("fake-file.txt")
		return f
	}
)

func TestInquire(t *testing.T) {
	tests := []struct {
		name      string
		input     io.Reader
		wantError error
		want      string
	}{
		{
			name:      "ScannerError",
			input:     fakeFile(),
			wantError: cmpopts.AnyError,
		},
		{
			name:      "MalformedSoftwareID",
			input:     bytes.NewBufferString("#SOFTWAREID"),
			wantError: cmpopts.AnyError,
		},
		{
			name:      "FormattedSoftwareID",
			input:     bytes.NewBufferString(`#SOFTWAREID "backint 1.50"`),
			want:      fmt.Sprintf(`#SOFTWAREID "backint 1.50" "Google %s %s"`+"\n", configuration.AgentName, configuration.AgentVersion),
			wantError: nil,
		},
		// There are 2 objects in the fake bucket with the same user ID prefix.
		// This test case will return just 1 (object.txt).
		{
			name:      "AllObjectsWithPrefixWithTimestamp",
			input:     bytes.NewBufferString(`#SOFTWAREID "backint 1.50"` + "\n" + `#NULL "/object.txt"`),
			want:      fmt.Sprintf(`#SOFTWAREID "backint 1.50" "Google %s %s"`+"\n"+`#BACKUP "12345" "/object.txt" %q`+"\n", configuration.AgentName, configuration.AgentVersion, time.UnixMilli(12345).Format(parse.BackintRFC3339Millis)),
			wantError: nil,
		},
		{
			name:      "AllObjectsNoTimestamps",
			input:     bytes.NewBufferString(`#NULL`),
			want:      fmt.Sprintf(`#BACKUP "12345" "/object'with\"special\characters.txt"` + "\n" + `#BACKUP "12345" "/object.txt"` + "\n"),
			wantError: nil,
		},
		{
			name:      "MalformedExternalBackupID",
			input:     bytes.NewBufferString("#EBID"),
			wantError: cmpopts.AnyError,
		},
		{
			name:      "FormattedExternalBackupIDNoTimestamp",
			input:     bytes.NewBufferString(`#EBID "12345" "/object'with\"special\characters.txt"`),
			want:      fmt.Sprintf(`#BACKUP "12345" "/object'with\"special\characters.txt"` + "\n"),
			wantError: nil,
		},
		{
			name:      "FormattedExternalBackupIDNoFileName",
			input:     bytes.NewBufferString(`#EBID "12345"`),
			want:      fmt.Sprintf(`#BACKUP "12345" "/object'with\"special\characters.txt"` + "\n" + `#BACKUP "12345" "/object.txt"` + "\n"),
			wantError: nil,
		},
		{
			name:      "EmptyInput",
			input:     bytes.NewBufferString(""),
			wantError: nil,
		},
		{
			name:      "NoSpecifiedPrefix",
			input:     bytes.NewBufferString("#TEST"),
			wantError: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := bytes.NewBufferString("")
			got := inquire(context.Background(), defaultConfig, defaultConnectParameters, test.input, output)
			if output.String() != test.want {
				t.Errorf("inquire() = %s, want: %s", output.String(), test.want)
			}
			if !cmp.Equal(got, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("inquire() = %v, want: %v", got, test.wantError)
			}
		})
	}
}

func TestInquireFiles(t *testing.T) {
	tests := []struct {
		name             string
		bucket           *s.BucketHandle
		prefix           string
		fileName         string
		externalBackupID string
		backintVersion   string
		filter           string
		config           *bpb.BackintConfiguration
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
			name:           "ObjectFoundNoErrorVersion1.00",
			bucket:         defaultBucketHandle,
			prefix:         "test@TST/object.txt/12345.bak",
			backintVersion: "1.00",
			wantPrefix:     "#BACKUP",
		},
		{
			name:           "ObjectFoundNoErrorVersion1.50",
			bucket:         defaultBucketHandle,
			prefix:         "test@TST/object.txt/12345.bak",
			backintVersion: "1.50",
			wantPrefix:     "#BACKUP",
		},
		{
			name:           "FolderPrefix",
			bucket:         fakeServer.Client().Bucket("test-bucket-folder-prefix"),
			prefix:         "folder/prefix/test@TST/object.txt/12345.bak",
			backintVersion: "1.50",
			wantPrefix:     "#BACKUP",
			config: &bpb.BackintConfiguration{
				UserId:       "test@TST",
				FolderPrefix: "/folder/prefix",
			},
		},
		{
			name:             "ShortenPath",
			bucket:           fakeServer.Client().Bucket("test-bucket-shorten-path"),
			prefix:           "test@TST/tenant_db/object.txt/12345.bak",
			fileName:         "usr/sap/test@TST/SYS/global/hdb/backint/tenant_db/object.txt",
			externalBackupID: "12345",
			backintVersion:   "1.50",
			wantPrefix:       `#BACKUP "12345" "/usr/sap/test@TST/SYS/global/hdb/backint/tenant_db/object.txt"`,
			config: &bpb.BackintConfiguration{
				UserId:            "test@TST",
				ShortenFolderPath: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := inquireFiles(context.Background(), test.bucket, test.prefix, test.fileName, test.externalBackupID, test.backintVersion, test.filter, test.config)
			if !strings.HasPrefix(string(got), test.wantPrefix) {
				t.Errorf("inquireFiles(%s, %s, %s) = %s, wantPrefix: %s", test.prefix, test.fileName, test.externalBackupID, got, test.wantPrefix)
			}
		})
	}
}
