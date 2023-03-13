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

package onetime

import (
	"context"
	"fmt"
	"runtime"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// LogUsage has args for logusage subcommands.
type LogUsage struct {
	usagePriorVersion, usageStatus string
	usageAction, usageError        int
}

// Name implements the subcommand interface for logusage.
func (*LogUsage) Name() string { return "logusage" }

// Synopsis implements the subcommand interface for logusage.
func (*LogUsage) Synopsis() string { return "invoke usage status logging" }

// Usage implements the subcommand interface for logusage.
func (*LogUsage) Usage() string {
	return "logusage [-status <RUNNING|INSTALLED|...>] [-action <integer action code>] [-error <integer error code>]\\n"
}

// SetFlags implements the subcommand interface for logusage.
func (l *LogUsage) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&l.usagePriorVersion, "prior-version", "", "prior installed version")
	fs.StringVar(&l.usagePriorVersion, "pv", "", "prior installed version")
	fs.StringVar(&l.usageStatus, "status", "", "usage status value")
	fs.StringVar(&l.usageStatus, "s", "", "usage status value")
	fs.IntVar(&l.usageAction, "action", 0, "usage action code")
	fs.IntVar(&l.usageAction, "a", 0, "usage action code")
	fs.IntVar(&l.usageError, "error", 0, "usage error code")
	fs.IntVar(&l.usageError, "e", 0, "usage error code")
}

// Execute implements the subcommand interface for logusage.
func (l *LogUsage) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return l.logUsageHandler()
}

func (l *LogUsage) logUsageHandler() subcommands.ExitStatus {
	log.SetupOneTimeLogging(runtime.GOOS, l.Name(), cpb.Configuration_INFO)
	switch {
	case l.usageStatus == "":
		log.Print("A usage status value is required.")
		return subcommands.ExitUsageError
	case l.usageStatus == string(usagemetrics.StatusUpdated) && l.usagePriorVersion == "":
		log.Print("Prior agent version is required.")
		return subcommands.ExitUsageError
	case l.usageStatus == string(usagemetrics.StatusError) && l.usageError <= 0:
		log.Print("For status ERROR, an error code is required.")
		return subcommands.ExitUsageError
	case l.usageStatus == string(usagemetrics.StatusAction) && l.usageAction <= 0:
		log.Print("For status ACTION, an action code is required.")
		return subcommands.ExitUsageError
	}

	if err := l.logUsageStatus(metadataserver.FetchCloudProperties()); err != nil {
		log.Logger.Warnw("Could not log usage", "error", err)
	}

	return subcommands.ExitSuccess
}

// logUsageStatus makes a call to the appropriate usage metrics API.
func (l *LogUsage) logUsageStatus(cloudProps *iipb.CloudProperties) error {
	configureUsageMetricsForOTE(cloudProps, l.usagePriorVersion)
	switch usagemetrics.Status(l.usageStatus) {
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
		usagemetrics.Action(l.usageAction)
	default:
		return fmt.Errorf("logUsageStatus() called with an unknown status: %s", l.usageStatus)
	}
	return nil
}

func configureUsageMetricsForOTE(cp *iipb.CloudProperties, version string) {
	if version == "" {
		version = configuration.AgentVersion
	}
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            configuration.AgentName,
		Version:         version,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}
