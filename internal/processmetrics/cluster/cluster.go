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

// Package cluster provides functionality to collect SAP Linux cluster metrics.
// The package implements processmetrics.Collector Interface to collect below metrics:
//   - sap/cluster/failcounts - The failcount value of the Linux HA resources.
//   - sap/cluster/nodes - Indicates the state of the Linux HA cluster state.
//   - sap/cluster/resources - Indicates if the Linux HA cluster resource is up and running.
package cluster

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/metricevents"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// Node states.
const (
	nodeUnclean  = -1
	nodeShutdown = 0
	nodeStandby  = 1
	nodeOnline   = 2
)

// Resource states.
const (
	resourceFailed   = 0
	resourceStopped  = 1
	resourceStarting = 2
	resourceStarted  = 3
)

const (
	stateUnknown   = -10
	metricURL      = "workload.googleapis.com"
	failCountsPath = "/sap/cluster/failcounts"
	nodesPath      = "/sap/cluster/nodes"
	resourcesPath  = "/sap/cluster/resources"
)

type (
	// InstanceProperties has necessary context for metrics collection.
	// InstanceProperties implements Collector interface for cluster metrics.
	InstanceProperties struct {
		SAPInstance     *sapb.SAPInstance
		Config          *cnfpb.Configuration
		Client          cloudmonitoring.TimeSeriesCreator
		PMBackoffPolicy backoff.BackOffContext
		SkippedMetrics  map[string]bool
	}
	readPacemakerNodeState     func(crm *pacemaker.CRMMon) (map[string]string, error)
	readPacemakerResourceState func(crm *pacemaker.CRMMon) ([]pacemaker.Resource, error)
	readPacemakerFailCount     func(crm *pacemaker.CRMMon) ([]pacemaker.ResourceFailCount, error)
)

var (
	nodeStates = map[string]int{
		"unclean":  nodeUnclean,
		"shutdown": nodeShutdown,
		"standby":  nodeStandby,
		"online":   nodeOnline,
	}
	resourceStates = map[string]int{
		"Started":  resourceStarted,
		"Master":   resourceStarted,
		"Slave":    resourceStarted,
		"Starting": resourceStarting,
		"Stopped":  resourceStopped,
		"Failed":   resourceFailed,
	}
)

// Collect is cluster metrics implementation of Collector interface from
// processmetrics.go. Returns a list of Linux cluster related metrics.
// Collect method keeps on collecting all the metrics it can, logs errors if it encounters
// any and returns the collected metrics with the last error encountered while collecting metrics.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	var metricsCollectionErr error
	// we only want to run crm_mon once
	data, err := pacemaker.Data(ctx)
	if err != nil {
		// could not collect data from crm_mon
		log.CtxLogger(ctx).Debugw("Failure in reading crm_mon data from pacemaker", log.Error(err))
		return metrics, err
	}
	// TODO: Test actual timeseries in unit test instead of returning an extra int.
	nodeMetrics, _, err := collectNodeState(ctx, p, pacemaker.NodeState, data)
	if err != nil {
		metricsCollectionErr = err
	}
	if nodeMetrics != nil {
		metrics = append(metrics, nodeMetrics...)
	}
	resourceMetrics, _, err := collectResourceState(ctx, p, pacemaker.ResourceState, data)
	if err != nil {
		metricsCollectionErr = err
	}
	if resourceMetrics != nil {
		metrics = append(metrics, resourceMetrics...)
	}
	failCountMetrics, _, err := collectFailCount(ctx, p, pacemaker.FailCount, data)
	if err != nil {
		metricsCollectionErr = err
	}
	if failCountMetrics != nil {
		metrics = append(metrics, failCountMetrics...)
	}
	return metrics, metricsCollectionErr
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debugw("Context cancelled, exiting CollectWithRetry")
			return nil
		default:
			var err error
			res, err = p.Collect(ctx)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
				attempt++
			}
			return err
		}
	}, p.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Retry limit exceeded", "InstanceId", p.SAPInstance.GetSapsid())
	}
	return res, err
}

