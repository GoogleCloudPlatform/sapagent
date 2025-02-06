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
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/prototext"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/backinthandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/configurehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/configureinstancehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/gcbdrbackuphandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/gcbdrdiscoveryhandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/hanadiskbackuphandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/instancemetadatahandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/performancediagnosticshandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/supportbundlehandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/guestactions/handlers/versionhandler"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/metadataserver"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/uap"
	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/guestactions"
)

const (
	defaultChannel  = "workloadmanager.googleapis.com/wlm-sap-channel"
	testChannel     = "wlm-sap-test-channel"
	defaultEndpoint = ""
	agentCommand    = "agent_command"
	shellCommand    = "shell_command"
)

// GuestActions is a struct that holds the state for guest actions.
type GuestActions struct {
	CancelFunc context.CancelFunc
}

type guestActionHandler func(context.Context, *gpb.Command, *metadataserver.CloudProperties) *gpb.CommandResult

var guestActionsHandlers = map[string]guestActionHandler{
	"backint":                backinthandler.BackintHandler,
	"configure":              configurehandler.ConfigureHandler,
	"configureinstance":      configureinstancehandler.ConfigureInstanceHandler,
	"gcbdr-backup":           gcbdrbackuphandler.GCBDRBackupHandler,
	"gcbdr-discovery":        gcbdrdiscoveryhandler.GCBDRDiscoveryHandler,
	"hanadiskbackup":         hanadiskbackuphandler.HANADiskBackupHandler,
	"instancemetadata":       instancemetadatahandler.InstanceMetadataHandler,
	"performancediagnostics": performancediagnosticshandler.PerformanceDiagnosticsHandler,
	"supportbundle":          supportbundlehandler.SupportBundleHandler,
	"version":                versionhandler.VersionHandler,
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

func (g *GuestActions) handleAgentCommand(ctx context.Context, command *gpb.Command, cloudProperties *metadataserver.CloudProperties) *gpb.CommandResult {
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

func (g *GuestActions) messageHandler(ctx context.Context, gar *gpb.GuestActionRequest, cloudProperties *metadataserver.CloudProperties) *gpb.GuestActionResponse {
	var results []*gpb.CommandResult
	log.CtxLogger(ctx).Debugw("received GuestActionRequest to handle", "gar", prototext.Format(gar))
	for _, command := range gar.GetCommands() {
		log.CtxLogger(ctx).Debugw("processing command.", "command", prototext.Format(command))
		pr := command.ProtoReflect()
		fd := pr.WhichOneof(pr.Descriptor().Oneofs().ByName("command_type"))
		result := &gpb.CommandResult{}
		switch {
		case fd == nil:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return guestActionResponse(ctx, results, errMsg)
		case fd.Name() == shellCommand:
			result = handleShellCommand(ctx, command, commandlineexecutor.ExecuteCommand)
			results = append(results, result)
		case fd.Name() == agentCommand:
			result = g.handleAgentCommand(ctx, command, cloudProperties)
			results = append(results, result)
		default:
			errMsg := fmt.Sprintf("received unknown command: %s", prototext.Format(command))
			results = append(results, errorResult(errMsg))
			return guestActionResponse(ctx, results, errMsg)
		}
		// Exit early if we get an error
		if result.GetExitCode() != int32(0) {
			errMsg := fmt.Sprintf("received nonzero exit code with output: %s", result.GetStdout())
			return guestActionResponse(ctx, results, errMsg)
		}
	}
	return guestActionResponse(ctx, results, "")
}

func (g *GuestActions) start(ctx context.Context, a any) {
	args, ok := a.(guestActionsOptions)
	if !ok {
		log.CtxLogger(ctx).Warn("args is not of type guestActionsArgs")
		return
	}
	uap.CommunicateWithUAP(ctx, args.endpoint, args.channel, g.messageHandler, protostruct.ConvertCloudPropertiesToStruct(args.cloudProperties))
}

// StartUAPCommunication establishes communication with UAP Highway.
// Returns true if the goroutine is started, and false otherwise.
func (g *GuestActions) StartUAPCommunication(ctx context.Context, config *cpb.Configuration) bool {
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
		Routine: g.start,
		RoutineArg: guestActionsOptions{
			channel:         defaultChannel,
			endpoint:        defaultEndpoint,
			cloudProperties: config.CloudProperties,
		},
		ErrorCode:           usagemetrics.GuestActionsFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 10 * time.Second,
	}
	log.CtxLogger(ctx).Info("Starting UAP communication routine")
	communicateRoutine.StartRoutine(ctx)

	if config.GetUapConfiguration().GetTestChannelEnabled().GetValue() {
		testRoutine := &recovery.RecoverableRoutine{
			Routine: g.start,
			RoutineArg: guestActionsOptions{
				channel:         testChannel,
				endpoint:        defaultEndpoint,
				cloudProperties: config.CloudProperties,
			},
			ErrorCode:           usagemetrics.GuestActionsFailure,
			UsageLogger:         *usagemetrics.Logger,
			ExpectedMinDuration: 10 * time.Second,
		}
		log.CtxLogger(ctx).Info("Starting UAP communication routine for test channel")
		testRoutine.StartRoutine(ctx)
	}
	return true
}
