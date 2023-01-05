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
	"errors"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
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

func TestCollect(t *testing.T) {
	tests := []struct {
		name      string
		executor  commandExecutor
		errorCode exitCode
		wantCount int
	}{
		{
			name: "CollectMetricsWithFailureExitCodeInIsFailedAndIsDisabled",
			executor: func(cmd, args string) (string, string, error) {
				if strings.HasPrefix(args, "is-failed") {
					// a zero exit code will represent an error here
					return "", "", nil
				}
				return "", "", errors.New("unable to execute command")
			},
			errorCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 10,
		},
		{
			name: "CollectMetricsWithFailuresInExitCodeInIsFailed",
			executor: func(string, string) (string, string, error) {
				return "", "", nil
			},
			errorCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 5,
		},
		{
			name: "NoMetricsCollectedFromIsFailedAndIsDisabled",
			executor: func(cmd, args string) (string, string, error) {
				if strings.HasPrefix(args, "is-failed") {
					// a non zero code is treated as no error here.
					return "", "", errors.New("is-failed error")
				}
				return "", "", nil
			},
			errorCode: func(err error) int {
				if err == nil {
					return 0
				}
				return 1
			},
			wantCount: 0,
		},
		{
			name: "CollectMetricsWithFailureInIsDisabled",
			executor: func(cmd, args string) (string, string, error) {
				return "", "", errors.New("unable to execute command")
			},
			errorCode: func(err error) int {
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
				Config:   defaultConfig,
				Executor: test.executor,
				ExitCode: test.errorCode,
				Client:   &fake.TimeSeriesCreator{},
			}
			got := testInstanceProperties.Collect()
			if len(got) != test.wantCount {
				t.Errorf("Got %d != want %d", len(got), test.wantCount)
			}
		})
	}
}
