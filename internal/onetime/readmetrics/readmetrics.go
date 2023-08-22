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

// Package readmetrics implements OTE mode for reading Cloud Monitoring metrics.
package readmetrics

import (
	"context"
	"fmt"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	defaultHanaAvailability   = "fetch gce_instance | metric 'workload.googleapis.com/sap/hana/availability' | group_by 1m, [value_availability_mean: mean(value.availability)] | every 1m | group_by [metric.sid, resource.instance_id], [value_availability_mean_mean: mean(value_availability_mean)] | within 24h"
	defaultHanaHAAvailability = "fetch gce_instance | metric 'workload.googleapis.com/sap/hana/ha/availability' | group_by 1m, [value_availability_mean: mean(value.availability)] | every 1m | group_by [metric.sid, resource.instance_id], [value_availability_mean_mean: mean(value_availability_mean)] | within 24h"
)

// ReadMetrics has args for readmetrics subcommands.
type ReadMetrics struct {
	projectID                  string
	inputFile, outputFolder    string
	bucketName, serviceAccount string
	sendToMonitoring           bool
	help, version              bool
	logLevel                   string

	queries           map[string]string
	cmr               *cloudmetricreader.CloudMetricReader
	status            bool
	timeSeriesCreator cloudmonitoring.TimeSeriesCreator
	cloudProps        *ipb.CloudProperties
}

// Name implements the subcommand interface for readmetrics.
func (*ReadMetrics) Name() string { return "readmetrics" }

// Synopsis implements the subcommand interface for readmetrics.
func (*ReadMetrics) Synopsis() string { return "read metrics from Cloud Monitoring" }

// Usage implements the subcommand interface for readmetrics.
func (*ReadMetrics) Usage() string {
	return `readmetrics -project=<project-id> [-i=<input-file>] [-o=output-folder]
	[-bucket=<bucket-name>] [-service-account=<service-account>]
	[-h] [-v] [loglevel=<debug|info|warn|error>]
	`
}

// SetFlags implements the subcommand interface for readmetrics.
func (r *ReadMetrics) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.projectID, "project", "", "Project ID, defaults to the value from the metadata server")
	fs.StringVar(&r.inputFile, "i", "", "Input file")
	fs.StringVar(&r.outputFolder, "o", "/tmp", "Output folder")
	fs.StringVar(&r.bucketName, "bucket", "", "GCS bucket name to send packaged results to")
	fs.StringVar(&r.serviceAccount, "service-account", "", "Service account to authenticate with")
	fs.BoolVar(&r.sendToMonitoring, "send-status-to-monitoring", true, "Send the execution status to cloud monitoring as a metric")
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.BoolVar(&r.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&r.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for readmetrics.
func (r *ReadMetrics) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 3 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	r.cloudProps, ok = args[2].(*ipb.CloudProperties)
	if !ok {
		log.Logger.Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	if r.help {
		f.Usage()
		return subcommands.ExitSuccess
	}
	if r.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	onetime.SetupOneTimeLogging(lp, r.Name(), log.StringLevelToZapcore(r.logLevel))
	if r.projectID == "" {
		r.projectID = r.cloudProps.GetProjectId()
		log.Logger.Warnf("Project ID defaulted to: %s", r.projectID)
	}

	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return subcommands.ExitFailure
	}
	r.timeSeriesCreator = mc

	mqc, err := monitoring.NewQueryClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring query client", "error", err)
		usagemetrics.Error(usagemetrics.QueryClientCreateFailure)
		return subcommands.ExitFailure
	}
	r.cmr = &cloudmetricreader.CloudMetricReader{
		QueryClient: &cloudmetricreader.QueryClient{Client: mqc},
		BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
	}

	r.queries = make(map[string]string)
	r.queries["default_hana_availability"] = defaultHanaHAAvailability
	r.queries["default_hana_ha_availability"] = defaultHanaHAAvailability
	return r.readMetricsHandler(ctx)
}

func (r *ReadMetrics) readMetricsHandler(ctx context.Context) subcommands.ExitStatus {
	usagemetrics.Action(usagemetrics.ReadMetricsStarted)

	for identifier, query := range r.queries {
		req := &monitoringpb.QueryTimeSeriesRequest{
			Name:  fmt.Sprintf("projects/%s", r.projectID),
			Query: query,
		}

		log.Logger.Infow("Executing query", "identifier", identifier, "query", query)
		data, err := cloudmonitoring.QueryTimeSeriesWithRetry(ctx, r.cmr.QueryClient, req, r.cmr.BackOffs)
		if err != nil {
			log.Logger.Errorw("Query failed", "error", err)
			usagemetrics.Error(usagemetrics.ReadMetricsQueryFailure)
			return subcommands.ExitFailure
		}
		log.Logger.Infow("Query succeeded", "identifier", identifier)
		log.Logger.Debugw("Query response", "identifier", identifier, "response", data)
	}

	usagemetrics.Action(usagemetrics.ReadMetricsFinished)
	return subcommands.ExitSuccess
}
