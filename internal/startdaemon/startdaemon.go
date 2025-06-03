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
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"flag"
	"cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/agentmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcbdractions"
	"github.com/GoogleCloudPlatform/sapagent/internal/gcebeta"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/status"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapguestactions"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/wlm"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/metricevents"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/osinfo"

	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/recovery"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	collectionDefinitionName   = "collectiondefinition"
	hostMetricsServiceName     = "hostmetrics"
	processMetricsServiceName  = "processmetrics"
	workloadManagerServiceName = "workloadmanager"
	statusServiceName          = "status"
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
	return "Usage: startdaemon [-config <path-to-config-file>] \n"
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
	d.createLogDir()
	log.SetupLogging(d.lp)
	ctx, cancel := context.WithCancel(ctx)
	d.config, _ = configuration.ReadFromFile(d.configFilePath, os.ReadFile)
	if d.config.GetBareMetal() && d.config.GetCloudProperties() == nil {
		log.Logger.Error("Bare metal instance detected without cloud properties set. Manually set cloud properties in the configuration file to continue.")
		usagemetrics.Error(usagemetrics.BareMetalCloudPropertiesNotSet)
		os.Exit(0)
	}
	d.config = configuration.ApplyDefaults(d.config, d.cloudProps)
	d.lp.CloudLoggingClient = log.CloudLoggingClientWithUserAgent(ctx, d.config.GetCloudProperties().GetProjectId(), configuration.UserAgent())
	if d.lp.CloudLoggingClient != nil {
		defer d.lp.CloudLoggingClient.Close()
	}
	if d.config.GetCollectionConfiguration().GetMetricEventsLogDelaySeconds() > 0 {
		metricevents.SetLogDelay(time.Duration(d.config.GetCollectionConfiguration().GetMetricEventsLogDelaySeconds()) * time.Second)
	}
	return d.startdaemonHandler(ctx, cancel, false)
}

func (d *Daemon) createLogDir() {
	// Setup demon logging with default config, till we read it from the config file.
	d.lp.CloudLogName = `google-cloud-sap-agent`
	d.lp.LogFileName = `/var/log/google-cloud-sap-agent.log`
	oteDir := `/var/log/google-cloud-sap-agent`
	if d.lp.OSType == "windows" {
		d.lp.LogFileName = `C:\Program Files\Google\google-cloud-sap-agent\logs\google-cloud-sap-agent.log`
		oteDir = `C:\Program Files\Google\google-cloud-sap-agent\logs`
	}
	// Create the directory for one time logs.
	if err := os.MkdirAll(oteDir, 0770); err != nil {
		log.Logger.Warnw("Failed to create log directory", "error", err)
		return
	}
	// In case of upgrades when directory already exists, change the permissions of exiting directory to 0770.
	if err := os.Chmod(oteDir, 0770); err != nil {
		log.Logger.Warnw("Failed to change permissions of log directory", "error", err)
		return
	}

	// Make the group sapsys own the log directory. This is done on a best effort basis.
	// If any of the steps fail, we will just log the error and move on.
	g, err := user.LookupGroup("sapsys")
	if err != nil {
		log.Logger.Warnw("Failed to lookup group sapsys", "error", err)
		return
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		log.Logger.Warnw("Failed to convert sapsys group id to int", "error", err)
		return
	}
	if err := os.Chown(oteDir, -1, gid); err != nil {
		log.Logger.Warnw("Failed to change ownership of log directory", "error", err)
		return
	}
}

