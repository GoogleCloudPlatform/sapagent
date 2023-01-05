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

package agentmetrics

import (
	"context"
	"errors"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monrespb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func basicParameters() Parameters {
	return Parameters{
		Config: &cfgpb.Configuration{
			CollectionConfiguration: &cfgpb.CollectionConfiguration{
				AgentMetricsFrequency: 500,
				CollectAgentMetrics:   true,
			},
		},
		timeSeriesCreator: &fake.TimeSeriesCreator{},
	}
}

func createService(ctx context.Context, params Parameters, t *testing.T) *Service {
	service, err := NewService(ctx, params)
	if err != nil {
		t.Fatalf("This should not happen: %s", err)
	}
	return service
}

func TestValidateParameters_shouldCorrectlyValidate(t *testing.T) {
	var testData = []struct {
		testName string
		params   Parameters
		want     error
	}{
		{
			testName: "Config is nil",
			params: Parameters{
				Config: nil,
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection enabled with positive frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 500,
					},
				},
			},
			want: nil,
		},
		{
			testName: "Collection enabled with 0 frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 0,
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection disabled with 0 frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   false,
						AgentMetricsFrequency: 0,
					},
				},
			},
			want: nil,
		},
		{
			testName: "Collection enabled with negative frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: -1,
					},
				},
			},
			want: cmpopts.AnyError,
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			got := validateParameters(d.params)
			if !cmp.Equal(got, d.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(%v) = %v, want %v", d.params, got, d.want)
			}
		})
	}
}

func TestDefaultTimeSeriesFactory_createsCorrectTimeSeriesForUsage(t *testing.T) {
	var testData = []struct {
		testName string
		cpu      float64
		memory   float64
		want     []*monrespb.TimeSeries
	}{
		{
			testName: "cpu 0.0 memory 0.0",
			cpu:      0.0,
			memory:   0.0,
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/cpu/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{0.0},
							},
						},
					},
				},
				&monrespb.TimeSeries{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/memory/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{0.0},
							},
						},
					},
				},
			},
		},
		{
			testName: "cpu 1.2 memory 3.4",
			cpu:      1.2,
			memory:   3.4,
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/cpu/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{1.2},
							},
						},
					},
				},
				&monrespb.TimeSeries{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/memory/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{3.4},
							},
						},
					},
				},
			},
		},
	}
	params := basicParameters()
	ctx := context.Background()
	service := createService(ctx, params, t)
	pointComparer := cmp.Comparer(func(a, b *monrespb.Point) bool {
		return a.GetValue().GetDoubleValue() == b.GetValue().GetDoubleValue()
	})
	comparer := cmp.Comparer(func(a, b *monrespb.TimeSeries) bool {
		points := cmp.Equal(a.GetPoints(), b.GetPoints(), pointComparer)
		metricType := a.GetMetric().GetType() == b.GetMetric().GetType()
		metricLabel := cmp.Equal(a.GetMetric().GetLabels(), b.GetMetric().GetLabels())
		return points && metricType && metricLabel
	})
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			usage := usage{d.cpu, d.memory}
			got := service.createTimeSeries(usage)
			if diff := cmp.Diff(d.want, got, comparer); diff != "" {
				t.Errorf("timeSeriesFactory() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestCollectAndSubmit_shouldFailWhenUsageReaderFails(t *testing.T) {
	ctx := context.Background()
	params := basicParameters()
	expectedErr := errors.New("Intentional failure")
	params.usageReader = func(ctx context.Context) (usage, error) {
		return usage{}, expectedErr
	}
	service := createService(ctx, params, t)
	err := service.collectAndSubmit(ctx)
	if err == nil {
		t.Errorf("collectAndSubmit() = nil, want %v", expectedErr)
	}
}

func TestCollectAndSubmit_shouldFailWhenSubmitFails(t *testing.T) {
	ctx := context.Background()
	params := basicParameters()
	params.usageReader = func(ctx context.Context) (usage, error) {
		return usage{cpu: 0.0, memory: 0.0}, nil
	}
	expectedErr := errors.New("Intentional failure")
	params.timeSeriesSubmitter = func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error {
		return expectedErr
	}
	service := createService(ctx, params, t)
	err := service.collectAndSubmit(ctx)
	if err == nil {
		t.Errorf("collectAndSubmit() = nil, want %v", expectedErr)
	}
}

func TestCollectAndSubmit_shouldSucceedWhenSubmitSucceeds(t *testing.T) {
	ctx := context.Background()
	params := basicParameters()
	params.usageReader = func(ctx context.Context) (usage, error) {
		return usage{cpu: 5, memory: 6}, nil
	}
	submitCount := 0
	params.timeSeriesSubmitter = func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error {
		submitCount++
		return nil
	}

	service := createService(ctx, params, t)
	if err := service.collectAndSubmit(ctx); err != nil {
		t.Fatalf("collectAndSubmit() = %v, want nil", err)
	}

	if submitCount != 1 {
		t.Errorf("submitCount = %v, want %v", submitCount, 1)
	}
}

func TestStart_returnsImmediatelyIfNotConfigured(t *testing.T) {
	ctx := context.Background()
	params := basicParameters()
	params.Config.CollectionConfiguration.CollectAgentMetrics = false
	service := createService(ctx, params, t)
	numCollections := 0
	wrappedUsageReader := service.usageReader
	service.usageReader = usageReader(func(ctx context.Context) (usage, error) {
		numCollections++
		return wrappedUsageReader(ctx)
	})
	service.Start(ctx)
	if numCollections != 0 {
		t.Errorf("numCollections = %v, want 0", numCollections)
	}
}

func TestCollectAndSubmitLoop_respectsContextCancellation(t *testing.T) {
	var testData = []struct {
		testName  string
		timeout   time.Duration
		frequency int64
		want      int
	}{
		{
			testName:  "600ms timeout 500ms collect",
			timeout:   600 * time.Millisecond,
			frequency: 500,
			want:      1,
		},
		{
			testName:  "600ms timeout 250ms collect",
			timeout:   600 * time.Millisecond,
			frequency: 250,
			want:      2,
		},
		{
			testName:  "100ms timeout 250ms collect",
			timeout:   100 * time.Millisecond,
			frequency: 250,
			want:      0,
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, d.timeout)
			defer cancel()
			params := basicParameters()
			params.Config.CollectionConfiguration.AgentMetricsFrequency = d.frequency
			service := createService(ctx, params, t)
			got := 0
			wrappedUsageReader := service.usageReader
			service.usageReader = usageReader(func(ctx context.Context) (usage, error) {
				got++
				return wrappedUsageReader(ctx)
			})
			service.Start(ctx)
			<-ctx.Done()
			if got != d.want {
				t.Errorf("usageReader invocation count mismatch: got %v, want %v", got, d.want)
			}
		})
	}
}
