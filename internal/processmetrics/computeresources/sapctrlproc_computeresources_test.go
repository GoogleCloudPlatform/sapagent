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
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
)

func TestCollectForSAPControlProcesses(t *testing.T) {
	tests := []struct {
		name      string
		executor  commandExecutor
		wantCount int
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
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   111\nsapstart    222\n", "", nil
				}
				if cmd == "getconf" {
					return "100\n", "", nil
				}
				return "", "", nil
			},
			wantCount: 3,
		},
		{
			name: "OnlyCPUPerProcessMetricAvailable",
			executor: func(cmd, args string) (string, string, error) {
				if cmd == "ps" {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    333\n", "", nil
				}
				if cmd == "getconf" {
					return "100\n", "", nil
				}
				return "", "", nil
			},
			wantCount: 1,
		},
		{
			name: "FetchedBothCPUAndMemoryMetricsSuccessfully",
			executor: func(cmd, args string) (string, string, error) {
				if cmd == "ps" {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    444\n", "", nil
				}
				if cmd == "getconf" {
					return "100\n", "", nil
				}
				return "", "", nil
			},
			wantCount: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testProps := &SAPControlProcInstanceProperties{
				Config:        defaultConfig,
				Client:        &fake.TimeSeriesCreator{},
				Executor:      test.executor,
				NewProcHelper: newProcessWithContextHelperTest,
			}
			got := testProps.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() = %d , want %d", len(got), test.wantCount)
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
