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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestSetAgentProperties(t *testing.T) {
	want := &cpb.AgentProperties{
		Name:            "sapagent",
		Version:         "1.0",
		LogUsageMetrics: true,
	}
	SetAgentProperties(want)
	if d := cmp.Diff(want, logger.agentProps, protocmp.Transform()); d != "" {
		t.Errorf("SetAgentProperties(%v) mismatch (-want, +got):\n%s", want, d)
	}
}

func TestSetCloudProperties(t *testing.T) {
	tests := []struct {
		name              string
		cloudProps        *iipb.CloudProperties
		wantImage         string
		wantIsTestProject bool
	}{
		{
			name:              "nil",
			cloudProps:        nil,
			wantImage:         metadataserver.ImageUnknown,
			wantIsTestProject: false,
		},
		{
			name: "notNil",
			cloudProps: &iipb.CloudProperties{
				ProjectId:        "test-project",
				InstanceId:       "test-instance",
				Zone:             "test-zone",
				InstanceName:     "test-instance-name",
				Image:            "projects/rhel-cloud/global/images/rhel-8-v20220101",
				NumericProjectId: "922508251869",
			},
			wantImage:         "rhel-8-v20220101",
			wantIsTestProject: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetCloudProperties(test.cloudProps)
			if d := cmp.Diff(test.cloudProps, logger.cloudProps, protocmp.Transform()); d != "" {
				t.Errorf("SetCloudProperties(%v) mismatch (-want, +got):\n%s", test.cloudProps, d)
			}
			if logger.image != test.wantImage {
				t.Errorf("SetCloudProperties(%v) unexpected image. got=%s want=%s", test.cloudProps, logger.image, test.wantImage)
			}
			if logger.isTestProject != test.wantIsTestProject {
				t.Errorf("SetCloudProperties(%v) unexpected isTestProject. got=%t want=%t", test.cloudProps, logger.isTestProject, test.wantIsTestProject)
			}
		})
	}
}

func TestRunning(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusRunning]
	Running()
	if logger.lastCalled[StatusRunning].Equal(prevLastCalled) {
		t.Errorf("Running() did not update lastCalled timestamp for RUNNING status")
	}
}

func TestStarted(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusStarted]
	Started()
	if logger.lastCalled[StatusStarted].Equal(prevLastCalled) {
		t.Errorf("Started() did not update lastCalled timestamp for STARTED status")
	}
}

func TestStopped(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusStopped]
	Stopped()
	if logger.lastCalled[StatusStopped].Equal(prevLastCalled) {
		t.Errorf("Stopped() did not update lastCalled timestamp for STOPPED status")
	}
}

func TestConfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusConfigured]
	Configured()
	if logger.lastCalled[StatusConfigured].Equal(prevLastCalled) {
		t.Errorf("Configured() did not update lastCalled timestamp for CONFIGURED status")
	}
}

func TestMisconfigured(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusMisconfigured]
	Misconfigured()
	if logger.lastCalled[StatusMisconfigured].Equal(prevLastCalled) {
		t.Errorf("Misconfigured() did not update lastCalled timestamp for MISCONFIGURED status")
	}
}

func TestError(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusError]
	Error(1)
	if logger.lastCalled[StatusError].Equal(prevLastCalled) {
		t.Error("Error() did not update lastCalled timestamp for ERROR status")
	}
}

func TestInstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusInstalled]
	Installed()
	if logger.lastCalled[StatusInstalled].Equal(prevLastCalled) {
		t.Errorf("Installed() did not update lastCalled timestamp for INSTALLED status")
	}
}

func TestUpdated(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusUpdated]
	Updated("2.0")
	if logger.lastCalled[StatusUpdated].Equal(prevLastCalled) {
		t.Errorf("Updated() did not update lastCalled timestamp for UPDATED status")
	}
}

func TestUninstalled(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusUninstalled]
	Uninstalled()
	if logger.lastCalled[StatusUninstalled].Equal(prevLastCalled) {
		t.Errorf("Uninstalled() did not update lastCalled timestamp for UNINSTALLED status")
	}
}

func TestAction(t *testing.T) {
	// Choose a test project number to bypass sending a request to the compute server.
	SetCloudProperties(&iipb.CloudProperties{NumericProjectId: "922508251869"})
	SetAgentProperties(&cpb.AgentProperties{LogUsageMetrics: true})

	prevLastCalled := logger.lastCalled[StatusAction]
	Action(1)
	if logger.lastCalled[StatusAction].Equal(prevLastCalled) {
		t.Errorf("Action() did not update lastCalled timestamp for ACTION status")
	}
}
