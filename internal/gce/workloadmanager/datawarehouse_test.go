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

package workloadmanager

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/googleapi"
)

func TestWriteInsightDo(t *testing.T) {
	tests := []struct {
		name    string
		request *WriteInsightRequest
		service *Service
		want    *WriteInsightResponse
		wantErr error
	}{{
		name:    "success",
		request: &WriteInsightRequest{},
		service: &Service{SendRequest: func(context.Context, *http.Client, *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
			}, nil
		}},
		want: &WriteInsightResponse{
			googleapi.ServerResponse{
				HTTPStatusCode: http.StatusOK,
			},
		},
		wantErr: nil,
	}, {
		name:    "noContent",
		request: &WriteInsightRequest{},
		service: &Service{SendRequest: func(context.Context, *http.Client, *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNoContent,
			}, nil
		}},
		want: &WriteInsightResponse{
			googleapi.ServerResponse{
				HTTPStatusCode: http.StatusNoContent,
			},
		},
		wantErr: nil,
	}, {
		name:    "notModified",
		request: &WriteInsightRequest{},
		service: &Service{
			SendRequest: func(context.Context, *http.Client, *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNoContent,
				}, nil
			}},
		want: &WriteInsightResponse{
			googleapi.ServerResponse{
				HTTPStatusCode: http.StatusNoContent,
			},
		},
		wantErr: nil,
	}, {
		name:    "sendRequestErr",
		request: &WriteInsightRequest{},
		service: &Service{
			SendRequest: func(context.Context, *http.Client, *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader("{}")),
				}, nil
			}},
		want:    nil,
		wantErr: cmpopts.AnyError,
	}, {
		name:    "bodyErr",
		request: &WriteInsightRequest{},
		service: &Service{
			SendRequest: func(context.Context, *http.Client, *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{something:not\njson{{")),
				}, nil
			}},
		want:    nil,
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := &WriteInsightCall{s: test.service, writeinsightrequest: test.request, urlParams: make(map[string][]string)}
			got, err := call.Do()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("WriteInsight.Do() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("WriteInsight.Do() got error %v, want %v", err, test.wantErr)
			}
		})
	}
}
