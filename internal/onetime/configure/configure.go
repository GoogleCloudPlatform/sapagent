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
	"strconv"
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
	feature, logLevel, logConfig                                    string
	setting, path                                                   string
	skipMetrics                                                     string
	validationMetricsFrequency, dbFrequency                         string
	fastMetricsFrequency, slowMetricsFrequency                      string
	agentMetricsFrequency, agentHealthFrequency, heartbeatFrequency string
	sampleIntervalSec, queryTimeoutSec                              string
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
)

var (
	loglevels = map[string]cpb.Configuration_LogLevel{
		"debug": cpb.Configuration_DEBUG,
		"info":  cpb.Configuration_INFO,
		"warn":  cpb.Configuration_WARNING,
		"error": cpb.Configuration_ERROR,
	}
	spaces           = regexp.MustCompile(`\s+`)
	keyMatchRegex    = regexp.MustCompile(`"(\w+)":`)
	wordBarrierRegex = regexp.MustCompile(`(\w)([A-Z])`)
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
	return `configure --feature=<host_metrics|process_metrics|hana_monitoring|sap_discovery|agent_metrics|workload_validation>
	--enable | --disable
	[--loglevel=<debug|info|warn|error>]
	[--showall]
	`
}

// SetFlags implements the subcommand interface for features.
func (c *Configure) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.feature, "feature", "", "The requested feature")
	fs.StringVar(&c.feature, "f", "", "The requested feature")
	fs.StringVar(&c.logLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&c.logConfig, "logconfig", "", "Sets the logging level for the agent configuration file")
	fs.StringVar(&c.setting, "setting", "", "The requested setting")
	fs.StringVar(&c.skipMetrics, "process-metrics-to-skip", "", "Add or remove the list of metrics to skip during process metrics collection")
	fs.StringVar(&c.validationMetricsFrequency, "workload-validation-metrics-frequency", "", "Sets the frequency of workload validation metrics collection")
	fs.StringVar(&c.dbFrequency, "db-frequency", "", "Sets the database frequency of workload validation metrics collection")
	fs.StringVar(&c.fastMetricsFrequency, "process-metrics-frequency", "", "Sets the frequency of fast moving process metrics collection")
	fs.StringVar(&c.slowMetricsFrequency, "slow-process-metrics-frequency", "", "Sets the frequency of slow moving process metrics collection")
	fs.StringVar(&c.agentMetricsFrequency, "agent-metrics-frequency", "", "Sets the agent metrics frequency")
	fs.StringVar(&c.agentHealthFrequency, "agent-health-frequency", "", "Sets the agent health frequency")
	fs.StringVar(&c.heartbeatFrequency, "heartbeat-frequency", "", "Sets the heartbeat frequency")
	fs.StringVar(&c.sampleIntervalSec, "sample-interval-sec", "", "Sets the sample interval sec for HANA Monitoring")
	fs.StringVar(&c.queryTimeoutSec, "query-timeout-sec", "", "Sets the query timeout for HANA Monitoring")
	fs.StringVar(&c.path, "path", "", "Sets the path for configuration.json")
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
	onetime.SetupOneTimeLogging(lp, c.Name(), log.StringLevelToZapcore(c.logLevel))

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

	res := c.modifyConfig(ctx, os.ReadFile)
	if res == subcommands.ExitSuccess {
		fmt.Println("Successfully modified configuration.json and restarted the agent.")
	}
	return c.modifyConfig(ctx, os.ReadFile)
}

func setStatus(ctx context.Context, config *cpb.Configuration) map[string]bool {
	featureStatus := make(map[string]bool)
	if hm := config.GetProvideSapHostAgentMetrics(); hm != nil {
		featureStatus[hostMetrics] = hm.GetValue()
	} else {
		featureStatus[hostMetrics] = false
	}
	if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
		featureStatus[hanaMonitoring] = hmc.GetEnabled()
	} else {
		featureStatus[hanaMonitoring] = false
	}

	if cc := config.GetCollectionConfiguration(); cc == nil {
		featureStatus[agentMetrics] = false
		featureStatus[workloadValidation] = false
		featureStatus[processMetrics] = false
	} else {
		featureStatus[agentMetrics] = cc.GetCollectAgentMetrics()
		featureStatus[workloadValidation] = cc.GetCollectWorkloadValidationMetrics()
		featureStatus[processMetrics] = cc.GetCollectProcessMetrics()
	}

	if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
		featureStatus[sapDiscovery] = true
	} else {
		featureStatus[sapDiscovery] = false
	}

	log.CtxLogger(ctx).Debug("Feature status: ", featureStatus)
	return featureStatus
}

