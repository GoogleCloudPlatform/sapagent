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

// Package hostmetrics provides Google Cloud VM metric information for the SAP host agent to collect.
package hostmetrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/configurationmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cpustatsreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/diskstatsreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/memorymetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/osmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/recovery"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
)

// Parameters holds the parameters for all of the Collect* function calls.
type Parameters struct {
	Config             *cpb.Configuration
	InstanceInfoReader instanceinfo.Reader
	CloudMetricReader  cloudmetricreader.CloudMetricReader
	AgentTime          agenttime.AgentTime
	HeartbeatSpec      *heartbeat.Spec
}

type hostMetricsReaders struct {
	configmr *configurationmetricreader.ConfigMetricReader
	cpusr    *cpustatsreader.Reader
	mmr      *memorymetricreader.Reader
	dsr      *diskstatsreader.Reader
}

var (
	metricsXML string = "<metrics></metrics>"

	httpServerRoutine         *recovery.RecoverableRoutine
	collectHostMetricsRoutine *recovery.RecoverableRoutine
	dailyUsageRoutineStarted  sync.Once
)

func requestHandler(w http.ResponseWriter, r *http.Request) {
	log.Logger.Debug("Writing metrics XML response")
	fmt.Fprint(w, metricsXML)
}

// StartSAPHostAgentProvider will startup the http server and collect metrics for the sap host agent
// if enabled in the configuration. Returns true if the collection goroutine is started, and false otherwise.
func StartSAPHostAgentProvider(ctx context.Context, cancel context.CancelFunc, restarting bool, params Parameters) bool {
	if !params.Config.GetProvideSapHostAgentMetrics().GetValue() {
		log.CtxLogger(ctx).Info("Not providing SAP Host Agent metrics")
		return false
	}
	// This routine runs forever (without respecting ctx cancellation) and does not need to be restarted.
	httpServerRoutine = &recovery.RecoverableRoutine{
		Routine:             runHTTPServer,
		RoutineArg:          cancel,
		ErrorCode:           usagemetrics.HostMetricsHTTPServerRoutineFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: time.Minute,
	}
	if restarting {
		log.CtxLogger(ctx).Debug("Not starting HTTP server routine as it is already running")
	} else {
		log.CtxLogger(ctx).Debug("Starting HTTP server routine")
		httpServerRoutine.StartRoutine(ctx)
	}

	collectHostMetricsRoutine = &recovery.RecoverableRoutine{
		Routine:             collectHostMetrics,
		RoutineArg:          params,
		ErrorCode:           usagemetrics.HostMetricsCollectionRoutineFailure,
		UsageLogger:         *usagemetrics.Logger,
		ExpectedMinDuration: time.Minute,
	}
	collectHostMetricsRoutine.StartRoutine(ctx)

	return true
}

// runHTTPServer starts an HTTP server on localhost:18181 that stays alive forever.
func runHTTPServer(ctx context.Context, a any) {
	var cancel context.CancelFunc
	if v, ok := a.(context.CancelFunc); ok {
		cancel = v
	} else {
		log.CtxLogger(ctx).Info("Host Metrics Context arg is not a context.CancelFunc")
		return
	}
	http.HandleFunc("/", requestHandler)
	if err := http.ListenAndServe("localhost:18181", nil); err != nil {
		usagemetrics.Error(usagemetrics.LocalHTTPListenerCreateFailure) // Could not create HTTP listener
		log.CtxLogger(ctx).Errorw("Could not start HTTP server on localhost:18181", "error", log.Error(err))
		log.CtxLogger(ctx).Info("Cancelling Host Metrics Context")
		cancel()
		return
	}
	log.CtxLogger(ctx).Info("HTTP server listening on localhost:18181 for SAP Host Agent connections")
}

// collectHostMetrics continuously collects metrics for the SAP Host Agent.
func collectHostMetrics(ctx context.Context, a any) {
	var params Parameters
	if v, ok := a.(Parameters); ok {
		params = v
	} else {
		log.CtxLogger(ctx).Info("Host Metrics Parameters arg is not a Parameters")
		return
	}

	readers := hostMetricsReaders{
		configmr: &configurationmetricreader.ConfigMetricReader{OS: runtime.GOOS},
		cpusr:    cpustatsreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
		mmr:      memorymetricreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
		dsr:      diskstatsreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
	}

	collectTicker := time.NewTicker(60 * time.Second)
	heartbeatTicker := params.HeartbeatSpec.CreateTicker()
	defer collectTicker.Stop()
	defer heartbeatTicker.Stop()

	dailyUsageRoutineStarted.Do(func() {
		// LogActionDaily never returns -only sleeps for 24 and then executes the provided function.
		go usagemetrics.LogActionDaily(usagemetrics.CollectHostMetrics)
	})

	// Do not wait for the first 60s tick and start collection immediately
	select {
	case <-ctx.Done():
		log.CtxLogger(ctx).Info("Host metrics cancellation requested")
		return
	default:
		collectHostMetricsOnce(ctx, params, readers)
	}

	// Start the daemon to collect once every 60s till context is canceled
	for {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Info("Host metrics cancellation requested")
			return
		case <-heartbeatTicker.C:
			params.HeartbeatSpec.Beat()
		case <-collectTicker.C:
			collectHostMetricsOnce(ctx, params, readers)
		}
	}
}

func collectHostMetricsOnce(ctx context.Context, params Parameters, readers hostMetricsReaders) {
	log.CtxLogger(ctx).Info("Collecting host metrics...")
	params.HeartbeatSpec.Beat()

	params.InstanceInfoReader.Read(ctx, params.Config, instanceinfo.NetworkInterfaceAddressMap)
	cpuStats := readers.cpusr.Read(ctx)
	diskStats := readers.dsr.Read(ctx, params.InstanceInfoReader.InstanceProperties())
	memoryStats := readers.mmr.MemoryStats(ctx)
	params.AgentTime.UpdateRefreshTimes()

	var allMetrics []*mpb.Metric

	cloudMetrics := params.CloudMetricReader.Read(ctx, params.Config, params.InstanceInfoReader.InstanceProperties(), params.AgentTime)
	allMetrics = append(allMetrics, cloudMetrics.GetMetrics()...)

	osMetrics := osmetricreader.Read(cpuStats, params.InstanceInfoReader.InstanceProperties(), memoryStats, diskStats, params.AgentTime)
	allMetrics = append(allMetrics, osMetrics.GetMetrics()...)

	configMetrics := readers.configmr.Read(params.Config, cpuStats, params.InstanceInfoReader.InstanceProperties(), params.AgentTime)
	allMetrics = append(allMetrics, configMetrics.GetMetrics()...)

	metricsCollection := &mpb.MetricsCollection{Metrics: allMetrics}
	metricsXML = GenerateXML(metricsCollection)

	log.CtxLogger(ctx).Infow("Metrics collection complete", "metricscollected", len(metricsCollection.GetMetrics()))
}
