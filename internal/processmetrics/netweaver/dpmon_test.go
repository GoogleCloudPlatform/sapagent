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

package netweaver

import (
	_ "embed"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

//go:embed dpmon_output/abap_sessions.txt
var abapSessionsOutput string // Has Plgs column enabled.

//go:embed dpmon_output/abap_sessions_no_plugin.txt
var abapSessionsNoPluginOutput string // Default: Plgs column disabled.

func TestParseABAPSessionStats(t *testing.T) {
	tests := []struct {
		name             string
		dpmonOutput      string
		wantSessionCount map[string]int
		wantTotalCount   int
		wantErr          error
	}{
		{
			name:        "SuccessFullOutput",
			dpmonOutput: abapSessionsOutput,
			wantSessionCount: map[string]int{
				"ASYNC_RFC": 1,
				"BATCH":     1,
				"SYNC_RFC":  1,
			},
			wantTotalCount: 3,
		},
		{
			name:        "SuccessOutputWithoutPlugin",
			dpmonOutput: abapSessionsNoPluginOutput,
			wantSessionCount: map[string]int{
				"ASYNC_RFC": 1,
				"BATCH":     1,
				"SYNC_RFC":  1,
			},
			wantTotalCount: 3,
		},
		{
			name:        "NoActiveSession",
			dpmonOutput: "SomeTextWithNoSessions",
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotSessionCount, gotTotalCount, gotErr := parseABAPSessionStats(test.dpmonOutput)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parseABAPSessionStats() error got=%v, want=%v", gotErr, test.wantErr)
			}

			if diff := cmp.Diff(test.wantSessionCount, gotSessionCount); diff != "" {
				t.Errorf("parseABAPSessionStats() sessionCount mismatch (-want, +got):\n%s", diff)
			}

			if test.wantTotalCount != gotTotalCount {
				t.Errorf("parseABAPSessionStats() totalCount got=%d, want=%d", gotTotalCount, test.wantTotalCount)
			}
		})
	}
}

//go:embed dpmon_output/rfc_connections.txt
var rfcConnectionsOutput string

func TestParseRFCStats(t *testing.T) {
	tests := []struct {
		name              string
		dpmonOutput       string
		wantRFCStateCount map[string]int
	}{
		{
			name:        "SuccessFullOutput",
			dpmonOutput: rfcConnectionsOutput,
			wantRFCStateCount: map[string]int{
				"server_allocated": 2,
				"client_allocated": 1,
				"server":           2,
				"client":           1,
			},
		},
		{
			name: "OneMalformedRow",
			dpmonOutput: `| 148|I SHOULD| NOT BE HERE |
|57|92243243|92243243SU27870_M0|T104_U27870_M0_I|ALLOCATED|SERVER|0| 75|Mon Nov 14 00:01:39 2022|`,
			wantRFCStateCount: map[string]int{
				"server_allocated": 1,
				"server":           1,
			},
		},
		{
			name:        "ClientOnly",
			dpmonOutput: `|57|92243243|92243243SU27870_M0|T104_U27870_M0_I|ALLOCATED|CLIENT|0| 75|Mon Nov 14 00:01:39 2022|`,
			wantRFCStateCount: map[string]int{
				"client_allocated": 1,
				"client":           1,
			},
		},
		{
			name:        "ServerOnly",
			dpmonOutput: `|57|92243243|92243243SU27870_M0|T104_U27870_M0_I|ALLOCATED|SERVER|0| 75|Mon Nov 14 00:01:39 2022|`,
			wantRFCStateCount: map[string]int{
				"server_allocated": 1,
				"server":           1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRFCStateCount := parseRFCStats(test.dpmonOutput)

			if diff := cmp.Diff(test.wantRFCStateCount, gotRFCStateCount); diff != "" {
				t.Errorf("parseRFCStats() rfcStateCount mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
