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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

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

// ssOutput returns ssOutput as received from ss command.
func ssOutput(strs ...string) string {
	return strings.Join(strs, "\n") + "\n"
}

func TestCollect(t *testing.T) {
	tests := []struct {
		name    string
		p       Properties
		wantTS  []*mrpb.TimeSeries
		wantErr bool
	}{
		{
			name: "SampleTest",
			p: Properties{
				Executor: commandlineexecutor.ExecuteCommand,
				Config:   defaultConfig,
				CommandParams: commandlineexecutor.Params{
					Executable:  "bash",
					ArgsToSplit: "-c 'nc -l 5060 & (: echo dummyMessage | nc -w 5 -v localhost 5060 & (sleep 1;echo 23456;ss -tin src *:5060) & (sleep 10; pkill nc))'",
				},
				Client: &fake.TimeSeriesCreator{},
			},
			wantTS: []*mrpb.TimeSeries{
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/rtt",
						Labels: map[string]string{
							"name":    "rtt",
							"process": "hdbnameserver",
							"pid":     "23456",
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
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/lastsnd",
						Labels: map[string]string{
							"name":    "lastsnd",
							"process": "hdbnameserver",
							"pid":     "23456",
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
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/lastrcv",
						Labels: map[string]string{
							"name":    "lastrcv",
							"process": "hdbnameserver",
							"pid":     "23456",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 23415,
								},
							},
						},
					},
				},
				{
					Metric: &metricpb.Metric{
						Type: "workload.googleapis.com/sap/networkstats/rto",
						Labels: map[string]string{
							"name":    "rto",
							"process": "hdbnameserver",
							"pid":     "23456",
						},
					},
					MetricKind: metricpb.MetricDescriptor_GAUGE,
					Points: []*mrpb.Point{
						{
							Value: &cpb.TypedValue{
								Value: &cpb.TypedValue_Int64Value{
									Int64Value: 23415,
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "FaultyTest1",
			p: Properties{
				Executor: commandlineexecutor.ExecuteCommand,
				Config:   defaultConfig,
				CommandParams: commandlineexecutor.Params{
					Executable:  "bash",
					ArgsToSplit: "-c 'nsc -l 5060 & (: echo dummyMessage | nc -w 5 -v localhost 5060 & (echo 23456;ss -tin src *:5060))'",
				},
				Client: &fake.TimeSeriesCreator{},
			},
			wantTS:  nil,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metricTS, err := test.p.Collect(context.Background())

			sortProtos := cmpopts.SortSlices(func(m1, m2 *mrpb.TimeSeries) bool { return m1.String() < m2.String() })
			cmpOpts := []cmp.Option{
				protocmp.Transform(),

				// These fields get generated on the backend, and it'd make a messy test to check them all.
				protocmp.IgnoreFields(&cpb.TimeInterval{}, "end_time", "start_time"),
				protocmp.IgnoreFields(&mrpb.Point{}, "interval"),
				protocmp.IgnoreFields(&mrpb.Point{}, "value"),
				protocmp.IgnoreFields(&mrpb.TimeSeries{}, "resource"),
				protocmp.IgnoreFields(&monitoredresourcepb.MonitoredResource{}, "labels", "type"),
				sortProtos,
			}
			if d := cmp.Diff(test.wantTS, metricTS, cmpOpts...); d != "" {
				t.Errorf("Collect() mismatch in metricTS (-want, +got):\n%s", d)
			}
			if gotErr := (err != nil); gotErr != test.wantErr {
				t.Errorf("Collect() mismatch in err\nError:%s", err)
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
		wantPID     string
		wantMetrics []string
	}{
		{
			name: "NoPID1",
			ssOutput: ssOutput(
				"",
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
			),
			wantPID:     "",
			wantMetrics: nil,
		},
		{
			name: "NoPID2",
			ssOutput: ssOutput(
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
			),
			wantPID:     "",
			wantMetrics: nil,
		},
		{
			name: "NoTCPMetrics",
			ssOutput: ssOutput(
				"20210",
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
			),
			wantPID:     "",
			wantMetrics: nil,
		},
		{
			name: "TCPOutput",
			ssOutput: ssOutput(
				"20210",
				"State    Recv-Q    Send-Q       Local Address:Port        Peer Address:Port      ",
				"ESTAB    0         0                127.0.0.1:30013          127.0.0.1:55494",
				"\t cubic wscale:7,7 rto:204 rtt:0.017/0.008 send 154202352941bps lastsnd:28 lastrcv:28 lastack:28 pacing_rate 306153576640bps delivered:3 app_limited rcv_space:65483 minrtt:0.015",
			),
			wantPID:     "20210",
			wantMetrics: []string{"wscale:7,7", "rto:204", "rtt:0.017/0.008", "send 154202352941bps", "lastsnd:28", "lastrcv:28", "lastack:28", "pacing_rate 306153576640bps", "delivered:3", "rcv_space:65483", "minrtt:0.015"},
		},
	}
	for _, test := range input {
		t.Run(test.name, func(t *testing.T) {
			pid, list := parseSSOutput(context.Background(), test.ssOutput)
			if pid != test.wantPID {
				t.Fatalf("parseSSOutput with argstosplit returned unexpected diff in pid\nwant:%s\ngot:%s", test.wantPID, pid)
			}
			if diff := cmp.Diff(test.wantMetrics, list); diff != "" {
				t.Fatalf("parseSSOutput with argstosplit returned unexpected diff in metrics List(-want +got):\n%s", diff)
			}
		})
	}
}
