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
	"embed"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	//go:embed test_data/metricoverride.yaml
	sampleOverride string
	//go:embed test_data/metricoverride.yaml test_data/os-release.txt test_data/os-release-bad.txt test_data/os-release-empty.txt
	testFS                  embed.FS
	defaultBackOffIntervals = cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond)
	DefaultTestReader       = ConfigFileReader(func(data string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(data)), nil
	})
)

func TestSetOSReleaseInfo(t *testing.T) {
	defaultFileReader := ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := testFS.Open(path)
		var f io.ReadCloser = file
		return f, err
	})

	tests := []struct {
		name        string
		filePath    string
		reader      ConfigFileReader
		wantID      string
		wantVersion string
	}{
		{
			name:        "Success",
			filePath:    "test_data/os-release.txt",
			reader:      defaultFileReader,
			wantID:      "debian",
			wantVersion: "11",
		},
		{
			name:        "ConfigFileReaderNil",
			filePath:    "test_data/os-release.txt",
			reader:      nil,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "OSReleaseFilePathEmpty",
			filePath:    "",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:     "FileReadError",
			filePath: "test_data/os-release.txt",
			reader: ConfigFileReader(func(path string) (io.ReadCloser, error) {
				return nil, errors.New("File Read Error")
			}),
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "FileParseError",
			filePath:    "test_data/os-release-bad.txt",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
		{
			name:        "FieldsEmpty",
			filePath:    "test_data/os-release-empty.txt",
			reader:      defaultFileReader,
			wantID:      "",
			wantVersion: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := Parameters{
				OSReleaseFilePath: test.filePath,
				ConfigFileReader:  test.reader,
			}
			params.SetOSReleaseInfo()
			if params.osVendorID != test.wantID {
				t.Errorf("SetOSReleaseInfo() unexpected osVendorID, got %q want %q", params.osVendorID, test.wantID)
			}
			if params.osVersion != test.wantVersion {
				t.Errorf("SetOSReleaseInfo() unexpected osVersion, got %q want %q", params.osVersion, test.wantVersion)
			}
		})
	}
}

func TestCollectMetricsFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		params   Parameters
		override string
		want     WorkloadMetrics
	}{
		{
			name: "HasMetricOverride",
			params: Parameters{
				Config: defaultConfiguration,
				OSStatReader: func(data string) (os.FileInfo, error) {
					f, err := testFS.Open(data)
					if err != nil {
						return nil, err
					}
					return f.Stat()
				},
				ConfigFileReader: ConfigFileReader(func(path string) (io.ReadCloser, error) {
					file, err := testFS.Open(path)
					var f io.ReadCloser = file
					return f, err
				}),
			},
			override: "test_data/metricoverride.yaml",
			want: WorkloadMetrics{Metrics: append(createTimeSeries(
				"workload.googleapis.com/sap/validation/system",
				map[string]string{"blank_metric": "", "metric_with_colons": "val1:val2:val3", "os": "rhel-8.4"},
				1.0,
				defaultConfiguration,
			), createTimeSeries(
				"workload.googleapis.com/sap/validation/hana",
				map[string]string{"hana_metric": "/hana/log"},
				0.0,
				defaultConfiguration,
			)...)},
		},
		{
			name: "HasWorkloadConfig",
			params: Parameters{
				Config: defaultConfiguration,
				WorkloadConfig: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "agent",
								},
								Value: wlmpb.SystemVariable_AGENT_NAME,
							},
						},
					},
				},
				Exists: func(string) bool { return false },
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{}
				},
				OSStatReader:       func(data string) (os.FileInfo, error) { return nil, nil },
				InstanceInfoReader: *instanceinfo.New(&fakeDiskMapper{}, defaultGCEService),
			},
			want: WorkloadMetrics{Metrics: []*monitoringresourcespb.TimeSeries{
				createTimeSeries(
					"workload.googleapis.com/sap/validation/system",
					map[string]string{"agent": "sapagent"},
					1.0,
					defaultConfiguration,
				)[0],
				createTimeSeries(
					"workload.googleapis.com/sap/validation/corosync",
					map[string]string{"agent": "sapagent"},
					1.0,
					defaultConfiguration,
				)[0],
				createTimeSeries(
					"workload.googleapis.com/sap/validation/hana",
					map[string]string{"agent": "sapagent"},
					0.0,
					defaultConfiguration,
				)[0],
				createTimeSeries(
					"workload.googleapis.com/sap/validation/netweaver",
					map[string]string{"agent": "sapagent"},
					0.0,
					defaultConfiguration,
				)[0],
				createTimeSeries(
					"workload.googleapis.com/sap/validation/pacemaker",
					map[string]string{"agent": "sapagent"},
					0.0,
					defaultConfiguration,
				)[0],
				createTimeSeries(
					"workload.googleapis.com/sap/validation/custom",
					map[string]string{"agent": "sapagent"},
					1.0,
					defaultConfiguration,
				)[0],
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectMetricsFromConfig(context.Background(), test.params, test.override)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectMetricsFromConfig() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectMetrics_hasMetrics(t *testing.T) {
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	want := 5
	p := Parameters{
		Config: &cfgpb.Configuration{},
		Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  nil,
			}
		},
		Exists:           func(string) bool { return true },
		ConfigFileReader: DefaultTestReader,
		OSStatReader:     func(data string) (os.FileInfo, error) { return nil, nil },
		OSType:           "linux",
		BackOffs:         defaultBackOffIntervals,
	}
	m := collectMetrics(context.Background(), p, metricOverridePath)
	got := len(m.Metrics)
	if got != want {
		t.Errorf("collectMetrics returned unexpected metric count, got: %d, want: %d", got, want)
	}
}

func TestCollectMetrics_systemLabelsAppend(t *testing.T) {
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	defaultDiskMapper = &fakeDiskMapper{err: nil, out: "disk-mapping"}
	defaultNetworkIP = "127.0.0.1"
	p := Parameters{
		Config: &cfgpb.Configuration{
			CloudProperties: &iipb.CloudProperties{
				InstanceName: "test-instance-name",
				InstanceId:   "test-instance-id",
				Zone:         "test-zone",
				ProjectId:    "test-project-id",
			},
			AgentProperties: &cfgpb.AgentProperties{Version: "1.0"},
		},
		ConfigFileReader: DefaultTestReader,
		OSStatReader:     func(data string) (os.FileInfo, error) { return nil, nil },
		Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		Exists:             func(string) bool { return false },
		OSType:             "windows",
		InstanceInfoReader: *instanceinfo.New(defaultDiskMapper, defaultGCEService),
		BackOffs:           defaultBackOffIntervals,
	}
	wantOSVersion := "microsoft_windows_server_2019_datacenter-10.0.17763"
	wantTimestamp := &timestamppb.Timestamp{Seconds: now()}

	agentServiceStatus = func(context.Context, string) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "Running",
			StdErr: "",
		}
	}
	cmdExists = func(c string) bool {
		return true
	}
	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1, ip2}, nil
	}
	osCaptionExecute = func(context.Context) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "\n\nCaption=Microsoft Windows Server 2019 Datacenter \n   \n    \n",
			StdErr: "",
		}
	}
	osVersionExecute = func(context.Context) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			StdOut: "\n\nVersion=10.0.17763  \n\n",
			StdErr: "",
		}
	}

	labels := make(map[string]string)
	for k, v := range defaultLabels {
		labels[k] = v
	}
	labels["os"] = wantOSVersion
	want := wantSystemMetrics(wantTimestamp, labels)
	for _, metric := range []string{"corosync", "hana", "netweaver", "pacemaker"} {
		wantMetric := wantSystemMetrics(wantTimestamp, labels).Metrics[0]
		wantMetric.Metric.Type = "workload.googleapis.com/sap/validation/" + metric
		wantMetric.Points[0].Value.Value = &cpb.TypedValue_DoubleValue{DoubleValue: 0}
		want.Metrics = append(want.Metrics, wantMetric)
	}

	got := collectMetrics(context.Background(), p, metricOverridePath)
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("System labels were not properly appended (-want +got):\n%s", diff)
	}
}

