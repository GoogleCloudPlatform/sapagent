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

// Package features provides the leverage of enabling or disabling features by modifying configuration.json.
package features

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

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

// Feature has args for backint subcommands.
type Feature struct {
	feature, logLevel, path  string
	help, version            bool
	enable, disable, showall bool
	restartAgent             RestartAgent
}

// RestartAgent abstracts restart functions(windows & linux) for testability.
type RestartAgent func(context.Context) subcommands.ExitStatus

// TODO: Add more variables and properties to extend the capabilities of this OTE.

const (
	hostMetrics        = "host_metrics"
	processMetrics     = "process_metrics"
	hanaMonitoring     = "hana_monitoring"
	sapDiscovery       = "sap_discovery"
	agentMetrics       = "agent_metrics"
	workloadValidation = "workload_validation"
)

// Name implements the subcommand interface for features.
func (*Feature) Name() string {
	return "configure"
}

// Synopsis implements the subcommand interface for features.
func (*Feature) Synopsis() string {
	return `enable/disable collection of following: host metrics, process metrics, hana monitoring, sap discovery, agent metrics, workload validation metrics`
}

// Usage implements the subcommand interface for features.
func (*Feature) Usage() string {
	return `configure --feature=<host_metrics|process_metrics|hana_monitoring|sap_discovery|agent_metrics|workload_validation>
	--enable | --disable
	[--loglevel=<debug|info|warn|error>]
	[--showall]
	`
}

// SetFlags implements the subcommand interface for features.
func (f *Feature) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&f.feature, "feature", "", "The requested feature")
	fs.StringVar(&f.feature, "f", "", "The requested feature")
	fs.StringVar(&f.logLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.BoolVar(&f.help, "help", false, "Display help")
	fs.BoolVar(&f.help, "h", false, "Display help")
	fs.BoolVar(&f.version, "version", false, "Print the agent version")
	fs.BoolVar(&f.version, "v", false, "Print the agent version")
	fs.BoolVar(&f.showall, "showall", false, "Display the status of all features")
	fs.BoolVar(&f.enable, "enable", false, "Enable the requested feature")
	fs.BoolVar(&f.disable, "disable", false, "Disable the requested feature")
}

// Execute implements the subcommand interface for feature.
func (f *Feature) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// This check will never be hit when executing the command line
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

	if f.help {
		fs.Usage()
		return subcommands.ExitSuccess
	}
	if f.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	onetime.SetupOneTimeLogging(lp, f.Name(), log.StringLevelToZapcore(f.logLevel))

	if f.path == "" {
		f.path = configuration.LinuxConfigPath
		if runtime.GOOS == "windows" {
			f.path = configuration.WindowsConfigPath
		}
	}

	if f.showall {
		return f.showFeatures(ctx)
	}

	if f.restartAgent == nil {
		f.restartAgent = restartAgent
	}
	return f.modifyConfig(ctx, os.ReadFile)
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
		featureStatus[sapDiscovery] = false
	} else {
		featureStatus[agentMetrics] = cc.GetCollectAgentMetrics()
		featureStatus[workloadValidation] = cc.GetCollectWorkloadValidationMetrics()
		featureStatus[processMetrics] = cc.GetCollectProcessMetrics()

		if ssd := cc.GetSapSystemDiscovery(); ssd != nil {
			featureStatus[sapDiscovery] = ssd.GetValue()
		} else {
			featureStatus[sapDiscovery] = false
		}
	}

	log.CtxLogger(ctx).Debug("Feature status: ", featureStatus)
	return featureStatus
}

func (f *Feature) showFeatures(ctx context.Context) subcommands.ExitStatus {
	config := configuration.Read(f.path, os.ReadFile)
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
	log.Print(output)
	return subcommands.ExitSuccess
}

// modifyConfig takes user input and enables/disables features in configuration.json and restarts the agent.
func (f *Feature) modifyConfig(ctx context.Context, read configuration.ReadConfigFile) subcommands.ExitStatus {
	log.Logger.Debugw("Beginning execution of features command")
	config := configuration.Read(f.path, read)
	if config == nil {
		log.CtxLogger(ctx).Error("Unable to read configuration.json")
		return subcommands.ExitFailure
	}

	if !f.disable && !f.enable {
		log.Print("Please choose to enable or disable the given feature\n")
		return subcommands.ExitUsageError
	}

	isEnabled := false
	if f.enable {
		isEnabled = true
	}

	switch f.feature {
	case "host_metrics":
		config.ProvideSapHostAgentMetrics = &wpb.BoolValue{Value: isEnabled}
	case "process_metrics":
		checkCollectionConfig(config).CollectProcessMetrics = isEnabled
	case "hana_monitoring":
		if hmc := config.GetHanaMonitoringConfiguration(); hmc != nil {
			hmc.Enabled = isEnabled
		} else {
			config.HanaMonitoringConfiguration = &cpb.HANAMonitoringConfiguration{Enabled: isEnabled}
		}
	case "sap_discovery":
		checkCollectionConfig(config).SapSystemDiscovery = &wpb.BoolValue{Value: isEnabled}
	case "agent_metrics":
		checkCollectionConfig(config).CollectAgentMetrics = isEnabled
	case "workload_validation":
		checkCollectionConfig(config).CollectWorkloadValidationMetrics = isEnabled
	default:
		log.CtxLogger(ctx).Error("Unsupported Metric")
		return subcommands.ExitUsageError
	}

	if err := writeFile(ctx, config, f.path); err != nil {
		log.CtxLogger(ctx).Errorw("Unable to write configuration.json", "error:", err)
		return subcommands.ExitUsageError
	}
	return f.restartAgent(ctx)
}

func writeFile(ctx context.Context, config *cpb.Configuration, path string) error {
	file, err := protojson.Marshal(config)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Unable to marshal configuration.json")
		return err
	}

	var fileBuf bytes.Buffer
	json.Indent(&fileBuf, file, "", " ")
	// log.CtxLogger(ctx).Debug("File: ", string(fileBuf.Bytes()))

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
		log.CtxLogger(ctx).Errorw("failed restarting sap agent", "errorCode:", result.ExitCode, "error:", result.StdErr)
		return subcommands.ExitFailure
	}

	log.CtxLogger(ctx).Debug("Restarted the agent")
	// TODO: Verify if the agent is up and running.
	return subcommands.ExitSuccess
}
