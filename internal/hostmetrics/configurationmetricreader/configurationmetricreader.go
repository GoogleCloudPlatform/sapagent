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

/*
Package configurationmetricreader contains utility functions that build collections of protocol
buffers that store various configuration metrics
*/
package configurationmetricreader

import (
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	confpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

/*
ConfigMetricReader for reading configuration metric statistics from the virtual machine
*/
type ConfigMetricReader struct {
	OS string
}

/*
Read returns a collection of metrics read for cpu, disk, network, and memory metrics.
*/
func (r *ConfigMetricReader) Read(config *confpb.Configuration, cpuStats *statspb.CpuStats, instanceProps *iipb.InstanceProperties, at agenttime.AgentTime) *mpb.MetricsCollection {
	mc := &mpb.MetricsCollection{}

	mc.Metrics = []*mpb.Metric{
		&mpb.Metric{
			Name:            "Data Provider Version",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           config.GetAgentProperties().GetVersion(),
		},
		&mpb.Metric{
			Name:            "Cloud Provider",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "Google Cloud Platform",
		},
	}

	// Return the bare metal metrics if the configuration instance is "bare metal".
	if config.GetBareMetal() {
		return r.bareMetalMetrics(mc, cpuStats, at)
	}

	machineType := instanceProps.GetMachineType()
	machineType = machineType[strings.LastIndex(machineType, "/")+1:]

	mc.Metrics = append(mc.Metrics,
		&mpb.Metric{
			Name:            "Instance Type",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           machineType,
		},
		&mpb.Metric{
			Name:            "Virtualization Solution",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "KVM",
		},
		&mpb.Metric{
			Name:            "Virtualization Solution Version",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "N/A",
		},
		&mpb.Metric{
			Name:            "Hardware Manufacturer",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "Google",
		},
		&mpb.Metric{
			Name:            "Hardware Model",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "Google",
		},
		&mpb.Metric{
			Name:            "CPU Over-Provisioning",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "No",
		},
		&mpb.Metric{
			Name:            "Memory Over-Provisioning",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "No",
		},
		&mpb.Metric{
			Name:            "Host Identifier",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           config.GetCloudProperties().GetInstanceId(),
		},
	)

	return mc
}

/*
bareMetalMetrics appends alternative metrics to an existing metrics collections if the current
virtual machine is configured as a "bare metal" instance.
*/
func (r *ConfigMetricReader) bareMetalMetrics(mc *mpb.MetricsCollection, cpuStats *statspb.CpuStats, at agenttime.AgentTime) *mpb.MetricsCollection {
	mc.Metrics = append(mc.Metrics, &mpb.Metric{
		Name:            "Instance Type",
		Context:         mpb.Context_CONTEXT_VM,
		Category:        mpb.Category_CATEGORY_CONFIG,
		Type:            mpb.Type_TYPE_STRING,
		Unit:            mpb.Unit_UNIT_NONE,
		RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
		LastRefresh:     at.Startup().UnixMilli(),
		Value:           "bms-" + strconv.FormatInt(cpuStats.GetCpuCount(), 10),
	},
		&mpb.Metric{
			Name:            "Virtualization Solution",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "N/A",
		},
		&mpb.Metric{
			Name:            "Hardware Manufacturer",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "Google",
		},
		&mpb.Metric{
			Name:            "Hardware Model",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CONFIG,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     at.Startup().UnixMilli(),
			Value:           "Google Cloud Bare Metal",
		})
	return mc
}
