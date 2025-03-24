/*
Copyright 2025 Google LLC

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

// Package gcbdractions connects to Agent Communication Service (ACS) and handles gcbdr actions in
// the agent. Messages received via ACS will typically have been sent via UAP Communication Highway.
package gcbdractions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcbdractions/handlers/gcbdrbackuphandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcbdractions/handlers/gcbdrdiscoveryhandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/communication"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

const (
	defaultChannel  = "backupdr.googleapis.com/backupdr-sap-channel"
	stagingChannel  = "backupdr.googleapis.com/backupdr-sap-channel-staging"
	autopushChannel = "backupdr.googleapis.com/backupdr-sap-channel-autopush"
	testChannel     = "gcbdr-test-channel"
	defaultEndpoint = ""
	agentCommand    = "agent_command"
	shellCommand    = "shell_command"
)

type gcbdrActionsHandler func(context.Context, *gpb.Command, *metadataserver.CloudProperties) *gpb.CommandResult

var gcbdrActionsHandlers = map[string]gcbdrActionsHandler{
	"gcbdr-backup":    gcbdrbackuphandler.GCBDRBackupHandler,
	"gcbdr-discovery": gcbdrdiscoveryhandler.GCBDRDiscoveryHandler,
}

type gcbdrActionsOptions struct {
	channel         string
	endpoint        string
	cloudProperties *ipb.CloudProperties
}

func anyResponse(ctx context.Context, gar *gpb.GCBDRActionResponse) *anypb.Any {
	any, err := anypb.New(gar)
	if err != nil {
		log.CtxLogger(ctx).Infow("Failed to marshal response to any.", "err", err)
		any = &anypb.Any{}
	}
	return any
}

func parseRequest(ctx context.Context, msg *anypb.Any) (*gpb.GCBDRActionRequest, error) {
	gaReq := &gpb.GCBDRActionRequest{}
	if err := msg.UnmarshalTo(gaReq); err != nil {
		errMsg := fmt.Sprintf("failed to unmarshal message: %v", err)
		return nil, errors.New(errMsg)
	}
	log.CtxLogger(ctx).Debugw("successfully unmarshalled message.", "gar", prototext.Format(gaReq))
	return gaReq, nil
}

func gcbdrActionResponse(ctx context.Context, results []*gpb.CommandResult, errorMessage string) *gpb.GCBDRActionResponse {
	return &gpb.GCBDRActionResponse{
		CommandResults: results,
		Error: &gpb.GCBDRActionError{
			ErrorMessage: errorMessage,
		},
	}
}

func handleShellCommand(ctx context.Context, command *gpb.Command, execute commandlineexecutor.Execute) *gpb.CommandResult {
	usagemetrics.Action(usagemetrics.UAPShellCommand)
	sc := command.GetShellCommand()
	result := execute(
		ctx,
		commandlineexecutor.Params{
			Executable:  sc.GetCommand(),
			ArgsToSplit: sc.GetArgs(),
			Timeout:     int(sc.GetTimeoutSeconds()),
		},
	)
	log.CtxLogger(ctx).Debugw("received result for shell command.",
		"command", prototext.Format(command), "stdOut", result.StdOut,
		"stdErr", result.StdErr, "error", result.Error, "exitCode", result.ExitCode)
	exitCode := int32(result.ExitCode)
	if exitCode == 0 && (result.Error != nil || result.StdErr != "") {
		exitCode = int32(1)
	}
	return &gpb.CommandResult{
		Command:  command,
		Stdout:   result.StdOut,
		Stderr:   result.StdErr,
		ExitCode: exitCode,
	}
}

func handleAgentCommand(ctx context.Context, command *gpb.Command, cloudProperties *metadataserver.CloudProperties) *gpb.CommandResult {
	agentCommand := strings.ToLower(command.GetAgentCommand().GetCommand())
	handler, ok := gcbdrActionsHandlers[agentCommand]
	if !ok {
		errMsg := fmt.Sprintf("received unknown agent command: %s", prototext.Format(command))
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   errMsg,
			Stderr:   errMsg,
			ExitCode: int32(1),
		}
		return result
	}
	result := handler(ctx, command, cloudProperties)
	log.CtxLogger(ctx).Debugw("received result for agent command.", "result", prototext.Format(result))
	return result
}

func errorResult(errMsg string) *gpb.CommandResult {
	return &gpb.CommandResult{
		Command:  nil,
		Stdout:   errMsg,
		Stderr:   errMsg,
		ExitCode: int32(1),
	}
}

func messageHandler(ctx context.Context, req *anypb.Any, cloudProperties *metadataserver.CloudProperties) (*anypb.Any, error) {
	var results []*gpb.CommandResult
	gar, err := parseRequest(ctx, req)
	if err != nil {
		log.CtxLogger(ctx).Debugw("failed to parse request.", "err", err)
		return nil, err
	}
	log.CtxLogger(ctx).Debugw("received GCBDRActionRequest to handle", "gar", prototext.Format(gar))
	for _, command := range gar.GetCommands() {
		log.CtxLogger(ctx).Debugw("processing command.", "command", prototext.Format(command))
		pr := command.ProtoReflect()
		fd := pr.WhichOneof(pr.Descriptor().Oneofs().ByName("command_type"))
		result := &gpb.CommandResult{}
		switch {
		case fd == nil:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return anyResponse(ctx, gcbdrActionResponse(ctx, results, errMsg)), errors.New(errMsg)
		case fd.Name() == shellCommand:
			result = handleShellCommand(ctx, command, commandlineexecutor.ExecuteCommand)
			results = append(results, result)
		case fd.Name() == agentCommand:
			result = handleAgentCommand(ctx, command, cloudProperties)
			results = append(results, result)
		default:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return anyResponse(ctx, gcbdrActionResponse(ctx, results, errMsg)), errors.New(errMsg)
		}
		// Exit early if we get an error
		if result.GetExitCode() != int32(0) {
			errMsg := fmt.Sprintf("received nonzero exit code with output: %s", result.GetStdout())
			return anyResponse(ctx, gcbdrActionResponse(ctx, results, errMsg)), errors.New(errMsg)
		}
	}
	return anyResponse(ctx, gcbdrActionResponse(ctx, results, "")), nil
}

func start(ctx context.Context, a any) {
	args, ok := a.(gcbdrActionsOptions)
	if !ok {
		log.CtxLogger(ctx).Warn("args is not of type gcbdrActionsOptions")
		return
	}
	communication.Communicate(ctx, args.endpoint, args.channel, messageHandler, protostruct.ConvertCloudPropertiesToStruct(args.cloudProperties))
}

// StartACSCommunication establishes communication with ACS.
// Returns true if the goroutine is started, and false otherwise.
func StartACSCommunication(ctx context.Context, config *cpb.Configuration) bool {
	if !config.GetGcbdrConfiguration().GetCommunicationEnabled().GetValue() {
		log.CtxLogger(ctx).Info("Not configured to communicate with GCBDR via ACS")
		return false
	}
	dailyMetricsRoutine := &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.GCBDRActionsStarted) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)

	var gcbdrChannel string
	switch config.GetGcbdrConfiguration().GetEnvironment() {
	case cpb.TargetEnvironment_STAGING:
		gcbdrChannel = stagingChannel
	case cpb.TargetEnvironment_AUTOPUSH:
		gcbdrChannel = autopushChannel
	default:
		gcbdrChannel = defaultChannel
	}
	communicateRoutine := &recovery.RecoverableRoutine{
		Routine: start,
		RoutineArg: gcbdrActionsOptions{
			channel:         gcbdrChannel,
			endpoint:        defaultEndpoint,
			cloudProperties: config.CloudProperties,
		},
		ErrorCode:           usagemetrics.GCBDRActionsFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting ACS GCBDR communication routine")
	communicateRoutine.StartRoutine(ctx)

	if config.GetGcbdrConfiguration().GetTestChannelEnabled().GetValue() {
		testRoutine := &recovery.RecoverableRoutine{
			Routine: start,
			RoutineArg: gcbdrActionsOptions{
				channel:         testChannel,
				endpoint:        defaultEndpoint,
				cloudProperties: config.CloudProperties,
			},
			ErrorCode:           usagemetrics.GCBDRActionsFailure,
			UsageLogger:         *usagemetrics.Logger,
			ExpectedMinDuration: 10 * time.Second,
		}
		log.CtxLogger(ctx).Info("Starting ACS GCBDR communication routine for test channel")
		testRoutine.StartRoutine(ctx)
	}
	return true
}
