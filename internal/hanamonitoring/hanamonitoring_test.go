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
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	gcefake "github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultParams = Parameters{
		Config: &configpb.Configuration{
			CloudProperties: &ipb.CloudProperties{
				ProjectId:  "test-project",
				Zone:       "test-zone",
				InstanceId: "123456",
			},
			HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
				Enabled: true,
			},
		},
		GCEService: &gcefake.TestGCE{
			GetSecretResp: []string{"fakePassword"},
			GetSecretErr:  []error{nil},
		},
		BackOffs: cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
	}
	defaultParamsWithExpMetrics = Parameters{
		Config: &configpb.Configuration{
			CloudProperties: &ipb.CloudProperties{
				ProjectId:  "test-project",
				Zone:       "test-zone",
				InstanceId: "123456",
			},
			CollectionConfiguration: &configpb.CollectionConfiguration{
				CollectExperimentalMetrics: true,
			},
			HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
				Enabled: true,
				Queries: []*configpb.Query{
					&configpb.Query{SampleIntervalSec: 5},
				},
			},
		},
	}
	defaultTimestamp = &tspb.Timestamp{Seconds: 123}
	defaultDb        = &database{
		queryFunc: fakeQueryFuncError,
		instance:  &configpb.HANAInstance{Password: "fakePassword"},
	}
	defaultQuery = &configpb.Query{
		Columns: []*configpb.Column{
			&configpb.Column{},
		},
	}
)

func newDefaultCumulativeMetric(st, et int64) *mrpb.TimeSeries {
	return &mrpb.TimeSeries{
		MetricKind: mpb.MetricDescriptor_CUMULATIVE,
		Resource: &mrespb.MonitoredResource{
			Type: "gce_instance",
			Labels: map[string]string{
				"project_id":  "test-project",
				"zone":        "test-zone",
				"instance_id": "123456",
			},
		},
		Points: []*mrpb.Point{
			{
				Interval: &cpb.TimeInterval{
					StartTime: tspb.New(time.Unix(st, 0)),
					EndTime:   tspb.New(time.Unix(et, 0)),
				},
				Value: &cpb.TypedValue{},
			},
		},
	}
}

func fakeHRCSucessPrimary(ctx context.Context, user, sid, instID string) (int, []string, int64, error) {
	return 1, []string{"random"}, 1, nil
}

func fakeHRCSucessSecondary(ctx context.Context, user, sid, instID string) (int, []string, int64, error) {
	return 2, []string{"random"}, 2, nil
}

func fakeHRCSucessError(ctx context.Context, user, sid, instID string) (int, []string, int64, error) {
	return 0, []string{"random"}, 0, errors.New("fake error")
}

func fakeHRCSuccessForStandAlone(ctx context.Context, user, sid, instID string) (int, []string, int64, error) {
	return 0, []string{"random"}, 1, nil
}

func newTimeSeriesKey(metricType, metricLabels string) timeSeriesKey {
	tsk := timeSeriesKey{
		MetricKind:   mpb.MetricDescriptor_CUMULATIVE.String(),
		MetricType:   metricType,
		MetricLabels: metricLabels,
	}
	return tsk
}

func fakeQueryFunc(context.Context, string, commandlineexecutor.Execute) (*databaseconnector.QueryResults, error) {
	return &databaseconnector.QueryResults{}, nil
}

func fakeQueryFuncError(context.Context, string, commandlineexecutor.Execute) (*databaseconnector.QueryResults, error) {
	return nil, cmpopts.AnyError
}

