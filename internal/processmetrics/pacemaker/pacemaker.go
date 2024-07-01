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

// Package pacemaker package is responsible for sending linux cluster related metrics
// to cloud monitoring by interacting with pacemaker command.
//   - /sap/pacemaker
package pacemaker

import (
	"context"
	"strconv"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/metricevents"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// PMCollector provides testable replacement for workloadmanager.CollectPacemakerMetrics API.
type PMCollector interface {
	CollectPacemakerMetrics(ctx context.Context) (float64, map[string]string)
}

// Params has the necessary context to collect pacemaker metrics.
type Params struct {
	PCMParams pacemaker.Parameters
}

// InstanceProperties have the necessary context for pacemaker metric collection
type InstanceProperties struct {
	Config             *cnfpb.Configuration
	Client             cloudmonitoring.TimeSeriesCreator
	Sids               map[string]bool
	SkippedMetrics     map[string]bool
	PMBackoffPolicy    backoff.BackOffContext
	PacemakerCollector PMCollector
}

// TODO: Document this in public docs post launch.
const pacemakerPath = "workload.googleapis.com/sap/pacemaker"

// CollectPacemakerMetrics is a PMCollector implementation of the PMCollector interface.
func (pm Params) CollectPacemakerMetrics(ctx context.Context) (float64, map[string]string) {
	return pacemaker.CollectPacemakerMetrics(ctx, pm.PCMParams)
}

// Collect is a Pacemaker implementation of the Collector interface from
// processmetrics. It returns the value of current linux cluster related pacemaker
// metrics configured per sid as a metric list.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	if _, ok := p.SkippedMetrics[pacemakerPath]; ok {
		return metrics, nil
	}
	if p.Sids == nil {
		log.CtxLogger(ctx).Debug("Sids is nil, skipping pacemaker metric collection.")
		return metrics, nil
	}

	log.CtxLogger(ctx).Debug("Starting pacemaker metric collection.")
	pacemakerVal, labels := p.PacemakerCollector.CollectPacemakerMetrics(ctx)

	for sid := range p.Sids {
		l := map[string]string{
			"sid": sid,
		}
		for k, v := range labels {
			l[k] = v
		}

		params := timeseries.Params{
			CloudProp:    p.Config.CloudProperties,
			MetricType:   pacemakerPath,
			MetricLabels: l,
			Timestamp:    tspb.Now(),
			Int64Value:   int64(pacemakerVal),
			BareMetal:    p.Config.BareMetal,
		}
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:       pacemakerPath,
			Message:    "Pacemaker Metrics",
			Value:      strconv.FormatInt(int64(pacemakerVal), 10),
			Labels:     l,
			Identifier: sid,
		})
		metrics = append(metrics, timeseries.BuildInt(params))
	}
	log.CtxLogger(ctx).Debugw("Finished pacemaker metric collection.", "metrics", metrics)
	return metrics, nil
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		var err error
		res, err = p.Collect(ctx)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
			attempt++
		}
		return err
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debug("Retry limit exceeded", "error", err)
	}
	return res, err
}
