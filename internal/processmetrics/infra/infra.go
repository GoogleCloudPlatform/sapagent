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

// Package infra contains functions that gather infra level migration events for the SAP agent.
package infra

import (
	"context"
	"fmt"
	"time"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"golang.org/x/exp/slices"
	compute "google.golang.org/api/compute/v0.alpha"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// GCEAlphaInterface provides a testable interface to gcealpha.
type GCEAlphaInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	ListNodeGroups(project, zone string) (*compute.NodeGroupList, error)
	ListNodeGroupNodes(project, zone, nodeGroup string) (*compute.NodeGroupsListNodes, error)
}

const (
	metricURL = "workload.googleapis.com"
	// Reports a scheduled migration for a GCE instance due to host maintenance.
	// Value of 1 is reported 60 seconds before the migration starts until its completion.
	// Value of 0 is reported otherwise.
	migrationPath = "/sap/infra/migration"
	/*
			Metrics in /sap/infra/upcoming_maintenance:

			/sap/infra/upcoming_maintenance/window_start (int64): Start of upcoming maintenance window, as a Unix timestamp
			/sap/infra/upcoming_maintenance/window_end (int64): End of upcoming maintenance window, as a Unix timestamp
			/sap/infra/upcoming_maintenance/can_reschedule (bool) True if a user can reschedule the maintenance, False otherwise
			/sap/infra/upcoming_maintenance/type (int64): 1 if upcoming maintenance is scheduled, 2 if unscheduled
		  /sap/infra/upcoming_maintenance/latest_window_start (int64): The latest window start time that can be requested, as a Unix timestamp
		  /sap/infra/upcoming_maintenance/status (int64): 1 if there is pending maintenance, 2 if the maintenance is ongoing
	*/
	maintPath = "/sap/infra/upcoming_maintenance"

	metadataMigrationResponse = "MIGRATE_ON_HOST_MAINTENANCE"
)

var (
	metadataServerCall = metadataserver.FetchGCEMaintenanceEvent
	// MaintenanceTypes map upcoming maintenance types to int metric values.
	MaintenanceTypes = map[string]int64{
		"SCHEDULED":   1,
		"UNSCHEDULED": 2,
	}
	// MaintenanceStatuses map upcoming maintenance status to int metric values.
	MaintenanceStatuses = map[string]int64{
		"PENDING": 1,
		"ONGOING": 2,
	}
)

// Properties struct has necessary context for Metrics collection.
// InstanceProperties implements Collector interface for infra level metric collection.
type Properties struct {
	Config          *cnfpb.Configuration
	Client          cloudmonitoring.TimeSeriesCreator
	gceAlphaService GCEAlphaInterface
}

// New creates a new instance of Properties
func New(config *cnfpb.Configuration, client cloudmonitoring.TimeSeriesCreator, gceAlphaService GCEAlphaInterface) *Properties {
	return &Properties{
		Config:          config,
		Client:          client,
		gceAlphaService: gceAlphaService,
	}
}

// Collect is an implementation of Collector interface from processmetrics
// responsible for collecting infra migration event metrics.
func (p *Properties) Collect(ctx context.Context) []*mrpb.TimeSeries {
	if p.Config.BareMetal {
		return nil
	}
	log.Logger.Info("Collecting infrastructure metrics")

	scheduledMigration := collectScheduledMigration(p, metadataServerCall)
	upcomingMaintenance, _ := p.collectUpcomingMaintenance()
	return append(scheduledMigration, upcomingMaintenance...)
}

func collectScheduledMigration(p *Properties, f func() (string, error)) []*mrpb.TimeSeries {
	if slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), migrationPath) {
		return []*mrpb.TimeSeries{}
	}
	event, err := f()
	if err != nil {
		return []*mrpb.TimeSeries{}
	}
	var scheduledMigration int64
	if event == metadataMigrationResponse {
		scheduledMigration = 1
	}
	return []*mrpb.TimeSeries{p.createIntMetric(migrationPath, scheduledMigration)}
}

