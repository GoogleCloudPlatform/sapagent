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
	"context"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"go.uber.org/zap/zapcore"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultCloudProperties = &iipb.CloudProperties{
		ProjectId:        "default-project",
		InstanceId:       "default-instance-id",
		InstanceName:     "default-instance",
		Zone:             "default-zone",
		NumericProjectId: "13102003",
	}
)

func createTestDirectory(t *testing.T, dir string) string {
	t.Helper()
	path := t.TempDir() + "/" + dir
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", path, err)
	}
	return path
}

func createTestFile(t *testing.T) string {
	filePath := t.TempDir() + "/test.log"
	_, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("os.Create(%v) failed: %v", filePath, err)
	}
	return filePath
}

func TestInit(t *testing.T) {
	tests := []struct {
		name             string
		opt              InitOptions
		args             []any
		wantCloudProps   *iipb.CloudProperties
		wantExitStatus   subcommands.ExitStatus
		wantInitComplete bool
	}{
		{
			name: "FailLogPathIsDirectory",
			opt: InitOptions{
				LogPath: createTestDirectory(t, "test"),
			},
			args:             []any{},
			wantCloudProps:   nil,
			wantExitStatus:   subcommands.ExitUsageError,
			wantInitComplete: false,
		},
		{
			name: "FailLogPathIsCurrentDirectory",
			opt: InitOptions{
				LogPath: ".",
			},
			args:             []any{},
			wantCloudProps:   nil,
			wantExitStatus:   subcommands.ExitUsageError,
			wantInitComplete: false,
		},
		{
			name: "FailCurrentDirectoryParentProvided",
			opt: InitOptions{
				LogPath: "..",
			},
			args:             []any{},
			wantCloudProps:   nil,
			wantExitStatus:   subcommands.ExitUsageError,
			wantInitComplete: false,
		},
		{
			name: "SuccessLogPathIsFilePath",
			opt: InitOptions{
				LogPath: createTestFile(t),
			},
			args: []any{
				"anything",
				log.Parameters{},
				defaultCloudProperties,
			},
			wantCloudProps:   defaultCloudProperties,
			wantExitStatus:   subcommands.ExitSuccess,
			wantInitComplete: true,
		},
		{
			name: "SuccessLogPathIsEmpty",
			opt: InitOptions{
				LogPath: "",
			},
			args: []any{
				"anything",
				log.Parameters{},
				defaultCloudProperties,
			},
			wantCloudProps:   defaultCloudProperties,
			wantExitStatus:   subcommands.ExitSuccess,
			wantInitComplete: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotCloudProps, gotExitStatus, gotInitComplete := Init(context.Background(), test.opt, test.args...)
			if diff := cmp.Diff(test.wantCloudProps, gotCloudProps, protocmp.Transform()); diff != "" {
				t.Errorf("Init() returned an unexpected diff (-want +got):\n%s", diff)
			}
			if gotExitStatus != test.wantExitStatus {
				t.Errorf("Init() returned an unexpected exit status, got: %v, want: %v", gotExitStatus, test.wantExitStatus)
			}
			if gotInitComplete != test.wantInitComplete {
				t.Errorf("Init() = %v, want: %v", gotInitComplete, test.wantInitComplete)
			}
		})
	}
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
