/*
Copyright 2023 Google LLC

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

	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// collectDBMetricsOnce  returns the result of metric collection using the HANA Insights module.
func collectDBMetricsOnce(ctx context.Context, params Parameters) {
	if params.Config.GetCollectionConfiguration().GetHanaMetricsConfig() == nil {
		log.Logger.Debug("Cannot collect database metrics without DB credentials.")
		return
	}

	// TODO: Implement remote mode for database metrics

	log.Logger.Info("Collecting Workload Manager Database metrics...")
	dpb := databaseconnector.Params{
		Username:       params.Config.GetCollectionConfiguration().GetHanaMetricsConfig().GetHanaDbUser(),
		Password:       params.Config.GetCollectionConfiguration().GetHanaMetricsConfig().GetHanaDbPassword(),
		PasswordSecret: params.Config.GetCollectionConfiguration().GetHanaMetricsConfig().GetHanaDbPasswordSecretName(),
		Host:           params.Config.GetCollectionConfiguration().GetHanaMetricsConfig().GetHostname(),
		Port:           params.Config.GetCollectionConfiguration().GetHanaMetricsConfig().GetPort(),
		GCEService:     params.GCEService,
		Project:        params.Config.GetCloudProperties().GetProjectId(),
	}
	db, err := databaseconnector.Connect(ctx, dpb)
	if err != nil {
		log.Logger.Error(err)
		return
	}
	insights, err := ruleengine.Run(ctx, db)
	if err != nil {
		log.Logger.Error(err)
		return
	}
	processInsightsAndSend(ctx, params, insights)
}

func processInsightsAndSend(ctx context.Context, params Parameters, insights ruleengine.Insights) int {
	var labels map[string]string
	ts := []*monitoringresourcespb.TimeSeries{}

	for ruleID, evalResults := range insights {
		mPath := fmt.Sprintf("%sinsights/%s", metricTypePrefix, ruleID)
		for _, evalResult := range evalResults {
			if evalResult.Result {
				labels = map[string]string{"recommendation": evalResult.RecommendationID}
			}
			ts = append(ts, createTimeSeries(mPath, labels, boolToFloat(evalResult.Result), params.Config)...)
		}
	}
	metrics := WorkloadMetrics{Metrics: ts}
	return sendMetrics(ctx, metrics, params.Config.GetCloudProperties().GetProjectId(), &params.TimeSeriesCreator, params.BackOffs)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
