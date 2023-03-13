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
	"sync"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monrespb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type fakeHealthMonitor struct {
	statuses          map[string]bool
	registrationSpec  *heartbeat.Spec
	registrationError error
}

func (f fakeHealthMonitor) GetStatuses() map[string]bool {
	return f.statuses
}

func (f fakeHealthMonitor) Register(name string) (*heartbeat.Spec, error) {
	return f.registrationSpec, f.registrationError
}

func basicParameters() Parameters {
	return Parameters{
		Config: &cfgpb.Configuration{
			CollectionConfiguration: &cfgpb.CollectionConfiguration{
				AgentHealthFrequency:     5,
				AgentMetricsFrequency:    5,
				CollectAgentMetrics:      true,
				HeartbeatFrequency:       5,
				MissedHeartbeatThreshold: 5,
			},
		},
		HealthMonitor:     fakeHealthMonitor{},
		timeSeriesCreator: &fake.TimeSeriesCreator{},
		BackOffs:          cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
	}
}

func createService(ctx context.Context, params Parameters, t *testing.T) *Service {
	t.Helper()
	service, err := NewService(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

var (
	pointComparer = cmp.Comparer(func(a, b *monrespb.Point) bool {
		valueEqual := cmp.Equal(a.GetValue().GetDoubleValue(), b.GetValue().GetDoubleValue())
		startTimeEqual := cmp.Equal(a.GetInterval().GetStartTime(), b.GetInterval().GetStartTime(), protocmp.Transform())
		endTimeEqual := cmp.Equal(a.GetInterval().GetEndTime(), b.GetInterval().GetEndTime(), protocmp.Transform())
		aDescriptor0, aDescriptor1 := a.Descriptor()
		bDescriptor0, bDescriptor1 := b.Descriptor()
		descriptorEqual := cmp.Equal(aDescriptor0, bDescriptor0) && cmp.Equal(aDescriptor1, bDescriptor1)
		return valueEqual && startTimeEqual && endTimeEqual && descriptorEqual
	})

	resourceComparer = cmp.Comparer(func(a, b *mrpb.MonitoredResource) bool {
		typeEqual := cmp.Equal(a.GetType(), b.GetType())
		labelsEqual := cmp.Equal(a.GetLabels(), b.GetLabels())
		return typeEqual && labelsEqual
	})
	timeSeriesComparer = cmp.Comparer(func(a, b *monrespb.TimeSeries) bool {
		points := cmp.Equal(a.GetPoints(), b.GetPoints(), pointComparer)
		metricType := cmp.Equal(a.GetMetric().GetType(), b.GetMetric().GetType())
		metricLabel := cmp.Equal(a.GetMetric().GetLabels(), b.GetMetric().GetLabels())
		resourcesEqual := cmp.Equal(a.GetResource(), b.GetResource(), resourceComparer)
		return points && metricType && metricLabel && resourcesEqual
	})
	fakeTimestamp = &tspb.Timestamp{
		Seconds: 42,
	}
	fakeNow = func() *tspb.Timestamp {
		return fakeTimestamp
	}
	paramsFactory = func() Parameters {
		parameters := basicParameters()
		parameters.now = fakeNow
		parameters.timeSeriesCreator = &fake.TimeSeriesCreator{}
		parameters.Config.BareMetal = false
		parameters.Config.CloudProperties = &ipb.CloudProperties{
			InstanceId: "test-instance",
			ProjectId:  "test-project",
			Zone:       "test-zone",
			Region:     "test-region",
		}
		return parameters
	}

	bareMetalLabels = map[string]string{
		"project_id": "test-project",
		"location":   "test-region",
		"namespace":  "test-instance",
		"node_id":    "test-instance",
	}
	vmLabels = map[string]string{
		"instance_id": "test-instance",
		"project_id":  "test-project",
		"zone":        "test-zone",
	}
)

func TestNewService_shouldValidateParameters(t *testing.T) {
	var testData = []struct {
		testName string
		params   Parameters
		want     error
	}{
		{
			testName: "Valid configuration",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 10,
						AgentHealthFrequency:  60,
					},
				},
			},
			want: nil,
		},
		{
			testName: "Config is nil",
			params: Parameters{
				Config: nil,
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection enabled metrics without health",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 500,
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection enabled with 0 metrics frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 0,
						AgentHealthFrequency:  10,
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection enabled with <5 health frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						AgentMetricsFrequency: 10,
						AgentHealthFrequency:  4,
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			testName: "Collection disabled with <5 frequency",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectAgentMetrics:   false,
						AgentMetricsFrequency: 4,
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
						AgentHealthFrequency:  10,
					},
				},
			},
			want: cmpopts.AnyError,
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			ctx := context.Background()
			d.params.timeSeriesCreator = &fake.TimeSeriesCreator{}
			_, got := NewService(ctx, d.params)
			if !cmp.Equal(got, d.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(%v) = %v, want %v", d.params, got, d.want)
			}
		})
	}
}

