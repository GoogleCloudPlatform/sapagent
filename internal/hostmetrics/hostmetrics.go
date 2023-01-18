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
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/configurationmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cpustatsreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/diskstatsreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/memorymetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/osmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
)

// Parameters holds the paramters for all of the Collect* function calls.
type Parameters struct {
	Config             *cpb.Configuration
	InstanceInfoReader instanceinfo.Reader
	CloudMetricReader  cloudmetricreader.CloudMetricReader
	AgentTime          agenttime.AgentTime
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
		usagemetrics.Error(2) // Could not create HTTP listener
		log.Logger.Fatal("Could not start HTTP server on localhost:18181", log.Error(err))
	}
	log.Logger.Info("HTTP server listening on localhost:18181 for SAP Host Agent connections")
}

// collectHostMetrics continuously collects metrics for the SAP Host Agent.
func collectHostMetrics(ctx context.Context, params Parameters) {
	configmr := &configurationmetricreader.ConfigMetricReader{runtime.GOOS}
	cpusr := cpustatsreader.New(runtime.GOOS, os.ReadFile, commandlineexecutor.ExecuteCommand)
	mmr := &memorymetricreader.MemoryMetricReader{runtime.GOOS}
	dsr := diskstatsreader.New()

	for {
		log.Logger.Info("Collecting host metrics...")
		usagemetrics.Action(2) // Collecting SAP host metrics

		params.InstanceInfoReader.Read(params.Config, instanceinfo.NetworkInterfaceAddressMap)
		cpuStats := cpusr.Read()
		diskStats := dsr.Read(params.InstanceInfoReader.InstanceProperties())
		memoryStats := mmr.MemoryStats()
		params.AgentTime.UpdateRefreshTimes()

		var allMetrics []*mpb.Metric

		cloudMetrics := params.CloudMetricReader.Read(ctx, params.Config, params.InstanceInfoReader.InstanceProperties(), params.AgentTime)
		allMetrics = append(allMetrics, cloudMetrics.GetMetrics()...)

		osMetrics := osmetricreader.Read(cpuStats, params.InstanceInfoReader.InstanceProperties(), memoryStats, diskStats, params.AgentTime)
		allMetrics = append(allMetrics, osMetrics.GetMetrics()...)

		configMetrics := configmr.Read(params.Config, cpuStats, params.InstanceInfoReader.InstanceProperties(), params.AgentTime)
		allMetrics = append(allMetrics, configMetrics.GetMetrics()...)

		metricsCollection := &mpb.MetricsCollection{Metrics: allMetrics}
		metricsXML = GenerateXML(metricsCollection)

		log.Logger.Infof("Metrics collection complete, collected %d metrics.", len(metricsCollection.GetMetrics()))
		time.Sleep(60 * time.Second)
	}
}
