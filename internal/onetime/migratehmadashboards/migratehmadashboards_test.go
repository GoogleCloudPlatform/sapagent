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

package migratehmadashboards

import (
	"context"
	"embed"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	dashboard "cloud.google.com/go/monitoring/dashboard/apiv1"
	dashboardpb "cloud.google.com/go/monitoring/dashboard/apiv1/dashboardpb"
)

//go:embed testdata/*.json
var dashboardsDir embed.FS

type (
	fakeDashboardsAPICaller        struct{}
	fakeDashboardsAPICallerFailure struct{}
)

func (f *fakeDashboardsAPICaller) ListDashboards(context.Context, *dashboardpb.ListDashboardsRequest, ...gax.CallOption) *dashboard.DashboardIterator {
	return &dashboard.DashboardIterator{Response: &dashboardpb.ListDashboardsResponse{Dashboards: []*dashboardpb.Dashboard{}}}
}

func (f *fakeDashboardsAPICaller) CreateDashboard(context.Context, *dashboardpb.CreateDashboardRequest, ...gax.CallOption) (*dashboardpb.Dashboard, error) {
	return nil, nil
}

func (f *fakeDashboardsAPICallerFailure) ListDashboards(context.Context, *dashboardpb.ListDashboardsRequest, ...gax.CallOption) *dashboard.DashboardIterator {
	return nil
}

func (f *fakeDashboardsAPICallerFailure) CreateDashboard(ctx context.Context, req *dashboardpb.CreateDashboardRequest, opts ...gax.CallOption) (*dashboardpb.Dashboard, error) {
	return nil, cmpopts.AnyError
}

func readDashboards(files []string) []*dashboardpb.Dashboard {
	dashboards := []*dashboardpb.Dashboard{}
	for _, f := range files {
		content, err := dashboardsDir.ReadFile(f)
		if err != nil {
			continue
		}
		db := &dashboardpb.Dashboard{}
		err = protojson.Unmarshal(content, db)
		if err != nil {
			continue
		}
		dashboards = append(dashboards, db)
	}
	return dashboards
}

func TestSetFlags(t *testing.T) {
	m := &MigrateHMADashboards{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	m.SetFlags(fs)

	flags := []string{"project", "v", "h", "loglevel"}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		m    *MigrateHMADashboards
		f    *flag.FlagSet
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			m:    &MigrateHMADashboards{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			m:    &MigrateHMADashboards{},
			f:    &flag.FlagSet{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
		{
			name: "SuccessForAgentVersion",
			m:    &MigrateHMADashboards{version: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForHelp",
			m:    &MigrateHMADashboards{help: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}

	ctx := context.Background()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.m.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.m, test.args, got, test.want)
			}
		})
	}
}

func TestMigrateHMADashboards(t *testing.T) {
	tests := []struct {
		name               string
		project            string
		dashboardAPIClient dashboardsAPICaller
		dashboards         []*dashboardpb.Dashboard
		wantSuccessCount   int
		wantFailureCount   int
		wantNoOpCount      int
	}{
		{
			name:               "MigrateHMADashboardsSuccessfully",
			project:            "test-project",
			dashboardAPIClient: &fakeDashboardsAPICaller{},
			dashboards: readDashboards([]string{
				"testdata/test_dashboard_with_hma_metrics.json",
				"testdata/test_dashboard_without_hma_metrics.json",
			}),
			wantSuccessCount: 1,
			wantFailureCount: 0,
			wantNoOpCount:    1,
		},
		{
			name:               "MigrateHMADashboardsFailure",
			project:            "test-project",
			dashboardAPIClient: &fakeDashboardsAPICallerFailure{},
			dashboards: readDashboards([]string{
				"testdata/test_dashboard_with_hma_metrics.json",
				"testdata/test_dashboard_without_hma_metrics.json",
			}),
			wantSuccessCount: 0,
			wantFailureCount: 1,
			wantNoOpCount:    1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := migrateHMADashboards(context.Background(), tc.dashboardAPIClient, tc.dashboards, tc.project)
			if len(res.successfulUpdates) != tc.wantSuccessCount {
				t.Errorf("len(successfulUpdates) = %v, want %v", len(res.successfulUpdates), tc.wantSuccessCount)
			}
			if len(res.failedUpdates) != tc.wantFailureCount {
				t.Errorf("len(failedUpdates) = %v, want %v", len(res.failedUpdates), tc.wantFailureCount)
			}
			if len(res.noOps) != tc.wantNoOpCount {
				t.Errorf("len(noOps) = %v, want %v", len(res.noOps), tc.wantNoOpCount)
			}
		})
	}
}

func TestReplaceMetric(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			name: "test-1",
			s:    `metric.type="custom.googleapis.com/sap_hana/host/instance_memory/total_allocated_size"`,
			want: `metric.type="workload.googleapis.com/sap/hanamonitoring/host/instance_memory/total_allocated_size"`,
		},
		{
			name: "test-2",
			s:    `metric.type="custom.googleapis.com/sap_hana/host/instance_memory/total_allocated_size", metric.type="custom.googleapis.com/sap_hana/host/instance_memory/total_used_size"`,
			want: `metric.type="workload.googleapis.com/sap/hanamonitoring/host/instance_memory/total_allocated_size", metric.type="workload.googleapis.com/sap/hanamonitoring/host/instance_memory/total_used_size"`,
		},
		{
			name: "test-3",
			s:    "random",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := replaceMetric(tc.s)
			if got != tc.want {
				t.Errorf("replaceMetric(%v) = %v, want: %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestOldDashboardName(t *testing.T) {
	s := "test-dashboard-google-cloud-sap-agent"
	want := "test-dashboard"
	got := oldDashboardName(s)
	if got != want {
		t.Errorf("oldDashboardName(%v) = %v, want: %v", s, got, want)
	}
}

func TestUsage(t *testing.T) {
	want := `Usage: migratehmadashboards -project=<project-name> [-h] [-v] [-loglevel]=<debug|info|warn|error>\n`
	m := &MigrateHMADashboards{}
	got := m.Usage()
	if got != want {
		t.Errorf("Usage() = %v, want: %v", got, want)
	}
}

func TestSynopsis(t *testing.T) {
	want := "migrate HANA Monitoring Agent dashboards to use metrics generated by Google Cloud's Agent for SAP"
	m := &MigrateHMADashboards{help: true}
	got := m.Synopsis()
	if got != want {
		t.Errorf("Synopsis() = %v, want: %v", got, want)
	}
}

func TestName(t *testing.T) {
	want := "migratehmadashboards"
	m := &MigrateHMADashboards{}
	got := m.Name()
	if got != want {
		t.Errorf("Name() = %v, want: %v", got, want)
	}
}