func newDefaultMetrics() *mrpb.TimeSeries {
	return &mrpb.TimeSeries{
		MetricKind: mpb.MetricDescriptor_GAUGE,
		Resource: &mrespb.MonitoredResource{
			Type: "gce_instance",
			Labels: map[string]string{
				"project_id":  "test-project",
				"zone":        "test-zone",
				"instance_id": "123456",
			},
		},
		Points: []*mrpb.Point{
			{
				Interval: &cpb.TimeInterval{
					StartTime: tspb.New(time.Unix(123, 0)),
					EndTime:   tspb.New(time.Unix(123, 0)),
				},
				Value: &cpb.TypedValue{},
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
				Config: &configpb.Configuration{},
			},
			want: false,
		},
		{
			name: "FailsWithDisabled",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: false,
					},
				},
			},
			want: false,
		},
		{
			name: "FailsWithEmptyQueries",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
					},
				},
			},
			want: false,
		},
		{
			name: "FailsWithEmptyDatabases",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*configpb.Query{
							&configpb.Query{},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "FailedToFetchSID",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*configpb.Query{
							&configpb.Query{SampleIntervalSec: 5},
						},
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{Password: "fakePassword"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Succeeds",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*configpb.Query{
							&configpb.Query{SampleIntervalSec: 5},
						},
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{Password: "fakePassword", Sid: "fakeSID"},
						},
					},
				},
			}, want: true,
		},
		{
			name: "SuceedsWithQueriesToRunDefined_WithRunAllTrue",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*configpb.Query{
							&configpb.Query{SampleIntervalSec: 5},
						},
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{
								Password: "fakePassword",
								Sid:      "fakeSID",
								QueriesToRun: &configpb.QueriesToRun{
									RunAll: true,
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "SuceedsWithQueriesToRunDefined_WithRunAllFalse",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						Enabled: true,
						Queries: []*configpb.Query{
							&configpb.Query{SampleIntervalSec: 5, Name: "fakeQueryName"},
							&configpb.Query{SampleIntervalSec: 5, Name: "fakeQueryName2"},
						},
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{
								Password: "fakePassword",
								Sid:      "fakeSID",
								QueriesToRun: &configpb.QueriesToRun{
									RunAll:     false,
									QueryNames: []string{"fakeQueryName", "InvalidQueryName"},
								},
							},
						},
					},
				},
			},
			want: true,
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
	// We test that the queryAndSend() workflow returns an error and retries or cancels
	// the query based on the if the query results in an authentication error.
	tests := []struct {
		name        string
		opts        queryOptions
		isCancelled bool
		want        bool
		wantErr     error
	}{
		{
			name: "queryRetried",
			opts: queryOptions{
				db:        defaultDb,
				query:     defaultQuery,
				params:    defaultParams,
				wp:        workerpool.New(1),
				failCount: 0,
			},
			want:    true,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "queryRetriedWithExperimentalMetrics",
			opts: queryOptions{
				db:        defaultDb,
				query:     defaultQuery,
				params:    defaultParamsWithExpMetrics,
				wp:        workerpool.New(1),
				failCount: 0,
			},
			want:    true,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "queryCancelled",
			opts: queryOptions{
				db:     defaultDb,
				query:  defaultQuery,
				params: defaultParams,
				wp:     workerpool.New(1),
			},
			isCancelled: true,
			want:        false,
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "authFailure",
			opts: queryOptions{
				db:     defaultDb,
				query:  defaultQuery,
				params: defaultParams,
				wp:     workerpool.New(1),
				isAuthErrorFunc: func(err error) bool {
					return true
				},
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "nonAuthFailure",
			opts: queryOptions{
				db:     defaultDb,
				query:  defaultQuery,
				params: defaultParams,
				wp:     workerpool.New(1),
				isAuthErrorFunc: func(err error) bool {
					return false
				},
			},
			want:    true,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			if test.isCancelled {
				cancel()
			}

			got, gotErr := queryAndSend(ctx, test.opts)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("queryAndSend(%#v) = %v want: %v.", test.opts, gotErr, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("queryAndSend(%#v) = %t want: %t", test.opts, got, test.want)
			}
		})
	}
}

