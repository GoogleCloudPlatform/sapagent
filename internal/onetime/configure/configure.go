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

// Package configure provides the leverage of enabling or disabling features by modifying configuration.json.
package configure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"

	"flag"
	"github.com/google/safetext/shsprintf"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/restart"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Configure has args for backint subcommands.
type Configure struct {
	Feature                    string       `json:"feature"`
	LogLevel                   string       `json:"loglevel"`
	Setting                    string       `json:"setting"`
	Path                       string       `json:"path"`
	SkipMetrics                string       `json:"process_metrics_to_skip"`
	ValidationMetricsFrequency int64        `json:"workload_evaluation_metrics_frequency,string"`
	DbFrequency                int64        `json:"workload_evaluation_db_metrics_frequency,string"`
	FastMetricsFrequency       int64        `json:"process_metrics_frequency,string"`
	SlowMetricsFrequency       int64        `json:"slow_process_metrics_frequency,string"`
	AgentMetricsFrequency      int64        `json:"agent_metrics_frequency,string"`
	AgentHealthFrequency       int64        `json:"agent_health_frequency,string"`
	HeartbeatFrequency         int64        `json:"heartbeat_frequency,string"`
	SampleIntervalSec          int64        `json:"sample_interval_sec,string"`
	QueryTimeoutSec            int64        `json:"query_timeout_sec,string"`
	Help                       bool         `json:"help,string"`
	Enable                     bool         `json:"enable,string"`
	Disable                    bool         `json:"disable,string"`
	Showall                    bool         `json:"showall,string"`
	Add                        bool         `json:"add,string"`
	Remove                     bool         `json:"remove,string"`
	RestartAgent               RestartAgent `json:"-"`
	LogPath                    string       `json:"log-path"`
	usageFunc                  func()
	oteLogger                  *onetime.OTELogger
}

// RestartAgent abstracts restart functions(windows & linux) for testability.
type RestartAgent func(context.Context) subcommands.ExitStatus

const (
	hostMetrics        = "host_metrics"
	processMetrics     = "process_metrics"
	hanaMonitoring     = "hana_monitoring"
	sapDiscovery       = "sap_discovery"
	agentMetrics       = "agent_metrics"
	workloadValidation = "workload_evaluation"
	workloadDiscovery  = "workload_discovery"
)

var (
	loglevels = map[string]cpb.Configuration_LogLevel{
		"debug": cpb.Configuration_DEBUG,
		"info":  cpb.Configuration_INFO,
		"warn":  cpb.Configuration_WARNING,
		"error": cpb.Configuration_ERROR,
	}
	spaces = regexp.MustCompile(`\s+`)
)

// Name implements the subcommand interface for features.
func (*Configure) Name() string {
	return "configure"
}

// Synopsis implements the subcommand interface for features.
func (*Configure) Synopsis() string {
	return `enable/disable collection of following: host metrics, process metrics, hana monitoring, sap discovery, agent metrics, workload validation metrics`
}

// Usage implements the subcommand interface for features.
func (*Configure) Usage() string {
	return `Usage:
configure [-feature=<host_metrics|process_metrics|hana_monitoring|sap_discovery|agent_metrics|workload_evaluation|workload_discovery> | -setting=<bare_metal|log_to_cloud>]
[-enable|-disable] [-showall] [-h]
[process_metrics_frequency=<int>] [slow_process_metrics_frequency=<int>]
[process_metrics_to_skip=<"comma-separated-metrics">] [-add|-remove]
[workload_evaluation_metrics_frequency=<int>] [workload_evaluation_db_metrics_frequency=<int>]
[-agent_metrics_frequency=<int>] [agent_health_frequency=<int>]
[heartbeat_frequency=<int>] [sample_interval_sec=<int>] [query_timeout_sec=<int>] [-log-path=<log-path>]
`
}

