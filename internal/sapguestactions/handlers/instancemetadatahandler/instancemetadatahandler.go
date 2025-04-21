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
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/instancemetadata"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// InstanceMetadataHandler is the handler for instancemetadata command.
func InstanceMetadataHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	return instanceMetadataHandlerHelper(ctx, command, cp, nil)
}

func instanceMetadataHandlerHelper(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties, frc instancemetadata.ReadCloser) *gpb.CommandResult {
	log.CtxLogger(ctx).Debugw("Instance metadata handler called.", "command", prototext.Format(command))
	im := &instancemetadata.InstanceMetadata{}
	handlers.ParseAgentCommandParameters(ctx, command.GetAgentCommand(), im)
	im.RC = frc
	instanceMetaDataResponse, msg, exitStatus := im.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true))
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
		return result
	}
	result := &gpb.CommandResult{
		Command:  command,
		Payload:  anyInstanceMetaDataResponse,
		Stdout:   msg,
		ExitCode: int32(exitStatus),
	}
	return result
}
