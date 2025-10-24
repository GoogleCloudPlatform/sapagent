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

package processmetrics

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/configurablemetrics"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

type fakeDiscoveryInterface struct {
	systems   []*spb.SapDiscovery
	instances *sapb.SAPInstances
}

func (d *fakeDiscoveryInterface) GetSAPSystems() []*spb.SapDiscovery  { return d.systems }
func (d *fakeDiscoveryInterface) GetSAPInstances() *sapb.SAPInstances { return d.instances }

var (
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:        "test-project",
		InstanceId:       "test-instance",
		Zone:             "test-zone",
		InstanceName:     "test-instance",
		Image:            "test-image",
		NumericProjectId: "123456",
	}

	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       true,
			ProcessMetricsFrequency:     5,
			SlowProcessMetricsFrequency: 30,
		},
		CloudProperties: defaultCloudProperties,
	}

	quickTestConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       true,
			ProcessMetricsFrequency:     1, // Use small value for quick unit tests.
			SlowProcessMetricsFrequency: 6,
		},
		CloudProperties: defaultCloudProperties,
	}

	invalidSlowFrequencyTestConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       true,
			ProcessMetricsFrequency:     5,
			SlowProcessMetricsFrequency: 1,
		},
		CloudProperties: defaultCloudProperties,
	}

	defaultBackOffIntervals = cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond)
)

type (
	fakeProperties struct {
		Config *cpb.Configuration
		Client cloudmonitoring.TimeSeriesCreator
	}

	fakeCollector struct {
		timeSeriesCount             int
		timesCollectWithRetryCalled int
		m                           *sync.Mutex
	}

	fakeCollectorError struct {
	}

	fakeCollectorErrorWithTimeSeries struct {
		timeSeriesCount int
	}

	mockFileInfo struct {
	}
)

// Functions for mockFileInfo to implement a mock os.FileInfo object.
func (m *mockFileInfo) Name() string       { return "mock_file" }
func (m *mockFileInfo) Size() int64        { return 0 }
func (m *mockFileInfo) Mode() os.FileMode  { return os.ModePerm }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() any           { return nil }

func (f *fakeCollector) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	m := make([]*mrpb.TimeSeries, f.timeSeriesCount)
	for i := 0; i < f.timeSeriesCount; i++ {
		m[i] = &mrpb.TimeSeries{}
	}
	return m, nil
}

func (f *fakeCollector) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	if f.m != nil {
		f.m.Lock()
		f.timesCollectWithRetryCalled++
		f.m.Unlock()
	} else {
		f.timesCollectWithRetryCalled++
	}
	m := make([]*mrpb.TimeSeries, f.timeSeriesCount)
	for i := 0; i < f.timeSeriesCount; i++ {
		m[i] = &mrpb.TimeSeries{}
	}
	return m, nil
}

func (f *fakeCollectorError) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	return nil, cmpopts.AnyError
}

func (f *fakeCollectorError) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	return nil, cmpopts.AnyError
}

func (f *fakeCollectorErrorWithTimeSeries) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	m := make([]*mrpb.TimeSeries, f.timeSeriesCount)
	for i := 0; i < f.timeSeriesCount; i++ {
		m[i] = &mrpb.TimeSeries{}
	}
	return m, cmpopts.AnyError
}

func (f *fakeCollectorErrorWithTimeSeries) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	m := make([]*mrpb.TimeSeries, f.timeSeriesCount)
	for i := 0; i < f.timeSeriesCount; i++ {
		m[i] = &mrpb.TimeSeries{}
	}
	return m, cmpopts.AnyError
}

func fakeCollectors(count, timeSerisCountPerCollector int) []Collector {
	collectors := make([]Collector, count)
	for i := 0; i < count; i++ {
		collectors[i] = &fakeCollector{timeSeriesCount: timeSerisCountPerCollector}
	}
	return collectors
}

func fakeNewMetricClient(ctx context.Context, opts ...option.ClientOption) (cloudmonitoring.TimeSeriesCreator, error) {
	return &fake.TimeSeriesCreatorThreadSafe{}, nil
}

func fakeNewMetricClientFailure(ctx context.Context, opts ...option.ClientOption) (cloudmonitoring.TimeSeriesCreator, error) {
	return nil, cmpopts.AnyError
}

