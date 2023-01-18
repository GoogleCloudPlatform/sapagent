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

// Package fake implements test fakes for the cloudmonitoring wrappers.
package fake

import (
	"context"

	"github.com/googleapis/gax-go/v2"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// TimeSeriesCreator is a fake which implements the TimeSeriesCreator interface.
type TimeSeriesCreator struct {
	Calls []*monitoringpb.CreateTimeSeriesRequest
	Err   error
}

func (f *TimeSeriesCreator) CreateTimeSeries(ctx context.Context, req *monitoringpb.CreateTimeSeriesRequest, opts ...gax.CallOption) error {
	f.Calls = append(f.Calls, req)
	return f.Err
}

// TimeSeriesQuerier is a fake which implements the TimeSeriesQuerier interface.
type TimeSeriesQuerier struct {
	Calls []*monitoringpb.QueryTimeSeriesRequest
	Err   error
	TS    []*mrpb.TimeSeriesData
}

func (f *TimeSeriesQuerier) QueryTimeSeries(ctx context.Context, req *monitoringpb.QueryTimeSeriesRequest, ops ...gax.CallOption) ([]*mrpb.TimeSeriesData, error) {
	f.Calls = append(f.Calls, req)
	return f.TS, f.Err
}
