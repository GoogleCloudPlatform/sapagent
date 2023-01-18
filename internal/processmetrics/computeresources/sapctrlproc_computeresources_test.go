/*
Copyright 2022 Google LLC

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

package computeresources

import (
	"context"
	"errors"
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
)

func TestCollectForSAPControlProcesses(t *testing.T) {
	tests := []struct {
		name       string
		executor   commandExecutor
		fileReader maintenance.FileReader
		wantCount  int
	}{
		{
			name: "EmptyPIDsMap",
			executor: func(cmd, args string) (string, string, error) {
				return "", "", nil
			},
			wantCount: 0,
		},
		{
			name: "OnlyMemoryPerProcessMetricAvailable",
			executor: func(cmd, args string) (string, string, error) {
				if cmd == "ps" {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    9204\n", "", nil
				}
				if cmd == "getconf" {
					return "100\n", "", nil
				}
				return "", "", nil
			},
			fileReader: MockedFileReader{
				expectedDataList: [][]byte{
					[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
					[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
					[]byte(""),
				},
				expectedErrList: []error{
					nil,
					nil,
					errors.New("unable to read file (proc/uptime)"),
				},
			},
			wantCount: 3,
		},
		{
			name: "OnlyCPUPerProcessMetricAvailable",
			executor: func(cmd, args string) (string, string, error) {
				if cmd == "ps" {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    9204\n", "", nil
				}
				if cmd == "getconf" {
					return "100\n", "", nil
				}
				return "", "", nil
			},
			fileReader: MockedFileReader{
				expectedDataList: [][]byte{
					[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
					[]byte("Name:  xxx\nVmSize:   sample\nVmRSS:   sample\nVmSwap:     sample\n"),
					[]byte("28057.65 218517.53\n"),
				},
				expectedErrList: []error{
					nil,
					nil,
					nil,
				},
			},
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testProps := &SAPControlProcInstanceProperties{
				Config:     defaultConfig,
				Client:     &fake.TimeSeriesCreator{},
				Executor:   test.executor,
				FileReader: test.fileReader,
			}
			got := testProps.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Got metrics length (%d) != Want metrics length (%d) from CollectForSAPControlProcesses.", len(got), test.wantCount)
			}

			for _, metric := range got {
				points := metric.TimeSeries.GetPoints()
				if points[0].GetValue().GetDoubleValue() < 0 {
					t.Errorf("Metric value for compute resources cannot be negative.")
				}
			}
		})
	}

}
