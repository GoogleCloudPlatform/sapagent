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
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"flag"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/agentmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type executionMode int

const (
	daemonMode = iota
	oneTimeMode
)
const (
	hostMetricsServiceName     = "hostmetrics"
	processMetricsServiceName  = "processmetrics"
	workloadManagerServiceName = "workloadmanager"
)

var (
	configPath   string
	config       *cpb.Configuration
	osStatReader = workloadmanager.OSStatReader(func(f string) (os.FileInfo, error) {
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

func configureUsageMetricsForDaemon(cp *iipb.CloudProperties) {
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            configuration.AgentName,
		Version:         configuration.AgentVersion,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
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

	// When not collecting agent metrics and service health, the NullMonitor will provide
	// sensible NOOPs. Downstream services can safely register and use the provided *Spec
	// without fear nor penalty.
	var healthMonitor agentmetrics.HealthMonitor = &heartbeat.NullMonitor{}
	if config.GetCollectionConfiguration().GetCollectAgentMetrics() {
		heartbeatParams := heartbeat.Parameters{
			Config: config,
		}
		heartMonitor, err := heartbeat.NewMonitor(heartbeatParams)
		healthMonitor = heartMonitor
		if err != nil {
			log.Logger.Error("Failed to create heartbeat monitor", log.Error(err))
			usagemetrics.Error(usagemetrics.AgentMetricsServiceCreateFailure)
			return
		}
		agentMetricsParams := agentmetrics.Parameters{
			Config:        config,
			BackOffs:      cloudmonitoring.NewDefaultBackOffIntervals(),
			HealthMonitor: healthMonitor,
		}
		agentmetricsService, err := agentmetrics.NewService(ctx, agentMetricsParams)
		if err != nil {
			log.Logger.Error("Failed to create agent metrics service", log.Error(err))
			usagemetrics.Error(usagemetrics.AgentMetricsServiceCreateFailure)
			return
		}
		agentmetricsService.Start(ctx)
	}

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
		heartbeatSpec, err := healthMonitor.Register(workloadManagerServiceName)
		if err != nil {
			log.Logger.Error("Failed to register workload manager service", log.Error(err))
			usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
			return
		}
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
			HeartbeatSpec:        heartbeatSpec,
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
		heartbeatSpec, err := healthMonitor.Register(hostMetricsServiceName)
		if err != nil {
			log.Logger.Error("Failed to register host metrics service", log.Error(err))
			usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
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
			HeartbeatSpec:      heartbeatSpec,
		}
		hostmetrics.StartSAPHostAgentProvider(ctx, hmparams)

		// Start the Workload Manager metrics collection
		heartbeatSpec, err = healthMonitor.Register(workloadManagerServiceName)
		if err != nil {
			log.Logger.Error("Failed to register workload manager service", log.Error(err))
			usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
			return
		}
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
			HeartbeatSpec:         heartbeatSpec,
		}
		workloadmanager.StartMetricsCollection(ctx, wlmparams)

		heartbeatSpec, err = healthMonitor.Register(processMetricsServiceName)
		if err != nil {
			log.Logger.Error("Failed to register process metrics service", log.Error(err))
			usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
			return
		}
		// Start the Process metrics collection
		pmparams := processmetrics.Parameters{
			Config:        config,
			OSType:        goos,
			MetricClient:  processmetrics.NewMetricClient,
			BackOffs:      cloudmonitoring.NewDefaultBackOffIntervals(),
			HeartbeatSpec: heartbeatSpec,
			GCEService:    gceService,
		}
		processmetrics.Start(ctx, pmparams)

		system.StartSAPSystemDiscovery(ctx, config, gceService)

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

func readExecutionMode(args []string) executionMode {
	switch {
	case len(args) == 1:
		return daemonMode
	case len(args) == 2 && (strings.HasPrefix(args[1], "-c") || strings.HasPrefix(args[1], "--config")):
		return daemonMode
	default:
		return oneTimeMode
	}
}

func registerSubCommands() {
	for _, command := range [...]subcommands.Command{
		subcommands.HelpCommand(),  // Implement "help"
		subcommands.FlagsCommand(), // Implement "flags"
		&onetime.LogUsage{},
		&onetime.MaintenanceMode{},
		&onetime.RemoteValidation{},
		&onetime.Snapshot{},
	} {
		subcommands.Register(command, "")
	}
	// Global flags for daemon mode config file override.
	flag.StringVar(&configPath, "config", "", "configuration path for daemon mode")
	flag.StringVar(&configPath, "c", "", "configuration path for daemon mode")
	flag.Parse()
}

func main() {
	registerSubCommands()
	if readExecutionMode(os.Args) == oneTimeMode {
		// One-time execution
		os.Exit(int(subcommands.Execute(context.Background())))
		return
	}

	// Daemon mode operation
	log.SetupDaemonLogging(runtime.GOOS, cpb.Configuration_INFO)
	config = configuration.ReadFromFile(configPath, os.ReadFile)
	if config.GetBareMetal() && config.GetCloudProperties() == nil {
		log.Logger.Error("Bare metal instance detected without cloud properties set. Manually set cloud properties in the configuration file to continue.")
		usagemetrics.Error(usagemetrics.BareMetalCloudPropertiesNotSet)
		os.Exit(0)
	}
	log.SetupDaemonLogging(runtime.GOOS, config.GetLogLevel())
	cloudProps := metadataserver.FetchCloudProperties()
	config = configuration.ApplyDefaults(config, cloudProps)

	configureUsageMetricsForDaemon(cloudProps)
	usagemetrics.Configured()
	usagemetrics.Started()
	startServices(runtime.GOOS)
}
