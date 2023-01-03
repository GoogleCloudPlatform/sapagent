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
	"os"
	"reflect"
	"testing"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	cfgpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
)

var (
	//go:embed test_data/metricoverride.yaml
	sampleOverride string
	//go:embed test_data/metricoverride.yaml
	sampleOverrideFS embed.FS
)

func TestCollectMetrics_hasMetrics(t *testing.T) {
	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	want := 5
	p := Parameters{
		Config:               &cfgpb.Configuration{},
		CommandRunner:        func(string, string) (string, string, error) { return "", "", nil },
		CommandRunnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
		ConfigFileReader:     DefaultTestReader,
		OSStatReader:         func(data string) (os.FileInfo, error) { return nil, nil },
		OSType:               "linux",
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
		CommandRunner:        func(string, string) (string, string, error) { return "", "", nil },
		CommandRunnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
		ConfigFileReader:     DefaultTestReader,
		OSStatReader:         func(data string) (os.FileInfo, error) { return nil, nil },
		CommandExistsRunner:  func(string) bool { return false },
		OSType:               "windows",
	}
	wantOSVersion := "microsoft_windows_server_2019_datacenter-10.0.17763"
	wantTimestamp := &timestamppb.Timestamp{Seconds: now()}

	want := wantSystemMetrics(wantTimestamp, wantOSVersion)
	for _, metric := range []string{"corosync", "hana", "netweaver", "pacemaker"} {
		wantMetric := wantSystemMetrics(wantTimestamp, wantOSVersion).Metrics[0]
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
		Config:               &cfgpb.Configuration{},
		CommandRunner:        func(string, string) (string, string, error) { return "", "", nil },
		CommandRunnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
		ConfigFileReader: ConfigFileReader(func(path string) (io.ReadCloser, error) {
			file, err := sampleOverrideFS.Open(path)
			var f io.ReadCloser = file
			return f, err
		}),
		OSStatReader: func(data string) (os.FileInfo, error) {
			f, err := sampleOverrideFS.Open(data)
			if err != nil {
				return nil, err
			}
			return f.Stat()
		},
		OSType: "linux",
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sendMetrics(context.Background(), test.workLoadMetrics, "test-project", &test.client)
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
			name: "succeeds",
			params: Parameters{
				Config: &cfgpb.Configuration{
					CollectionConfiguration: &cfgpb.CollectionConfiguration{
						CollectWorkloadValidationMetrics: true,
					}},
				CommandRunner:        func(string, string) (string, string, error) { return "", "", nil },
				CommandRunnerNoSpace: func(string, ...string) (string, string, error) { return "", "", nil },
				ConfigFileReader:     DefaultTestReader,
				OSStatReader:         func(data string) (os.FileInfo, error) { return nil, nil },
				TimeSeriesCreator:    &fake.TimeSeriesCreator{},
				OSType:               "linux",
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
				OSType: "linux",
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
				OSType: "windows",
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
