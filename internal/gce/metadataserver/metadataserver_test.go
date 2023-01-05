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
	"testing"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
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

func testBackOffPolicy() backoff.BackOff {
	return backoff.WithMaxRetries(&backoff.ZeroBackOff{}, 1)
}

func TestCloudPropertiesWithRetry(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		statusCode    int
		contentLength string
		responseBody  string
		want          *instancepb.CloudProperties
	}{
		{
			name: "badRequest",
			url:  "notAValidURL",
			want: nil,
		},
		{
			name:       "statusNotOK",
			statusCode: 400,
			want:       nil,
		},
		{
			name:          "cannotReadResponse",
			statusCode:    200,
			contentLength: "1",
			responseBody:  "",
			want:          nil,
		},
		{
			name:         "emptyBody",
			statusCode:   200,
			responseBody: "",
			want:         nil,
		},
		{
			name:       "missingProjectID",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{NumericProjectID: 1},
				Instance: instanceInfo{
					ID:    101,
					Zone:  "projects/test-project/zones/test-zone",
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "missingNumericProjectID",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project"},
				Instance: instanceInfo{
					ID:    101,
					Zone:  "projects/test-project/zones/test-zone",
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "missingInstanceID",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					Zone:  "projects/test-project/zones/test-zone",
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "missingZone",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					ID:    101,
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "nonMatchingZone",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					ID:    101,
					Zone:  "test-zone",
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "missingInstanceName",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					ID:    101,
					Zone:  "projects/test-project/zones/test-zone",
					Image: "test-image",
				},
			}),
			want: nil,
		},
		{
			name:       "success",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					ID:    101,
					Zone:  "projects/test-project/zones/test-zone",
					Name:  "test-instance-name",
					Image: "test-image",
				},
			}),
			want: &instancepb.CloudProperties{
				ProjectId:        "test-project",
				NumericProjectId: "1",
				InstanceId:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            "test-image",
			},
		},
		{
			name:       "unknownImage",
			statusCode: 200,
			responseBody: marshalResponse(t, metadataServerResponse{
				Project: projectInfo{ProjectID: "test-project", NumericProjectID: 1},
				Instance: instanceInfo{
					ID:   101,
					Zone: "projects/test-project/zones/test-zone",
					Name: "test-instance-name",
				},
			}),
			want: &instancepb.CloudProperties{
				ProjectId:        "test-project",
				NumericProjectId: "1",
				InstanceId:       "101",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            ImageUnknown,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.contentLength != "" {
					w.Header().Set("Content-Length", test.contentLength)
					return
				}
				w.WriteHeader(test.statusCode)
				fmt.Fprint(w, test.responseBody)
			}))
			defer ts.Close()
			metadataServerURL = ts.URL
			if test.url != "" {
				metadataServerURL = test.url
			}

			got := CloudPropertiesWithRetry(testBackOffPolicy())
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("CloudProperties() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}
