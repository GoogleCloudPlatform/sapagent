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
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cnfpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

const (
	metricURL    = "workload.googleapis.com"
	activeMPath  = "/sap/service/is_active"
	enabledMPath = "/sap/service/is_enabled"
)

var (
	services = []string{"pacemaker", "corosync", "sapinit", "sapconf", "saptune"}
	mPathMap = map[string]string{"is-active": activeMPath, "is-enabled": enabledMPath}
)

type (
	// commandExecutor is a function to execute command. Production callers
	// to pass commandlineexecutor.ExpandAndExecuteCommand while calling
	// this package's APIs.
	commandExecutor func(string, string) (string, string, error)

	// InstanceProperties has necessary context for Metrics collection.
	// InstanceProperties implements Collector interface for sapservice.
	InstanceProperties struct {
		Config   *cnfpb.Configuration
		Client   cloudmonitoring.TimeSeriesCreator
		Executor commandExecutor
	}
)

// Collect is an implementation of Collector interface from processmetrics
// responsible for collecting sap service statuses metric.
func (p *InstanceProperties) Collect() []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	activeMetrics := queryInstanceState(p, "is-active", p.Executor)
	metrics = append(metrics, activeMetrics...)
	enabledMetrics := queryInstanceState(p, "is-enabled", p.Executor)
	metrics = append(metrics, enabledMetrics...)
	return metrics
}

// queryInstanceState is resonsible for collecting active / enabled state of OS
// services related to SAP and cluster services.
// In case of `systemctl is-active service` it returns 0 if the specified service is
// active (i.e running), non-zero otherwise.
//
// In case of `systemctl is-enabled service` it returns 0 if the specified service is enabled,
// non-zero otherwise.
func queryInstanceState(p *InstanceProperties, metric string, executor commandExecutor) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	for _, service := range services {
		command := "systemctl"
		args := metric + " --quiet " + service
		_, stderr, err := executor(command, args)
		exitCode := commandlineexecutor.ExitCode(err)
		if err != nil {
			log.Logger.Debugf("Error while executing command: %s %s, error: %s, exitcode: %d", command, args, stderr, exitCode)
			continue
		}

		params := timeseries.Params{
			CloudProp:    p.Config.CloudProperties,
			MetricType:   metricURL + mPathMap[metric],
			MetricLabels: map[string]string{"service": service},
			Timestamp:    tspb.Now(),
			Int64Value:   int64(exitCode),
			BareMetal:    p.Config.BareMetal,
		}
		ts := timeseries.BuildInt(params)
		metrics = append(metrics, &sapdiscovery.Metrics{TimeSeries: ts})
	}
	return metrics
}