func TestCreateColumns(t *testing.T) {
	tests := []struct {
		name string
		cols []*configpb.Column
		want []any
	}{
		{
			name: "EmptyColumns",
			cols: nil,
			want: nil,
		},
		{
			name: "ColumnsWithMultipleTypes",
			cols: []*configpb.Column{
				&configpb.Column{
					ValueType: configpb.ValueType_VALUE_BOOL,
				},
				&configpb.Column{
					ValueType: configpb.ValueType_VALUE_STRING,
				},
				&configpb.Column{
					ValueType: configpb.ValueType_VALUE_INT64,
				},
				&configpb.Column{
					ValueType: configpb.ValueType_VALUE_DOUBLE,
				},
				&configpb.Column{
					ValueType: configpb.ValueType_VALUE_UNSPECIFIED,
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
		query     *configpb.Query
		want      error
	}{
		{
			name:  "FailsWithNilQuery",
			query: nil,
			want:  cmpopts.AnyError,
		},
		{
			name: "FailsWithNilColumns",
			query: &configpb.Query{
				Columns: nil,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailsWithQueryError",
			query: &configpb.Query{
				Columns: []*configpb.Column{
					&configpb.Column{},
				},
			},
			queryFunc: fakeQueryFuncError,
			want:      cmpopts.AnyError,
		},
		{
			name: "Succeeds",
			query: &configpb.Query{
				Columns: []*configpb.Column{
					&configpb.Column{},
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
	// For go-hdb driver: Connecting to a database with empty user, host and port arguments will still be able to validate the driver and create a Database Handle.
	// For command-line based access: Connecting to a database needs the SID and HDBUserstore key.
	tests := []struct {
		name    string
		params  Parameters
		want    int
		wantErr error
	}{
		{
			name: "ConnectValidatesDriver",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{Password: "fakePassword"},
							&configpb.HANAInstance{Password: "fakePassword"},
							&configpb.HANAInstance{Password: "fakePassword"}},
					},
				},
			},
			want: 3,
		},
		{
			name: "ConnectFailsEmptyInstance",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "ConnectFailsPassword",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{
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
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{
								User:       "fakeUser",
								Host:       "fakeHost",
								Port:       "fakePort",
								SecretName: "fakeSecretName",
							},
						},
					},
				},
				GCEService: &gcefake.TestGCE{
					GetSecretResp: []string{"fakePassword"},
					GetSecretErr:  []error{nil},
				},
			},
			want: 0,
		},
		{
			name: "SecretNameFailsToReadNoDBConnection",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{
								SecretName: "fakeSecretName",
							},
						},
					},
				},
				GCEService: &gcefake.TestGCE{
					GetSecretResp: []string{"fakePassword"},
					GetSecretErr:  []error{errors.New("error")},
				},
			},
			want: 0,
		},
		{
			name: "HANAMonitoringConfigNotSet",
			params: Parameters{
				Config: &configpb.Configuration{},
			},
			want: 0,
		},
		{
			name: "ConnectViaHDBUserstoreKey",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{Sid: "fakeSID", HdbuserstoreKey: "fakeKey"},
							&configpb.HANAInstance{Sid: "fakeSID", HdbuserstoreKey: "fakeKey"},
							&configpb.HANAInstance{Sid: "fakeSID", HdbuserstoreKey: "fakeKey"}},
					},
				},
			},
			want: 3,
		},
		{
			name: "ConnectViaHDBUserstoreKeyFailsNoSID",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{HdbuserstoreKey: "fakeKey"}},
					},
				},
			},
			want: 0,
		},
		{
			name: "ConnectViaHDBUserstoreKeyFailsNoKey",
			params: Parameters{
				Config: &configpb.Configuration{
					HanaMonitoringConfiguration: &configpb.HANAMonitoringConfiguration{
						HanaInstances: []*configpb.HANAInstance{
							&configpb.HANAInstance{Sid: "fakeSID"}},
					},
				},
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
	query := &configpb.Query{
		Name: "testQuery",
		Columns: []*configpb.Column{
			{ValueType: configpb.ValueType_VALUE_INT64, Name: "testColInt", MetricType: configpb.MetricType_METRIC_GAUGE},
			{ValueType: configpb.ValueType_VALUE_DOUBLE, Name: "testColDouble", MetricType: configpb.MetricType_METRIC_GAUGE},
			{ValueType: configpb.ValueType_VALUE_BOOL, Name: "testColBool", MetricType: configpb.MetricType_METRIC_GAUGE},
			{ValueType: configpb.ValueType_VALUE_STRING, Name: "stringLabel", MetricType: configpb.MetricType_METRIC_LABEL},
			{ValueType: configpb.ValueType_VALUE_STRING, Name: "stringLabel2", MetricType: configpb.MetricType_METRIC_LABEL},
			{ValueType: configpb.ValueType_VALUE_DOUBLE, Name: "testColDouble2", MetricType: configpb.MetricType_METRIC_CUMULATIVE},
			// Add a misconfigured column (STRING cannot be GAUGE. This would be caught in the config validator) to kill mutants.
			{ValueType: configpb.ValueType_VALUE_STRING, Name: "misconfiguredCol", MetricType: configpb.MetricType_METRIC_GAUGE},
		},
	}
	cols := make([]any, len(query.Columns))
	cols[0], cols[1], cols[2], cols[3], cols[4], cols[5], cols[6] = new(int64), new(float64), new(bool), new(string), new(string), new(float64), new(string)

	runningSum := make(map[timeSeriesKey]prevVal)
	tsKey := newTimeSeriesKey("workload.googleapis.com/sap/hanamonitoring/testQuery/testColDouble2", "instance_name:testName,sid:testSID,stringLabel2:,stringLabel:")
	runningSum[tsKey] = prevVal{val: float64(123.456), startTime: &tspb.Timestamp{Seconds: 0}}

	wantMetrics := 4
	got := createMetricsForRow(context.Background(), "testName", "testSID", query, cols, defaultParams, runningSum)
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

// For the following test, QueryResults.ReadRow() requires pointers in order to populate the column values.
// These values will eventually be passed to createGaugeMetric(). Simulate this behavior by creating pointers and populating them with a value.
func TestCreateGaugeMetric(t *testing.T) {
	tests := []struct {
		name       string
		column     *configpb.Column
		val        any
		want       *mrpb.TimeSeries
		wantMetric *mpb.Metric
		wantValue  *cpb.TypedValue
	}{
		{
			name:       "Int",
			column:     &configpb.Column{ValueType: configpb.ValueType_VALUE_INT64, Name: "testCol"},
			val:        proto.Int64(123),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_Int64Value{Int64Value: 123}},
		},
		{
			name:       "Double",
			column:     &configpb.Column{ValueType: configpb.ValueType_VALUE_DOUBLE, Name: "testCol"},
			val:        proto.Float64(123.456),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_DoubleValue{DoubleValue: 123.456}},
		},
		{
			name:       "BoolWithNameOverride",
			column:     &configpb.Column{ValueType: configpb.ValueType_VALUE_BOOL, Name: "testCol", NameOverride: "override/metric/path"},
			val:        proto.Bool(true),
			want:       newDefaultMetrics(),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/override/metric/path", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_BoolValue{BoolValue: true}},
		},
		{
			name:   "Fails",
			column: &configpb.Column{ValueType: configpb.ValueType_VALUE_STRING, Name: "testCol"},
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

func TestCreateCumulativeMetric(t *testing.T) {
	tests := []struct {
		name       string
		column     *configpb.Column
		val        any
		want       *mrpb.TimeSeries
		runningSum map[timeSeriesKey]prevVal
		wantMetric *mpb.Metric
		wantValue  *cpb.TypedValue
	}{
		{
			name:       "FirstValueInCumulativeTimeSeriesInt",
			column:     &configpb.Column{ValueType: configpb.ValueType_VALUE_INT64, Name: "testCol", MetricType: configpb.MetricType_METRIC_CUMULATIVE},
			val:        proto.Int64(123),
			runningSum: map[timeSeriesKey]prevVal{},
			want:       nil,
			wantMetric: nil,
			wantValue:  nil,
		},
		{
			name:       "FirstValueInCumulativeTimeSeriesDouble",
			column:     &configpb.Column{ValueType: configpb.ValueType_VALUE_DOUBLE, Name: "testCol", MetricType: configpb.MetricType_METRIC_CUMULATIVE},
			val:        proto.Float64(123.23),
			runningSum: map[timeSeriesKey]prevVal{},
			want:       nil,
			wantMetric: nil,
			wantValue:  nil,
		},
		{
			name:   "KeyAlreadyExistInCumulativeTimeSeries",
			column: &configpb.Column{ValueType: configpb.ValueType_VALUE_INT64, Name: "testCol", MetricType: configpb.MetricType_METRIC_CUMULATIVE},
			val:    proto.Int64(123),
			runningSum: map[timeSeriesKey]prevVal{
				newTimeSeriesKey("workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", "abc:def"): prevVal{val: int64(123), startTime: &tspb.Timestamp{Seconds: 0}},
			},
			want:       newDefaultCumulativeMetric(0, 123),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_Int64Value{Int64Value: 246}},
		},
		{
			name:   "CumulativeTimeSeriesDouble",
			column: &configpb.Column{ValueType: configpb.ValueType_VALUE_DOUBLE, Name: "testCol", MetricType: configpb.MetricType_METRIC_CUMULATIVE},
			val:    proto.Float64(123.23),
			runningSum: map[timeSeriesKey]prevVal{
				newTimeSeriesKey("workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", "abc:def"): prevVal{val: float64(123.23), startTime: &tspb.Timestamp{Seconds: 0}},
			},
			want:       newDefaultCumulativeMetric(0, 123),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_DoubleValue{DoubleValue: 246.46}},
		},
		{
			name:   "IntWithNameOverride",
			column: &configpb.Column{ValueType: configpb.ValueType_VALUE_INT64, Name: "testCol", MetricType: configpb.MetricType_METRIC_CUMULATIVE, NameOverride: "override/path"},
			val:    proto.Int64(123),
			runningSum: map[timeSeriesKey]prevVal{
				newTimeSeriesKey("workload.googleapis.com/sap/hanamonitoring/override/path", "abc:def"): prevVal{val: int64(123), startTime: &tspb.Timestamp{Seconds: 0}},
			},
			want:       newDefaultCumulativeMetric(0, 123),
			wantMetric: &mpb.Metric{Type: "workload.googleapis.com/sap/hanamonitoring/override/path", Labels: map[string]string{"abc": "def"}},
			wantValue:  &cpb.TypedValue{Value: &cpb.TypedValue_Int64Value{Int64Value: 246}},
		},
		{
			name:   "Fails",
			column: &configpb.Column{ValueType: configpb.ValueType_VALUE_STRING, Name: "testCol"},
			val:    proto.String("test"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.want != nil {
				test.want.Metric = test.wantMetric
				test.want.Points[0].Value = test.wantValue
			}
			got, _ := createCumulativeMetric(context.Background(), test.column, test.val, map[string]string{"abc": "def"}, "testQuery", defaultParams, defaultTimestamp, test.runningSum)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("createCumulativeMetric(%#v) unexpected diff: (-want +got):\n%s", test.column, diff)
			}
		})
	}
}