// SetFlags implements the subcommand interface for features.
func (c *Configure) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.Feature, "feature", "", "The requested feature. Valid values are: host_metrics, process_metrics, hana_monitoring, sap_discovery, agent_metrics, workload_evaluation, workload_discovery")
	fs.StringVar(&c.Feature, "f", "", "The requested feature. Valid values are: host_metrics, process_metrics, hana_monitoring, sap_discovery, agent_metrics, workload_evaluation, workload_discovery")
	fs.StringVar(&c.LogLevel, "loglevel", "", "Sets the logging level for the agent configuration file")
	fs.StringVar(&c.Setting, "setting", "", "The requested setting. Valid values are: bare_metal, log_to_cloud")
	fs.StringVar(&c.SkipMetrics, "process_metrics_to_skip", "", "Add or remove the list of metrics to skip during process metrics collection")
	fs.Int64Var(&c.ValidationMetricsFrequency, "workload_evaluation_metrics_frequency", 0, "Sets the frequency of workload validation metrics collection. Default value is 300(s)")
	fs.Int64Var(&c.DbFrequency, "workload_evaluation_db_metrics_frequency", 0, "Sets the database frequency of workload validation metrics collection. Default value is 3600(s)")
	fs.Int64Var(&c.FastMetricsFrequency, "process_metrics_frequency", 0, "Sets the frequency of fast moving process metrics collection. Default value is 5(s)")
	fs.Int64Var(&c.SlowMetricsFrequency, "slow_process_metrics_frequency", 0, "Sets the frequency of slow moving process metrics collection. Default value is 30(s)")
	fs.Int64Var(&c.AgentMetricsFrequency, "agent_metrics_frequency", 0, "Sets the agent metrics frequency. Default value is 60(s)")
	fs.Int64Var(&c.AgentHealthFrequency, "agent_health_frequency", 0, "Sets the agent health frequency. Default value is 60(s)")
	fs.Int64Var(&c.HeartbeatFrequency, "heartbeat_frequency", 0, "Sets the heartbeat frequency. Default value is 60(s)")
	fs.Int64Var(&c.SampleIntervalSec, "sample_interval_sec", 0, "Sets the sample interval sec for HANA Monitoring. Default value is 300(s)")
	fs.Int64Var(&c.QueryTimeoutSec, "query_timeout_sec", 0, "Sets the query timeout for HANA Monitoring. Default value is 300(s)")
	fs.BoolVar(&c.Help, "help", false, "Display help")
	fs.BoolVar(&c.Help, "h", false, "Display help")
	fs.StringVar(&c.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/configure.log")
	fs.BoolVar(&c.Showall, "showall", false, "Display the status of all features")
	fs.BoolVar(&c.Enable, "enable", false, "Enable the requested feature/setting")
	fs.BoolVar(&c.Disable, "disable", false, "Disable the requested feature/setting")
	fs.BoolVar(&c.Add, "add", false, "Add the requested list of process metrics to skip. process-metrics-to-skip should not be empty")
	fs.BoolVar(&c.Remove, "remove", false, "Remove the requested list of process metrics to skip. process-metrics-to-skip should not be empty")
}

// Execute implements the subcommand interface for feature.
func (c *Configure) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     c.Name(),
		Help:     c.Help,
		Fs:       fs,
		LogLevel: c.LogLevel,
		LogPath:  c.LogPath,
	}, args...)
	if !completed {
		return exitStatus
	}
	c.usageFunc = fs.Usage
	_, res := c.Run(ctx, onetime.CreateRunOptions(cp, false), args...)
	return res
}

// Run executes the command and returns a message string in addition to the status.
func (c *Configure) Run(ctx context.Context, runOpts *onetime.RunOptions, args ...any) (string, subcommands.ExitStatus) {
	c.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	if c.Path == "" {
		c.Path = configuration.LinuxConfigPath
		if runtime.GOOS == "windows" {
			c.Path = configuration.WindowsConfigPath
		}
	}

	if c.Showall {
		return c.showFeatures(ctx)
	}

	if c.RestartAgent == nil {
		c.RestartAgent = restart.LinuxRestartAgent
		if runtime.GOOS == "windows" {
			c.RestartAgent = restart.WindowsRestartAgent
		}
	}

	newCfg, res := c.modifyConfig(ctx, os.ReadFile)
	if res == subcommands.ExitSuccess {
		c.oteLogger.LogMessageToConsole("Successfully modified configuration.json and restarted the agent.")
	}
	return newCfg, res
}

