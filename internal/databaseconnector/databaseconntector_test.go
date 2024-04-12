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

package databaseconnector

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
)

func valuesFromPointerArray(dest []any) []any {
	values := make([]any, len(dest))
	for i, v := range dest {
		val := reflect.ValueOf(v)
		if val.Kind() == reflect.Ptr {
			values[i] = val.Elem().Interface()
		} else {
			return nil
		}
	}
	return values
}

func TestConnectFailure(t *testing.T) {
	p := Params{
		Username: "fakeUser",
		Password: "fakePass",
		Host:     "fakeHost",
		Port:     "fakePort",
	}
	_, err := Connect(context.Background(), p)
	if err == nil {
		t.Errorf("Connect(%#v) = nil, want any error", p)
	}
}

func TestConnectValidatesDriver(t *testing.T) {
	// Connect() with empty arguments will still be able to validate the hdb driver and create a *sql.DB.
	// A call to Query() with this returned *sql.DB would encounter a ping error.
	p := Params{Password: "fakePass"}
	_, err := Connect(context.Background(), p)
	if err != nil {
		t.Errorf("Connect(%#v) = %v, want nil error", p, err)
	}
}

func TestConnectWithSSLParams(t *testing.T) {
	tests := []struct {
		name    string
		p       Params
		wantErr error
	}{
		{
			name: "EnableSSLOnAndValidateCertificateOn",
			p: Params{
				Username:       "fakeUser",
				Password:       "fakePass",
				Host:           "fakeHost",
				Port:           "fakePort",
				EnableSSL:      true,
				HostNameInCert: "hostname",
				RootCAFile:     "/path",
			},
		},
		{
			name: "EnableSSLOffAndValidateCertificateOn",
			p: Params{
				Username:       "fakeUser",
				Password:       "fakePass",
				Host:           "fakeHost",
				Port:           "fakePort",
				EnableSSL:      false,
				HostNameInCert: "hostname",
				RootCAFile:     "/path",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Connect(context.Background(), test.p); err == nil {
				t.Errorf("Connect(%#v) = nil, want any error", test.p)
			}
		})
	}
}

