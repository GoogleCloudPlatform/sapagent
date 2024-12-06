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
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// Logger is the standard usage logger for the SAP agent.
var Logger = usagemetrics.NewLogger(nil, nil, clockwork.NewRealClock(), projectExclusionList)

func getImageOsFromImageURI(uri string) string {
	split := strings.Split(uri, "/")
	if len(split) > 1 {
		return split[len(split)-1]
	}
	return metadataserver.ImageUnknown
}

// SetProperties sets the configured agent properties on the standard Logger.
func SetProperties(ap *cpb.AgentProperties, cp *iipb.CloudProperties) {
	img := getImageOsFromImageURI(cp.GetImage())
	opt := fmt.Sprintf("%s-%s", img, cp.GetInstanceId())
	Logger.SetAgentProps(
		&usagemetrics.AgentProperties{
			Name:             ap.GetName(),
			Version:          ap.GetVersion(),
			LogUsageMetrics:  ap.GetLogUsageMetrics(),
			LogUsagePrefix:   "sap-core-eng",
			LogUsageOptional: opt,
		})
	Logger.SetCloudProps(&usagemetrics.CloudProperties{
		ProjectID:     cp.ProjectId,
		Zone:          cp.Zone,
		InstanceName:  cp.InstanceName,
		ProjectNumber: cp.GetNumericProjectId(),
		Image:         cp.Image,
		InstanceID:    cp.InstanceId,
	})
}

// ParseStatus parses the status string to a Status enum.
func ParseStatus(status string) usagemetrics.Status {
	return usagemetrics.Status(status)
}

// Running uses the standard Logger to log the RUNNING status. This status is reported at most once per day.
func Running() {
	Logger.Running()
}

// Started uses the standard Logger to log the STARTED status.
func Started() {
	Logger.Started()
}

// Stopped uses the standard Logger to log the STOPPED status.
func Stopped() {
	Logger.Stopped()
}

// Configured uses the standard Logger to log the CONFIGURED status.
func Configured() {
	Logger.Configured()
}

// Misconfigured uses the standard Logger to log the MISCONFIGURED status.
func Misconfigured() {
	Logger.Misconfigured()
}

// Error uses the standard Logger to log the ERROR status.
//
// Any calls to Error should have an id mapping in this mapping sheet: go/sap-core-eng-tool-mapping.
func Error(id int) {
	Logger.Error(id)
}

// Installed uses the standard Logger to log the INSTALLED status.
func Installed() {
	Logger.Installed()
}

// Updated uses the standard Logger to log the UPDATED status.
func Updated(version string) {
	Logger.Updated(version)
}

// Uninstalled uses the standard Logger to log the UNINSTALLED status.
func Uninstalled() {
	Logger.Uninstalled()
}

// Action uses the standard Logger to log the ACTION status.
func Action(id int) {
	Logger.Action(id)
}

// LogRunningDaily log that the agent is running once a day.
func LogRunningDaily() {
	if Logger.IsDailyLogRunningStarted() {
		log.Logger.Debugw("Daily log of RUNNING status already started")
		return
	}
	Logger.DailyLogRunningStarted()
	log.Logger.Debugw("Starting daily log of RUNNING status")
	for {
		Logger.Running()
		// sleep for 24 hours and a minute.
		time.Sleep(24*time.Hour + 1*time.Minute)
	}
}

// LogActionDaily uses the standard Logger to log the ACTION once a day.
// Should be called exactly once for each ACTION code.
func LogActionDaily(id int) {
	log.Logger.Debugw("Starting daily log action", "ACTION", id)
	for {
		// sleep for 24 hours and a minute.
		time.Sleep(24*time.Hour + 1*time.Minute)
		Logger.Action(id)
	}
}
