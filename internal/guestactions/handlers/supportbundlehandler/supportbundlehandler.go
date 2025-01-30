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

// Package supportbundlehandler implements the handler for the support bundle command.
package supportbundlehandler

import (
	"context"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// RestartAgent indicates that the agent should be restarted after the supportbundle guest action has been handled.
const RestartAgent = false

// SupportBundleHandler is the handler for support bundle command.
func SupportBundleHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) (*gpb.CommandResult, bool) {
	return runCommand(ctx, command, cp, commandlineexecutor.ExecuteCommand)
}

func runCommand(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties, exec commandlineexecutor.Execute) (*gpb.CommandResult, bool) {
	usagemetrics.Action(usagemetrics.UAPSupportBundleCommand)
	log.CtxLogger(ctx).Debugw("Support bundle handler called.", "command", prototext.Format(command))
	sb := &supportbundle.SupportBundle{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), sb)
	msg, exitStatus := sb.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true), exec)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
