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
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	nwCPUPath       = "/sap/nw/cpu/utilization"
	nwMemoryPath    = "/sap/nw/memory/utilization"
	nwIOPSReadsPath = "/sap/nw/iops/reads"
	nwIOPSWritePath = "/sap/nw/iops/writes"
)

type (
	// NetweaverInstanceProperties have the required context for collecting metrics for cpu and
	// memory per process for Netweaver.
	NetweaverInstanceProperties struct {
		Config                  *cnfpb.Configuration
		Client                  cloudmonitoring.TimeSeriesCreator
		Executor                commandlineexecutor.Execute
		SAPInstance             *sapb.SAPInstance
		NewProcHelper           newProcessWithContextHelper
		SAPControlProcessParams commandlineexecutor.Params
		ABAPProcessParams       commandlineexecutor.Params
		SAPControlClient        sapcontrol.ClientInterface
		UseSAPControlAPI        bool
		LastValue               map[string]*process.IOCountersStat
	}
)

// Collect SAP additional metrics like per process CPU and per process memory
// utilization of SAP Netweaver processes.
func (p *NetweaverInstanceProperties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	params := parameters{
		executor:             p.Executor,
		client:               p.Client,
		config:               p.Config,
		memoryMetricPath:     nwMemoryPath,
		cpuMetricPath:        nwCPUPath,
		iopsReadsMetricPath:  nwIOPSReadsPath,
		iopsWritesMetricPath: nwIOPSWritePath,
		lastValue:            p.LastValue,
		sapInstance:          p.SAPInstance,
		newProc:              p.NewProcHelper,
		getProcessListParams: p.SAPControlProcessParams,
		getABAPWPTableParams: p.ABAPProcessParams,
		SAPControlClient:     p.SAPControlClient,
		useSAPControlAPI:     p.UseSAPControlAPI,
	}
	processes := collectProcessesForInstance(ctx, params)
	if len(processes) == 0 {
		log.Logger.Debug("cannot collect CPU and memory per process for Netweaver, empty process list.")
		return nil
	}
	res := collectCPUPerProcess(ctx, params, processes)
	res = append(res, collectMemoryPerProcess(ctx, params, processes)...)
	res = append(res, collectIOPSPerProcess(ctx, params, processes)...)
	return res
}
