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
	"golang.org/x/exp/slices"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/cluster"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/computeresources"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/fastmovingmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/hana"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/hanavolume"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/infra"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/netweaver"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/networkstats"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapservice"
	"github.com/GoogleCloudPlatform/sapagent/internal/recovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	pcm "github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

var (
	dailyMetricsRoutine                     *recovery.RecoverableRoutine
	collectAndSendFastMetricsRoutine        *recovery.RecoverableRoutine
	collectAndSendReliabilityMetricsRoutine *recovery.RecoverableRoutine
	slowMetricsRoutine                      *recovery.RecoverableRoutine
)

type (
	// Collector interface is SAP application specific metric collection logic.
	// This needs to be implemented by application specific modules that want to leverage
	// startMetricGroup functionality.
	Collector interface {
		Collect(context.Context) ([]*mrpb.TimeSeries, error)
		CollectWithRetry(context.Context) ([]*mrpb.TimeSeries, error)
	}

	// discoveryInterface allows for a testable implementation of system discovery.
	discoveryInterface interface {
		GetSAPSystems() []*spb.SapDiscovery
		GetSAPInstances() *sapb.SAPInstances
	}

	// Properties has necessary context for Metrics collection.
	Properties struct {
		Config                *cpb.Configuration
		Client                cloudmonitoring.TimeSeriesCreator
		Collectors            []Collector
		FastMovingCollectors  []Collector
		ReliabilityCollectors []Collector
		HeartbeatSpec         *heartbeat.Spec
	}

	// CreateMetricClient provides an easily testable translation to the cloud monitoring API.
	CreateMetricClient func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error)

	// Parameters has parameters necessary to invoke Start().
	Parameters struct {
		Config         *cpb.Configuration
		OSType         string
		MetricClient   CreateMetricClient
		BackOffs       *cloudmonitoring.BackOffIntervals
		HeartbeatSpec  *heartbeat.Spec
		GCEService     sapdiscovery.GCEInterface
		GCEBetaService infra.GCEBetaInterface
		Discovery      discoveryInterface
		PCMParams      pcm.Parameters
	}
)

const (
	maxTSPerRequest                = 200 // Reference: https://cloud.google.com/monitoring/quotas
	minimumFrequency               = 5
	minimumFrequencyForSlowMoving  = 30
	minimumFrequencyForReliability = 60
)

/*
startProcessMetrics starts collection if collect_process_metrics config option is enabled
in the configuration. The function is a NO-OP if the config option is not enabled.

If the config option is enabled, start the metric collector jobs
in the background and return control to the caller with return value = true.

Return false if the config option is not enabled.
*/
func startProcessMetrics(ctx context.Context, parameters Parameters) bool {
	cpm := parameters.Config.GetCollectionConfiguration().GetCollectProcessMetrics()
	cf := parameters.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()
	slowPmf := parameters.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()

	log.CtxLogger(ctx).Infow("Configuration option for collect_process_metrics", "collectprocessmetrics", cpm)

	switch {
	case !cpm:
		log.CtxLogger(ctx).Info("Not collecting Process Metrics.")
		return false
	case parameters.OSType == "windows":
		log.CtxLogger(ctx).Info("Process Metrics collection is not supported for windows platform.")
		return false
	case cf < minimumFrequency:
		log.CtxLogger(ctx).Infow("Process metrics frequency is smaller than minimum supported value.", "frequency", cf, "minimumfrequency", minimumFrequency)
		log.CtxLogger(ctx).Info("Not collecting Process Metrics.")
		return false
	case slowPmf < minimumFrequencyForSlowMoving:
		log.CtxLogger(ctx).Infow("Slow process metrics frequency is smaller than minimum supported value.", "frequency", slowPmf, "minimumfrequency", minimumFrequencyForSlowMoving)
		log.CtxLogger(ctx).Info("Not collecting Process Metrics.")
		return false
	}

	mc, err := parameters.MetricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring client", "error", err)
		usagemetrics.Error(usagemetrics.ProcessMetricsMetricClientCreateFailure) // Failed to create Cloud Monitoring client
		return false
	}

	sapInstances := instancesWithCredentials(ctx, &parameters)
	if len(sapInstances.GetInstances()) == 0 {
		log.CtxLogger(ctx).Error("No SAP Instances found. Cannot start process metrics collection.")
		usagemetrics.Error(usagemetrics.NoSAPInstancesFound) // NO SAP instances found
		return false
	}

	log.CtxLogger(ctx).Info("Starting process metrics collection in background.")

	dailyMetricsRoutine = &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.CollectProcessMetrics) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)

	p := createProcessCollectors(ctx, parameters, mc, sapInstances)
	collectAndSendFastMetricsRoutine = &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			if parameters, ok := a.(Parameters); ok {
				p.collectAndSendFastMovingMetrics(ctx, parameters.BackOffs)
			}
		},
		RoutineArg:          parameters,
		ErrorCode:           usagemetrics.CollectMetricsRoutineFailure,
		ExpectedMinDuration: time.Minute,
	}
	// NOMUTANTS--will be covered by integration testing
	collectAndSendFastMetricsRoutine.StartRoutine(ctx)

	// createWorkerPool creates a pool of workers to start collecting slow moving metrics in parallel
	// each collector has its own job of collecting and then sending the metrics to cloudmonitoring.
	// So after an error is encountered during metrics collection within a collector, while it retried
	// as per the retry policy, other collectors remain unaffected.
	slowMetricsRoutine = &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			if parameters, ok := a.(Parameters); ok {
				createWorkerPoolForSlowMetrics(ctx, p, parameters.BackOffs)
			}
		},
		RoutineArg:          parameters,
		ErrorCode:           usagemetrics.SlowMetricsCollectionFailure,
		ExpectedMinDuration: time.Minute,
	}
	slowMetricsRoutine.StartRoutine(ctx)

	return true
}

