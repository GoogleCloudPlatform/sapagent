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
	"testing"
)

func TestConnectFailure(t *testing.T) {
	p := Params{
		Username: "fakeUser",
		Password: "fakePass",
		Host:     "fakeHost",
		Port:     "fakePort",
	}
	_, err := Connect(p)
	if err == nil {
		t.Errorf("Connect(%#v) = nil, want any error", p)
	}
}

func TestConnectValidatesDriver(t *testing.T) {
	// Connect() with empty arguments will still be able to validate the hdb driver and create a *sql.DB.
	// A call to Query() with this returned *sql.DB would encounter a ping error.
	p := Params{}
	_, err := Connect(p)
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
			if _, err := Connect(test.p); err == nil {
				t.Errorf("Connect(%#v) = nil, want any error", test.p)
			}
		})
	}
}
