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

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

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
	verbose           bool
	help              bool
	logLevel, logPath string
	oteLogger         *onetime.OTELogger
}

// Name implements the subcommand interface for status.
func (*Status) Name() string { return "status" }

// Synopsis implements the subcommand interface for status.
func (*Status) Synopsis() string { return "get the status of the agent its services" }

// Usage implements the subcommand interface for status.
func (*Status) Usage() string {
	return `status [-v]

  Get the status of the agent and its services.
  `
}

// SetFlags implements the subcommand interface for status.
func (s *Status) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&s.verbose, "v", false, "display verbose status information")
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

	// Run the status checks.
	_, exitStatus = s.Run(ctx, onetime.CreateRunOptions(cp, false))
	if exitStatus == subcommands.ExitFailure {
		// Collect support bundle if there's an error.
		supportbundle.CollectAgentSupport(ctx, f, lp, cp, s.Name())
	}
	return exitStatus
}

// Run executes the command and returns the status.
func (s *Status) Run(ctx context.Context, opts *onetime.RunOptions) (*spb.AgentStatus, subcommands.ExitStatus) {
	s.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	status, err := s.statusHandler(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not get agent status", "error", err)
		return nil, subcommands.ExitFailure
	}
	return status, subcommands.ExitSuccess
}

// statusHandler executes the status checks and returns the results as the AgentStatus proto.
func (s *Status) statusHandler(ctx context.Context) (*spb.AgentStatus, error) {
	// This will be implemented in subsequent CLs for this functionality.
	return nil, fmt.Errorf("statusHandler is not yet implemented")
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
