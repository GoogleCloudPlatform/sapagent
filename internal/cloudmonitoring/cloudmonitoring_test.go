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

package cloudmonitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	commonpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

var (
	defaultBackOffIntervals = NewBackOffIntervals(time.Millisecond, time.Millisecond)
)

func createFakeTimeSeries(count int) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries
	for i := 0; i < count; i++ {
		metrics = append(metrics, &mrpb.TimeSeries{})
	}
	return metrics
}

func TestCreateTimeSeriesWithRetry(t *testing.T) {
	tests := []struct {
		name          string
		client        *fake.TimeSeriesCreator
		req           *monitoringpb.CreateTimeSeriesRequest
		want          error
		wantCallCount int
	}{
		{
			name:   "succeedsWithEmpty",
			client: &fake.TimeSeriesCreator{},
			req: &monitoringpb.CreateTimeSeriesRequest{
				Name:       "test-project",
				TimeSeries: []*mrpb.TimeSeries{},
			},
			want:          nil,
			wantCallCount: 1,
		},
		{
			name:   "SucceedsWithOneTS",
			client: &fake.TimeSeriesCreator{},
			req: &monitoringpb.CreateTimeSeriesRequest{
				Name: "test-project",
				TimeSeries: []*mrpb.TimeSeries{{
					Metric:   &metricpb.Metric{},
					Resource: &monitoredresourcepb.MonitoredResource{},
					Points:   []*mrpb.Point{},
				}}},
			want:          nil,
			wantCallCount: 1,
		},
		{
			name:   "retryLimitExceeded",
			client: &fake.TimeSeriesCreator{Err: errors.New("CreateTimeSeries error")},
			req: &monitoringpb.CreateTimeSeriesRequest{
				Name:       "test-project",
				TimeSeries: []*mrpb.TimeSeries{},
			},
			want:          cmpopts.AnyError,
			wantCallCount: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CreateTimeSeriesWithRetry(context.Background(), test.client, test.req, defaultBackOffIntervals)
			if !cmp.Equal(test.want, got, cmpopts.EquateErrors()) {
				t.Errorf("Failure in CreateTimeSeriesWithRetry(%v) = %v, want %v", test.req, got, test.want)
			}
			if len(test.client.Calls) != test.wantCallCount {
				t.Errorf("CreateTimeSeriesWithRetry() unexpected call count. got: %d want: %d", len(test.client.Calls), test.wantCallCount)
			}
		})
	}
}

func TestQueryTimeSeriesWithRetry(t *testing.T) {
	newTimeSeriesData := func(v float64) []*mrpb.TimeSeriesData {
		return []*mrpb.TimeSeriesData{
			{
				PointData: []*mrpb.TimeSeriesData_PointData{
					{
						Values: []*commonpb.TypedValue{{Value: &commonpb.TypedValue_DoubleValue{v}}},
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		client        *fake.TimeSeriesQuerier
		req           *monitoringpb.QueryTimeSeriesRequest
		want          []*mrpb.TimeSeriesData
		wantErr       error
		wantCallCount int
	}{
		{
			name:          "succeeds",
			client:        &fake.TimeSeriesQuerier{TS: newTimeSeriesData(0.12345)},
			req:           &monitoringpb.QueryTimeSeriesRequest{},
			want:          newTimeSeriesData(0.12345),
			wantErr:       nil,
			wantCallCount: 1,
		},
		{
			name:          "retryLimitExceeded",
			client:        &fake.TimeSeriesQuerier{Err: errors.New("QueryTimeSeries error")},
			req:           &monitoringpb.QueryTimeSeriesRequest{},
			want:          nil,
			wantErr:       cmpopts.AnyError,
			wantCallCount: 5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := QueryTimeSeriesWithRetry(context.Background(), test.client, test.req, defaultBackOffIntervals)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("QueryTimeSeriesWithRetry() mismatch (-want, +got):\n%s", d)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("QueryTimeSeriesWithRetry() err: %v want: %v", err, test.wantErr)
			}
			if len(test.client.Calls) != test.wantCallCount {
				t.Errorf("QueryTimeSeriesWithRetry() unexpected call count. got: %d want: %d", len(test.client.Calls), test.wantCallCount)
			}
		})
	}
}

func TestQueryTimeSeriesNilBackOffs(t *testing.T) {
	_, err := QueryTimeSeriesWithRetry(context.Background(), &fake.TimeSeriesQuerier{}, &monitoringpb.QueryTimeSeriesRequest{}, nil)
	if err != nil {
		t.Errorf("QueryTimeSeriesWithRetry() with nil back off intervals returned err: %v", err)
	}
}

func TestCreateTimeSeriesNilBackOffs(t *testing.T) {
	err := CreateTimeSeriesWithRetry(context.Background(), &fake.TimeSeriesCreator{}, &monitoringpb.CreateTimeSeriesRequest{Name: "test-project", TimeSeries: []*mrpb.TimeSeries{}}, nil)
	if err != nil {
		t.Errorf("CreateTimeSeriesWithRetry() with nil back off intervals returned err: %v", err)
	}
}

func TestSendTimeSeries(t *testing.T) {
	tests := []struct {
		name              string
		count             int
		timeSeriesCreator *fake.TimeSeriesCreator
		wantSentCount     int
		wantBatchCount    int
		wantErr           error
	}{
		{
			name:              "SingleBatch",
			count:             199,
			timeSeriesCreator: &fake.TimeSeriesCreator{},
			wantSentCount:     199,
			wantBatchCount:    1,
		},
		{
			name:              "SingleBatchMaximumTSInABatch",
			count:             200,
			timeSeriesCreator: &fake.TimeSeriesCreator{},
			wantSentCount:     200,
			wantBatchCount:    1,
		},
		{
			name:              "MultipleBatches",
			count:             399,
			timeSeriesCreator: &fake.TimeSeriesCreator{},
			wantSentCount:     399,
			wantBatchCount:    2,
		},
		{
			name:              "SendErrorSingleBatch",
			count:             5,
			timeSeriesCreator: &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
			wantErr:           cmpopts.AnyError,
			wantSentCount:     0,
			wantBatchCount:    1,
		},
		{
			name:              "SendErrorMultipleBatches",
			count:             399,
			timeSeriesCreator: &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
			wantErr:           cmpopts.AnyError,
			wantSentCount:     0,
			wantBatchCount:    1,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			timeSeries := createFakeTimeSeries(test.count)
			gotSentCount, gotBatchCount, gotErr := SendTimeSeries(context.Background(), timeSeries, test.timeSeriesCreator, NewBackOffIntervals(time.Millisecond, time.Millisecond), "test-project")

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("send(%d, %v) = %v wantErr: %v", len(timeSeries), test.timeSeriesCreator, gotErr, test.wantErr)
			}

			if gotSentCount != test.wantSentCount {
				t.Errorf("send(%d, %v) = %v wantSentCount: %v", len(timeSeries), test.timeSeriesCreator, gotSentCount, test.wantSentCount)
			}

			if gotBatchCount != test.wantBatchCount {
				t.Errorf("send(%d, %v) = %v wantBatchCount: %v", len(timeSeries), test.timeSeriesCreator, gotBatchCount, test.wantBatchCount)
			}
		})
	}
}

