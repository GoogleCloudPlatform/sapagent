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
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Configure has args for backint subcommands.
type Configure struct {
	feature, logLevel                                               string
	setting, path                                                   string
	skipMetrics                                                     string
	validationMetricsFrequency, dbFrequency                         int64
	fastMetricsFrequency, slowMetricsFrequency                      int64
	agentMetricsFrequency, agentHealthFrequency, heartbeatFrequency int64
	reliabilityMetricsFrequency                                     int64
	sampleIntervalSec, queryTimeoutSec                              int64
	help, version                                                   bool
	enable, disable, showall                                        bool
	add, remove                                                     bool
	restartAgent                                                    RestartAgent
}

// RestartAgent abstracts restart functions(windows & linux) for testability.
type RestartAgent func(context.Context) subcommands.ExitStatus

const (
	hostMetrics        = "host_metrics"
	processMetrics     = "process_metrics"
	hanaMonitoring     = "hana_monitoring"
	sapDiscovery       = "sap_discovery"
	agentMetrics       = "agent_metrics"
	workloadValidation = "workload_validation"
	reliabilityMetrics = "reliability_metrics"
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
configure [-feature=<host_metrics|process_metrics|hana_monitoring|sap_discovery|agent_metrics|workload_validation|reliability_metrics> | -setting=<bare_metal|log_to_cloud>]
[-enable|-disable] [-showall] [-v] [-h]
[process-metrics-frequency=<int>] [slow-process-metrics-frequency=<int>]
[process-metrics-to-skip=<"comma-separated-metrics">] [-add|-remove]
[workload-validation-metrics-frequency=<int>] [db-frequency=<int>]
[-agent-metrics-frequency=<int>] [agent-health-frequency=<int>]
[heartbeat-frequency=<int>] [reliability_metrics_frequency=<int>]
[sample-interval-sec=<int>] [query-timeout-sec=<int>]
`
}

// SetFlags implements the subcommand interface for features.
func (c *Configure) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.feature, "feature", "", "The requested feature. Valid values are: host_metrics, process_metrics, hana_monitoring, sap_discovery, agent_metrics, workload_validation, reliability_metrics")
	fs.StringVar(&c.feature, "f", "", "The requested feature. Valid values are: host_metrics, process_metrics, hana_monitoring, sap_discovery, agent_metrics, workload_validation, reliability_metrics")
	fs.StringVar(&c.logLevel, "loglevel", "", "Sets the logging level for the agent configuration file")
	fs.StringVar(&c.setting, "setting", "", "The requested setting. Valid values are: bare_metal, log_to_cloud")
	fs.StringVar(&c.skipMetrics, "process-metrics-to-skip", "", "Add or remove the list of metrics to skip during process metrics collection")
	fs.Int64Var(&c.validationMetricsFrequency, "workload-validation-metrics-frequency", 0, "Sets the frequency of workload validation metrics collection. Default value is 300(s)")
	fs.Int64Var(&c.dbFrequency, "db-frequency", 0,
		"Sets the database frequency of workload validation metrics collection. Default value is 3600(s)")
	fs.Int64Var(&c.fastMetricsFrequency, "process-metrics-frequency", 0, "Sets the frequency of fast moving process metrics collection. Default value is 5(s)")
	fs.Int64Var(&c.slowMetricsFrequency, "slow-process-metrics-frequency", 0, "Sets the frequency of slow moving process metrics collection. Default value is 30(s)")
	fs.Int64Var(&c.agentMetricsFrequency, "agent-metrics-frequency", 0, "Sets the agent metrics frequency. Default value is 60(s)")
	fs.Int64Var(&c.agentHealthFrequency, "agent-health-frequency", 0,
		"Sets the agent health frequency. Default value is 60(s)")
	fs.Int64Var(&c.heartbeatFrequency, "heartbeat-frequency", 0,
		"Sets the heartbeat frequency. Default value is 60(s)")
	fs.Int64Var(&c.sampleIntervalSec, "sample-interval-sec", 0,
		"Sets the sample interval sec for HANA Monitoring. Default value is 300(s)")
	fs.Int64Var(&c.queryTimeoutSec, "query-timeout-sec", 0,
		"Sets the query timeout for HANA Monitoring. Default value is 300(s)")
	fs.Int64Var(&c.reliabilityMetricsFrequency, "reliability_metrics_frequency", 0, "Sets the reliability metric collection frequency. Default value is 60(s)")
	fs.BoolVar(&c.help, "help", false, "Display help")
	fs.BoolVar(&c.help, "h", false, "Display help")
	fs.BoolVar(&c.version, "version", false, "Print the agent version")
	fs.BoolVar(&c.version, "v", false, "Print the agent version")
	fs.BoolVar(&c.showall, "showall", false, "Display the status of all features")
	fs.BoolVar(&c.enable, "enable", false, "Enable the requested feature/setting")
	fs.BoolVar(&c.disable, "disable", false, "Disable the requested feature/setting")
	fs.BoolVar(&c.add, "add", false, "Add the requested list of process metrics to skip. process-metrics-to-skip should not be empty")
	fs.BoolVar(&c.remove, "remove", false, "Remove the requested list of process metrics to skip. process-metrics-to-skip should not be empty")
}

// Execute implements the subcommand interface for feature.
func (c *Configure) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// this check will never be hit when executing the command line
	if len(args) < 2 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	// This check will never be hit when executing the command line
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}

	if c.help {
		fs.Usage()
		return subcommands.ExitSuccess
	}
	if c.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	onetime.SetupOneTimeLogging(lp, c.Name(), log.StringLevelToZapcore("info"))

	if c.path == "" {
		c.path = configuration.LinuxConfigPath
		if runtime.GOOS == "windows" {
			c.path = configuration.WindowsConfigPath
		}
	}

	if c.showall {
		return c.showFeatures(ctx)
	}

	if c.restartAgent == nil {
		c.restartAgent = restartAgent
	}

	res := c.modifyConfig(ctx, fs, os.ReadFile)
	if res == subcommands.ExitSuccess {
		fmt.Println("Successfully modified configuration.json and restarted the agent.")
	}
	return res
}

func setStatus(ctx context.Context, config *cpb.Configuration) map[string]bool {
	featureStatus := make(map[string]bool)
	if hm := config.GetProvideSapHostAgentMetrics(); hm != nil {
		featureStatus[hostMetrics] = hm.GetValue()
	} else {
		featureStatus[hostMetrics] = true
	}
	if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
		featureStatus[hanaMonitoring] = hmc.GetEnabled()
	} else {
		featureStatus[hanaMonitoring] = false
	}

	if cc := config.GetCollectionConfiguration(); cc == nil {
		featureStatus[agentMetrics] = false
		featureStatus[workloadValidation] = true
		featureStatus[processMetrics] = false
		featureStatus[reliabilityMetrics] = false
	} else {
		featureStatus[agentMetrics] = cc.GetCollectAgentMetrics()
		featureStatus[workloadValidation] = cc.GetCollectWorkloadValidationMetrics().GetValue()
		featureStatus[processMetrics] = cc.GetCollectProcessMetrics()
		featureStatus[reliabilityMetrics] = cc.GetCollectReliabilityMetrics().GetValue()
	}

	if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
		featureStatus[sapDiscovery] = true
	} else {
		featureStatus[sapDiscovery] = false
	}

	log.CtxLogger(ctx).Info("Feature status: ", featureStatus)
	return featureStatus
}

func (c *Configure) showFeatures(ctx context.Context) subcommands.ExitStatus {
	config := configuration.Read(c.path, os.ReadFile)
	if config == nil {
		onetime.LogMessageToFileAndConsole("Unable to read configuration.json")
		return subcommands.ExitFailure
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

	for _, feature := range enabled {
		out, err := showStatus(ctx, feature, featureStatus[feature])
		if err != nil {
			return subcommands.ExitFailure
		}
		output += out
	}
	for _, feature := range disabled {
		out, err := showStatus(ctx, feature, featureStatus[feature])
		if err != nil {
			return subcommands.ExitFailure
		}
		output += out
	}
	fmt.Println(output)
	return subcommands.ExitSuccess
}

// modifyConfig takes user input and enables/disables features in configuration.json and restarts the agent.
func (c *Configure) modifyConfig(ctx context.Context, fs *flag.FlagSet, read configuration.ReadConfigFile) subcommands.ExitStatus {
	log.Logger.Infow("Beginning execution of features command")
	config := configuration.Read(c.path, read)
	if config == nil {
		onetime.LogMessageToFileAndConsole("Unable to read configuration.json")
		return subcommands.ExitFailure
	}

	isCmdValid := false
	if len(c.logLevel) > 0 {
		if _, ok := loglevels[c.logLevel]; !ok {
			onetime.LogMessageToFileAndConsole("Invalid log level. Please use [debug, info, warn, error]")
			return subcommands.ExitUsageError
		}
		isCmdValid = true
		config.LogLevel = loglevels[c.logLevel]
	}

	if len(c.feature) > 0 {
		if res := c.modifyFeature(ctx, fs, config); res != subcommands.ExitSuccess {
			return res
		}
		isCmdValid = true
	} else if len(c.setting) > 0 {
		if !c.enable && !c.disable {
			onetime.LogMessageToFileAndConsole("Please choose to enable or disable the given feature/setting\n")
			return subcommands.ExitUsageError
		}

		isEnabled := c.enable
		switch c.setting {
		case "bare_metal":
			config.BareMetal = isEnabled
		case "log_to_cloud":
			config.LogToCloud = &wpb.BoolValue{Value: isEnabled}
		default:
			onetime.LogMessageToFileAndConsole("Unsupported setting")
			return subcommands.ExitUsageError
		}
		isCmdValid = true
	}

	if !isCmdValid {
		onetime.LogMessageToFileAndConsole("Insufficient flags. Please check usage:\n")
		// Checking for nil for testing purposes
		if fs != nil {
			fs.Usage()
		}
		return subcommands.ExitUsageError
	}

	if err := writeFile(ctx, config, c.path); err != nil {
		log.Print("Unable to write configuration.json")
		log.CtxLogger(ctx).Errorw("Unable to write configuration.json", "error:", err)
		return subcommands.ExitUsageError
	}
	return c.restartAgent(ctx)
}

// modifyFeature takes user input and modifies fields related to a particular feature, which could simply
// be enabling or disabling the feature or updating frequency values, for instance, of the feature.
func (c *Configure) modifyFeature(ctx context.Context, fs *flag.FlagSet, config *cpb.Configuration) subcommands.ExitStatus {
	// var isEnabled any
	var isEnabled *bool
	if c.enable || c.disable {
		isEnabled = &c.enable
	}

	isCmdValid := false
	switch c.feature {
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

		if c.fastMetricsFrequency != 0 {
			if c.fastMetricsFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).ProcessMetricsFrequency = c.fastMetricsFrequency
		}

		if c.slowMetricsFrequency != 0 {
			if c.slowMetricsFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("slow-metrics-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).SlowProcessMetricsFrequency = c.slowMetricsFrequency
		}

		if len(c.skipMetrics) > 0 {
			log.CtxLogger(ctx).Info("Skip Metrics: ", c.skipMetrics)
			if !c.add && !c.remove {
				onetime.LogMessageToFileAndConsole("Please choose to add or remove given list of process metrics.")
				return subcommands.ExitUsageError
			}

			isCmdValid = true
			if res := c.modifyProcessMetricsToSkip(config); res != subcommands.ExitSuccess {
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
		if c.sampleIntervalSec != 0 {
			if c.sampleIntervalSec < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("sample-interval-sec must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.SampleIntervalSec = c.sampleIntervalSec
		}
		if c.queryTimeoutSec != 0 {
			if c.queryTimeoutSec < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("query-timeout-sec must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.QueryTimeoutSec = c.queryTimeoutSec
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
		if c.agentMetricsFrequency != 0 {
			if c.agentMetricsFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentMetricsFrequency = c.agentMetricsFrequency
		}
		if c.agentHealthFrequency != 0 {
			if c.agentHealthFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("agent-health-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentHealthFrequency = c.agentHealthFrequency
		}
		if c.heartbeatFrequency != 0 {
			if c.heartbeatFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("heartbeat-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).HeartbeatFrequency = c.heartbeatFrequency
		}
	case workloadValidation:
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectWorkloadValidationMetrics = &wpb.BoolValue{Value: *isEnabled}
		}
		if c.validationMetricsFrequency != 0 {
			if c.validationMetricsFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationMetricsFrequency = c.validationMetricsFrequency
		}
		if c.dbFrequency != 0 {
			if c.dbFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("db-frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationDbMetricsFrequency = c.dbFrequency
		}
	case reliabilityMetrics:
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectReliabilityMetrics.Value = *isEnabled
		}
		if c.reliabilityMetricsFrequency != 0 {
			if c.reliabilityMetricsFrequency < 0 {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values:", fmt.Errorf("frequency must be non-negative"))
				return subcommands.ExitUsageError
			}
			isCmdValid = true
			checkCollectionConfig(config).ReliabilityMetricsFrequency = c.reliabilityMetricsFrequency
		}
	default:
		onetime.LogMessageToFileAndConsole("Unsupported Metric")
		return subcommands.ExitUsageError
	}

	if !isCmdValid {
		onetime.LogMessageToFileAndConsole("Insufficient flags. Please check usage:\n")
		// Checking for nil for testing purposes
		if fs != nil {
			fs.Usage()
		}
		return subcommands.ExitUsageError
	}
	return subcommands.ExitSuccess
}

// modifyProcessMetricsToSkip modifies 'process_metrics_to_skip' field of the configuration file.
func (c *Configure) modifyProcessMetricsToSkip(config *cpb.Configuration) subcommands.ExitStatus {
	str := spaces.ReplaceAllString(c.skipMetrics, "")
	metricsSkipList := strings.Split(str, ",")
	currList := checkCollectionConfig(config).GetProcessMetricsToSkip()

	if c.add {
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
	} else if c.remove {
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
		onetime.LogErrorToFileAndConsole("Error: ", fmt.Errorf("no -add or -remove flag with -process-metrics-to-skip"))
		return subcommands.ExitUsageError
	}
	return subcommands.ExitSuccess
}

func writeFile(ctx context.Context, config *cpb.Configuration, path string) error {
	file, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(config)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Unable to marshal configuration.json")
		return err
	}

	var fileBuf bytes.Buffer
	json.Indent(&fileBuf, file, "", "  ")
	log.CtxLogger(ctx).Info("Config file before writing: ", fileBuf.String())

	err = os.WriteFile(path, fileBuf.Bytes(), 0644)
	if err != nil {
		fmt.Println("Unable to write configuration.json", "err: ", err)
		log.CtxLogger(ctx).Errorw("Unable to write configuration.json")
		return err
	}
	return nil
}

func checkCollectionConfig(config *cpb.Configuration) *cpb.CollectionConfiguration {
	if cc := config.GetCollectionConfiguration(); cc != nil {
		return cc
	}
	return &cpb.CollectionConfiguration{}
}

func checkDiscoveryConfig(config *cpb.Configuration) *cpb.DiscoveryConfiguration {
	if dc := config.GetDiscoveryConfiguration(); dc != nil {
		return dc
	}
	return &cpb.DiscoveryConfiguration{}
}

// TODO: Extend this function for Windows systems.
func showStatus(ctx context.Context, feature string, enabled bool) (string, error) {
	var color, status string
	if enabled {
		color = "$`\\e[0;32m`"
		status = "ENABLED"
	} else {
		color = "$`\\e[0;31m`"
		status = "DISABLED"
	}
	NC := "$`\\e[0m`" // No colour

	args := `-c 'COLOR=` + color + `;NC=` + NC + `;echo "` + feature + ` ${COLOR}[` + status + `]${NC}"'`

	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: args,
	})
	if result.ExitCode != 0 {
		log.CtxLogger(ctx).Errorw("failed displaying feature status", "feature:", feature, "errorCode:", result.ExitCode, "error:", result.StdErr)
		return "", result.Error
	}
	return result.StdOut, nil
}

// TODO: Extend this function for Windows systems.
func restartAgent(ctx context.Context) subcommands.ExitStatus {
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: "-c 'sudo systemctl restart google-cloud-sap-agent'",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent")
		log.CtxLogger(ctx).Errorw("failed restarting sap agent", "errorCode:", result.ExitCode, "error:", result.StdErr)
		return subcommands.ExitFailure
	}

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: "-c 'sudo systemctl status google-cloud-sap-agent'",
	})
	if result.ExitCode != 0 {
		log.Print("Could not restart the agent")
		log.CtxLogger(ctx).Errorw("failed checking the status of sap agent", "errorCode:", result.ExitCode, "error:", result.StdErr)
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
