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
Package osmetricreader contains utlity functions that build collections of protocol buffers that
store various VM metrics
*/
package osmetricreader

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

/*
min compares two 64 bit integers and returns whichever one is smaller.
*/
func min(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

/*
Read returns a collection of metrics read for cpu, disk, network, and memory metrics.
*/
func Read(cpuStats *statspb.CpuStats, instanceProps *iipb.InstanceProperties, memStats *statspb.MemoryStats, diskStats *statspb.DiskStatsCollection, at agenttime.AgentTime) *mpb.MetricsCollection {
	log.Logger.Debug("Getting OS metrics...")
	var metrics []*mpb.Metric

	metrics = append(metrics, memoryMetrics(memStats, at.Startup().UnixMilli())...)
	metrics = append(metrics, diskMetrics(diskStats, instanceProps, at.Startup().UnixMilli(), at.LocalRefresh().UnixMilli())...)
	metrics = append(metrics, networkMetrics(cpuStats, instanceProps, at.Startup().UnixMilli())...)
	metrics = append(metrics, cpuMetrics(cpuStats, instanceProps, at.Startup().UnixMilli())...)
	return &mpb.MetricsCollection{Metrics: metrics}
}

/*
memoryMetrics returns a slice of memory metrics from the OS - total memory available and the
percentage that is consumed.
*/
func memoryMetrics(memoryStats *statspb.MemoryStats, startTime int64) []*mpb.Metric {
	// Memory Consumption defaults to "0.0" instead of unavailable.
	consumption := float64(0)
	if memoryStats.GetUsed() > 0 && memoryStats.GetTotal() > 0 {
		consumption = float64(memoryStats.GetUsed()) / float64(memoryStats.GetTotal())
		consumption = metricsformatter.ToPercentage(consumption, 3)
	}

	return []*mpb.Metric{
		&mpb.Metric{
			Name:            "Guaranteed Memory assigned",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_MEMORY,
			Type:            mpb.Type_TYPE_INT64,
			Unit:            mpb.Unit_UNIT_MB,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           strconv.FormatInt(memoryStats.GetTotal(), 10),
		},
		&mpb.Metric{
			Name:            "Memory Consumption",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_MEMORY,
			Type:            mpb.Type_TYPE_DOUBLE,
			Unit:            mpb.Unit_UNIT_PERCENT,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
			LastRefresh:     startTime,
			Value:           strconv.FormatFloat(consumption, 'f', 1, 64),
		},
	}
}

func formattedDeviceType(diskType string) string {
	t := strings.Replace(diskType, "-", "_", -1)
	return strings.ToUpper(t)
}

/*
diskMetrics returns a slice of disk metrics from the OS.
*/
func diskMetrics(diskStats *statspb.DiskStatsCollection, instanceProperties *iipb.InstanceProperties, startTime int64, refreshTime int64) []*mpb.Metric {
	var diskMetrics []*mpb.Metric

	for _, disk := range instanceProperties.GetDisks() {
		deviceID := disk.GetDeviceName()
		if disk.GetDiskName() != "" {
			deviceID = disk.GetDiskName()
		}
		localDiskMapping := localDiskForMapping(diskStats, disk.GetMapping())
		volumeQueueLength := fmt.Sprintf("%g", metricsformatter.Unavailable)
		volumeReadResponseTime := fmt.Sprintf("%g", metricsformatter.Unavailable)
		volumeWriteResponseTime := fmt.Sprintf("%g", metricsformatter.Unavailable)

		if localDiskMapping != nil {
			volumeQueueLength = strconv.FormatInt(localDiskMapping.GetQueueLength(), 10)
			volumeReadResponseTime = strconv.FormatInt(localDiskMapping.GetAverageReadResponseTimeMillis(), 10)
			volumeWriteResponseTime = strconv.FormatInt(localDiskMapping.GetAverageWriteResponseTimeMillis(), 10)
		}

		diskMetrics = append(diskMetrics,
			&mpb.Metric{
				Name:            "Volume ID",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				DeviceId:        deviceID,
				Value:           deviceID,
			},
			&mpb.Metric{
				Name:            "Volume Type",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				DeviceId:        deviceID,
				Value:           formattedDeviceType(disk.GetDeviceType()),
			},
			&mpb.Metric{
				Name:            "Mapping",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				DeviceId:        deviceID,
				Value:           disk.GetMapping(),
			},
			&mpb.Metric{
				Name:            "Volume Queue Length",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				LastRefresh:     refreshTime,
				DeviceId:        deviceID,
				Value:           volumeQueueLength,
			},
			&mpb.Metric{
				Name:            "Volume Read Response Time",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_MSEC,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				LastRefresh:     refreshTime,
				DeviceId:        deviceID,
				Value:           volumeReadResponseTime,
			},
			&mpb.Metric{
				Name:            "Volume Write Response Time",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_DISK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_MSEC,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				LastRefresh:     refreshTime,
				DeviceId:        deviceID,
				Value:           volumeWriteResponseTime,
			})
	}

	return diskMetrics
}

/*
networkMetrics returns a slice of network adapter metrics from the OS - adapter ID, adapter device
mapping, minimum bandwidth, maximum bandwidth, and the current bandwidth.

This sequence of metrics is generated for each network adapter in use by the OS
(i.e. {adapterID_1, mapping_1, minBandwidth_1, ..., adapterID_2, mapping_2, ...}).
*/
func networkMetrics(cpuStats *statspb.CpuStats, instanceProperties *iipb.InstanceProperties, startTime int64) []*mpb.Metric {
	var networkMetrics []*mpb.Metric

	for _, networkAdapter := range instanceProperties.GetNetworkAdapters() {
		adapterID := networkAdapter.GetName()
		networkMetrics = append(networkMetrics,
			&mpb.Metric{
				Name:            "Adapter ID",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_NETWORK,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           adapterID,
				DeviceId:        adapterID,
			},
		)
		networkMetrics = append(networkMetrics,
			&mpb.Metric{
				Name:            "Mapping",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_NETWORK,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           networkAdapter.GetMapping(),
				DeviceId:        adapterID,
			},
		)
		bandwidth := min(16, cpuStats.GetCpuCount()*2) * 1000
		networkMetrics = append(networkMetrics,
			&mpb.Metric{
				Name:            "Minimum Network Bandwidth",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_NETWORK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_MBPS,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           strconv.FormatInt(bandwidth, 10),
				DeviceId:        adapterID,
			},
			&mpb.Metric{
				Name:            "Maximum Network Bandwidth",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_NETWORK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_MBPS,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           strconv.FormatInt(bandwidth, 10),
				DeviceId:        adapterID,
			},
			&mpb.Metric{
				Name:            "Current Network Bandwidth",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_NETWORK,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_MBPS,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           strconv.FormatInt(bandwidth, 10),
				DeviceId:        adapterID,
			},
		)
	}
	return networkMetrics
}

/*
cpuMetrics returns a slice of cpu metrics from the OS.
*/
func cpuMetrics(cpuStats *statspb.CpuStats, instanceProperties *iipb.InstanceProperties, startTime int64) []*mpb.Metric {
	// CPU metrics.
	var cpuMetrics []*mpb.Metric
	if instanceProperties.GetCpuPlatform() != "" {
		cpuMetrics = []*mpb.Metric{
			&mpb.Metric{
				Name:            "Processor Type",
				Context:         mpb.Context_CONTEXT_HOST,
				Category:        mpb.Category_CATEGORY_CPU,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     startTime,
				Value:           instanceProperties.GetCpuPlatform(),
			},
		}
	}
	// usually 2 threads per core.
	var threadsPerCore int64 = 2
	if cpuStats.GetCpuCount() > 0 && cpuStats.GetCpuCores() > 9 {
		threadsPerCore = cpuStats.GetCpuCount() / cpuStats.GetCpuCores()
	}

	cpuMetrics = append(cpuMetrics,
		&mpb.Metric{
			Name:            "Number of Threads per Core",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_INT64,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           strconv.FormatInt(threadsPerCore, 10),
		},
		&mpb.Metric{
			Name:            "Current HW frequency",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_INT64,
			Unit:            mpb.Unit_UNIT_MHZ,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           strconv.FormatInt(cpuStats.GetMaxMhz(), 10),
		},
		&mpb.Metric{
			Name:            "Max. HW frequency",
			Context:         mpb.Context_CONTEXT_HOST,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_INT64,
			Unit:            mpb.Unit_UNIT_MHZ,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           strconv.FormatInt(cpuStats.GetMaxMhz(), 10),
		},
		&mpb.Metric{
			Name:            "Reference Compute Unit [CU]",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_STRING,
			Unit:            mpb.Unit_UNIT_NONE,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           cpuStats.GetProcessorType(),
		},
		&mpb.Metric{
			Name:            "Phys. Processing Power per vCPU",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_DOUBLE,
			Unit:            mpb.Unit_UNIT_CU,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           "1.00",
		},
		&mpb.Metric{
			Name:            "Guaranteed VM Processing Power",
			Context:         mpb.Context_CONTEXT_VM,
			Category:        mpb.Category_CATEGORY_CPU,
			Type:            mpb.Type_TYPE_DOUBLE,
			Unit:            mpb.Unit_UNIT_CU,
			RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
			LastRefresh:     startTime,
			Value:           strconv.FormatInt(cpuStats.GetCpuCount(), 10) + ".00",
		},
	)
	return cpuMetrics
}

/*
localDiskForMapping returns the disk stats associated with a disk mapping "mapping" from the
collection of disk stats "collection".
*/
func localDiskForMapping(collection *statspb.DiskStatsCollection, mapping string) *statspb.DiskStats {
	for _, diskStats := range collection.GetDiskStats() {
		if diskStats.GetDeviceName() == mapping {
			return diskStats
		}
	}
	return nil
}