// startdaemonHandler starts up the main daemon for SAP Agent.
func (d *Daemon) startdaemonHandler(ctx context.Context, cancel context.CancelFunc, restarting bool) subcommands.ExitStatus {
	// Daemon mode operation
	if restarting {
		d.config, _ = configuration.ReadFromFile(d.configFilePath, os.ReadFile)
		d.config = configuration.ApplyDefaults(d.config, d.cloudProps)
	}
	d.lp.LogToCloud = d.config.GetLogToCloud().GetValue()
	d.lp.Level = configuration.LogLevelToZapcore(d.config.GetLogLevel())
	log.SetupLogging(d.lp)

	log.Logger.Infow("Agent version currently running", "version", configuration.AgentVersion)

	log.Logger.Infow("Cloud Properties we got from metadata server",
		"projectid", d.cloudProps.GetProjectId(),
		"projectnumber", d.cloudProps.GetNumericProjectId(),
		"instanceid", d.cloudProps.GetInstanceId(),
		"region", d.cloudProps.GetRegion(),
		"zone", d.cloudProps.GetZone(),
		"instancename", d.cloudProps.GetInstanceName(),
		"image", d.cloudProps.GetImage())

	log.Logger.Infow("Cloud Properties after applying defaults",
		"projectid", d.config.CloudProperties.GetProjectId(),
		"projectnumber", d.config.CloudProperties.GetNumericProjectId(),
		"instanceid", d.config.CloudProperties.GetInstanceId(),
		"region", d.config.CloudProperties.GetRegion(),
		"zone", d.config.CloudProperties.GetZone(),
		"instancename", d.config.CloudProperties.GetInstanceName(),
		"image", d.config.CloudProperties.GetImage())

	configureUsageMetricsForDaemon(d.config.GetCloudProperties())
	usagemetrics.Configured()
	if !restarting {
		usagemetrics.Started()
		go usagemetrics.LogRunningDaily()
		d.startGuestActions(cancel)
		d.startGCBDRActions()
		d.startConfigPollerRoutine(cancel)
	}
	d.startServices(ctx, cancel, runtime.GOOS, restarting)
	return subcommands.ExitSuccess
}

// configureUsageMetricsForDaemon sets up UsageMetrics for Daemon.
func configureUsageMetricsForDaemon(cp *iipb.CloudProperties) {
	usagemetrics.SetProperties(&cpb.AgentProperties{
		Name:            configuration.AgentName,
		Version:         configuration.AgentVersion,
		LogUsageMetrics: true,
	}, cp)
}

