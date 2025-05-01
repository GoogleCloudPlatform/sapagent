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

package networkstats

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
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

	fakeTimestamp = &tspb.Timestamp{
		Seconds: 42,
	}
)

func returnExecutor(out, err string) commandlineexecutor.Execute {
	return func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
		fmt.Println(out, err)
		return commandlineexecutor.Result{
			StdOut: out,
			StdErr: err,
			Error:  errors.New(err),
		}
	}
}

// ssOutput returns ssOutput as received from ss command.
func ssOutput(strs ...string) string {
	return strings.Join(strs, "\n") + "\n"
}

func TestFetchHDBSocket(t *testing.T) {
	tests := []struct {
		name    string
		p       *Properties
		wantOut string
		wantErr error
	}{
		{
			name: "CNF1",
			p: &Properties{
				Config:   defaultConfig,
				Executor: returnExecutor("", "sudo: lsof: command not found"),
			},
			wantOut: "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CNF2",
			p: &Properties{
				Config:   defaultConfig,
				Executor: returnExecutor("", "sudo: ss: command not found"),
			},
			wantOut: "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			p: &Properties{
				Config:   defaultConfig,
				Executor: returnExecutor("*:30013", ""),
			},
			wantOut: "*:30013",
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, err := test.p.fetchHDBSocket(ctx)

			if d := cmp.Diff(test.wantOut, out); d != "" {
				t.Errorf("fetchHDBSocket() mismatch in out (-want, +got):\n%s", d)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("fetchHDBSocket() error: got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestFetchSSOutput(t *testing.T) {
	tests := []struct {
		name   string
		p      *Properties
		socket string
		want   string
	}{
		{
			name: "Success1",
			p: &Properties{
				Config: defaultConfig,
				Executor: returnExecutor(ssOutput(
					"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
				), ""),
			},
			socket: "*:30013",
			want: ssOutput(
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
			),
		},
		{
			name: "Success2",
			p: &Properties{
				Config: defaultConfig,
				Executor: returnExecutor(ssOutput(
					"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
					"ESTAB    0         0                127.0.0.1:30013          127.0.0.1:55494",
					"\t cubic wscale:7,7 rto:204 rtt:0.017/0.008 send 154202352941bps lastsnd:28 lastrcv:28 lastack:28 pacing_rate 306153576640bps delivered:3 app_limited rcv_space:65483 minrtt:0.015",
				), ""),
			},
			socket: "*:30013",
			want: ssOutput(
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
				"ESTAB    0         0                127.0.0.1:30013          127.0.0.1:55494",
				"\t cubic wscale:7,7 rto:204 rtt:0.017/0.008 send 154202352941bps lastsnd:28 lastrcv:28 lastack:28 pacing_rate 306153576640bps delivered:3 app_limited rcv_space:65483 minrtt:0.015",
			),
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.p.fetchSSOutput(ctx, test.socket)
			if got != test.want {
				t.Errorf("fetchSSOutput(%v) = %v, want: %v", test.socket, got, test.want)
			}
		})
	}
}

func TestMapValues(t *testing.T) {
	tests := []struct {
		name    string
		metrics []string
		want    map[string]string
	}{
		{
			name:    "SampleTest1",
			metrics: []string{"wscale:7,7", "rto:204", "rtt:0.027/0.021", "ato:40", "mss:32768", "pmtu:65535", "rcvmss:552", "advmss:65483", "cwnd:10", "ssthresh:42", "bytes_sent:1884"},
			want: map[string]string{
				"wscale":     "7,7",
				"rto":        "204",
				"rtt":        "0.027/0.021",
				"ato":        "40",
				"mss":        "32768",
				"pmtu":       "65535",
				"rcvmss":     "552",
				"advmss":     "65483",
				"cwnd":       "10",
				"ssthresh":   "42",
				"bytes_sent": "1884",
			},
		},
		{
			name:    "SampleTest2",
			metrics: []string{"delivery_rate 2352bps", "send 523bsp", "metric1 233409458ps", "metric2 42434ps"},
			want: map[string]string{
				"delivery_rate": "2352bps",
				"send":          "523bsp",
				"metric1":       "233409458ps",
				"metric2":       "42434ps",
			},
		},
		{
			name:    "FaultyTest1",
			metrics: []string{"wscale:7,7", "rto:204", "rtt:0.027/0.021", "faultymMetric"},
			want: map[string]string{
				"wscale": "7,7",
				"rto":    "204",
				"rtt":    "0.027/0.021",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ssMap := mapValues(test.metrics)

			if d := cmp.Diff(test.want, ssMap); d != "" {
				t.Errorf("mapValues() mismatch in metricTS (-want, +got):\n%s", d)
			}
		})
	}
}

func TestCreateTSList(t *testing.T) {
	tests := []struct {
		name       string
		p          *Properties
		pid        string
		t          string
		reqMetrics []string
		ssMap      map[string]string
		wantTS     []*mrpb.TimeSeries
		wantErr    error
	}{
		{
			name: "SampleFloatTest",
			p: &Properties{
				Config: defaultConfig,
			},
			pid:        "20210",
			t:          "float64",
			reqMetrics: []string{"rtt"},
			ssMap: map[string]string{
				"rtt": "0.027/0.021",
			},
			wantTS: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/rtt",
						Labels: map[string]string{
							"name":    "rtt",
							"process": "hdbnameserver",
							"pid":     "20210",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{
									DoubleValue: 0.027,
								},
							},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "SampleIntTest",
			p: &Properties{
				Config: defaultConfig,
			},
			pid:        "20210",
			t:          "int64",
			reqMetrics: []string{"lastsnd"},
			ssMap: map[string]string{
				"lastsnd": "234159",
			},
			wantTS: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/lastsnd",
						Labels: map[string]string{
							"name":    "lastsnd",
							"process": "hdbnameserver",
							"pid":     "20210",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 234159,
								},
							},
						},
					},
				},
			},
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			TSList, err := test.p.createTSList(context.Background(), test.pid, test.reqMetrics, test.ssMap, test.t)

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if d := cmp.Diff(test.wantTS, TSList, cmpOpts...); d != "" {
				t.Errorf("createTSList() mismatch (-want, +got):\n%s", d)
			}
			if err != nil {
				t.Errorf("createTSList() failed: %v", err)
			}
		})
	}
}