// NewMetricClient is the production version that calls cloud monitoring API.
func NewMetricClient(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
	return monitoring.NewMetricClient(ctx)
}

// createProcessCollectors sets up the processmetrics properties and metric collectors for SAP Instances.
func createProcessCollectors(ctx context.Context, params Parameters, client cloudmonitoring.TimeSeriesCreator, sapInstances *sapb.SAPInstances) *Properties {
	p := &Properties{
		Config:        params.Config,
		Client:        client,
		HeartbeatSpec: params.HeartbeatSpec,
	}

	// For retries logic and backoff policy:
	// For fast moving process metrics we are going ahead with 3 retries on failures, which means 4 attempts in total.
	// Attempt - 1 Failure: wait for 30 seconds
	// Attempt - 2 Failure: wait for 60 seconds
	// Attempt - 3 Failure: wait for 120 seconds

	// For fast moving process metrics we are going ahead with 3 retries on failures, which means 4 attempts in total.
	// Attempt - 1 Failure: wait for 5 seconds
	// Attempt - 2 Failure: wait for 10 seconds
	// Attempt - 3 Failure: wait for 20 seconds
	// Note: There is also randomization factor associated with exponential backoffs the intervals can
	// have a delta of 3-4 seconds, which does not affect the overall process.

	pmSlowFreq := p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()
	pmFastFreq := p.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()

	skippedMetrics := make(map[string]bool)
	skipMetricsForNetweaverKernel(ctx, params.Discovery, skippedMetrics)
	sl := p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip()
	for _, metric := range sl {
		skippedMetrics[metric] = true
	}

	log.CtxLogger(ctx).Info("Creating SAP additional metrics collector for sapservices (active and enabled metric).")
	sapServiceCollector := &sapservice.InstanceProperties{
		Config:          p.Config,
		Client:          p.Client,
		Execute:         commandlineexecutor.ExecuteCommand,
		SkippedMetrics:  skippedMetrics,
		PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
	}

	log.CtxLogger(ctx).Info("Creating SAP control processes per process CPU, memory usage metrics collector.")
	sapStartCollector := &computeresources.SAPControlProcInstanceProperties{
		Config:          p.Config,
		Client:          p.Client,
		Executor:        commandlineexecutor.ExecuteCommand,
		SkippedMetrics:  skippedMetrics,
		PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
	}

	log.CtxLogger(ctx).Info("Creating infra migration event metrics collector.")
	migrationCollector := infra.New(p.Config, p.Client, params.GCEBetaService, skippedMetrics,
		cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute))

	log.CtxLogger(ctx).Info("Creating networkstats metrics collector.")
	networkstatsCollector := &networkstats.Properties{
		Executor:        commandlineexecutor.ExecuteCommand,
		Config:          p.Config,
		Client:          p.Client,
		PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
		SkippedMetrics:  skippedMetrics,
	}

	log.CtxLogger(ctx).Info("Creating volume availability metrics collector.")
	volumeDetailsCollector := &hanavolume.Properties{
		Executor: commandlineexecutor.ExecuteCommand,
		Config:   p.Config,
		Client:   p.Client,
		CommandParams: commandlineexecutor.Params{
			Executable:  "df",
			ArgsToSplit: "-h",
		},
		PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
	}

	p.Collectors = append(
		p.Collectors,
		sapServiceCollector,
		sapStartCollector,
		migrationCollector,
		networkstatsCollector,
		volumeDetailsCollector,
	)

	sids := make(map[string]bool)
	clusterCollectorCreated := false
	for _, instance := range sapInstances.GetInstances() {
		sids[instance.GetSapsid()] = true
		if sapInstances.GetLinuxClusterMember() && clusterCollectorCreated == false {
			log.CtxLogger(ctx).Infow("Creating cluster collector for instance", "instance", instance)
			clusterCollector := &cluster.InstanceProperties{
				SAPInstance:     instance,
				Config:          p.Config,
				Client:          p.Client,
				SkippedMetrics:  skippedMetrics,
				PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
			}
			p.Collectors = append(p.Collectors, clusterCollector)
			clusterCollectorCreated = true
		}
		if instance.GetType() == sapb.InstanceType_HANA {
			log.CtxLogger(ctx).Infow("Creating HANA per process CPU, memory usage metrics collector for instance", "instance", instance)
			hanaComputeresourcesCollector := &computeresources.HanaInstanceProperties{
				Config:           p.Config,
				Client:           p.Client,
				Executor:         commandlineexecutor.ExecuteCommand,
				SAPInstance:      instance,
				SAPControlClient: sapcontrolclient.New(instance.GetInstanceNumber()),
				LastValue:        make(map[string]*process.IOCountersStat),
				SkippedMetrics:   skippedMetrics,
				PMBackoffPolicy:  cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
			}

			log.CtxLogger(ctx).Infow("Creating HANA collector for instance.", "instance", instance)
			hanaCollector := &hana.InstanceProperties{
				SAPInstance:        instance,
				Config:             p.Config,
				Client:             p.Client,
				HANAQueryFailCount: 0,
				SkippedMetrics:     skippedMetrics,
				PMBackoffPolicy:    cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
			}
			p.Collectors = append(p.Collectors, hanaComputeresourcesCollector, hanaCollector)

			log.CtxLogger(ctx).Infow("Creating FastMoving Collector for HANA", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance:     instance,
				Config:          p.Config,
				Client:          p.Client,
				PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmFastFreq)*time.Second, 3, time.Minute, 35*time.Second),
			}
			p.FastMovingCollectors = append(p.FastMovingCollectors, fmCollector)
		}
		if instance.GetType() == sapb.InstanceType_NETWEAVER {
			log.CtxLogger(ctx).Infow("Creating Netweaver per process CPU, memory usage metrics collector for instance.", "instance", instance)
			netweaverComputeresourcesCollector := &computeresources.NetweaverInstanceProperties{
				Config:           p.Config,
				Client:           p.Client,
				Executor:         commandlineexecutor.ExecuteCommand,
				SAPInstance:      instance,
				SAPControlClient: sapcontrolclient.New(instance.GetInstanceNumber()),
				LastValue:        make(map[string]*process.IOCountersStat),
				SkippedMetrics:   skippedMetrics,
				PMBackoffPolicy:  cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
			}

			log.CtxLogger(ctx).Infow("Creating Netweaver collector for instance.", "instance", instance)
			netweaverCollector := &netweaver.InstanceProperties{
				SAPInstance:     instance,
				Config:          p.Config,
				Client:          p.Client,
				SkippedMetrics:  skippedMetrics,
				PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
			}
			p.Collectors = append(p.Collectors, netweaverComputeresourcesCollector, netweaverCollector)

			log.CtxLogger(ctx).Infow("Creating FastMoving Collector for Netweaver", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance:     instance,
				Config:          p.Config,
				Client:          p.Client,
				SkippedMetrics:  skippedMetrics,
				PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmFastFreq)*time.Second, 3, time.Minute, 35*time.Second),
			}
			p.FastMovingCollectors = append(p.FastMovingCollectors, fmCollector)
		}
	}

	if len(sids) != 0 {
		log.CtxLogger(ctx).Info("Creating maintenance mode collector.")
		maintenanceModeCollector := &maintenance.InstanceProperties{
			Config:          p.Config,
			Client:          p.Client,
			Reader:          maintenance.ModeReader{},
			Sids:            sids,
			SkippedMetrics:  skippedMetrics,
			PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
		}
		p.Collectors = append(p.Collectors, maintenanceModeCollector)

		if params.PCMParams.WorkloadConfig == nil {
			log.CtxLogger(ctx).Debug("Cannot collect pacemaker metrics, no collection definition detected.")
		} else {
			log.CtxLogger(ctx).Debug("Creating pacemaker metrics collector.")
			pacemakerCollector := &pacemaker.InstanceProperties{
				Config:          p.Config,
				Client:          p.Client,
				Sids:            sids,
				SkippedMetrics:  skippedMetrics,
				PMBackoffPolicy: cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(pmSlowFreq)*time.Second, 3, 3*time.Minute, 2*time.Minute),
				PacemakerCollector: &pacemaker.Params{
					PCMParams: params.PCMParams,
				},
			}
			p.Collectors = append(p.Collectors, pacemakerCollector)
		}
	}

	log.CtxLogger(ctx).Infow("Created process metrics collectors.", "numberofcollectors", len(p.Collectors))
	return p
}

