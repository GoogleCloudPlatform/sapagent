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

package metadataserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func marshalResponse(t *testing.T, r metadataServerResponse) string {
	s, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(s)
}

func mockMetadataServer(t *testing.T, handler endpoint) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if h, _ := r.Header["Metadata-Flavor"]; len(h) != 1 || h[0] != "Google" {
			w.WriteHeader(403)
			fmt.Fprint(w, "Metadata-flavor header missing")
		}
		if r.URL.Path != cloudPropertiesURI && r.URL.Path != maintenanceEventURI && r.URL.Path != upcomingMaintenanceURI && !strings.HasPrefix(r.URL.Path, diskType) {
			w.WriteHeader(404)
			fmt.Fprint(w, "404 Page not found")
		}

		if (r.URL.Path == upcomingMaintenanceURI) && (handler.responseBody == metadataNoUpcomingMaintenanceResponse) {
			w.WriteHeader(503)
			fmt.Fprint(w, metadataNoUpcomingMaintenanceResponse)
			return
		}

		if (r.URL.Path == upcomingMaintenanceURI) && (handler.responseBody == "error") {
			w.WriteHeader(502)
			fmt.Fprint(w, "Some other error")
			return
		}

		if handler.contentLength != "" {
			w.Header().Set("Content-Length", handler.contentLength)
			return
		}
		if handler.responseBody == "error" {
			w.WriteHeader(404)
			fmt.Fprint(w, "404 Page not found")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(handler.responseBody)))
		w.WriteHeader(200)
		fmt.Fprint(w, handler.responseBody)
	}))
	return ts
}

type endpoint struct {
	uri           string
	contentLength string
	responseBody  string
}

func testBackOffPolicy() backoff.BackOff {
	return backoff.WithMaxRetries(&backoff.ZeroBackOff{}, 1)
}

