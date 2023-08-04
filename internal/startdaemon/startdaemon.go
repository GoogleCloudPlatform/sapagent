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

// Package startdaemon implements startdaemon mode execution subcommand in agent for SAP.
package startdaemon

import (
	"context"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/agentmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	hostMetricsServiceName     = "hostmetrics"
	processMetricsServiceName  = "processmetrics"
	workloadManagerServiceName = "workloadmanager"
)

var (
	osStatReader = workloadmanager.OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
	configFileReader = workloadmanager.ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := os.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	execute = commandlineexecutor.Execute(func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.ExecuteCommand(ctx, params)
	})
	exists = commandlineexecutor.Exists(func(exe string) bool {
		return commandlineexecutor.CommandExists(exe)
	})
	defaultTokenGetter = workloadmanager.DefaultTokenGetter(func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
		return google.DefaultTokenSource(ctx, scopes...)
	})
	jsonCredentialsGetter = workloadmanager.JSONCredentialsGetter(func(ctx context.Context, json []byte, scopes ...string) (*google.Credentials, error) {
		return google.CredentialsFromJSON(ctx, json, scopes...)
	})
)

// Daemon has args for startdaemon subcommand.
type Daemon struct {
	configFilePath string
	lp             log.Parameters
	config         *cpb.Configuration
	cloudProps     *iipb.CloudProperties
}

// Name implements the subcommand interface for startdaemon.
func (*Daemon) Name() string { return "startdaemon" }

// Synopsis implements the subcommand interface for startdaemon.
func (*Daemon) Synopsis() string { return "start daemon mode of the agent" }

// Usage implements the subcommand interface for startdaemon.
func (*Daemon) Usage() string {
	return "startdaemon [-config <path-to-config-file>] \n"
}

// SetFlags implements the subcommand interface for startdaemon.
func (d *Daemon) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&d.configFilePath, "config", "", "configuration path for startdaemon mode")
	fs.StringVar(&d.configFilePath, "c", "", "configuration path for startdaemon mode")
}