// instancesWithCredentials uses the results from SAP discovery to detect
// SAP instances on this machine and update DB Credentials from configuration
// into the instances.
func instancesWithCredentials(ctx context.Context, params *Parameters) *sapb.SAPInstances {
	sapInstances := params.Discovery.GetSAPInstances()
	for _, instance := range sapInstances.GetInstances() {
		if instance.GetType() == sapb.InstanceType_HANA {
			var err error
			projectID := params.Config.GetCloudProperties().GetProjectId()
			hanaConfig := params.Config.GetCollectionConfiguration().GetHanaMetricsConfig()

			instance.HanaDbUser, instance.HanaDbPassword, err = sapdiscovery.ReadHANACredentials(ctx, projectID, hanaConfig, params.GCEService)
			if err != nil {
				log.CtxLogger(ctx).Infow("HANA DB Credentials not set, will not collect HANA DB Query related metrics.", "error", err)
			}
		}
	}
	return sapInstances
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
func (p *Properties) collectAndSendFastMovingMetrics(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) error {

	if len(p.Collectors) == 0 {
		return fmt.Errorf("expected non-zero collectors, got: %d", len(p.Collectors))
	}

	cf := p.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()
	collectTicker := time.NewTicker(time.Duration(cf) * time.Second)
	defer collectTicker.Stop()

	heartbeatTicker := p.HeartbeatSpec.CreateTicker()
	defer heartbeatTicker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("Process metrics context cancelled, exiting collectAndSend.")
			return lastErr
		case <-heartbeatTicker.C:
			p.HeartbeatSpec.Beat()
		case <-collectTicker.C:
			p.HeartbeatSpec.Beat()
			sent, batchCount, err := p.collectAndSendFastMovingMetricsOnce(ctx, bo)
			if err != nil {
				log.CtxLogger(ctx).Errorw("Error sending process metrics", "error", err)
				lastErr = err
			}
			log.CtxLogger(ctx).Infow("Sent process metrics from collectAndSendFastMovingMetrics.", "sent", sent, "batches", batchCount, "sleeping", cf)
		}
	}
}

