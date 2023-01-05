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
	"errors"
	"net"
	"testing"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/testing/protocmp"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	cnf = &cnfpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			InstanceName: "test-instance-name",
			InstanceId:   "test-instance-id",
			Zone:         "test-zone",
			ProjectId:    "test-project-id",
		},
		AgentProperties: &cnfpb.AgentProperties{Version: "1.0"},
	}
)

func wantSystemMetrics(ts *timestamppb.Timestamp, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/system",
				Labels: map[string]string{
					"instance_name": "test-instance-name",
					"os":            os,
					"agent":         "gcagent",
					"agent_version": "1.0",
					"agent_state":   "running",
					"gcloud":        "true",
					"gsutil":        "true",
					"network_ips":   "192.168.0.1,192.168.0.2",
				},
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
						DoubleValue: 1,
					},
				},
			}},
		}},
	}
}

func TestCollectSystemMetrics(t *testing.T) {
	tests := []struct {
		name          string
		os            string
		agentStatus   string
		wantOsVersion string
	}{
		{
			name:          "linuxHasMetrics",
			os:            "linux",
			agentStatus:   "Active: active",
			wantOsVersion: "test-os-version",
		},
		{
			name:          "windowsHasMetrics",
			os:            "windows",
			agentStatus:   "Running",
			wantOsVersion: "microsoft_windows_server_2019_datacenter-10.0.17763",
		},
	}
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1, ip2}, nil
	}
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}
	osCaptionExecute = func() (string, string, error) {
		return "\n\nCaption=Microsoft Windows Server 2019 Datacenter \n   \n    \n", "", nil
	}
	osVersionExecute = func() (string, string, error) {
		return "\n Version=10.0.17763  \n\n", "", nil
	}
	cmdExists = func(c string) bool {
		return true
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agentServiceStatus = func(string) (string, string, error) {
				return test.agentStatus, "", nil
			}
			want := wantSystemMetrics(nts, test.wantOsVersion)
			sch := make(chan WorkloadMetrics)
			p := Parameters{
				Config: cnf,
				OSType: test.os,
			}
			go CollectSystemMetrics(p, sch)
			got := <-sch
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCollectSystemMetricsErrors(t *testing.T) {
	tests := []struct {
		name          string
		os            string
		agentStatus   string
		wantOsVersion string
	}{
		{
			name:          "linuxOsReleaseError",
			os:            "linux",
			agentStatus:   "Active: active",
			wantOsVersion: "-",
		},
		{
			name:          "windowsOsReleaseError",
			os:            "windows",
			agentStatus:   "Running",
			wantOsVersion: "-",
		},
	}
	defer func(f func(f string) *goini.INI) { iniParse = f }(iniParse)
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		return ini
	}
	ip1, err := net.ResolveIPAddr("ip", "192.168.0.1")
	if err != nil {
		t.Fatalf("Error resolving 192.168.0.1: %v", err)
	}
	ip2, err := net.ResolveIPAddr("ip", "192.168.0.2")
	if err != nil {
		t.Fatalf("Error resolving 192.168.0.2: %v", err)
	}
	defer func(f func() ([]net.Addr, error)) { netInterfaceAdddrs = f }(netInterfaceAdddrs)
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1, ip2}, nil
	}
	defer func(f func() int64) { now = f }(now)
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}
	defer func(f func() (string, string, error)) { osCaptionExecute = f }(osCaptionExecute)
	osCaptionExecute = func() (string, string, error) {
		return "", "", errors.New("Error")
	}
	defer func(f func() (string, string, error)) { osVersionExecute = f }(osVersionExecute)
	osVersionExecute = func() (string, string, error) {
		return "", "", errors.New("Error")
	}
	defer func(f func(c string) bool) { cmdExists = f }(cmdExists)
	cmdExists = func(c string) bool {
		return true
	}

	defer func(f func(string) (string, string, error)) { agentServiceStatus = f }(agentServiceStatus)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agentServiceStatus = func(string) (string, string, error) {
				return test.agentStatus, "", nil
			}
			want := wantSystemMetrics(nts, test.wantOsVersion)
			sch := make(chan WorkloadMetrics)
			p := Parameters{
				Config: cnf,
				OSType: test.os,
			}
			go CollectSystemMetrics(p, sch)
			got := <-sch
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("CollectSystemMetrics(%#v) returned unexpected metric labels diff (-want +got):\n%s", p, diff)
			}
		})
	}
}