// startServices starts underlying services of SAP Agent.
func (d *Daemon) startServices(ctx context.Context, cancel context.CancelFunc, goos string, restarting bool) {
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
		amCtx := log.SetCtx(ctx, "context", "AgentMetrics")
		healthMonitor, err = startAgentMetricsService(amCtx, d.config)
		if err != nil {
			return
		}
	}

	// Create channels to subscribe to collection definition updates.
	chWLM := make(chan *cdpb.CollectionDefinition)
	chs := []chan<- *cdpb.CollectionDefinition{chWLM}

	cdCtx := log.SetCtx(ctx, "context", "CollectionDefinition")
	cdHeartbeatSpec, err := healthMonitor.Register(collectionDefinitionName)
	if err != nil {
		log.CtxLogger(cdCtx).Errorw("Failed to register collection definition health monitor", "error", err)
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		return
	}
	cd := collectiondefinition.Start(cdCtx, chs, collectiondefinition.StartOptions{
		HeartbeatSpec: cdHeartbeatSpec,
		LoadOptions: collectiondefinition.LoadOptions{
			CollectionConfig: d.config.GetCollectionConfiguration(),
			ReadFile:         os.ReadFile,
			OSType:           goos,
			Version:          configuration.AgentVersion,
			FetchOptions: collectiondefinition.FetchOptions{
				OSType:     goos,
				Env:        d.config.GetCollectionConfiguration().GetWorkloadValidationCollectionDefinition().GetConfigTargetEnvironment(),
				Client:     storage.NewClient,
				CreateTemp: os.CreateTemp,
				Execute:    execute,
			},
		},
	})

	gceService, err := gce.NewGCEClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create GCE service", "error", err)
		usagemetrics.Error(usagemetrics.GCEServiceCreateFailure)
		return
	}

	wlmService, err := wlm.NewWLMClient(ctx, d.config.GetCollectionConfiguration().GetDataWarehouseEndpoint())
	if err != nil {
		log.Logger.Errorw("Error creating WLM Client", "error", err)
		usagemetrics.Error(usagemetrics.WLMServiceCreateFailure)
		return
	}

	// Start SAP System Discovery
	ssdCtx := log.SetCtx(ctx, "context", "SAPSystemDiscovery")
	systemDiscovery := &system.Discovery{
		WlmService:    wlmService,
		AppsDiscovery: sapdiscovery.SAPApplications,
		CloudDiscoveryInterface: &clouddiscovery.CloudDiscovery{
			GceService:   gceService,
			HostResolver: net.LookupHost,
		},
		HostDiscoveryInterface: &hostdiscovery.HostDiscovery{
			Exists:  commandlineexecutor.CommandExists,
			Execute: commandlineexecutor.ExecuteCommand,
		},
		SapDiscoveryInterface: &appsdiscovery.SapDiscovery{
			Execute:    commandlineexecutor.ExecuteCommand,
			FileSystem: filesystem.Helper{},
		},
		OSStatReader: osStatReader,
		FileReader:   configFileReader,
	}
	if d.lp.CloudLoggingClient != nil {
		systemDiscovery.CloudLogInterface = d.lp.CloudLoggingClient.Logger("google-cloud-sap-agent")
		system.StartSAPSystemDiscovery(ssdCtx, d.config, systemDiscovery)
		log.FlushCloudLog()
	} else {
		system.StartSAPSystemDiscovery(ssdCtx, d.config, systemDiscovery)
	}

	gceBetaService := &gcebeta.GCEBeta{}
	if strings.Contains(d.config.GetServiceEndpointOverride(), "beta") {
		gceBetaService, err = gcebeta.NewGCEClient(ctx)
		if err != nil {
			log.Logger.Errorw("Failed to create GCE beta service", "error", err)
			usagemetrics.Error(usagemetrics.GCEServiceCreateFailure)
			return
		}
		log.Logger.Infow("Service endpoint override", "endpoint", d.config.GetServiceEndpointOverride())
		gceService.OverrideComputeBasePath(d.config.GetServiceEndpointOverride())
		if gceBetaService != nil {
			gceBetaService.OverrideComputeBasePath(d.config.GetServiceEndpointOverride())
		}
	}
	ppr := &instanceinfo.PhysicalPathReader{OS: goos}
	instanceInfoReader := instanceinfo.New(ppr, gceService)
	ua := fmt.Sprintf("sap-core-eng/%s/%s.%s/wlmevaluation", configuration.AgentName, configuration.AgentVersion, configuration.AgentBuildChange)
	clientOptions := []option.ClientOption{option.WithUserAgent(ua)}
	wlmMetricClient, err := monitoring.NewMetricClient(ctx, clientOptions...)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring metric client for workload manager evalution metrics", "error", err)
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
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
		WorkloadConfig:    cd.GetWorkloadValidation(),
		WorkloadConfigCh:  chWLM,
		Remote:            false,
		TimeSeriesCreator: wlmMetricClient,
		BackOffs:          cloudmonitoring.NewDefaultBackOffIntervals(),
		Execute:           execute,
		Exists:            exists,
		HeartbeatSpec:     wlmHeartbeatSpec,
		GCEService:        gceService,
		WLMService:        wlmService,
		Discovery:         systemDiscovery,
	}
	if d.config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection() != nil {
		// When set to collect workload manager metrics remotely then that is all this runtime will do.
		wlmparams.Remote = true
		log.Logger.Info("Collecting Workload Manager metrics remotely, will not start any other services")
		wmCtx := log.SetCtx(ctx, "context", "WorkloadManagerMetrics")
		workloadmanager.StartMetricsCollection(wmCtx, wlmparams)
		waitForShutdown(ctx, shutdownch, cancel, restarting)
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
	hmCtx := log.SetCtx(ctx, "context", "HostMetrics")
	hmp := HostMetricsParams{d.config, instanceInfoReader, cmr, healthMonitor}
	hmp.startCollection(hmCtx, restarting)

	// Start the Workload Manager metrics collection
	wmCtx := log.SetCtx(ctx, "context", "WorkloadManagerMetrics")
	wmp := WorkloadManagerParams{wlmparams, instanceInfoReader}
	wmp.startCollection(wmCtx)

	// Declaring pacemaker Params
	pcmp := pacemaker.Parameters{
		Config:                d.config,
		WorkloadConfig:        cd.GetWorkloadValidation(),
		ConfigFileReader:      pacemaker.ConfigFileReader(configFileReader),
		DefaultTokenGetter:    pacemaker.DefaultTokenGetter(defaultTokenGetter),
		JSONCredentialsGetter: pacemaker.JSONCredentialsGetter(jsonCredentialsGetter),
		Execute:               execute,
		Exists:                exists,
	}

	// Start Process Metrics Collection
	pmCtx := log.SetCtx(ctx, "context", "ProcessMetrics")
	pmp := ProcessMetricsParams{d.config, goos, healthMonitor, gceService, gceBetaService, systemDiscovery, pcmp}
	pmp.startCollection(pmCtx)

	// Start HANA Monitoring
	hanaCtx := log.SetCtx(ctx, "context", "HANAMonitoring")
	ua = fmt.Sprintf("sap-core-eng/%s/%s.%s/hanamonitoring", configuration.AgentName, configuration.AgentVersion, configuration.AgentBuildChange)
	clientOptions = []option.ClientOption{option.WithUserAgent(ua)}
	hanaMonitoringMetricClient, err := monitoring.NewMetricClient(ctx, clientOptions...)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring metric client for HANA Monitoring metrics", "error", err)
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return
	}
	hanamonitoring.Start(hanaCtx, hanamonitoring.Parameters{
		Config:                  d.config,
		GCEService:              gceService,
		BackOffs:                cloudmonitoring.NewDefaultBackOffIntervals(),
		TimeSeriesCreator:       hanaMonitoringMetricClient,
		HRC:                     sapdiscovery.HANAReplicationConfig,
		SystemDiscovery:         systemDiscovery,
		ConnectionRetryInterval: 300 * time.Second,
	})

	// Start Status Collection
	statusCtx := log.SetCtx(ctx, "context", "Status")
	sp := StatusParams{&status.Status{
		ConfigFilePath: d.configFilePath,
		CloudProps:     d.cloudProps,
		WLMService:     wlmService}, healthMonitor}
	sp.startCollection(statusCtx)

	waitForShutdown(ctx, shutdownch, cancel, restarting)
}

