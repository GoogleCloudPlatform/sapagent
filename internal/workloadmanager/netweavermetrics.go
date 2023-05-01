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
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// CollectNetWeaverMetricsFromConfig collects the netweaver metrics as
// specified by the WorkloadValidation config and formats the results as a
// time series to be uploaded to a Collection Storage mechanism.
func CollectNetWeaverMetricsFromConfig(params Parameters) WorkloadMetrics {
	log.Logger.Info("Collecting Workload Manager NetWeaver metrics...")
	t := "workload.googleapis.com/sap/validation/netweaver"
	l := make(map[string]string)

	netweaver := params.WorkloadConfig.GetValidationNetweaver()
	for _, m := range netweaver.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(t, l, 0, params.Config)}
}

/*
CollectNetWeaverMetrics collects NetWeaver metrics for Workload Manager and sends them to the wm channel.
*/
func CollectNetWeaverMetrics(params Parameters, wm chan<- WorkloadMetrics) {
	log.Logger.Info("Collecting workload netweaver metrics...")
	t := "workload.googleapis.com/sap/validation/netweaver"

	wm <- WorkloadMetrics{Metrics: createTimeSeries(t, map[string]string{}, 0, params.Config)}
}
