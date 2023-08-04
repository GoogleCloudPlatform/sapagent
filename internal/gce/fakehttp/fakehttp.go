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

// Package fakehttp provides a HTTP server to serve hard-coded responses for unit tests.
package fakehttp

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"

	"google3/third_party/sapagent/shared/log"
)

// FakeServer defines a fake HTTP server returning a set of hard-coded responses.
type FakeServer struct {
	Responses []HardCodedResponse
}

// HardCodedResponse defines a static response from a FakeServer.
type HardCodedResponse struct {
	StatusCode         int    // Optional - HTTP status code eg. http.StatusOK
	RequestEscapedPath string // Optional - if present compare to request URL
	RequestBody        string // Optional - if present compare to request body
	RequestMethod      string // Optional - method, default GET
	Response           []byte // Response to send back to client
}

func (f *FakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Logger.Fatalf("Error reading body")
	}
	body := strings.TrimSuffix(string(bodyBytes), "\n")
	log.Logger.Infof("RequestURL: %v", string(r.URL.EscapedPath()))
	log.Logger.Infof("RequestBODY: %v", body)
	for _, resp := range f.Responses {
		if r.Method == resp.RequestMethod &&
			string(r.URL.EscapedPath()) == resp.RequestEscapedPath &&
			strings.Contains(body, resp.RequestBody) {
			log.Logger.Infof("Serving matching response for method=%q path=%q body=%q: %d %s",
				resp.RequestMethod, resp.RequestEscapedPath, body, resp.StatusCode, string(resp.Response))
			if resp.StatusCode == 0 {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(resp.StatusCode)
			}
			w.Write(resp.Response)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	errorMsg := fmt.Sprintf("No matching response for method=%s path=%s body=%s",
		r.Method, string(r.URL.EscapedPath()), body)
	log.Logger.Warn(errorMsg)
	w.Write([]byte(errorMsg))
}

// NewServer returns a new FakeServer with a new web server object.
func NewServer(r []HardCodedResponse) *httptest.Server {
	log.SetupLoggingForTest()
	f := &FakeServer{Responses: r}
	return httptest.NewServer(http.Handler(f))
}