func TestPrepareKey(t *testing.T) {
	tests := []struct {
		name string
		ts   *mrpb.TimeSeries
		want timeSeriesKey
	}{
		{
			name: "TestEmptyKey",
			ts:   &mrpb.TimeSeries{},
			want: timeSeriesKey{
				MetricKind:        metricpb.MetricDescriptor_METRIC_KIND_UNSPECIFIED.String(),
				MetricType:        "",
				MetricLabels:      "",
				MonitoredResource: "",
				ResourceLabels:    "",
			},
		},
		{
			name: "TestKey",
			ts: &mrpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type:   "custom.googleapis.com/invoice/paid/amount",
					Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
				},
				MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
				Resource: &monitoredresourcepb.MonitoredResource{
					Type:   "datastream.googleapis.com/Stream",
					Labels: map[string]string{"abc": "def", "sample": "labels"},
				},
			},
			want: timeSeriesKey{
				MetricKind:        metricpb.MetricDescriptor_CUMULATIVE.String(),
				MetricType:        "custom.googleapis.com/invoice/paid/amount",
				MetricLabels:      "abc+def,sample+labels,sample2+labels2",
				MonitoredResource: "datastream.googleapis.com/Stream",
				ResourceLabels:    "abc+def,sample+labels",
			},
		},
		{
			name: "TestKeyWithUnsortedLabels",
			ts: &mrpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type:   "custom.googleapis.com/invoice/paid/amount",
					Labels: map[string]string{"sample": "labels", "abc": "def", "sample2": "labels2"},
				},
				MetricKind: metricpb.MetricDescriptor_GAUGE,
				Resource: &monitoredresourcepb.MonitoredResource{
					Type:   "datastream.googleapis.com/Stream",
					Labels: map[string]string{"sample": "labels", "abc": "def"},
				},
			},
			want: timeSeriesKey{
				MetricKind:        metricpb.MetricDescriptor_GAUGE.String(),
				MetricType:        "custom.googleapis.com/invoice/paid/amount",
				MetricLabels:      "abc+def,sample+labels,sample2+labels2",
				MonitoredResource: "datastream.googleapis.com/Stream",
				ResourceLabels:    "abc+def,sample+labels",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := prepareKey(test.ts)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("prepareKey() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestPruneBatch(t *testing.T) {
	tests := []struct {
		name  string
		batch []*mrpb.TimeSeries
		want  []*mrpb.TimeSeries
	}{
		{
			name:  "EmptyBatch",
			batch: []*mrpb.TimeSeries{},
			want:  nil,
		},
		{
			name: "SingleTS",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
		},
		{
			name: "MultipleTSWithDifferentMetricTypes",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "appengine.googleapis.com/http/server/response_latencies",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "appengine.googleapis.com/http/server/response_latencies",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
		},
		{
			name: "MultipleTSWithDifferentMetricKinds",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
		},
		{
			name: "MultipleTSWithDifferentMetricLabels",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abcd": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abcd": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_DELTA,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
		},
		{
			name: "MultipleTSWithDifferentResourceLabels",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abcd": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abcd": "def", "sample": "labels"},
					},
				},
			},
		},
		{
			name: "MultipleTSWithIdenticalTS",
			batch: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"sample": "labels", "abc": "def", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "external.googleapis.com/prometheus/up",
						Labels: map[string]string{"sample": "labels", "abc": "def", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
			want: []*mrpb.TimeSeries{
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "custom.googleapis.com/invoice/paid/amount",
						Labels: map[string]string{"abc": "def", "sample": "labels", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"sample": "labels", "abc": "def"},
					},
				},
				&mrpb.TimeSeries{
					Metric: &metricpb.Metric{
						Type:   "external.googleapis.com/prometheus/up",
						Labels: map[string]string{"sample": "labels", "abc": "def", "sample2": "labels2"},
					},
					MetricKind: metricpb.MetricDescriptor_CUMULATIVE,
					Resource: &monitoredresourcepb.MonitoredResource{
						Type:   "datastream.googleapis.com/Stream",
						Labels: map[string]string{"abc": "def", "sample": "labels"},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := pruneBatch(test.batch)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("pruneBatch() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}
