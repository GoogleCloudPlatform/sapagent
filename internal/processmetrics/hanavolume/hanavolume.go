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

// Package hanavolume is responsible for collection of HANA Volume metrics.
package hanavolume

import (
	"context"
	"path"
	"strings"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Properties struct contains the parameters necessary for hanavolume package common methods.
type Properties struct {
	Executor        commandlineexecutor.Execute
	Config          *cnfpb.Configuration
	Client          cloudmonitoring.TimeSeriesCreator
	CommandParams   commandlineexecutor.Params
	PMBackoffPolicy backoff.BackOffContext
}

const (
	metricURL  = "workload.googleapis.com"
	volumePath = "/sap/hana/volumes"
	hanaLog    = "/hana/log"
	hanaShared = "/hana/shared"
	hanaBackup = "/hanabackup"
	hanaData   = "/hana/data"
	usrSap     = "/usr/sap"
)

var mountPaths = map[string]string{
	hanaLog:    hanaLog,
	hanaShared: hanaShared,
	hanaBackup: hanaBackup,
	hanaData:   hanaData,
	usrSap:     usrSap,
}

/*
Collect is an implementation of Collector interface defined in processmetrics.go.
Collect method collects metrics to show the details about Hana logical volumes
including total size, available space, amount used and usage percentage, for each volumes.
*/
func (p *Properties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	log.CtxLogger(ctx).Debug("Collecting hana volume metrics")
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  p.CommandParams.Executable,
		ArgsToSplit: p.CommandParams.ArgsToSplit,
	})

	if result.Error != nil {
		log.CtxLogger(ctx).Warnw("could not execute df -h command", "error", result.Error)
		return nil, result.Error
	}

	return p.createTSList(ctx, result.StdOut), nil
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *Properties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	attempt := 1
	var res []*mrpb.TimeSeries
	err := backoff.Retry(func() error {
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.CtxLogger(ctx).Errorw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Infow("Retry limit exceeded", "error", err)
	}
	return res, err
}

// createTSList creates a slice of TimeSeries HANA logical volume metrics
// to send to cloud monitoring.
func (p *Properties) createTSList(ctx context.Context, cmdOutput string) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries

	lines := strings.Split(cmdOutput, "\n")
	for _, line := range lines {
		items := strings.Fields(line)
		if len(items) == 0 {
			continue
		}
		if path, ok := mountPaths[items[len(items)-1]]; ok {
			if len(items) < 6 {
				log.CtxLogger(ctx).Warnw("too few items. need exactly 6", "length:", len(items), "line:", line)
				continue
			}
			size := items[1]
			used := items[2]
			avail := items[3]
			usage := items[4]

			volMetrics := p.collectVolumeMetrics(ctx, path, size, used, avail, usage)
			if volMetrics != nil {
				metrics = append(metrics, volMetrics...)
			}
		}
	}
	log.CtxLogger(ctx).Debug("HANA Volume Metrics created", metrics)

	return metrics
}

func (p *Properties) collectVolumeMetrics(ctx context.Context, path, size, used, avail, usage string) []*mrpb.TimeSeries {
	labels := map[string]string{
		"mountPath": path,
		"size":      size,
		"used":      used,
		"avail":     avail,
		"usage":     usage,
	}

	return []*mrpb.TimeSeries{p.createMetric(labels)}
}

func (p *Properties) createMetric(labels map[string]string) *mrpb.TimeSeries {
	log.Logger.Debugw("Creating metric for instance", "metric", labels["mountPath"], "value", true, "labels", labels)

	ts := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   path.Join(metricURL, volumePath),
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		BareMetal:    p.Config.BareMetal,
		BoolValue:    true,
	}
	log.Logger.Debug("Created metric path: ", ts.MetricType)

	return timeseries.BuildBool(ts)
}
