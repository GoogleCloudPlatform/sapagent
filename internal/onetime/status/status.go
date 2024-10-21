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

// Package status implements the status subcommand to provide information
// on the agent, configuration, IAM and functional statuses.
package status

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/statushelper"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
)

type (
	// operatingConfig defines the operating configuration of a workflow.
	operatingConfig map[string]any

	// functionalCheck defines the functions that smoke test a workflow.
	functionalCheck func(context.Context, operatingConfig) (bool, string, error)

	// serviceCheck captures the info needed to run checks for a service.
	serviceCheck struct {
		Name            string
		IAMRoles        []string
		Config          operatingConfig
		FunctionalCheck functionalCheck
	}
)

const agentPackageName = "google-cloud-sap-agent"

// serviceChecks stores the check params for each agent service check.
// TOOD(b/362288235): Add checks for all services.
var serviceChecks = []serviceCheck{
	{
		Name: "Process Metrics",
		IAMRoles: []string{
			"roles/compute.viewer",
			"roles/monitoring.viewer",
			"roles/monitoring.metricWriter",
		},
		FunctionalCheck: processMetricsCheck,
	},
}

// Status stores the status subcommand parameters.
type Status struct {
	ConfigFilePath string

	verbose           bool
	help              bool
	logLevel, logPath string
	oteLogger         *onetime.OTELogger
	cloudProps        *iipb.CloudProperties
	readFile          configuration.ReadConfigFile
}

// Name implements the subcommand interface for status.
func (*Status) Name() string { return "status" }

// Synopsis implements the subcommand interface for status.
func (*Status) Synopsis() string { return "get the status of the agent and its services" }

// Usage implements the subcommand interface for status.
func (*Status) Usage() string {
	return `status [-config <path-to-config-file>] [-v]

  Get the status of the agent and its services.
  `
}

// SetFlags implements the subcommand interface for status.
func (s *Status) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.ConfigFilePath, "config", "", "Configuration path override")
	fs.StringVar(&s.ConfigFilePath, "c", "", "Configuration path override")
	fs.BoolVar(&s.verbose, "v", false, "Display verbose status information")
	fs.BoolVar(&s.help, "h", false, "Display help")
}

// Execute implements the subcommand interface for status.
func (s *Status) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// Initialize logging and cloud properties.
	lp, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     s.Name(),
		Help:     s.help,
		LogLevel: s.logLevel,
		LogPath:  s.logPath,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}
	s.cloudProps = cp

	// Run the status checks.
	agentStatus, exitStatus := s.Run(ctx, onetime.CreateRunOptions(cp, false))
	if exitStatus == subcommands.ExitFailure {
		// Collect support bundle if there's an error.
		supportbundle.CollectAgentSupport(ctx, f, lp, cp, s.Name())
	}
	statushelper.PrintStatus(ctx, agentStatus)
	return exitStatus
}

// Run executes the command and returns the status.
func (s *Status) Run(ctx context.Context, opts *onetime.RunOptions) (*spb.AgentStatus, subcommands.ExitStatus) {
	s.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	s.readFile = os.ReadFile
	status, err := s.statusHandler(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not get agent status", "error", err)
		return nil, subcommands.ExitFailure
	}
	return status, subcommands.ExitSuccess
}

// statusHandler executes the status checks and returns the results as the AgentStatus proto.
func (s *Status) statusHandler(ctx context.Context) (*spb.AgentStatus, error) {
	agentStatus, config := s.agentStatus(ctx)

	// TODO: Remove this once we have the functional checks.
	log.CtxLogger(ctx).Debugw("agentStatus", "agentStatus", agentStatus, "config", config)

	return agentStatus, nil
}

func (s *Status) agentStatus(ctx context.Context) (*spb.AgentStatus, *cpb.Configuration) {
	agentStatus := &spb.AgentStatus{
		AgentName:        agentPackageName,
		InstalledVersion: fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
	}

	path := s.ConfigFilePath
	if len(path) == 0 {
		switch runtime.GOOS {
		case "linux":
			path = configuration.LinuxConfigPath
		case "windows":
			path = configuration.WindowsConfigPath
		}
	}
	agentStatus.ConfigurationFilePath = path
	config, err := configuration.Read(path, s.readFile)
	agentStatus.ConfigurationValid = spb.State_SUCCESS_STATE
	if err != nil {
		agentStatus.ConfigurationValid = spb.State_FAILURE_STATE
		agentStatus.ConfigurationErrorMessage = err.Error()
	}
	config = configuration.ApplyDefaults(config, s.cloudProps)

	return agentStatus, config
}

// RunChecks runs the status checks for an agent service.
//
// Assumes the agent service is enabled - we do not need to perform these
// checks otherwise.
func (fc *serviceCheck) RunChecks(ctx context.Context) (*spb.ServiceStatus, error) {
	return nil, fmt.Errorf("RunChecks is not yet implemented")
}

// Below implement functional checks for each agent service.

// processMetricsCheck implements the functional check for process metrics.
func processMetricsCheck(ctx context.Context, config operatingConfig) (bool, string, error) {
	return false, "", fmt.Errorf("processMetricsCheck is not yet implemented")
}
