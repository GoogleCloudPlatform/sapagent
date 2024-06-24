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
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RestartAgent indicates that the agent should be restarted after the supportbundle guest action has been handled.
const RestartAgent = false

// SupportBundleHandler is the handler for support bundle command.
func SupportBundleHandler(ctx context.Context, command *gpb.AgentCommand, cp *ipb.CloudProperties) (string, subcommands.ExitStatus, bool) {
	log.CtxLogger(ctx).Debugw("Support bundle handler called.", "command", prototext.Format(command))
	sb := &supportbundle.SupportBundle{}
	handlers.ParseAgentCommandParameters(ctx, command, sb)
	msg, exitStatus := sb.ExecuteAndGetMessage(ctx, nil)
	return msg, exitStatus, RestartAgent
}
