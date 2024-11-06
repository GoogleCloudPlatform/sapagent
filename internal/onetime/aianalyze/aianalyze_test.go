/*
Copyright 2024 Google LLC

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

package aianalyze

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"

	cpb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultCloudProps = &ipb.CloudProperties{
		ProjectId:    "test-project",
		Zone:         "test-zone-a",
		InstanceName: "test-instance",
	}
)

type (
	mockRestService struct {
		getResponseResp []byte
		getResponseErr  error
	}
)

func (m *mockRestService) NewRest() {}

func (m *mockRestService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	return m.getResponseResp, m.getResponseErr
}

func TestSupportAnalyzerHandler(t *testing.T) {
	tests := []struct {
		name       string
		a          *AiAnalyzer
		wantResult string
		wantStatus subcommands.ExitStatus
	}{
		{
			name: "FailureValidation",
			a: &AiAnalyzer{
				Sid:     "",
				Project: "test-project",
				Region:  "test-region",
			},
			wantResult: "Error while validating parameters",
			wantStatus: subcommands.ExitUsageError,
		},
		{
			name: "FailureOverview",
			a: &AiAnalyzer{
				Sid:     "test-sid",
				Project: "test-project",
				Region:  "test-region",
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			wantResult: "Error while getting overview",
			wantStatus: subcommands.ExitFailure,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.a.oteLogger = onetime.CreateOTELogger(false)
			gotResult, gotStatus := tc.a.supportAnalyzerHandler(ctx, &onetime.RunOptions{CloudProperties: defaultCloudProps})
			if gotResult != tc.wantResult {
				t.Errorf("supportAnalyzerHandler() returned unexpected result: %v, want: %v", gotResult, tc.wantResult)
			}
			if gotStatus != tc.wantStatus {
				t.Errorf("supportAnalyzerHandler() returned unexpected status: %v, want: %v", gotStatus, tc.wantStatus)
			}
		})
	}
}

func TestProtosToJSONList(t *testing.T) {
	tests := []struct {
		name        string
		contentList []*cpb.Content
		want        []byte
		wantErr     error
	}{
		{
			name: "Failure",
			contentList: []*cpb.Content{
				&cpb.Content{
					Role:  "user",
					Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: string([]byte{0xff, 0xfe, 0xfd})}}},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			contentList: []*cpb.Content{
				&cpb.Content{
					Role:  "user",
					Parts: []*cpb.Part{&cpb.Part{Data: &cpb.Part_Text{Text: "Test Prompt"}}},
				},
			},
			want: []byte(`{"contents":[{"role":"user","parts":[{"text":"Test Prompt"}]}]}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := protosToJSONList(context.Background(), test.contentList)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("protosToJSONList(%v) returned unexpected error: %v, want: %v", test.contentList, err, test.wantErr)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("protosToJSONList(%v) returned unexpected diff (-want +got):\n%s", test.contentList, diff)
			}
		})
	}
}

func TestGetOverview(t *testing.T) {
	tests := []struct {
		name    string
		a       *AiAnalyzer
		wantErr error
	}{
		{
			name: "Success",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt"}]}}]}]`),
					getResponseErr:  nil,
				},
			},
			wantErr: nil,
		},
		{
			name: "Failure",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			err := test.a.getOverview(context.Background())
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getOverview() returned unexpected error: %v, want: %v", err, test.wantErr)
			}
		})
	}
}

func TestGenerateContentREST(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "success") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt"}]}}]}]`)
		} else if strings.Contains(r.URL.Path, "error") {
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		} else if strings.Contains(r.URL.Path, "unmarshal_error") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"Candidate": [{"content": "incorrect_field"}]`)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name    string
		a       *AiAnalyzer
		data    []byte
		want    []generateContentResponse
		wantErr error
	}{
		{
			name: "GetResponseError",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseErr: cmpopts.AnyError,
				},
			},
			data:    []byte(`{"contents":[{"role":"user","parts":[{"text":"Test prompt?"}]}]}`),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UnmarshalError",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`{"Candidate": [{"content": "incorrect_field"}]`),
					getResponseErr:  nil,
				},
			},
			data:    []byte(`{"contents":[{"role":"user","parts":[{"text":"Test Prompt?"}]}]}`),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			a: &AiAnalyzer{
				rest: &mockRestService{
					getResponseResp: []byte(`[{"candidates": [{"content": {"role": "user", "parts": [{"text": "Test prompt answer"}]}}]}]`),
					getResponseErr:  nil,
				},
			},
			data: []byte(`{"contents":[{"role":"user","parts":[{"text":"Test prompt?"}]}]}`),
			want: []generateContentResponse{
				generateContentResponse{
					Candidates: []candidate{
						candidate{
							Content: content{
								Role:  "user",
								Parts: []part{part{Text: "Test prompt answer"}},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.a.oteLogger = onetime.CreateOTELogger(false)
			got, err := test.a.generateContentREST(context.Background(), test.data)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("generateContentREST(%v) returned unexpected error: %v, want: %v", test.data, err, test.wantErr)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("generateContentREST(%v) returned unexpected diff (-want +got):\n%s", test.data, diff)
			}
		})
	}
}

func TestValidateParametersErrors(t *testing.T) {
	tests := []struct {
		name    string
		a       *AiAnalyzer
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "NoSID",
			a: &AiAnalyzer{
				Sid:     "",
				Project: "test-project",
				Region:  "test-region",
			},
			cp:      defaultCloudProps,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoProject",
			a: &AiAnalyzer{
				Sid:     "test-sid",
				Project: "",
			},
			cp:      defaultCloudProps,
			wantErr: nil,
		},
		{
			name: "NoRegionInvalid",
			a: &AiAnalyzer{
				Sid:     "test-sid",
				Project: "test-project",
			},
			cp: &ipb.CloudProperties{
				ProjectId:  "test-project",
				Zone:       "invalid-zone",
				InstanceId: "test-instance",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoRegionValid",
			a: &AiAnalyzer{
				Sid:     "test-sid",
				Project: "test-project",
			},
			cp:      defaultCloudProps,
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.a.validateParameters(ctx, tc.cp)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("validateParameters(%v, %v) returned unexpected diff (-want +got):\n%s", tc.a, tc.cp, diff)
			}
		})
	}
}
