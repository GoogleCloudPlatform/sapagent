/*
Copyright 2022 Google LLC

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

// Package main serves as the Main entry point for the GC SAP Agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/internal/agentmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const usage = `Usage of google_cloud_sap_agent:
  -h, --help                                    prints help information
  -c=PATH, --config=PATH                        path to configuration.json
  -mm=true|false, --maintenancemode=true|false  to configure maintenance mode
	-sid=SAP System Identifier										[required if mm] SAP system id on which to configure maintenance mode
  -mm-show, --maintenancemode-show              displays the current value configured for maintenancemode
  -r, --remote                                  runs the agent in remote mode to collect workload manager metrics
  -p=project_id, --project=project_id           [required if remote] project id of this instance
  -i=instance_id, --instance=instance_id        [required if remote] instance id of this instance
  -n=instance_name, --name=instance_name        [required if remote] instance name of this instance
  -z=zone, --zone=zone                          [required if remote] zone of this instance
`

var (
	configPath, logfile, usagePriorVersion, usageStatus string
	logUsage                                            bool
	remoteMode                                          bool
	usageAction, usageError                             int
	config                                              *cpb.Configuration
	errQuiet                                            = fmt.Errorf("a quiet error which just signals the program to exit")
	osStatReader                                        = workloadmanager.OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
	configFileReader = workloadmanager.ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := os.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	commandRunnerNoSpace = commandlineexecutor.CommandRunnerNoSpace(func(exe string, args ...string) (string, string, error) {
		return commandlineexecutor.ExecuteCommand(exe, args...)
	})
	commandRunner = commandlineexecutor.CommandRunner(func(exe string, args string) (string, string, error) {
		return commandlineexecutor.ExpandAndExecuteCommand(exe, args)
	})
	commandExistsRunner = commandlineexecutor.CommandExistsRunner(func(exe string) bool {
		return commandlineexecutor.CommandExists(exe)
	})
	defaultTokenGetter = workloadmanager.DefaultTokenGetter(func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
		return google.DefaultTokenSource(ctx, scopes...)
	})
	jsonCredentialsGetter = workloadmanager.JSONCredentialsGetter(func(ctx context.Context, json []byte, scopes ...string) (*google.Credentials, error) {
		return google.CredentialsFromJSON(ctx, json, scopes...)
	})
)

func fetchCloudProperties() *iipb.CloudProperties {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 5 * time.Second
	bo := backoff.WithMaxRetries(exp, 2) // 2 retries (3 total attempts)
	return metadataserver.CloudPropertiesWithRetry(bo)
}

func configureUsageMetrics(cp *iipb.CloudProperties, version string) {
	if usagePriorVersion == "" {
		version = configuration.AgentVersion
	}
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            configuration.AgentName,
		Version:         version,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}

func setupFlagsAndParse(fs *flag.FlagSet, args []string, fr maintenance.FileReader, fw maintenance.FileWriter) error {
	var help, mntmode, showMntMode bool
	var project, instanceid, instancename, zone, sid string
	fs.StringVar(&configPath, "config", "", "configuration path")
	fs.StringVar(&configPath, "c", "", "configuration path")
	fs.BoolVar(&help, "help", false, "display help")
	fs.BoolVar(&help, "h", false, "display help")
	fs.BoolVar(&logUsage, "log-usage", false, "invoke usage status logging")
	fs.BoolVar(&logUsage, "lu", false, "invoke usage status logging")
	fs.StringVar(&usagePriorVersion, "log-usage-prior-version", "", "prior installed version")
	fs.StringVar(&usagePriorVersion, "lup", "", "prior installed version")
	fs.StringVar(&usageStatus, "log-usage-status", "", "usage status value")
	fs.StringVar(&usageStatus, "lus", "", "usage status value")
	fs.IntVar(&usageAction, "log-usage-action", 0, "usage action code")
	fs.IntVar(&usageAction, "lua", 0, "usage action code")
	fs.IntVar(&usageError, "log-usage-error", 0, "usage error code")
	fs.IntVar(&usageError, "lue", 0, "usage error code")
	fs.BoolVar(&mntmode, "maintenancemode", false, "configure maintenance mode")
	fs.BoolVar(&mntmode, "mm", false, "configure maintenance mode")
	fs.StringVar(&sid, "sid", "", "SAP System Identifier")
	fs.BoolVar(&showMntMode, "mm-show", false, "show maintenance mode")
	fs.BoolVar(&showMntMode, "maintenancemode-show", false, "show maintenance mode")
	fs.BoolVar(&remoteMode, "r", false, "run in remote mode to collect workload manager metrics")
	fs.BoolVar(&remoteMode, "remote", false, "run in remote mode to collect workload manager metrics")
	fs.StringVar(&project, "p", "", "project id of this instance")
	fs.StringVar(&project, "project", "", "project id of this instance")
	fs.StringVar(&instanceid, "i", "", "instance id of this instance")
	fs.StringVar(&instanceid, "instance", "", "instance id of this instance")
	fs.StringVar(&instancename, "n", "", "instance name of this instance")
	fs.StringVar(&instancename, "name", "", "instance name of this instance")
	fs.StringVar(&zone, "z", "", "zone of this instance")
	fs.StringVar(&zone, "zone", "", "zone of this instance")
	fs.Usage = func() { fmt.Print(usage) }
	fs.Parse(args[1:])

	if isFlagPresent(fs, "mm") || isFlagPresent(fs, "maintenancemode") {
		if sid == "" {
			return errors.New("invalid SID provided.\n" + usage)
		}
		_, err := maintenance.UpdateMaintenanceMode(mntmode, sid, fr, fw)
		if err != nil {
			return err
		}
		log.Print(fmt.Sprintf("Updated maintenace mode for the SID: %s", sid))
		return errQuiet
	}

	if showMntMode {
		res, err := maintenance.ReadMaintenanceMode(fr)
		if err != nil {
			return err
		}
		if len(res) == 0 {
			log.Print("No SID is under maintenance.")
			return errQuiet
		}
		log.Print(fmt.Sprintf("Maintenance mode flag for process metrics is set to true for the following SIDs:\n"))
		for _, v := range res {
			log.Print(v + "\n")
		}
		return errQuiet
	}

	if help {
		log.Print(usage)
		return errQuiet
	}

	if logUsage {
		switch {
		case usageStatus == "":
			log.Print("A usage status value is required to be set")
			return errQuiet
		case usageStatus == string(usagemetrics.StatusUpdated) && usagePriorVersion == "":
			log.Print("Prior agent version is required to be set")
			return errQuiet
		case usageStatus == string(usagemetrics.StatusError) && !isFlagPresent(fs, "log-usage-error") && !isFlagPresent(fs, "lue"):
			log.Print("For status ERROR, an error code is required to be set")
			return errQuiet
		case usageStatus == string(usagemetrics.StatusAction) && !isFlagPresent(fs, "log-usage-action") && !isFlagPresent(fs, "lua"):
			log.Print("For status ACTION, an action code is required to be set")
			return errQuiet
		}
	}

	if remoteMode {
		if project == "" || instanceid == "" || zone == "" {
			log.Print("ERROR When running in remote mode the project, instanceid, and zone are required")
			os.Exit(1)
		}
		config = &cpb.Configuration{}
		config.CloudProperties = &iipb.CloudProperties{}
		config.CloudProperties.ProjectId = project
		config.CloudProperties.InstanceId = instanceid
		config.CloudProperties.InstanceName = instancename
		config.CloudProperties.Zone = zone
	}

	return nil
}

// isFlagPresent checks if a value for the flag `name` is passed from the
// command line. flag.Lookup did not work in case of bool flags as a missing
// bool flag is treated as false.
func isFlagPresent(fs *flag.FlagSet, name string) bool {
	present := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			present = true
		}
	})
	return present
}

// logUsageStatus makes a call to the appropriate usage metrics API.
func logUsageStatus(status string, actionID, errorID int) error {
	switch usagemetrics.Status(status) {
	case usagemetrics.StatusRunning:
		usagemetrics.Running()
	case usagemetrics.StatusStarted:
		usagemetrics.Started()
	case usagemetrics.StatusStopped:
		usagemetrics.Stopped()
	case usagemetrics.StatusConfigured:
		usagemetrics.Configured()
	case usagemetrics.StatusMisconfigured:
		usagemetrics.Misconfigured()
	case usagemetrics.StatusError:
		usagemetrics.Error(errorID)
	case usagemetrics.StatusInstalled:
		usagemetrics.Installed()
	case usagemetrics.StatusUpdated:
		usagemetrics.Updated(configuration.AgentVersion)
	case usagemetrics.StatusUninstalled:
		usagemetrics.Uninstalled()
	case usagemetrics.StatusAction:
		usagemetrics.Action(actionID)
	default:
		return fmt.Errorf("logUsageStatus() called with an unknown status: %s", status)
	}
	return nil
}

func startServices(goos string) {
	if config.GetCloudProperties() == nil {
		log.Logger.Error("Cloud properties are not set, cannot start services.")
		usagemetrics.Error(usagemetrics.CloudPropertiesNotSet)
		return
	}

	shutdownch := make(chan os.Signal, 1)
	signal.Notify(shutdownch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	ctx := context.Background()
	gceService, err := gce.New(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create GCE service", "error", err)
		usagemetrics.Error(usagemetrics.GCEServiceCreateFailure)
		return
	}
	ppr := &instanceinfo.PhysicalPathReader{goos}
	instanceInfoReader := instanceinfo.New(ppr, gceService)
	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return
	}

	// If this instance is doing remote collection then that is all that is done
	if config.GetCollectionConfiguration() != nil && config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection() != nil {
		// When set to collect workload manager metrics remotely then that is all this runtime will do.
		log.Logger.Info("Collecting Workload Manager metrics remotely, will not start any other services")
		wlmparameters := workloadmanager.Parameters{
			Config:               config,
			Remote:               true,
			ConfigFileReader:     configFileReader,
			CommandRunner:        commandRunner,
			CommandRunnerNoSpace: commandRunnerNoSpace,
			CommandExistsRunner:  commandExistsRunner,
			InstanceInfoReader:   *instanceInfoReader,
			OSStatReader:         osStatReader,
			TimeSeriesCreator:    mc,
			BackOffs:             cloudmonitoring.NewDefaultBackOffIntervals(),
		}
		workloadmanager.StartMetricsCollection(ctx, wlmparameters)
	} else {
		/* The functions being called here should be asynchronous.
		A typical StartXXX() will do the necessary initialisation synchronously and start its own goroutines
		for the long running tasks. The control should be returned to main immediately after init succeeds.
		*/

		// Start the SAP Host Metrics provider
		mqc, err := monitoring.NewQueryClient(ctx)
		if err != nil {
			log.Logger.Errorw("Failed to create Cloud Monitoring query client", "error", err)
			usagemetrics.Error(usagemetrics.QueryClientCreateFailure)
			return
		}
		cmr := &cloudmetricreader.CloudMetricReader{
			QueryClient: &cloudmetricreader.QueryClient{Client: mqc},
			BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
		}
		at := agenttime.New(agenttime.Clock{})
		hmparams := hostmetrics.Parameters{
			Config:             config,
			InstanceInfoReader: *instanceInfoReader,
			CloudMetricReader:  *cmr,
			AgentTime:          *at,
		}
		hostmetrics.StartSAPHostAgentProvider(ctx, hmparams)

		// Start the Workload Manager metrics collection
		wlmparams := workloadmanager.Parameters{
			Config:                config,
			Remote:                false,
			ConfigFileReader:      configFileReader,
			CommandRunner:         commandRunner,
			CommandRunnerNoSpace:  commandRunnerNoSpace,
			CommandExistsRunner:   commandExistsRunner,
			InstanceInfoReader:    *instanceInfoReader,
			OSStatReader:          osStatReader,
			TimeSeriesCreator:     mc,
			DefaultTokenGetter:    defaultTokenGetter,
			JSONCredentialsGetter: jsonCredentialsGetter,
			OSType:                goos,
			BackOffs:              cloudmonitoring.NewDefaultBackOffIntervals(),
		}
		workloadmanager.StartMetricsCollection(ctx, wlmparams)

		// Start the Process metrics collection
		pmparams := processmetrics.Parameters{
			Config:       config,
			OSType:       goos,
			MetricClient: processmetrics.NewMetricClient,
			BackOffs:     cloudmonitoring.NewDefaultBackOffIntervals(),
			GCEService:   gceService,
		}
		processmetrics.Start(ctx, pmparams)

		system.StartSAPSystemDiscovery(ctx, config, gceService)

		agentMetricsParams := agentmetrics.Parameters{
			Config:   config,
			BackOffs: cloudmonitoring.NewDefaultBackOffIntervals(),
		}
		agentmetricsService, err := agentmetrics.NewService(ctx, agentMetricsParams)
		if err != nil {
			log.Logger.Errorw("Failed to create agent metrics service", "error", err)
		} else {
			agentmetricsService.Start(ctx)
		}

		// Start HANA Monitoring
		hanamonitoring.Start(ctx, hanamonitoring.Parameters{
			Config:     config,
			GCEService: gceService,
		})
	}

	go logRunningDaily()

	// wait for the shutdown signal
	<-shutdownch
	// once we have a shutdown event we will wait for up to 3 seconds before for final terminations
	_, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer handleShutdown(cancel)
}

