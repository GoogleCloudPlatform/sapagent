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
	"runtime"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/configurehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/hanadiskbackuphandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/supportbundlehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/performancediagnosticshandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/versionhandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/restart"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/recovery"
	"github.com/GoogleCloudPlatform/sapagent/shared/uap"
)

const (
	defaultChannel  = "wlm-sap-channel"
	defaultEndpoint = ""
	agentCommand    = "agent_command"
	shellCommand    = "shell_command"
)

type guestActionHandler func(context.Context, *gpb.AgentCommand, *ipb.CloudProperties) (string, subcommands.ExitStatus, bool)

var guestActionsHandlers = map[string]guestActionHandler{
	"configure":      configurehandler.ConfigureHandler,
	"hanadiskbackup": hanadiskbackuphandler.HANADiskBackupHandler,
	"performancediagnostics": performancediagnosticshandler.PerformanceDiagnosticsHandler,
	"supportbundle": supportbundlehandler.SupportBundleHandler,
	"version":            versionhandler.VersionHandler,
}

type guestActionsOptions struct {
	channel         string
	endpoint        string
	cloudProperties *ipb.CloudProperties
}

func guestActionResponse(ctx context.Context, results []*gpb.CommandResult, errorMessage string) *gpb.GuestActionResponse {
	return &gpb.GuestActionResponse{
		CommandResults: results,
		Error: &gpb.GuestActionError{
			ErrorMessage: errorMessage,
		},
	}
}

func handleShellCommand(ctx context.Context, command *gpb.Command, execute commandlineexecutor.Execute) *gpb.CommandResult {
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

func handleAgentCommand(ctx context.Context, command *gpb.Command, requiresRestart bool, cloudProperties *ipb.CloudProperties) (*gpb.CommandResult, bool) {
	agentCommand := strings.ToLower(command.GetAgentCommand().GetCommand())
	handler, ok := guestActionsHandlers[agentCommand]
	if !ok {
		errMsg := fmt.Sprintf("received unknown agent command: %s", prototext.Format(command))
		result := &gpb.CommandResult{
			Command:  command,
			Stdout:   errMsg,
			Stderr:   errMsg,
			ExitCode: int32(1),
		}
		return result, requiresRestart
	}
	output, exitStatus, restart := handler(ctx, command.GetAgentCommand(), cloudProperties)
	requiresRestart = requiresRestart || restart
	log.CtxLogger(ctx).Debugw("received result for agent command",
		"command", prototext.Format(command), "output", output, "exitStatus", exitStatus)
	result := &gpb.CommandResult{
		Command:  command,
		Stdout:   output,
		Stderr:   "",
		ExitCode: int32(exitStatus),
	}
	return result, requiresRestart
}

func errorResult(errMsg string) *gpb.CommandResult {
	return &gpb.CommandResult{
		Command:  nil,
		Stdout:   errMsg,
		Stderr:   errMsg,
		ExitCode: int32(1),
	}
}

func messageHandler(ctx context.Context, gar *gpb.GuestActionRequest, cloudProperties *ipb.CloudProperties) (*gpb.GuestActionResponse, bool) {
	requiresRestart := false
	var results []*gpb.CommandResult
	log.CtxLogger(ctx).Debugw("received GuestActionReqest to handle", "gar", prototext.Format(gar))
	for _, command := range gar.GetCommands() {
		log.CtxLogger(ctx).Debugw("processing command.", "command", prototext.Format(command))
		pr := command.ProtoReflect()
		fd := pr.WhichOneof(pr.Descriptor().Oneofs().ByName("command_type"))
		result := &gpb.CommandResult{}
		switch {
		case fd == nil:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return guestActionResponse(ctx, results, errMsg), false
		case fd.Name() == shellCommand:
			result = handleShellCommand(ctx, command, commandlineexecutor.ExecuteCommand)
			results = append(results, result)
		case fd.Name() == agentCommand:
			result, requiresRestart = handleAgentCommand(ctx, command, requiresRestart, cloudProperties)
			results = append(results, result)
		default:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return guestActionResponse(ctx, results, errMsg), requiresRestart
		}
		// Exit early if we get an error
		if result.GetExitCode() != int32(0) {
			errMsg := fmt.Sprintf("received nonzero exit code with output: %s", result.GetStdout())
			return guestActionResponse(ctx, results, errMsg), requiresRestart
		}
	}
	return guestActionResponse(ctx, results, ""), requiresRestart
}

func start(ctx context.Context, a any) {
	args, ok := a.(guestActionsOptions)
	if !ok {
		log.CtxLogger(ctx).Warn("args is not of type guestActionsArgs")
		return
	}
	restartHandler := restart.LinuxRestartAgent
	if runtime.GOOS == "windows" {
		restartHandler = restart.WindowsRestartAgent
	}
	uap.CommunicateWithUAP(ctx, args.endpoint, args.channel, messageHandler, restartHandler, args.cloudProperties)
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
		RoutineArg:          guestActionsOptions{channel: defaultChannel, endpoint: defaultEndpoint, cloudProperties: config.CloudProperties},
		ErrorCode:           usagemetrics.GuestActionsFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting UAP communication routine")
	communicateRoutine.StartRoutine(ctx)
	return true
}