func TestConnect(t *testing.T) {
	tests := []struct {
		name string
		p    Params
		want error
	}{
		{
			name: "Password",
			p:    Params{Password: "my-pass"},
		},
		{
			name: "PasswordSecret",
			p: Params{
				PasswordSecret: "my-secret",
				GCEService: &fake.TestGCE{
					GetSecretResp: []string{"fakePassword"},
					GetSecretErr:  []error{nil},
				},
			},
		},
		{
			name: "GetSecretFailure",
			p: Params{
				PasswordSecret: "my-secret",
				GCEService: &fake.TestGCE{
					GetSecretResp: []string{""},
					GetSecretErr:  []error{cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "PasswordAndSecret",
			p: Params{
				Password:       "my-pass",
				PasswordSecret: "my-secret",
				GCEService: &fake.TestGCE{
					GetSecretResp: []string{""},
					GetSecretErr:  []error{cmpopts.AnyError},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := Connect(context.Background(), test.p)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("Connect()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestQueryValid(t *testing.T) {
	testCMDDBHandle, _ := NewCMDDBHandle(Params{
		SID:        "testSID",
		HDBUserKey: "testHDBUserKey",
	})
	testQueryOut := `test1, test2
	test3, test4
	test5, test6
	`
	testResultRows := strings.Split(testQueryOut, "\n")
	testName := "ValidQuery"
	testQuery := "TEST VALID 'QUERY'"
	fakeExec := func(ctx context.Context, cmdParams commandlineexecutor.Params) commandlineexecutor.Result {
		if cmdParams.Args[len(cmdParams.Args)-1] == testQuery {
			return commandlineexecutor.Result{
				StdOut:   testQueryOut,
				ExitCode: 0,
			}
		}
		return commandlineexecutor.Result{
			StdOut:   "incorrect result",
			ExitCode: 0,
		}
	}

	t.Run(testName, func(t *testing.T) {
		gotRes, gotErr := testCMDDBHandle.Query(context.Background(), testQuery, fakeExec)
		if gotErr != nil {
			t.Fatalf("Query(%s) returns error=%s, want error=nil", testQuery, gotErr)
		}
		if !cmp.Equal(gotRes.cmdDBResult, testResultRows) {
			t.Errorf("Query(%s).cmdDBResult=%v, want=%v", testQuery, gotRes.cmdDBResult, testResultRows)
		}
	})
}

func TestQueryInvalid(t *testing.T) {
	testCMDDBHandle, _ := NewCMDDBHandle(Params{
		SID:        "testSID",
		HDBUserKey: "testHDBUserKey",
	})

	testName := "InvalidQuery"
	testQuery := "TEST INVALID QUERY"
	fakeExec := func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdErr:   "test error",
			ExitCode: 3, // Exit code for invalid queries
		}
	}

	t.Run(testName, func(t *testing.T) {
		gotRes, gotErr := testCMDDBHandle.Query(context.Background(), testQuery, fakeExec)
		if gotRes != nil || gotErr == nil {
			t.Errorf("Query(%s)=(%#v, %v), want=(nil, any error)", testQuery, gotRes, gotErr)
		}
	})
}

func TestReadRow(t *testing.T) {
	tests := []struct {
		name      string
		sqlResult []string
		wantDest  [][]any
		wantErr   []error
	}{
		{
			name: "ValidResult",
			sqlResult: []string{
				`1,"test 1",1.1,TRUE,"2000-01-23"`,
				`2,"test 2",1.2,FALSE,4`,
			},
			wantDest: [][]any{
				[]any{int64(1), "test 1", float64(1.1), true, "2000-01-23"},
				[]any{int64(2), "test 2", float64(1.2), false, "4"},
			},
			wantErr: []error{nil, nil},
		},
		{
			name: "ValidResultNullValues",
			sqlResult: []string{
				`1,"test1",1.1,TRUE,"2000-01-23"`,
				`2,?,1.2,TRUE,"2000-02-23"`,
				`?,"test3",1.3,FALSE,"2000-03-23"`,
				`4,"test4",?,TRUE,"2000-04-23"`,
				`5,"test5",1.5,?,"2000-05-23"`,
				`6,"test6",1.6,TRUE,?`,
			},
			wantDest: [][]any{
				[]any{int64(1), "test1", float64(1.1), true, "2000-01-23"},
				[]any{int64(2), "", float64(1.2), true, "2000-02-23"},
				[]any{int64(0), "test3", float64(1.3), false, "2000-03-23"},
				[]any{int64(4), "test4", float64(0.0), true, "2000-04-23"},
				[]any{int64(5), "test5", float64(1.5), false, "2000-05-23"},
				[]any{int64(6), "test6", float64(1.6), true, ""},
			},
			wantErr: []error{nil, nil, nil, nil, nil, nil, nil},
		},
		{
			name: "ValidResultWithDelimLiteral",
			sqlResult: []string{
				`1,"test,1",1.1,TRUE,"2000-01-23"`,
				`2,"test,2",1.2,FALSE,"2000-02-23"`,
			},
			wantDest: [][]any{
				[]any{int64(1), "test,1", float64(1.1), true, "2000-01-23"},
				[]any{int64(2), "test,2", float64(1.2), false, "2000-02-23"},
			},
			wantErr: []error{nil, nil},
		},
		{
			name: "InvalidResultIncorrectColumnCount",
			sqlResult: []string{
				`1,"test1","2000-01-23"`,
				`2,"test2","2000-02-23"`,
			},
			wantDest: [][]any{nil, nil},
			wantErr:  []error{cmpopts.AnyError, cmpopts.AnyError},
		},
		{
			name: "InvalidResultInvalidColumnType",
			sqlResult: []string{
				`1,"test1","Incorrect type",TRUE,"2000-01-23"`,
				`3.4,"test2",1.2,FALSE,"2000-02-23"`,
			},
			wantDest: [][]any{nil, nil},
			wantErr:  []error{cmpopts.AnyError, cmpopts.AnyError},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qr := QueryResults{
				useCMD:           true,
				cmdDBResult:      tc.sqlResult,
				cmdDBResultIndex: -1,
			}
			dest := []any{new(int64), new(string), new(float64), new(bool), new(any)}
			parsedResults := [][]any{}
			gotErrs := []error{}
			for qr.Next() {
				gotErr := qr.ReadRow(dest...)
				gotErrs = append(gotErrs, gotErr)
				values := valuesFromPointerArray(dest)
				parsedResults = append(parsedResults, values)
			}

			if len(parsedResults) != len(tc.wantDest) {
				t.Fatalf("ReadRow() read: %v rows, want: %v rows", len(parsedResults), len(tc.wantDest))
			}
			for i, result := range parsedResults {
				if !cmp.Equal(gotErrs[i], tc.wantErr[i], cmpopts.EquateErrors()) {
					t.Errorf("ReadRow() at index %d read error: %v, want: %v", i, gotErrs[i], tc.wantErr[i])
					continue
				}
				if gotErrs[i] != nil {
					// Behaviour for error case is undefined. Do not check parsed values
					continue
				}
				if !cmp.Equal(result, tc.wantDest[i]) {
					t.Errorf("ReadRow() at index %d read values: %v, want: %v", i, result, tc.wantDest[i])
				}
			}
		})
	}
}
