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

// Package gcbdrbackuphandler contains the handler for the gcbdr-backup command.
package gcbdrbackuphandler

import (
	"context"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcbdractions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/backup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

// GCBDRBackupHandler is the handler for gcbdr-backup command.
func GCBDRBackupHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPGCBDRBackupCommand)
	log.CtxLogger(ctx).Debugw("gcbdr-backup handler called.", "command", prototext.Format(command))
	b := &backup.Backup{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), b)
	result := b.Run(ctx, commandlineexecutor.ExecuteCommand, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true))
	result.Command = command
	return result
}
