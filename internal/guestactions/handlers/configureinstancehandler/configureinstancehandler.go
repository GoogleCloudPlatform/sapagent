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

// Package configureinstancehandler contains the handler for the configure instance command.
package configureinstancehandler

import (
	"context"
	"os"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configureinstance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// ConfigureInstanceHandler is the handler for configure instance command.
func ConfigureInstanceHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPConfigureInstanceCommand)
	log.CtxLogger(ctx).Debugw("Configure Instance handler called.", "command", prototext.Format(command))
	c := &configureinstance.ConfigureInstance{
		ReadFile:    os.ReadFile,
		WriteFile:   os.WriteFile,
		ExecuteFunc: commandlineexecutor.ExecuteCommand,
	}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), c)
	runOptions := onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true)
	exitStatus, message := c.Run(ctx, runOptions)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   message,
		ExitCode: int32(exitStatus),
	}
	return result
}
