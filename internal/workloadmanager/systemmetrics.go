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

package workloadmanager

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const (
	// OSReleaseFilePath lists the location of the os-release file in the Linux system.
	OSReleaseFilePath = "/etc/os-release"

	sapValidationSystem = "workload.googleapis.com/sap/validation/system"
)

// InterfaceAddrsGetter satisfies the function signature for net.InterfaceAddrs().
type InterfaceAddrsGetter func() ([]net.Addr, error)

// CollectSystemMetricsFromConfig collects the system metrics specified by the
// WorkloadValidation config and formats the results as a time series to be
// uploaded to a Collection Storage mechanism.
func CollectSystemMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.Logger.Info("Collecting Workload Manager System metrics...")
	l := make(map[string]string)

	system := params.WorkloadConfig.GetValidationSystem()
	for _, m := range system.GetSystemMetrics() {
		v := collectSystemVariable(m, params)
		l[m.GetMetricInfo().GetLabel()] = v
	}
	for _, m := range system.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationSystem, l, 1, params.Config)}
}

// collectSystemVariable collects and returns the metric value for a given system metric variable.
func collectSystemVariable(m *wlmpb.SystemMetric, params Parameters) string {
	v := m.GetValue()
	switch v {
	case wlmpb.SystemVariable_INSTANCE_NAME:
		return params.Config.GetCloudProperties().GetInstanceName()
	case wlmpb.SystemVariable_OS_NAME_VERSION:
		return fmt.Sprintf("%s-%s", params.osVendorID, params.osVersion)
	case wlmpb.SystemVariable_AGENT_NAME:
		return params.Config.GetAgentProperties().GetName()
	case wlmpb.SystemVariable_AGENT_VERSION:
		return params.Config.GetAgentProperties().GetVersion()
	case wlmpb.SystemVariable_NETWORK_IPS:
		return networkIPAddrs(params)
	default:
		log.Logger.Warnw("System metric has no system variable value to collect from", "metric", m.GetMetricInfo().GetLabel())
		return ""
	}
}

// networkIPAddrs parses the network interface addresses from the system.
func networkIPAddrs(params Parameters) string {
	addrs, err := params.InterfaceAddrsGetter()
	if err != nil {
		log.Logger.Warnw("Could not get network interface addresses", "error", err)
		return ""
	}
	v := []string{}
	for _, ipaddr := range addrs {
		v = append(v, ipaddr.String())
	}
	return strings.Join(v, ",")
}
