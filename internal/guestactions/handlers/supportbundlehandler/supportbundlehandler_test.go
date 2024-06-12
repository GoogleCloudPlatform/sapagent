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

package supportbundlehandler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestBuildSupportBundleCommand(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.AgentCommand
		want    supportbundle.SupportBundle
	}{
		{
			name: "TrueValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"sid":                 "testsid",
					"instance-numbers":    "testinstance",
					"hostname":            "testhostname",
					"loglevel":            "debug",
					"result-bucket":       "testbucket",
					"pacemaker-diagnosis": "true",
					"agent-logs-only":     "true",
					"help":                "true",
					"version":             "true",
				},
			},
			want: supportbundle.SupportBundle{
				Sid:                "testsid",
				InstanceNums:       "testinstance",
				Hostname:           "testhostname",
				LogLevel:           "debug",
				ResultBucket:       "testbucket",
				PacemakerDiagnosis: true,
				AgentLogsOnly:      true,
				Help:               true,
				Version:            true,
			},
		},
		{
			name: "FalseValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"sid":                 "testsid",
					"instance-numbers":    "testinstance",
					"hostname":            "testhostname",
					"loglevel":            "debug",
					"result-bucket":       "testbucket",
					"pacemaker-diagnosis": "false",
					"agent-logs-only":     "false",
					"help":                "false",
					"version":             "false",
				},
			},
			want: supportbundle.SupportBundle{
				Sid:          "testsid",
				InstanceNums: "testinstance",
				Hostname:     "testhostname",
				LogLevel:     "debug",
				ResultBucket: "testbucket",
			},
		},
		{
			name: "InvalidValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"sid":                 "",
					"instance-numbers":    "",
					"hostname":            "",
					"loglevel":            "",
					"result-bucket":       "",
					"pacemaker-diagnosis": "invalid",
					"agent-logs-only":     "2334",
					"help":                "12",
					"version":             "test_invalid",
				},
			},
			want: supportbundle.SupportBundle{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSupportBundleCommand(tc.command)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(supportbundle.SupportBundle{})); diff != "" {
				t.Errorf("buildSupportBundleCommand(%v) returned an unexpected diff (-want +got):\n%v", tc.command, diff)
			}
		})
	}
}
