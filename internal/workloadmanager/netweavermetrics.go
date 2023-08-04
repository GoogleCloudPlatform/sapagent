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

	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"google3/third_party/sapagent/shared/log/log"
)

const sapValidationNetweaver = "workload.googleapis.com/sap/validation/netweaver"

// CollectNetWeaverMetricsFromConfig collects the netweaver metrics as
// specified by the WorkloadValidation config and formats the results as a
// time series to be uploaded to a Collection Storage mechanism.
func CollectNetWeaverMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.Logger.Info("Collecting Workload Manager NetWeaver metrics...")
	l := make(map[string]string)

	netweaver := params.WorkloadConfig.GetValidationNetweaver()
	for _, m := range netweaver.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationNetweaver, l, params.netweaverPresent, params.Config)}
}
