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

package workloadmanager

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestSetOSReleaseInfo(t *testing.T) {
	defaultFileReader := ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := testFS.Open(path)
		var f io.ReadCloser = file
		return f, err
	})

	tests := []struct {
		name        string
		filePath    string
		reader      ConfigFileReader
		wantID      string
		wantVersion string
	}{
		{
			name:        "Success",
			filePath:    "test_data/os-release.txt",
			reader:      defaultFileReader,
			wantID:      "debian",
			wantVersion: "11",
		},
		{
			name:        "ConfigFileReaderNil",
			filePath:    "test_data/os-release.txt",
			reader:      nil,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "OSReleaseFilePathEmpty",
			filePath:    "",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:     "FileReadError",
			filePath: "test_data/os-release.txt",
			reader: ConfigFileReader(func(path string) (io.ReadCloser, error) {
				return nil, errors.New("File Read Error")
			}),
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "FileParseError",
			filePath:    "test_data/os-release-bad.txt",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "FieldsEmpty",
			filePath:    "test_data/os-release-empty.txt",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotOSVendorID, gotOSVersion := setOSReleaseInfo(context.Background(), test.reader, test.filePath)
			if gotOSVendorID != test.wantID {
				t.Errorf("SetOSReleaseInfo() unexpected osVendorID, got %q want %q", gotOSVendorID, test.wantID)
			}
			if gotOSVersion != test.wantVersion {
				t.Errorf("SetOSReleaseInfo() unexpected osVersion, got %q want %q", gotOSVersion, test.wantVersion)
			}
		})
	}
}

func TestReadHANAInsightsRules(t *testing.T) {
	gotRules := readHANAInsightsRules()
	if len(gotRules) != 20 {
		t.Errorf("ReadHANAInsightsRules() got: %d rules, want: %d.", len(gotRules), 20)
	}
}
