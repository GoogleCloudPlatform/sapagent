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

package sapservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       false,
			ProcessMetricsFrequency:     5,
			ProcessMetricsSendFrequency: 60,
		},
		CloudProperties: &iipb.CloudProperties{
			ProjectId:        "test-project",
			InstanceId:       "test-instance",
			Zone:             "test-zone",
			InstanceName:     "test-instance",
			Image:            "test-image",
			NumericProjectId: "123456",
		},
	}
)

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		skippedMetrics map[string]bool
		execute        commandlineexecutor.Execute
		exitCode       commandlineexecutor.ExitCode
		wantCount      int
	}{
		{
			name:   "CollectMetricsWithFailureExitCodeInIsFailedAndIsDisabled",
			config: defaultConfig,
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.HasPrefix(params.ArgsToSplit, "is-failed") {
					// a zero exit code will represent an error here
					return commandlineexecutor.Result{}
				}
				return commandlineexecutor.Result{
					Error: errors.New("unable to execute command"),
				}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 10,
		},
		{
			name:   "CollectMetricsWithFailuresInExitCodeInIsFailed",
			config: defaultConfig,
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 5,
		},
		{
			name:   "NoMetricsCollectedFromIsFailedAndIsDisabled",
			config: defaultConfig,
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.HasPrefix(params.ArgsToSplit, "is-failed") {
					// a zero exit code will represent an error here
					return commandlineexecutor.Result{
						Error:            errors.New("is-failed error"),
						ExitStatusParsed: true,
						ExitCode:         1,
					}
				}
				return commandlineexecutor.Result{}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 0,
		},
		{
			name:   "CollectMetricsWithFailureInIsDisabled",
			config: defaultConfig,
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error:            errors.New("unable to execute command"),
					ExitStatusParsed: true,
					ExitCode:         1,
				}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 5,
		},
		{
			name: "SkipIsFailedMetrics",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{failedMPath},
				},
			},
			skippedMetrics: map[string]bool{failedMPath: true},
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 5,
		},
		{
			name: "SkipIsDisableddMetrics",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{disabledMPath},
				},
			},
			skippedMetrics: map[string]bool{disabledMPath: true},
			execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			exitCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testInstanceProperties := &InstanceProperties{
				Config:         defaultConfig,
				Execute:        test.execute,
				ExitCode:       test.exitCode,
				Client:         &fake.TimeSeriesCreator{},
				SkippedMetrics: test.skippedMetrics,
			}
			got, _ := testInstanceProperties.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Got %d != want %d", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	c := context.Background()
	p := &InstanceProperties{
		Config: defaultConfig,
		Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{}
		},
		ExitCode: func(err error) int {
			if err == nil {
				return 0
			}
			return 1
		},
		Client:          &fake.TimeSeriesCreator{},
		PMBackoffPolicy: defaultBOPolicy(c),
	}
	_, err := p.CollectWithRetry(c)
	if err != nil {
		t.Errorf("CollectWithRetry returned unexpected error: %v", err)
	}
}
