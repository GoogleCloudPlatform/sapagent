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
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
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
	"google3/third_party/sapagent/shared/log"

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

var metricsXML string = "<metrics></metrics>"

func requestHandler(w http.ResponseWriter, r *http.Request) {
	log.Logger.Debug("Writing metrics XML response")
	fmt.Fprint(w, metricsXML)
}

// StartSAPHostAgentProvider will startup the http server and collect metrics for the sap host agent
// if enabled in the configuration. Returns true if the collection goroutine is started, and false otherwise.
func StartSAPHostAgentProvider(ctx context.Context, params Parameters) bool {
	if !params.Config.GetProvideSapHostAgentMetrics() {
		log.Logger.Info("Not providing SAP Host Agent metrics")
		return false
	}
	log.Logger.Info("Starting provider for SAP Host Agent metrics")
	go runHTTPServer()
	go collectHostMetrics(ctx, params)
	return true
}

// runHTTPServer starts an HTTP server on localhost:18181 that stays alive forever.
func runHTTPServer() {
	http.HandleFunc("/", requestHandler)
	if err := http.ListenAndServe("localhost:18181", nil); err != nil {
		usagemetrics.Error(usagemetrics.LocalHTTPListenerCreateFailure) // Could not create HTTP listener
		log.Logger.Fatalw("Could not start HTTP server on localhost:18181", log.Error(err))
	}
	log.Logger.Info("HTTP server listening on localhost:18181 for SAP Host Agent connections")
}

// collectHostMetrics continuously collects metrics for the SAP Host Agent.
func collectHostMetrics(ctx context.Context, params Parameters) {

	readers := hostMetricsReaders{
		configmr: &configurationmetricreader.ConfigMetricReader{runtime.GOOS},
		cpusr:    cpustatsreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
		mmr:      memorymetricreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
		dsr:      diskstatsreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand),
	}

	collectTicker := time.NewTicker(60 * time.Second)
	heartbeatTicker := params.HeartbeatSpec.CreateTicker()
	defer collectTicker.Stop()
	defer heartbeatTicker.Stop()

	go usagemetrics.LogActionDaily(usagemetrics.CollectHostMetrics)
	// Do not wait for the first 60s tick and start collection immediately
	select {
	case <-ctx.Done():
		log.Logger.Info("cancellation requested")
		return
	default:
		collectHostMetricsOnce(ctx, params, readers)
	}

	// Start the daemon to collect once every 60s till context is canceled
	for {
		select {
		case <-ctx.Done():
			log.Logger.Info("cancellation requested")
			return
		case <-heartbeatTicker.C:
			params.HeartbeatSpec.Beat()
		case <-collectTicker.C:
			collectHostMetricsOnce(ctx, params, readers)
		}
	}
}

func collectHostMetricsOnce(ctx context.Context, params Parameters, readers hostMetricsReaders) {
	log.Logger.Info("Collecting host metrics...")
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

	log.Logger.Infow("Metrics collection complete", "metricscollected", len(metricsCollection.GetMetrics()))
}
