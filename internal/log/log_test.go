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

package log

import (
	"testing"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func TestSetupLoggingToFile(t *testing.T) {
	tests := []struct {
		name        string
		config      *cpb.Configuration
		want        string
		os          string
		wantlogfile string
	}{
		{
			name:        "LogLevelDEBUG",
			config:      &cpb.Configuration{LogLevel: cpb.Configuration_DEBUG},
			want:        "debug",
			os:          "linux",
			wantlogfile: "/var/log/google-cloud-sap-agent.log",
		},
		{
			name:        "LogLevelERROR",
			config:      &cpb.Configuration{LogLevel: cpb.Configuration_ERROR},
			want:        "error",
			os:          "linux",
			wantlogfile: "/var/log/google-cloud-sap-agent.log",
		},
		{
			name:        "LogLevelDEFAULT",
			config:      &cpb.Configuration{},
			want:        "info",
			os:          "linux",
			wantlogfile: "/var/log/google-cloud-sap-agent.log",
		},
		{
			name:        "LogLevelINFO",
			config:      &cpb.Configuration{LogLevel: cpb.Configuration_INFO},
			want:        "info",
			os:          "linux",
			wantlogfile: "/var/log/google-cloud-sap-agent.log",
		},
		{
			name:        "LogLevelWARN",
			config:      &cpb.Configuration{LogLevel: cpb.Configuration_WARNING},
			want:        "warn",
			os:          "windows",
			wantlogfile: "C:\\Program Files\\Google\\google-cloud-sap-agent\\logs\\google-cloud-sap-agent.log",
		},
	}
	for _, test := range tests {
		SetupLoggingToFile(test.os, test.config.LogLevel)
		got := level
		if got != test.want {
			t.Errorf("setupLogging() LogLevel is incorrect, got: %s, want: %s", got, test.want)
		}

		got = logfile
		if got != test.wantlogfile {
			t.Errorf("setupLogging() logfile is incorrect, got: %s, want: %s", got, test.wantlogfile)
		}
	}
}
