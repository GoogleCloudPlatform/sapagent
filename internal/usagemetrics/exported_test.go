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
	"testing"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestRunning(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusRunning)
	Running()
	if logger.LastCalled(StatusRunning).Equal(prevLastCalled) {
		t.Errorf("Running() did not update lastCalled timestamp for RUNNING status")
	}
}

func TestStarted(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusStarted)
	Started()
	if logger.LastCalled(StatusStarted).Equal(prevLastCalled) {
		t.Errorf("Started() did not update lastCalled timestamp for STARTED status")
	}
}

func TestStopped(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusStopped)
	Stopped()
	if logger.LastCalled(StatusStopped).Equal(prevLastCalled) {
		t.Errorf("Stopped() did not update lastCalled timestamp for STOPPED status")
	}
}

func TestConfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusConfigured)
	Configured()
	if logger.LastCalled(StatusConfigured).Equal(prevLastCalled) {
		t.Errorf("Configured() did not update lastCalled timestamp for CONFIGURED status")
	}
}

func TestMisconfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusMisconfigured)
	Misconfigured()
	if logger.LastCalled(StatusMisconfigured).Equal(prevLastCalled) {
		t.Errorf("Misconfigured() did not update lastCalled timestamp for MISCONFIGURED status")
	}
}

func TestError(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusError)
	Error(1)
	if logger.LastCalled(StatusError).Equal(prevLastCalled) {
		t.Error("Error() did not update lastCalled timestamp for ERROR status")
	}
}

func TestInstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusInstalled)
	Installed()
	if logger.LastCalled(StatusInstalled).Equal(prevLastCalled) {
		t.Errorf("Installed() did not update lastCalled timestamp for INSTALLED status")
	}
}

func TestUpdated(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusUpdated)
	Updated("2.0")
	if logger.LastCalled(StatusUpdated).Equal(prevLastCalled) {
		t.Errorf("Updated() did not update lastCalled timestamp for UPDATED status")
	}
}

func TestUninstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusUninstalled)
	Uninstalled()
	if logger.LastCalled(StatusUninstalled).Equal(prevLastCalled) {
		t.Errorf("Uninstalled() did not update lastCalled timestamp for UNINSTALLED status")
	}
}

func TestAction(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.LastCalled(StatusAction)
	Action(1)
	if logger.LastCalled(StatusAction).Equal(prevLastCalled) {
		t.Errorf("Action() did not update lastCalled timestamp for ACTION status")
	}
}
