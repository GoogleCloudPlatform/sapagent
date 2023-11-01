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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	s "cloud.google.com/go/storage"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	userAgent = "ReadMetrics for GCS"

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
	bucket            *s.BucketHandle
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
	return `Usage: readmetrics -project=<project-id> [-i=<input-file>] [-o=output-folder]
	[-bucket=<bucket-name>] [-service-account=<service-account>]
	[-h] [-v] [loglevel=<debug|info|warn|error>]` + "\n"
}

// SetFlags implements the subcommand interface for readmetrics.
func (r *ReadMetrics) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.projectID, "project", "", "Project ID, defaults to the value from the metadata server")
	fs.StringVar(&r.inputFile, "i", "", "Input file")
	fs.StringVar(&r.outputFolder, "o", "/tmp/google-cloud-sap-agent", "Output folder")
	fs.StringVar(&r.bucketName, "bucket", "", "GCS bucket name to send packaged results to")
	fs.StringVar(&r.serviceAccount, "service-account", "", "Service account to authenticate with")
	fs.BoolVar(&r.sendToMonitoring, "send-status-to-monitoring", true, "Send the execution status to cloud monitoring as a metric")
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.BoolVar(&r.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&r.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for readmetrics.
func (r *ReadMetrics) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if r.help {
		return onetime.HelpCommand(f)
	}
	if r.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	r.cloudProps, ok = args[2].(*ipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, r.Name(), log.StringLevelToZapcore(r.logLevel))
	log.CtxLogger(ctx).Info("ReadMetrics starting")
	if r.projectID == "" {
		r.projectID = r.cloudProps.GetProjectId()
		log.CtxLogger(ctx).Warnf("Project ID defaulted to: %s", r.projectID)
	}

	var err error
	if r.queries, err = r.createQueryMap(); err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create queries", "inputFile", r.inputFile, "err", err)
		return subcommands.ExitFailure
	}

	if r.bucketName != "" {
		if r.bucket, ok = storage.ConnectToBucket(ctx, s.NewClient, r.serviceAccount, r.bucketName, userAgent, false); !ok {
			log.CtxLogger(ctx).Errorw("Failed to connect to bucket", "bucketName", r.bucketName)
			return subcommands.ExitFailure
		}
	}

	var opts []option.ClientOption
	if r.serviceAccount != "" {
		opts = append(opts, option.WithCredentialsFile(r.serviceAccount))
	}
	if r.timeSeriesCreator, err = monitoring.NewMetricClient(ctx, opts...); err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return subcommands.ExitFailure
	}

	mqc, err := monitoring.NewQueryClient(ctx, opts...)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring query client", "error", err)
		usagemetrics.Error(usagemetrics.QueryClientCreateFailure)
		return subcommands.ExitFailure
	}
	r.cmr = &cloudmetricreader.CloudMetricReader{
		QueryClient: &cloudmetricreader.QueryClient{Client: mqc},
		BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
	}

	return r.readMetricsHandler(ctx, io.Copy)
}

// readMetricsHandler executes all queries, saves results to the local
// filesystem, and optionally uploads results to a GCS bucket.
func (r *ReadMetrics) readMetricsHandler(ctx context.Context, copier storage.IOFileCopier) subcommands.ExitStatus {
	usagemetrics.Action(usagemetrics.ReadMetricsStarted)
	if err := os.MkdirAll(r.outputFolder, os.ModePerm); err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create output folder", "outputFolder", r.outputFolder, "err", err)
		return subcommands.ExitFailure
	}
	defer r.sendStatusToMonitoring(ctx, cloudmonitoring.NewDefaultBackOffIntervals())

	for identifier, query := range r.queries {
		if query == "" {
			log.CtxLogger(ctx).Infow("Skipping empty query", "identifier", identifier)
			continue
		}

		data, err := r.executeQuery(ctx, identifier, query)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to execute query", "identifier", identifier, "query", query, "err", err)
			usagemetrics.Error(usagemetrics.ReadMetricsQueryFailure)
			return subcommands.ExitFailure
		}

		fileName, err := r.writeResults(data, identifier)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to write results", "identifier", identifier, "query", query, "err", err)
			usagemetrics.Error(usagemetrics.ReadMetricsWriteFileFailure)
			return subcommands.ExitFailure
		}

		if r.bucket != nil {
			if err := r.uploadFile(ctx, fileName, copier); err != nil {
				log.CtxLogger(ctx).Errorw("Failed to upload file", "fileName", fileName, "err", err)
				usagemetrics.Error(usagemetrics.ReadMetricsBucketUploadFailure)
				return subcommands.ExitFailure
			}
		}
	}

	log.CtxLogger(ctx).Info("ReadMetrics finished")
	r.status = true
	usagemetrics.Action(usagemetrics.ReadMetricsFinished)
	return subcommands.ExitSuccess
}