func TestCollectMetrics_hasOverrideMetrics(t *testing.T) {
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	want := 2
	p := Parameters{
		Config: &cfgpb.Configuration{},
		Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		Exists: func(string) bool { return true },
		ConfigFileReader: ConfigFileReader(func(path string) (io.ReadCloser, error) {
			file, err := testFS.Open(path)
			var f io.ReadCloser = file
			return f, err
		}),
		OSStatReader: func(data string) (os.FileInfo, error) {
			f, err := testFS.Open(data)
			if err != nil {
				return nil, err
			}
			return f.Stat()
		},
		OSType:   "linux",
		BackOffs: defaultBackOffIntervals,
	}
	m := collectMetrics(context.Background(), p, "test_data/metricoverride.yaml")
	got := len(m.Metrics)
	if got != want {
		t.Errorf("collectMetrics_hasOverrideMetrics returned unexpected metric count, got: %d, want: %d", got, want)
	}
}

func TestOverrideMetrics(t *testing.T) {
	tests := []struct {
		file   string
		reader ConfigFileReader
		want   WorkloadMetrics
	}{
		{
			file:   "",
			reader: DefaultTestReader,
			want:   WorkloadMetrics{},
		},
		{
			file: sampleOverride,
			reader: ConfigFileReader(func(data string) (io.ReadCloser, error) {
				return nil, errors.New("failed to read file")
			}),
			want: WorkloadMetrics{},
		},
		{
			file:   sampleOverride,
			reader: DefaultTestReader,
			want: WorkloadMetrics{Metrics: append(createTimeSeries(
				"workload.googleapis.com/sap/validation/system",
				map[string]string{"blank_metric": "", "metric_with_colons": "val1:val2:val3", "os": "rhel-8.4"},
				1.0,
				&cfgpb.Configuration{},
			), createTimeSeries(
				"workload.googleapis.com/sap/validation/hana",
				map[string]string{"hana_metric": "/hana/log"},
				.0,
				&cfgpb.Configuration{},
			)...)},
		},
	}

	for _, test := range tests {
		got := collectOverrideMetrics(&cfgpb.Configuration{}, test.reader, test.file)

		if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("collectOverrideMetrics returned unexpected metrics diff (-want +got):\n%s", diff)
		}
	}
}

func TestSendMetrics(t *testing.T) {
	tests := []struct {
		name            string
		client          cloudmonitoring.TimeSeriesCreator
		workLoadMetrics WorkloadMetrics
		wantMetricCount int
	}{
		{
			name:            "succeedsWithZeroMetrics",
			client:          &fake.TimeSeriesCreator{},
			workLoadMetrics: WorkloadMetrics{},
			wantMetricCount: 0,
		},
		{
			name:   "succeedsWithMetrics",
			client: &fake.TimeSeriesCreator{},
			workLoadMetrics: WorkloadMetrics{Metrics: []*monitoringresourcespb.TimeSeries{{
				Metric:   &metricpb.Metric{},
				Resource: &monitoredresourcepb.MonitoredResource{},
				Points:   []*monitoringresourcespb.Point{},
			}}},
			wantMetricCount: 1,
		},
		{
			name:   "failsWithMetrics",
			client: &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
			workLoadMetrics: WorkloadMetrics{Metrics: []*monitoringresourcespb.TimeSeries{{
				Metric:   &metricpb.Metric{},
				Resource: &monitoredresourcepb.MonitoredResource{},
				Points:   []*monitoringresourcespb.Point{},
			}}},
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sendMetrics(context.Background(), test.workLoadMetrics, "test-project", &test.client, defaultBackOffIntervals)
			if got != test.wantMetricCount {
				t.Errorf("sendMetrics returned unexpected metric count for %s, got: %d, want: %d",
					test.name, got, test.wantMetricCount)
			}
		})
	}
}

