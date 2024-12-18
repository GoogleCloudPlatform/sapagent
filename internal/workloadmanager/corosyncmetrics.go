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

	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/configurablemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

const sapValidationCorosync = "workload.googleapis.com/sap/validation/corosync"

// CollectCorosyncMetricsFromConfig collects the corosync metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectCorosyncMetricsFromConfig(ctx context.Context, params Parameters, pacemakerVal float64) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager Corosync metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	l := make(map[string]string)

	if pacemakerVal == 0 {
		log.CtxLogger(ctx).Debug("Skipping Corosync metrics collection, Pacemaker not active on instance.")
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationCorosync, l, 0, params.Config)}
	}

	corosync := params.WorkloadConfig.GetValidationCorosync()
	for k, v := range configurablemetrics.CollectMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), corosync.GetConfigPath(), corosync.GetConfigMetrics()) {
		l[k] = v
	}
	for _, m := range corosync.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationCorosync, l, 1, params.Config)}
}
