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

package hanavolume

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:        "test-project",
		InstanceId:       "test-instance",
		Zone:             "test-zone",
		InstanceName:     "test-instance",
		Image:            "test-image",
		NumericProjectId: "123456",
	}

	defaultConfig = &cgpb.Configuration{
		CollectionConfiguration: &cgpb.CollectionConfiguration{
			CollectProcessMetrics:       true,
			ProcessMetricsFrequency:     5,
			SlowProcessMetricsFrequency: 30,
		},
		CloudProperties: defaultCloudProperties,
		BareMetal:       false,
	}

	defaultBOPolicy = func(ctx context.Context) backoff.BackOffContext {
		return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
	}
)

func TestCollect(t *testing.T) {
	tests := []struct {
		name      string
		p         *Properties
		cmdOutput string
		cmdError  error
		wantErr   bool
	}{
		{
			name: "Success",
			p: &Properties{
				Config: defaultConfig,
				Executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: `Filesystem            Size  Used Avail Use% Mounted on
						/dev/hda1             378G     0  378G   0% /
						/dev/hda3             378G     0  378G   0% /hana/log
						tmpfs                 378G     0  378G   0% /dev/shm`,
					}
				},
			},
			wantErr: false,
		},
		{
			name: "CommandExecutionFailure",
			p: &Properties{
				Config: defaultConfig,
				Executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						Error: cmpopts.AnyError,
					}
				},
			},
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.p.Collect(ctx)
			if (err != nil) != tc.wantErr {
				t.Errorf("Collect() returned an unexpected error: %v", err)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	tests := []struct {
		name      string
		p         *Properties
		wantError bool
	}{
		{
			name: "Success",
			p: &Properties{
				Config: defaultConfig,
				Executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: `Filesystem            Size  Used Avail Use% Mounted on
				/dev/hda1             378G     0  378G   0% /
				/dev/hda3             378G     0  378G   0% /hana/log
				tmpfs                 378G     0  378G   0% /dev/shm`,
					}
				},
				PMBackoffPolicy: defaultBOPolicy(context.Background()),
			},
			wantError: false,
		},
		{
			name: "CollectFailure",
			p: &Properties{
				Config: defaultConfig,
				Executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						Error: cmpopts.AnyError,
					}
				},
				PMBackoffPolicy: defaultBOPolicy(context.Background()),
			},
			wantError: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.p.CollectWithRetry(ctx)
			if (err != nil) != tc.wantError {
				t.Fatalf("CollectWithRetry() returned an unexpected error: %v", err)
			}
		})
	}
}

func TestCreateTSList(t *testing.T) {
	tests := []struct {
		name      string
		p         *Properties
		cmdOutput string
		want      []*mrpb.TimeSeries
	}{
		{
			name: "EmptyCmdOutput",
			p: &Properties{
				Config: defaultConfig,
			},
		},
		{
			name: "InvalidCmdOutput",
			p: &Properties{
				Config: defaultConfig,
			},
			cmdOutput: `Filesystem            Size  Used Avail Use% Mounted on
			/dev/hda1                  0  378G    /hana/log
			/dev/hda3             378G     0    0% /export/hda3
			/dev/hda3             378G     0  378G   0%
			`,
		},
		{
			name: "NoMounts",
			p: &Properties{
				Config: defaultConfig,
			},
			cmdOutput: `Filesystem            Size  Used Avail Use% Mounted on
			`,
		},
		{
			name: "NoValidMount",
			p: &Properties{
				Config: defaultConfig,
			},
			cmdOutput: `Filesystem            Size  Used Avail Use% Mounted on
			/dev/hda1             378G     0  378G   0% /
			/dev/hda3             378G     0  378G   0% /export/hda3
			tmpfs                 378G     0  378G   0% /dev/shm
			`,
		},
		{
			name: "ValidMount",
			p: &Properties{
				Config: defaultConfig,
			},
			cmdOutput: `Filesystem            Size  Used Avail Use% Mounted on
			/dev/hda1             378G     0  378G   0% /
			/dev/hda3             378G     0  378G   0% /hana/log
			tmpfs                 378G     0  378G   0% /dev/shm
			`,
			want: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/hana/volumes",
						Labels: map[string]string{
							"mountPath": "/hana/log",
							"size":      "378G",
							"used":      "0",
							"avail":     "378G",
							"usage":     "0%",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{
									BoolValue: true,
								},
							},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.createTSList(ctx, tc.cmdOutput)
			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if diff := cmp.Diff(tc.want, got, cmpOpts...); diff != "" {
				t.Errorf("createTSList(%v) returned an unexpected diff (-want +got): %v", tc.cmdOutput, diff)
			}
		})
	}
}

func TestCollectVolumeMetrics(t *testing.T) {
	tests := []struct {
		name  string
		p     *Properties
		path  string
		size  string
		used  string
		avail string
		usage string
		want  []*mrpb.TimeSeries
	}{
		{
			name: "SampleTest",
			p: &Properties{
				Config: defaultConfig,
			},
			path:  "/hanabackup",
			size:  "234GB",
			used:  "120GB",
			avail: "124GB",
			usage: "51%",
			want: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/hana/volumes",
						Labels: map[string]string{
							"mountPath": "/hanabackup",
							"size":      "234GB",
							"used":      "120GB",
							"avail":     "124GB",
							"usage":     "51%",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_BoolValue{
									BoolValue: true,
								},
							},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.collectVolumeMetrics(ctx, tc.path, tc.size, tc.used, tc.avail, tc.usage)
			fmt.Println(got)

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if diff := cmp.Diff(tc.want, got, cmpOpts...); diff != "" {
				t.Errorf("collectVolumeMetrics(%v, %v, %v, %v, %v) returned an unexpected diff (-want +got): %v", tc.path, tc.size, tc.used, tc.avail, tc.usage, diff)
			}
		})
	}
}

func TestCreateMetric(t *testing.T) {
	tests := []struct {
		name   string
		p      *Properties
		labels map[string]string
		want   *mrpb.TimeSeries
	}{
		{
			name: "SampleTest",
			p: &Properties{
				Config: defaultConfig,
			},
			labels: map[string]string{
				"mountPath": "/hanabackup",
				"size":      "234GB",
				"used":      "120GB",
				"avail":     "124GB",
				"usage":     "51%",
			},
			want: &mrpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type: "workload.googleapis.com/sap/hana/volumes",
					Labels: map[string]string{
						"mountPath": "/hanabackup",
						"size":      "234GB",
						"used":      "120GB",
						"avail":     "124GB",
						"usage":     "51%",
					},
				},
				MetricKind: metricpb.MetricDescriptor_GAUGE,
				Points: []*mrpb.Point{
					{
						Value: &cpb.TypedValue{
							Value: &cpb.TypedValue_BoolValue{
								BoolValue: true,
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.createMetric(tc.labels)
			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if diff := cmp.Diff(tc.want, got, cmpOpts...); diff != "" {
				t.Errorf("createMetric(%v) returned an unexpected diff (-want +got): %v", tc.labels, diff)
			}
		})
	}
}
