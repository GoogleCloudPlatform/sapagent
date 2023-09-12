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

// Package processmetrics provides process metrics collection capability for GC SAP Agent.
// This package is the landing space for process metrics collection in SAP GC Agent.
// This package abstracts the underlying metric collectors from the caller. The package
// is responsible for discovering the SAP applications running on this machine, and
// starting the relevant metric collectors in background.
// The package is responsible for the lifecycle of process metric specific jobs which involves:
//   - Create asynchronous jobs using the configuration passed by the caller.
//   - Monitor the job statuses and communicate with other components of GC SAP Agent.
//   - Restart the jobs that fail to ensure we continue to push the metrics.
//   - Send telemetry on aggregated job stats.
//
// The package defines and consumes Collector interface. Each individual metric collection file -
// hana, netweaver, cluster etc need to implement this interface to leverage the common
// collectMetrics and sendMetrics functions.
package processmetrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/cluster"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/computeresources"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/fastmovingmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/hana"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/infra"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/netweaver"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapservice"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (
	// Collector interface is SAP application specific metric collection logic.
	// This needs to be implemented by application specific modules that want to leverage
	// startMetricGroup functionality.
	Collector interface {
		Collect(context.Context) ([]*mrpb.TimeSeries, error)
		CollectWithRetry(context.Context) ([]*mrpb.TimeSeries, error)
	}

	// Properties has necessary context for Metrics collection.
	Properties struct {
		SAPInstances         *sapb.SAPInstances // Optional for production use cases, used by unit tests.
		Config               *cpb.Configuration
		Client               cloudmonitoring.TimeSeriesCreator
		Collectors           []Collector
		FastMovingCollectors []Collector
		HeartbeatSpec        *heartbeat.Spec
	}

	// CreateMetricClient provides an easily testable translation to the cloud monitoring API.
	CreateMetricClient func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error)

	// Parameters has parameters necessary to invoke Start().
	Parameters struct {
		Config          *cpb.Configuration
		OSType          string
		MetricClient    CreateMetricClient
		SAPInstances    *sapb.SAPInstances
		BackOffs        *cloudmonitoring.BackOffIntervals
		HeartbeatSpec   *heartbeat.Spec
		GCEService      sapdiscovery.GCEInterface
		GCEAlphaService infra.GCEAlphaInterface
	}
)

const (
	maxTSPerRequest               = 200 // Reference: https://cloud.google.com/monitoring/quotas
	minimumFrequency              = 5
	minimumFrequencyForSlowMoving = 30
)

/*
Start starts collection if collect_process_metrics config option is enabled
in the configuration. The function is a NO-OP if the config option is not enabled.

If the config option is enabled, start the metric collector jobs
in the background and return control to the caller with return value = true.

Return false if the config option is not enabled.
*/
func Start(ctx context.Context, parameters Parameters) bool {
	cpm := parameters.Config.GetCollectionConfiguration().GetCollectProcessMetrics()
	cf := parameters.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()
	slowPmf := parameters.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()

	log.Logger.Infow("Configuration option for collect_process_metrics", "collectprocessmetrics", cpm)

	switch {
	case !cpm:
		log.Logger.Info("Not collecting Process Metrics.")
		return false
	case parameters.OSType == "windows":
		log.Logger.Info("Process Metrics collection is not supported for windows platform.")
		return false
	case cf < minimumFrequency:
		log.Logger.Infow("Process metrics frequency is smaller than minimum supported value.", "frequency", cf, "minimumfrequency", minimumFrequency)
		log.Logger.Info("Not collecting Process Metrics.")
		return false
	case slowPmf < minimumFrequencyForSlowMoving:
		log.Logger.Infow("Slow process metrics frequency is smaller than minimum supported value.", "frequency", slowPmf, "minimumfrequency", minimumFrequencyForSlowMoving)
		log.Logger.Info("Not collecting Process Metrics.")
		return false
	}

	mc, err := parameters.MetricClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring client", "error", err)
		usagemetrics.Error(usagemetrics.ProcessMetricsMetricClientCreateFailure) // Failed to create Cloud Monitoring client
		return false
	}

	sapInstances := instancesWithCredentials(ctx, &parameters)
	if len(sapInstances.GetInstances()) == 0 {
		log.Logger.Error("No SAP Instances found. Cannot start process metrics collection.")
		usagemetrics.Error(usagemetrics.NoSAPInstancesFound) // NO SAP instances found
		return false
	}

	log.Logger.Info("Starting process metrics collection in background.")
	go usagemetrics.LogActionDaily(usagemetrics.CollectProcessMetrics)
	p := create(ctx, parameters, mc, sapInstances)
	// NOMUTANTS--will be covered by integration testing
	go p.collectAndSend(ctx, parameters.BackOffs)
	// createWorkerPool creates a pool of workers to start collecting slow moving metrics in parallel
	// each collector has its own job of collecting and then sending the metrics to cloudmonitoring.
	// So after an error is encountered during metrics collection within a collector, while it retried
	// as per the retry policy, other collectors remain unaffected.
	go createWorkerPoolForSlowMetrics(ctx, p, parameters.BackOffs)
	return true
}

