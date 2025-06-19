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
	"net/http"
	"testing"

	"golang.org/x/oauth2"
)

type mockHTTPClient struct {
	responses map[string]map[string]httpResponse
}

type httpResponse struct {
	statusCode int
	body       string
}

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
		Body:       http.NoBody,
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