func (c *Configure) showFeatures(ctx context.Context) subcommands.ExitStatus {
	config := configuration.Read(c.path, os.ReadFile)
	if config == nil {
		log.CtxLogger(ctx).Error("Unable to read configuration.json")
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
func (c *Configure) modifyConfig(ctx context.Context, read configuration.ReadConfigFile) subcommands.ExitStatus {
	log.Logger.Debugw("Beginning execution of features command")
	config := configuration.Read(c.path, read)
	if config == nil {
		log.CtxLogger(ctx).Error("Unable to read configuration.json")
		return subcommands.ExitFailure
	}

	isCmdValid := false
	if len(c.logConfig) > 0 {
		if _, ok := loglevels[c.logConfig]; !ok {
			onetime.LogMessageToFileAndConsole("Invalid log level. Please use [debug, info, warn, error]")
			return subcommands.ExitUsageError
		}
		isCmdValid = true
		config.LogLevel = loglevels[c.logConfig]
	}

	if len(c.feature) > 0 {
		if res := c.modifyFeature(ctx, config); res != subcommands.ExitSuccess {
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
func (c *Configure) modifyFeature(ctx context.Context, config *cpb.Configuration) subcommands.ExitStatus {
	// var isEnabled any
	var isEnabled *bool
	if c.enable || c.disable {
		isEnabled = &c.enable
	}

	isCmdValid := false
	switch c.feature {
	case "host_metrics":
		if isEnabled != nil {
			isCmdValid = true
			config.ProvideSapHostAgentMetrics = &wpb.BoolValue{Value: *isEnabled}
		}
	case "process_metrics":
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectProcessMetrics = *isEnabled
		}

		if c.fastMetricsFrequency != "" {
			i, err := c.updateField(c.fastMetricsFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).ProcessMetricsFrequency = i
		}

		if c.slowMetricsFrequency != "" {
			i, err := c.updateField(c.slowMetricsFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).SlowProcessMetricsFrequency = i
		}

		if len(c.skipMetrics) > 0 {
			log.CtxLogger(ctx).Debug("Skip Metrics: ", c.skipMetrics)
			if !c.add && !c.remove {
				onetime.LogMessageToFileAndConsole("Please choose to add or remove given list of process metrics.")
				return subcommands.ExitUsageError
			}

			isCmdValid = true
			if res := c.modifyProcessMetricsToSkip(config); res != subcommands.ExitSuccess {
				return res
			}
		}
	case "hana_monitoring":
		if isEnabled != nil {
			isCmdValid = true
			if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
				hmc.Enabled = *isEnabled
			} else {
				config.HanaMonitoringConfiguration = &cpb.HANAMonitoringConfiguration{Enabled: *isEnabled}
			}
		}
		if c.sampleIntervalSec != "" {
			i, err := c.updateField(c.sampleIntervalSec)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.SampleIntervalSec = i
		}
		if c.queryTimeoutSec != "" {
			i, err := c.updateField(c.queryTimeoutSec)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			config.HanaMonitoringConfiguration.QueryTimeoutSec = i
		}
	case "sap_discovery":
		if isEnabled != nil {
			isCmdValid = true
			checkDiscoveryConfig(config).EnableDiscovery = &wpb.BoolValue{Value: *isEnabled}
		}
	case "agent_metrics":
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectAgentMetrics = *isEnabled
		}
		if c.agentMetricsFrequency != "" {
			i, err := c.updateField(c.agentMetricsFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentMetricsFrequency = i
		}
		if c.agentHealthFrequency != "" {
			i, err := c.updateField(c.agentHealthFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).AgentHealthFrequency = i
		}
		if c.heartbeatFrequency != "" {
			i, err := c.updateField(c.heartbeatFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).HeartbeatFrequency = i
		}
	case "workload_validation":
		if isEnabled != nil {
			isCmdValid = true
			checkCollectionConfig(config).CollectWorkloadValidationMetrics = *isEnabled
		}
		if c.validationMetricsFrequency != "" {
			i, err := c.updateField(c.validationMetricsFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationMetricsFrequency = i
		}
		if c.dbFrequency != "" {
			i, err := c.updateField(c.dbFrequency)
			if err != nil {
				onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
				return c.returnError(err)
			}
			isCmdValid = true
			checkCollectionConfig(config).WorkloadValidationDbMetricsFrequency = i
		}
	default:
		onetime.LogMessageToFileAndConsole("Unsupported Metric")
		return subcommands.ExitUsageError
	}

	if !isCmdValid {
		onetime.LogMessageToFileAndConsole("Insufficient flags. Please check usage:\n")
		log.Print(c.Usage())
		return subcommands.ExitUsageError
	}
	return subcommands.ExitSuccess
}

func (c *Configure) returnError(err error) subcommands.ExitStatus {
	onetime.LogErrorToFileAndConsole("Inappropriate flag values: ", err)
	log.Print("Please check usage:\n")
	log.Print(c.Usage())
	return subcommands.ExitUsageError
}

func (c *Configure) updateField(val string) (int64, error) {
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, err
	}
	if i < 0 {
		return 0, fmt.Errorf("inappropriate value. flag value cannot be negative or floating")
	}

	return i, nil
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

func snakeMarshal(config *cpb.Configuration) ([]byte, error) {
	camelCaseFile, err := protojson.Marshal(config)
	if err != nil {
		return nil, err
	}
	snakeCaseFile := keyMatchRegex.ReplaceAllFunc(
		camelCaseFile,
		func(match []byte) []byte {
			return bytes.ToLower(wordBarrierRegex.ReplaceAll(
				match,
				[]byte(`${1}_${2}`),
			))
		},
	)

	return snakeCaseFile, nil
}

func writeFile(ctx context.Context, config *cpb.Configuration, path string) error {
	file, err := snakeMarshal(config)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Unable to marshal configuration.json")
		return err
	}

	var fileBuf bytes.Buffer
	json.Indent(&fileBuf, file, "", "  ")
	log.CtxLogger(ctx).Debug("Config file before writing: ", fileBuf.String())

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
		log.CtxLogger(ctx).Debug("Restarted the agent")
		return subcommands.ExitSuccess
	}
	log.Print("Could not restart the agent")
	log.CtxLogger(ctx).Error("Agent is not running")
	return subcommands.ExitFailure
}
