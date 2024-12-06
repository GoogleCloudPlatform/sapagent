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
	"os"
	"testing"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestRunning(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusRunning)
	Running()
	if Logger.LastCalled(StatusRunning).Equal(prevLastCalled) {
		t.Errorf("Running() did not update lastCalled timestamp for RUNNING status")
	}
}

func TestStarted(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusStarted)
	Started()
	if Logger.LastCalled(StatusStarted).Equal(prevLastCalled) {
		t.Errorf("Started() did not update lastCalled timestamp for STARTED status")
	}
}

func TestStopped(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusStopped)
	Stopped()
	if Logger.LastCalled(StatusStopped).Equal(prevLastCalled) {
		t.Errorf("Stopped() did not update lastCalled timestamp for STOPPED status")
	}
}

func TestConfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusConfigured)
	Configured()
	if Logger.LastCalled(StatusConfigured).Equal(prevLastCalled) {
		t.Errorf("Configured() did not update lastCalled timestamp for CONFIGURED status")
	}
}

func TestMisconfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusMisconfigured)
	Misconfigured()
	if Logger.LastCalled(StatusMisconfigured).Equal(prevLastCalled) {
		t.Errorf("Misconfigured() did not update lastCalled timestamp for MISCONFIGURED status")
	}
}

func TestError(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusError)
	Error(1)
	if Logger.LastCalled(StatusError).Equal(prevLastCalled) {
		t.Error("Error() did not update lastCalled timestamp for ERROR status")
	}
}

func TestInstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusInstalled)
	Installed()
	if Logger.LastCalled(StatusInstalled).Equal(prevLastCalled) {
		t.Errorf("Installed() did not update lastCalled timestamp for INSTALLED status")
	}
}

func TestUpdated(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusUpdated)
	Updated("2.0")
	if Logger.LastCalled(StatusUpdated).Equal(prevLastCalled) {
		t.Errorf("Updated() did not update lastCalled timestamp for UPDATED status")
	}
}

func TestUninstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusUninstalled)
	Uninstalled()
	if Logger.LastCalled(StatusUninstalled).Equal(prevLastCalled) {
		t.Errorf("Uninstalled() did not update lastCalled timestamp for UNINSTALLED status")
	}
}

func TestAction(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetProperties(&cpb.AgentProperties{LogUsageMetrics: true}, &iipb.CloudProperties{NumericProjectId: "922508251869"})

	prevLastCalled := Logger.LastCalled(StatusAction)
	Action(1)
	if Logger.LastCalled(StatusAction).Equal(prevLastCalled) {
		t.Errorf("Action() did not update lastCalled timestamp for ACTION status")
	}
}

func TestGetImageOsFromImageURI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{
			name: "emptyuri",
			uri:  "",
			want: "unknown",
		},
		{
			name: "invaliduri",
			uri:  "invaliduri-noslashes",
			want: "unknown",
		},
		{
			name: "validuri",
			uri:  "/projects/test-project/global/images/test-image",
			want: "test-image",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getImageOsFromImageURI(test.uri)
			if got != test.want {
				t.Errorf("getImageOsFromImageURI(%v) = %v, want %v", test.uri, got, test.want)
			}
		})
	}
}
