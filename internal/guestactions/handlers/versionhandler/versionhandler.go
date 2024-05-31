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
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// RestartAgent indicates that the agent should be restarted after the version guest action has been handled.
const RestartAgent = false

// VersionHandler is the handler for the version command.
func VersionHandler(ctx context.Context, command *gpb.AgentCommand) (string, subcommands.ExitStatus, bool) {
	log.CtxLogger(ctx).Infow("VersionHandler was called. Command passed in is", "command", prototext.Format(command))
	msg := onetime.GetAgentVersion()
	exitStatus := subcommands.ExitSuccess
	log.CtxLogger(ctx).Infow("VersionHandler was called. Version returned -", "msg", msg, "exitStatus", exitStatus)
	return msg, exitStatus, RestartAgent
}
