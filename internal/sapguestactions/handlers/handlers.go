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

// Package handlers contains common methods which will be used by multiple handlers.
package handlers

import (
	"context"
	"encoding/json"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

// ParseAgentCommandParameters parses the command parameters from the
// AgentCommand proto into the provided object, using the json marshal/unmarshal.
// If json.Marshal fails, the object is not modified.
// "obj" is expected to be a pointer to a Command struct.
func ParseAgentCommandParameters(ctx context.Context, command *gpb.AgentCommand, obj subcommands.Command) {
	params := command.GetParameters()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failed to marshal all command parameters to JSON", "error", err)
		return
	}
	if err = json.Unmarshal(paramsJSON, obj); err != nil {
		log.CtxLogger(ctx).Debugw("Failed to unmarshal all command parameters into struct", "error", err)
	}
}
