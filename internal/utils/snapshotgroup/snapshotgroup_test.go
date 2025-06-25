/*
Copyright 2025 Google LLC

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

package snapshotgroup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2"
)

type (
	mockHTTPClient struct {
		responses map[string]map[string]httpResponse
	}
	httpResponse struct {
		statusCode int
		body       string
	}
)

func (c *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	verbResponses, ok := c.responses[req.Method]
	if !ok {
		return nil, fmt.Errorf("unexpected verb: %s", req.Method)
	}
	resp, ok := verbResponses[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected URL: %s", req.URL.String())
	}
	return newResponse(resp.statusCode, resp.body), nil
}

func newResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// mockTokenSource is a mock implementation of oauth2.TokenSource.
type mockTokenSource struct {
	AccessToken string
	Err         error
}

func (mts *mockTokenSource) Token() (*oauth2.Token, error) {
	if mts.Err != nil {
		return nil, mts.Err
	}
	return &oauth2.Token{AccessToken: mts.AccessToken}, nil
}

func defaultTokenGetterMock(err error) func(context.Context, ...string) (oauth2.TokenSource, error) {
	return func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
		if err != nil {
			return nil, err
		}
		return &mockTokenSource{AccessToken: "test-token"}, nil
	}
}

func TestNewService(t *testing.T) {
	s := &SGService{}
	err := s.NewService()
	if err != nil {
		t.Fatalf("NewService() failed: %v", err)
	}
	if s.rest == nil {
		t.Error("NewService() did not initialize rest client")
	}
	if s.backoff == nil {
		t.Error("NewService() did not initialize backoff")
	}
	if s.maxRetries == 0 {
		t.Error("NewService() did not initialize maxRetries")
	}
}

func TestSetupBackoff(t *testing.T) {
	var b backoff.BackOff
	b = &backoff.ExponentialBackOff{
		InitialInterval:     2 * time.Second,
		RandomizationFactor: 0,
		Multiplier:          2,
		MaxInterval:         1 * time.Hour,
		MaxElapsedTime:      30 * time.Minute,
		Clock:               backoff.SystemClock,
	}
	gotB := setupBackoff()
	if diff := cmp.Diff(b, gotB, cmpopts.IgnoreUnexported(backoff.ExponentialBackOff{})); diff != "" {
		t.Errorf("setupBackoff() returned diff (-want +got):\n%s", diff)
	}
}

func TestGetResponse(t *testing.T) {
	tests := []struct {
		name           string
		httpResponses  map[string]map[string]httpResponse
		method         string
		url            string
		data           []byte
		expectedBody   string
		expectedError  bool
		tokenGetterErr error
	}{
		{
			name: "success",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://test.com/success": {statusCode: 200, body: `{"key":"value"}`}},
			},
			method:       "GET",
			url:          "https://test.com/success",
			expectedBody: `{"key":"value"}`,
		},
		{
			name: "http_error",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://test.com/error": {statusCode: 500, body: `{"error":{"code":500,"message":"server error"}}`}},
			},
			method:        "GET",
			url:           "https://test.com/error",
			expectedError: true, // GetResponse now returns the googleapi.Error as error
		},
		{
			name: "unmarshal_error_generic_response",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://test.com/unmarshal_error": {statusCode: 200, body: `invalid_json`}},
			},
			method:        "GET",
			url:           "https://test.com/unmarshal_error",
			expectedError: true,
		},
		{
			name: "unmarshal_error_googleapi_error",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://test.com/unmarshal_google_error": {statusCode: 400, body: `{"error": "not_an_object"}`}},
			},
			method:        "GET",
			url:           "https://test.com/unmarshal_google_error",
			expectedError: true,
		},
		{
			name:           "token_getter_error",
			httpResponses:  map[string]map[string]httpResponse{},
			method:         "GET",
			url:            "https://test.com/any",
			expectedError:  true,
			tokenGetterErr: fmt.Errorf("token error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sgService := &SGService{}
			sgService.NewService() // Initialize with default backoff and retries
			sgService.rest.HTTPClient = &mockHTTPClient{responses: test.httpResponses}
			sgService.rest.TokenGetter = defaultTokenGetterMock(test.tokenGetterErr)

			body, err := sgService.GetResponse(ctx, test.method, test.url, test.data)

			if (err != nil) != test.expectedError {
				t.Errorf("GetResponse() error = %v, wantErr %v", err, test.expectedError)
				return
			}
			if err == nil && string(body) != test.expectedBody {
				t.Errorf("GetResponse() body = %s, want %s", string(body), test.expectedBody)
			}
		})
	}
}

func TestGetSG(t *testing.T) {
	tests := []struct {
		name           string
		project        string
		sgName         string
		httpResponses  map[string]map[string]httpResponse
		expectedSGItem *SGItem
		expectedError  bool
		tokenGetterErr error
	}{
		{
			name:    "success",
			project: "test-project",
			sgName:  "test-sg",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups/test-sg": {statusCode: 200, body: `{"name":"test-sg", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg"}`}},
			},
			expectedSGItem: &SGItem{
				Name: "test-sg",
			},
		},
		{
			name:    "http_error",
			project: "test-project",
			sgName:  "test-sg",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups/test-sg": {statusCode: 500, body: `{"error":{"code":500,"message":"server error"}}`}},
			},
			expectedError: true,
		},
		{
			name:    "unmarshal_error",
			project: "test-project",
			sgName:  "test-sg",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups/test-sg": {statusCode: 200, body: `invalid_json`}},
			},
			expectedError: true,
		},
		{
			name:           "token_getter_error",
			project:        "test-project",
			sgName:         "test-sg",
			httpResponses:  map[string]map[string]httpResponse{},
			expectedError:  true,
			tokenGetterErr: fmt.Errorf("token error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sgService := &SGService{}
			sgService.NewService()
			sgService.rest.HTTPClient = &mockHTTPClient{responses: test.httpResponses}
			sgService.rest.TokenGetter = defaultTokenGetterMock(test.tokenGetterErr)

			sgItem, err := sgService.GetSG(ctx, test.project, test.sgName)

			if (err != nil) != test.expectedError {
				t.Errorf("GetSG() error = %v, wantErr %v", err, test.expectedError)
				return
			}
			if diff := cmp.Diff(test.expectedSGItem, sgItem); diff != "" && !test.expectedError {
				t.Errorf("GetSG() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListSGs(t *testing.T) {
	tests := []struct {
		name            string
		project         string
		httpResponses   map[string]map[string]httpResponse
		expectedSGItems []SGItem
		expectedError   bool
		tokenGetterErr  error
	}{
		{
			name:    "success",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 200, body: `{"items":[{"name":"test-sg1", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg1"},{"name":"test-sg2", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg2"}]}`}},
			},
			expectedSGItems: []SGItem{
				{Name: "test-sg1"},
				{Name: "test-sg2"},
			},
		},
		{
			name:    "success_with_pagination",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {
					"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups":                           {statusCode: 200, body: `{"items":[{"name":"test-sg1", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg1"}], "nextPageToken":"next-page-token"}`},
					"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups?pageToken=next-page-token": {statusCode: 200, body: `{"items":[{"name":"test-sg2", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg2"}]}`},
				},
			},
			expectedSGItems: []SGItem{
				{Name: "test-sg1"},
				{Name: "test-sg2"},
			},
		},
		{
			name:    "http_error",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 500, body: `{"error":{"code":500,"message":"server error"}}`}},
			},
			expectedError: true,
		},
		{
			name:    "unmarshal_error",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"GET": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 200, body: `invalid_json`}},
			},
			expectedError: true,
		},
		{
			name:           "token_getter_error",
			project:        "test-project",
			httpResponses:  map[string]map[string]httpResponse{},
			expectedError:  true,
			tokenGetterErr: fmt.Errorf("token error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sgService := &SGService{}
			sgService.NewService()
			sgService.rest.HTTPClient = &mockHTTPClient{responses: test.httpResponses}
			sgService.rest.TokenGetter = defaultTokenGetterMock(test.tokenGetterErr)

			sgItems, err := sgService.ListSGs(ctx, test.project)

			if (err != nil) != test.expectedError {
				t.Errorf("ListSGs() error = %v, wantErr %v", err, test.expectedError)
				return
			}
			if diff := cmp.Diff(test.expectedSGItems, sgItems); diff != "" && !test.expectedError {
				t.Errorf("ListSGs() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateSG(t *testing.T) {
	tests := []struct {
		name            string
		s               *SGService
		project         string
		data            []byte
		httpResponses   map[string]map[string]httpResponse
		expectedError   bool
		expectedBaseURL string
	}{
		// TODO: Add a success test.
		{
			name:    "error_http",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"POST": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 500, body: `{"error":{"code":500,"message":"server error"}}`}},
			},
			expectedError:   true,
			expectedBaseURL: "https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups",
		},
		{
			name:    "error_unmarshal_operation",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"POST": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 200, body: `invalid_json`}},
			},
			expectedError:   true,
			expectedBaseURL: "https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups",
		},
		{
			name:    "error_operation_error",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"POST": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 200, body: `{"name":"operation-123", "error": {"errors": [{"message": "failed to create"}]}}`}},
			},
			expectedError:   true,
			expectedBaseURL: "https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups",
		},
		{
			name:    "error_operation_error_http_code",
			project: "test-project",
			httpResponses: map[string]map[string]httpResponse{
				"POST": {"https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups": {statusCode: 200, body: `{"name":"operation-123", "error": {"errors": []}, "httpErrorStatusCode": 400, "httpErrorMessage": "bad request"}`}},
			},
			expectedError:   true,
			expectedBaseURL: "https://compute.googleapis.com/compute/alpha/projects/test-project/global/snapshotGroups",
		},
	}

	for _, test := range tests {
		test.data = []byte(`{"name": "test-sg", "sourceInstantSnapshotGroup":"projects/test-project/zones/us-central1-a/instantSnapshotGroups/test-isg"}`)
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sgService := &SGService{}
			sgService.NewService()
			sgService.maxRetries = 1
			sgService.rest.HTTPClient = &mockHTTPClient{responses: test.httpResponses}

			err := sgService.CreateSG(ctx, test.project, test.data)

			if (err != nil) != test.expectedError {
				t.Errorf("CreateSG() error = %v, wantErr %v", err, test.expectedError)
			}
			if sgService.baseURL != test.expectedBaseURL {
				t.Errorf("CreateSG() baseURL = %s, want %s", sgService.baseURL, test.expectedBaseURL)
			}
		})
	}
}
