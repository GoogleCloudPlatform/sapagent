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

	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

const sapValidationPacemaker = "workload.googleapis.com/sap/validation/pacemaker"

// CollectPacemakerMetricsFromConfig collects the pacemaker metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectPacemakerMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager Pacemaker metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	pacemakerParams := pacemaker.Parameters{
		Config:                params.Config,
		WorkloadConfig:        params.WorkloadConfig,
		ConfigFileReader:      pacemaker.ConfigFileReader(params.ConfigFileReader),
		Execute:               params.Execute,
		Exists:                params.Exists,
		DefaultTokenGetter:    pacemaker.DefaultTokenGetter(params.DefaultTokenGetter),
		JSONCredentialsGetter: pacemaker.JSONCredentialsGetter(params.JSONCredentialsGetter),
		OSReleaseFilePath:     params.OSReleaseFilePath,
		OSVendorID:            params.osVendorID,
	}
	pacemakerVal, l := pacemaker.CollectPacemakerMetrics(ctx, pacemakerParams)

	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationPacemaker, l, pacemakerVal, params.Config)}
}
