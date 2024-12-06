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

package osmetricreader

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestRead(t *testing.T) {
	diskName1, diskName2 := "sda_test", "sda2"
	cpuStats := &statspb.CpuStats{
		CpuUtilizationPercent: 100,
		CpuCount:              64,
		CpuCores:              16,
		MaxMhz:                2400,
		ProcessorType:         "Celeron",
	}
	diskStats := &statspb.DiskStatsCollection{
		DiskStats: []*statspb.DiskStats{
			&statspb.DiskStats{
				DeviceName:                     diskName2,
				QueueLength:                    2,
				AverageReadResponseTimeMillis:  3,
				AverageWriteResponseTimeMillis: 4,
				ReadOpsCount:                   5,
				ReadSvcTimeMillis:              6,
				WriteOpsCount:                  7,
				WriteSvcTimeMillis:             8,
			},
		},
	}
	memoryStats := &statspb.MemoryStats{
		Total: 3000,
		Used:  1000,
		Free:  2000,
	}
	instanceProps := &instancepb.InstanceProperties{
		CpuPlatform: "Intel",
		Disks: []*instancepb.Disk{
			&instancepb.Disk{
				Type:       "PERSISTENT",
				DiskName:   diskName1,
				DeviceName: "sda1",
				DeviceType: "LOCAL_SSD",
				IsLocalSsd: true,
				Mapping:    "sda1",
			},
			&instancepb.Disk{
				Type:       "PERSISTENT",
				DeviceName: diskName2,
				DeviceType: "UNKNOWN",
				IsLocalSsd: false,
				Mapping:    "sda2",
			},
		},
		NetworkAdapters: []*instancepb.NetworkAdapter{
			&instancepb.NetworkAdapter{
				Name:      "lo",
				NetworkIp: "127.0.0.1",
				Mapping:   "loopback_device",
			},
		},
	}

	want := 26
	got := Read(cpuStats, instanceProps, memoryStats, diskStats, *agenttime.New(agenttime.Clock{}))

	// For ease of test maintenance, validating the individual metrics returned occurs in
	// the test functions for TestMemoryMetrics, TestDiskMetrics, TestNetworkMetrics, TestCPUMetrics.
	// To satisfy the test requirements for Read(), check if we have the correct metric result count.
	if len(got.GetMetrics()) != want {
		t.Errorf("Read() got %d metrics, want %d", len(got.GetMetrics()), want)
	}
}

