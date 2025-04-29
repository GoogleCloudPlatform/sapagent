/*
Copyright 2025 Google LLC

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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
)

var (
	defaultExecutor = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		if params.Executable == "sar" {
			return commandlineexecutor.Result{
				StdOut: defaultSarStdOut,
			}
		}
		if params.Executable == "free" {
			return commandlineexecutor.Result{
				StdOut: defaultFreeStdOut,
			}
		}
		return commandlineexecutor.Result{
			StdOut: "0",
		}
	}
	defaultSarStdOut  = "Linux 5.14.21-150500.55.97-default (vis-hsr-pri) 04/28/25 _x86_64_ (8 CPU)\n\n 17:37:33 kbmemfree kbavail kbmemused %memused kbbuffers kbcached kbcommit %commit kbactive kbinact kbdirty\n 17:37:34 565928 39475952 21647932 32.88 6668 42877684 29825848 43.90 3326856 60780804 24"
	defaultFreeStdOut = "total        used        free      shared  buff/cache   available\n Mem:       131902960    18861708   109490620     4807332     9611728   113041252\n Swap:        2097148           0     2097148"
)

func TestCollectWithRetryLinuxOSMetrics(t *testing.T) {
	c := context.Background()
	lp := &LinuxOsMetricsInstanceProperties{
		Config:          defaultConfig,
		Client:          &fake.TimeSeriesCreator{},
		Executor:        defaultExecutor,
		SAPInstance:     defaultSAPInstanceHANA,
		PMBackoffPolicy: defaultBOPolicy(c),
	}
	_, err := lp.CollectWithRetry(c)
	if err != nil {
		t.Errorf("CollectWithRetry() = %v, want nil", err)
	}
}

func TestCollectForLinuxOsMetrics(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		skippedMetrics map[string]bool
		executor       commandlineexecutor.Execute
		fakeClient     sapcontrolclienttest.Fake
		wantCount      int
		wantErr        bool
		checkLabels    bool
		expectedLabels map[string]string
	}{
		{
			name:       "SuccessfulMetricsCollection",
			config:     defaultConfig,
			executor:   defaultExecutor,
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  20,
		},
		{
			name:           "SuccessfulMetricsCollectionWithSkippedMetrics",
			config:         defaultConfig,
			executor:       defaultExecutor,
			fakeClient:     sapcontrolclienttest.Fake{},
			skippedMetrics: map[string]bool{freeMemUsedPath: true, sarkBMemFreePath: true, freeSwapTotalPath: true},
			wantCount:      17,
		},
		{
			name:   "SarMetricsCollectionFailure",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						Error: cmpopts.AnyError,
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: defaultFreeStdOut,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  9,
			wantErr:    true,
		},
		{
			name:   "FreeMetricsCollectionFailure",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						StdOut: defaultSarStdOut,
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						Error: cmpopts.AnyError,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  11,
			wantErr:    true,
		},
		{
			name:   "SarMetricsCollectionFailureInvalidOutput1",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						StdOut: "Linux 5.14.21-150500.55.97-default (vis-hsr-pri) 04/28/25 _x86_64_ (8 CPU)\n\n 17:37:33 kbmemfree kbavail kbmemused %memused kbbuffers kbcached kbcommit %commit kbactive kbinact kbdirty",
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: defaultFreeStdOut,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  9,
			wantErr:    false,
		},
		{
			name:   "SarMetricsCollectionFailureHeaderFieldsMismatch",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						StdOut: "Linux 5.14.21-150500.55.97-default (vis-hsr-pri) 04/28/25 _x86_64_ (8 CPU)\n\n 17:37:33 kbmemfree kbavail kbmemused %memused kbbuffers kbcached kbcommit %commit kbactive kbinact kbdirty\n",
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: defaultFreeStdOut,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  9,
			wantErr:    false,
		},
		{
			name:   "FreeMetricsCollectionFailureInvalidOutput",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						StdOut: defaultSarStdOut,
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: "total        used        free      shared \n Mem:       131902960    18861708   109490620     4807332     9611728   113041252\n Swap:        2097148           0     2097148\n",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  11,
			wantErr:    false,
		},
		{
			name:   "SuccessfulMetricsCollectionWithSkippedLabels",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sar" {
					return commandlineexecutor.Result{
						StdOut: defaultSarStdOut,
					}
				}
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: defaultFreeStdOut,
					}
				}
				return commandlineexecutor.Result{
					Error: cmpopts.AnyError,
				}
			},
			fakeClient:     sapcontrolclienttest.Fake{},
			wantCount:      20,
			wantErr:        false,
			checkLabels:    true,
			expectedLabels: map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testLinuxOsMetricsInstanceProps := &LinuxOsMetricsInstanceProperties{
				Config:         test.config,
				Client:         &fake.TimeSeriesCreator{},
				Executor:       test.executor,
				SAPInstance:    defaultSAPInstanceHANA,
				SkippedMetrics: test.skippedMetrics,
			}
			metrics, err := testLinuxOsMetricsInstanceProps.Collect(context.Background())
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("Collect() returned an unexpected error: %v, want error presence: %v", err, test.wantErr)
			}
			if len(metrics) != test.wantCount {
				t.Errorf("Collect() = %d , want %d", len(metrics), test.wantCount)
			}
			if test.checkLabels && len(metrics) > 0 {
				labels := metrics[0].GetMetric().GetLabels()
				if cmp.Diff(labels, test.expectedLabels) != "" {
					t.Errorf("Collect() returned unexpected metric labels diff (-want +got):\n%s", cmp.Diff(labels, test.expectedLabels))
				}
			}
		})
	}
}
