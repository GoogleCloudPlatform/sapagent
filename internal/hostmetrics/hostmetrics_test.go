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
	"sync"
	"testing"
	"time"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/jonboulle/clockwork"
	"github.com/google/go-cmp/cmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func TestRequestHandler_ReturnsXML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	requestHandler(w, req)
	got := w.Body.String()
	want := metricsXML

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
					ProvideSapHostAgentMetrics: wpb.Bool(true),
				},
				AgentTime: *at,
			},
			want: true,
		},
		{
			name: "failsDueToParams",
			params: Parameters{
				Config: &cpb.Configuration{
					ProvideSapHostAgentMetrics: wpb.Bool(false),
				},
				AgentTime: *at,
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func(s string) { metricsXML = s }(metricsXML)
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
			defer cancel()
			got := StartSAPHostAgentProvider(ctx, cancel, test.params)
			if got != test.want {
				t.Errorf("StartSAPHostAgentProvider(ProvideSapHostAgentMetrics: %t) = %t, want: %t", test.params.Config.ProvideSapHostAgentMetrics.GetValue(), got, test.want)
			}
		})
	}
}

func TestCollectHostMetrics_shouldBeatAccordingToHeartbeatSpec(t *testing.T) {
	testData := []struct {
		name         string
		beatInterval time.Duration
		timeout      time.Duration
		want         int
	}{
		{
			name:         "cancel before beat",
			beatInterval: time.Second * 100,
			timeout:      time.Millisecond * 100,
			want:         1,
		},
		{
			name:         "1 beat timeout",
			beatInterval: time.Millisecond * 500,
			timeout:      time.Millisecond * 520,
			want:         2,
		},
		{
			name:         "2 beat timeout",
			beatInterval: time.Millisecond * 500,
			timeout:      time.Millisecond * 1020,
			want:         3,
		},
	}
	for _, test := range testData {
		t.Run(test.name, func(t *testing.T) {
			defer func(s string) { metricsXML = s }(metricsXML)
			ctx, cancel := context.WithTimeout(context.Background(), test.timeout)
			defer cancel()
			got := 0
			lock := sync.Mutex{}
			at := agenttime.New(clockwork.NewFakeClock())
			params := Parameters{
				AgentTime: *at,
				HeartbeatSpec: &heartbeat.Spec{
					BeatFunc: func() {
						lock.Lock()
						defer lock.Unlock()
						got++
					},
					Interval: test.beatInterval,
				},
			}
			collectHostMetrics(ctx, params)
			lock.Lock()
			defer lock.Unlock()
			if got != test.want {
				t.Errorf("collectHostMetrics() heartbeat mismatch got %d, want %d", got, test.want)
			}
		})
	}
}
