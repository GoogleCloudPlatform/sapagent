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

// Package restart contains the logic to restart the agent.
package restart

import (
	"context"
	"strings"
	"time"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

// LinuxRestartAgent restarts the agent.
func LinuxRestartAgent(ctx context.Context) subcommands.ExitStatus {
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: "systemctl restart google-cloud-sap-agent",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent")
		log.CtxLogger(ctx).Errorw("failed restarting sap agent", "errorCode:", result.ExitCode, "error:", result.StdErr, "stdout:", result.StdOut)
		return subcommands.ExitFailure
	}
	log.Print("Waiting 70 seconds for agent restart")
	log.CtxLogger(ctx).Info("Waiting 70 seconds for agent restart")
	time.Sleep(70 * time.Second)

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: "systemctl status google-cloud-sap-agent",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent")
		log.CtxLogger(ctx).Errorw("failed checking the status of sap agent", "errorCode:", result.ExitCode, "error:", result.StdErr, "stdout:", result.StdOut)
		return subcommands.ExitFailure
	}

	if strings.Contains(result.StdOut, "running") {
		log.CtxLogger(ctx).Info("Restarted the agent")
		return subcommands.ExitSuccess
	}
	log.Print("Could not restart the agent")
	log.CtxLogger(ctx).Error("Agent is not running")
	return subcommands.ExitFailure
}

// WindowsRestartAgent restarts the agent on Windows OS.
func WindowsRestartAgent(ctx context.Context) subcommands.ExitStatus {
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "Powershell",
		ArgsToSplit: "Restart-Service -Force -Name 'google-cloud-sap-agent'",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent on Windows OS")
		log.CtxLogger(ctx).Errorw("failed restarting sap agent on Windows OS", "errorCode:", result.ExitCode, "error:", result.StdErr)
		return subcommands.ExitFailure
	}
	log.Print("Waiting 70 seconds for agent restart on Windows OS")
	log.CtxLogger(ctx).Info("Waiting 70 seconds for agent restart on Windows OS")
	time.Sleep(70 * time.Second)

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "Powershell",
		ArgsToSplit: "$(Get-Service -Name 'google-cloud-sap-agent').Status",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent on Windows OS")
		log.CtxLogger(ctx).Errorw("failed checking the status of sap agent on Windows OS", "errorCode:", result.ExitCode, "error:", result.StdErr)
		return subcommands.ExitFailure
	}

	if strings.Contains(strings.ToLower(result.StdOut), "running") {
		log.CtxLogger(ctx).Info("Restarted the agent on Windows OS")
		return subcommands.ExitSuccess
	}
	log.Print("Could not restart the agent on Windows OS")
	log.CtxLogger(ctx).Error("Agent is not running on Windows OS")
	return subcommands.ExitFailure
}
