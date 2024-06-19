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

package configurehandler

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestBuildConfigureFromMap(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.AgentCommand
		want    configure.Configure
	}{
		{
			name: "FullyPopulated",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"feature":                               "test_feature",
					"loglevel":                              "debug",
					"setting":                               "test_setting",
					"path":                                  "test_path",
					"process_metrics_to_skip":               "test_skip_metrics",
					"workload_evaluation_metrics_frequency": "30",
					"workload_evaluation_db_metrics_frequency": "25",
					"process_metrics_frequency":                "5",
					"slow_process_metrics_frequency":           "10",
					"agent_metrics_frequency":                  "15",
					"agent_health_frequency":                   "20",
					"heartbeat_frequency":                      "12",
					"reliability_metrics_frequency":            "35",
					"sample_interval_sec":                      "40",
					"query_timeout_sec":                        "45",
					"help":                                     "true",
					"version":                                  "false",
					"enable":                                   "true",
					"disable":                                  "false",
					"showall":                                  "true",
					"add":                                      "false",
					"remove":                                   "true",
				},
			},
			want: configure.Configure{
				Feature:                     "test_feature",
				LogLevel:                    "debug",
				Setting:                     "test_setting",
				Path:                        "test_path",
				SkipMetrics:                 "test_skip_metrics",
				ValidationMetricsFrequency:  30,
				DbFrequency:                 25,
				FastMetricsFrequency:        5,
				SlowMetricsFrequency:        10,
				AgentMetricsFrequency:       15,
				AgentHealthFrequency:        20,
				HeartbeatFrequency:          12,
				ReliabilityMetricsFrequency: 35,
				SampleIntervalSec:           40,
				QueryTimeoutSec:             45,
				Help:                        true,
				Version:                     false,
				Enable:                      true,
				Disable:                     false,
				Showall:                     true,
				Add:                         false,
				Remove:                      true,
				RestartAgent:                noOpRestart,
			},
		},
		{
			name: "Empty",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{},
			},
			want: configure.Configure{
				RestartAgent: noOpRestart,
			},
		},
		{
			name: "IgnoreBadFormattedData",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"path":                                     "test_path",
					"process_metrics_to_skip":                  "test_skip_metrics",
					"workload_evaluation_metrics_frequency":    "not a number",
					"workload_evaluation_db_metrics_frequency": "test bad data",
					"Enable": "not a boolean",
					"Help":   "test bad data",
				},
			},
			want: configure.Configure{
				Path:         "test_path",
				SkipMetrics:  "test_skip_metrics",
				RestartAgent: noOpRestart,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildConfigureFromMap(context.Background(), test.command)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreFields(configure.Configure{}, "RestartAgent")); diff != "" {
				t.Errorf("buildConfigureFromMap(%v) returned diff (-want +got):\n%s", test.command, diff)
			}
		})
	}
}
