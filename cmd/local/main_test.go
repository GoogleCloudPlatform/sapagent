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

package main

import (
	"testing"

	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	defaultCP *iipb.CloudProperties = &iipb.CloudProperties{
		ProjectId:        "test-project",
		InstanceId:       "test-instanceId",
		Zone:             "test-zone",
		InstanceName:     "test-instanceName",
		Image:            "test-image",
		NumericProjectId: "12345",
	}
)

func TestReadExecutionMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want executionMode
	}{
		{
			name: "BinaryWithNoArgs",
			args: []string{"google_cloud_sap_agent"},
			want: daemonMode,
		},
		{
			name: "ConfigFlagShortForm",
			args: []string{"google_cloud_sap_agent", "-c=configuration.json"},
			want: daemonMode,
		},
		{
			name: "ConfigFlagLongForm",
			args: []string{"google_cloud_sap_agent", "--config=configuration.json"},
			want: daemonMode,
		},
		{
			name: "MaintenanceOTE",
			args: []string{"google_cloud_sap_agent", "maintenance"},
			want: oneTimeMode,
		},
		{
			name: "LogUsageOTE",
			args: []string{"google_cloud_sap_agent", "logusage"},
			want: oneTimeMode,
		},
		{
			name: "WLMRemoteOTE",
			args: []string{"google_cloud_sap_agent", "remote"},
			want: oneTimeMode,
		},
		{
			name: "Help",
			args: []string{"google_cloud_sap_agent", "help"},
			want: oneTimeMode,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := readExecutionMode(test.args)
			if got != test.want {
				t.Errorf("readExecutionMode(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
