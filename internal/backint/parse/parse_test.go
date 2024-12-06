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

package parse

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
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
			s:    `"\"Hello \"World"`,
			want: []string{`"\"Hello \"World"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Split(test.s)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("split(%v) had unexpected diff (-want +got):\n%s", test.s, diff)
			}
		})
	}
}

func TestWriteSoftwareVersion(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    string
		wantErr error
	}{
		{
			name:    "EmptyString",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseVersion",
			line: `#SOFTWAREID "backint 1.50" "hdb nameserver 2.00.50"`,
			want: "1.50",
		},
	}
	for _, tc := range tests {
		output := bytes.NewBufferString("")
		got, err := WriteSoftwareVersion(tc.line, output)
		if cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()) != "" {
			t.Errorf("WriteSoftwareVersion(%v) = %v, wantError: %v", tc.line, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("WriteSoftwareVersion(%v) = %v, want: %v", tc.line, got, tc.want)
		}
	}
}

func TestTrimAndClean(t *testing.T) {
	tests := []struct {
		name string
		str  string
		want string
	}{
		{
			name: "EmptyString",
		},
		{
			name: "TrimQuotes",
			str:  `"Hello World"`,
			want: `Hello World`,
		},
		{
			name: "EmbeddedQuotesAndBackslashes",
			str:  `Hello\" W\orld`,
			want: `Hello" W\orld`,
		},
	}

	for _, tc := range tests {
		got := TrimAndClean(tc.str)
		if got != tc.want {
			t.Errorf("TrimAndClean(%v) = %v, want: %v", tc.str, got, tc.want)
		}
	}
}

func TestRestoreFilename(t *testing.T) {
	tests := []struct {
		name string
		str  string
		want string
	}{
		{
			name: "EmptyString",
			want: `"/"`,
		},
		{
			name: "RegularFileName",
			str:  `tmp/test.txt`,
			want: `"/tmp/test.txt"`,
		},
		{
			name: "EmbeddedQuotesAndBackslashes",
			str:  `tmp/test".\txt`,
			want: `"/tmp/test\".\txt"`,
		},
	}

	for _, tc := range tests {
		got := RestoreFilename(tc.str)
		if got != tc.want {
			t.Errorf("RestoreFilename(%v) = %v, want: %v", tc.str, got, tc.want)
		}
	}
}

func TestCreateObjectPath(t *testing.T) {
	tests := []struct {
		name             string
		config           *bpb.BackintConfiguration
		fileNameTrim     string
		externalBackupID string
		extension        string
		want             string
	}{
		{
			name: "EmptyString",
			want: "/",
		},
		{
			name: "FolderPrefixAndUserIDOnly",
			config: &bpb.BackintConfiguration{
				FolderPrefix:      "folder_prefix/",
				UserId:            "user_id",
				ShortenFolderPath: true,
			},
			want: "folder_prefix/user_id/",
		},
		{
			name: "FullFilePath",
			config: &bpb.BackintConfiguration{
				FolderPrefix:      "folder_prefix/",
				UserId:            "user_id",
				ShortenFolderPath: false,
			},
			fileNameTrim:     "/this/is/a/long/file/path",
			externalBackupID: "12345",
			extension:        ".bak",
			want:             "folder_prefix/user_id/this/is/a/long/file/path/12345.bak",
		},
		{
			name: "ShortenFilePathHANA",
			config: &bpb.BackintConfiguration{
				FolderPrefix:      "folder_prefix/",
				UserId:            "user_id",
				ShortenFolderPath: true,
			},
			fileNameTrim:     "/usr/sap/user_id/SYS/global/hdb/backint/tenant_db/basename",
			externalBackupID: "12345",
			extension:        ".bak",
			want:             "folder_prefix/user_id/tenant_db/basename/12345.bak",
		},
		{
			name: "ShortenFilePathNoHANA",
			config: &bpb.BackintConfiguration{
				FolderPrefix:      "folder_prefix/",
				UserId:            "user_id",
				ShortenFolderPath: true,
			},
			fileNameTrim:     "/this/is/a/long/file/path",
			externalBackupID: "12345",
			extension:        ".bak",
			want:             "folder_prefix/user_id/this/is/a/long/file/path/12345.bak",
		},
	}

	for _, tc := range tests {
		got := CreateObjectPath(tc.config, tc.fileNameTrim, tc.externalBackupID, tc.extension)
		if got != tc.want {
			t.Errorf("CreateObjectPath(%v, %v, %v, %v) = %v, want: %v", tc.config, tc.fileNameTrim, tc.externalBackupID, tc.extension, got, tc.want)
		}
	}
}

func TestOpenFileWithRetries(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		createFile bool
		wantError  error
	}{
		{
			name:       "FileFound",
			fileName:   t.TempDir() + "/found.txt",
			createFile: true,
			wantError:  nil,
		},
		{
			name:       "FileNotFound",
			fileName:   t.TempDir() + "/not_found.txt",
			createFile: false,
			wantError:  cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		if tc.createFile {
			f, err := os.Create(tc.fileName)
			if err != nil {
				t.Errorf("os.Create(%v) failed: %v", tc.fileName, err)
			}
			defer f.Close()
		}

		_, err := OpenFileWithRetries(tc.fileName, os.O_RDONLY, 0, 100)
		if cmp.Diff(err, tc.wantError, cmpopts.EquateErrors()) != "" {
			t.Errorf("OpenFileWithRetries(%v) = %v, wantError: %v", tc.fileName, err, tc.wantError)
		}
	}
}

func TestCustomTime(t *testing.T) {
	testCases := []struct {
		name       string
		customTime string
		now        time.Time
		want       time.Time
	}{
		{
			name:       "Empty CustomTime",
			customTime: "",
			now:        time.Now(),
			want:       time.Time{},
		},
		{
			name:       "UTCNow",
			customTime: "UTCNow",
			now:        time.Date(2024, 8, 13, 13, 8, 0, 0, time.UTC),
			want:       time.Date(2024, 8, 13, 13, 8, 0, 0, time.UTC),
		},
		{
			name:       "UTCNow+NNd Format",
			customTime: "UTCNow+3d",
			now:        time.Date(2024, 8, 13, 13, 8, 0, 0, time.UTC),
			want:       time.Date(2024, 8, 16, 13, 8, 0, 0, time.UTC),
		},
		{
			name:       "RFC3339 Format",
			customTime: "2024-08-20T10:30:00Z",
			now:        time.Now(),
			want:       time.Date(2024, 8, 20, 10, 30, 0, 0, time.UTC),
		},
		{
			name:       "Invalid UTCNow+NNd Format (missing 'd')",
			customTime: "UTCNow+3",
			now:        time.Now(),
			want:       time.Time{},
		},
		{
			name:       "Invalid UTCNow+NNd Format (non-numeric days)",
			customTime: "UTCNow+threed",
			now:        time.Now(),
			want:       time.Time{},
		},
		{
			name:       "Invalid RFC3339 Format",
			customTime: "2024-08-20 10:30:00", // Missing 'T' and 'Z'
			now:        time.Now(),
			want:       time.Time{},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			got := CustomTime(context.Background(), test.customTime, test.now)
			if !got.Equal(test.want) {
				t.Errorf("customTime(%v, %v) = %v, want %v", test.customTime, test.now, got, test.want)
			}
		})
	}
}