func fakeSAPInstances(app string) *sapb.SAPInstances {
	switch app {
	case "HANA":
		return &sapb.SAPInstances{
			Instances: []*sapb.SAPInstance{
				&sapb.SAPInstance{
					Type:   sapb.InstanceType_HANA,
					Sapsid: "DEH",
				},
			},
		}
	case "HANACluster":
		return &sapb.SAPInstances{
			Instances: []*sapb.SAPInstance{
				&sapb.SAPInstance{
					Type:   sapb.InstanceType_HANA,
					Sapsid: "DVA",
				},
			},
			LinuxClusterMember: true,
		}
	case "NetweaverCluster":
		return &sapb.SAPInstances{
			Instances: []*sapb.SAPInstance{
				&sapb.SAPInstance{
					Type:   sapb.InstanceType_NETWEAVER,
					Sapsid: "AEK",
				},
			},
			LinuxClusterMember: true,
		}
	case "TwoNetweaverInstancesOnSameMachine":
		return &sapb.SAPInstances{
			Instances: []*sapb.SAPInstance{
				&sapb.SAPInstance{
					Type:   sapb.InstanceType_NETWEAVER,
					Sapsid: "AEK",
				}, &sapb.SAPInstance{
					Type:   sapb.InstanceType_NETWEAVER,
					Sapsid: "AEK",
				},
			},
			LinuxClusterMember: true,
		}
	default:
		return nil
	}
}

