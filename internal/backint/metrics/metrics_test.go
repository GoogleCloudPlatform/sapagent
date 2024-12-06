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

package metrics

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:    "default-project",
		InstanceName: "default-instance",
	}
	defaultBackOffIntervals = cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond)
	defaultMetricClient     = func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
		return &fake.TimeSeriesCreator{}, nil
	}
)

func TestSendToCloudMonitoring(t *testing.T) {
	tests := []struct {
		name         string
		fileSize     int64
		config       *bpb.BackintConfiguration
		success      bool
		metricClient metricClientFunc
		want         bool
	}{
		{
			name:   "DontSendToMonitoring",
			config: &bpb.BackintConfiguration{SendMetricsToMonitoring: &wpb.BoolValue{Value: false}},
			want:   false,
		},
		{
			name:   "FailedToCreateMetricClient",
			config: &bpb.BackintConfiguration{SendMetricsToMonitoring: &wpb.BoolValue{Value: true}},
			metricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
				return nil, fmt.Errorf("failed to create metric client")
			},
			want: false,
		},
		{
			name:   "FailedToSendMetric",
			config: &bpb.BackintConfiguration{SendMetricsToMonitoring: &wpb.BoolValue{Value: true}},
			metricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
				return &fake.TimeSeriesCreator{Err: fmt.Errorf("failed to send status")}, nil
			},
			want: false,
		},
		{
			name:         "SuccessOnlyStatus",
			config:       &bpb.BackintConfiguration{SendMetricsToMonitoring: &wpb.BoolValue{Value: true}},
			metricClient: defaultMetricClient,
			fileSize:     oneGB,
			want:         true,
		},
		{
			name:         "SuccessWithThroughput",
			config:       &bpb.BackintConfiguration{SendMetricsToMonitoring: &wpb.BoolValue{Value: true}},
			metricClient: defaultMetricClient,
			success:      true,
			fileSize:     oneGB,
			want:         true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SendToCloudMonitoring(context.Background(), "backup", "test.txt", test.fileSize, time.Second, test.config, test.success, defaultCloudProperties, defaultBackOffIntervals, test.metricClient)
			if got != test.want {
				t.Errorf("SendToCloudMonitoring(%v, %v, %v) = %v, want: %v", test.fileSize, test.config, test.success, got, test.want)
			}
		})
	}
}