func TestDefaultTimeSeriesFactory_createsCorrectTimeSeriesForHealth(t *testing.T) {
	var testData = []struct {
		testName  string
		health    bool
		timestamp *tspb.Timestamp
		params    Parameters
		want      []*monrespb.TimeSeries
	}{
		{
			testName:  "healthy baremetal",
			health:    true,
			timestamp: fakeTimestamp,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = true
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "generic_node",
						Labels: bareMetalLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/health",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{true},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		}, {
			testName:  "healthy vm",
			health:    true,
			timestamp: fakeTimestamp,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = false
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "gce_instance",
						Labels: vmLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/health",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{true},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		},
		{
			testName:  "unhealthy baremetal",
			health:    false,
			timestamp: fakeTimestamp,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = true
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "generic_node",
						Labels: bareMetalLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/health",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{false},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		}, {
			testName:  "unhealthy vm",
			health:    false,
			timestamp: fakeTimestamp,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = false
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "gce_instance",
						Labels: vmLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/health",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{false},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			ctx := context.Background()
			service := createService(ctx, d.params, t)
			got := service.createHealthTimeSeries(d.health)
			if diff := cmp.Diff(d.want, got, timeSeriesComparer); diff != "" {
				t.Errorf("createHealthTimeSeries() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultTimeSeriesFactory_createsCorrectTimeSeriesForUsage(t *testing.T) {

	var testData = []struct {
		testName  string
		cpu       float64
		memory    uint64
		timestamp *tspb.Timestamp
		params    Parameters
		want      []*monrespb.TimeSeries
	}{
		{
			testName:  "cpu 0.0 memory 0 baremetal",
			cpu:       0.0,
			memory:    0,
			timestamp: fakeTimestamp,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = true
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "generic_node",
						Labels: bareMetalLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/cpu/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{0.0},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "generic_node",
						Labels: bareMetalLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/memory/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{0},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		},
		{
			testName: "cpu 1.2 memory 1kb vm",
			cpu:      1.2,
			memory:   1000,
			params: func() Parameters {
				p := paramsFactory()
				p.Config.BareMetal = false
				return p
			}(),
			want: []*monrespb.TimeSeries{
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "gce_instance",
						Labels: vmLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/cpu/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{1.2},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
				&monrespb.TimeSeries{
					Resource: &mrpb.MonitoredResource{
						Type:   "gce_instance",
						Labels: vmLabels,
					},
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/agent/memory/utilization",
					},
					Points: []*monrespb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{1000},
							},
							Interval: &cpb.TimeInterval{
								StartTime: fakeTimestamp,
								EndTime:   fakeTimestamp,
							},
						},
					},
				},
			},
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			ctx := context.Background()
			service := createService(ctx, d.params, t)
			usage := usage{d.cpu, d.memory}
			got := service.createMetricTimeSeries(usage)
			if diff := cmp.Diff(d.want, got, timeSeriesComparer); diff != "" {
				t.Errorf("createMetricTimeSeries() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestCollectHealthStatus_shouldIndicateUnhealthyIfAnyServiceIsUnhealthy(t *testing.T) {
	testData := []struct {
		name     string
		statuses map[string]bool
		want     bool
	}{
		{
			name:     "0 services",
			statuses: map[string]bool{},
			want:     true,
		},
		{
			name: "1 healthy",
			statuses: map[string]bool{
				"foo": true,
			},
			want: true,
		},
		{
			name: "1 unhealthy",
			statuses: map[string]bool{
				"foo": false,
			},
			want: false,
		},
		{
			name: "many healthy",
			statuses: map[string]bool{
				"foo": true,
				"bar": true,
				"baz": true,
				"qux": true,
			},
			want: true,
		},
		{
			name: "many healthy and 1 unhealthy",
			statuses: map[string]bool{
				"foo": true,
				"bar": true,
				"baz": false,
				"qux": true,
			},
			want: false,
		},
		{
			name: "many unhealthy",
			statuses: map[string]bool{
				"foo": false,
				"bar": false,
				"baz": false,
				"qux": false,
			},
			want: false,
		},
	}

	for _, d := range testData {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			params := basicParameters()
			fake := fakeHealthMonitor{
				statuses: d.statuses,
			}
			params.HealthMonitor = fake
			s := createService(ctx, params, t)
			got := s.collectHealthStatus(ctx)
			if got != d.want {
				t.Errorf("collectHealthStatus() = %v, want %v", got, d.want)
			}
		})
	}
}

func TestCollectAndSubmitHealth_shouldReturnErrorWhenSubmitFails(t *testing.T) {
	testData := []struct {
		name      string
		submitRes error
		want      error
	}{
		{
			name:      "submit succeeds",
			submitRes: nil,
			want:      nil,
		},
		{
			name:      "submit fails",
			submitRes: errors.New("intentional failure"),
			want:      cmpopts.AnyError,
		},
	}
	for _, d := range testData {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			params := basicParameters()
			s := createService(ctx, params, t)
			s.timeSeriesSubmitter = func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error {
				return d.submitRes
			}
			got := s.collectAndSubmitHealth(ctx)
			if !cmp.Equal(got, d.want, cmpopts.EquateErrors()) {
				t.Errorf("collectAndSubmitHealth() = %v, want %v", got, d.want)
			}
		})
	}
}

func TestCollectAndSubmitMetrics_shouldReturnErrorWhenSubmitFails(t *testing.T) {
	testData := []struct {
		name      string
		submitRes error
		want      error
	}{
		{
			name:      "submit succeeds",
			submitRes: nil,
			want:      nil,
		},
		{
			name:      "submit fails",
			submitRes: errors.New("intentional failure"),
			want:      cmpopts.AnyError,
		},
	}
	for _, d := range testData {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			params := basicParameters()
			s := createService(ctx, params, t)
			s.timeSeriesSubmitter = func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error {
				return d.submitRes
			}
			got := s.collectAndSubmitMetrics(ctx)
			if !cmp.Equal(got, d.want, cmpopts.EquateErrors()) {
				t.Errorf("collectAndSubmitMetrics() = %v, want %v", got, d.want)
			}
		})
	}
}

