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

// Package reliability implements OTE mode for reading, parsing, and sending
// Cloud Monitoring metrics reliability data to a GCS bucket.
package reliability

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"google.golang.org/api/option"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/storage"

	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	s "cloud.google.com/go/storage"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	userAgent = "Reliability for GCS"
)

var (
	// Force the numeric project id to be returned rather than the display name.
	// See the following for more information: https://g3doc.corp.google.com/monitoring/monarch/mql/g3doc/user/how_to.md?cl=head#the-project-id-column.
	defaultQueries = []queryInfo{
		{
			query:      "fetch gce_instance | metric 'workload.googleapis.com/sap/hana/ha/availability' | map rename [resource.project_number: resource.project_id] | group_by drop [resource.zone, metric.instance_nr] | every 1s | within 2h",
			identifier: "hana_ha_availability",
			headers:    []string{"project_id", "instance_id", "sid", "value", "start_time", "end_time"},
			wantLabels: []string{"project_id", "instance_id", "sid"},
		},
		{
			query:      "fetch gce_instance | metric 'workload.googleapis.com/sap/hana/availability' | map rename [resource.project_number: resource.project_id] | group_by drop [resource.zone, metric.instance_nr] | every 1s | within 2h",
			identifier: "hana_availability",
			headers:    []string{"project_id", "instance_id", "sid", "value", "start_time", "end_time"},
			wantLabels: []string{"project_id", "instance_id", "sid"},
		},
	}
)

// queryInfo stores information for queries and their results.
type queryInfo struct {
	query, identifier   string
	headers, wantLabels []string
}

// Reliability has args for reliability subcommands.
type Reliability struct {
	projectID, outputFolder    string
	bucketName, serviceAccount string
	help                       bool
	logLevel, logPath          string

	queries    []queryInfo
	cmr        *cloudmetricreader.CloudMetricReader
	bucket     *s.BucketHandle
	cloudProps *ipb.CloudProperties
	oteLogger  *onetime.OTELogger
}

// Name implements the subcommand interface for reliability.
func (*Reliability) Name() string { return "reliability" }

// Synopsis implements the subcommand interface for reliability.
func (*Reliability) Synopsis() string { return "read reliability data from Cloud Monitoring" }

