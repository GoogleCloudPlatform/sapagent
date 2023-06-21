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
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// LogUsage has args for logusage subcommands.
type LogUsage struct {
	name, version, priorVersion, status string
	action, usageError                  int
}

// Name implements the subcommand interface for logusage.
func (*LogUsage) Name() string { return "logusage" }

// Synopsis implements the subcommand interface for logusage.
func (*LogUsage) Synopsis() string { return "invoke usage status logging" }

// Usage implements the subcommand interface for logusage.
func (*LogUsage) Usage() string {
	return "logusage [-name <tool or agent name>] [-version tool or agent version] [-status <RUNNING|INSTALLED|...>] [-action <integer action code>] [-error <integer error code>]\n"
}

// SetFlags implements the subcommand interface for logusage.
func (l *LogUsage) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&l.name, "name", configuration.AgentName, "Agent or Tool  name")
	fs.StringVar(&l.name, "n", configuration.AgentName, "Agent or Tool  name")
	fs.StringVar(&l.version, "version", configuration.AgentVersion, "Agent or Tool version")
	fs.StringVar(&l.version, "v", configuration.AgentVersion, "Agent or Tool version")
	fs.StringVar(&l.priorVersion, "prior-version", "", "prior installed version")
	fs.StringVar(&l.priorVersion, "pv", "", "prior installed version")
	fs.StringVar(&l.status, "status", "", "usage status value")
	fs.StringVar(&l.status, "s", "", "usage status value")
	fs.IntVar(&l.action, "action", 0, "usage action code")
	fs.IntVar(&l.action, "a", 0, "usage action code")
	fs.IntVar(&l.usageError, "error", 0, "usage error code")
	fs.IntVar(&l.usageError, "e", 0, "usage error code")
}

// Execute implements the subcommand interface for logusage.
func (l *LogUsage) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 3 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	cloudProps, ok := args[2].(*iipb.CloudProperties)
	if !ok {
		log.Logger.Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, l.Name())
	return l.logUsageHandler(cloudProps)
}

func (l *LogUsage) logUsageHandler(cloudProps *iipb.CloudProperties) subcommands.ExitStatus {
	switch {
	case l.status == "":
		log.Print("A usage status value is required.")
		return subcommands.ExitUsageError
	case l.status == string(usagemetrics.StatusUpdated) && l.priorVersion == "":
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
	configureUsageMetricsForOTE(cloudProps, l.name, l.priorVersion)
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

func configureUsageMetricsForOTE(cp *iipb.CloudProperties, name, version string) {
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            name,
		Version:         version,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}
