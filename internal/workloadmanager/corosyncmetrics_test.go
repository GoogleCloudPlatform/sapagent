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
	"io"
	"net"
	"strings"
	"testing"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/go-cmp/cmp"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
)

var (
	DefaultTestReader = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(data)), nil
	})
	DefaultTestReaderError = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return nil, errors.New("Could not find file")
	})
)

func csTestCommand(cmd string, args ...string) (string, string, error) {
	cases := map[string]string{
		"totem.token": "token_test",
		"totem.token_retransmits_before_loss_const": "-1",
		"totem.consensus":       "consensus_test",
		"totem.join":            "true",
		"totem.max_messages":    "10",
		"totem.transport":       "false",
		"totem.fail_recv_const": "Can't get key",
		"quorum.two_node":       "two_node value test",
	}
	if len(args) < 2 {
		return "", "", errors.New("not enough arguments")
	}
	return cases[args[1]], "", nil
}

func wantWindowsCorosyncMetrics(ts *timestamppb.Timestamp, corosyncExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/corosync",
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
						DoubleValue: corosyncExists,
					},
				},
			}},
		}},
	}
}

func wantLinuxCorosyncMetrics(ts *timestamppb.Timestamp, corosyncExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/corosync",
				Labels: map[string]string{
					"token":                               "",
					"token_runtime":                       "",
					"token_retransmits_before_loss_const": "",
					"token_retransmits_before_loss_const_runtime": "",
					"consensus":               "",
					"consensus_runtime":       "",
					"join":                    "",
					"join_runtime":            "",
					"max_messages":            "",
					"max_messages_runtime":    "",
					"transport":               "",
					"transport_runtime":       "",
					"fail_recv_const":         "",
					"fail_recv_const_runtime": "",
					"two_node":                "",
					"two_node_runtime":        "",
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
						DoubleValue: corosyncExists,
					},
				},
			}},
		}},
	}
}

func TestReadCorosyncRuntime(t *testing.T) {
	tests := []struct {
		cscommand func(string, ...string) (string, string, error)
		want      map[string]string
	}{
		{
			cscommand: func(string, ...string) (string, string, error) { return "TEST2", "", nil },
			want: map[string]string{
				"token_runtime": "TEST2",
				"token_retransmits_before_loss_const_runtime": "TEST2",
				"consensus_runtime":                           "TEST2",
				"join_runtime":                                "TEST2",
				"max_messages_runtime":                        "TEST2",
				"transport_runtime":                           "TEST2",
				"fail_recv_const_runtime":                     "TEST2",
				"two_node_runtime":                            "TEST2",
			},
		}, {
			cscommand: func(key string, args ...string) (string, string, error) { return "Can't get key " + key, "", nil },
			want: map[string]string{
				"token_runtime": "",
				"token_retransmits_before_loss_const_runtime": "",
				"consensus_runtime":                           "",
				"join_runtime":                                "",
				"max_messages_runtime":                        "",
				"transport_runtime":                           "",
				"fail_recv_const_runtime":                     "",
				"two_node_runtime":                            "",
			},
		},
		{
			cscommand: csTestCommand,
			want: map[string]string{
				"token_runtime": "token_test",
				"token_retransmits_before_loss_const_runtime": "-1",
				"consensus_runtime":                           "consensus_test",
				"join_runtime":                                "true",
				"max_messages_runtime":                        "10",
				"transport_runtime":                           "false",
				"fail_recv_const_runtime":                     "",
				"two_node_runtime":                            "test",
			},
		},
	}
	for _, test := range tests {
		got := readCorosyncRuntime(test.cscommand)
		want := test.want

		if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
			t.Errorf("readCorosyncRuntime returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	}
}

func TestReadCorosyncConfig(t *testing.T) {
	tests := []struct {
		file   string
		reader ConfigFileReader
		want   map[string]string
	}{
		{
			file:   "",
			reader: DefaultTestReader,
			want: map[string]string{
				"token":                               "",
				"token_retransmits_before_loss_const": "",
				"consensus":                           "",
				"join":                                "",
				"max_messages":                        "",
				"transport":                           "",
				"fail_recv_const":                     "",
				"two_node":                            "",
			},
		},
		{
			file: `token: test1 test2 test3
join : join_value
token_retransmits_before_loss_const: test_retransmit
two_node: true
`,
			reader: DefaultTestReader,
			want: map[string]string{
				"token":                               "test1",
				"token_retransmits_before_loss_const": "test_retransmit",
				"consensus":                           "",
				"join":                                "",
				"max_messages":                        "",
				"transport":                           "",
				"fail_recv_const":                     "",
				"two_node":                            "true",
			},
		},
	}

	for _, test := range tests {
		got := readCorosyncConfig(test.reader, test.file)

		if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("readCorosyncConfig returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	}
}

func TestReadCorosyncConfigWithErrors(t *testing.T) {
	tests := []struct {
		file   string
		reader ConfigFileReader
		want   map[string]string
	}{
		{
			file:   "",
			reader: DefaultTestReaderError,
			want: map[string]string{
				"token":                               "",
				"token_retransmits_before_loss_const": "",
				"consensus":                           "",
				"join":                                "",
				"max_messages":                        "",
				"transport":                           "",
				"fail_recv_const":                     "",
				"two_node":                            "",
			},
		},
	}

	for _, test := range tests {
		got := readCorosyncConfig(test.reader, test.file)

		if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("readCorosyncConfig returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	}
}

func TestSetConfigMapValueForLine(t *testing.T) {
	tests := []struct {
		line string
		keys map[string]string
		want map[string]string
	}{
		{
			line: "",
			keys: map[string]string{
				"test": "",
			},
			want: map[string]string{
				"test": "",
			},
		},
		{
			line: "param:  value asdf",
			keys: map[string]string{
				"param": "",
			},
			want: map[string]string{
				"param": "value",
			},
		},
		{
			line: "short:    ",
			keys: map[string]string{
				"short": "",
			},
			want: map[string]string{
				"short": "",
			},
		},
	}

	for _, test := range tests {
		config := test.keys
		setConfigMapValueForLine(config, test.line)
		got := config

		if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("setConfigMapValueForLine returned unexpected metric labels diff (-want +got):\n%s", diff)
		}
	}
}

func TestCollectCorosyncMetrics(t *testing.T) {
	tests := []struct {
		name               string
		runtimeOS          string
		wantOsVersion      string
		wantCorosyncExists float64
	}{
		{
			name:               "linuxHasMetrics",
			runtimeOS:          "linux",
			wantOsVersion:      "test-os-version",
			wantCorosyncExists: float64(1.0),
		},
		{
			name:               "windowsHasMetrics",
			runtimeOS:          "windows",
			wantOsVersion:      "microsoft_windows_server_2019_datacenter-10.0.17763",
			wantCorosyncExists: float64(0.0),
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
			runtimeOS := test.runtimeOS
			want := wantWindowsCorosyncMetrics(nts, test.wantCorosyncExists, test.wantOsVersion)
			if runtimeOS != "windows" {
				want = wantLinuxCorosyncMetrics(nts, test.wantCorosyncExists, test.wantOsVersion)
			}
			cch := make(chan WorkloadMetrics)
			p := Parameters{
				Config:               cnf,
				ConfigFileReader:     DefaultTestReader,
				CommandRunnerNoSpace: commandlineexecutor.ExecuteCommand,
				OSType:               runtimeOS,
			}
			go CollectCorosyncMetrics(p, cch, csConfigPath)
			got := <-cch
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}
