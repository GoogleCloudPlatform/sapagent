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

// Package guestactions connects to UAP Highway and handles guest actions in the agent.
package guestactions

import (
	"context"
	"fmt"
	"time"

	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/recovery"
	"github.com/GoogleCloudPlatform/sapagent/shared/uap"
)

const (
	defaultChannel  = "wlm-sap-channel"
	defaultEndpoint = ""
)

type guestActionsOptions struct {
	channel  string
	endpoint string
}

var handleShellCommand = func(ctx context.Context, command *gpb.ShellCommand) commandlineexecutor.Result {
	params := commandlineexecutor.Params{
		Executable:  command.GetCommand(),
		ArgsToSplit: command.GetArgs(),
	}
	if command.GetTimeoutSeconds() > 0 {
		params.Timeout = int(command.GetTimeoutSeconds())
	}
	return commandlineexecutor.ExecuteCommand(ctx, params)
}

func getAnyResponse(ctx context.Context, results []*gpb.CommandResult, errorMessage string) *anypb.Any {
	log.CtxLogger(ctx).Debugw("getAnyResponse() called on.", "results", results)
	response := &gpb.GuestActionResponse{
		CommandResults: results,
		Error: &gpb.GuestActionError{
			ErrorMessage: errorMessage,
		},
	}
	any, err := anypb.New(response)
	if err != nil {
		log.CtxLogger(ctx).Error("Failed to marshal response to any.", err)
	}
	return any
}

func messageHandler(ctx context.Context, message *anypb.Any) (*anypb.Any, error) {
	req := &gpb.GuestActionRequest{}
	if err := message.UnmarshalTo(req); err != nil {
		return nil, fmt.Errorf("messageHandler() failed to unmarshal message: %v", err)
	}
	log.CtxLogger(ctx).Debugw("Processing commands for guest action", "action",req.GetWorkloadAction().GetSapWorkloadAction())
	var results []*gpb.CommandResult
	for _, command := range req.GetCommands() {
		pr := command.ProtoReflect()
		fd := pr.WhichOneof(pr.Descriptor().Oneofs().ByName("command_type"))
		switch {
		case fd == nil || fd.Name() == "agent_command":
			error := fmt.Errorf("messageHandler() received unknown command: %s", prototext.Format(command))
			response := getAnyResponse(ctx, results, error.Error())
			return response, error
		case fd.Name() == "shell_command":
			result := handleShellCommand(ctx, command.GetShellCommand())
			log.CtxLogger(ctx).Debugw("messageHandler() received result for shell command.",
				"command", prototext.Format(command), "stdOut", result.StdOut,
				"stdErr", result.StdErr, "error", result.Error, "exitCode", result.ExitCode)
			results = append(results, &gpb.CommandResult{
				Command:  command,
				Stdout:   result.StdOut,
				Stderr:   result.StdErr,
				ExitCode: int32(result.ExitCode),
			})
			if result.Error != nil {
				response := getAnyResponse(ctx, results, result.Error.Error())
				return response, result.Error
			}
		}
	}

	response := getAnyResponse(ctx, results, "")
	return response, nil
}

func start(ctx context.Context, a any) {
	args, ok := a.(guestActionsOptions)
	if !ok {
		log.CtxLogger(ctx).Warn("args is not of type guestActionsArgs")
		return
	}
	uap.CommunicateWithUAP(ctx, args.endpoint, args.channel, messageHandler)
}

// StartUAPCommunication establishes communication with UAP Highway.
// Returns true if the goroutine is started, and false otherwise.
func StartUAPCommunication(ctx context.Context, config *cpb.Configuration) bool {
	if !config.GetUapConfiguration().GetEnabled().GetValue() {
		log.CtxLogger(ctx).Info("Not configured to communicate with UAP")
		return false
	}
	dailyMetricsRoutine := &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.GuestActionsStarted) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)

	communicateRoutine := &recovery.RecoverableRoutine{
		Routine:             start,
		RoutineArg:          guestActionsOptions{channel: defaultChannel, endpoint: defaultEndpoint},
		ErrorCode:           usagemetrics.GuestActionsFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting UAP communication routine")
	communicateRoutine.StartRoutine(ctx)
	return true
}