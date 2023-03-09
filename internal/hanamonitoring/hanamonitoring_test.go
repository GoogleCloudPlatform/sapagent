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
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func fakeQueryFunc(context.Context, string, ...any) (*sql.Rows, error) {
	return &sql.Rows{}, nil
}

func fakeQueryFuncError(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, cmpopts.AnyError
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
	queryFunc := fakeQueryFuncError
	query := &cpb.Query{
		Columns: []*cpb.Column{
			&cpb.Column{},
		},
	}
	var timeout, sampleInterval int64 = 1, 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)

	got := queryAndSend(ctx, queryFunc, query, timeout, sampleInterval)
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
					ValueType: cpb.ValueType_VALUE_DISTRIBUTION,
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
				GCEService: &fake.TestGCE{
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
				GCEService: &fake.TestGCE{
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
