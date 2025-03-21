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
	"os"
	"strings"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configureinstance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/configurablemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

const (
	sapValidationSystem = "workload.googleapis.com/sap/validation/system"
)

var (
	// sharedSystemMetrics are the system metric labels that should be populated
	// across all SAP metric tables.
	sharedSystemMetrics = []string{
		"agent",
		"agent_state",
		"agent_version",
		"collection_config_version",
		"instance_name",
		"os",
	}
)

// InterfaceAddrsGetter satisfies the function signature for net.InterfaceAddrs().
type InterfaceAddrsGetter func() ([]net.Addr, error)

// CollectSystemMetricsFromConfig collects the system metrics specified by the
// WorkloadValidation config and formats the results as a time series to be
// uploaded to a Collection Storage mechanism.
func CollectSystemMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager System metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	l := make(map[string]string)

	system := params.WorkloadConfig.GetValidationSystem()
	for _, m := range system.GetSystemMetrics() {
		v := collectSystemVariable(ctx, m, params)
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
func collectSystemVariable(ctx context.Context, m *wlmpb.SystemMetric, params Parameters) string {
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
		return networkIPAddrs(ctx, params)
	case wlmpb.SystemVariable_OS_SETTINGS:
		return osSettings(ctx, params)
	case wlmpb.SystemVariable_COLLECTION_CONFIG_VERSION:
		return fmt.Sprint(params.WorkloadConfig.GetVersion())
	case wlmpb.SystemVariable_APP_SERVER_ZONAL_SEPARATION:
		return appServerZonalSeparation(ctx, params)
	case wlmpb.SystemVariable_HAS_APP_SERVER:
		return hasAppServer(ctx, params)
	default:
		log.CtxLogger(ctx).Warnw("System metric has no system variable value to collect from", "metric", m.GetMetricInfo().GetLabel())
		return ""
	}
}

func appServerZonalSeparation(ctx context.Context, params Parameters) string {
	if params.Discovery == nil {
		log.CtxLogger(ctx).Warn("Discovery has not been initialized, cannot check SAP instances")
		return "false"
	}
	appServerZones := make(map[string]bool)
	centralServiceZones := make(map[string]bool)
	for _, system := range params.Discovery.GetSAPSystems() {
		if system.GetApplicationLayer() == nil {
			continue
		}
		for _, r := range system.GetApplicationLayer().GetResources() {
			if r.GetResourceType() == spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE && r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
				if strings.Contains(r.InstanceProperties.GetInstanceRole().String(), "APP_SERVER") {
					log.CtxLogger(ctx).Debugw("Found app server in zone", "zone", clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones"))
					appServerZones[clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones")] = true
				}
				if strings.Contains(r.InstanceProperties.GetInstanceRole().String(), "ASCS") || strings.Contains(r.InstanceProperties.GetInstanceRole().String(), "ERS") {
					log.CtxLogger(ctx).Debugw("Found central service in zone", "zone", clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones"))
					centralServiceZones[clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones")] = true
				}
			}
		}
	}
	for appServerZone := range appServerZones {
		if _, found := centralServiceZones[appServerZone]; !found {
			log.CtxLogger(ctx).Debugw("Found app server without central service in zone", "zone", appServerZone)
			return "true"
		}
	}
	return "false"
}

func hasAppServer(ctx context.Context, params Parameters) string {
	if params.Discovery == nil {
		log.CtxLogger(ctx).Warn("Discovery has not been initialized, cannot check SAP instances")
		return "false"
	}
	zone := params.Config.GetCloudProperties().GetZone()
	for _, system := range params.Discovery.GetSAPSystems() {
		if system.GetApplicationLayer() == nil {
			continue
		}
		for _, r := range system.GetApplicationLayer().GetResources() {
			if r.GetResourceType() == spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE && r.GetResourceKind() == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
				if strings.Contains(r.InstanceProperties.GetInstanceRole().String(), "APP_SERVER") && clouddiscovery.ExtractFromURI(r.GetResourceUri(), "zones") == zone {
					log.CtxLogger(ctx).Debugw("Found app server in the same zone as the instance", "zone", zone)
					return "true"
				}
			}
		}
	}
	log.CtxLogger(ctx).Debugw("No app server found in the same zone as the instance", "zone", zone)
	return "false"
}

// osSettings runs the configureinstance command to check if OS settings are configured correctly.
func osSettings(ctx context.Context, params Parameters) string {
	ci := &configureinstance.ConfigureInstance{
		Check:          true,
		HyperThreading: "default",
		ReadFile:       os.ReadFile,
		WriteFile:      os.WriteFile,
		ExecuteFunc:    params.Execute,
		MachineType:    params.Config.GetCloudProperties().GetMachineType(),
	}
	// Short-circuit OTE execution for unsupported machine types.
	if !ci.IsSupportedMachineType() {
		return ""
	}
	status, msg := ci.Run(ctx, onetime.CreateRunOptions(params.Config.GetCloudProperties(), true))
	switch status {
	case subcommands.ExitSuccess:
		return "pass"
	case subcommands.ExitFailure:
		log.CtxLogger(ctx).Debugw("Failure detected in checking os_settings via configureinstance", "error", msg)
		return "fail"
	default:
		log.CtxLogger(ctx).Debugw("Unexpected exit status detected in checking os_settings via configureinstance", "exitStatus", status, "error", msg)
		return ""
	}
}

// networkIPAddrs parses the network interface addresses from the system.
func networkIPAddrs(ctx context.Context, params Parameters) string {
	addrs, err := params.InterfaceAddrsGetter()
	if err != nil {
		log.CtxLogger(ctx).Debugw("Could not get network interface addresses", "error", err)
		return ""
	}
	v := []string{}
	for _, ipaddr := range addrs {
		v = append(v, ipaddr.String())
	}
	return strings.Join(v, ",")
}
