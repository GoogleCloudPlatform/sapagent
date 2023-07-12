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

package fakehttp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func doRequest(method string, URI string, body string) (*http.Response, error) {
	if method == "GET" {
		return http.Get(URI)
	}
	if method == "POST" {
		return http.Post(URI, "application/json", bytes.NewBuffer([]byte(body)))
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func TestServeHTTP(t *testing.T) {
	responses := []HardCodedResponse{
		HardCodedResponse{
			StatusCode:         http.StatusOK,
			RequestEscapedPath: "/path",
			RequestBody:        "",
			RequestMethod:      "GET",
			Response:           []byte("response"),
		},
		HardCodedResponse{
			StatusCode:         http.StatusOK,
			RequestEscapedPath: "/restapi",
			RequestBody:        "var: value",
			RequestMethod:      "POST",
			Response:           []byte("response"),
		},
		HardCodedResponse{
			StatusCode:         http.StatusForbidden,
			RequestEscapedPath: "/forbidden",
			RequestBody:        "",
			RequestMethod:      "GET",
			Response:           []byte("response"),
		},
	}
	testCases := []struct {
		URI            string
		body           string
		method         string
		wantStatusCode int
		wantResponse   string
	}{
		{"/path", "", "GET", http.StatusOK, "response"},
		{"/forbidden", "", "GET", http.StatusForbidden, "response"},
		{"/restapi", "var: value", "POST", http.StatusOK, "response"},
		{"/restapi", "var: other", "POST", http.StatusNotFound, ""},
		{"/nonexistant", "", "GET", http.StatusNotFound, ""},
	}
	s := NewServer(responses)
	defer s.Close()
	for _, tc := range testCases {
		u := fmt.Sprintf("%s%s", s.URL, tc.URI)
		res, err := doRequest(tc.method, u, tc.body)
		if err != nil {
			t.Errorf("error calling %s: %+v", u, err)
		}
		if res.StatusCode != tc.wantStatusCode {
			t.Errorf("URI %s got statusCode %d want %d", u, res.StatusCode, tc.wantStatusCode)
		}
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Errorf("error reading response body for %s: %+v", u, err)
		}
		if tc.wantResponse != "" && string(body) != tc.wantResponse {
			t.Errorf("URI %s got response %q want %q", u, string(body), tc.wantResponse)
		}
	}
}
