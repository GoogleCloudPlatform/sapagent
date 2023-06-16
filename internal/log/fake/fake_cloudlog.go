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

// Package fake implements a fake version of the Cloud Logging interface.
package fake

import (
	"testing"

	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestCloudLogging provides fake implementations for the cloud logging interface.
type TestCloudLogging struct {
	T                  *testing.T
	ExpectedLogEntries []logging.Entry
	LogCallCount       int

	FlushErr       []error
	FlushCallCount int
}

func compareEntryPayload() cmp.Option {
	filter := func(k string, _ any) bool {
		return k == "discovery"
	}
	return cmpopts.IgnoreMapEntries(filter)
}

// Log provides a fake implementation of the cloud logging Log method.
func (l *TestCloudLogging) Log(e logging.Entry) {
	if l.LogCallCount >= len(l.ExpectedLogEntries) {
		l.T.Errorf("Log called too many times")
		return
	}
	// Always check the payload for the expected keys
	for k := range l.ExpectedLogEntries[l.LogCallCount].Payload.(map[string]string) {
		if _, ok := e.Payload.(map[string]string)[k]; !ok {
			l.T.Errorf("Log argument missing key: %s", k)
		}
	}
	if diff := cmp.Diff(l.ExpectedLogEntries[l.LogCallCount], e, cmpopts.IgnoreFields(logging.Entry{}, "Timestamp"), compareEntryPayload()); diff != "" {
		l.T.Errorf("Mismatched Log arguments: (-want +got): %s", diff)
	}
	l.LogCallCount++
}

// Flush provides a fake implementation of the cloud logging Flush method.
func (l *TestCloudLogging) Flush() error {
	defer func() { l.FlushCallCount++ }()
	return l.FlushErr[l.FlushCallCount]
}

// CheckCallCount checks whether the faked methods have been invoked the expected number of times.
func (l *TestCloudLogging) CheckCallCount() {
	if l.LogCallCount != len(l.ExpectedLogEntries) {
		l.T.Errorf("Mismatched call count to Log(). (got, want) (%d, %d)", l.LogCallCount, len(l.ExpectedLogEntries))
	}
	if l.FlushCallCount != len(l.FlushErr) {
		l.T.Errorf("Mismatched call count to Flush(). (got, want) (%d, %d)", l.FlushCallCount, len(l.FlushErr))
	}
}