func TestCollectTCPMetrics(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		pid    string
		data   metricVal
		p      *Properties
		want   []*mrpb.TimeSeries
	}{
		{
			name:   "SampleFloat64Test",
			metric: "rtt",
			pid:    "20210",
			data: metricVal{
				val:  0.2341,
				Type: "float64",
			},
			p: &Properties{
				Config: defaultConfig,
			},
			want: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/rtt",
						Labels: map[string]string{
							"name":    "rtt",
							"process": "hdbnameserver",
							"pid":     "20210",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_DoubleValue{
									DoubleValue: 0.2341,
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "SampleInt64Test",
			metric: "lastsnd",
			pid:    "20210",
			data: metricVal{
				val:  int64(23451),
				Type: "int64",
			},
			p: &Properties{
				Config: defaultConfig,
			},
			want: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/lastsnd",
						Labels: map[string]string{
							"name":    "lastsnd",
							"process": "hdbnameserver",
							"pid":     "20210",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 23451,
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
			metrics := test.p.collectTCPMetrics(context.Background(), test.metric, test.pid, test.data)

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if d := cmp.Diff(test.want, metrics, cmpOpts...); d != "" {
				t.Errorf("collectTCPMetrics() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestCreateMetric(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		data   metricVal
		p      *Properties
		want   *mrpb.TimeSeries
	}{
		{
			name: "SampleFloat64Test",
			labels: map[string]string{
				"name":    "rtt",
				"process": "hdbnameserver",
				"pid":     "20210",
			},
			data: metricVal{
				val:  float64(0.2341),
				Type: "float64",
			},
			p: &Properties{
				Config: defaultConfig,
			},
			want: &mrpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type: "workload.googleapis.com/sap/networkstats/rtt",
					Labels: map[string]string{
						"name":    "rtt",
						"process": "hdbnameserver",
						"pid":     "20210",
					},
				},
				MetricKind: metricpb.MetricDescriptor_GAUGE,
				Points: []*mrpb.Point{
					{
						Value: &cpb.TypedValue{
							Value: &cpb.TypedValue_DoubleValue{
								DoubleValue: 0.2341,
							},
						},
					},
				},
			},
		},
		{
			name: "SampleInt64Test",
			labels: map[string]string{
				"name":    "lastsnd",
				"process": "hdbnameserver",
				"pid":     "20210",
			},
			data: metricVal{
				val:  int64(23451),
				Type: "int64",
			},
			p: &Properties{
				Config: defaultConfig,
			},
			want: &mrpb.TimeSeries{
				Metric: &metricpb.Metric{
					Type: "workload.googleapis.com/sap/networkstats/lastsnd",
					Labels: map[string]string{
						"name":    "lastsnd",
						"process": "hdbnameserver",
						"pid":     "20210",
					},
				},
				MetricKind: metricpb.MetricDescriptor_GAUGE,
				Points: []*mrpb.Point{
					{
						Value: &cpb.TypedValue{
							Value: &cpb.TypedValue_Int64Value{
								Int64Value: 23451,
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metric := test.p.createMetric(test.labels, test.data)

			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
			}
			if d := cmp.Diff(test.want, metric, cmpOpts...); d != "" {
				t.Errorf("createMetric() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestParseSSOutput(t *testing.T) {
	input := []struct {
		name        string
		ssOutput    string
		wantMetrics []string
	}{
		{
			name: "NoTCPMetrics",
			ssOutput: ssOutput(
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
			),
			wantMetrics: nil,
		},
		{
			name: "TCPOutput",
			ssOutput: ssOutput(
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
				"ESTAB    0         0                127.0.0.1:30013          127.0.0.1:55494",
				"\t cubic wscale:7,7 rto:204 rtt:0.017/0.008 send 154202352941bps lastsnd:28 lastrcv:28 lastack:28 pacing_rate 306153576640bps delivered:3 app_limited rcv_space:65483 minrtt:0.015",
			),
			wantMetrics: []string{"wscale:7,7", "rto:204", "rtt:0.017/0.008", "send 154202352941bps", "lastsnd:28", "lastrcv:28", "lastack:28", "pacing_rate 306153576640bps", "delivered:3", "rcv_space:65483", "minrtt:0.015"},
		},
	}
	for _, test := range input {
		t.Run(test.name, func(t *testing.T) {
			list := parseSSOutput(context.Background(), test.ssOutput)
			if diff := cmp.Diff(test.wantMetrics, list); diff != "" {
				t.Fatalf("parseSSOutput with argstosplit returned unexpected diff in metrics List(-want +got):\n%s", diff)
			}
		})
	}
}
