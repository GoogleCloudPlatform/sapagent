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

// Package sapservice is responsible for collecting metrics for SAP service
// statuses using systemctl is-* cmd.
package sapservice

import (
	"context"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const (
	metricURL     = "workload.googleapis.com"
	failedMPath   = "/sap/service/is_failed"
	disabledMPath = "/sap/service/is_disabled"
)

var (
	services = []string{"pacemaker", "corosync", "sapinit", "sapconf", "saptune"}
	mPathMap = map[string]string{"is-failed": failedMPath, "is-enabled": disabledMPath}
)

type (
	// InstanceProperties has the necessary context for Metrics collection.
	// InstanceProperties implements the Collector interface for sapservice.
	InstanceProperties struct {
		Config   *cnfpb.Configuration
		Client   cloudmonitoring.TimeSeriesCreator
		Execute  commandlineexecutor.Execute
		ExitCode commandlineexecutor.ExitCode
	}
)

// Collect is an implementation of Collector interface from processmetrics
// responsible for collecting sap service statuses metric.
func (p *InstanceProperties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries
	isFailedMetrics := queryInstanceState(ctx, p, "is-failed")
	metrics = append(metrics, isFailedMetrics...)
	isDisabledMetrics := queryInstanceState(ctx, p, "is-enabled")
	metrics = append(metrics, isDisabledMetrics...)
	return metrics
}

// queryInstanceState is responsible for collecting is_failed / is_enabled state of OS
// services related to SAP and cluster services.
// In case of `systemctl is_failed service` it returns 0 if there has been an error in starting the
// service, metric will be sent only in case of an error.
//
// In case of `systemctl is-enabled service` it returns 0 if the specified service is enabled,
// non-zero otherwise, metric will be sent only in case service is disabled.
func queryInstanceState(ctx context.Context, p *InstanceProperties, metric string) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries
	for _, service := range services {
		command := "systemctl"
		args := metric + " --quiet " + service
		result := p.Execute(ctx, commandlineexecutor.Params{
			Executable:  command,
			ArgsToSplit: args,
		})
		if metric == "is-failed" && result.ExitCode != 0 && result.ExitStatusParsed {
			log.Logger.Debugw("No error while executing command, not sending is_failed metric", "command", command, "args", args)
			continue
		} else if metric != "is-failed" && result.Error == nil {
			log.Logger.Debugw("No error while executing command, not sending is_disabled metric", "command", command, "args", args)
			continue
		}
		log.Logger.Debugw("Error while executing command", "command", command, "args", args, "stderr", result.StdErr)
		params := timeseries.Params{
			CloudProp:    p.Config.CloudProperties,
			MetricType:   metricURL + mPathMap[metric],
			MetricLabels: map[string]string{"service": service},
			Timestamp:    tspb.Now(),
			Int64Value:   1,
			BareMetal:    p.Config.BareMetal,
		}
		metrics = append(metrics, timeseries.BuildInt(params))
	}
	return metrics
}