type collectFastMetricsRoutineArgs struct {
	c    Collector
	slot int
}

func (p *Properties) collectAndSendFastMovingMetricsOnce(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) (sent, batchCount int, err error) {
	var wg sync.WaitGroup
	msgs := make([][]*mrpb.TimeSeries, len(p.FastMovingCollectors))
	defer (func() { msgs = nil })() // free up reference in memory.
	log.CtxLogger(ctx).Debugw("Starting collectors in parallel.", "numberOfCollectors", len(p.Collectors))
	var routines []*recovery.RecoverableRoutine
	for i, collector := range p.FastMovingCollectors {
		wg.Add(1)
		r := &recovery.RecoverableRoutine{
			Routine: func(ctx context.Context, a any) {
				defer wg.Done()
				if args, ok := a.(collectFastMetricsRoutineArgs); ok {
					var err error
					msgs[args.slot], err = args.c.CollectWithRetry(ctx) // Each collector writes to its own slot.
					if err != nil {
						log.CtxLogger(ctx).Debugw("Error collecting fast moving metrics", "error", err)
					}
					log.CtxLogger(ctx).Debugw("Collected fast moving metrics", "numberofmetrics", len(msgs[args.slot]))
				}
			},
			RoutineArg:          collectFastMetricsRoutineArgs{c: collector, slot: i},
			ErrorCode:           usagemetrics.CollectFastMetrcsRoutineFailure,
			ExpectedMinDuration: time.Second,
		}
		routines = append(routines, r)
		r.StartRoutine(ctx)
	}
	log.CtxLogger(ctx).Debug("Waiting for fast moving collectors to finish.")
	wg.Wait()
	return cloudmonitoring.SendTimeSeries(ctx, flatten(msgs), p.Client, bo, p.Config.GetCloudProperties().GetProjectId())
}