// setStatus returns a map of feature name and its status.
func setStatus(ctx context.Context, config *cpb.Configuration) map[string]bool {
	featureStatus := map[string]bool{
		hostMetrics:        true,
		hanaMonitoring:     false,
		agentMetrics:       false,
		workloadValidation: true,
		processMetrics:     false,
		sapDiscovery:       false,
		workloadDiscovery:  false,
	}

	if hm := config.GetProvideSapHostAgentMetrics(); hm != nil {
		featureStatus[hostMetrics] = hm.GetValue()
	}
	if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
		featureStatus[hanaMonitoring] = hmc.GetEnabled()
	}

	if cc := config.GetCollectionConfiguration(); cc != nil {
		featureStatus[agentMetrics] = cc.GetCollectAgentMetrics()
		if wlm := cc.GetCollectWorkloadValidationMetrics(); wlm != nil {
			featureStatus[workloadValidation] = wlm.GetValue()
		}
		featureStatus[processMetrics] = cc.GetCollectProcessMetrics()
	}

	if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
		featureStatus[sapDiscovery] = true
	}

	if config.GetDiscoveryConfiguration().GetEnableWorkloadDiscovery().GetValue() {
		featureStatus[workloadDiscovery] = true
	}

	log.CtxLogger(ctx).Info("Feature status: ", featureStatus)
	return featureStatus
}

// showFeatures displays the status of all features.
func (c *Configure) showFeatures(ctx context.Context) (string, subcommands.ExitStatus) {
	config := configuration.Read(c.Path, os.ReadFile)
	if config == nil {
		c.oteLogger.LogMessageToFileAndConsole(ctx, "Unable to read configuration.json")
		return "Unable to read configuration.json", subcommands.ExitFailure
	}

	featureStatus := setStatus(ctx, config)
	var enabled, disabled []string
	var output string

	for feature, status := range featureStatus {
		if status {
			enabled = append(enabled, feature)
		} else {
			disabled = append(disabled, feature)
		}
	}

	isWindows := runtime.GOOS == "windows"
	for _, feature := range enabled {
		out, err := showStatus(ctx, feature, featureStatus[feature], isWindows)
		if err != nil {
			return err.Error(), subcommands.ExitFailure
		}
		output += out
	}
	for _, feature := range disabled {
		out, err := showStatus(ctx, feature, featureStatus[feature], isWindows)
		if err != nil {
			return err.Error(), subcommands.ExitFailure
		}
		output += out
	}
	c.oteLogger.LogMessageToConsole(output)
	return output, subcommands.ExitSuccess
}

// modifyConfig takes user input and enables/disables features in configuration.json and restarts the agent.
func (c *Configure) modifyConfig(ctx context.Context, read configuration.ReadConfigFile) (string, subcommands.ExitStatus) {
	log.Logger.Infow("Beginning execution of features command")
	config := configuration.Read(c.Path, read)
	if config == nil {
		c.oteLogger.LogMessageToFileAndConsole(ctx, "Unable to read configuration.json")
		return "Unable to read configuration.json", subcommands.ExitFailure
	}
	log.CtxLogger(ctx).Infow("Config before any changes", "config", config)

	isCmdValid := false
	if len(c.LogLevel) > 0 {
		if _, ok := loglevels[c.LogLevel]; !ok {
			c.oteLogger.LogMessageToFileAndConsole(ctx, "Invalid log level. Please use [debug, info, warn, error]")
			return "Invalid log level. Please use [debug, info, warn, error]", subcommands.ExitUsageError
		}
		isCmdValid = true
		config.LogLevel = loglevels[c.LogLevel]
	}

	if len(c.Feature) > 0 {
		if res := c.modifyFeature(ctx, config); res != subcommands.ExitSuccess {
			return "Failed to modify feature", res
		}
		isCmdValid = true
	} else if len(c.Setting) > 0 {
		if !c.Enable && !c.Disable {
			c.oteLogger.LogMessageToFileAndConsole(ctx, "Please choose to enable or disable the given feature/setting\n")
			return "Please choose to enable or disable the given feature/setting", subcommands.ExitUsageError
		}

		isEnabled := c.Enable
		switch c.Setting {
		case "bare_metal":
			config.BareMetal = isEnabled
		case "log_to_cloud":
			config.LogToCloud = &wpb.BoolValue{Value: isEnabled}
		default:
			c.oteLogger.LogMessageToFileAndConsole(ctx, "Unsupported setting")
			return "Unsupported setting", subcommands.ExitUsageError
		}
		isCmdValid = true
	}

	if !isCmdValid {
		c.oteLogger.LogMessageToFileAndConsole(ctx, "Insufficient flags. Please check usage:\n")
		// Checking for nil for testing purposes
		if c.usageFunc != nil {
			c.usageFunc()
		}
		return "Insufficient flags. Please check usage", subcommands.ExitUsageError
	}

	newCfg, err := c.writeFile(ctx, config, c.Path)
	if err != nil {
		c.oteLogger.LogErrorToFileAndConsole(ctx, "Unable to write configuration.json", err)
		return newCfg, subcommands.ExitUsageError
	}
	return newCfg, c.RestartAgent(ctx)
}