func (p *Properties) collectUpcomingMaintenance() ([]*mrpb.TimeSeries, error) {
	cp := p.Config.CloudProperties
	project, zone, instName := cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName()

	// TODO : use the regular GCE service once the UpcomingMaintenance API
	// is available there.
	if p.gceAlphaService == nil {
		log.Logger.Debug("compute alpha API not initialized; skipping maint checks.")
		return []*mrpb.TimeSeries{}, fmt.Errorf("compute alpha API not initialized; skipping maint checks")
	}

	instance, err := p.gceAlphaService.GetInstance(project, zone, instName)
	if err != nil {
		log.Logger.Errorw("Could not get instance from compute API", "project", project, "zone", zone, "instance", instName, "error", err)
		return []*mrpb.TimeSeries{}, fmt.Errorf("Could not get instance from compute API: %w", err)
	}
	if instance.Scheduling == nil || len(instance.Scheduling.NodeAffinities) == 0 {
		log.Logger.Debug("Not a sole tenant node; skipping maintenance metric collection.")
		return []*mrpb.TimeSeries{}, fmt.Errorf("not a sole tenant node; skipping maintenance metric collection")
	}

	n, err := p.resolveNodeGroup(project, zone, instance.SelfLink)
	if err != nil {
		log.Logger.Errorw("Could not resolve node", "link", instance.SelfLink, "error", err)
		return []*mrpb.TimeSeries{}, fmt.Errorf("Could not resolve node: %w", err)
	}
	if n.UpcomingMaintenance == nil {
		log.Logger.Debugw("No upcoming maintenance", "cp", cp)
		return []*mrpb.TimeSeries{}, nil
	}

	log.Logger.Infof("Found upcoming maintenance: %+v", n.UpcomingMaintenance)

	m := []*mrpb.TimeSeries{}

	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/can_reschedule") {
		m = append(m, p.createBoolMetric(maintPath+"/can_reschedule", n.UpcomingMaintenance.CanReschedule))
	}
	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/type") {
		m = append(m, p.createIntMetric(maintPath+"/type", enumToInt(n.UpcomingMaintenance.Type, MaintenanceTypes)))
	}
	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/maintenance_status") {
		m = append(m, p.createIntMetric(maintPath+"/maintenance_status", enumToInt(n.UpcomingMaintenance.MaintenanceStatus, MaintenanceStatuses)))
	}
	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/window_start_time") {
		m = append(m, p.createIntMetric(maintPath+"/window_start_time", rfc3339ToUnix(n.UpcomingMaintenance.WindowStartTime)))
	}
	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/window_end_time") {
		m = append(m, p.createIntMetric(maintPath+"/window_end_time", rfc3339ToUnix(n.UpcomingMaintenance.WindowEndTime)))
	}
	if !slices.Contains(p.Config.GetCollectionConfiguration().GetProcessMetricsToSkip(), maintPath+"/latest_window_start_time") {
		m = append(m, p.createIntMetric(maintPath+"/latest_window_start_time", rfc3339ToUnix(n.UpcomingMaintenance.LatestWindowStartTime)))
	}
	return m, nil
}

// createMetric creates a mrpb.TimeSeries object for the given metric.
func (p *Properties) createIntMetric(mPath string, val int64) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:  p.Config.CloudProperties,
		MetricType: metricURL + mPath,
		Timestamp:  tspb.Now(),
		Int64Value: val,
		BareMetal:  p.Config.BareMetal,
	}
	return timeseries.BuildInt(params)
}

// createBoolMetric creates a mrpb.TimeSeries object for the given boolean metric.
func (p *Properties) createBoolMetric(mPath string, val bool) *mrpb.TimeSeries {
	params := timeseries.Params{
		CloudProp:  p.Config.CloudProperties,
		MetricType: metricURL + mPath,
		Timestamp:  tspb.Now(),
		BoolValue:  val,
		BareMetal:  p.Config.BareMetal,
	}
	return timeseries.BuildBool(params)
}

// rfc3339ToUnix converts a RFC3339 date into a Unix timestamp;  unparseable dates return 0
func rfc3339ToUnix(rfc3339 string) int64 {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		log.Logger.Warnf("Could not parse date", "date", rfc3339, "error", err)
		return 0
	}
	return t.Unix()
}

// enumToInt looks up a string value in a map, returning 0 if not found
func enumToInt(s string, m map[string]int64) int64 {
	e, ok := m[s]
	if !ok {
		return 0
	}
	return e
}

// resolveNodeGroup looks up the sole tenancy node group and nodes data for a given instance.
func (p *Properties) resolveNodeGroup(project, zone, instanceLink string) (*compute.NodeGroupNode, error) {
	nodeGroups, err := p.gceAlphaService.ListNodeGroups(project, zone)
	if err != nil {
		return nil, fmt.Errorf("could not get node groups: %w", err)
	}
	for _, nodeGroup := range nodeGroups.Items {
		nodes, err := p.gceAlphaService.ListNodeGroupNodes(project, zone, nodeGroup.Name)
		if err != nil {
			return nil, fmt.Errorf("could not get node group nodes from cloud API: %w", err)
		}
		for _, node := range nodes.Items {
			for _, i := range node.Instances {
				log.Logger.Debugw("Comparing nodes", "nodeGroupInstances", i, "instance", instanceLink)
				if i == instanceLink {
					log.Logger.Debugw("Found sole tenant node group", "nodeGroup", nodeGroup)
					return node, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no matching node groups")
}
