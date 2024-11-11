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

package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2"
)

type (
	mockToken struct {
		token *oauth2.Token
		err   error
	}
)

func (m *mockToken) Token() (*oauth2.Token, error) {
	return m.token, m.err
}

func TestToken(t *testing.T) {
	tests := []struct {
		name        string
		tokenGetter defaultTokenGetter
		wantErr     error
	}{
		{
			name: "ErrorToken",
			tokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
				return &mockToken{
					token: nil,
					err:   cmpopts.AnyError,
				}, cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			tokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
				return &mockToken{
					token: &oauth2.Token{
						AccessToken: "access-token",
					},
					err: nil,
				}, nil
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := token(ctx, tc.tokenGetter)
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("token(%v) returned diff (-want +got):\n%s", tc.tokenGetter, diff)
			}
		})
	}
}

func TestGetResponseWithURLVariations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/success":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"key": "success_value"}`)
		case "/test/error":
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		case "/test/illegal_bytes":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, []byte{0xFE, 0x0F})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	testCases := []struct {
		name    string
		r       *Rest
		method  string
		baseURL string
		wantErr error
	}{
		{
			name:    "RequestCreationFailure",
			method:  "INVALID",
			baseURL: fmt.Sprintf("%c", 0x7f),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TokenErr",
			r: &Rest{
				HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
				TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
					return &mockToken{
						token: nil,
						err:   cmpopts.AnyError,
					}, cmpopts.AnyError
				},
			},
			method:  "GET",
			baseURL: ts.URL + "/test/error",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "RequestError",
			r: &Rest{
				HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
				TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
					return &mockToken{
						token: &oauth2.Token{
							AccessToken: "access-token",
						},
						err: nil,
					}, nil
				},
			},
			method:  "GET",
			baseURL: ts.URL + "/test/error",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Rest{
				HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
				TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
					return &mockToken{
						token: &oauth2.Token{
							AccessToken: "access-token",
						},
						err: nil,
					}, nil
				},
			},
			baseURL: ts.URL + "/test/success",
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.r.GetResponse(ctx, tc.method, tc.baseURL, nil)
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("GetResponse(%v, %v) returned diff (-want +got):\n%s", tc.method, tc.baseURL, diff)
			}
		})
	}
}
