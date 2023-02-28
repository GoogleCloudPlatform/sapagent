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
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

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
			got := ConnectToDatabases(context.Background(), test.params)

			if len(got) != test.want {
				t.Errorf("ConnectToDatabases(%#v) returned unexpected database count, got: %d, want: %d", test.params, len(got), test.want)
			}
		})
	}
}
