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
	"fmt"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/backup"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	apb "google.golang.org/protobuf/types/known/anypb"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RestartAgent indicates if the agent should be restarted after the gcbdr-backup guest action has been handled.
const RestartAgent = false

// GCBDRBackupHandler is the handler for gcbdr-backup command.
func GCBDRBackupHandler(ctx context.Context, command *gpb.Command, cp *ipb.CloudProperties) (*gpb.CommandResult, bool) {
	log.CtxLogger(ctx).Debugw("gcbdr-backup handler called.", "command", prototext.Format(command))
	b := &backup.Backup{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), b)
	backupResponse, message, exitStatus := b.Run(ctx, commandlineexecutor.ExecuteCommand)
	anyBackupResponse, err := apb.New(backupResponse)
	if err != nil {
		failureMessage := fmt.Sprintf("Failed to marshal response to any. Error: %v", err)
		log.CtxLogger(ctx).Debug(failureMessage)
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   failureMessage,
			ExitCode: int32(subcommands.ExitFailure),
		}
		return result, RestartAgent
	}
	result := &gpb.CommandResult{
		Command:  command,
		Payload:  anyBackupResponse,
		Stdout:   message,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
