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

// Package backinthandler implements the handler for the backint command.
package backinthandler

import (
	"context"

	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/backint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// RestartAgent indicates if the agent should be restarted after the backint guest action has been handled.
const RestartAgent = false

// BackintHandler is the handler for the backint command.
func BackintHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) (*gpb.CommandResult, bool) {
	usagemetrics.Action(usagemetrics.UAPBackintCommand)
	log.CtxLogger(ctx).Debugw("Handling command", "command", command)
	b := &backint.Backint{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), b)
	msg, exitStatus := b.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true))
	log.CtxLogger(ctx).Debugw("Handled command result", "msg", msg, "exitStatus", exitStatus)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
