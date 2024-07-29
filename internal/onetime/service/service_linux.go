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

// Package service implements an OTE for disable+stop / enable+start the SAP Agent service.
package service

import (
	"context"
	_ "embed"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	executeCommand = commandlineexecutor.ExecuteCommand
)

// Service has args for service subcommands.
type Service struct {
	help, disable, enable bool
	logPath               string
}

// Name implements the subcommand interface for the service OTE.
func (*Service) Name() string { return "service" }

// Synopsis implements the subcommand interface for the service OTE.
func (*Service) Synopsis() string {
	return "disable and stop OR enable and start the google-cloud-sap-agent systemd service"
}

// Usage implements the subcommand interface for the service OTE.
func (*Service) Usage() string {
	return `Usage: service <subcommand> [args]

  Subcommands:
    -disable	  disables and stops the google-cloud-sap-agent systemd service
    -enable	    enables and starts the google-cloud-sap-agent systemd service

  Args (optional):
    [-log-path="/var/log/google-cloud-sap-agent/service.log"]			The full linux log path to write the log file (optional).
		    Default value is /var/log/google-cloud-sap-agent/service.log

  Global options:
    [-h]` + "\n"
}

// SetFlags implements the subcommand interface for the service OTE.
func (c *Service) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.help, "h", false, "Displays help")
	fs.BoolVar(&c.disable, "disable", false, "Disables and stops the google-cloud-sap-agent systemd service")
	fs.BoolVar(&c.enable, "enable", false, "Enables and starts the google-cloud-sap-agent systemd service")
	fs.StringVar(&c.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/service.log")
}

// Execute implements the subcommand interface for the service OTE.
func (c *Service) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     c.Name(),
		Help:     c.help,
		Fs:       f,
		LogLevel: "info",
		LogPath:  c.logPath,
	}, args...)
	if !completed {
		return exitStatus
	}
	if c.disable && c.enable {
		fmt.Printf("-disable and -enable cannot be specified at the same time.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("-disable and -enable cannot be specified at the same time")
		return subcommands.ExitUsageError
	}
	if !c.disable && !c.enable {
		fmt.Printf("-disable or -enable must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("-disable or -enable must be specified")
		return subcommands.ExitUsageError
	}
	if c.disable {
		if err := c.disableService(ctx); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Service disable: FAILED", err)
			usagemetrics.Error(usagemetrics.ServiceDisableFailure)
			return subcommands.ExitFailure
		}
	}
	if c.enable {
		if err := c.enableService(ctx); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Service enable: FAILED", err)
			usagemetrics.Error(usagemetrics.ServiceEnableFailure)
			return subcommands.ExitFailure
		}
	}
	return subcommands.ExitSuccess
}

func (c *Service) disableService(ctx context.Context) error {
	onetime.LogMessageToFileAndConsole(ctx, "Service disable starting")
	usagemetrics.Action(usagemetrics.ServiceDisableStarted)
	result := executeCommand(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"systemctl", "disable", "google-cloud-sap-agent"},
	})
	if result.Error != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Service dsiable failed", result.Error)
		return result.Error
	}
	result = executeCommand(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"systemctl", "stop", "google-cloud-sap-agent"},
	})
	if result.Error != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Service stop failed", result.Error)
		return result.Error
	}
	onetime.LogMessageToFileAndConsole(ctx, "Service disable succeeded")
	usagemetrics.Action(usagemetrics.ServiceDisableFinished)
	return nil
}

func (c *Service) enableService(ctx context.Context) error {
	onetime.LogMessageToFileAndConsole(ctx, "Service enable starting")
	usagemetrics.Action(usagemetrics.ServiceEnableStarted)
	result := executeCommand(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"systemctl", "enable", "google-cloud-sap-agent"},
	})
	if result.Error != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Service enable failed", result.Error)
		return result.Error
	}
	result = executeCommand(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args:       []string{"systemctl", "start", "google-cloud-sap-agent"},
	})
	if result.Error != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Service start failed", result.Error)
		return result.Error
	}
	onetime.LogMessageToFileAndConsole(ctx, "Service enable succeeded")
	usagemetrics.Action(usagemetrics.ServiceEnableFinished)
	return nil
}
