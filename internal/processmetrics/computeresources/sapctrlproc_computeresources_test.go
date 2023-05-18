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
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
)

func TestCollectForSAPControlProcesses(t *testing.T) {
	tests := []struct {
		name      string
		executor  commandlineexecutor.Execute
		wantCount int
	}{
		{
			name: "EmptyPIDsMap",
			executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			wantCount: 0,
		},
		{
			name: "OnlyMemoryPerProcessMetricAvailable",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "ps" {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   111\nsapstart    222\n",
					}
				} else if params.Executable == "getconf" {
					return commandlineexecutor.Result{
						StdOut: "100\n",
					}
				}
				return commandlineexecutor.Result{}
			},
			wantCount: 3,
		},
		{
			name: "OnlyCPUPerProcessMetricAvailable",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "ps" {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    333\n",
					}
				} else if params.Executable == "getconf" {
					return commandlineexecutor.Result{
						StdOut: "100\n",
					}
				}
				return commandlineexecutor.Result{}
			},
			wantCount: 1,
		},
		{
			name: "FetchedBothCPUAndMemoryMetricsSuccessfully",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "ps" {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nhdbindexserver   9603\nsapstart    444\n",
					}
				} else if params.Executable == "getconf" {
					return commandlineexecutor.Result{
						StdOut: "100\n",
					}
				}
				return commandlineexecutor.Result{}
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
				points := metric.GetPoints()
				if points[0].GetValue().GetDoubleValue() < 0 {
					t.Errorf("Metric value for compute resources cannot be negative.")
				}
			}
		})
	}

}
