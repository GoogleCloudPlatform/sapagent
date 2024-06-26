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

// Package performancediagnosticshandler contains the handler for the performance diagnostics command.
package performancediagnosticshandler

import (
	"context"
	"runtime"

	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/performancediagnostics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RestartAgent indicates that the agent should be restarted after the performance diagnostics guest action has been handled.
const RestartAgent = false

// PerformanceDiagnosticsHandler is the handler for the performance diagnostics command.
func PerformanceDiagnosticsHandler(ctx context.Context, command *gpb.Command, cp *ipb.CloudProperties) (*gpb.CommandResult, bool) {
	d := &performancediagnostics.Diagnose{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), d)
	lp := log.Parameters{OSType: runtime.GOOS}
	message, exitStatus := d.ExecuteAndGetMessage(ctx, nil, lp, cp)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   message,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
