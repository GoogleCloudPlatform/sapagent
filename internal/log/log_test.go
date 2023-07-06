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