// NewMetricClient is the production version that calls cloud monitoring API.
func NewMetricClient(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
	return monitoring.NewMetricClient(ctx)
}

// create sets up the processmetrics properties and metric collectors for SAP Instances.
func create(ctx context.Context, params Parameters, client cloudmonitoring.TimeSeriesCreator, sapInstances *sapb.SAPInstances) *Properties {
	p := &Properties{
		SAPInstances:  sapInstances,
		Config:        params.Config,
		Client:        client,
		HeartbeatSpec: params.HeartbeatSpec,
	}

	skippedMetrics := make(map[string]bool)
	sl := p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip()
	for _, metric := range sl {
		skippedMetrics[metric] = true
	}

	log.Logger.Info("Creating SAP additional metrics collector for sapservices (active and enabled metric).")
	sapServiceCollector := &sapservice.InstanceProperties{
		Config:         p.Config,
		Client:         p.Client,
		Execute:        commandlineexecutor.ExecuteCommand,
		SkippedMetrics: skippedMetrics,
	}

	log.Logger.Info("Creating SAP control processes per process CPU, memory usage metrics collector.")
	sapStartCollector := &computeresources.SAPControlProcInstanceProperties{
		Config:         p.Config,
		Client:         p.Client,
		Executor:       commandlineexecutor.ExecuteCommand,
		SkippedMetrics: skippedMetrics,
	}

	log.Logger.Info("Creating infra migration event metrics collector.")
	migrationCollector := infra.New(p.Config, p.Client, params.GCEAlphaService, skippedMetrics, nil)

	p.Collectors = append(p.Collectors, sapServiceCollector, sapStartCollector, migrationCollector)

	sids := make(map[string]bool)
	clusterCollectorCreated := false
	for _, instance := range p.SAPInstances.GetInstances() {
		sids[instance.GetSapsid()] = true
		if p.SAPInstances.GetLinuxClusterMember() && clusterCollectorCreated == false {
			log.Logger.Infow("Creating cluster collector for instance", "instance", instance)
			clusterCollector := &cluster.InstanceProperties{
				SAPInstance:    instance,
				Config:         p.Config,
				Client:         p.Client,
				SkippedMetrics: skippedMetrics,
			}
			p.Collectors = append(p.Collectors, clusterCollector)
			clusterCollectorCreated = true
		}
		if instance.GetType() == sapb.InstanceType_HANA {
			log.Logger.Infow("Creating HANA per process CPU, memory usage metrics collector for instance", "instance", instance)
			hanaComputeresourcesCollector := &computeresources.HanaInstanceProperties{
				Config:      p.Config,
				Client:      p.Client,
				Executor:    commandlineexecutor.ExecuteCommand,
				SAPInstance: instance,
				ProcessListParams: commandlineexecutor.Params{
					User:        instance.GetUser(),
					Executable:  instance.GetSapcontrolPath(),
					ArgsToSplit: fmt.Sprintf("-nr %s -function GetProcessList -format script", instance.GetInstanceNumber()),
					Env:         []string{"LD_LIBRARY_PATH=" + instance.GetLdLibraryPath()},
				},
				SAPControlClient: sapcontrolclient.New(instance.GetInstanceNumber()),
				LastValue:        make(map[string]*process.IOCountersStat),
				SkippedMetrics:   skippedMetrics,
			}

			log.Logger.Infow("Creating HANA collector for instance.", "instance", instance)
			hanaCollector := &hana.InstanceProperties{
				SAPInstance:        instance,
				Config:             p.Config,
				Client:             p.Client,
				HANAQueryFailCount: 0,
				SkippedMetrics:     skippedMetrics,
			}
			p.Collectors = append(p.Collectors, hanaComputeresourcesCollector, hanaCollector)

			log.Logger.Infow("Creating FastMoving Collector for HANA", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance: instance,
				Config:      p.Config,
				Client:      p.Client,
			}
			p.FastMovingCollectors = append(p.FastMovingCollectors, fmCollector)
		}
		if instance.GetType() == sapb.InstanceType_NETWEAVER {
			log.Logger.Infow("Creating Netweaver per process CPU, memory usage metrics collector for instance.", "instance", instance)
			netweaverComputeresourcesCollector := &computeresources.NetweaverInstanceProperties{
				Config:      p.Config,
				Client:      p.Client,
				Executor:    commandlineexecutor.ExecuteCommand,
				SAPInstance: instance,
				SAPControlProcessParams: commandlineexecutor.Params{
					User:        instance.GetUser(),
					Executable:  instance.GetSapcontrolPath(),
					ArgsToSplit: fmt.Sprintf("-nr %s -function GetProcessList -format script", instance.GetInstanceNumber()),
					Env:         []string{"LD_LIBRARY_PATH=" + instance.GetLdLibraryPath()},
				},
				ABAPProcessParams: commandlineexecutor.Params{
					User:        instance.GetUser(),
					Executable:  instance.GetSapcontrolPath(),
					ArgsToSplit: fmt.Sprintf("-nr %s -function ABAPGetWPTable", instance.GetInstanceNumber()),
					Env:         []string{"LD_LIBRARY_PATH=" + instance.GetLdLibraryPath()},
				},
				SAPControlClient: sapcontrolclient.New(instance.GetInstanceNumber()),
				LastValue:        make(map[string]*process.IOCountersStat),
				SkippedMetrics:   skippedMetrics,
			}

			log.Logger.Infow("Creating Netweaver collector for instance.", "instance", instance)
			netweaverCollector := &netweaver.InstanceProperties{
				SAPInstance:    instance,
				Config:         p.Config,
				Client:         p.Client,
				SkippedMetrics: skippedMetrics,
			}
			p.Collectors = append(p.Collectors, netweaverComputeresourcesCollector, netweaverCollector)

			log.Logger.Infow("Creating FastMoving Collector for Netweaver", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance:    instance,
				Config:         p.Config,
				Client:         p.Client,
				SkippedMetrics: skippedMetrics,
			}
			p.FastMovingCollectors = append(p.FastMovingCollectors, fmCollector)
		}
	}

	if len(sids) != 0 {
		log.Logger.Info("Creating maintenance mode collector.")
		maintenanceModeCollector := &maintenance.InstanceProperties{
			Config:         p.Config,
			Client:         p.Client,
			Reader:         maintenance.ModeReader{},
			Sids:           sids,
			SkippedMetrics: skippedMetrics,
		}
		p.Collectors = append(p.Collectors, maintenanceModeCollector)
	}

	log.Logger.Infow("Created process metrics collectors.", "numberofcollectors", len(p.Collectors))
	return p
}