// logRunningDaily log that the agent is running once a day.
func logRunningDaily() {
	for {
		usagemetrics.Running()
		// sleep for 24 hours and a minute, we only log running once a day
		time.Sleep(24*time.Hour + 1*time.Minute)
	}
}

func handleShutdown(cancel context.CancelFunc) {
	log.Logger.Info("Shutting down...")
	usagemetrics.Stopped()
	cancel()
}

func collecRemoteModetMetrics(goos string) {
	ctx := context.Background()
	gceService, err := gce.New(ctx)
	if err != nil {
		log.Print(fmt.Sprintf("ERROR: Failed to create GCE service: %v", err))
		os.Exit(0)
	}
	ppr := &instanceinfo.PhysicalPathReader{goos}
	instanceInfoReader := instanceinfo.New(ppr, gceService)

	wlmparams := workloadmanager.Parameters{
		Config:                config,
		Remote:                true,
		ConfigFileReader:      configFileReader,
		CommandRunner:         commandRunner,
		CommandRunnerNoSpace:  commandRunnerNoSpace,
		CommandExistsRunner:   commandExistsRunner,
		InstanceInfoReader:    *instanceInfoReader,
		OSStatReader:          osStatReader,
		DefaultTokenGetter:    defaultTokenGetter,
		JSONCredentialsGetter: jsonCredentialsGetter,
		OSType:                goos,
	}
	fmt.Println(workloadmanager.CollectMetricsToJSON(ctx, wlmparams))
}

