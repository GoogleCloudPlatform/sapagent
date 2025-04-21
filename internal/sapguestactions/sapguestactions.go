/*
Copyright 2024 Google LLC

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

// Package sapguestactions connects to Agent Communication Service (ACS) and handles guest actions in
// the agent. Messages received via ACS will typically have been sent via UAP Communication Highway.
package sapguestactions

import (
	"context"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/backinthandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/configurehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/configureinstancehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/hanadiskbackuphandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/instancemetadatahandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/performancediagnosticshandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/supportbundlehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers/versionhandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"

	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/guestactions"
)

const (
	defaultChannel = "workloadmanager.googleapis.com/wlm-sap-channel"
	testChannel    = "wlm-sap-test-channel"
)

var guestActionsHandlers = map[string]guestactions.GuestActionHandler{
	"backint":                backinthandler.BackintHandler,
	"configure":              configurehandler.ConfigureHandler,
	"configureinstance":      configureinstancehandler.ConfigureInstanceHandler,
	"hanadiskbackup":         hanadiskbackuphandler.HANADiskBackupHandler,
	"instancemetadata":       instancemetadatahandler.InstanceMetadataHandler,
	"performancediagnostics": performancediagnosticshandler.PerformanceDiagnosticsHandler,
	"supportbundle":          supportbundlehandler.SupportBundleHandler,
	"version":                versionhandler.VersionHandler,
}

// StartACSCommunication establishes communication with ACS.
// Returns true if the goroutine is started, and false otherwise.
func StartACSCommunication(ctx context.Context, config *cpb.Configuration) bool {
	if !config.GetUapConfiguration().GetEnabled().GetValue() {
		log.CtxLogger(ctx).Info("Not configured to communicate via ACS")
		return false
	}
	dailyMetricsRoutine := &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.GuestActionsStarted) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)
	cloudProps := protostruct.ConvertCloudPropertiesToStruct(config.CloudProperties)
	ga := &guestactions.GuestActions{}
	gaOpts := guestactions.Options{
		Channel:         defaultChannel,
		CloudProperties: cloudProps,
		Handlers:        guestActionsHandlers,
	}
	communicateRoutine := &recovery.RecoverableRoutine{
		Routine:             ga.Start,
		RoutineArg:          gaOpts,
		ErrorCode:           usagemetrics.GuestActionsFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting ACS communication routine")
	communicateRoutine.StartRoutine(ctx)

	testGA := &guestactions.GuestActions{}
	testGAOpts := guestactions.Options{
		Channel:         testChannel,
		CloudProperties: cloudProps,
		Handlers:        guestActionsHandlers,
	}
	if config.GetUapConfiguration().GetTestChannelEnabled().GetValue() {
		testRoutine := &recovery.RecoverableRoutine{
			Routine:             testGA.Start,
			RoutineArg:          testGAOpts,
			ErrorCode:           usagemetrics.GuestActionsFailure,
			UsageLogger:         *usagemetrics.Logger,
			ExpectedMinDuration: 10 * time.Second,
		}
		log.CtxLogger(ctx).Info("Starting ACS communication routine for test channel")
		testRoutine.StartRoutine(ctx)
	}
	return true
}
