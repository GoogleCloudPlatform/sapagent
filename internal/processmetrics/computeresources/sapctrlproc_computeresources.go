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

package computeresources

import (
	"context"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/cenkalti/backoff/v4"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	sapCTRLCPUPath    = "/sap/control/cpu/utilization"
	sapCtrlMemoryPath = "/sap/control/memory/utilization"
)

type (
	// SAPControlProcInstanceProperties have the required context for collecting metrics for cpu
	// and memory per process for SAPControl processes.
	SAPControlProcInstanceProperties struct {
		Config          *cnfpb.Configuration
		Client          cloudmonitoring.TimeSeriesCreator
		Executor        commandlineexecutor.Execute
		NewProcHelper   NewProcessWithContextHelper
		SkippedMetrics  map[string]bool
		PMBackoffPolicy backoff.BackOffContext
	}
)

// Collect SAP additional metrics like per process CPU and per process memory
// utilization of SAP Control Processes.
// Collect method keeps on collecting all the metrics it can, logs errors if it encounters
// any and returns the collected metrics with the last error encountered while collecting metrics.
func (p *SAPControlProcInstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	params := Parameters{
		executor:         p.Executor,
		Config:           p.Config,
		client:           p.Client,
		cpuMetricPath:    sapCTRLCPUPath,
		memoryMetricPath: sapCtrlMemoryPath,
		NewProc:          p.NewProcHelper,
	}
	processes := collectControlProcesses(ctx, params)
	var metricsCollectionError error
	if len(processes) == 0 {
		log.CtxLogger(ctx).Debug("Cannot collect CPU and memory per process for Netweaver, empty process list.")
		return nil, nil
	}
	res := []*mrpb.TimeSeries{}
	if _, ok := p.SkippedMetrics[sapCTRLCPUPath]; !ok {
		cpuMetrics, err := collectTimeSeriesMetrics(ctx, params, processes, collectCPUMetric)
		if err != nil {
			metricsCollectionError = err
		}
		if cpuMetrics != nil {
			res = append(res, cpuMetrics...)
		}
	}
	if _, ok := p.SkippedMetrics[sapCtrlMemoryPath]; !ok {
		memoryMetrics, err := collectTimeSeriesMetrics(ctx, params, processes, collectMemoryMetric)
		if err != nil {
			metricsCollectionError = err
		}
		if memoryMetrics != nil {
			res = append(res, memoryMetrics...)
		}
	}
	return res, metricsCollectionError
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *SAPControlProcInstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debugw("Context cancelled, exiting CollectWithRetry")
			return nil
		default:
			var err error
			res, err = p.Collect(ctx)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
				attempt++
			}
			return err
		}
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Retry limit exceeded")
	}
	return res, err
}
