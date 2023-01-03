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
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
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
		wantCount int
	}{
		{
			name: "CollectMetricsSuccessfully",
			executor: func(string, string) (string, string, error) {
				return "", "", nil
			},
			wantCount: 10,
		},
		{
			name: "CollectMetricsWithFailureExitCode",
			executor: func(string, string) (string, string, error) {
				return "", "", errors.New("unable to execute command")
			},
			wantCount: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testInstanceProperties := &InstanceProperties{
				Config:   defaultConfig,
				Executor: test.executor,
				Client:   &fake.TimeSeriesCreator{},
			}
			got := testInstanceProperties.Collect()
			if len(got) != test.wantCount {
				t.Errorf("Got %d != want %d", len(got), test.wantCount)
			}
		})
	}
}
