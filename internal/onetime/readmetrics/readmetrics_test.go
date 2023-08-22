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

package readmetrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	defaultQueries = map[string]string{
		"test": "testQuery",
	}
	defaultBackoff = cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond)
)

func TestExecuteReadMetrics(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			r: ReadMetrics{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			r: ReadMetrics{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.r, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForReadMetrics(t *testing.T) {
	want := "read metrics from Cloud Monitoring"
	r := ReadMetrics{}
	got := r.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForReadMetrics(t *testing.T) {
	r := ReadMetrics{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"project", "i", "o", "send-status-to-monitoring", "bucket", "service-account", "v", "h", "loglevel"}
	r.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestReadMetricsHandler(t *testing.T) {
	tests := []struct {
		name string
		r    ReadMetrics
		want subcommands.ExitStatus
	}{
		{
			name: "NoQueries",
			r:    ReadMetrics{},
			want: subcommands.ExitSuccess,
		},
		{
			name: "QueryFailure",
			r: ReadMetrics{
				queries: defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{
						Err: fmt.Errorf("query failure"),
					},
					BackOffs: defaultBackoff,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "QuerySuccess",
			r: ReadMetrics{
				queries: defaultQueries,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fake.TimeSeriesQuerier{},
					BackOffs:    defaultBackoff,
				},
			},
			want: subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.readMetricsHandler(context.Background())
			if got != test.want {
				t.Errorf("readMetricsHandler()=%v want %v", got, test.want)
			}
		})
	}
}
