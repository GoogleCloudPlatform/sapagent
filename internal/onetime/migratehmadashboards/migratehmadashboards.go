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

// Package migratehmadashboards implements the one time execution for migrating all old HMA based
// dashboards to Google Agent for SAP based metrics dashboard.
package migratehmadashboards

import (
	"context"
	"fmt"
	"regexp"

	"flag"
	"github.com/googleapis/gax-go/v2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	dashboard "cloud.google.com/go/monitoring/dashboard/apiv1"
	dashboardpb "cloud.google.com/go/monitoring/dashboard/apiv1/dashboardpb"
)

const (
	newNameSpace string = "workload.googleapis.com"
)

// MigrateHMADashboards is a struct which implements subcommands interface.
type (
	MigrateHMADashboards struct {
		project       string
		help, version bool
		logLevel      string
	}

	dashboardUpdatesResults struct {
		successfulUpdates []string
		failedUpdates     []string
		noOps             []string
	}

	dashboardsAPICaller interface {
		ListDashboards(context.Context, *dashboardpb.ListDashboardsRequest, ...gax.CallOption) *dashboard.DashboardIterator
		CreateDashboard(context.Context, *dashboardpb.CreateDashboardRequest, ...gax.CallOption) (*dashboardpb.Dashboard, error)
	}
)

var (
	metricReplacementRegex = regexp.MustCompile(`custom\.googleapis\.com/sap_hana/`)
	googleCloudSAPAgent    = regexp.MustCompile(`-google-cloud-sap-agent`)
)

// Name implements the subcommand interface for MigrateHMADashboards.
func (*MigrateHMADashboards) Name() string { return "migratehmadashboards" }

// Synopsis implements the subcommand interface for MigrateHMADashboards.
func (*MigrateHMADashboards) Synopsis() string {
	return "migrate HANA Monitoring Agent dashboards to use metrics generated by Google Cloud's Agent for SAP"
}

// Usage implements the subcommand interface for MigrateHMADashboards.
func (*MigrateHMADashboards) Usage() string {
	return `Usage: migratehmadashboards -project=<project-name> [-h] [-v] [-loglevel]=<debug|info|warn|error>\n`
}

// SetFlags implements the subcommand interface for MigrateHMADashboards.
func (m *MigrateHMADashboards) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&m.project, "project", "", "GCP project. (required)")
	fs.BoolVar(&m.help, "h", false, "Display help")
	fs.BoolVar(&m.version, "v", false, "Display agent version")
	fs.StringVar(&m.logLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for Migrating HANA Monitoring Agent.
func (m *MigrateHMADashboards) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if m.help {
		return onetime.HelpCommand(f)
	}
	if m.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 2 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, m.Name(), log.StringLevelToZapcore(m.logLevel))
	dc, err := dashboard.NewDashboardsClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create dashboard client: error", err)
		return subcommands.ExitFailure
	}
	return m.migrateHMADashboardHandler(ctx, f, dc)
}

func (m *MigrateHMADashboards) migrateHMADashboardHandler(ctx context.Context, fs *flag.FlagSet, dc dashboardsAPICaller) subcommands.ExitStatus {
	dashboards := fetchDashboards(ctx, dc, m.project)
	res := migrateHMADashboards(ctx, dc, dashboards, m.project)
	onetime.LogMessageToFileAndConsole(fmt.Sprintf("HANA Monitoring Agent Dashboard Migration Results.\nDashboard-Name | New Dashboard-Name | Migration Result"))
	for _, s := range res.successfulUpdates {
		onetime.LogMessageToFileAndConsole(fmt.Sprintf("%s | %s | successful", oldDashboardName(s), s))
	}
	for _, s := range res.failedUpdates {
		onetime.LogMessageToFileAndConsole(fmt.Sprintf("%s | %s | failed", oldDashboardName(s), s))
	}
	return subcommands.ExitSuccess
}