// The goal of these unit tests is to test the interaction of this package with respective collectors.
// This assumes that the collector is tested by its own unit tests.
func TestStartProcessMetrics(t *testing.T) {
	tests := []struct {
		name       string
		parameters Parameters
		want       bool
	}{
		{
			name: "SuccessEnabled",
			parameters: Parameters{
				Config:       defaultConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: true,
		},
		{
			name: "FailsDisabled",
			parameters: Parameters{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						CollectProcessMetrics: false,
					},
				},
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: false,
		},
		{
			name: "FailsForWindowsOS",
			parameters: Parameters{
				Config:       defaultConfig,
				OSType:       "windows",
				BackOffs:     defaultBackOffIntervals,
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: false,
		},
		{
			name: "InvalidProcessMetricFrequency",
			parameters: Parameters{
				Config:       quickTestConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: false,
		},
		{
			name: "InvalidProcessMetricFrequencyForSlowMetrics",
			parameters: Parameters{
				Config:       invalidSlowFrequencyTestConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: false,
		},
		{
			name: "CreateMetricClientFailure",
			parameters: Parameters{
				Config:       defaultConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClientFailure,
				BackOffs:     defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
				OSStatReader: func(data string) (os.FileInfo, error) { return nil, nil },
			},
			want: false,
		},
		{
			name: "DemoCollectionMode",
			parameters: Parameters{
				Config:       defaultConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				OSStatReader: func(data string) (os.FileInfo, error) {
					return &mockFileInfo{}, nil
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := startProcessMetrics(ctx, test.parameters)
			if got != test.want {
				t.Errorf("StartProcessMetrics(%v), got: %t want: %t", test.parameters, got, test.want)
			}
		})
	}
}

func TestCreateProcessCollectors(t *testing.T) {
	tests := []struct {
		name                   string
		sapInstances           *sapb.SAPInstances
		wantCollectorCount     int
		wantFastCollectorCount int
		params                 Parameters
	}{
		{
			name:                   "HANAStandaloneInstance",
			sapInstances:           fakeSAPInstances("HANA"),
			wantCollectorCount:     10,
			wantFastCollectorCount: 1,
			params: Parameters{
				Config: defaultConfig,
			},
		},
		{
			name:                   "HANAClusterInstance",
			sapInstances:           fakeSAPInstances("HANACluster"),
			wantCollectorCount:     10,
			wantFastCollectorCount: 1,
			params: Parameters{
				Config: defaultConfig,
			},
		},
		{
			name:                   "NetweaverClusterInstance",
			sapInstances:           fakeSAPInstances("NetweaverCluster"),
			wantCollectorCount:     10,
			wantFastCollectorCount: 1,
			params: Parameters{
				Config: defaultConfig,
			},
		},
		{
			name:                   "TwoNetweaverInstancesOnSameMachine",
			sapInstances:           fakeSAPInstances("TwoNetweaverInstancesOnSameMachine"),
			wantCollectorCount:     13,
			wantFastCollectorCount: 2,
			params: Parameters{
				Config: defaultConfig,
			},
		},
		{
			name:                   "NonNilWorkloadConfig",
			sapInstances:           fakeSAPInstances("TwoNetweaverInstancesOnSameMachine"),
			wantCollectorCount:     14,
			wantFastCollectorCount: 2,
			params: Parameters{
				Config: defaultConfig,
				PCMParams: pacemaker.Parameters{
					Config: defaultConfig,
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
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := createProcessCollectors(context.Background(), test.params, &fake.TimeSeriesCreatorThreadSafe{}, test.sapInstances)

			if len(got.Collectors) != test.wantCollectorCount {
				t.Errorf("createProcessCollectors() returned %d collectors, want %d", len(got.Collectors), test.wantCollectorCount)
			}
			if len(got.FastMovingCollectors) != test.wantFastCollectorCount {
				t.Errorf("createProcessCollectors() returned %d fast collectors, want %d", len(got.FastMovingCollectors), test.wantFastCollectorCount)
			}
		})
	}
}

func createFakeMetrics(count int) []*mrpb.TimeSeries {
	var metrics []*mrpb.TimeSeries

	for i := 0; i < count; i++ {
		metrics = append(metrics, &mrpb.TimeSeries{})
	}
	return metrics
}

func TestCollectAndSendFastMovingMetrics(t *testing.T) {
	tests := []struct {
		name       string
		properties *Properties
		runtime    time.Duration
		want       error
	}{
		{
			name: "TenCollectorsRunForFiveSeconds",
			properties: &Properties{
				Client:               &fake.TimeSeriesCreatorThreadSafe{},
				FastMovingCollectors: fakeCollectors(10, 1),
				Config:               quickTestConfig,
			},
			runtime: 5 * time.Second,
		},
		{
			name: "ZeroCollectors",
			properties: &Properties{
				Client:               &fake.TimeSeriesCreatorThreadSafe{},
				FastMovingCollectors: nil,
				Config:               quickTestConfig,
			},
			runtime: 2 * time.Second,
			want:    cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), test.runtime)
			defer cancel()

			got := test.properties.collectAndSendFastMovingMetrics(ctx, defaultBackOffIntervals)

			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("Failure in collectAndSendFastMovingMetrics(), got: %v want: %v.", got, test.want)
			}
		})
	}
}

func TestCollectAndSendSlowMovingMetricsOnce(t *testing.T) {
	tests := []struct {
		name           string
		properties     *Properties
		collector      Collector
		wantSent       int
		wantBatchCount int
		wantErr        error
	}{
		{
			name: "CollectorSuccess",
			properties: &Properties{
				Client:     &fake.TimeSeriesCreatorThreadSafe{},
				Collectors: fakeCollectors(10, 1),
				Config:     quickTestConfig,
			},
			collector: &fakeCollector{
				timeSeriesCount: 10,
			},
			wantSent:       10,
			wantBatchCount: 1,
		},
		{
			name: "CollectorFailure",
			properties: &Properties{
				Client:     &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
				Collectors: fakeCollectors(1, 1),
				Config:     quickTestConfig,
			},
			collector:      &fakeCollectorError{},
			wantErr:        cmpopts.AnyError,
			wantBatchCount: 0,
		},
		{
			name: "CollectorFailureWithSomeTimeSeriesData",
			properties: &Properties{
				Client:     &fake.TimeSeriesCreatorThreadSafe{},
				Collectors: fakeCollectors(10, 1),
				Config:     quickTestConfig,
			},
			collector: &fakeCollectorErrorWithTimeSeries{
				timeSeriesCount: 10,
			},
			wantErr:        nil,
			wantSent:       10,
			wantBatchCount: 1,
		},
		{
			name: "SendFailure",
			properties: &Properties{
				Client:     &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
				Collectors: fakeCollectors(1, 1),
				Config:     quickTestConfig,
			},
			collector: &fakeCollector{
				timeSeriesCount: 10,
			},
			wantErr:        cmpopts.AnyError,
			wantBatchCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotSent, gotBatchCount, gotErr := collectAndSendSlowMovingMetricsOnce(context.Background(), test.properties, test.collector, defaultBackOffIntervals)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Failure in collectAndSendOnce(), gotErr: %v wantErr: %v.", gotErr, test.wantErr)
			}

			if gotBatchCount != test.wantBatchCount {
				t.Errorf("Failure in collectAndSendOnce(), gotBatchCount: %v wantBatchCount: %v.",
					gotBatchCount, test.wantBatchCount)
			}

			if gotSent != test.wantSent {
				t.Errorf("Failure in collectAndSendOnce(), gotSent: %v wantSent: %v.", gotSent, test.wantSent)
			}
		})
	}
}

