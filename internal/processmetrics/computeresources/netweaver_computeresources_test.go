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

	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
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
		name          string
		config        *cpb.Configuration
		executor      commandlineexecutor.Execute
		fakeClient    sapcontrolclienttest.Fake
		wantCount     int
		processParams commandlineexecutor.Params
		lastValue     map[string]*process.IOCountersStat
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
					{"hdbdaemon", "SAPControl-GREEN", 111},
					{"hdbcompileserver", "SAPControl-GREEN", 222},
				},
			},
			lastValue: make(map[string]*process.IOCountersStat),
			wantCount: 3,
		},
		{
			name:   "OnlyCPUPerProcessMetricAvailableWebmethod",
			config: defaultConfig,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"msg_server", "SAPControl-GREEN", 111},
					{"enserver", "SAPControl-GREEN", 333},
				},
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
			}, wantCount: 0,
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
				SAPControlClient:        test.fakeClient,
			}

			got := testNetweaverInstanceProperties.Collect(context.Background())
			if len(got) != test.wantCount {
				t.Errorf("Collect() = %d , want %d", len(got), test.wantCount)
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
