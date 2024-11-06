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
	"io"
	"testing"

	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights"
)

func TestInit(t *testing.T) {
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := Parameters{
				ConfigFileReader:  test.reader,
				OSReleaseFilePath: test.filePath,
			}
			p.Init(context.Background())
			if p.osVendorID != test.wantID {
				t.Errorf("Init() unexpected osVendorID, got %q want %q", p.osVendorID, test.wantID)
			}
			if p.osVersion != test.wantVersion {
				t.Errorf("Init() unexpected osVersion, got %q want %q", p.osVersion, test.wantVersion)
			}
			if p.hanaInsightRules == nil {
				t.Errorf("Init() unexpected hanaInsightRules, got nil want %T", []*rpb.Rule{})
			}
		})
	}
}
