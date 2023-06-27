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

	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

// collectDBMetricsOnce  returns the result of metric collection using the HANA Insights module.
func collectDBMetricsOnce(ctx context.Context, params Parameters) error {
	if params.Config.GetCollectionConfiguration().GetHanaMetricsConfig() == nil {
		log.Logger.Debug("Cannot collect database metrics without DB credentials.")
		return fmt.Errorf("Cannot collect database metrics without DB credentials")
	}

	// TODO: Implement remote mode for database metrics
	if len(params.HANAInsightRules) == 0 {
		log.Logger.Debug("HANA Insights rules not found")
		return fmt.Errorf("HANA Insights rules not found")
	}

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
		return err
	}

	insights, err := ruleengine.Run(ctx, db, params.HANAInsightRules)
	if err != nil {
		log.Logger.Error(err)
		return err
	}
	metrics := processInsights(ctx, params, insights)

	system := CollectSystemMetricsFromConfig(ctx, params)
	appendLabels(metrics.Metrics[0].Metric.Labels, system.Metrics[0].Metric.Labels)

	sendMetrics(ctx, metrics, params.Config.GetCloudProperties().GetProjectId(), &params.TimeSeriesCreator, params.BackOffs)
	return nil
}

func processInsights(ctx context.Context, params Parameters, insights ruleengine.Insights) WorkloadMetrics {
	labels := make(map[string]string)
	mPath := metricTypePrefix + "hanasecurity"

	for ruleID, evalResults := range insights {
		for _, evalResult := range evalResults {
			// Create label - ruleID-recommendationID : TRUE|FALSE
			labels[ruleID+"_"+evalResult.RecommendationID] = fmt.Sprintf("%t", evalResult.Result)
		}
	}
	return WorkloadMetrics{Metrics: createTimeSeries(mPath, labels, 1.0, params.Config)}
}
