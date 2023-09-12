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
	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
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
		Config         *cnfpb.Configuration
		Client         cloudmonitoring.TimeSeriesCreator
		Executor       commandlineexecutor.Execute
		NewProcHelper  newProcessWithContextHelper
		SkippedMetrics map[string]bool
		pmbo           *cloudmonitoring.BackOffIntervals
	}
)

// Collect SAP additional metrics like per process CPU and per process memory
// utilization of SAP Control Processes.
func (p *SAPControlProcInstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	params := parameters{
		executor:         p.Executor,
		config:           p.Config,
		client:           p.Client,
		cpuMetricPath:    sapCTRLCPUPath,
		memoryMetricPath: sapCtrlMemoryPath,
		newProc:          p.NewProcHelper,
	}
	processes := collectControlProcesses(ctx, params)
	if len(processes) == 0 {
		log.Logger.Debug("Cannot collect CPU and memory per process for Netweaver, empty process list.")
		return nil, nil
	}
	res := []*mrpb.TimeSeries{}
	if _, ok := p.SkippedMetrics[sapCTRLCPUPath]; !ok {
		cpuMetrics, err := collectCPUPerProcess(ctx, params, processes)
		if err != nil {
			return nil, err
		}
		res = append(res, cpuMetrics...)
	}
	if _, ok := p.SkippedMetrics[sapCtrlMemoryPath]; !ok {
		memoryMetrics, err := collectMemoryPerProcess(ctx, params, processes)
		if err != nil {
			return nil, err
		}
		res = append(res, memoryMetrics...)
	}
	return res, nil
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *SAPControlProcInstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	if p.pmbo == nil {
		p.pmbo = cloudmonitoring.NewDefaultBackOffIntervals()
	}
	err := backoff.Retry(func() error {
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.Logger.Errorw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, cloudmonitoring.LongExponentialBackOffPolicy(ctx, p.pmbo.LongExponential))
	return res, err
}
