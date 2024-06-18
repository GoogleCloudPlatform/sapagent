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

package performancediagnosticshandler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/performancediagnostics"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestBuildPerformanceDiagnosticsCommand(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.AgentCommand
		want    performancediagnostics.Diagnose
	}{
		{
			name: "FullyPopulated",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"type":                "test-type",
					"test-bucket":         "test-test-bucket",
					"backint-config-file": "test-backint-param-file",
					"output-bucket":       "test-result-bucket",
					"output-file-name":    "test-bundle-name",
					"output-file-path":    "test-path",
					"hyper-threading":     "test-hyper-threading",
					"loglevel":            "debug",
				},
			},
			want: performancediagnostics.Diagnose{
				Type:              "test-type",
				TestBucket:        "test-test-bucket",
				BackintConfigFile: "test-backint-param-file",
				OutputBucket:      "test-result-bucket",
				OutputFileName:    "test-bundle-name",
				OutputFilePath:    "test-path",
				HyperThreading:    "test-hyper-threading",
				LogLevel:          "debug",
			},
		},
		{
			name: "EmptyValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"scope":           "",
					"test-bucket":     "",
					"param-file":      "",
					"result-bucket":   "",
					"bundle-name":     "",
					"path":            "",
					"hyper-threading": "",
					"loglevel":        "",
				},
			},
			want: performancediagnostics.Diagnose{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPerformanceDiagnosticsCommand(tc.command)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(performancediagnostics.Diagnose{})); diff != "" {
				t.Errorf("buildPerformanceDiagnosticsCommand(%v) returned an unexpected diff (-want +got):\n%v", tc.command, diff)
			}
		})
	}
}
