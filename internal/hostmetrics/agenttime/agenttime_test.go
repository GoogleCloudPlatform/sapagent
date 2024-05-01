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

package agenttime

import (
	"os"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/google/go-cmp/cmp"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	fc = clockwork.NewFakeClock()
)

// newForTests generates a AgentTime instance with a fake clock, suitable for testing.
func newForTests() *AgentTime {
	return New(fc)
}

func TestCloudMetricRefresh(t *testing.T) {
	testUtilsInstance := newForTests()
	for _, test := range []struct {
		name string
		want time.Time
	}{
		{
			name: "returns cloudMetricRefresh time",
			want: fc.Now().Add(-180 * time.Second),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := testUtilsInstance.CloudMetricRefresh()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("CloudMetricRefresh() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLocalRefresh(t *testing.T) {
	testUtilsInstance := newForTests()
	for _, test := range []struct {
		name string
		want time.Time
	}{
		{
			name: "returns localRefresh time",
			want: fc.Now(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := testUtilsInstance.LocalRefresh()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("LocalRefresh() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStartup(t *testing.T) {
	testUtilsInstance := newForTests()
	for _, test := range []struct {
		name string
		want time.Time
	}{
		{
			name: "returns startup time",
			want: fc.Now(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := testUtilsInstance.Startup()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Startup() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateRefreshTimes(t *testing.T) {
	testUtilsInstance := newForTests()
	for _, test := range []struct {
		name                   string
		wantCloudMetricRefresh time.Time
		wantLocalRefresh       time.Time
	}{
		{
			name:                   "updates refresh times",
			wantCloudMetricRefresh: fc.Now().Add(-180 * time.Second),
			wantLocalRefresh:       fc.Now(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			testUtilsInstance.UpdateRefreshTimes()
			if diff := cmp.Diff(test.wantCloudMetricRefresh, testUtilsInstance.CloudMetricRefresh()); diff != "" {
				t.Errorf("UpdateRefreshTimes() cloudMetricRefresh mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantLocalRefresh, testUtilsInstance.LocalRefresh()); diff != "" {
				t.Errorf("UpdateRefreshTimes() localRefresh mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