func TestGet(t *testing.T) {
	tests := []struct {
		name      string
		url       endpoint
		want      []byte
		wantError error
	}{
		{
			name:      "badRequest",
			url:       endpoint{uri: "/unsupported"},
			wantError: cmpopts.AnyError,
		},
		{
			name:      "cannotReadResponse",
			url:       endpoint{uri: cloudPropertiesURI, contentLength: "1", responseBody: ""},
			wantError: cmpopts.AnyError,
		},
		{
			name: "success",
			url:  endpoint{uri: cloudPropertiesURI, responseBody: "successful response"},
			want: []byte("successful response"),
		},

		{
			name: "upcomingMaintenance",
			url:  endpoint{uri: upcomingMaintenanceURI, responseBody: metadataNoUpcomingMaintenanceResponse},
			want: []byte(metadataNoUpcomingMaintenanceResponse),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := mockMetadataServer(t, test.url)
			defer ts.Close()
			metadataServerURL = ts.URL

			got, err := get(test.url.uri, "")
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("get() response body mismatch (-want, +got):\n%s", d)
			}
			if d := cmp.Diff(test.wantError, err, cmpopts.EquateErrors()); d != "" {
				t.Errorf("get() error mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestCloudPropertiesWithRetry(t *testing.T) {
	tests := []struct {
		name string
		url  endpoint
		want *instancepb.CloudProperties
	}{
		{
			name: "missingProjectID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				}),
			},
			want: nil,
		},
		{
			name: "missingNumericProjectID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project"},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				}),
			},
			want: nil,
		},
		{
			name: "missingInstanceID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "missingZone",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "nonMatchingZone",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "missingInstanceName",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "success",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:          101,
						Zone:        "projects/test-project/zones/test-zone",
						Name:        "test-instance-name",
						Image:       "test-image",
						MachineType: "projects/test-project/machineTypes/test-machine-type",
					},
				})},
			want: &instancepb.CloudProperties{
				ProjectId:        "test-project",
				NumericProjectId: "1",
				InstanceId:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            "test-image",
				MachineType:      "test-machine-type",
			},
		},
		{
			name: "unknownImageAndMachineType",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:   101,
						Zone: "projects/test-project/zones/test-zone",
						Name: "test-instance-name",
					},
				})},
			want: &instancepb.CloudProperties{
				ProjectId:        "test-project",
				NumericProjectId: "1",
				InstanceId:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            ImageUnknown,
				MachineType:      MachineTypeUnknown,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := mockMetadataServer(t, test.url)
			defer ts.Close()
			metadataServerURL = ts.URL

			got := CloudPropertiesWithRetry(testBackOffPolicy())
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("CloudProperties() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestReadCloudPropertiesWithRetry(t *testing.T) {
	tests := []struct {
		name string
		url  endpoint
		want *CloudProperties
	}{
		{
			name: "missingProjectID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				}),
			},
			want: nil,
		},
		{
			name: "missingNumericProjectID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project"},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				}),
			},
			want: nil,
		},
		{
			name: "missingInstanceID",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						Zone:  "projects/test-project/zones/test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "missingZone",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "nonMatchingZone",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "test-zone",
						Name:  "test-instance-name",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "missingInstanceName",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:    101,
						Zone:  "projects/test-project/zones/test-zone",
						Image: "test-image",
					},
				})},
			want: nil,
		},
		{
			name: "success",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:          101,
						Zone:        "projects/test-project/zones/test-zone",
						Name:        "test-instance-name",
						Image:       "test-image",
						MachineType: "projects/test-project/machineTypes/test-machine-type",
					},
				})},
			want: &CloudProperties{
				ProjectID:        "test-project",
				NumericProjectID: "1",
				InstanceID:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            "test-image",
				MachineType:      "test-machine-type",
			},
		},
		{
			name: "unknownImageAndMachineType",
			url: endpoint{
				uri: cloudPropertiesURI,
				responseBody: marshalResponse(t, metadataServerResponse{
					Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
					Instance: instanceInfo{
						ID:   101,
						Zone: "projects/test-project/zones/test-zone",
						Name: "test-instance-name",
					},
				})},
			want: &CloudProperties{
				ProjectID:        "test-project",
				NumericProjectID: "1",
				InstanceID:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            ImageUnknown,
				MachineType:      MachineTypeUnknown,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := mockMetadataServer(t, test.url)
			defer ts.Close()
			metadataServerURL = ts.URL

			got := ReadCloudPropertiesWithRetry(testBackOffPolicy())
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("CloudProperties() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestDiskTypeWithRetry(t *testing.T) {
	tests := []struct {
		name string
		url  endpoint
		want string
	}{
		{
			name: "success",
			url: endpoint{
				uri:          fmt.Sprintf("%s%s/type", diskType, "any"),
				responseBody: "anyType",
			},
			want: "anyType",
		},
		{
			name: "invalid diskType",
			url: endpoint{
				uri:          fmt.Sprintf("%s%s/type", diskType, "any"),
				responseBody: "error",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := mockMetadataServer(t, test.url)
			defer ts.Close()
			metadataServerURL = ts.URL

			got := DiskTypeWithRetry(testBackOffPolicy(), "any")
			if test.want != got {
				t.Errorf("DiskTypeWithRetry()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestFetchCloudProperties(t *testing.T) {
	url := endpoint{
		uri: cloudPropertiesURI,
		responseBody: marshalResponse(t, metadataServerResponse{
			Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
			Instance: instanceInfo{
				ID:          101,
				Zone:        "projects/test-project/zones/test-zone",
				Name:        "test-instance-name",
				Image:       "test-image",
				MachineType: "projects/test-project/machineTypes/test-machine-type",
			},
		})}
	want := &CloudProperties{
		ProjectID:        "test-project",
		NumericProjectID: "1",
		InstanceID:       "101",
		Zone:             "test-zone",
		InstanceName:     "test-instance-name",
		Image:            "test-image",
		MachineType:      "test-machine-type",
	}

	ts := mockMetadataServer(t, url)
	defer ts.Close()
	metadataServerURL = ts.URL

	got := FetchCloudProperties()
	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("FetchCloudProperties() mismatch (-want, +got):\n%s", d)
	}
}

func TestFetchGCEMaintenanceEvent(t *testing.T) {
	endpoint := endpoint{uri: maintenanceEventURI, responseBody: "NONE"}

	ts := mockMetadataServer(t, endpoint)
	defer ts.Close()
	metadataServerURL = ts.URL

	want := endpoint.responseBody
	got, _ := FetchGCEMaintenanceEvent()
	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("get() response body mismatch (-want, +got):\n%s", d)
	}
}
func TestFetchGCEUpcomingMaintenance(t *testing.T) {
	var maintenanceBody = `can_reschedule false
latest_window_start_time 2024-07-29T14:19:53+00:00
maintenance_status PENDING
type SCHEDULED
window_end_time 2024-07-29T18:19:52+00:00
window_start_time 2024-07-29T14:19:57+00:00`
	tests := []struct {
		name      string
		url       endpoint
		want      string
		wantError error
	}{
		{
			"upcoming_maintenance",
			endpoint{uri: upcomingMaintenanceURI, responseBody: maintenanceBody},
			maintenanceBody,
			nil,
		},

		{
			"no_maintenance",
			endpoint{uri: upcomingMaintenanceURI, responseBody: metadataNoUpcomingMaintenanceResponse},
			metadataNoUpcomingMaintenanceResponse,
			nil,
		},

		{
			"unknown_error",
			endpoint{uri: upcomingMaintenanceURI, responseBody: "error"},
			"",
			cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		ts := mockMetadataServer(t, test.url)
		defer ts.Close()
		metadataServerURL = ts.URL
		want := test.want
		got, err := FetchGCEUpcomingMaintenance()
		if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
			t.Errorf("TestFetchGCEUpcomingMaintenance(%s) response body mismatch (-want, +got):\n%s", test.name, d)
		}
		if d := cmp.Diff(test.wantError, err, cmpopts.EquateErrors()); d != "" {
			t.Errorf("TestFetchGCEUpcomingMaintenance(%s) error mismatch (-want, +got):\n%s", test.name, d)
		}
	}
}