func TestCollectAndSubmitMetrics_shouldReturnErrorUsageReaderFails(t *testing.T) {
	testData := []struct {
		name  string
		usage usage
		err   error
		want  error
	}{
		{
			name:  "collect succeeds",
			usage: usage{},
			want:  nil,
		},
		{
			name: "collect fails",
			err:  errors.New("intentional failure"),
			want: cmpopts.AnyError,
		},
	}
	for _, d := range testData {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			params := basicParameters()
			s := createService(ctx, params, t)
			s.usageReader = func(ctx context.Context) (usage, error) { return d.usage, d.err }
			got := s.collectAndSubmitMetrics(ctx)
			if !cmp.Equal(got, d.want, cmpopts.EquateErrors()) {
				t.Errorf("collectAndSubmitMetrics() = %v, want %v", got, d.want)
			}
		})
	}
}

func TestCollectAndSubmitMetrics_shouldFailWhenSubmitFails(t *testing.T) {
	ctx := context.Background()
	params := basicParameters()
	params.usageReader = func(ctx context.Context) (usage, error) {
		return usage{cpu: 0.0, memory: 0.0}, nil
	}
	expectedErr := errors.New("intentional failure")
	params.timeSeriesSubmitter = func(ctx context.Context, request *monpb.CreateTimeSeriesRequest) error {
		return expectedErr
	}
	service := createService(ctx, params, t)
	err := service.collectAndSubmitMetrics(ctx)
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
	if err := service.collectAndSubmitMetrics(ctx); err != nil {
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
		frequency int64 // seconds
		want      int
	}{
		{
			testName:  "600ms timeout 1s collect",
			timeout:   600 * time.Millisecond,
			frequency: 1,
			want:      0,
		},
		{
			testName:  "1500ms timeout 1s collect",
			timeout:   1500 * time.Millisecond,
			frequency: 1,
			want:      1,
		},
		{
			testName:  "2500ms timeout 1s collect",
			timeout:   2500 * time.Millisecond,
			frequency: 1,
			want:      2,
		},
	}
	for _, d := range testData {
		t.Run(d.testName, func(t *testing.T) {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, d.timeout)
			defer cancel()
			params := basicParameters()
			service := createService(ctx, params, t)
			service.config.CollectionConfiguration.AgentMetricsFrequency = d.frequency
			got := 0
			wrappedUsageReader := service.usageReader
			lock := sync.Mutex{}
			service.usageReader = usageReader(func(ctx context.Context) (usage, error) {
				lock.Lock()
				got++
				lock.Unlock()
				return wrappedUsageReader(ctx)
			})
			service.Start(ctx)
			<-ctx.Done()
			lock.Lock()
			if got != d.want {
				t.Errorf("usageReader invocation count mismatch: got %v, want %v", got, d.want)
			}
			lock.Unlock()
		})
	}
}
