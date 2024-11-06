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

	"cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap/zapcore"
)

func TestSetupLoggingToDiscard(t *testing.T) {
	wantLevel := ""
	wantLogFile := ""
	SetupLoggingToDiscard()
	got := GetLevel()
	if got != wantLevel {
		t.Errorf("SetupLoggingToDiscard() level is incorrect, got: %s, want: %s", got, wantLevel)
	}

	got = GetLogFile()
	if got != wantLogFile {
		t.Errorf("SetupLoggingToDiscard() logFile is incorrect, got: %s, want: %s", got, wantLogFile)
	}
}

func TestSetupLoggingForTest(t *testing.T) {
	wantLevel := "debug"
	wantLogFile := ""
	SetupLoggingForTest()
	got := GetLevel()
	if got != wantLevel {
		t.Errorf("TestSetupLoggingForTest() level is incorrect, got: %s, want: %s", got, wantLevel)
	}

	got = GetLogFile()
	if got != wantLogFile {
		t.Errorf("TestSetupLoggingForTest() logFile is incorrect, got: %s, want: %s", got, wantLogFile)
	}
}

func TestLogLevelStringToZapcore(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  zapcore.Level
	}{
		{
			name:  "info",
			level: "info",
			want:  zapcore.InfoLevel,
		},
		{
			name:  "error",
			level: "error",
			want:  zapcore.ErrorLevel,
		},
		{
			name:  "warn",
			level: "warn",
			want:  zapcore.WarnLevel,
		},
		{
			name:  "debug",
			level: "debug",
			want:  zapcore.DebugLevel,
		},
		{
			name:  "unknown",
			level: "unknown",
			want:  zapcore.InfoLevel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StringLevelToZapcore(tc.level)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("LogLevelStringToZapcore(%v) returned an unexpected diff (-want +got): %v", tc.level, diff)
			}
		})
	}
}

func TestSetupLogging(t *testing.T) {
	wantLevel := "warn"
	wantLogFile := "log-file"
	SetupLogging(Parameters{
		Level:              zapcore.WarnLevel,
		LogFileName:        "log-file",
		LogToCloud:         true,
		CloudLoggingClient: &logging.Client{},
	})
	got := GetLevel()
	if got != wantLevel {
		t.Errorf("TestSetupLogging() level is incorrect, got: %s, want: %s", got, wantLevel)
	}

	got = GetLogFile()
	if got != wantLogFile {
		t.Errorf("TestSetupLogging() logFile is incorrect, got: %s, want: %s", got, wantLogFile)
	}
}

func TestSetupLoggingForOTE(t *testing.T) {
	tests := []struct {
		name           string
		agent          string
		subCommandName string
		want           string
	}{
		{
			name:           "SetCloudLogName",
			agent:          "test-agent",
			subCommandName: "logusage",
			want:           "test-agent-logusage",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lp := Parameters{}
			gotparams := SetupLoggingForOTE(test.agent, test.subCommandName, lp)
			if gotparams.CloudLogName != test.want {
				t.Errorf("SetupLoggingForOTE(%s, %s) CloudLogName is incorrect, got: %s, want: %s", test.agent, test.subCommandName, gotparams.CloudLogName, test.want)
			}
		})
	}
}

func TestDefaultOTEPath(t *testing.T) {
	tests := []struct {
		name        string
		agent       string
		osType      string
		logFilePath string
		want        string
	}{
		{
			name:        "Windows",
			agent:       "test-agent",
			osType:      "windows",
			logFilePath: ``,
			want:        `C:\Program Files\Google\test-agent\logs\test-agent-{COMMAND}.log`,
		},
		{
			name:        "WindowsWithPath",
			agent:       "test-agent",
			osType:      "windows",
			logFilePath: `C:\tmp\`,
			want:        `C:\tmp\test-agent-{COMMAND}.log`,
		},
		{
			name:        "WindowsWithPathNoSlash",
			agent:       "test-agent",
			osType:      "windows",
			logFilePath: `C:\tmp`,
			want:        `C:\tmp\test-agent-{COMMAND}.log`,
		},
		{
			name:        "Linux",
			agent:       "test-agent",
			osType:      "linux",
			logFilePath: ``,
			want:        `/var/log/test-agent-{COMMAND}.log`,
		},
		{
			name:        "LinuxWithPath",
			agent:       "test-agent",
			osType:      "linux",
			logFilePath: `/tmp/`,
			want:        `/tmp/test-agent-{COMMAND}.log`,
		},
		{
			name:        "LinuxWithPathNoSlash",
			agent:       "test-agent",
			osType:      "linux",
			logFilePath: `/tmp`,
			want:        `/tmp/test-agent-{COMMAND}.log`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DefaultOTEPath(test.agent, test.osType, test.logFilePath)
			if got != test.want {
				t.Errorf("DefaultOTEPath(%s, %s) Default path is incorrect, got: %s, want: %s", test.agent, test.osType, got, test.want)
			}
		})
	}
}

func TestOTEFilePath(t *testing.T) {
	tests := []struct {
		name        string
		agent       string
		command     string
		osType      string
		logFilePath string
		want        string
	}{
		{
			name:        "Windows",
			agent:       "test-agent",
			command:     "echo",
			osType:      "windows",
			logFilePath: ``,
			want:        `C:\Program Files\Google\test-agent\logs\test-agent-echo.log`,
		},
		{
			name:        "WindowsWithPath",
			agent:       "test-agent",
			command:     "echo",
			osType:      "windows",
			logFilePath: `C:\tmp\`,
			want:        `C:\tmp\test-agent-echo.log`,
		},
		{
			name:        "WindowsWithPathNoSlash",
			agent:       "test-agent",
			command:     "echo",
			osType:      "windows",
			logFilePath: `C:\tmp`,
			want:        `C:\tmp\test-agent-echo.log`,
		},
		{
			name:        "Linux",
			agent:       "test-agent",
			command:     "echo",
			osType:      "linux",
			logFilePath: ``,
			want:        `/var/log/test-agent-echo.log`,
		},
		{
			name:        "LinuxWithPath",
			agent:       "test-agent",
			command:     "echo",
			osType:      "linux",
			logFilePath: `/tmp/`,
			want:        `/tmp/test-agent-echo.log`,
		},
		{
			name:        "LinuxWithPathNoSlash",
			agent:       "test-agent",
			command:     "echo",
			osType:      "linux",
			logFilePath: `/tmp`,
			want:        `/tmp/test-agent-echo.log`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := OTEFilePath(test.agent, test.command, test.osType, test.logFilePath)
			if got != test.want {
				t.Errorf("OTEFilePath(%s, %s, %s) Default path is incorrect, got: %s, want: %s", test.agent, test.command, test.osType, got, test.want)
			}
		})
	}
}
