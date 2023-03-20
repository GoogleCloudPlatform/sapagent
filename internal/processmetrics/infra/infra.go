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

// Package infra contains functions that gather infra level migration events for the SAP agent.
package infra

import (
	"context"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const (
	metricURL = "workload.googleapis.com"
	// Reports a scheduled migration for a GCE instance due to host maintenance.
	// Value of 1 is reported 60 seconds before the migration starts until its completion.
	// Value of 0 is reported otherwise.
	migrationPath = "/sap/infra/migration"

	metadataMigrationResponse = "MIGRATE_ON_HOST_MAINTENANCE"
)

var metadataServerCall = metadataserver.FetchGCEMaintenanceEvent

// Properties struct has necessary context for Metrics collection.
// InstanceProperties implements Collector interface for infra level metric collection.
type Properties struct {
	Config *cnfpb.Configuration
	Client cloudmonitoring.TimeSeriesCreator
}

// Collect is an implementation of Collector interface from processmetrics
// responsible for collecting infra migration event metrics.
func (p *Properties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	if p.Config.BareMetal {
		return nil
	}
	return collectScheduledMigration(p, metadataServerCall)
}

func collectScheduledMigration(p *Properties, f func() (string, error)) []*mrpb.TimeSeries {
	event, err := f()
	if err != nil {
		return []*mrpb.TimeSeries{}
	}
	var scheduledMigration int64
	if event == metadataMigrationResponse {
		scheduledMigration = 1
	}
	return []*mrpb.TimeSeries{createMetrics(p, migrationPath, scheduledMigration)}
}

// createMetrics - create mrpb.TimeSeries object for the given metric.
func createMetrics(p *Properties, mPath string, val int64) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:  p.Config.CloudProperties,
		MetricType: metricURL + mPath,
		Timestamp:  tspb.Now(),
		Int64Value: val,
		BareMetal:  p.Config.BareMetal,
	}
	return timeseries.BuildInt(params)
}
