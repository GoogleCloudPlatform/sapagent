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
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
)

func TestProcessInsightsAndSend(t *testing.T) {
	params := Parameters{
		Config:            defaultConfigurationDBMetrics,
		TimeSeriesCreator: &fake.TimeSeriesCreator{},
		BackOffs:          defaultBackOffIntervals,
	}

	insights := make(ruleengine.Insights)
	insights["metric"] = []ruleengine.ValidationResult{
		ruleengine.ValidationResult{
			RecommendationID: "recommendation-1",
			Result:           true,
		},
		ruleengine.ValidationResult{
			RecommendationID: "recommendation-2",
		},
	}

	got := processInsightsAndSend(context.Background(), params, insights)
	if got != 2 {
		t.Errorf("processInsightsAndSend()=%d, want: 2", got)
	}
}