func TestAppendLabels(t *testing.T) {
	s := make(map[string]string)
	s["skey"] = "svalue"
	got := make(map[string]string)
	got["dkey"] = "dvalue"
	want := make(map[string]string)
	want["dkey"] = "dvalue"
	want["skey"] = "svalue"

	appendLabels(got, s)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendLabels maps are not equal, got: %v, want: %v", got, want)
	}
}

func TestStartMetricsCollection(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		os     string
		want   bool
	}{
		{
			name: "succeedsForLocal",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
					}},
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
					}
				},
				Exists:            func(string) bool { return true },
				ConfigFileReader:  DefaultTestReader,
				OSStatReader:      func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				OSType:            "linux",
				Remote:            false,
				BackOffs:          defaultBackOffIntervals,
			},
			want: true,
		},
		{
			name: "succeedsForRemote",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
					}},
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
					}
				},
				Exists:            func(string) bool { return true },
				ConfigFileReader:  DefaultTestReader,
				OSStatReader:      func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				OSType:            "linux",
				Remote:            true,
				BackOffs:          defaultBackOffIntervals,
			},
			want: true,
		},
		{
			name: "failsDueToParams",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: false,
					}},
				OSType:   "linux",
				BackOffs: defaultBackOffIntervals,
			},
			want: false,
		},
		{
			name: "failsDueToOS",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
					}},
				OSType:   "windows",
				BackOffs: defaultBackOffIntervals,
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := StartMetricsCollection(context.Background(), test.params)
			if got != test.want {
				t.Errorf("StartMetricsCollection(%#v) returned unexpected result, got: %t, want: %t", test.params, got, test.want)
			}
		})
	}
}

func TestCollectAndSend_shouldBeatAccordingToHeartbeatSpec(t *testing.T) {
	testData := []struct {
		name         string
		beatInterval time.Duration
		timeout      time.Duration
		want         int
	}{
		{
			name:         "CancelBeforeInitialCollection",
			beatInterval: time.Second,
			timeout:      time.Second * 0,
			want:         0,
		},
		{
			name:         "CancelBeforeBeat",
			beatInterval: time.Second * 1,
			timeout:      time.Millisecond * 50,
			want:         1,
		},
		{
			name:         "Cancel1Beat",
			beatInterval: time.Millisecond * 75,
			timeout:      time.Millisecond * 140,
			want:         2,
		},
		{
			name:         "Cancel2Beats",
			beatInterval: time.Millisecond * 45,
			timeout:      time.Millisecond * 125,
			want:         3,
		},
	}
	for _, test := range testData {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, test.timeout)
			defer cancel()
			got := 0
			lock := sync.Mutex{}
			params := Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
					}},
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
					}
				},
				Exists:            func(string) bool { return true },
				ConfigFileReader:  DefaultTestReader,
				OSStatReader:      func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator: &fake.TimeSeriesCreator{},
				OSType:            "linux",
				Remote:            false,
				BackOffs:          defaultBackOffIntervals,
				HeartbeatSpec: &heartbeat.Spec{
					BeatFunc: func() {
						lock.Lock()
						defer lock.Unlock()
						got++
					},
					Interval: test.beatInterval,
				},
			}

			StartMetricsCollection(ctx, params)

			<-ctx.Done()
			lock.Lock()
			defer lock.Unlock()
			if got != test.want {
				t.Errorf("collectAndSend() heartbeat mismatch got %d, want %d", got, test.want)
			}
		})
	}
}
