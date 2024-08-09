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

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// RestartAgent indicates that the agent should be restarted after the configure guest action has been handled.
const RestartAgent = true

func noOpRestart(ctx context.Context) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("Delaying restart until after we respond to UAP.")
	return subcommands.ExitSuccess
}

// ConfigureHandler is the handler for the configure command.
func ConfigureHandler(ctx context.Context, command *gpb.Command, cp *ipb.CloudProperties) (*gpb.CommandResult, bool) {
	usagemetrics.Action(usagemetrics.UAPConfigureCommand)
	log.CtxLogger(ctx).Debugw("Handling command", "command", command)
	c := &configure.Configure{
		RestartAgent: noOpRestart,
	}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), c)
	msg, exitStatus := c.Run(ctx, onetime.CreateRunOptions(nil, true))
	log.CtxLogger(ctx).Debugw("handled command result -", "msg", msg, "exitStatus", exitStatus)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