/*
Start starts the collection of relevant metrics based on whether the
collect_reliability_metrics or collect_process_metrics is enabled in the
configuration. The function is a NO-OP if both config options are disabled".

If the config option is enabled, start the metric collector jobs
in the background and return control to the caller with return value = true.

Return false if the config option is not enabled.
*/
func Start(ctx context.Context, parameters Parameters) bool {
	rm := startReliabilityMetrics(ctx, parameters)
	pm := startProcessMetrics(ctx, parameters)
	return rm || pm
}

/*
startReliabilityMetrics starts collection if collect_reliability_metrics config option is enabled
in the configuration. The function is a NO-OP if the config option is not enabled.

If the config option is enabled, start the metric collector jobs
in the background and return control to the caller with return value = true.

Return false if the config option is not enabled.
*/
func startReliabilityMetrics(ctx context.Context, parameters Parameters) bool {
	crm := parameters.Config.GetCollectionConfiguration().GetCollectReliabilityMetrics().GetValue()
	cf := parameters.Config.GetCollectionConfiguration().GetReliabilityMetricsFrequency()

	log.CtxLogger(ctx).Debugw("Configuration option for collect_reliability_metrics", "collectreliabilitymetrics", crm)
	switch {
	case !crm:
		log.CtxLogger(ctx).Info("Not collecting Reliability Metrics.")
		return false
	case cf < minimumFrequencyForReliability:
		log.CtxLogger(ctx).Infow("Reliability metrics frequency is smaller than minimum supported value.", "frequency", cf, "minimumfrequency", minimumFrequencyForReliability)
		log.CtxLogger(ctx).Infow("Setting reliability metrics collection frequency to default value.", "frequency", minimumFrequencyForReliability)
		parameters.Config.CollectionConfiguration.ReliabilityMetricsFrequency = minimumFrequencyForReliability
	}

	mc, err := parameters.MetricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring client", "error", err)
		usagemetrics.Error(usagemetrics.ProcessMetricsMetricClientCreateFailure) // Failed to create Cloud Monitoring client
		return false
	}

	sapInstances := instancesWithCredentials(ctx, &parameters)
	if len(sapInstances.GetInstances()) == 0 {
		log.CtxLogger(ctx).Error("No SAP Instances found. Cannot start reliability metrics collection.")
		usagemetrics.Error(usagemetrics.NoSAPInstancesFound) // NO SAP instances found
		return false
	}

	dailyMetricsRoutine = &recovery.RecoverableRoutine{
		Routine:             func(context.Context, any) { usagemetrics.LogActionDaily(usagemetrics.CollectReliabilityMetrics) },
		RoutineArg:          nil,
		ErrorCode:           usagemetrics.UsageMetricsDailyLogError,
		ExpectedMinDuration: 24 * time.Hour,
	}
	dailyMetricsRoutine.StartRoutine(ctx)

	p := createReliabilityCollectors(ctx, parameters, mc, sapInstances)

	log.CtxLogger(ctx).Info("Starting reliability metrics collection in background.")
	collectAndSendReliabilityMetricsRoutine = &recovery.RecoverableRoutine{
		Routine: func(ctx context.Context, a any) {
			if parameters, ok := a.(Parameters); ok {
				p.collectAndSendReliabilityMetrics(ctx, parameters.BackOffs)
			}
		},
		RoutineArg:          parameters,
		ErrorCode:           usagemetrics.CollectMetricsRoutineFailure,
		ExpectedMinDuration: 5 * time.Minute,
	}
	collectAndSendReliabilityMetricsRoutine.StartRoutine(ctx)
	return true
}