// instancesWithCredentials run SAP discovery to detect SAP instances on this machine and update
// DB Credentials from configuration into the instances.
func instancesWithCredentials(ctx context.Context, params *Parameters) *sapb.SAPInstances {
	// For unit tests we do not want to run sap discovery, caller will pass the SAPInstances.
	if params.SAPInstances == nil {
		params.SAPInstances = sapdiscovery.SAPApplications(ctx)
	}
	for _, instance := range params.SAPInstances.GetInstances() {
		if instance.GetType() == sapb.InstanceType_HANA {
			var err error
			projectID := params.Config.GetCloudProperties().GetProjectId()
			hanaConfig := params.Config.GetCollectionConfiguration().GetHanaMetricsConfig()

			instance.HanaDbUser, instance.HanaDbPassword, err = sapdiscovery.ReadHANACredentials(ctx, projectID, hanaConfig, params.GCEService)
			if err != nil {
				log.Logger.Warnw("HANA DB Credentials not set, will not collect HANA DB Query related metrics.", "error", err)
			}
		}
	}
	return params.SAPInstances
}

/*
collectAndSend runs the perpetual collect metrics and send to cloud monitoring workflow.

The collectAndSendFastMovingMetricsOnce workflow is called once every process_metrics_frequency.
The function returns an error if no collectors exist. If any errors
occur during collect or send, they are logged and the workflow continues.

For unit testing, the caller can cancel the context to terminate the workflow.
An exit induced by context cancellation returns the last error seen during
the workflow or nil if no error occurred.
*/
func (p *Properties) collectAndSend(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) error {

	if len(p.Collectors) == 0 {
		return fmt.Errorf("expected non-zero collectors, got: %d", len(p.Collectors))
	}

	cf := p.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()
	collectTicker := time.NewTicker(time.Duration(cf) * time.Second)

	slowProcessMetricsFrequency := p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()
	slowCollectTicker := time.NewTicker(time.Duration(slowProcessMetricsFrequency) * time.Second)
	defer collectTicker.Stop()
	defer slowCollectTicker.Stop()

	heartbeatTicker := p.HeartbeatSpec.CreateTicker()
	defer heartbeatTicker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			log.Logger.Info("Context cancelled, exiting collectAndSend.")
			return lastErr
		case <-heartbeatTicker.C:
			p.HeartbeatSpec.Beat()
		case <-collectTicker.C:
			p.HeartbeatSpec.Beat()
			sent, batchCount, err := p.collectAndSendFastMovingMetricsOnce(ctx, bo)
			if err != nil {
				log.Logger.Errorw("Error sending metrics", "error", err)
				lastErr = err
			}
			log.Logger.Infow("Sent metrics from collectAndSend.", "sent", sent, "batches", batchCount, "sleeping", cf)
		}
	}
}

