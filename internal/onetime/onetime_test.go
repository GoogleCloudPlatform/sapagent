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

package onetime

import (
	"os"
	"testing"

	"flag"
	"github.com/google/subcommands"
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestSetupOneTimeLogging(t *testing.T) {
	tests := []struct {
		name             string
		os               string
		logFileOverride  string
		subCommandName   string
		want             string
		wantCloudLogName string
	}{
		{
			name:             "Windows",
			os:               "windows",
			subCommandName:   "logusage",
			want:             `C:\Program Files\Google\google-cloud-sap-agent\logs\logusage.log`,
			wantCloudLogName: "google-cloud-sap-agent-logusage",
		},
		{
			name:             "Linux",
			os:               "linux",
			subCommandName:   "snapshot",
			want:             `/var/log/google-cloud-sap-agent/snapshot.log`,
			wantCloudLogName: "google-cloud-sap-agent-snapshot",
		},
		{
			name:             "LinuxWithLogFileOverride",
			os:               "linux",
			subCommandName:   "snapshot",
			logFileOverride:  "/tmp/snapshot.log",
			want:             "/tmp/snapshot.log",
			wantCloudLogName: "google-cloud-sap-agent-snapshot",
		},
		{
			name:             "WindowsWithLogFileOverride",
			os:               "windows",
			subCommandName:   "logusage",
			logFileOverride:  "/tmp/logusage.log",
			want:             "/tmp/logusage.log",
			wantCloudLogName: "google-cloud-sap-agent-logusage",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lp := log.Parameters{
				LogToCloud:  false,
				OSType:      test.os,
				Level:       2,
				LogFileName: test.logFileOverride,
			}
			gotparams := SetupOneTimeLogging(lp, test.subCommandName, zapcore.ErrorLevel)
			if gotparams.CloudLogName != test.wantCloudLogName {
				t.Errorf("SetupOneTimeLogging(%s,%s) cloudlogname is incorrect, got: %s, want: %s", test.os, test.subCommandName, gotparams.CloudLogName, test.wantCloudLogName)
			}

			got := log.GetLogFile()
			if got != test.want {
				t.Errorf("SetupOneTimeLogging(%s,%s)=%s, want: %s", test.os, test.subCommandName, got, test.want)
			}
		})
	}
}

func TestHelpCommand(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Usage = func() { log.Print("test usage message") }
	want := subcommands.ExitSuccess
	got := HelpCommand(fs)

	if got != want {
		t.Errorf("HelpCommand()=%v, want: %v", got, want)
	}
}

func TestLogFilesPath(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		iiote *InternallyInvokedOTE
		want  string
	}{
		{
			name: "IIOTEParamsNil",
			s:    "snapshot",
			want: `/var/log/google-cloud-sap-agent/snapshot.log`,
		},
		{
			name: "IIOTEParamsNotNil",
			s:    "snapshot",
			iiote: &InternallyInvokedOTE{
				InvokedBy: "changedisktype",
			},
			want: `/var/log/google-cloud-sap-agent/changedisktype.log`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := LogFilePath(test.s, test.iiote)
			if got != test.want {
				t.Errorf("LogFilesPath(%v, %v)=%s, want: %s", test.s, test.iiote, got, test.want)
			}
		})
	}
}
