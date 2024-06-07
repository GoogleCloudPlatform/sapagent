/*
Copyright 2024 Google LLC

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

// Package metrics provides common functions to send Backint monitoring metrics.
package metrics

import (
	"context"
	"strconv"
	"time"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"
)

const (
	oneGB        = 1024 * 1024 * 1024
	metricPrefix = "workload.googleapis.com/sap/agent/backint/"
)

type metricClientFunc func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error)

// DefaultMetricClient creates a new metric client from the monitoring package.
var DefaultMetricClient = func(ctx context.Context) (cloudmonitoring.TimeSeriesCreator, error) {
	return monitoring.NewMetricClient(ctx)
}

// SendToCloudMonitoring creates and sends time series for backint.
// Status metrics are sent for each file detailing success/failure.
// Throughput metrics are sent if the file size exceeds 1GB.
func SendToCloudMonitoring(ctx context.Context, operation, fileName string, fileSize int64, transferTime time.Duration, sendToMonitoring, success bool, cloudProps *ipb.CloudProperties, bo *cloudmonitoring.BackOffIntervals, metricClient metricClientFunc) bool {
	if !sendToMonitoring {
		return false
	}
	mtype := metricPrefix + operation
	log.CtxLogger(ctx).Infow("Sending Backint file transfer metrics to cloud monitoring", "mtype", mtype, "fileName", fileName, "fileSize", fileSize, "success", success)
	mc, err := metricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failed to create Cloud Monitoring metric client", "err", err)
		return false
	}
	ts := []*mrpb.TimeSeries{
		timeseries.BuildBool(timeseries.Params{
			CloudProp:  cloudProps,
			MetricType: mtype + "/status",
			Timestamp:  tspb.Now(),
			BoolValue:  success,
			MetricLabels: map[string]string{
				"fileName": fileName,
				"fileSize": strconv.FormatInt(fileSize, 10),
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, mc, bo, cloudProps.GetProjectId()); err != nil {
		log.CtxLogger(ctx).Debugw("Error sending status metric to cloud monitoring", "error", err.Error())
		return false
	}

	if success && fileSize >= oneGB {
		if transferTime.Seconds() == 0 {
			transferTime = time.Second
		}
		avgTransferSpeedMBps := float64(fileSize) / (transferTime.Seconds() * 1024 * 1024)
		ts := []*mrpb.TimeSeries{
			timeseries.BuildFloat64(timeseries.Params{
				CloudProp:    cloudProps,
				MetricType:   mtype + "/throughput",
				Timestamp:    tspb.Now(),
				Float64Value: avgTransferSpeedMBps,
				MetricLabels: map[string]string{
					"fileName":     fileName,
					"fileSize":     strconv.FormatInt(fileSize, 10),
					"transferTime": strconv.FormatInt(int64(transferTime.Seconds()), 10),
				},
			}),
		}
		if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, mc, bo, cloudProps.GetProjectId()); err != nil {
			log.CtxLogger(ctx).Debugw("Error sending throughput metric to cloud monitoring", "error", err.Error())
			return false
		}
	}

	log.CtxLogger(ctx).Infow("Successfully sent Backint file transfer metrics to cloud monitoring", "mtype", mtype, "fileName", fileName)
	return true
}
