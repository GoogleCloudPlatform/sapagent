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

// Package versionhandler implements the handler for the version command.
package versionhandler

import (
	"context"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// VersionHandler is the handler for the version command.
func VersionHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPVersionCommand)
	log.CtxLogger(ctx).Infow("VersionHandler was called. Command passed in is", "command", prototext.Format(command))
	msg := onetime.GetAgentVersion()
	exitStatus := subcommands.ExitSuccess
	log.CtxLogger(ctx).Infow("VersionHandler was called. Version returned -", "msg", msg, "exitStatus", exitStatus)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result
}
