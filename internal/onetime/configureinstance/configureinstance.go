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

// Package configureinstance implements OTE mode for checking and applying
// OS settings to support SAP HANA workloads.
package configureinstance

import (
	"context"
	"fmt"
	"os"
	"strings"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// writeFileFunc provides a testable replacement for os.WriteFile.
	writeFileFunc func(string, []byte, os.FileMode) error

	// readFileFunc provides a testable replacement for os.ReadFile.
	readFileFunc func(string) ([]byte, error)
)

// ConfigureInstance has args for configureinstance subcommands.
type ConfigureInstance struct {
	check, apply  bool
	machineType   string
	overrideOLAP  bool
	help, version bool

	writeFile writeFileFunc
	readFile  readFileFunc
	execute   commandlineexecutor.Execute
}

// Name implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Name() string { return "configureinstance" }

// Synopsis implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Synopsis() string {
	return "check and apply OS settings to support SAP HANA workloads"
}

// Usage implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Usage() string {
	return `Usage: configureinstance <subcommand> [args]

  Subcommands:
    -check	Check settings and print errors, but do not apply any changes
    -apply	Make changes as necessary to the settings

  Args:
    -overrideType="type"	Bypass the metadata machine type lookup
    -overrideOLAP=true		If true, removes 'nosmt' from the 'GRUB_CMDLINE_LINUX_DEFAULT' in '/etc/default/grub'

  Global options:
    [-h] [-v]` + "\n"
}

// SetFlags implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.check, "check", false, "Check settings and print errors, but do not apply any changes")
	fs.BoolVar(&c.apply, "apply", false, "Apply changes as necessary to the settings")
	fs.StringVar(&c.machineType, "overrideType", "", "Bypass the metadata machine type lookup")
	fs.BoolVar(&c.overrideOLAP, "overrideOLAP", false, "If true, removes 'nosmt' from the 'GRUB_CMDLINE_LINUX_DEFAULT' in '/etc/default/grub'")
	fs.BoolVar(&c.help, "h", false, "Displays help")
	fs.BoolVar(&c.version, "v", false, "Displays the current version of the agent")
}

// Execute implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if c.help {
		return onetime.HelpCommand(f)
	}
	if c.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	cloudProps, ok := args[2].(*ipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, c.Name(), log.StringLevelToZapcore("info"))

	if !c.check && !c.apply {
		fmt.Printf("-check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("-check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if c.check && c.apply {
		fmt.Printf("Only one of -check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("Only one of -check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if c.machineType == "" {
		c.machineType = cloudProps.GetMachineType()
	}

	c.writeFile = os.WriteFile
	c.readFile = os.ReadFile
	c.execute = commandlineexecutor.ExecuteCommand
	if err := c.configureInstanceHandler(ctx); err != nil {
		fmt.Println("ConfigureInstance: FAILED, detailed logs are at /var/log/google-cloud-sap-agent/configureinstance.log, err: ", err)
		log.CtxLogger(ctx).Errorw("ConfigureInstance failed", "err", err)
		usagemetrics.Error(usagemetrics.ConfigureInstanceFailure)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

// configureInstanceHandler checks and applies OS settings
// depending on the machine type.
func (c *ConfigureInstance) configureInstanceHandler(ctx context.Context) error {
	LogToBoth(ctx, "ConfigureInstance starting")
	usagemetrics.Action(usagemetrics.ConfigureInstanceStarted)
	rebootRequired := false

	LogToBoth(ctx, fmt.Sprintf("Using machine type: %s", c.machineType))
	switch {
	// TODO: Add support for X4 customizations
	case strings.HasPrefix(c.machineType, "x4"):
	default:
		return fmt.Errorf("unsupported machine type: %s", c.machineType)
	}

	LogToBoth(ctx, "ConfigureInstance: SUCCESS, detailed logs are at /var/log/google-cloud-sap-agent/configureinstance.log")
	usagemetrics.Action(usagemetrics.ConfigureInstanceFinished)
	if rebootRequired {
		LogToBoth(ctx, "\nReboot required")
	}
	return nil
}

// LogToBoth prints to the console and writes an INFO msg to the log file.
func LogToBoth(ctx context.Context, msg string) {
	fmt.Println(msg)
	log.CtxLogger(ctx).Info(msg)
}
