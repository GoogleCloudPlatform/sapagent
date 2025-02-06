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

// Package gcbdrdiscoveryhandler contains the handler for the gcbdr-discovery command.
package gcbdrdiscoveryhandler

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/discovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	apb "google.golang.org/protobuf/types/known/anypb"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// RestartAgent indicates that the agent should be restarted after the gcbdr-discovery guest action has been handled.
const RestartAgent = false

// GCBDRDiscoveryHandler is the handler for gcbdr-discovery command.
func GCBDRDiscoveryHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPGCBDRDiscoveryCommand)
	log.CtxLogger(ctx).Debugw("gcbdr-discovery handler called.", "command", prototext.Format(command))
	d := &discovery.Discovery{}
	applications, exitStatus := d.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true), commandlineexecutor.ExecuteCommand, filesystem.Helper{})
	if exitStatus != subcommands.ExitSuccess {
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   fmt.Sprintf("Failed to get HANA discovery applications: %v", exitStatus),
			ExitCode: int32(exitStatus),
		}
		return result
	}
	anyApplications, err := apb.New(applications)
	if err != nil {
		failureMessage := fmt.Sprintf("Failed to marshal response to any. Error: %v", err)
		log.CtxLogger(ctx).Debug(failureMessage)
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   failureMessage,
			ExitCode: int32(subcommands.ExitFailure),
		}
		return result
	}
	result := &gpb.CommandResult{
		Command:  command,
		Payload:  anyApplications,
		Stdout:   "HANA Applications discovered",
		ExitCode: int32(subcommands.ExitSuccess),
	}
	return result
}
