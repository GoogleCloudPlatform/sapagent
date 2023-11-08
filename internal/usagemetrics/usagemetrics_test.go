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

package usagemetrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmpopts/cmpopts"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/metadataserver"
)

var (
	testImage = "rhel-8-v20220101"

	// Choose a project number which maps to a test instance.
	// This value is used in tests to ensure that the Logger.isTestProject field is set to true.
	testProjectNumber = "922508251869"

	defaultAgentProps = &configpb.AgentProperties{
		Version:         "1.0",
		Name:            "Agent Name",
		LogUsageMetrics: true,
	}
	defaultCloudProps = &instancepb.CloudProperties{
		NumericProjectId: testProjectNumber,
		ProjectId:        "test-project",
		Zone:             "test-zone",
		InstanceName:     "test-instance",
		Image:            fmt.Sprintf("projects/rhel-cloud/global/images/%s", testImage),
	}
	defaultNow        = time.Now()
	defaultTimeSource = clockwork.NewFakeClockAt(defaultNow)
)

func TestLogger_Running(t *testing.T) {
	tests := []struct {
		name       string
		agentProps *configpb.AgentProperties
		nowOffset  time.Time
		want       time.Time
	}{
		{
			name:       "success",
			agentProps: defaultAgentProps,
			nowOffset:  defaultNow.Add(24 * time.Hour),
			want:       defaultNow.Add(24 * time.Hour),
		},
		{
			name: "loggerDisabled",
			agentProps: &configpb.AgentProperties{
				Version:         "1.0",
				Name:            "Agent Name",
				LogUsageMetrics: false,
			},
			nowOffset: defaultNow.Add(24 * time.Hour),
			want:      defaultNow,
		},
		{
			name:       "tooSoon",
			agentProps: defaultAgentProps,
			nowOffset:  defaultNow,
			want:       defaultNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := NewLogger(test.agentProps, defaultCloudProps, clockwork.NewFakeClockAt(test.nowOffset))
			logger.lastCalled[StatusRunning] = defaultNow
			logger.Running()
			if got := logger.lastCalled[StatusRunning]; !got.Equal(test.want) {
				t.Errorf("Logger.Running() last called mismatch. got: %v want: %v", got, test.want)
			}
		})
	}
}

func TestLogger_Started(t *testing.T) {
	tests := []struct {
		name       string
		agentProps *configpb.AgentProperties
		want       time.Time
	}{
		{
			name:       "success",
			agentProps: defaultAgentProps,
			want:       defaultNow,
		},
		{
			name: "loggerDisabled",
			agentProps: &configpb.AgentProperties{
				Version:         "1.0",
				Name:            "Agent Name",
				LogUsageMetrics: false,
			},
			want: time.Time{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := NewLogger(test.agentProps, defaultCloudProps, defaultTimeSource)
			logger.Started()
			if got := logger.lastCalled[StatusStarted]; !got.Equal(test.want) {
				t.Errorf("Logger.Started() last called mismatch. got: %v want: %v", got, test.want)
			}
		})
	}
}

func TestLogger_Stopped(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Stopped()
	if got := logger.lastCalled[StatusStopped]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Stopped() last called mismatch. got: %v want: %v", got, defaultNow)
	}
}

func TestLogger_Configured(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Configured()
	if got := logger.lastCalled[StatusConfigured]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Configured() last called mismatch. got: %v want: %v", got, defaultNow)
	}
}

func TestLogger_Misconfigured(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Misconfigured()
	if got := logger.lastCalled[StatusMisconfigured]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Misconfigured() last called mismatch. got: %v want: %v", got, defaultNow)
	}
}

func TestLogger_Error(t *testing.T) {
	tests := []struct {
		name      string
		nowOffset time.Time
		want      time.Time
	}{
		{
			name:      "success",
			nowOffset: defaultNow.Add(24 * time.Hour),
			want:      defaultNow.Add(24 * time.Hour),
		},
		{
			name:      "tooSoon",
			nowOffset: defaultNow,
			want:      defaultNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := NewLogger(defaultAgentProps, defaultCloudProps, clockwork.NewFakeClockAt(test.nowOffset))
			logger.lastCalled[StatusError] = defaultNow
			logger.Error(1)
			if got := logger.lastCalled[StatusError]; !got.Equal(test.want) {
				t.Errorf("Logger.Error(%d) last called mismatch. got: %v want: %v", 1, got, test.want)
			}
		})
	}
}

