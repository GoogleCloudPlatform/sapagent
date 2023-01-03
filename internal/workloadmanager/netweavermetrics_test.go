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

package workloadmanager

import (
	"context"
	"net"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/go-cmp/cmp"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/testing/protocmp"
)

func wantLinuxNetWeaverMetrics(ts *timestamppb.Timestamp, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/netweaver",
				Labels: map[string]string{},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: 0.0,
					},
				},
			}},
		}},
	}
}

func TestCollectNetWeaverMetrics(t *testing.T) {
	duration := time.Now().Add(3 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), duration)

	defer cancel()

	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}

	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1}, nil
	}
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}

	nch := make(chan WorkloadMetrics)
	p := Parameters{
		Config: cnf,
		OSType: "linux",
	}
	go CollectNetWeaverMetrics(p, nch)

	select {
	case got := <-nch:
		want := wantLinuxNetWeaverMetrics(nts, "test-os-version")

		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("CollectNetWeaverMetrics() returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Errorf("CollectNetWeaverMetrics() timed out")
	}

}