// createReliabilityCollectors sets up the processmetrics properties and metric collectors for SAP Instances.
func createReliabilityCollectors(ctx context.Context, params Parameters, client cloudmonitoring.TimeSeriesCreator, sapInstances *sapb.SAPInstances) *Properties {
	p := &Properties{
		Config:        params.Config,
		Client:        client,
		HeartbeatSpec: params.HeartbeatSpec,
	}

	// For retries logic and backoff policy: 3 retries on failures, which means 4 attempts in total.
	// Attempt - 1 Failure: wait for 30 seconds
	// Attempt - 2 Failure: wait for 60 seconds
	// Attempt - 3 Failure: wait for 120 seconds

	rmFreq := p.Config.GetCollectionConfiguration().GetReliabilityMetricsFrequency()

	for _, instance := range sapInstances.GetInstances() {
		if instance.GetType() == sapb.InstanceType_HANA {
			log.CtxLogger(ctx).Infow("Creating reliability metrics collector for HANA", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance:       instance,
				Config:            p.Config,
				Client:            p.Client,
				PMBackoffPolicy:   cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(rmFreq)*time.Second, 3, time.Minute, 35*time.Second),
				ReliabilityMetric: true,
			}
			p.ReliabilityCollectors = append(p.ReliabilityCollectors, fmCollector)
		}
		if instance.GetType() == sapb.InstanceType_NETWEAVER {
			log.CtxLogger(ctx).Infow("Creating reliability metrics collector for Netweaver", "instance", instance)
			fmCollector := &fastmovingmetrics.InstanceProperties{
				SAPInstance:       instance,
				Config:            p.Config,
				Client:            p.Client,
				PMBackoffPolicy:   cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(rmFreq)*time.Second, 3, time.Minute, 35*time.Second),
				ReliabilityMetric: true,
			}
			p.ReliabilityCollectors = append(p.ReliabilityCollectors, fmCollector)
		}
	}

	log.CtxLogger(ctx).Infow("Created reliability metrics collectors", "numberofcollectors", len(p.ReliabilityCollectors))
	return p
}

func (p *Properties) collectAndSendReliabilityMetrics(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) error {

	if len(p.ReliabilityCollectors) == 0 {
		return fmt.Errorf("expected non-zero collectors, got: %d", len(p.Collectors))
	}

	rmf := p.Config.GetCollectionConfiguration().GetReliabilityMetricsFrequency()
	reliabilityCollectTicker := time.NewTicker(time.Duration(rmf) * time.Second)
	defer reliabilityCollectTicker.Stop()

	heartbeatTicker := p.HeartbeatSpec.CreateTicker()
	defer heartbeatTicker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("Process metrics context cancelled, exiting collectAndSend.")
			return lastErr
		case <-heartbeatTicker.C:
			p.HeartbeatSpec.Beat()
		case <-reliabilityCollectTicker.C:
			p.HeartbeatSpec.Beat()
			sent, batchCount, err := p.collectAndSendReliabilityMetricsOnce(ctx, bo)
			if err != nil {
				log.CtxLogger(ctx).Errorw("Error sending reliability metrics", "error", err)
				lastErr = err
			}
			log.CtxLogger(ctx).Infow("Sent reliability metrics from collectAndSend.", "sent", sent, "batches", batchCount, "sleeping", rmf)
		}
	}
}

type collectReliabilityMetricsRoutineArgs struct {
	c    Collector
	slot int
}

