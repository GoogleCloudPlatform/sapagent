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
	"strings"
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
		if params.Executable == "free" {
			return commandlineexecutor.Result{
				StdOut: defaultFreeStdOut,
			}
		}
		if strings.Contains(params.ArgsToSplit, "meminfo") {
			return commandlineexecutor.Result{
				StdOut: defaultMemInfoStdOut,
			}
		}
		return commandlineexecutor.Result{
			StdOut: "0",
		}
	}
	defaultMemInfoStdOut = "MemTotal: 65319908 kB \nMemFree: 784588 kB\nMemAvailable: 37180312 kB\nBuffers: 6644 kB\nCached: 39375836 kB\nSwapCached: 44 kB\nActive: 26813028 kB\nInactive: 35262344 kB\nDirty: 196 kB\nShmem: 3692176 kB\nCommitLimit: 34757100 kB\nCommitted_AS: 32541940 kB"
	defaultFreeStdOut    = "total        used        free      shared  buff/cache   available\n Mem:       131902960    18861708   109490620     4807332     9611728   113041252\n Swap:        2097148           0     2097148"

	allMetrics = []string{
		memFreeKBPath,
		memAvailKBPath,
		memTotalKBPath,
		buffersKBPath,
		cachedKBPath,
		swapCachedKBPath,
		commitKBPath,
		commitPercentPath,
		activeKBPath,
		inactiveKBPath,
		dirtyKBPath,
		shMemKBPath,
		freeMemUsedKBPath,
		freeMemTotalKBPath,
		freeMemFreeKBPath,
		freeMemSharedKBPath,
		freeMemBuffersAndCacheKBPath,
		freeMemAvailableKBPath,
		freeSwapTotalKBPath,
		freeSwapUsedKBPath,
		freeSwapFreeKBPath,
	}
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
		wantLabels     map[string]string
	}{
		{
			name:       "SuccessfulMetricsCollection",
			config:     defaultConfig,
			executor:   defaultExecutor,
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  21,
		},
		{
			name:           "SuccessfulMetricsCollectionWithSkippedMetrics",
			config:         defaultConfig,
			executor:       defaultExecutor,
			fakeClient:     sapcontrolclienttest.Fake{},
			skippedMetrics: map[string]bool{freeMemUsedKBPath: true, memFreeKBPath: true, freeSwapTotalKBPath: true},
			wantCount:      18,
		},
		{
			name:   "ProcMemInfoMetricsCollectionFailure",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "meminfo") {
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
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						Error: cmpopts.AnyError,
					}
				}
				if strings.Contains(params.ArgsToSplit, "meminfo") {
					return commandlineexecutor.Result{
						StdOut: defaultMemInfoStdOut,
					}
				}
				return commandlineexecutor.Result{
					StdOut: "0",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  12,
			wantErr:    true,
		},
		{
			name:   "ProcMemInfoMetricsCollectionFailureInvalidOutput",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "meminfo") {
					return commandlineexecutor.Result{
						StdOut: "MemTotal: \nMemFree: 784588 kB\nMemAvailable: 37180312 kB",
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
			wantCount:  11,
			wantErr:    false,
		},
		{
			name:   "ProcMemInfoCommitRatioSkippedMemTotalIsZero",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "meminfo") {
					return commandlineexecutor.Result{
						StdOut: "MemTotal: 0 kB \nCommitted_AS: 37180312 kB",
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
			wantCount:  11,
			wantErr:    false,
		},
		{
			name:   "FreeMetricsCollectionFailureInvalidOutput",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "meminfo") {
					return commandlineexecutor.Result{
						StdOut: defaultMemInfoStdOut,
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
			wantCount:  12,
			wantErr:    false,
		},
		{
			name:   "SuccessfulMetricsCollectionWithSkippedLabels",
			config: defaultConfig,
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "free" {
					return commandlineexecutor.Result{
						StdOut: defaultFreeStdOut,
					}
				}
				if strings.Contains(params.ArgsToSplit, "meminfo") {
					return commandlineexecutor.Result{
						StdOut: defaultMemInfoStdOut,
					}
				}
				return commandlineexecutor.Result{
					Error: cmpopts.AnyError,
				}
			},
			fakeClient:  sapcontrolclienttest.Fake{},
			wantCount:   21,
			wantErr:     false,
			checkLabels: true,
			wantLabels:  map[string]string{},
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
				gotLabels := metrics[0].GetMetric().GetLabels()
				if cmp.Diff(gotLabels, test.wantLabels) != "" {
					t.Errorf("Collect() returned unexpected metric labels diff (-want +got):\n%s", cmp.Diff(gotLabels, test.wantLabels))
				}
			}
		})
	}

	for _, metricToSkip := range allMetrics {
		t.Run("Skip "+metricToSkip, func(t *testing.T) {
			testLinuxOsMetricsInstanceProps := &LinuxOsMetricsInstanceProperties{
				Config:         defaultConfig,
				Client:         &fake.TimeSeriesCreator{},
				Executor:       defaultExecutor,
				SAPInstance:    defaultSAPInstanceHANA,
				SkippedMetrics: map[string]bool{metricToSkip: true},
			}
			metrics, err := testLinuxOsMetricsInstanceProps.Collect(context.Background())
			if err != nil {
				t.Errorf("Collect() returned an unexpected error: %v", err)
			}
			for _, metric := range metrics {
				if strings.HasSuffix(metric.GetMetric().GetType(), metricToSkip) {
					t.Errorf("Collect() returned unexpected metric: %v", metric.GetMetric().GetType())
				}
			}
		})
	}
}
