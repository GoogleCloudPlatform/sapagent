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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var (
	defaultSAPInstanceNetWeaver = &sapb.SAPInstance{
		Type: sapb.InstanceType_NETWEAVER,
	}
	defaultSapControlOutputNetWeaver = `OK
		0 name: msg_server
		0 dispstatus: GREEN
		0 pid: 111
		1 name: enserver
		1 dispstatus: GREEN
		1 pid: 222`
)

func TestCollectForNetweaver(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		executor       commandlineexecutor.Execute
		skippedMetrics map[string]bool
		fakeClient     sapcontrolclienttest.Fake
		wantCount      int
		wantErr        error
		processParams  commandlineexecutor.Params
		lastValue      map[string]*process.IOCountersStat
	}{
		{
			name:       "EmptyPIDsMapWebmethod",
			config:     defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{},
			lastValue:  make(map[string]*process.IOCountersStat),
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
				nwCPUPath:       true,
				nwIOPSReadsPath: true,
				nwIOPSWritePath: true,
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
				nwMemoryPath:    true,
				nwIOPSReadsPath: true,
				nwIOPSWritePath: true,
			},
			lastValue: make(map[string]*process.IOCountersStat),
			wantCount: 1,
		},
		{
			name: "MetricsSkipped",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       false,
					ProcessMetricsFrequency:     5,
					ProcessMetricsSendFrequency: 60,
					ProcessMetricsToSkip:        []string{nwCPUPath, nwMemoryPath, nwIOPSReadsPath, nwIOPSWritePath},
				},
			},
			skippedMetrics: map[string]bool{
				nwCPUPath:       true,
				nwMemoryPath:    true,
				nwIOPSReadsPath: true,
				nwIOPSWritePath: true,
			},
			wantCount:  0,
			lastValue:  make(map[string]*process.IOCountersStat),
			fakeClient: sapcontrolclienttest.Fake{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testNetweaverInstanceProperties := &NetweaverInstanceProperties{
				Config:                  test.config,
				Client:                  &fake.TimeSeriesCreator{},
				Executor:                test.executor,
				SAPInstance:             defaultSAPInstanceNetWeaver,
				LastValue:               test.lastValue,
				NewProcHelper:           newProcessWithContextHelperTest,
				SAPControlProcessParams: test.processParams,
				SkippedMetrics:          test.skippedMetrics,
				SAPControlClient:        test.fakeClient,
			}

			got, err := testNetweaverInstanceProperties.Collect(context.Background())
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

func TestCollectWithRetryNW(t *testing.T) {
	c := context.Background()
	nwp := &NetweaverInstanceProperties{
		Config:        defaultConfig,
		Client:        &fake.TimeSeriesCreator{},
		Executor:      commandlineexecutor.ExecuteCommand,
		NewProcHelper: newProcessWithContextHelperTest,
		SAPInstance:   defaultSAPInstanceNetWeaver,
		SAPControlClient: sapcontrolclienttest.Fake{
			Processes: []sapcontrolclient.OSProcess{
				{"msg_server", "SAPControl-GREEN", 111},
			},
		},
		PMBackoffPolicy: defaultBOPolicy(c),
	}
	_, err := nwp.CollectWithRetry(c)
	if err == nil {
		t.Errorf("CollectWithRetry() = %v, want error", err)
	}
}
