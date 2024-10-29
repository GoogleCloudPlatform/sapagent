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

const (
	agentPackageName        = "google-cloud-sap-agent"
	fetchLatestVersionError = "Error: could not fetch latest version"
)

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
	agentStatus.Services = append(agentStatus.Services, s.hostMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.processMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.hanaMonitoringMetricsStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.systemDiscoveryStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.backintStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.diskSnapshotStatus(ctx, config))
	agentStatus.Services = append(agentStatus.Services, s.workloadManagerStatus(ctx, config))

	return agentStatus, nil
}

// agentStatus returns the agent version, enabled/running, config path, and the
// configuration as parsed by the agent.
func (s *Status) agentStatus(ctx context.Context) (*spb.AgentStatus, *cpb.Configuration) {
	agentStatus := &spb.AgentStatus{
		AgentName:        agentPackageName,
		InstalledVersion: fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
	}

	var err error
	agentStatus.AvailableVersion, err = statushelper.FetchLatestVersion(ctx, agentPackageName, agentPackageName, runtime.GOOS)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not fetch latest version", "error", err)
		agentStatus.AvailableVersion = fetchLatestVersionError
	}

	enabled, running, err := statushelper.CheckAgentEnabledAndRunning(ctx, agentPackageName, runtime.GOOS)
	agentStatus.SystemdServiceEnabled = spb.State_FAILURE_STATE
	agentStatus.SystemdServiceRunning = spb.State_FAILURE_STATE
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not check agent enabled and running", "error", err)
		agentStatus.SystemdServiceEnabled = spb.State_ERROR_STATE
		agentStatus.SystemdServiceRunning = spb.State_ERROR_STATE
	} else {
		if enabled {
			agentStatus.SystemdServiceEnabled = spb.State_SUCCESS_STATE
		}
		if running {
			agentStatus.SystemdServiceRunning = spb.State_SUCCESS_STATE
		}
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

func (s *Status) hostMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:    "Host Metrics",
		Enabled: config.GetProvideSapHostAgentMetrics().GetValue(),
		IamRoles: []*spb.IAMRole{
			{Name: "Example compute viewer", Role: "roles/compute.viewer"},
		},
		ConfigValues: []*spb.ConfigValue{
			configValue("provide_sap_host_agent_metrics", config.GetProvideSapHostAgentMetrics().GetValue(), true),
		},
	}

	return status
}

func (s *Status) processMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "Process Metrics",
		Enabled:  config.GetCollectionConfiguration().GetCollectProcessMetrics(),
		IamRoles: []*spb.IAMRole{},
		ConfigValues: []*spb.ConfigValue{
			configValue("collect_process_metrics", config.GetCollectionConfiguration().GetCollectProcessMetrics(), false),
			configValue("process_metrics_frequency", config.GetCollectionConfiguration().GetProcessMetricsFrequency(), 5),
			configValue("process_metrics_to_skip", config.GetCollectionConfiguration().GetProcessMetricsToSkip(), []string{}),
			configValue("slow_process_metrics_frequency", config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency(), 30),
		},
	}

	return status
}

func (s *Status) hanaMonitoringMetricsStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "HANA Monitoring Metrics",
		Enabled:  config.GetHanaMonitoringConfiguration().GetEnabled(),
		IamRoles: []*spb.IAMRole{},
		ConfigValues: []*spb.ConfigValue{
			configValue("connection_timeout", config.GetHanaMonitoringConfiguration().GetConnectionTimeout().GetSeconds(), 120),
			configValue("enabled", config.GetHanaMonitoringConfiguration().GetEnabled(), false),
			configValue("execution_threads", config.GetHanaMonitoringConfiguration().GetExecutionThreads(), 10),
			configValue("max_connect_retries", config.GetHanaMonitoringConfiguration().GetMaxConnectRetries().GetValue(), 1),
			configValue("query_timeout_sec", config.GetHanaMonitoringConfiguration().GetQueryTimeoutSec(), 300),
			configValue("sample_interval_sec", config.GetHanaMonitoringConfiguration().GetSampleIntervalSec(), 300),
			configValue("send_query_response_time", config.GetHanaMonitoringConfiguration().GetSendQueryResponseTime(), false),
		},
	}
	// TODO: Do we need to include instances and queries in configuration?

	return status
}

func (s *Status) systemDiscoveryStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "System Discovery",
		Enabled:  config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue(),
		IamRoles: []*spb.IAMRole{},
		ConfigValues: []*spb.ConfigValue{
			configValue("enable_discovery", config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue(), true),
			configValue("enable_workload_discovery", config.GetDiscoveryConfiguration().GetEnableWorkloadDiscovery().GetValue(), true),
			configValue("sap_instances_update_frequency", config.GetDiscoveryConfiguration().GetSapInstancesUpdateFrequency().GetSeconds(), 60),
			configValue("system_discovery_update_frequency", config.GetDiscoveryConfiguration().GetSystemDiscoveryUpdateFrequency().GetSeconds(), 4*60*60),
		},
	}

	return status
}

func (s *Status) backintStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "Backint",
		Enabled:  false,
		IamRoles: []*spb.IAMRole{},
		// TODO: Add config values.
		ConfigValues: []*spb.ConfigValue{},
	}

	return status
}

func (s *Status) diskSnapshotStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "Disk Snapshot",
		Enabled:  false,
		IamRoles: []*spb.IAMRole{},
		// TODO: Add config values.
		ConfigValues: []*spb.ConfigValue{},
	}

	return status
}

func (s *Status) workloadManagerStatus(ctx context.Context, config *cpb.Configuration) *spb.ServiceStatus {
	status := &spb.ServiceStatus{
		Name:     "Workload Manager",
		Enabled:  config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics().GetValue(),
		IamRoles: []*spb.IAMRole{},
		ConfigValues: []*spb.ConfigValue{
			configValue("collect_workload_validation_metrics", config.GetCollectionConfiguration().GetCollectWorkloadValidationMetrics().GetValue(), true),
			configValue("config_target_environment", config.GetCollectionConfiguration().GetWorkloadValidationCollectionDefinition().GetConfigTargetEnvironment(), cpb.TargetEnvironment_PRODUCTION),
			configValue("fetch_latest_config", config.GetCollectionConfiguration().GetWorkloadValidationCollectionDefinition().GetFetchLatestConfig().GetValue(), true),
			configValue("workload_validation_db_metrics_frequency", config.GetCollectionConfiguration().GetWorkloadValidationDbMetricsFrequency(), 3600),
			configValue("workload_validation_metrics_frequency", config.GetCollectionConfiguration().GetWorkloadValidationMetricsFrequency(), 300),
		},
	}

	return status
}

func configValue(name string, value any, defaultValue any) *spb.ConfigValue {
	return &spb.ConfigValue{
		Name:      name,
		Value:     fmt.Sprint(value),
		IsDefault: fmt.Sprint(value) == fmt.Sprint(defaultValue),
	}
}