func (p *Properties) collectAndSendFastMovingMetricsOnce(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) (sent, batchCount int, err error) {
	var wg sync.WaitGroup
	msgs := make([][]*mrpb.TimeSeries, len(p.FastMovingCollectors))
	defer (func() { msgs = nil })() // free up reference in memory.
	log.Logger.Debugw("Starting collectors in parallel.", "numberOfCollectors", len(p.Collectors))

	for i, collector := range p.FastMovingCollectors {
		wg.Add(1)
		go func(slot int, c Collector) {
			defer wg.Done()
			msgs[slot], err = c.CollectWithRetry(ctx) // Each collector writes to its own slot.
			if err != nil {
				log.Logger.Errorw("Error collecting fast moving metrics", "error", err)
			}
			log.Logger.Debugw("Collected metrics", "numberofmetrics", len(msgs[slot]))
		}(i, collector)
	}
	log.Logger.Debug("Waiting for fast moving collectors to finish.")
	wg.Wait()
	return cloudmonitoring.SendTimeSeries(ctx, flatten(msgs), p.Client, bo, p.Config.GetCloudProperties().GetProjectId())
}

func createWorkerPoolForSlowMetrics(ctx context.Context, p *Properties, bo *cloudmonitoring.BackOffIntervals) {
	wp := workerpool.New(len(p.Collectors))
	for _, collector := range p.Collectors {
		collector := collector
		// Since wp.Submit() is non-blocking, the for loop might progress before the
		// task is executed in the workerpool. Creating a copy of Collector outside
		// of Submit() to ensure we copy the collector into the call.
		// Reference: https://go.dev/doc/faq#closures_and_goroutines
		wp.Submit(func() {
			collectAndSendSlowMovingMetrics(ctx, p, collector, bo, wp)
		})
	}
}

func collectAndSendSlowMovingMetrics(ctx context.Context, p *Properties, c Collector, bo *cloudmonitoring.BackOffIntervals, wp *workerpool.WorkerPool) error {
	sent, batchCount, err := collectAndSendSlowMovingMetricsOnce(ctx, p, c, bo)
	log.Logger.Debugw("Sent metrics from collectAndSend.", "sent", sent, "batches", batchCount, "error", err)
	time.AfterFunc(time.Duration(p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency())*time.Second, func() {
		wp.Submit(func() {
			collectAndSendSlowMovingMetrics(ctx, p, c, bo, wp)
		})
	})
	return err
}

func collectAndSendSlowMovingMetricsOnce(ctx context.Context, p *Properties, c Collector, bo *cloudmonitoring.BackOffIntervals) (sent, batchCount int, err error) {
	metrics, err := c.CollectWithRetry(ctx)
	if err != nil {
		return 0, 0, err
	}
	return cloudmonitoring.SendTimeSeries(ctx, metrics, p.Client, bo, p.Config.GetCloudProperties().GetProjectId())
}

// flatten converts an 2D array of metric slices to a flat 1D array of metrics.
func flatten(msgs [][]*mrpb.TimeSeries) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries
	for _, msg := range msgs {
		metrics = append(metrics, msg...)
	}
	return metrics
}
