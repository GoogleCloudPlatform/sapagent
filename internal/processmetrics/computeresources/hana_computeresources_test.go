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

package computeresources

import (
	"context"
	"testing"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var (
	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:       false,
			ProcessMetricsFrequency:     5,
			ProcessMetricsSendFrequency: 60,
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
	defaultSAPInstanceHANA = &sapb.SAPInstance{
		Type: sapb.InstanceType_HANA,
	}
	defaultSapControlOutputHANA = `OK
		0 name: hdbdaemon
		0 dispstatus: GREEN
		0 pid: 111
		1 name: hdbcompileserver
		1 dispstatus: GREEN
		1 pid: 222`
)

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestCollectForHANA(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		skippedMetrics map[string]bool
		executor       commandlineexecutor.Execute
		fakeClient     sapcontrolclienttest.Fake
		wantCount      int
		wantErr        error
		lastValue      map[string]*process.IOCountersStat
	}{
		{
			name:       "EmptyPIDsMapWebmethod",
			config:     defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{},
			wantCount:  0,
		},
		{
			name:   "OnlyMemoryPerProcessMetricAvailableWebmethod",
			config: defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbcompileserver", "SAPControl-GREEN", 222},
				},
			},
			skippedMetrics: map[string]bool{
				hanaCPUPath:        true,
				hanaIOPSReadsPath:  true,
				hanaIOPSWritesPath: true,
			},
			lastValue: make(map[string]*process.IOCountersStat),
			wantCount: 3,
		},
		{
			name:   "OnlyCPUPerProcessMetricAvailableWebmethod",
			config: defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"enserver", "SAPControl-GREEN", 333},
				},
			},
			skippedMetrics: map[string]bool{
				hanaMemoryPath:     true,
				hanaIOPSReadsPath:  true,
				hanaIOPSWritesPath: true,
			},
			lastValue: make(map[string]*process.IOCountersStat),
			wantCount: 1,
		},
		{
			name:   "FetchedAllMetricsSuccessfullyWebmethod",
			config: defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbdaemon", "SAPControl-GREEN", 555},
					{"hdbcompileserver", "SAPControl-GREEN", 666},
				},
			},
			lastValue: map[string]*process.IOCountersStat{
				"hdbdaemon:555": &process.IOCountersStat{
					ReadCount:  1,
					WriteCount: 1,
				},
				"hdbcompileserver:666": &process.IOCountersStat{
					ReadCount:  1,
					WriteCount: 1,
				},
			},
			wantCount: 12,
		},
		{
			name: "MetricsSkipped",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       false,
					ProcessMetricsFrequency:     5,
					ProcessMetricsSendFrequency: 60,
					ProcessMetricsToSkip:        []string{hanaCPUPath, hanaMemoryPath, hanaIOPSReadsPath, hanaIOPSWritesPath},
				},
			},
			skippedMetrics: map[string]bool{
				hanaCPUPath:        true,
				hanaMemoryPath:     true,
				hanaIOPSReadsPath:  true,
				hanaIOPSWritesPath: true,
			},
			wantCount:  0,
			lastValue:  make(map[string]*process.IOCountersStat),
			fakeClient: sapcontrolclienttest.Fake{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testHanaInstanceProps := &HanaInstanceProperties{
				Config:           test.config,
				Client:           &fake.TimeSeriesCreator{},
				Executor:         test.executor,
				SAPInstance:      defaultSAPInstanceHANA,
				NewProcHelper:    newProcessWithContextHelperTest,
				SAPControlClient: test.fakeClient,
				LastValue:        test.lastValue,
				SkippedMetrics:   test.skippedMetrics,
			}
			got, err := testHanaInstanceProps.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() = %d , want %d", len(got), test.wantCount)
			}

			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Collect() = %v, want %v", err, test.wantErr)
			}

			for _, metric := range got {
				points := metric.GetPoints()
				if points[0].GetValue().GetDoubleValue() < 0 {
					t.Errorf("Metric value for compute resources cannot be negative.")
				}
			}
		})
	}
}

func TestCollectWithRetryHANA(t *testing.T) {
	c := context.Background()
	hp := &HanaInstanceProperties{
		Config:           defaultConfig,
		Client:           &fake.TimeSeriesCreator{},
		Executor:         commandlineexecutor.ExecuteCommand,
		SAPInstance:      defaultSAPInstanceHANA,
		NewProcHelper:    newProcessWithContextHelperTest,
		SAPControlClient: sapcontrolclienttest.Fake{},
		PMBackoffPolicy:  defaultBOPolicy(c),
	}
	_, err := hp.CollectWithRetry(c)
	if err != nil {
		t.Errorf("CollectWithRetry() = %v, want nil", err)
	}
}
