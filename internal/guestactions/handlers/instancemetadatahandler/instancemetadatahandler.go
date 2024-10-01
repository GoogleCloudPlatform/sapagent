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

// Package instancemetadatahandler implements the handler for instancemetadata command.
package instancemetadatahandler

import (
	"context"
	"fmt"

	apb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/instancemetadata"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// RestartAgent indicates that the agent should be restarted after the instancemetadata guest action has been handled.
const RestartAgent = false

// InstanceMetadataHandler is the handler for instancemetadata command.
func InstanceMetadataHandler(ctx context.Context, command *gpb.Command, cp *ipb.CloudProperties) (*gpb.CommandResult, bool) {
	return instanceMetadataHandlerHelper(ctx, command, cp, nil)
}

func instanceMetadataHandlerHelper(ctx context.Context, command *gpb.Command, cp *ipb.CloudProperties, frc instancemetadata.ReadCloser) (*gpb.CommandResult, bool) {
	log.CtxLogger(ctx).Debugw("Instance metadata handler called.", "command", prototext.Format(command))
	im := &instancemetadata.InstanceMetadata{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), im)
	im.RC = frc
	instanceMetaDataResponse, msg, exitStatus := im.Run(ctx, onetime.CreateRunOptions(cp, true))
	anyInstanceMetaDataResponse, err := apb.New(instanceMetaDataResponse)
	if err != nil {
		failureMessage := fmt.Sprintf("failed to marshal response to any. Error: %v", err)
		log.CtxLogger(ctx).Debug(failureMessage)
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   msg,
			Stderr:   failureMessage,
			ExitCode: int32(subcommands.ExitFailure),
		}
		return result, RestartAgent
	}
	result := &gpb.CommandResult{
		Command:  command,
		Payload:  anyInstanceMetaDataResponse,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result, RestartAgent
}