func TestLogger_Installed(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Installed()
	if got := logger.lastCalled[StatusInstalled]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Installed() last called mismatch. got: %v want: %v", got, defaultNow)
	}
}

func TestLogger_Updated(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Updated("1.1")
	if got := logger.lastCalled[StatusUpdated]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Updated(%s) last called mismatch. got: %v want: %v", "1.1", got, defaultNow)
	}
}

func TestLogger_Uninstalled(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Uninstalled()
	if got := logger.lastCalled[StatusUninstalled]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Uninstalled() last called mismatch. got: %v want: %v", got, defaultNow)
	}
}

func TestLogger_Action(t *testing.T) {
	logger := NewLogger(defaultAgentProps, defaultCloudProps, defaultTimeSource)
	logger.Action(1)
	if got := logger.lastCalled[StatusAction]; !got.Equal(defaultNow) {
		t.Errorf("Logger.Action(%d) last called mismatch. got: %v want: %v", 1, got, defaultNow)
	}
}

func TestLogger_RequestComputeAPIWithUserAgent(t *testing.T) {
	tests := []struct {
		name          string
		cloudProps    *instancepb.CloudProperties
		url           string
		ua            string
		contentLength string
		want          error
	}{
		{
			name: "success",
			ua:   "sap-core-eng/AgentName/1.0/rhel-8-v2022-0101/RUNNING",
			want: nil,
		},
		{
			name: "testProject",
			cloudProps: &instancepb.CloudProperties{
				NumericProjectId: testProjectNumber,
			},
			want: nil,
		},
		{
			name: "error",
			url:  "notAValidURL",
			ua:   "sap-core-eng/AgentName/1.0/rhel-8-v2022-0101/RUNNING",
			want: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header["User-Agent"][0]
				if got != test.ua {
					t.Errorf("Logger.requestComputeAPIWithUserAgent(url, %q) unexpected User-Agent header set. got=%s want=%s", test.ua, got, test.ua)
				}
			}))
			defer ts.Close()

			url := ts.URL
			if test.url != "" {
				url = test.url
			}
			l := NewLogger(defaultAgentProps, test.cloudProps, defaultTimeSource)
			if got := l.requestComputeAPIWithUserAgent(url, test.ua); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("Logger.requestComputeAPIWithUserAgent(%q, %q) got err=%v want err=%v", url, test.ua, got, test.want)
			}
		})
	}
}

func TestBuildComputeURL(t *testing.T) {
	tests := []struct {
		name       string
		cloudProps *instancepb.CloudProperties
		want       string
	}{
		{
			name: "withCloudProperties",
			cloudProps: &instancepb.CloudProperties{
				ProjectId:    "test-project",
				Zone:         "test-zone",
				InstanceName: "test-instance",
			},
			want: "https://compute.googleapis.com/compute/v1/projects/test-project/zones/test-zone/instances/test-instance",
		},
		{
			name: "withoutCloudProperties",
			want: "https://compute.googleapis.com/compute/v1/projects/unknown/zones/unknown/instances/unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildComputeURL(test.cloudProps); got != test.want {
				t.Errorf("buildComputeURL(%v) got=%s want=%s", test.cloudProps, got, test.want)
			}
		})
	}
}

func TestBuildUserAgent(t *testing.T) {
	tests := []struct {
		name       string
		agentProps *configpb.AgentProperties
		image      string
		status     string
		want       string
	}{
		{
			name:       "success",
			agentProps: defaultAgentProps,
			image:      testImage,
			status:     "RUNNING",
			want:       fmt.Sprintf("sap-core-eng/AgentName/1.0/%s/RUNNING", testImage),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildUserAgent(test.agentProps, test.image, test.status); got != test.want {
				t.Errorf("buildUserAgent() got=%s want %s", got, test.want)
			}
		})
	}
}

func TestParseImage(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "unknownImage",
			image: metadataserver.ImageUnknown,
			want:  metadataserver.ImageUnknown,
		},
		{
			name:  "noPatternMatch",
			image: "invalidImageString",
			want:  metadataserver.ImageUnknown,
		},
		{
			name:  "success",
			image: fmt.Sprintf("projects/rhel-cloud/global/images/%s", testImage),
			want:  testImage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseImage(test.image); got != test.want {
				t.Errorf("parseImage(%q) got=%s want=%s", test.image, got, test.want)
			}
		})
	}
}
