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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
)

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
