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

// Package sapservice is resposible for collecting metrics for SAP service
// statuses using systemctl is-* cmd.
package sapservice

import (
	"context"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapdiscovery"
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
	// commandExecutor is a function to execute command. Production callers
	// to pass commandlineexecutor.ExpandAndExecuteCommand while calling
	// this package's APIs.
	commandExecutor func(string, string) (string, string, error)

	// exitCode is a function to get the exit code for the error passed in the argument. Production
	// callers to pass commandlineexecutor.ExitCode while calling
	// this package's APIs.
	exitCode func(error) int

	// InstanceProperties has necessary context for Metrics collection.
	// InstanceProperties implements Collector interface for sapservice.
	InstanceProperties struct {
		Config   *cnfpb.Configuration
		Client   cloudmonitoring.TimeSeriesCreator
		Executor commandExecutor
		ExitCode exitCode
	}
)

// Collect is an implementation of Collector interface from processmetrics
// responsible for collecting sap service statuses metric.
func (p *InstanceProperties) Collect(ctx context.Context) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	isFailedMetrics := queryInstanceState(p, "is-failed", p.Executor)
	metrics = append(metrics, isFailedMetrics...)
	isDisabledMetrics := queryInstanceState(p, "is-enabled", p.Executor)
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
func queryInstanceState(p *InstanceProperties, metric string, executor commandExecutor) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	for _, service := range services {
		command := "systemctl"
		args := metric + " --quiet " + service
		_, stderr, err := executor(command, args)
		exitCode := p.ExitCode(err)
		if metric == "is-failed" && exitCode != 0 {
			log.Logger.Debugw("No error while executing command, not sending is_failed metric", "command", command, "args", args)
			continue
		} else if metric != "is-failed" && err == nil {
			log.Logger.Debugw("No error while executing command, not sending is_disabled metric", "command", command, "args", args)
			continue
		}
		log.Logger.Debugw("Error while executing command", "command", command, "args", args, "stderr", stderr)
		params := timeseries.Params{
			CloudProp:    p.Config.CloudProperties,
			MetricType:   metricURL + mPathMap[metric],
			MetricLabels: map[string]string{"service": service},
			Timestamp:    tspb.Now(),
			Int64Value:   1,
			BareMetal:    p.Config.BareMetal,
		}
		ts := timeseries.BuildInt(params)
		metrics = append(metrics, &sapdiscovery.Metrics{TimeSeries: ts})
	}
	return metrics
}
