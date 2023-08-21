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
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var logger = NewLogger(nil, nil, clockwork.NewRealClock())

// SetAgentProperties sets the configured agent properties on the standard logger.
func SetAgentProperties(ap *cpb.AgentProperties) {
	logger.agentProps = ap
}

// SetCloudProperties sets the configured cloud properties on the standard logger.
func SetCloudProperties(cp *iipb.CloudProperties) {
	logger.setCloudProps(cp)
}

// Running uses the standard logger to log the RUNNING status. This status is reported at most once per day.
func Running() {
	logger.Running()
}

// Started uses the standard logger to log the STARTED status.
func Started() {
	logger.Started()
}

// Stopped uses the standard logger to log the STOPPED status.
func Stopped() {
	logger.Stopped()
}

// Configured uses the standard logger to log the CONFIGURED status.
func Configured() {
	logger.Configured()
}

// Misconfigured uses the standard logger to log the MISCONFIGURED status.
func Misconfigured() {
	logger.Misconfigured()
}

// Error uses the standard logger to log the ERROR status. This status is reported at most once per day.
//
// Any calls to Error should have an id mapping in this mapping sheet: go/sap-core-eng-tool-mapping.
func Error(id int) {
	logger.Error(id)
}

// Installed uses the standard logger to log the INSTALLED status.
func Installed() {
	logger.Installed()
}

// Updated uses the standard logger to log the UPDATED status.
func Updated(version string) {
	logger.Updated(version)
}

// Uninstalled uses the standard logger to log the UNINSTALLED status.
func Uninstalled() {
	logger.Uninstalled()
}

// Action uses the standard logger to log the ACTION status.
func Action(id int) {
	logger.Action(id)
}

// LogRunningDaily log that the agent is running once a day.
func LogRunningDaily() {
	if logger.dailyLogRunningStarted {
		log.Logger.Debugw("Daily log of RUNNING status already started")
		return
	}
	logger.dailyLogRunningStarted = true
	log.Logger.Debugw("Starting daily log of RUNNING status")
	for {
		logger.Running()
		// sleep for 24 hours and a minute.
		time.Sleep(24*time.Hour + 1*time.Minute)
	}
}

// LogActionDaily uses the standard logger to log the ACTION once a day.
// Should be called exactly once for each ACTION code.
func LogActionDaily(id int) {
	log.Logger.Debugw("Starting daily log action", "ACTION", id)
	for {
		// sleep for 24 hours and a minute.
		time.Sleep(24*time.Hour + 1*time.Minute)
		logger.logStatus(StatusAction, fmt.Sprintf("%d", id))
	}
}
