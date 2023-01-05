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

package hostmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonboulle/clockwork"
	"github.com/google/go-cmp/cmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func TestRequestHandler_ReturnsXML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	requestHandler(w, req)
	got := w.Body.String()

	want := "<metrics></metrics>"

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("requestHandler returned unexpected diff (-want +got):\n%s\n  want:\n%s\n  got:\n%s", diff, want, got)
	}
}

func TestStartSAPHostAgentProvider(t *testing.T) {
	at := agenttime.New(clockwork.NewFakeClock())
	tests := []struct {
		name   string
		params Parameters
		want   bool
	}{
		{
			name: "succeeds",
			params: Parameters{
				Config: &cpb.Configuration{
					ProvideSapHostAgentMetrics: true,
				},
				AgentTime: *at,
			},
			want: true,
		},
		{
			name: "failsDueToParams",
			params: Parameters{
				Config: &cpb.Configuration{
					ProvideSapHostAgentMetrics: false,
				},
				AgentTime: *at,
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := StartSAPHostAgentProvider(context.Background(), test.params)
			if got != test.want {
				t.Errorf("StartSAPHostAgentProvider(ProvideSapHostAgentMetrics: %t) = %t, want: %t", test.params.Config.ProvideSapHostAgentMetrics, got, test.want)
			}
		})
	}
}