func TestCollectAndSendOnceFastMovingMetrics(t *testing.T) {
	tests := []struct {
		name           string
		properties     *Properties
		wantSent       int
		wantBatchCount int
		wantErr        error
	}{
		{
			name: "ThreeCollectorsSuccess",
			properties: &Properties{
				Client:               &fake.TimeSeriesCreatorThreadSafe{},
				FastMovingCollectors: fakeCollectors(3, 1),
				Config:               quickTestConfig,
			},
			wantSent:       3,
			wantBatchCount: 1,
		},
		{
			name: "SendFailure",
			properties: &Properties{
				Client:               &fake.TimeSeriesCreator{Err: cmpopts.AnyError},
				FastMovingCollectors: fakeCollectors(1, 1),
				Config:               quickTestConfig,
			},
			wantErr:        cmpopts.AnyError,
			wantBatchCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotSent, gotBatchCount, gotErr := test.properties.collectAndSendFastMovingMetricsOnce(context.Background(), defaultBackOffIntervals)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Failure in collectAndSendOnceFastMovingMetrics(), gotErr: %v wantErr: %v.", gotErr, test.wantErr)
			}

			if gotBatchCount != test.wantBatchCount {
				t.Errorf("Failure in collectAndSendOnceFastMovingMetrics(), gotBatchCount: %v wantBatchCount: %v.",
					gotBatchCount, test.wantBatchCount)
			}

			if gotSent != test.wantSent {
				t.Errorf("Failure in collectAndSendOnceFastMovingMetrics(), gotSent: %v wantSent: %v.", gotSent, test.wantSent)
			}
		})
	}
}

func TestInstancesWithCredentials(t *testing.T) {
	tests := []struct {
		name   string
		params *Parameters
		want   *sapb.SAPInstances
	}{
		{
			name: "CredentialsSetWithPassword",
			params: &Parameters{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						HanaMetricsConfig: &cpb.HANAMetricsConfig{
							HanaDbUser:     "test-db-user",
							HanaDbPassword: "test-pass",
						},
					},
				},
				BackOffs: defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
			},
			want: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{
						Type:           sapb.InstanceType_HANA,
						HanaDbUser:     "test-db-user",
						HanaDbPassword: "test-pass",
						Sapsid:         "DEH",
					},
				},
			},
		},
		{
			name: "CredentialsSetWithHDBUserstore",
			params: &Parameters{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						HanaMetricsConfig: &cpb.HANAMetricsConfig{
							HanaDbUser:      "test-db-user",
							HdbuserstoreKey: "test-key",
						},
					},
				},
				BackOffs: defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
			},
			want: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{
						Type:            sapb.InstanceType_HANA,
						HanaDbUser:      "test-db-user",
						HdbuserstoreKey: "test-key",
						Sapsid:          "DEH",
					},
				},
			},
		},
		{
			name: "CredentialsNotSet",
			params: &Parameters{
				Config:   quickTestConfig,
				BackOffs: defaultBackOffIntervals,
				Discovery: &fakeDiscoveryInterface{
					instances: fakeSAPInstances("HANA"),
				},
			},
			want: fakeSAPInstances("HANA"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			got := instancesWithCredentials(context.Background(), test.params)

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("instancesWithCredentials() returned diff (-want +got):\n%s", diff)
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
			name:         "cancel before beat",
			beatInterval: time.Millisecond * 200,
			timeout:      time.Millisecond * 100,
			want:         0,
		},
		{
			name:         "1 beat timeout",
			beatInterval: time.Millisecond * 75,
			timeout:      time.Millisecond * 100,
			want:         1,
		},
		{
			name:         "2 beat timeout",
			beatInterval: time.Millisecond * 45,
			timeout:      time.Millisecond * 130,
			want:         2,
		},
	}
	for _, test := range testData {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, test.timeout)
			defer cancel()
			got := 0
			lock := sync.Mutex{}
			parameters := Parameters{
				Config:       defaultConfig,
				OSType:       "linux",
				MetricClient: fakeNewMetricClient,
				BackOffs:     defaultBackOffIntervals,
				HeartbeatSpec: &heartbeat.Spec{
					BeatFunc: func() {
						lock.Lock()
						defer lock.Unlock()
						got++
					},
					Interval: test.beatInterval,
				},
			}
			properties := createProcessCollectors(context.Background(), parameters, &fake.TimeSeriesCreatorThreadSafe{}, fakeSAPInstances("HANA"))
			properties.collectAndSendFastMovingMetrics(ctx, defaultBackOffIntervals)
			<-ctx.Done()
			lock.Lock()
			defer lock.Unlock()
			if got != test.want {
				t.Errorf("collectAndSendFastMovingMetrics() heartbeat mismatch got %d, want %d", got, test.want)
			}
		})
	}
}

func TestCollectAndSendSlowMovingMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := sync.Mutex{}
	c := &fakeCollector{
		timeSeriesCount: 10,
		m:               &m,
	}
	p := &Properties{
		Client:     &fake.TimeSeriesCreatorThreadSafe{},
		Collectors: []Collector{c},
		Config:     quickTestConfig,
	}
	wp := workerpool.New(1)
	wp.Submit(func() {
		collectAndSendSlowMovingMetrics(ctx, p, c, defaultBackOffIntervals, wp)
	})

	// Wait for some iterations
	time.Sleep(time.Duration(p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()) * time.Millisecond * 2500)
	m.Lock()
	before := c.timesCollectWithRetryCalled
	m.Unlock()
	cancel()

	// Wait some more to ensure workers have stopped
	time.Sleep(time.Duration(p.Config.GetCollectionConfiguration().GetSlowProcessMetricsFrequency()) * time.Second * 2)
	m.Lock()
	after := c.timesCollectWithRetryCalled
	m.Unlock()

	if before != after {
		t.Errorf("collectAndSendSlowMovingMetrics() timesCalled mismatch got %d, want %d", after, before)
	}
}

func TestSkipMetricsForNetweaverKernel(t *testing.T) {
	tests := []struct {
		name           string
		Discovery      discoveryInterface
		skippedMetrics map[string]bool
		want           map[string]bool
	}{
		{
			name: "AffectedKernelVersion794",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 794 Patch 000",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion753",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 753 Patch 122",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion777",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 777 Patch 612",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion789",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 789 Patch 200",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion754",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 754 Patch 22",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion791",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 791 Patch 040",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion792",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 792 Patch 005",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion785",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 785 Patch 312",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "AffectedKernelVersion793",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 793 Patch 059",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want: map[string]bool{
				"/sap/nw/abap/sessions": true,
				"/sap/nw/abap/rfc":      true,
			},
		},
		{
			name: "KernelVersionNotAffected1",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 794 Patch 007",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{"another/metric": true},
			want:           map[string]bool{"another/metric": true},
		},
		{
			name: "KernelVersionNotAffected2",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
									KernelVersion: "SAP Kernel 794 Patch 009",
								},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{"another/metric": true},
			want:           map[string]bool{"another/metric": true},
		},
		{
			name: "KernelVersionNotSet",
			Discovery: &fakeDiscoveryInterface{
				systems: []*spb.SapDiscovery{
					{
						ApplicationLayer: &spb.SapDiscovery_Component{
							Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
								ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{},
							},
						},
					},
				},
			},
			skippedMetrics: map[string]bool{},
			want:           map[string]bool{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			skipMetricsForNetweaverKernel(context.Background(), test.Discovery, test.skippedMetrics)
			if diff := cmp.Diff(test.want, test.skippedMetrics); diff != "" {
				t.Errorf("skipMetricsForNetweaverKernel() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtractKernelVersionAndPatch(t *testing.T) {
	tests := []struct {
		name          string
		kernelVersion string
		wantKernel    int
		wantPatch     int
		wantErr       error
	}{
		{
			name:          "ValidKernelVersion",
			kernelVersion: "SAP Kernel 794 Patch 003",
			wantKernel:    794,
			wantPatch:     3,
		},
		{
			name:          "ValidKernelVersion2",
			kernelVersion: "SAP Kernel 753 Patch 1224",
			wantKernel:    753,
			wantPatch:     1224,
		},
		{
			name:          "InvalidKernelVersion",
			kernelVersion: "SAP Kernel NA Patch 007",
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "InvalidPatch",
			kernelVersion: "SAP Kernel 794 Patch NA",
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "InvalidKernelVersionWithLargeKernel",
			kernelVersion: "SAP Kernel 794000000000000000000000 Patch 9",
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "InvalidKernelVersionWithLargePatch",
			kernelVersion: "SAP Kernel 794 Patch 900000000000000000000000",
			wantErr:       cmpopts.AnyError,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		gotKernel, gotPatch, err := extractKernelVersionAndPatch(ctx, tc.kernelVersion)
		if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
			t.Errorf("extractKernelVersionAndPatch(%v) returned an unexpected error: %v, want: %v", tc.kernelVersion, err, tc.wantErr)
			continue
		}

		if gotKernel != tc.wantKernel {
			t.Errorf("extractKernelVersionAndPatch(%v) = %v, want: %v", tc.kernelVersion, gotKernel, tc.wantKernel)
		}
		if gotPatch != tc.wantPatch {
			t.Errorf("extractKernelVersionAndPatch(%v) = %v, want: %v", tc.kernelVersion, gotPatch, tc.wantPatch)
		}
	}
}