// modifyFeature takes user input and modifies fields related to a particular feature, which could simply
// be enabling or disabling the feature or updating frequency values, for instance, of the feature.
func (c *Configure) modifyFeature(ctx context.Context, config *cpb.Configuration) subcommands.ExitStatus {
	// var isEnabled any
	var isEnabled *bool
	if c.Enable || c.Disable {
		isEnabled = &c.Enable
	}

	isCmdValid := false
	switch c.Feature {
	case hostMetrics:
		if isEnabled != nil {
			isCmdValid = true
			config.ProvideSapHostAgentMetrics = &wpb.BoolValue{Value: *isEnabled}
		}
	case processMetrics:
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectProcessMetrics = *isEnabled
		}

		if c.FastMetricsFrequency != 0 {
			if c.FastMetricsFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).ProcessMetricsFrequency = c.FastMetricsFrequency
		}

		if c.SlowMetricsFrequency != 0 {
			if c.SlowMetricsFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("slow-metrics-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).SlowProcessMetricsFrequency = c.SlowMetricsFrequency
		}

		if len(c.SkipMetrics) > 0 {
			log.CtxLogger(ctx).Info("Skip Metrics: ", c.SkipMetrics)
			if !c.Add && !c.Remove {
				c.oteLogger.LogMessageToFileAndConsole(ctx, "Please choose to add or remove given list of process metrics.")
				return subcommands.ExitUsageError
			}

			isCmdValid = true
			if res := c.modifyProcessMetricsToSkip(ctx, config); res != subcommands.ExitSuccess {
				return res
			}
		}
	case hanaMonitoring:
		if isEnabled != nil {
			isCmdValid = true
			if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
				hmc.Enabled = *isEnabled
			} else {
				config.HanaMonitoringConfiguration = &cpb.HANAMonitoringConfiguration{Enabled: *isEnabled}
			}
		}
		if c.SampleIntervalSec != 0 {
			if c.SampleIntervalSec < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("sample-interval-sec must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.SampleIntervalSec = c.SampleIntervalSec
		}
		if c.QueryTimeoutSec != 0 {
			if c.QueryTimeoutSec < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("query-timeout-sec must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.QueryTimeoutSec = c.QueryTimeoutSec
		}
	case sapDiscovery:
		if isEnabled != nil {
			isCmdValid = true
			checkDiscoveryConfig(config).EnableDiscovery = &wpb.BoolValue{Value: *isEnabled}
		}
	case agentMetrics:
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectAgentMetrics = *isEnabled
		}
		if c.AgentMetricsFrequency != 0 {
			if c.AgentMetricsFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentMetricsFrequency = c.AgentMetricsFrequency
		}
		if c.AgentHealthFrequency != 0 {
			if c.AgentHealthFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("agent-health-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentHealthFrequency = c.AgentHealthFrequency
		}
		if c.HeartbeatFrequency != 0 {
			if c.HeartbeatFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("heartbeat-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).HeartbeatFrequency = c.HeartbeatFrequency
		}
	case workloadValidation:
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectWorkloadValidationMetrics = &wpb.BoolValue{Value: *isEnabled}
		}
		if c.ValidationMetricsFrequency != 0 {
			if c.ValidationMetricsFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationMetricsFrequency = c.ValidationMetricsFrequency
		}
		if c.DbFrequency != 0 {
			if c.DbFrequency < 0 {
				c.oteLogger.LogErrorToFileAndConsole(ctx, "Inappropriate flag values:", fmt.Errorf("db-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationDbMetricsFrequency = c.DbFrequency
		}
	case workloadDiscovery:
		if isEnabled != nil {
			isCmdValid = true
			checkDiscoveryConfig(config).EnableWorkloadDiscovery = &wpb.BoolValue{Value: *isEnabled}
		}
	default:
		c.oteLogger.LogMessageToFileAndConsole(ctx, "Unsupported Metric")
		return subcommands.ExitUsageError
	}

	if !isCmdValid {
		c.oteLogger.LogMessageToFileAndConsole(ctx, "Insufficient flags. Please check usage:\n")
		// Checking for nil for testing purposes
		if c.usageFunc != nil {
			c.usageFunc()
		}
		return subcommands.ExitUsageError
	}
	return subcommands.ExitSuccess
}

// modifyProcessMetricsToSkip modifies 'process_metrics_to_skip' field of the configuration file.
func (c *Configure) modifyProcessMetricsToSkip(ctx context.Context, config *cpb.Configuration) subcommands.ExitStatus {
	str := spaces.ReplaceAllString(c.SkipMetrics, "")
	metricsSkipList := strings.Split(str, ",")
	currList := checkCollectionConfig(config).GetProcessMetricsToSkip()

	if c.Add {
		metricsPresent := map[string]bool{}
		for _, metric := range currList {
			metricsPresent[metric] = true
		}
		for _, metric := range metricsSkipList {
			metricsPresent[metric] = true
		}

		newList := []string{}
		for metric := range metricsPresent {
			newList = append(newList, metric)
		}
		checkCollectionConfig(config).ProcessMetricsToSkip = newList
	} else if c.Remove {
		metricsPresent := map[string]bool{}
		for _, metric := range currList {
			metricsPresent[metric] = true
		}
		for _, metric := range metricsSkipList {
			metricsPresent[metric] = false
		}

		newList := []string{}
		for metric, keep := range metricsPresent {
			if keep {
				newList = append(newList, metric)
			}
		}
		checkCollectionConfig(config).ProcessMetricsToSkip = newList
	} else {
		c.oteLogger.LogErrorToFileAndConsole(ctx, "Error: ", fmt.Errorf("no -add or -remove flag with -process-metrics-to-skip"))
		return subcommands.ExitUsageError
	}
	return subcommands.ExitSuccess
}

// writeFile writes the configuration to the given path.
func (c *Configure) writeFile(ctx context.Context, config *cpb.Configuration, path string) (string, error) {
	file, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(config)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Unable to marshal configuration.json")
		return "Unable to marshal configuration.json", err
	}

	var fileBuf bytes.Buffer
	json.Indent(&fileBuf, file, "", "  ")
	log.CtxLogger(ctx).Info("Config file data we're about to write: ", fileBuf.String())

	err = os.WriteFile(path, fileBuf.Bytes(), 0644)
	if err != nil {
		c.oteLogger.LogErrorToFileAndConsole(ctx, "Unable to write configuration.json", err)
		return "Unable to write configuration.json", err
	}
	return fileBuf.String(), nil
}

// checkCollectionConfig returns the collection configuration from the configuration file.
func checkCollectionConfig(config *cpb.Configuration) *cpb.CollectionConfiguration {
	if cc := config.GetCollectionConfiguration(); cc != nil {
		return cc
	}
	return &cpb.CollectionConfiguration{}
}

// checkDiscoveryConfig returns the discovery configuration from the configuration file.
func checkDiscoveryConfig(config *cpb.Configuration) *cpb.DiscoveryConfiguration {
	if dc := config.GetDiscoveryConfiguration(); dc != nil {
		return dc
	}
	return &cpb.DiscoveryConfiguration{}
}

func showStatus(ctx context.Context, feature string, enabled bool, isWindows bool) (string, error) {
	var color, status string
	if enabled {
		color = "\\033[0;32m"
		status = "ENABLED"
	} else {
		color = "\\033[0;31m"
		status = "DISABLED"
	}
	NC := "\\033[0m" // No color
	executable := "echo"
	args := fmt.Sprintf("-e %s %s[%s]%s", feature, color, status, NC)

	if isWindows {
		executable = "cmd"
		args, _ = shsprintf.Sprintf("/C echo %s [%s]", feature, status)
	}
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  executable,
		ArgsToSplit: args,
	})

	if result.ExitCode != 0 {
		log.CtxLogger(ctx).Errorw("failed displaying feature status", "feature:", feature, "errorCode:", result.ExitCode, "error:", result.StdErr)
		return "", result.Error
	}
	return result.StdOut, nil
}
