/*
Copyright 2023 Google LLC

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

package hanamonitoring

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
	commonpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	gceFake "github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultParams = Parameters{
		Config: &cpb.Configuration{
			CloudProperties: &ipb.CloudProperties{
				ProjectId:  "test-project",
				Zone:       "test-zone",
				InstanceId: "123456",
			},
			HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
				Enabled: true,
			},
		},
		GCEService: &gceFake.TestGCE{
			GetSecretResp: []string{"fakePassword"},
			GetSecretErr:  []error{nil},
		},
		BackOffs: cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
	}
	defaultTimestamp = &tspb.Timestamp{Seconds: 123}
)

func fakeQueryFunc(context.Context, string, ...any) (*sql.Rows, error) {
	return &sql.Rows{}, nil
}

func fakeQueryFuncError(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, cmpopts.AnyError
}

func newDefaultMetrics() *monitoringresourcespb.TimeSeries {
	return &monitoringresourcespb.TimeSeries{
		MetricKind: mpb.MetricDescriptor_GAUGE,
		Resource: &mrpb.MonitoredResource{
			Type: "gce_instance",
			Labels: map[string]string{
				"project_id":  "test-project",
				"zone":        "test-zone",
				"instance_id": "123456",
			},
		},
		Points: []*monitoringresourcespb.Point{
			{
				Interval: &commonpb.TimeInterval{
					StartTime: &tspb.Timestamp{Seconds: 123},
					EndTime:   &tspb.Timestamp{Seconds: 123},
				},
				Value: &commonpb.TypedValue{},
			},
		},
	}
}

func TestStart(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   bool
	}{
		{
			name: "FailsWithEmptyConfig",
			params: Parameters{
				Config: &cpb.Configuration{},
			},
			want: false,
		},
		{
			name: "FailsWithDisabled",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled: false,
					},
				},
			},
			want: false,
		},
		{
			name: "FailsWithEmptyQueries",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled: true,
					},
				},
			},
			want: false,
		},
		{
			name: "FailsWithEmptyDatabases",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*cpb.Query{
							&cpb.Query{},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "Succeeds",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*cpb.Query{
							&cpb.Query{SampleIntervalSec: 5},
						},
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{Password: "fakePassword"},
						},
					},
				},
			}, want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			got := Start(ctx, test.params)
			if got != test.want {
				t.Errorf("Start(%#v) = %t, want: %t", test.params, got, test.want)
			}
		})
	}
}

func TestQueryAndSend(t *testing.T) {
	// fakeQueryFuncError is needed here since a sql.Rows object cannot be easily created outside of the database/sql package.
	// However, we can still test the queryAndSend() workflow loop and test breaking out of it with a context timeout.
	db := &database{
		queryFunc: fakeQueryFuncError,
		instance:  &cpb.HANAInstance{Password: "fakePassword"},
	}
	query := &cpb.Query{
		Columns: []*cpb.Column{
			&cpb.Column{},
		},
	}
	var timeout, sampleInterval int64 = 1, 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)

	got := queryAndSend(ctx, db, query, timeout, sampleInterval, defaultParams)
	want := cmpopts.AnyError
	if !cmp.Equal(got, want, cmpopts.EquateErrors()) {
		t.Errorf("queryAndSend(%v, fakeQueryFuncError, %v, %v, %v) = %v want: %v.", ctx, query, timeout, sampleInterval, got, want)
	}
}

func TestCreateColumns(t *testing.T) {
	tests := []struct {
		name string
		cols []*cpb.Column
		want []any
	}{
		{
			name: "EmptyColumns",
			cols: nil,
			want: nil,
		},
		{
			name: "ColumnsWithMultipleTypes",
			cols: []*cpb.Column{
				&cpb.Column{
					ValueType: cpb.ValueType_VALUE_BOOL,
				},
				&cpb.Column{
					ValueType: cpb.ValueType_VALUE_STRING,
				},
				&cpb.Column{
					ValueType: cpb.ValueType_VALUE_INT64,
				},
				&cpb.Column{
					ValueType: cpb.ValueType_VALUE_DOUBLE,
				},
				&cpb.Column{
					ValueType: cpb.ValueType_VALUE_UNSPECIFIED,
				},
			},
			want: []any{
				new(bool),
				new(string),
				new(int64),
				new(float64),
				new(any),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := createColumns(test.cols)

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("createColumns(%#v) unexpected diff: (-want +got):\n%s", test.cols, diff)
			}
		})
	}
}

func TestQueryDatabase(t *testing.T) {
	tests := []struct {
		name      string
		params    Parameters
		queryFunc queryFunc
		query     *cpb.Query
		want      error
	}{
		{
			name:  "FailsWithNilQuery",
			query: nil,
			want:  cmpopts.AnyError,
		},
		{
			name: "FailsWithNilColumns",
			query: &cpb.Query{
				Columns: nil,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailsWithQueryError",
			query: &cpb.Query{
				Columns: []*cpb.Column{
					&cpb.Column{},
				},
			},
			queryFunc: fakeQueryFuncError,
			want:      cmpopts.AnyError,
		},
		{
			name: "Succeeds",
			query: &cpb.Query{
				Columns: []*cpb.Column{
					&cpb.Column{},
				},
			},
			queryFunc: fakeQueryFunc,
			want:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, got := queryDatabase(context.Background(), test.queryFunc, test.query)

			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("queryDatabase(%#v, %#v) = %v, want: %v", test.queryFunc, test.query, got, test.want)
			}
		})
	}
}

func TestConnectToDatabases(t *testing.T) {
	// Connecting to a database with empty user, host and port arguments will still be able to validate the hdb driver and create a *sql.DB.
	tests := []struct {
		name    string
		params  Parameters
		want    int
		wantErr error
	}{
		{
			name: "ConnectValidatesDriver",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{Password: "fakePassword"},
							&cpb.HANAInstance{Password: "fakePassword"},
							&cpb.HANAInstance{Password: "fakePassword"}},
					},
				},
			},
			want: 3,
		},
		{
			name: "ConnectFailsEmptyInstance",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "ConnectFailsPassword",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{
								User:     "fakeUser",
								Password: "fakePassword",
								Host:     "fakeHost",
								Port:     "fakePort",
							},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "ConnectFailsSecretNameOverride",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{
								User:       "fakeUser",
								Host:       "fakeHost",
								Port:       "fakePort",
								SecretName: "fakeSecretName",
							},
						},
					},
				},
				GCEService: &gceFake.TestGCE{
					GetSecretResp: []string{"fakePassword"},
					GetSecretErr:  []error{nil},
				},
			},
			want: 0,
		},
		{
			name: "SecretNameFailsToRead",
			params: Parameters{
				Config: &cpb.Configuration{
					HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
						HanaInstances: []*cpb.HANAInstance{
							&cpb.HANAInstance{
								SecretName: "fakeSecretName",
							},
						},
					},
				},
				GCEService: &gceFake.TestGCE{
					GetSecretResp: []string{"fakePassword"},
					GetSecretErr:  []error{errors.New("error")},
				},
			},
			want: 1,
		},
		{
			name: "HANAMonitoringConfigNotSet",
			params: Parameters{
				Config: &cpb.Configuration{},
			},
			want: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := connectToDatabases(context.Background(), test.params)

			if len(got) != test.want {
				t.Errorf("ConnectToDatabases(%#v) returned unexpected database count, got: %d, want: %d", test.params, len(got), test.want)
			}
		})
	}
}

func TestCreateMetricsForRow(t *testing.T) {
	// This test simulates a row with several GAUGE metrics (3), a couple LABELs (2).
	// The labels will be appended to each of the gauge metrics, making the number of gauge metrics (3) be the desired want value.
	query := &cpb.Query{
		Name: "testQuery",
		Columns: []*cpb.Column{
			{ValueType: cpb.ValueType_VALUE_INT64, Name: "testColInt", MetricType: cpb.MetricType_METRIC_GAUGE},
			{ValueType: cpb.ValueType_VALUE_DOUBLE, Name: "testColDouble", MetricType: cpb.MetricType_METRIC_GAUGE},
			{ValueType: cpb.ValueType_VALUE_BOOL, Name: "testColBool", MetricType: cpb.MetricType_METRIC_GAUGE},
			{ValueType: cpb.ValueType_VALUE_STRING, Name: "stringLabel", MetricType: cpb.MetricType_METRIC_LABEL},
			{ValueType: cpb.ValueType_VALUE_STRING, Name: "stringLabel2", MetricType: cpb.MetricType_METRIC_LABEL},
			// Add a misconfigured column (STRING cannot be GAUGE. This would be caught in the config validator) to kill mutants.
			{ValueType: cpb.ValueType_VALUE_STRING, Name: "misconfiguredCol", MetricType: cpb.MetricType_METRIC_GAUGE},
		},
	}
	cols := make([]any, len(query.Columns))
	cols[0], cols[1], cols[2], cols[3], cols[4], cols[5] = new(int64), new(float64), new(bool), new(string), new(string), new(string)

	wantMetrics := 3
	got := createMetricsForRow("testName", "testSID", query, cols, defaultParams)
	gotMetrics := len(got)
	if gotMetrics != wantMetrics {
		t.Errorf("createMetricsForRow(%#v) = %d, want metrics length: %d", query, gotMetrics, wantMetrics)
	}

	// 2 correctly configured labels in the column plus 2 default labels for instance_name and sid.
	wantLabels := 4
	gotLabels := 0
	if len(got) > 0 {
		gotLabels = len(got[0].Metric.Labels)
	}
	if gotLabels != wantLabels {
		t.Errorf("createMetricsForRow(%#v) = %d, want labels length: %d", query, gotLabels, wantLabels)
	}
}

// For the following test, sql.Rows.Scan() requires pointers in order to populate the column values.
// These values will eventually be passed to createGaugeMetric(). Simulate this behavior by creating pointers and populating them with a value.
func TestCreateGaugeMetric(t *testing.T) {
	tests := []struct {
		name       string
		column     *cpb.Column
		val        any
		want       *monitoringresourcespb.TimeSeries
		wantMetric *mpb.Metric
		wantValue  *commonpb.TypedValue
	}{
		{
			name:       "Int",
			column:     &cpb.Column{ValueType: cpb.ValueType_VALUE_INT64, Name: "testCol"},
			val:        proto.Int64(123),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &commonpb.TypedValue{Value: &commonpb.TypedValue_Int64Value{Int64Value: 123}},
		},
		{
			name:       "Double",
			column:     &cpb.Column{ValueType: cpb.ValueType_VALUE_DOUBLE, Name: "testCol"},
			val:        proto.Float64(123.456),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &commonpb.TypedValue{Value: &commonpb.TypedValue_DoubleValue{DoubleValue: 123.456}},
		},
		{
			name:       "BoolWithNameOverride",
			column:     &cpb.Column{ValueType: cpb.ValueType_VALUE_BOOL, Name: "testCol", NameOverride: "override/metric/path"},
			val:        proto.Bool(true),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/override/metric/path", Labels: map[string]string{"abc": "def"}},
			wantValue:  &commonpb.TypedValue{Value: &commonpb.TypedValue_BoolValue{BoolValue: true}},
		},
		{
			name:   "Fails",
			column: &cpb.Column{ValueType: cpb.ValueType_VALUE_STRING, Name: "testCol"},
			val:    proto.String("test"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.want != nil {
				test.want.Metric = test.wantMetric
				test.want.Points[0].Value = test.wantValue
			}
			got, _ := createGaugeMetric(test.column, test.val, map[string]string{"abc": "def"}, "testQuery", defaultParams, defaultTimestamp)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("createGaugeMetric(%#v) unexpected diff: (-want +got):\n%s", test.column, diff)
			}
		})
	}
}
