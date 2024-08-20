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
	"net"
	"os"

	"flag"
	logging "cloud.google.com/go/logging"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
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
	CloudLogInterface             system.CloudLogInterface
	CloudDiscoveryInterface       system.CloudDiscoveryInterface
	HostDiscoveryInterface        system.HostDiscoveryInterface
	SapDiscoveryInterface         system.SapDiscoveryInterface
	AppsDiscovery                 func(context.Context) *sappb.SAPInstances
	ConfigPath, LogLevel, LogPath string
	help                          bool
	IIOTEParams                   *onetime.InternallyInvokedOTE
	oteLogger                     *onetime.OTELogger
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
	[-loglevel=<debug|error|info|warn>] [-log-path=<log-path>] [-help]` + "\n"
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
	// Initialize the OTE.
	lp, cp, _, ok := onetime.Init(ctx, onetime.InitOptions{
		Name:     sd.Name(),
		Help:     sd.help,
		LogLevel: sd.LogLevel,
		LogPath:  sd.LogPath,
		IIOTE:    sd.IIOTEParams,
		Fs:       f,
	}, args...)

	if !ok {
		log.CtxLogger(ctx).Errorf("Failed to initialize the SystemDiscovery OTE: %v", fmt.Errorf("OTE initialization failed"))
		return subcommands.ExitFailure
	}

	_, status := sd.Run(ctx, lp.CloudLogName, onetime.CreateRunOptions(cp, false))
	return status
}

// Run performs the functionality specified by the systemdiscovery subcommand.
func (sd *SystemDiscovery) Run(ctx context.Context, logName string, runOpts *onetime.RunOptions) (*system.Discovery, subcommands.ExitStatus) {
	sd.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	if logName == "" {
		logName = "google-cloud-sap-agent"
	}
	cloudLoggingClient := log.CloudLoggingClientWithUserAgent(ctx, runOpts.CloudProperties.GetProjectId(), configuration.UserAgent())
	discovery, err := sd.SystemDiscoveryHandler(ctx, cloudLoggingClient, runOpts.CloudProperties, logName)
	if err != nil {
		sd.oteLogger.LogErrorToFileAndConsole(ctx, "Encountered an error during handling of SystemDiscovery OTE", err)
		return nil, subcommands.ExitFailure
	}

	return discovery, subcommands.ExitSuccess
}

// SystemDiscoveryHandler implements the
// execution logic of the systemdiscovery command.
//
// It is exported and made available to be used internally.
func (sd *SystemDiscovery) SystemDiscoveryHandler(ctx context.Context, cloudLoggingClient *logging.Client, cp *iipb.CloudProperties, logName string) (*system.Discovery, error) {

	config, err := sd.prepareConfig(ctx, cp)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare the configuration: %v", err)
	}

	// Logs with CtxLogger will now be
	// logged to <IIOTEParams.InvokedBy>.log
	// if initialization is successful and is through IIOTE mode.
	//
	// else it will be logged to systemdiscovery.log.
	log.CtxLogger(ctx).Info("SystemDiscovery one time execution initialized successfully.")
	log.CtxLogger(ctx).Infof("config: %v", config)

	// Initialize params to default if they are not already set.
	if err := sd.initDefaults(ctx, cloudLoggingClient, gce.NewGCEClient, logName); err != nil {
		return nil, fmt.Errorf("failed to initialize SystemDiscovery params: %v", err)
	}

	// Initialize the Discovery object.
	discovery := &system.Discovery{
		AppsDiscovery:           sd.AppsDiscovery,
		CloudDiscoveryInterface: sd.CloudDiscoveryInterface,
		CloudLogInterface:       sd.CloudLogInterface,
		HostDiscoveryInterface:  sd.HostDiscoveryInterface,
		SapDiscoveryInterface:   sd.SapDiscoveryInterface,
		OSStatReader:            func(string) (os.FileInfo, error) { return nil, nil },
	}

	log.CtxLogger(ctx).Debugf("Discovery object: %v", discovery)

	if ok := system.StartSAPSystemDiscovery(ctx, config, discovery); !ok {
		return nil, fmt.Errorf("failed to start the SAP system discovery")
	}
	log.CtxLogger(ctx).Infof("SAP Instances discovered: %v", discovery.GetSAPInstances())
	log.CtxLogger(ctx).Infof("SAP Systems discovered: %v", discovery.GetSAPSystems())

	return discovery, nil
}

// initDefaults initializes the SystemDiscovery
// params with default implementation if they aren't already.
func (sd *SystemDiscovery) initDefaults(ctx context.Context, cloudLoggingClient *logging.Client, gceServiceCreator onetime.GCEServiceFunc, logName string) error {
	if sd.AppsDiscovery == nil {
		sd.AppsDiscovery = sapdiscovery.SAPApplications
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

	// Set the CloudLogInterface if CloudLoggingClient is set.
	if cloudLoggingClient != nil {
		sd.CloudLogInterface = cloudLoggingClient.Logger(logName)
	}

	// Initialize the GCE service for cloud discovery.
	if sd.CloudDiscoveryInterface == nil {
		gceService, err := gceServiceCreator(ctx)
		if err != nil {
			return err
		}
		sd.CloudDiscoveryInterface = &clouddiscovery.CloudDiscovery{
			GceService:   gceService,
			HostResolver: net.LookupHost,
		}
	}

	return nil
}

// prepareConfig sets up configuration.
// for the SystemDiscovery OTE.
func (sd *SystemDiscovery) prepareConfig(ctx context.Context, cp *iipb.CloudProperties) (*cpb.Configuration, error) {
	config := configuration.ReadFromFile(sd.ConfigPath, os.ReadFile)

	// If file not found in user provided path,
	// return an error.
	if sd.ConfigPath != "" && config == nil {
		return nil, fmt.Errorf("file not found at path: %s", sd.ConfigPath)
	}

	// If config file found and had valid values,
	// those will be used to set the config,
	// else the default values will be used to set the config.
	config = configuration.ApplyDefaults(config, cp)

	// Make EnableDiscovery always false by default
	// to ensure WLM is not enabled for OTE mode.
	config.DiscoveryConfiguration.EnableDiscovery = &wpb.BoolValue{Value: false}

	// Validate if CloudProperties has all the required fields.
	if !validateCloudProperties(config.GetCloudProperties()) {
		return nil, fmt.Errorf("CloudProperties not found or has invalid fields")
	}

	return config, nil
}

// validateCloudProperties checks if the CloudProperties
// has all the required fields for SystemDiscovery.
func validateCloudProperties(cp *iipb.CloudProperties) bool {
	return cp.GetProjectId() != "" && cp.GetInstanceId() != "" && cp.GetZone() != "" && cp.GetInstanceName() != "" && cp.GetNumericProjectId() != ""
}