// Usage implements the subcommand interface for reliability.
func (*Reliability) Usage() string {
	return `Usage: reliability [-project=<project-id>] [-bucket=<bucket-name>]
	[-o=output-folder] [-service-account=<service-account>]
	[-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for reliability.
func (r *Reliability) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.projectID, "project", "", "Project ID, defaults to the value from the metadata server")
	fs.StringVar(&r.bucketName, "bucket", "", "GCS bucket name to send packaged results to")
	fs.StringVar(&r.outputFolder, "o", "/tmp/google-cloud-sap-agent", "Output folder")
	fs.StringVar(&r.serviceAccount, "service-account", "", "Service account to authenticate with")
	fs.StringVar(&r.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/reliability.log")
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.StringVar(&r.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for reliability.
func (r *Reliability) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cloudProps, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     r.Name(),
		Help:     r.help,
		LogLevel: r.logLevel,
		LogPath:  r.logPath,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	return r.Run(ctx, onetime.CreateRunOptions(cloudProps, false))
}

// Run performs the functionality specified by the reliability subcommand.
func (r *Reliability) Run(ctx context.Context, runOpts *onetime.RunOptions) subcommands.ExitStatus {
	r.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	r.cloudProps = runOpts.CloudProperties
	log.CtxLogger(ctx).Info("Reliability starting")
	r.queries = defaultQueries
	if r.projectID == "" {
		r.projectID = r.cloudProps.GetProjectId()
		log.CtxLogger(ctx).Warnf("Project ID defaulted to: %s", r.projectID)
	}

	if r.bucketName != "" {
		connectParams := &storage.ConnectParameters{
			StorageClient:   s.NewClient,
			ServiceAccount:  r.serviceAccount,
			BucketName:      r.bucketName,
			UserAgentSuffix: userAgent,
			UserAgent:       configuration.StorageAgentName(),
		}
		var ok bool
		if r.bucket, ok = storage.ConnectToBucket(ctx, connectParams); !ok {
			log.CtxLogger(ctx).Errorw("Failed to connect to bucket", "bucketName", r.bucketName)
			return subcommands.ExitFailure
		}
	}

	var opts []option.ClientOption
	if r.serviceAccount != "" {
		opts = append(opts, option.WithCredentialsFile(r.serviceAccount))
	}
	mqc, err := monitoring.NewQueryClient(ctx, opts...)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring query client", "error", err)
		r.oteLogger.LogUsageError(usagemetrics.QueryClientCreateFailure)
		return subcommands.ExitFailure
	}
	r.cmr = &cloudmetricreader.CloudMetricReader{
		QueryClient: &cloudmetricreader.QueryClient{Client: mqc},
		BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
	}

	return r.reliabilityHandler(ctx, io.Copy)
}

// reliabilityHandler executes all queries, parses the time series,
// writes CSV results and uploads them to a GCS bucket.
func (r *Reliability) reliabilityHandler(ctx context.Context, copier storage.IOFileCopier) subcommands.ExitStatus {
	r.oteLogger.LogUsageAction(usagemetrics.ReliabilityStarted)
	if err := os.MkdirAll(r.outputFolder, os.ModePerm); err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create output folder", "outputFolder", r.outputFolder, "err", err)
		return subcommands.ExitFailure
	}

	for _, q := range r.queries {
		data, err := r.executeQuery(ctx, q.identifier, q.query)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to execute query", "query", q.query, "err", err)
			r.oteLogger.LogUsageError(usagemetrics.ReliabilityQueryFailure)
			return subcommands.ExitFailure
		}

		results, err := r.parseData(ctx, q.headers, q.wantLabels, data)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to parse data", "query", q.query, "err", err)
			r.oteLogger.LogUsageError(usagemetrics.ReliabilityQueryFailure)
			return subcommands.ExitFailure
		}

		fileName, err := r.writeResults(results, q.identifier)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Failed to write results", "identifier", q.identifier, "query", q.query, "err", err)
			r.oteLogger.LogUsageError(usagemetrics.ReliabilityWriteFileFailure)
			return subcommands.ExitFailure
		}

		if r.bucket != nil {
			if err := r.uploadFile(ctx, fileName, copier); err != nil {
				log.CtxLogger(ctx).Errorw("Failed to upload file", "fileName", fileName, "err", err)
				r.oteLogger.LogUsageError(usagemetrics.ReliabilityBucketUploadFailure)
				return subcommands.ExitFailure
			}
		}
	}

	log.CtxLogger(ctx).Info("Reliability finished")
	r.oteLogger.LogUsageAction(usagemetrics.ReliabilityFinished)
	return subcommands.ExitSuccess
}

// executeQuery queries Cloud Monitoring and returns the results.
func (r *Reliability) executeQuery(ctx context.Context, identifier, query string) ([]*mrpb.TimeSeriesData, error) {
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

// parseData formats the raw time series data as CSV and
// buckets start and end times based on changing values.
func (r *Reliability) parseData(ctx context.Context, headers []string, wantLabels []string, data []*mrpb.TimeSeriesData) ([][]string, error) {
	results := [][]string{}
	results = append(results, headers)
	for _, instance := range data {
		labels := instance.GetLabelValues()
		if len(labels) != len(wantLabels) {
			return nil, fmt.Errorf("query results contained incorrect labels, want %s, got %s", wantLabels, labels)
		}
		stringLabels := []string{}
		for _, label := range labels {
			stringLabels = append(stringLabels, label.GetStringValue())
		}

		lastValue := instance.GetPointData()[0].GetValues()[0].GetInt64Value()
		endTime := instance.GetPointData()[0].GetTimeInterval().GetEndTime()
		startTime := endTime
		for _, point := range instance.GetPointData() {
			value := point.GetValues()[0].GetInt64Value()
			if value != lastValue {
				results = append(results, append(stringLabels, []string{strconv.FormatInt(lastValue, 10), strconv.FormatInt(startTime.GetSeconds(), 10), strconv.FormatInt(endTime.GetSeconds(), 10)}...))
				endTime = point.GetTimeInterval().GetEndTime()
			}
			startTime = point.GetTimeInterval().GetStartTime()
			lastValue = value
		}
		results = append(results, append(stringLabels, []string{strconv.FormatInt(lastValue, 10), strconv.FormatInt(startTime.GetSeconds(), 10), strconv.FormatInt(endTime.GetSeconds(), 10)}...))
	}
	return results, nil
}

// writeResults writes the string results to the output in CSV format.
func (r *Reliability) writeResults(results [][]string, identifier string) (string, error) {
	// Use ISO 8601 standard for printing the current time.
	outFile := fmt.Sprintf("%s/%s_%s.csv", r.outputFolder, identifier, time.Now().Format("20060102T150405Z0700"))
	log.Logger.Infow("Writing results", "outFile", outFile)
	f, err := os.Create(outFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := csv.NewWriter(f).WriteAll(results); err != nil {
		return "", err
	}
	log.Logger.Infow("Results written", "outFile", outFile)
	return outFile, nil
}

// uploadFile uploads the file to the GCS bucket.
func (r *Reliability) uploadFile(ctx context.Context, fileName string, copier storage.IOFileCopier) error {
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
