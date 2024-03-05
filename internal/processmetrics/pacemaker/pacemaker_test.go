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

package pacemaker

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type fakePacemakerCollector struct {
	val    float64
	labels map[string]string
}

var defaultConfig = &cgpb.Configuration{
	CollectionConfiguration: &cgpb.CollectionConfiguration{
		CollectProcessMetrics:   false,
		ProcessMetricsFrequency: 5,
	},
	CloudProperties: &iipb.CloudProperties{
		ProjectId:        "test-project",
		InstanceId:       "test-instance",
		Zone:             "test-zone",
		InstanceName:     "test-instance",
		Image:            "test-image",
		NumericProjectId: "123456",
	},
}

func (f *fakePacemakerCollector) CollectPacemakerMetrics(ctx context.Context) (float64, map[string]string) {
	return f.val, f.labels
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name   string
		p      *InstanceProperties
		wantTS []*mrpb.TimeSeries
	}{
		{
			name: "NoSIDs",
			p: &InstanceProperties{
				Config:             defaultConfig,
				PacemakerCollector: &fakePacemakerCollector{val: 1.0, labels: map[string]string{"label1": "val1", "label2": "val2"}},
			},
			wantTS: nil,
		},
		{
			name: "SuccessfulRead",
			p: &InstanceProperties{
				Config:             defaultConfig,
				Sids:               map[string]bool{"deh": true, "dev": true},
				PacemakerCollector: &fakePacemakerCollector{val: 1.0, labels: map[string]string{"label1": "val1", "label2": "val2"}},
			},
			wantTS: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/pacemaker",
						Labels: map[string]string{
							"sid":    "deh",
							"label1": "val1",
							"label2": "val2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 1,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/pacemaker",
						Labels: map[string]string{
							"sid":    "dev",
							"label1": "val1",
							"label2": "val2",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 1,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := test.p.Collect(context.Background())
			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
				cmpopts.SortSlices(func(a, b *mrpb.TimeSeries) bool { return a.String() < b.String() }),
			}

			if diff := cmp.Diff(test.wantTS, got, cmpOpts...); diff != "" {
				t.Errorf("Collect() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	c := context.Background()
	defaultBOPolicy := cloudmonitoring.LongExponentialBackOffPolicy(c, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
	p := &InstanceProperties{
		Config:          defaultConfig,
		PMBackoffPolicy: defaultBOPolicy,
	}
	metrics, err := p.CollectWithRetry(c)
	if len(metrics) != 0 {
		t.Errorf("CollectWithRetry() returned %d metrics, want 0", len(metrics))
	}
	if err != nil {
		t.Errorf("CollectWithRetry() returned error %v, want nil", err)
	}
}