// createQueryMap creates a map of identifiers to MQL queries from default
// queries and an optional inputFile supplied by the user.
func (r *ReadMetrics) createQueryMap() (map[string]string, error) {
	queries := map[string]string{
		"default_hana_availability":    defaultHanaAvailability,
		"default_hana_ha_availability": defaultHanaHAAvailability,
	}
	if r.inputFile == "" {
		return queries, nil
	}

	data, err := os.ReadFile(r.inputFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &queries); err != nil {
		return nil, err
	}
	return queries, nil
}

// executeQuery queries Cloud Monitoring and returns the results.
func (r *ReadMetrics) executeQuery(ctx context.Context, identifier, query string) ([]*mrpb.TimeSeriesData, error) {
	req := &mpb.QueryTimeSeriesRequest{
		Name:  fmt.Sprintf("projects/%s", r.projectID),
		Query: query,
	}
	log.CtxLogger(ctx).Infow("Executing query", "identifier", identifier, "query", query)
	data, err := cloudmonitoring.QueryTimeSeriesWithRetry(ctx, r.cmr.QueryClient, req, r.cmr.BackOffs)
	if err != nil {
		return nil, err
	}
	log.CtxLogger(ctx).Infow("Query succeeded", "identifier", identifier)
	log.CtxLogger(ctx).Debugw("Query response", "identifier", identifier, "response", data)
	return data, nil
}

// writeResults marshalls the query response data and writes it to the output.
func (r *ReadMetrics) writeResults(data []*mrpb.TimeSeriesData, identifier string) (string, error) {
	// Use ISO 8601 standard for printing the current time.
	outFile := fmt.Sprintf("%s/%s_%s.json", r.outputFolder, identifier, time.Now().Format("20060102T150405Z0700"))
	log.Logger.Infow("Writing results", "outFile", outFile)

	var output []byte
	if len(data) == 0 {
		log.Logger.Warnw("No data, writing empty file", "outFile", outFile)
	} else {
		jsonData := make([]json.RawMessage, len(data))
		var err error
		for i, d := range data {
			jsonData[i], err = protojson.Marshal(d)
			if err != nil {
				return "", err
			}
		}
		output, err = json.Marshal(jsonData)
		if err != nil {
			return "", err
		}
	}

	if err := os.WriteFile(outFile, output, os.ModePerm); err != nil {
		return "", err
	}
	log.Logger.Infow("Results written", "outFile", outFile)
	return outFile, nil
}

// uploadFile uploads the file to the GCS bucket.
func (r *ReadMetrics) uploadFile(ctx context.Context, fileName string, copier storage.IOFileCopier) error {
	if r.bucket == nil {
		return fmt.Errorf("bucket is nil")
	}

	log.CtxLogger(ctx).Infow("Uploading file", "bucket", r.bucketName, "fileName", fileName)
	f, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}

	rw := storage.ReadWriter{
		Reader:       f,
		Copier:       copier,
		BucketHandle: r.bucket,
		BucketName:   r.bucketName,
		ChunkSizeMb:  100,
		ObjectName:   r.cloudProps.GetNumericProjectId() + "/" + filepath.Base(fileName),
		TotalBytes:   fileInfo.Size(),
		MaxRetries:   5,
		LogDelay:     30 * time.Second,
		VerifyUpload: false,
	}
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("File uploaded", "bucket", r.bucketName, "fileName", fileName, "objectName", rw.ObjectName, "bytesWritten", bytesWritten, "fileSize", fileInfo.Size())
	return nil
}

// sendStatusToMonitoring sends the status of ReadMetrics one time execution
// to cloud monitoring as a GAUGE metric.
func (r *ReadMetrics) sendStatusToMonitoring(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) bool {
	if !r.sendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Sending ReadMetrics status to cloud monitoring", "status", r.status)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildBool(timeseries.Params{
			CloudProp:  r.cloudProps,
			MetricType: "workload.googleapis.com/sap/agent/" + r.Name(),
			Timestamp:  tspb.Now(),
			BoolValue:  r.status,
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, r.timeSeriesCreator, bo, r.projectID); err != nil {
		log.CtxLogger(ctx).Errorw("Error sending status metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}
