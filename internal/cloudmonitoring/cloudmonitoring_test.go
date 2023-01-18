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
