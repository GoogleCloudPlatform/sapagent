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

package osinfo

import (
	"context"
	"embed"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	//go:embed test_data/os-release.txt test_data/os-release-bad.txt test_data/os-release-empty.txt
	testFS embed.FS
)

func TestReadReleaseInfo(t *testing.T) {
	defaultFileReader := FileReadCloser(func(path string) (io.ReadCloser, error) {
		file, err := testFS.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	tests := []struct {
		name        string
		reader      FileReadCloser
		filePath    string
		wantID      string
		wantVersion string
		wantErr     error
	}{
		{
			name:        "success",
			reader:      defaultFileReader,
			filePath:    "test_data/os-release.txt",
			wantID:      "debian",
			wantVersion: "11",
		},
		{
			name:        "empty file",
			reader:      func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
			filePath:    "test_data/os-release-empty.txt",
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "MalformedFile",
			reader:      func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
			filePath:    "test_data/os-release-bad.txt",
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "FileReaderNil",
			reader:      nil,
			filePath:    "test_data/os-release.txt",
			wantID:      "",
			wantVersion: "",
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "FilePathEmpty",
			reader:      defaultFileReader,
			filePath:    "",
			wantID:      "",
			wantVersion: "",
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "ErrorInFileReading",
			reader: FileReadCloser(func(path string) (io.ReadCloser, error) {
				return nil, errors.New("File Read Error")
			}),
			filePath:    "test_data/os-release-error.txt",
			wantID:      "",
			wantVersion: "",
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotID, gotVersion, gotErr := readReleaseInfo(context.Background(), test.reader, test.filePath)
			if gotID != test.wantID {
				t.Errorf("GetReleaseInfo() unexpected osVendorID, got %q want %q", gotID, test.wantID)
			}
			if gotVersion != test.wantVersion {
				t.Errorf("GetReleaseInfo() unexpected osVersion, got %q want %q", gotVersion, test.wantVersion)
			}
			if cmp.Diff(gotErr, test.wantErr, cmpopts.EquateErrors()) != "" {
				t.Errorf("GetReleaseInfo() unexpected error, got %v want %v", gotErr, test.wantErr)
			}
		})
	}
}

func TestReadData(t *testing.T) {
	defaultFileReader := FileReadCloser(func(path string) (io.ReadCloser, error) {
		file, err := testFS.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	tests := []struct {
		name          string
		reader        FileReadCloser
		filePath      string
		wantOSName    string
		wantOSVendor  string
		wantOSVersion string
		wantErr       error
	}{
		{
			name:          "success",
			reader:        defaultFileReader,
			filePath:      "test_data/os-release.txt",
			wantOSName:    "linux",
			wantOSVendor:  "debian",
			wantOSVersion: "11",
			wantErr:       nil,
		},
		{
			name: "error",
			reader: FileReadCloser(func(path string) (io.ReadCloser, error) {
				return nil, errors.New("File Read Error")
			}),
			filePath:      "test_data/os-release-bad.txt",
			wantOSName:    "linux",
			wantOSVendor:  "",
			wantOSVersion: "",
			wantErr:       cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotInfo, err := ReadData(context.Background(), test.reader, test.filePath)
			if cmp.Diff(err, test.wantErr, cmpopts.EquateErrors()) != "" {
				t.Errorf("GetInfo() unexpected error, got %v want %v", err, test.wantErr)
			}
			if gotInfo.OSName != test.wantOSName {
				t.Errorf("GetInfo() unexpected osName, got %q want %q", gotInfo.OSName, test.wantOSName)
			}
		})
	}
}
