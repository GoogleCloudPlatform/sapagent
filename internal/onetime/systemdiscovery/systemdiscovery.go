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

// Package systemdiscovery implements the system discovery
// as an OTE to discover SAP systems running on the host.
package systemdiscovery

import (
	"context"
	"fmt"
	"os"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// SystemDiscovery will have the arguments needed for the systemdiscovery commands.
type SystemDiscovery struct {
	WlmService              system.WlmInterface
	CloudLogInterface       system.CloudLogInterface
	CloudDiscoveryInterface system.CloudDiscoveryInterface
	HostDiscoveryInterface  system.HostDiscoveryInterface
	SapDiscoveryInterface   system.SapDiscoveryInterface
	AppsDiscovery           func(context.Context) *sappb.SAPInstances
	configPath, logLevel    string
	help, version           bool
	IIOTEParams             *onetime.InternallyInvokedOTE
}

// Name implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Name() string {
	return "systemdiscovery"
}

// Synopsis implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Synopsis() string {
	return "discover SAP systems that are running on the host."
}

// Usage implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Usage() string {
	return `Usage: systemdiscovery [-config=<path to config file>]
	[-loglevel=<debug|error|info|warn>] [-help] [-version]` + "\n"
}

// SetFlags implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&sd.help, "h", false, "Displays help")
	fs.BoolVar(&sd.help, "help", false, "Displays help")
	fs.BoolVar(&sd.version, "v", false, "Displays the current version of the agent")
	fs.BoolVar(&sd.version, "version", false, "Displays the current version of the agent")
	fs.StringVar(&sd.logLevel, "loglevel", "info", "Sets the log level for the agent logging")
	fs.StringVar(&sd.configPath, "c", "", "Sets the configuration file path for systemdiscovery (default: agent's config file will be used)")
	fs.StringVar(&sd.configPath, "config", "", "Sets the configuration file path for systemdiscovery (default: agent's config file will be used)")
}

// Execute implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("The systemdiscovery command for OTE mode was invoked.")

	if sd.help {
		return onetime.HelpCommand(f)
	}

	if sd.version {
		return onetime.PrintAgentVersion()
	}

	_, err := sd.SystemDiscoveryHandler(ctx, f, args...)
	if err != nil {
		log.CtxLogger(ctx).Errorf("Failed to initialize the SystemDiscovery OTE: %v", err)
		return subcommands.ExitFailure
	}

	return subcommands.ExitSuccess
}

// SystemDiscoveryHandler implements the
// execution logic of the systemdiscovery command.
//
// It is exported and made available to be used internally.
func (sd *SystemDiscovery) SystemDiscoveryHandler(ctx context.Context, fs *flag.FlagSet, args ...any) (*system.Discovery, error) {
	// Initialize the OTE.
	_, cp, _, isOnetimeInitSuccessful := onetime.Init(ctx, onetime.InitOptions{
		Name:     sd.Name(),
		Help:     sd.help,
		Version:  sd.version,
		LogLevel: sd.logLevel,
		IIOTE:    sd.IIOTEParams,
		Fs:       fs,
	}, args...)

	if !isOnetimeInitSuccessful {
		return nil, fmt.Errorf("OTE initialization failed")
	}

	config, err := sd.prepareConfig(ctx, cp, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare the configuration: %v", err)
	}

	// logs with CtxLogger will now be
	// logged to <IIOTEParams.InvokedBy>.log
	// if initialization is successful and is through IIOTE mode.
	//
	// else it will be logged to systemdiscovery.log.
	log.CtxLogger(ctx).Info("SystemDiscovery OTE initialized successfully.")
	log.CtxLogger(ctx).Debugf("config: %v", config)

	// TODO: b/346953366 - Implement the SystemDiscoveryHandler.

	return nil, nil
}

// prepareConfig sets up configuration.
// for the SystemDiscovery OTE.
func (sd *SystemDiscovery) prepareConfig(ctx context.Context, cp *iipb.CloudProperties, args ...any) (*cpb.Configuration, error) {
	var discoveryConfig *cpb.DiscoveryConfiguration

	// config file path is not passed.
	if sd.configPath == "" {
		// "" is passed so that
		// ReadFromFile will read the agent config file.
		config := configuration.ReadFromFile("", os.ReadFile)

		// if agent config file also has no discovery config,
		// ApplyDefaultDiscoveryConfiguration will
		// apply config with default values.
		discoveryConfig = configuration.ApplyDefaultDiscoveryConfiguration(config.GetDiscoveryConfiguration())
	} else {
		// config file path is passed, read the config file.
		config := configuration.ReadFromFile(sd.configPath, os.ReadFile)

		// config file not found. return error.
		if config == nil {
			return nil, fmt.Errorf("config file not found in: %s", sd.configPath)
		}

		// config file found but has invalid params. return error.
		if !validateDiscoveryConfigParams(config.GetDiscoveryConfiguration()) {
			return nil, fmt.Errorf("invalid params found in config file")
		}

		// config file found and has valid params. use the config file.
		discoveryConfig = configuration.ApplyDefaultDiscoveryConfiguration(config.GetDiscoveryConfiguration())
	}

	config := &cpb.Configuration{
		CloudProperties: cp,
		AgentProperties: &cpb.AgentProperties{
			Name:    configuration.AgentName,
			Version: configuration.AgentVersion,
		},
		DiscoveryConfiguration: discoveryConfig,
	}

	return config, nil
}

// validateDiscoveryConfigParams validates the discovery config params.
func validateDiscoveryConfigParams(discoveryConfig *cpb.DiscoveryConfiguration) bool {
	if discoveryConfig == nil {
		return false
	}

	if discoveryConfig.GetEnableDiscovery() == nil {
		return false
	}

	if discoveryConfig.GetSapInstancesUpdateFrequency() == nil {
		return false
	}

	if discoveryConfig.GetSystemDiscoveryUpdateFrequency() == nil {
		return false
	}

	if discoveryConfig.GetEnableWorkloadDiscovery() == nil {
		return false
	}

	return true
}