func (d *Daemon) startGuestActions(cancel context.CancelFunc) {
	// Start Guest Actions ACS Communication with a separate new context (not impacted by cancels).
	guestActionsCtx := log.SetCtx(context.Background(), "context", "GuestActions")
	sapguestactions.StartACSCommunication(guestActionsCtx, d.config)
}

func (d *Daemon) startGCBDRActions() {
	// Start GCBDR ACS Communication with a separate new context (not impacted by cancels).
	gcbdrActionsCtx := log.SetCtx(context.Background(), "context", "GCBDRActions")
	gcbdractions.StartACSCommunication(gcbdrActionsCtx, d.config)
}

// startAgentMetricsService returns health monitor for services.
func startAgentMetricsService(ctx context.Context, c *cpb.Configuration) (*heartbeat.Monitor, error) {
	var healthMonitor *heartbeat.Monitor
	heartbeatParams := heartbeat.Parameters{
		Config: c,
	}
	heartMonitor, err := heartbeat.NewMonitor(heartbeatParams)
	healthMonitor = heartMonitor
	if err != nil {
		log.CtxLogger(ctx).Error("Failed to create heartbeat monitor", log.Error(err))
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
		log.CtxLogger(ctx).Error("Failed to create agent metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.AgentMetricsServiceCreateFailure)
		return nil, err
	}
	agentmetricsService.Start(ctx)
	return healthMonitor, nil
}

// ProcessMetricsParams has arguments for startProcessMetricsCollection.
type ProcessMetricsParams struct {
	config         *cpb.Configuration
	goos           string
	healthMonitor  agentmetrics.HealthMonitor
	gceService     *gce.GCE
	gceBetaService *gcebeta.GCEBeta
	discovery      *system.Discovery
	pcmparams      pacemaker.Parameters
}

// startCollection for ProcessMetricsParams initiates collection of ProcessMetrics.
func (pmp ProcessMetricsParams) startCollection(ctx context.Context) {
	pmHeartbeatSpec, err := pmp.healthMonitor.Register(processMetricsServiceName)
	if err != nil {
		log.Logger.Error("Failed to register process metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		log.Logger.Error("Process metrics collection could not be started")
		return
	}
	if success := processmetrics.Start(ctx, processmetrics.Parameters{
		Config:         pmp.config,
		OSType:         pmp.goos,
		MetricClient:   processmetrics.NewMetricClient,
		BackOffs:       cloudmonitoring.NewDefaultBackOffIntervals(),
		HeartbeatSpec:  pmHeartbeatSpec,
		GCEService:     pmp.gceService,
		GCEBetaService: pmp.gceBetaService,
		Discovery:      pmp.discovery,
		PCMParams:      pmp.pcmparams,
		OSStatReader:   osStatReader,
	}); success != true {
		log.Logger.Info("Process metrics collection not started")
	}
}

// HostMetricsParams has arguments for startHostMetricsCollection.
type HostMetricsParams struct {
	config             *cpb.Configuration
	instanceInfoReader *instanceinfo.Reader
	cmr                *cloudmetricreader.CloudMetricReader
	healthMonitor      agentmetrics.HealthMonitor
}

// startCollection for HostMetricsParams initiates collection of HostMetrics.
func (hmp HostMetricsParams) startCollection(ctx context.Context, restarting bool) {
	hmHeartbeatSpec, err := hmp.healthMonitor.Register(hostMetricsServiceName)
	if err != nil {
		log.Logger.Error("Failed to register host metrics service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		log.Logger.Error("Failed to start host metrics collection")
		return
	}
	hmCtx, hmCancel := context.WithCancel(ctx)
	hostmetrics.StartSAPHostAgentProvider(hmCtx, hmCancel, restarting, hostmetrics.Parameters{
		Config:             hmp.config,
		InstanceInfoReader: *hmp.instanceInfoReader,
		CloudMetricReader:  *hmp.cmr,
		AgentTime:          *agenttime.New(agenttime.Clock{}),
		HeartbeatSpec:      hmHeartbeatSpec,
	})
}

// WorkloadManagerParams has arguments for startWorkloadManagerMetricsCollection.
type WorkloadManagerParams struct {
	wlmparams          workloadmanager.Parameters
	instanceInfoReader *instanceinfo.Reader
}

// startCollection for WorkLoadManagerParams initiates collection of WorkloadManagerMetrics.
func (wmp WorkloadManagerParams) startCollection(ctx context.Context) {
	wmp.wlmparams.OSType = osinfo.OSName
	wmp.wlmparams.ConfigFileReader = configFileReader
	wmp.wlmparams.InstanceInfoReader = *wmp.instanceInfoReader
	wmp.wlmparams.OSStatReader = osStatReader
	wmp.wlmparams.OSReleaseFilePath = osinfo.OSReleaseFilePath
	wmp.wlmparams.InterfaceAddrsGetter = net.InterfaceAddrs
	wmp.wlmparams.DefaultTokenGetter = defaultTokenGetter
	wmp.wlmparams.JSONCredentialsGetter = jsonCredentialsGetter
	wmp.wlmparams.Init(ctx)
	workloadmanager.StartMetricsCollection(ctx, wmp.wlmparams)
}

// StatusParams has arguments for StartStatusCollection.
type StatusParams struct {
	status        *status.Status
	healthMonitor agentmetrics.HealthMonitor
}

func (sp StatusParams) startCollection(ctx context.Context) {
	statusHeartbeatSpec, err := sp.healthMonitor.Register(statusServiceName)
	if err != nil {
		log.Logger.Error("Failed to register status service", log.Error(err))
		usagemetrics.Error(usagemetrics.HeartbeatMonitorRegistrationFailure)
		return
	}
	sp.status.HeartbeatSpec = statusHeartbeatSpec
	if err := sp.status.Init(ctx); err != nil {
		log.CtxLogger(ctx).Errorw("Could not initialize status", "error", err)
		return
	}
	sp.status.StartStatusCollection(ctx)
}

// waitForShutdown observes a channel for a shutdown signal, then proceeds to shut down the Agent.
func waitForShutdown(ctx context.Context, ch <-chan os.Signal, cancel context.CancelFunc, restarting bool) {
	// If we're restarting, we wait for context cancellation instead of a shutdown signal.
	if restarting {
		<-ctx.Done()
		log.Logger.Info("Skipping shutdown signal handling during restart")
		return
	}

	// If the shutdown signal is observed, we will shut down by executing the following lines.
	<-ch

	log.Logger.Info("Shutdown signal observed, the agent will begin shutting down")
	cancel()
	usagemetrics.Stopped()
	time.Sleep(3 * time.Second)
	log.Logger.Info("Shutting down...")
}

func (d *Daemon) pollConfigFile(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	prev, err := d.lastModifiedTime(ctx)
	shutdownch := make(chan os.Signal, 1)
	signal.Notify(shutdownch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to get last modified time for config file", "error", err)
		return
	}
	for {
		select {
		case <-shutdownch:
			log.CtxLogger(ctx).Info("Shutdown signal observed, exiting the config poller")
			return
		case <-ticker.C:
			log.CtxLogger(ctx).Debug("Polling config file")
			res, err := d.lastModifiedTime(ctx)
			if err != nil {
				log.Logger.Errorw("Failed to get last modified time for config file", "error", err)
				continue
			}
			if res.After(prev) {
				log.CtxLogger(ctx).Infow("Config file changed, restarting daemon", "configFile", d.configFilePath)
				cancel = d.Restart(cancel)
				prev = res
			}
		}
	}
}

func (d *Daemon) lastModifiedTime(ctx context.Context) (time.Time, error) {
	path := d.configFilePath
	if len(path) == 0 {
		switch runtime.GOOS {
		case "linux":
			path = configuration.LinuxConfigPath
		case "windows":
			path = configuration.WindowsConfigPath
		}
	}
	res, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return res.ModTime(), nil
}

func (d *Daemon) startConfigPollerRoutine(cancel context.CancelFunc) {
	pollConfigFileRoutine := &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, arg any) {
			if cancelFunc, ok := arg.(context.CancelFunc); ok {
				d.pollConfigFile(ctx, cancelFunc)
			}
		},
		RoutineArg:          cancel,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: 1 * time.Second,
	}
	configPollerCtx := log.SetCtx(context.Background(), "context", "ConfigPoller")
	usagemetrics.Action(usagemetrics.ConfigPollerStarted)
	pollConfigFileRoutine.StartRoutine(configPollerCtx)
}

// Restart restarts the daemon services.
func (d *Daemon) Restart(cancel context.CancelFunc) context.CancelFunc {
	log.Logger.Info("Restarting daemon services")
	cancel()
	time.Sleep(5 * time.Second)
	ctx, newCancel := context.WithCancel(context.Background())
	log.Logger.Infow("Restarting daemon services", "d", d)
	go d.startdaemonHandler(ctx, cancel, true)
	return newCancel
}