func main() {
	fs := flag.NewFlagSet("cli-flags", flag.ExitOnError)
	err := setupFlagsAndParse(fs, os.Args, maintenance.ModeReader{}, maintenance.ModeWriter{})
	switch {
	case errors.Is(err, errQuiet):
		os.Exit(0)
	case err != nil:
		log.Print(err.Error())
		os.Exit(1)
	}

	// remote operation
	if remoteMode {
		log.SetupLoggingToDiscard()
		collecRemoteModetMetrics(runtime.GOOS)
		os.Exit(0)
	}

	// local operation
	log.SetupLoggingToFile(runtime.GOOS, cpb.Configuration_INFO)
	config = configuration.ReadFromFile(configPath, os.ReadFile)
	if config.GetBareMetal() && config.GetCloudProperties() == nil {
		log.Logger.Error("Bare metal instance detected without cloud properties set. Manually set cloud properties in the configuration file to continue.")
		usagemetrics.Error(usagemetrics.BareMetalCloudPropertiesNotSet)
		os.Exit(0)
	}
	log.SetupLoggingToFile(runtime.GOOS, config.GetLogLevel())
	cloudProps := fetchCloudProperties()
	config = configuration.ApplyDefaults(config, cloudProps)
	configureUsageMetrics(cloudProps, usagePriorVersion)
	if logUsage {
		err = logUsageStatus(usageStatus, usageAction, usageError)
		if err != nil {
			log.Logger.Warnw("Could not log usage", "error", err)
		}
		// exit the program, this was a one time execution to just log a usage
		os.Exit(0)
	}

	usagemetrics.Configured()
	usagemetrics.Started()
	startServices(runtime.GOOS)
}
