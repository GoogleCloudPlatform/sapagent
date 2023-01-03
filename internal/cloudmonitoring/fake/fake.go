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

// TimeSeriesLister is a fake which implements the TimeSeriesLister interface.
type TimeSeriesLister struct {
	Calls []*monitoringpb.ListTimeSeriesRequest
	Err   error
	TS    []*mrpb.TimeSeries
}

func (f *TimeSeriesLister) ListTimeSeries(ctx context.Context, req *monitoringpb.ListTimeSeriesRequest, ops ...gax.CallOption) ([]*mrpb.TimeSeries, error) {
	f.Calls = append(f.Calls, req)
	return f.TS, f.Err
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
