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

// Package configurehandler implements the handler for the configure command.
package configurehandler

import (
	"context"

	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// ConfigureHandler is the handler for the configure command.
func ConfigureHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPConfigureCommand)
	log.CtxLogger(ctx).Debugw("Handling command", "command", command)
	c := &configure.Configure{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), c)
	msg, exitStatus := c.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true))
	log.CtxLogger(ctx).Debugw("handled command result -", "msg", msg, "exitStatus", exitStatus)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result
}