func TestMemoryMetrics(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	tests := []struct {
		name        string
		memoryStats *statspb.MemoryStats
		want        []*mpb.Metric
	}{
		{
			name: "returnsMemoryMetrics",
			memoryStats: &statspb.MemoryStats{
				Total: 3000,
				Used:  2000,
				Free:  1000,
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Guaranteed Memory assigned",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_MEMORY,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MB,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "3000",
				},
				&mpb.Metric{
					Name:            "Memory Consumption",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_MEMORY,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_PERCENT,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "66.7",
				},
			},
		},
		{
			name: "memoryConsumptionUnavailable",
			memoryStats: &statspb.MemoryStats{
				Total: 0,
				Used:  0,
				Free:  0,
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Guaranteed Memory assigned",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_MEMORY,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MB,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "0",
				},
				&mpb.Metric{
					Name:            "Memory Consumption",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_MEMORY,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_PERCENT,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "0.0",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := memoryMetrics(test.memoryStats, at.Startup().UnixMilli())
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("memoryMetrics() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestDiskMetrics(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	deviceID1, deviceID2 := "sda_test", "sda2"
	tests := []struct {
		name          string
		diskStats     *statspb.DiskStatsCollection
		instanceProps *instancepb.InstanceProperties
		want          []*mpb.Metric
	}{
		{
			name: "returnsDiskMetrics",
			diskStats: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "sda",
						QueueLength:                    2,
						AverageReadResponseTimeMillis:  3,
						AverageWriteResponseTimeMillis: 4,
						ReadOpsCount:                   5,
						ReadSvcTimeMillis:              6,
						WriteOpsCount:                  7,
						WriteSvcTimeMillis:             8,
					},
				},
			},
			instanceProps: &instancepb.InstanceProperties{
				Disks: []*instancepb.Disk{
					&instancepb.Disk{
						Type:       "PERSISTENT",
						DiskName:   deviceID1,
						DeviceName: "sda1",
						DeviceType: "local-ssd",
						IsLocalSsd: true,
						Mapping:    "sda",
					},
				},
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Volume ID",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           deviceID1,
				},
				&mpb.Metric{
					Name:            "Volume Type",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           "LOCAL_SSD",
				},
				&mpb.Metric{
					Name:            "Mapping",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           "sda",
				},
				&mpb.Metric{
					Name:            "Volume Queue Length",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           "2",
				},
				&mpb.Metric{
					Name:            "Volume Read Response Time",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MSEC,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           "3",
				},
				&mpb.Metric{
					Name:            "Volume Write Response Time",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MSEC,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID1,
					Value:           "4",
				},
			},
		},
		{
			name: "NoDiskMapping",
			diskStats: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "sda",
						QueueLength:                    2,
						AverageReadResponseTimeMillis:  3,
						AverageWriteResponseTimeMillis: 4,
						ReadOpsCount:                   5,
						ReadSvcTimeMillis:              6,
						WriteOpsCount:                  7,
						WriteSvcTimeMillis:             8,
					},
				},
			},
			instanceProps: &instancepb.InstanceProperties{
				Disks: []*instancepb.Disk{
					&instancepb.Disk{
						Type:       "PERSISTENT",
						DeviceName: deviceID2,
						DeviceType: "local-ssd",
						IsLocalSsd: true,
						Mapping:    "",
					},
				},
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Volume ID",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           deviceID2,
				},
				&mpb.Metric{
					Name:            "Volume Type",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           "LOCAL_SSD",
				},
				&mpb.Metric{
					Name:            "Mapping",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           "",
				},
				&mpb.Metric{
					Name:            "Volume Queue Length",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           "-1",
				},
				&mpb.Metric{
					Name:            "Volume Read Response Time",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MSEC,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           "-1",
				},
				&mpb.Metric{
					Name:            "Volume Write Response Time",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_DISK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MSEC,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
					LastRefresh:     at.LocalRefresh().UnixMilli(),
					DeviceId:        deviceID2,
					Value:           "-1",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := diskMetrics(test.diskStats, test.instanceProps, at.Startup().UnixMilli(), at.LocalRefresh().UnixMilli())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("diskMetrics() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNetworkMetrics(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	adapterID := "lo"
	instanceProps := &instancepb.InstanceProperties{
		NetworkAdapters: []*instancepb.NetworkAdapter{
			&instancepb.NetworkAdapter{
				Name:      "lo",
				NetworkIp: "127.0.0.1",
				Mapping:   "loopback_device",
			},
		},
	}

	tests := []struct {
		name     string
		cpuStats *statspb.CpuStats
		want     []*mpb.Metric
	}{
		{
			name: "returnsNetworkMetrics",
			cpuStats: &statspb.CpuStats{
				CpuUtilizationPercent: 100,
				CpuCount:              2,
				CpuCores:              1,
				MaxMhz:                2400,
				ProcessorType:         "Celeron",
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Adapter ID",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           adapterID,
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Mapping",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "loopback_device",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Minimum Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4000",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Maximum Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4000",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Current Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4000",
					DeviceId:        adapterID,
				},
			},
		},
		{
			name: "bandwidthCapped",
			cpuStats: &statspb.CpuStats{
				CpuUtilizationPercent: 100,
				CpuCount:              64,
				CpuCores:              16,
				MaxMhz:                2400,
				ProcessorType:         "Celeron",
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Adapter ID",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           adapterID,
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Mapping",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "loopback_device",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Minimum Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "16000",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Maximum Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "16000",
					DeviceId:        adapterID,
				},
				&mpb.Metric{
					Name:            "Current Network Bandwidth",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_NETWORK,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MBPS,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "16000",
					DeviceId:        adapterID,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := networkMetrics(test.cpuStats, instanceProps, at.Startup().UnixMilli())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("networkMetrics() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCPUMetrics(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	tests := []struct {
		name          string
		cpuStats      *statspb.CpuStats
		instanceProps *instancepb.InstanceProperties
		want          []*mpb.Metric
	}{
		{
			name: "returnsCPUMetrics",
			cpuStats: &statspb.CpuStats{
				CpuUtilizationPercent: 100,
				CpuCount:              4,
				CpuCores:              1,
				MaxMhz:                2400,
				ProcessorType:         "Celeron",
			},
			instanceProps: &instancepb.InstanceProperties{
				CpuPlatform: "Intel",
			},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Processor Type",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "Intel",
				},
				&mpb.Metric{
					Name:            "Number of Threads per Core",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2",
				},
				&mpb.Metric{
					Name:            "Current HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Max. HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Reference Compute Unit [CU]",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "Celeron",
				},
				&mpb.Metric{
					Name:            "Phys. Processing Power per vCPU",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "1.00",
				},
				&mpb.Metric{
					Name:            "Guaranteed VM Processing Power",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4.00",
				},
			},
		},
		{
			name: "missingCPUPlatform",
			cpuStats: &statspb.CpuStats{
				CpuUtilizationPercent: 100,
				CpuCount:              4,
				CpuCores:              1,
				MaxMhz:                2400,
				ProcessorType:         "Celeron",
			},
			instanceProps: &instancepb.InstanceProperties{},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Number of Threads per Core",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2",
				},
				&mpb.Metric{
					Name:            "Current HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Max. HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Reference Compute Unit [CU]",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "Celeron",
				},
				&mpb.Metric{
					Name:            "Phys. Processing Power per vCPU",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "1.00",
				},
				&mpb.Metric{
					Name:            "Guaranteed VM Processing Power",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4.00",
				},
			},
		},
		{
			name: "customThreadsPerCore",
			cpuStats: &statspb.CpuStats{
				CpuUtilizationPercent: 100,
				CpuCount:              64,
				CpuCores:              16,
				MaxMhz:                2400,
				ProcessorType:         "Celeron",
			},
			instanceProps: &instancepb.InstanceProperties{CpuPlatform: "Intel"},
			want: []*mpb.Metric{
				&mpb.Metric{
					Name:            "Processor Type",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "Intel",
				},
				&mpb.Metric{
					Name:            "Number of Threads per Core",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "4",
				},
				&mpb.Metric{
					Name:            "Current HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Max. HW frequency",
					Context:         mpb.Context_CONTEXT_HOST,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_INT64,
					Unit:            mpb.Unit_UNIT_MHZ,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "2400",
				},
				&mpb.Metric{
					Name:            "Reference Compute Unit [CU]",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_STRING,
					Unit:            mpb.Unit_UNIT_NONE,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "Celeron",
				},
				&mpb.Metric{
					Name:            "Phys. Processing Power per vCPU",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "1.00",
				},
				&mpb.Metric{
					Name:            "Guaranteed VM Processing Power",
					Context:         mpb.Context_CONTEXT_VM,
					Category:        mpb.Category_CATEGORY_CPU,
					Type:            mpb.Type_TYPE_DOUBLE,
					Unit:            mpb.Unit_UNIT_CU,
					RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
					LastRefresh:     at.Startup().UnixMilli(),
					Value:           "64.00",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cpuMetrics(test.cpuStats, test.instanceProps, at.Startup().UnixMilli())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("cpuMetrics() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