func TestPrepareTimeSeriesKey(t *testing.T) {
	tests := []struct {
		name         string
		metricType   string
		metricKind   string
		metricLabels map[string]string
		want         timeSeriesKey
	}{
		{
			name:         "PrepareKey",
			metricType:   "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol",
			metricKind:   mpb.MetricDescriptor_CUMULATIVE.String(),
			metricLabels: map[string]string{"sample": "labels", "abc": "def"},
			want: timeSeriesKey{
				MetricKind:   mpb.MetricDescriptor_CUMULATIVE.String(),
				MetricType:   "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol",
				MetricLabels: "abc:def,sample:labels",
			},
		},
		{
			name:         "PrepareKeyWithDifferentOrderLabels",
			metricType:   "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol",
			metricKind:   mpb.MetricDescriptor_CUMULATIVE.String(),
			metricLabels: map[string]string{"abc": "def", "sample": "labels"},
			want: timeSeriesKey{
				MetricKind:   mpb.MetricDescriptor_CUMULATIVE.String(),
				MetricType:   "workload.googleapis.com/sap/hanamonitoring/testQuery/testCol",
				MetricLabels: "abc:def,sample:labels",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := prepareKey(test.metricType, test.metricKind, test.metricLabels)
			if got != test.want {
				t.Errorf("prepareKey() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCreateQueryTimeTakenMetric(t *testing.T) {
	dbName := "testDb"
	sid := "testSID"
	query := &configpb.Query{
		Name: "testQuery",
	}
	timeTaken := 2
	ts := tspb.Now()
	ctx := context.Background()
	createQueryResponseTimeMetric(ctx, dbName, sid, query, defaultParams, int64(timeTaken), ts)
}

func TestMatchQyeryAndInstanceType(t *testing.T) {
	tests := []struct {
		name    string
		opts    queryOptions
		testHRC hanaReplicationConfig
		want    bool
	}{
		{
			name: "InstanceNotMarkedLocal",
			opts: queryOptions{
				db: defaultDb,
			},
			want: true,
		},
		{
			name: "InstanceNumberMissing",
			opts: queryOptions{
				db: &database{instance: &configpb.HANAInstance{IsLocal: true}},
			},
			want: true,
		},
		{
			name: "QueryRunOnUnspecified",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_RUN_ON_UNSPECIFIED,
				},
			},
			want: true,
		},
		{
			name: "QuerySpecifiedRunOnAll",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_ALL,
				},
			},
			want: true,
		},
		{
			name: "ErrorInGettingInstanceRole",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_PRIMARY,
				},
				params: Parameters{HRC: fakeHRCSucessError},
			},
			want: false,
		},
		{
			name: "QueryRunOnPrimary",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_PRIMARY,
				},
				params: Parameters{HRC: fakeHRCSucessPrimary},
			},
			want: true,
		},
		{
			name: "QueryRunOnSecondary",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_SECONDARY,
				},
				params: Parameters{
					HRC: fakeHRCSucessSecondary,
				},
			},
			want: true,
		},
		{
			name: "QueryMisMatchedOnInstanceType",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_PRIMARY,
				},
				params: Parameters{
					HRC: fakeHRCSucessSecondary,
				},
			},
			want: false,
		},
		{
			name: "StandAloneInstance",
			opts: queryOptions{
				db: &database{
					instance: &configpb.HANAInstance{
						IsLocal:     true,
						InstanceNum: "00",
					},
				},
				query: &configpb.Query{
					Name:  "testQuery",
					RunOn: configpb.RunOn_PRIMARY,
				},
				params: Parameters{
					HRC: fakeHRCSuccessForStandAlone,
				},
			},
			want: true,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchQueryAndInstanceType(ctx, tc.opts)
			if got != tc.want {
				t.Errorf("matchQyeryAndInstanceType(%v) = %v, want: %v", tc.opts, got, tc.want)
			}
		})
	}
}

func TestCollectExpiementalMetrics(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   bool
	}{
		{
			name:   "CollectExpiementalMetrics",
			params: defaultParamsWithExpMetrics,
			want:   true,
		},
		{
			name:   "NoExpMetrics",
			params: defaultParams,
			want:   false,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := collectExpiementalMetrics(ctx, tc.params)
			if got != tc.want {
				t.Errorf("collectExpiementalMetrics(%v) = %v, want: %v", tc.params, got, tc.want)
			}
		})
	}
}