// collectAndSendReliabilityMetricsOnce collects and sends metrics from reliability collectors.
func (p *Properties) collectAndSendReliabilityMetricsOnce(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) (sent, batchCount int, err error) {
	var wg sync.WaitGroup
	msgs := make([][]*mrpb.TimeSeries, len(p.ReliabilityCollectors))
	defer (func() { msgs = nil })() // free up reference in memory.
	log.CtxLogger(ctx).Debugw("Starting collectors in parallel.", "numberOfCollectors", len(p.ReliabilityCollectors))

	var routines []*recovery.RecoverableRoutine
	for i, collector := range p.ReliabilityCollectors {
		wg.Add(1)
		r := &recovery.RecoverableRoutine{
			Routine: func(ctx context.Context, a any) {
				defer wg.Done()
				if args, ok := a.(collectReliabilityMetricsRoutineArgs); ok {
					var err error
					msgs[args.slot], err = args.c.CollectWithRetry(ctx) // Each collector writes to its own slot.
					if err != nil {
						log.CtxLogger(ctx).Debugw("Error collecting reliability metrics", "error", err)
					}
					log.CtxLogger(ctx).Debugw("Collected relaibility metrics", "numberofmetrics", len(msgs[args.slot]))
				}
			},
			RoutineArg:          collectReliabilityMetricsRoutineArgs{c: collector, slot: i},
			ErrorCode:           usagemetrics.CollectReliabilityMetricsRoutineFailure,
			ExpectedMinDuration: time.Second,
		}
		routines = append(routines, r)
		r.StartRoutine(ctx)
	}
	log.CtxLogger(ctx).Debug("Waiting for reliability collectors to finish.", "numberOfCollectors", len(routines))
	wg.Wait()
	return cloudmonitoring.SendTimeSeries(ctx, flatten(msgs), p.Client, bo, p.Config.GetCloudProperties().GetProjectId())
}

func createWorkerPoolForSlowMetrics(ctx context.Context, p *Properties, bo *cloudmonitoring.BackOffIntervals) {
	log.CtxLogger(ctx).Infow("Creating worker pool for slow metrics.", "numberofCollectors", len(p.Collectors))
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
	log.CtxLogger(ctx).Infow("Sent metrics from collectAndSendSlowMovingMetrics.", "sent", sent, "batches", batchCount, "error", err)
	time.AfterFunc(time.Duration(p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency())*time.Second, func() {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("Process metrics context cancelled, exiting collectAndSendSlowMovingMetrics.")
			return
		default:
			wp.Submit(func() {
				collectAndSendSlowMovingMetrics(ctx, p, c, bo, wp)
			})
		}
	})
	return err
}

func collectAndSendSlowMovingMetricsOnce(ctx context.Context, p *Properties, c Collector, bo *cloudmonitoring.BackOffIntervals) (sent, batchCount int, err error) {
	metrics, err := c.CollectWithRetry(ctx)
	if err != nil && len(metrics) == 0 {
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

// skipMetricsForNetweaverKernel checks the kernel version of the netweaver system and skips the metrics
// if the kernel version is affected by SAP Note: https://me.sap.com/notes/3366597/E.
func skipMetricsForNetweaverKernel(ctx context.Context, discovery discoveryInterface, sm map[string]bool) {
	affectedKernelVersions := []string{
		"SAP Kernel 794 Patch 003",
		"SAP Kernel 753 Patch 1224",
		"SAP Kernel 777 Patch 615",
		"SAP Kernel 789 Patch 211",
		"SAP Kernel 754 Patch 220",
		"SAP Kernel 791 Patch 041",
		"SAP Kernel 792 Patch 025",
		"SAP Kernel 785 Patch 313",
		"SAP Kernel 793 Patch 060",
	}
	if discovery == nil {
		return
	}
	for _, system := range discovery.GetSAPSystems() {
		kv := system.GetApplicationLayer().GetApplicationProperties().GetKernelVersion()
		if slices.Contains(affectedKernelVersions, kv) {
			log.CtxLogger(ctx).Infow("The netweaver kernel does not have fix for SAP Note:3366597. Skipping metrics: /sap/nw/abap/sessions and /sap/nw/abap/rfc.", "KernelVersion", kv)
			sm["/sap/nw/abap/sessions"] = true
			sm["/sap/nw/abap/rfc"] = true
			break
		}
	}
}
