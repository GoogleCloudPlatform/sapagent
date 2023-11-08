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

	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zapcore/zapcore"
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
