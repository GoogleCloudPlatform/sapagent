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

package configurationmetricreader

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/agenttime"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	confpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	mpb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestRead(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	c := ConfigMetricReader{OS: "linux"}

	cpuStats := &statspb.CpuStats{
		CpuUtilizationPercent: 100,
		CpuCount:              2,
		CpuCores:              4,
		MaxMhz:                2400,
		ProcessorType:         "Intel",
	}

	timestamp := "2019-10-12T07:20:50.52Z"

	instanceProperties := &iipb.InstanceProperties{
		MachineType:               "test_machine",
		LastMigrationEndTimestamp: timestamp,
	}

	config := &confpb.Configuration{
		BareMetal: false,
		AgentProperties: &confpb.AgentProperties{
			Version: "Version number",
		},
		CloudProperties: &iipb.CloudProperties{
			InstanceId: "unknown",
		},
	}

	got := c.Read(config, cpuStats, instanceProperties, *at)
	want := mpb.MetricsCollection{
		Metrics: []*mpb.Metric{
			&mpb.Metric{
				Name:            "Data Provider Version",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_CONFIG,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     at.Startup().UnixMilli(),
				Value:           "Version number",
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
			&mpb.Metric{
				Name:            "Instance Type",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_CONFIG,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     at.Startup().UnixMilli(),
				Value:           "test_machine",
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
				Value:           "unknown",
			},
			&mpb.Metric{
				Name:            "Last Host Change",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_CONFIG,
				Type:            mpb.Type_TYPE_INT64,
				Unit:            mpb.Unit_UNIT_SEC,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
				LastRefresh:     at.LocalRefresh().UnixMilli(),
				Value:           "1570864850",
			},
		},
	}

	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("Read() mismatch (-want, +got):\n%s", d)
	}
}

func TestGetBareMetalMetrics(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	c := ConfigMetricReader{OS: "linux"}

	cpuStats := &statspb.CpuStats{
		CpuUtilizationPercent: 100,
		CpuCount:              2,
		CpuCores:              4,
		MaxMhz:                2400,
		ProcessorType:         "Intel",
	}

	instanceProperties := &iipb.InstanceProperties{}

	config := &confpb.Configuration{
		BareMetal: true,
		AgentProperties: &confpb.AgentProperties{
			Version: "Version number",
		},
	}

	got := c.Read(config, cpuStats, instanceProperties, *at)
	want := mpb.MetricsCollection{
		Metrics: []*mpb.Metric{
			&mpb.Metric{
				Name:            "Data Provider Version",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_CONFIG,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     at.Startup().UnixMilli(),
				Value:           "Version number",
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
			&mpb.Metric{
				Name:            "Instance Type",
				Context:         mpb.Context_CONTEXT_VM,
				Category:        mpb.Category_CATEGORY_CONFIG,
				Type:            mpb.Type_TYPE_STRING,
				Unit:            mpb.Unit_UNIT_NONE,
				RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_RESTART,
				LastRefresh:     at.Startup().UnixMilli(),
				Value:           "bms-2",
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
			},
		},
	}

	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("bareMetalMetrics() mismatch (-want, +got):\n%s", d)
	}
}

func TestInvalidLastHostChangeTimeStamp(t *testing.T) {
	at := agenttime.New(agenttime.Clock{})
	c := ConfigMetricReader{OS: "linux"}

	cpuStats := &statspb.CpuStats{
		CpuUtilizationPercent: 100,
		CpuCount:              2,
		CpuCores:              4,
		MaxMhz:                2400,
		ProcessorType:         "Intel",
	}

	timestamp := "This is an invalid timestamp"

	instanceProperties := &iipb.InstanceProperties{
		MachineType:               "test_machine",
		LastMigrationEndTimestamp: timestamp,
	}

	config := &confpb.Configuration{
		BareMetal: false,
		AgentProperties: &confpb.AgentProperties{
			Version: "Version number",
		},
		CloudProperties: &iipb.CloudProperties{
			InstanceId: "unknown",
		},
	}

	metrics := c.Read(config, cpuStats, instanceProperties, *at).GetMetrics()

	got := metrics[len(metrics)-1]
	want := mpb.Metric{
		Name:            "Last Host Change",
		Context:         mpb.Context_CONTEXT_VM,
		Category:        mpb.Category_CATEGORY_CONFIG,
		Type:            mpb.Type_TYPE_INT64,
		Unit:            mpb.Unit_UNIT_SEC,
		RefreshInterval: mpb.RefreshInterval_REFRESHINTERVAL_PER_MINUTE,
		LastRefresh:     at.LocalRefresh().UnixMilli(),
		Value:           "-1",
	}

	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("bareMetalMetrics() mismatch (-want, +got):\n%s", d)
	}
}
