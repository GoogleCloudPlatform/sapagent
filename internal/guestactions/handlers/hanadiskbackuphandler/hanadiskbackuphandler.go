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

// Package hanadiskbackuphandler contains the handler for the hanadiskbackup command.
package hanadiskbackuphandler

import (
	"context"

	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// RestartAgent indicates that the agent should be restarted after the hanadiskbackup guest action has been handled.
const RestartAgent = false

// HANADiskBackupHandler is the handler for the hanadiskbackup command.
func HANADiskBackupHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) (*gpb.CommandResult, bool) {
	usagemetrics.Action(usagemetrics.UAPHANADiskBackupCommand)
	s := &hanadiskbackup.Snapshot{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), s)
	message, exitStatus := s.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true))
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   message,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
