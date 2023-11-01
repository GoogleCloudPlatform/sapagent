/*
Copyright 2023 Google LLC

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

// Package logusage implements the one time execution mode for
// invoking usage status logging.
package logusage

import (
	"context"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// LogUsage has args for logusage subcommands.
type LogUsage struct {
	name, agentVersion, agentPriorVersion, status, image string
	action, usageError                                   int
	help, version                                        bool
	logLevel                                             string
}

// Name implements the subcommand interface for logusage.
func (*LogUsage) Name() string { return "logusage" }

// Synopsis implements the subcommand interface for logusage.
func (*LogUsage) Synopsis() string { return "invoke usage status logging" }

// Usage implements the subcommand interface for logusage.
func (*LogUsage) Usage() string {
	return `Usage: logusage [-name <tool or agent name>] [-av <tool or agent version>]
	[-status <RUNNING|INSTALLED|...>] [-action <integer action code>] [-error <integer error code>]
	[-image <image URL of the compute instance>] [-v] [-h] [-loglevel=<debug|info|warn|error>]` + "\n"
}

// SetFlags implements the subcommand interface for logusage.
func (l *LogUsage) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&l.name, "name", configuration.AgentName, "Agent or Tool  name")
	fs.StringVar(&l.name, "n", configuration.AgentName, "Agent or Tool  name")
	fs.StringVar(&l.agentVersion, "agentVersion", configuration.AgentVersion, "Agent or Tool version")
	fs.StringVar(&l.agentVersion, "av", configuration.AgentVersion, "Agent or Tool version")
	fs.StringVar(&l.agentPriorVersion, "prior-version", "", "prior installed version")
	fs.StringVar(&l.agentPriorVersion, "pv", "", "prior installed version")
	fs.StringVar(&l.status, "status", "", "usage status value")
	fs.StringVar(&l.status, "s", "", "usage status value")
	fs.IntVar(&l.action, "action", 0, "usage action code")
	fs.IntVar(&l.action, "a", 0, "usage action code")
	fs.IntVar(&l.usageError, "error", 0, "usage error code")
	fs.IntVar(&l.usageError, "e", 0, "usage error code")
	fs.StringVar(&l.image, "image", "", "the image url of the compute instance(optional), default value is retreived from metadata)")
	fs.StringVar(&l.image, "i", "", "the image url of the compute instance(optional), default value is retreived from metadata)")
	fs.BoolVar(&l.help, "h", false, "help")
	fs.BoolVar(&l.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&l.logLevel, "loglevel", "", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for logusage.
func (l *LogUsage) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if l.help {
		return onetime.HelpCommand(f)
	}
	if l.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	_, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	cloudProps, ok := args[2].(*iipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	// Do not call SetupOnetimeLogging so file logging does not happen for this OTE
	return l.logUsageHandler(cloudProps)
}

func (l *LogUsage) logUsageHandler(cloudProps *iipb.CloudProperties) subcommands.ExitStatus {
	switch {
	case l.status == "":
		log.Print("A usage status value is required.")
		return subcommands.ExitUsageError
	case l.status == string(usagemetrics.StatusUpdated) && l.agentPriorVersion == "":
		log.Print("Prior agent version is required.")
		return subcommands.ExitUsageError
	case l.status == string(usagemetrics.StatusError) && l.usageError <= 0:
		log.Print("For status ERROR, an error code is required.")
		return subcommands.ExitUsageError
	case l.status == string(usagemetrics.StatusAction) && l.action <= 0:
		log.Print("For status ACTION, an action code is required.")
		return subcommands.ExitUsageError
	}

	if err := l.logUsageStatus(cloudProps); err != nil {
		log.Logger.Warnw("Could not log usage", "error", err)
	}
	return subcommands.ExitSuccess
}

// logUsageStatus makes a call to the appropriate usage metrics API.
func (l *LogUsage) logUsageStatus(cloudProps *iipb.CloudProperties) error {
	configureUsageMetricsForOTE(cloudProps, l.name, l.agentPriorVersion, l.image)
	switch usagemetrics.Status(l.status) {
	case usagemetrics.StatusRunning:
		usagemetrics.Running()
	case usagemetrics.StatusStarted:
		usagemetrics.Started()
	case usagemetrics.StatusStopped:
		usagemetrics.Stopped()
	case usagemetrics.StatusConfigured:
		usagemetrics.Configured()
	case usagemetrics.StatusMisconfigured:
		usagemetrics.Misconfigured()
	case usagemetrics.StatusError:
		usagemetrics.Error(l.usageError)
	case usagemetrics.StatusInstalled:
		usagemetrics.Installed()
	case usagemetrics.StatusUpdated:
		usagemetrics.Updated(configuration.AgentVersion)
	case usagemetrics.StatusUninstalled:
		usagemetrics.Uninstalled()
	case usagemetrics.StatusAction:
		usagemetrics.Action(l.action)
	default:
		return fmt.Errorf("logUsageStatus() called with an unknown status: %s", l.status)
	}
	return nil
}

func configureUsageMetricsForOTE(cp *iipb.CloudProperties, name, version, image string) {
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            name,
		Version:         version,
		LogUsageMetrics: true,
	})
	// Override the imageURL with value passed in args.
	if image != "" && cp != nil {
		cp.Image = image
	}
	usagemetrics.SetCloudProperties(cp)
}