// fetchDashboards gets all the dashboards in a GCP Project, converts the response into Proto and returns the list.
// In order to use the ListDashboards API, roles/monitoring.metricReader role is required in the project.
func fetchDashboards(ctx context.Context, dashboardClient dashboardsAPICaller, project string) []*dashboardpb.Dashboard {
	// Fetch All dashboards from a given Project
	listDashboardsReq := &dashboardpb.ListDashboardsRequest{
		Parent: "projects/" + project,
	}
	log.CtxLogger(ctx).Debugw("Fetching dashboards for project", project, "listDashboardsReq", listDashboardsReq.String())
	listDashboardResp := dashboardClient.ListDashboards(ctx, listDashboardsReq)
	var dashboards []*dashboardpb.Dashboard
	for db, _ := listDashboardResp.Next(); db != nil; {
		dashboards = append(dashboards, db)
		db, _ = listDashboardResp.Next()
	}
	return dashboards
}

// migrateHMADashboards gets all the dashboards in a GCP Project, converts the response into Proto and returns the list.
// In order to use the ListDashboards API, roles/monitoring.metricWriter role is required in the project.
func migrateHMADashboards(ctx context.Context, dashboardClient dashboardsAPICaller, dashboards []*dashboardpb.Dashboard, project string) dashboardUpdatesResults {
	result := dashboardUpdatesResults{}
	for i, d := range dashboards {
		log.CtxLogger(ctx).Debugw("Processing dashboard", d.GetDisplayName())
		newDashboard, err := updateDashboard(ctx, d, i, project)
		if err != nil {
			log.CtxLogger(ctx).Debugw("No update required for Dashboard", d.GetDisplayName())
			result.noOps = append(result.noOps, d.GetDisplayName())
			continue
		}
		createDbReq := &dashboardpb.CreateDashboardRequest{
			Parent:    "projects/" + project,
			Dashboard: newDashboard,
		}
		resp, err := dashboardClient.CreateDashboard(ctx, createDbReq)
		if err != nil {
			log.CtxLogger(ctx).Errorf("Failed to create dashboard: %v", err)
			result.failedUpdates = append(result.failedUpdates, resp.GetDisplayName())
		} else {
			log.CtxLogger(ctx).Debugw("Successfully created dashboard", resp.GetDisplayName())
			result.successfulUpdates = append(result.successfulUpdates, resp.GetDisplayName())
		}
	}
	return result
}

func updateDashboard(ctx context.Context, dashboard *dashboardpb.Dashboard, i int, project string) (*dashboardpb.Dashboard, error) {
	var dashboardUpdated bool
	if dashboard.GetMosaicLayout() == nil {
		return dashboard, fmt.Errorf("Dashboard %s does not have MosaicLayout", dashboard.GetDisplayName())
	}
	for i, tiles := range dashboard.GetMosaicLayout().GetTiles() {
		if tiles.GetWidget().GetXyChart() == nil {
			continue
		}
		for j, dataset := range tiles.GetWidget().GetXyChart().GetDataSets() {
			if dataset.GetTimeSeriesQuery().GetTimeSeriesFilter() == nil {
				continue
			}
			filter := dataset.GetTimeSeriesQuery().GetTimeSeriesFilter().GetFilter()
			newFilter := replaceMetric(filter)
			if newFilter != "" {
				dashboardUpdated = true
				dashboard.GetMosaicLayout().GetTiles()[i].GetWidget().GetXyChart().GetDataSets()[j].GetTimeSeriesQuery().GetTimeSeriesFilter().Filter = newFilter
			}
		}
	}
	if !dashboardUpdated {
		return dashboard, fmt.Errorf("Dashboard %s not updated", dashboard.GetDisplayName())
	}
	dashboard.Etag = ""
	dashboard.Name = ""
	dashboard.DisplayName = fmt.Sprintf("%s-%s", dashboard.GetDisplayName(), "google-cloud-sap-agent")
	return dashboard, nil
}

func replaceMetric(s string) string {
	res := metricReplacementRegex.ReplaceAllString(s, newNameSpace+"/sap/hanamonitoring/")
	if res == s {
		return ""
	}
	return res
}

func oldDashboardName(s string) string {
	res := googleCloudSAPAgent.ReplaceAllString(s, "")
	return res
}