// collectNodeState returns the Linux cluster node state metrics as time series.
// The integer values are returned as an array for testability.
func collectNodeState(ctx context.Context, p *InstanceProperties, read readPacemakerNodeState, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int, error) {
	if _, ok := p.SkippedMetrics[nodesPath]; ok {
		log.CtxLogger(ctx).Debugw("Skipping collection for", "metric", nodesPath)
		return nil, nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	nodeState, err := read(crm)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failure in reading pacemaker node state", log.Error(err))
		return nil, nil, err
	}

	for name, value := range nodeState {
		nodeValue := stateFromString(nodeStates, value)
		metricValues = append(metricValues, nodeValue)
		extraLabels := map[string]string{
			"node": name,
		}
		nodeMetric := createMetrics(p, nodesPath, extraLabels, now, int64(nodeValue))
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:       metricURL + nodesPath,
			Message:    fmt.Sprintf("Pacemaker Node State for %s", name),
			Value:      strconv.FormatInt(int64(nodeValue), 10),
			Labels:     metricLabels(p, extraLabels),
			Identifier: name,
		})
		metrics = append(metrics, nodeMetric)
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in nodeState()", "time", time.Since(now.AsTime()))
	return metrics, metricValues, nil
}

// collectResourceState returns the Linux cluster resource state metrics as time series.
// The integer values of metric are returned as an array for testability.
func collectResourceState(ctx context.Context, p *InstanceProperties, read readPacemakerResourceState, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int, error) {
	if slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), resourcesPath) {
		log.CtxLogger(ctx).Debugw("Skipping collection for", "metric", resourcesPath)
		return nil, nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	resourceState, err := read(crm)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failure in reading pacemaker resource state", log.Error(err))
		return nil, nil, err
	}

	// This is a map to prevent duplicates keyed on resource name and node it is monitoring.
	resource := make(map[string]bool)
	for _, r := range resourceState {
		key := r.Name + ":" + r.Node
		if _, ok := resource[key]; ok {
			log.CtxLogger(ctx).Debugw("Duplicate entry for resource", "name", r.Name, "node", r.Node)
			continue
		}
		resource[key] = true
		rValue := stateFromString(resourceStates, r.Role)
		metricValues = append(metricValues, rValue)
		extraLabels := map[string]string{
			"node":     r.Node,
			"resource": r.Name,
		}
		resourceMetric := createMetrics(p, resourcesPath, extraLabels, now, int64(rValue))
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:       metricURL + resourcesPath,
			Message:    fmt.Sprintf("Pacemaker Resource State for %s", key),
			Value:      strconv.FormatInt(int64(rValue), 10),
			Labels:     metricLabels(p, extraLabels),
			Identifier: key,
		})
		metrics = append(metrics, resourceMetric)
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in resourceState()", "time", time.Since(now.AsTime()))
	return metrics, metricValues, nil
}

// stateFromString converts state string value to integer value.
// Uses a param map[string]int for the conversion.
func stateFromString(m map[string]int, val string) int {
	if v, ok := m[val]; ok {
		return v
	}
	return stateUnknown
}

// collectFailCount returns the Linux cluster resource failcounts.
// The metrics are returned only for resources with a failcount entry in
// crm_mon history.
func collectFailCount(ctx context.Context, p *InstanceProperties, read readPacemakerFailCount, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int, error) {
	if slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), failCountsPath) {
		log.CtxLogger(ctx).Debugw("Skipping collection for", "metric", failCountsPath)
		return nil, nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	resourceFailCounts, err := read(crm)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failure reading pacemaker resource fail-count", log.Error(err))
		return nil, nil, err
	}

	for _, r := range resourceFailCounts {
		extraLabels := map[string]string{
			"node":     r.Node,
			"resource": r.ResourceName,
		}
		metrics = append(metrics, createMetrics(p, failCountsPath, extraLabels, now, int64(r.FailCount)))
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:       metricURL + failCountsPath,
			Message:    fmt.Sprintf("Pacemaker Resource Fail Count for %s:%s", r.ResourceName, r.Node),
			Value:      strconv.FormatInt(int64(r.FailCount), 10),
			Labels:     metricLabels(p, extraLabels),
			Identifier: fmt.Sprintf("%s:%s", r.ResourceName, r.Node),
		})
		metricValues = append(metricValues, r.FailCount)
	}
	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in collectFailCount()", "time", time.Since(now.AsTime()))
	return metrics, metricValues, nil
}

// createMetricsInt creates mrpb.TimeSeries for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(p.Config.CloudProperties),
		MetricType:   metricURL + mPath,
		MetricLabels: metricLabels(p, extraLabels),
		Timestamp:    now,
		Int64Value:   val,
		BareMetal:    p.Config.BareMetal,
	}
	return timeseries.BuildInt(params)
}

/*
metricLabels combines the default SAP Instance labels and extra labels
to return a consolidated map of metric labels.
*/
func metricLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":           p.SAPInstance.GetSapsid(),
		"type":          p.SAPInstance.GetType().String(),
		"instance_name": p.Config.CloudProperties.InstanceName,
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}
