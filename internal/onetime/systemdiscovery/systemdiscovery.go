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
	"io"
	"net"
	"os"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// SystemDiscovery will have the arguments
// needed for the systemdiscovery commands.
type SystemDiscovery struct {
	WlmService                    system.WlmInterface
	CloudLogInterface             system.CloudLogInterface
	CloudDiscoveryInterface       system.CloudDiscoveryInterface
	HostDiscoveryInterface        system.HostDiscoveryInterface
	SapDiscoveryInterface         system.SapDiscoveryInterface
	AppsDiscovery                 func(context.Context) *sappb.SAPInstances
	OsStatReader                  workloadmanager.OSStatReader
	ConfigFileReader              workloadmanager.ConfigFileReader
	ConfigPath, LogLevel, LogPath string
	help                          bool
	IIOTEParams                   *onetime.InternallyInvokedOTE
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
	[-loglevel=<debug|error|info|warn>] [-log-path=<log-path>] [-help] [-version]` + "\n"
}

// SetFlags implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&sd.help, "h", false, "Displays help")
	fs.BoolVar(&sd.help, "help", false, "Displays help")
	fs.StringVar(&sd.LogLevel, "loglevel", "info", "Sets the log level for the agent logging")
	fs.StringVar(&sd.ConfigPath, "c", "", "Sets the configuration file path for systemdiscovery (default: agent's config file will be used)")
	fs.StringVar(&sd.ConfigPath, "config", "", "Sets the configuration file path for systemdiscovery (default: agent's config file will be used)")
	fs.StringVar(&sd.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/systemdiscovery.log")
}

// Execute implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("The systemdiscovery command for OTE mode was invoked.")

	if sd.help {
		return onetime.HelpCommand(f)
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
	lp, cp, _, ok := onetime.Init(ctx, onetime.InitOptions{
		Name:     sd.Name(),
		Help:     sd.help,
		LogLevel: sd.LogLevel,
		LogPath:  sd.LogPath,
		IIOTE:    sd.IIOTEParams,
		Fs:       fs,
	}, args...)

	if !ok {
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
	log.CtxLogger(ctx).Info("SystemDiscovery one time execution initialized successfully.")
	log.CtxLogger(ctx).Infof("config: %v", config)

	// initialize params to default if they are not already set.
	if err := sd.initDefaults(ctx, config, &lp); err != nil {
		return nil, fmt.Errorf("failed to initialize SystemDiscovery params: %v", err)
	}

	// validate the params.
	if err := sd.validateParams(config, &lp); err != nil {
		return nil, fmt.Errorf("failed to validate the params: %v", err)
	}

	// initialize the Discovery object.
	discovery := &system.Discovery{
		AppsDiscovery:           sd.AppsDiscovery,
		CloudDiscoveryInterface: sd.CloudDiscoveryInterface,
		CloudLogInterface:       sd.CloudLogInterface,
		HostDiscoveryInterface:  sd.HostDiscoveryInterface,
		SapDiscoveryInterface:   sd.SapDiscoveryInterface,
		WlmService:              sd.WlmService,
		OSStatReader:            sd.OsStatReader,
		FileReader:              sd.ConfigFileReader,
	}

	log.CtxLogger(ctx).Debugf("Discovery object: %v", discovery)

	if ok = system.StartSAPSystemDiscovery(ctx, config, discovery); !ok {
		return nil, fmt.Errorf("failed to start the SAP system discovery")
	}
	log.CtxLogger(ctx).Infof("SAP Instances discovered: %v", discovery.GetSAPInstances())
	log.CtxLogger(ctx).Infof("SAP Systems discovered: %v", discovery.GetSAPSystems())

	return discovery, nil
}

// initDefaults initializes the SystemDiscovery
// params with default implementation if they aren't already.
func (sd *SystemDiscovery) initDefaults(ctx context.Context, config *cpb.Configuration, lp *log.Parameters) error {
	// Initialize the WLM service only if enable_discovery is set in config.
	if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() && sd.WlmService == nil {
		wlmBasePathURL := config.GetCollectionConfiguration().GetDataWarehouseEndpoint()
		wlmService, err := gce.NewWLMClient(ctx, wlmBasePathURL)
		if err != nil {
			return err
		}
		sd.WlmService = wlmService
	}

	if sd.AppsDiscovery == nil {
		sd.AppsDiscovery = sapdiscovery.SAPApplications
	}

	// Initialize the GCE service for cloud discovery.
	if sd.CloudDiscoveryInterface == nil {
		gceService, err := gce.NewGCEClient(ctx)
		if err != nil {
			return err
		}
		sd.CloudDiscoveryInterface = &clouddiscovery.CloudDiscovery{
			GceService:   gceService,
			HostResolver: net.LookupHost,
		}
	}

	if sd.HostDiscoveryInterface == nil {
		sd.HostDiscoveryInterface = &hostdiscovery.HostDiscovery{
			Exists:  commandlineexecutor.CommandExists,
			Execute: commandlineexecutor.ExecuteCommand,
		}
	}

	if sd.SapDiscoveryInterface == nil {
		sd.SapDiscoveryInterface = &appsdiscovery.SapDiscovery{
			Execute:    commandlineexecutor.ExecuteCommand,
			FileSystem: filesystem.Helper{},
		}
	}

	// set the CloudLogInterface if CloudLoggingClient is set.
	if lp.CloudLoggingClient != nil {
		sd.CloudLogInterface = lp.CloudLoggingClient.Logger(lp.CloudLogName)
	}

	// required to start the discovery process.
	if sd.OsStatReader == nil {
		sd.OsStatReader = workloadmanager.OSStatReader(os.Stat)
	}
	if sd.ConfigFileReader == nil {
		sd.ConfigFileReader = workloadmanager.ConfigFileReader(func(path string) (io.ReadCloser, error) {
			return os.Open(path)
		})
	}

	return nil
}

// validateParams validates params of SystemDiscovery.
func (sd *SystemDiscovery) validateParams(config *cpb.Configuration, lp *log.Parameters) error {
	// if enable_discovery is true, ensure that WlmService
	// is initialized to avoid nil pointer errors.
	if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() && sd.WlmService == nil {
		return fmt.Errorf("enable_discovery is enabled in config but WlmService is not set")
	}

	// optimize by not setting up the WlmService
	// if enable_discovery is not set.
	if !config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() {
		sd.WlmService = nil
	}

	// if CloudLoggingClient is set, ensure that
	// CloudLogInterface is set to avoid nil pointer errors.
	if lp.CloudLoggingClient != nil && sd.CloudLogInterface == nil {
		return fmt.Errorf("CloudLoggingClient is set in logParameters but CloudLogInterface is not set")
	}

	// required to discover compute resources.
	if sd.CloudDiscoveryInterface == nil {
		return fmt.Errorf("CloudDiscoveryInterface is not set")
	}

	// required to discover clusters in current host.
	if sd.HostDiscoveryInterface == nil {
		return fmt.Errorf("HostDiscoveryInterface is not set")
	}

	// required to discover SAP apps running in a given instance.
	if sd.SapDiscoveryInterface == nil {
		return fmt.Errorf("SapDiscoveryInterface is not set")
	}

	// required to discover SAP Application specific details.
	if sd.AppsDiscovery == nil {
		return fmt.Errorf("AppsDiscovery is not set")
	}

	return nil
}

// prepareConfig sets up configuration.
// for the SystemDiscovery OTE.
func (sd *SystemDiscovery) prepareConfig(ctx context.Context, cp *iipb.CloudProperties, args ...any) (*cpb.Configuration, error) {
	var config *cpb.Configuration

	// config file path is not passed.
	if sd.ConfigPath == "" {
		// "" is passed so that
		// ReadFromFile will read the agent config file.
		config = configuration.ReadFromFile("", os.ReadFile)

		// if agent config file also has no discovery config,
		// ApplyDefaults will apply config with default values.
		config = configuration.ApplyDefaults(config, cp)

		// make EnableDiscovery always false by default
		// to ensure WLM is not enabled by default.
		config.DiscoveryConfiguration.EnableDiscovery = &wpb.BoolValue{Value: false}
	} else {
		// config file path is passed, read the config file.
		config = configuration.ReadFromFile(sd.ConfigPath, os.ReadFile)

		// config file not found. return error.
		if config == nil {
			return nil, fmt.Errorf("config file not found in: %s", sd.ConfigPath)
		}

		// config file found but has invalid params. return error.
		if !validateDiscoveryConfigParams(config.GetDiscoveryConfiguration()) {
			return nil, fmt.Errorf("invalid params found in config file")
		}

		// config file found and has valid params. use the config file.
		config = configuration.ApplyDefaults(config, cp)
	}

	// Validate if CloudProperties has all the required fields.
	if !validateCloudProperties(cp) {
		return nil, fmt.Errorf("CloudProperties not found or has invalid fields")
	}

	return config, nil
}

// validateCloudProperties checks if the CloudProperties
// has all the required fields for SystemDiscovery.
func validateCloudProperties(cp *iipb.CloudProperties) bool {
	return cp.GetProjectId() != "" && cp.GetInstanceId() != "" && cp.GetZone() != "" && cp.GetInstanceName() != "" && cp.GetNumericProjectId() != ""
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
