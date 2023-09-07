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
	"time"

	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

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
		SAPInstance    *sapb.SAPInstance
		Config         *cnfpb.Configuration
		Client         cloudmonitoring.TimeSeriesCreator
		pmbo           *cloudmonitoring.BackOffIntervals
		SkippedMetrics map[string]bool
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
func (p *InstanceProperties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries
	// we only want to run crm_mon once
	data, err := pacemaker.Data(ctx)
	if err != nil {
		// could not collect data from crm_mon
		log.Logger.Errorw("Failure in reading crm_mon data from pacemaker", log.Error(err))
		return metrics
	}
	nodeMetrics, _ := collectNodeState(p, pacemaker.NodeState, data)
	metrics = append(metrics, nodeMetrics...)
	resourceMetrics, _ := collectResourceState(p, pacemaker.ResourceState, data)
	metrics = append(metrics, resourceMetrics...)
	failCountMetrics, _ := collectFailCount(p, pacemaker.FailCount, data)
	metrics = append(metrics, failCountMetrics...)
	return metrics
}

// collectNodeState returns the Linux cluster node state metrics as time series.
// The integer values are returned as an array for testability.
func collectNodeState(p *InstanceProperties, read readPacemakerNodeState, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int) {
	if _, ok := p.SkippedMetrics[nodesPath]; ok {
		return nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	nodeState, err := read(crm)
	if err != nil {
		log.Logger.Errorw("Failure in reading pacemaker node state", log.Error(err))
		return nil, nil
	}

	for name, value := range nodeState {
		nodeValue := stateFromString(nodeStates, value)
		metricValues = append(metricValues, nodeValue)
		extraLabels := map[string]string{
			"node": name,
		}
		nodeMetric := createMetrics(p, nodesPath, extraLabels, now, int64(nodeValue))
		metrics = append(metrics, nodeMetric)
	}
	log.Logger.Debugw("Time taken to collect metrics in nodeState()", "time", time.Since(now.AsTime()))
	return metrics, metricValues
}

// collectResourceState returns the Linux cluster resource state metrics as time series.
// The integer values of metric are returned as an array for testability.
func collectResourceState(p *InstanceProperties, read readPacemakerResourceState, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int) {
	if slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), resourcesPath) {
		return nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	resourceState, err := read(crm)
	if err != nil {
		log.Logger.Errorw("Failure in reading pacemaker resource state", log.Error(err))
		return nil, nil
	}

	resourceNames := make(map[string]bool)
	for _, r := range resourceState {
		if _, ok := resourceNames[r.Name]; ok {
			log.Logger.Debugw("Duplicate entry for resource", "name", r.Name)
			continue
		}
		resourceNames[r.Name] = true
		rValue := stateFromString(resourceStates, r.Role)
		metricValues = append(metricValues, rValue)
		extraLabels := map[string]string{
			"node":     r.Node,
			"resource": r.Name,
		}
		resourceMetric := createMetrics(p, resourcesPath, extraLabels, now, int64(rValue))
		metrics = append(metrics, resourceMetric)
	}
	log.Logger.Debugw("Time taken to collect metrics in resourceState()", "time", time.Since(now.AsTime()))
	return metrics, metricValues
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
func collectFailCount(p *InstanceProperties, read readPacemakerFailCount, crm *pacemaker.CRMMon) ([]*mrpb.TimeSeries, []int) {
	if slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), failCountsPath) {
		return nil, nil
	}
	var metricValues []int
	var metrics []*mrpb.TimeSeries

	now := tspb.Now()
	resourceFailCounts, err := read(crm)
	if err != nil {
		log.Logger.Debugw("Failure reading pacemaker resource fail-count", log.Error(err))
		return nil, nil
	}

	for _, r := range resourceFailCounts {
		extraLabels := map[string]string{
			"node":     r.Node,
			"resource": r.ResourceName,
		}
		metrics = append(metrics, createMetrics(p, failCountsPath, extraLabels, now, int64(r.FailCount)))
		metricValues = append(metricValues, r.FailCount)
	}
	log.Logger.Debugw("Time taken to collect metrics in collectFailCount()", "time", time.Since(now.AsTime()))
	return metrics, metricValues
}

// createMetricsInt creates mrpb.TimeSeries for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
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
to return a consilidated map of metric labels.
*/
func metricLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":  p.SAPInstance.GetSapsid(),
		"type": p.SAPInstance.GetType().String(),
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}
