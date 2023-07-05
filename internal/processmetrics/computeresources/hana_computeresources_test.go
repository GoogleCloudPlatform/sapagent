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

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
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

func TestCollectForHANA(t *testing.T) {
	tests := []struct {
		name            string
		executor        commandlineexecutor.Execute
		useSAPControlAPI bool
		fakeClient      sapcontrolclienttest.Fake
		wantCount       int
	}{
		{
			name: "EmptyPIDsMap",
			executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			wantCount: 0,
		},
		{
			name:            "EmptyPIDsMapWebmethod",
			useSAPControlAPI: true,
			fakeClient:      sapcontrolclienttest.Fake{},
			wantCount:       0,
		},
		{
			name: "OnlyMemoryPerProcessMetricAvailable",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultSapControlOutputHANA,
				}
			},
			wantCount: 3,
		},
		{
			name:            "OnlyMemoryPerProcessMetricAvailableWebmethod",
			useSAPControlAPI: true,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbdaemon", "SAPControl-GREEN", 111},
					{"hdbcompileserver", "SAPControl-GREEN", 222},
				},
			},
			wantCount: 3,
		},
		{
			name: "OnlyCPUPerProcessMetricAvailable",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `OK
					0 name: msg_server
					0 dispstatus: GREEN
					0 pid: 111
					1 name: enserver
					1 dispstatus: GREEN
					1 pid: 333
					`,
				}
			},
			wantCount: 1,
		},
		{
			name:            "OnlyCPUPerProcessMetricAvailableWebmethod",
			useSAPControlAPI: true,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"msg_server", "SAPControl-GREEN", 111},
					{"enserver", "SAPControl-GREEN", 333},
				},
			},
			wantCount: 1,
		},
		{
			name: "FetchedBothMemoryAndCPUMetricsSuccessfully",
			executor: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `OK
					0 name: hdbdaemon
					0 dispstatus: GREEN
					0 pid: 444
					1 name: hdbcompileserver
					1 dispstatus: GREEN
					1 pid: 555
					`,
				}
			},
			wantCount: 8,
		},
		{
			name:            "FetchedBothMemoryAndCPUMetricsSuccessfullyWebmethod",
			useSAPControlAPI: true,
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					{"hdbdaemon", "SAPControl-GREEN", 444},
					{"hdbcompileserver", "SAPControl-GREEN", 555},
				},
			},
			wantCount: 8,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testHanaInstanceProps := &HanaInstanceProperties{
				Config:           defaultConfig,
				Client:           &fake.TimeSeriesCreator{},
				Executor:         test.executor,
				SAPInstance:      defaultSAPInstanceHANA,
				NewProcHelper:    newProcessWithContextHelperTest,
				SAPControlClient: test.fakeClient,
				UseSAPControlAPI:  test.useSAPControlAPI,
			}
			got := testHanaInstanceProps.Collect(context.Background())
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