// Execute implements the subcommand interface for startdaemon.
func (d *Daemon) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 3 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	var ok bool
	d.lp, ok = args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	d.cloudProps, ok = args[2].(*iipb.CloudProperties)
	if !ok {
		log.Logger.Errorw("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	// Setup demon logging with default config, till we read it from the config file.
	d.lp.CloudLogName = `google-cloud-sap-agent`
	d.lp.LogFileName = `/var/log/google-cloud-sap-agent.log`
	if d.lp.OSType == "windows" {
		d.lp.LogFileName = `C:\Program Files\Google\google-cloud-sap-agent\logs\google-cloud-sap-agent.log`
	}
	log.SetupLogging(d.lp)

	return d.startdaemonHandler(ctx)
}

func (d *Daemon) startdaemonHandler(ctx context.Context) subcommands.ExitStatus {
	// Daemon mode operation
	d.config = configuration.ReadFromFile(d.configFilePath, os.ReadFile)
	if d.config.GetBareMetal() && d.config.GetCloudProperties() == nil {
		log.Logger.Error("Bare metal instance detected without cloud properties set. Manually set cloud properties in the configuration file to continue.")
		usagemetrics.Error(usagemetrics.BareMetalCloudPropertiesNotSet)
		os.Exit(0)
	}

	d.config = configuration.ApplyDefaults(d.config, d.cloudProps)
	d.lp.LogToCloud = d.config.GetLogToCloud()
	d.lp.Level = configuration.LogLevelToZapcore(d.config.GetLogLevel())
	d.lp.CloudLoggingClient = log.CloudLoggingClient(ctx, d.config.GetCloudProperties().GetProjectId())
	if d.lp.CloudLoggingClient != nil {
		defer d.lp.CloudLoggingClient.Close()
	}
	log.SetupLogging(d.lp)

	log.Logger.Infow("Agent version currently running", "version", configuration.AgentVersion)

	log.Logger.Infow("Cloud Properties we got from metadata server",
		"projectid", d.cloudProps.GetProjectId(),
		"projectnumber", d.cloudProps.GetNumericProjectId(),
		"instanceid", d.cloudProps.GetInstanceId(),
		"zone", d.cloudProps.GetZone(),
		"instancename", d.cloudProps.GetInstanceName(),
		"image", d.cloudProps.GetImage())

	log.Logger.Infow("Cloud Properties after applying defaults",
		"projectid", d.config.CloudProperties.GetProjectId(),
		"projectnumber", d.config.CloudProperties.GetNumericProjectId(),
		"instanceid", d.config.CloudProperties.GetInstanceId(),
		"zone", d.config.CloudProperties.GetZone(),
		"instancename", d.config.CloudProperties.GetInstanceName(),
		"image", d.config.CloudProperties.GetImage())

	configureUsageMetricsForDaemon(d.config.GetCloudProperties())
	usagemetrics.Configured()
	usagemetrics.Started()
	d.startServices(ctx, runtime.GOOS)
	return subcommands.ExitSuccess
}

func configureUsageMetricsForDaemon(cp *iipb.CloudProperties) {
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            configuration.AgentName,
		Version:         configuration.AgentVersion,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}

func (d *Daemon) startServices(ctx context.Context, goos string) {
	if d.config.GetCloudProperties() == nil {
		log.Logger.Error("Cloud properties are not set, cannot start services.")
		usagemetrics.Error(usagemetrics.CloudPropertiesNotSet)
		return
	}

	shutdownch := make(chan os.Signal, 1)
	signal.Notify(shutdownch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	// When not collecting agent metrics and service health, the NullMonitor will provide
	// sensible NOOPs. Downstream services can safely register and use the provided *Spec
	// without fear nor penalty.
	var healthMonitor agentmetrics.HealthMonitor = &heartbeat.NullMonitor{}
	var err error
	if d.config.GetCollectionConfiguration().GetCollectAgentMetrics() {
		healthMonitor, err = startAgentMetricsService(ctx, d.config)
		if err != nil {
			return
		}
	}

	gceService, err := gce.NewGCEClient(ctx)
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
	wlmService, err := gce.NewWLMClient(ctx)
	if err != nil {
		log.Logger.Errorw("Error creating WLM Client", "error", err)
		usagemetrics.Error(usagemetrics.WLMServiceCreateFailure)
		return
	}

	wlmHeartbeatSpec, err := healthMonitor.Register(workloadManagerServiceName)
	if err != nil {
		log.Logger.Error("Failed to register workload manager service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		return
	}
	wlmparams := workloadmanager.Parameters{
		Config:            d.config,
		Remote:            false,
		TimeSeriesCreator: mc,
		BackOffs:          cloudmonitoring.NewDefaultBackOffIntervals(),
		Execute:           execute,
		Exists:            exists,
		HeartbeatSpec:     wlmHeartbeatSpec,
		GCEService:        gceService,
		WLMService:        wlmService,
	}
	if d.config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection() != nil {
		// When set to collect workload manager metrics remotely then that is all this runtime will do.
		wlmparams.Remote = true
		log.Logger.Info("Collecting Workload Manager metrics remotely, will not start any other services")
		workloadmanager.StartMetricsCollection(ctx, wlmparams)
		go usagemetrics.LogRunningDaily()
		waitForShutdown(ctx, shutdownch)
		return
	}

	// The functions being called below should be asynchronous.
	// A typical StartXXX() will do the necessary initialisation synchronously
	// and start its own goroutines for the long running tasks. The control
	// should be returned to main immediately after init succeeds.

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

	// start the Host Metrics Collection
	if err = startHostMetricsCollection(ctx, d.config, instanceInfoReader, cmr, healthMonitor); err != nil {
		return
	}

	// Start the Workload Manager metrics collection
	startWorkloadManagerMetricsCollection(ctx, wlmparams, instanceInfoReader, goos)

	// Start Process Metrics Collection
	if err = startProcessMetricsCollection(ctx, d.config, goos, healthMonitor, gceService); err != nil {
		return
	}

	// Start SAP System Discovery
	// TODO: Use the global cloud logging client for sap system.
	logClient := log.CloudLoggingClient(ctx, d.config.GetCloudProperties().ProjectId)
	if logClient != nil {
		system.StartSAPSystemDiscovery(ctx, d.config, gceService, wlmService, logClient.Logger("google-cloud-sap-agent"))
		log.FlushCloudLog()
		defer logClient.Close()
	} else {
		system.StartSAPSystemDiscovery(ctx, d.config, gceService, wlmService, nil)
	}

	// Start HANA Monitoring
	hanamonitoring.Start(ctx, hanamonitoring.Parameters{
		Config:            d.config,
		GCEService:        gceService,
		BackOffs:          cloudmonitoring.NewDefaultBackOffIntervals(),
		TimeSeriesCreator: mc,
	})

	go usagemetrics.LogRunningDaily()
	waitForShutdown(ctx, shutdownch)
}

func startAgentMetricsService(ctx context.Context, c *cpb.Configuration) (*heartbeat.Monitor, error) {
	var healthMonitor *heartbeat.Monitor
	heartbeatParams := heartbeat.Parameters{
		Config: c,
	}
	heartMonitor, err := heartbeat.NewMonitor(heartbeatParams)
	healthMonitor = heartMonitor
	if err != nil {
		log.Logger.Error("Failed to create heartbeat monitor", log.Error(err))
		usagemetrics.Error(usagemetrics.AgentMetricsServiceCreateFailure)
		return nil, err
	}
	agentMetricsParams := agentmetrics.Parameters{
		Config:        c,
		BackOffs:      cloudmonitoring.NewDefaultBackOffIntervals(),
		HealthMonitor: healthMonitor,
	}
	agentmetricsService, err := agentmetrics.NewService(ctx, agentMetricsParams)
	if err != nil {
		log.Logger.Error("Failed to create agent metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.AgentMetricsServiceCreateFailure)
		return nil, err
	}
	agentmetricsService.Start(ctx)
	return healthMonitor, nil
}

func startProcessMetricsCollection(ctx context.Context, config *cpb.Configuration, goos string, healthMonitor agentmetrics.HealthMonitor, gceService *gce.GCE) error {
	pmHeartbeatSpec, err := healthMonitor.Register(processMetricsServiceName)
	if err != nil {
		log.Logger.Error("Failed to register process metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		return err
	}
	processmetrics.Start(ctx, processmetrics.Parameters{
		Config:        config,
		OSType:        goos,
		MetricClient:  processmetrics.NewMetricClient,
		BackOffs:      cloudmonitoring.NewDefaultBackOffIntervals(),
		HeartbeatSpec: pmHeartbeatSpec,
		GCEService:    gceService,
	})
	return nil
}

func startHostMetricsCollection(ctx context.Context, config *cpb.Configuration, instanceInfoReader *instanceinfo.Reader, cmr *cloudmetricreader.CloudMetricReader, healthMonitor agentmetrics.HealthMonitor) error {
	hmHeartbeatSpec, err := healthMonitor.Register(hostMetricsServiceName)
	if err != nil {
		log.Logger.Error("Failed to register host metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		return err
	}
	hostmetrics.StartSAPHostAgentProvider(ctx, hostmetrics.Parameters{
		Config:             config,
		InstanceInfoReader: *instanceInfoReader,
		CloudMetricReader:  *cmr,
		AgentTime:          *agenttime.New(agenttime.Clock{}),
		HeartbeatSpec:      hmHeartbeatSpec,
	})
	return nil
}

func startWorkloadManagerMetricsCollection(ctx context.Context, wlmparams workloadmanager.Parameters, instanceInfoReader *instanceinfo.Reader, goos string) {
	cd, err := collectiondefinition.Load(collectiondefinition.LoadOptions{
		ReadFile: os.ReadFile,
		OSType:   goos,
		Version:  configuration.AgentVersion,
	})
	if err != nil {
		// In the event of an error, log the problem that occurred but allow
		// other agent services to start up.
		id := usagemetrics.CollectionDefinitionLoadFailure
		if _, ok := err.(collectiondefinition.ValidationError); ok {
			id = usagemetrics.CollectionDefinitionValidateFailure
		}
		usagemetrics.Error(id)
		log.Logger.Error(err)
	} else {
		wlmparams.WorkloadConfig = cd.GetWorkloadValidation()
		wlmparams.OSType = goos
		wlmparams.ConfigFileReader = configFileReader
		wlmparams.InstanceInfoReader = *instanceInfoReader
		wlmparams.OSStatReader = osStatReader
		wlmparams.OSReleaseFilePath = workloadmanager.OSReleaseFilePath
		wlmparams.InterfaceAddrsGetter = net.InterfaceAddrs
		wlmparams.DefaultTokenGetter = defaultTokenGetter
		wlmparams.JSONCredentialsGetter = jsonCredentialsGetter
		wlmparams.SetOSReleaseInfo()
		wlmparams.DiscoverNetWeaver(ctx)
		wlmparams.ReadHANAInsightsRules()
		workloadmanager.StartMetricsCollection(ctx, wlmparams)
	}
}

// waitForShutdown observes a channel for a shutdown signal, then proceeds to shut down the Agent.
func waitForShutdown(ctx context.Context, ch <-chan os.Signal) {
	// wait for the shutdown signal
	<-ch
	// once we have a shutdown event we will wait for up to 3 seconds before for final terminations
	_, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer handleShutdown(cancel)
}

func handleShutdown(cancel context.CancelFunc) {
	log.Logger.Info("Shutting down...")
	usagemetrics.Stopped()
	cancel()
}
