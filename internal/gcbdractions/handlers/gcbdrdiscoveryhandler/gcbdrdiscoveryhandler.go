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
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/discovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	apb "google.golang.org/protobuf/types/known/anypb"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

var (
	gcbdrDiscoveryHandlerExec = func() commandlineexecutor.Execute {
		return commandlineexecutor.ExecuteCommand
	}
	anyNew = apb.New
)

// GCBDRDiscoveryHandler is the handler for the gcbdr-discovery command.
func GCBDRDiscoveryHandler(ctx context.Context, command *gpb.Command, cp *metadataserver.CloudProperties) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPGCBDRDiscoveryCommand)
	log.CtxLogger(ctx).Debugw("gcbdr-discovery handler called.", "command", prototext.Format(command))
	d := &discovery.Discovery{}
	applications, result := d.Run(ctx, onetime.CreateRunOptions(protostruct.ConvertCloudPropertiesToProto(cp), true), gcbdrDiscoveryHandlerExec(), filesystem.Helper{})
	result.Command = command
	if result.GetExitCode() != 0 {
		return result
	}
	anyApplications, err := anyNew(applications)
	if err != nil {
		failureMessage := fmt.Sprintf("Failed to marshal response to any. Error: %v", err)
		log.CtxLogger(ctx).Debug(failureMessage)
		result.Stderr = failureMessage
		return result
	}
	result.Payload = anyApplications
	return result
}
