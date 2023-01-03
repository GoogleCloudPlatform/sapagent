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

package netweaver

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sap-agent/protos/sapapp"
)

var (
	defaultSAPInstance = &sapb.SAPInstance{
		Sapsid:            "TST",
		InstanceNumber:    "00",
		InstanceId:        "D00",
		ServiceName:       "test-service",
		Type:              sapb.InstanceType_NETWEAVER,
		NetweaverHttpPort: "1234",
	}

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

	defaultInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultSAPInstance,
	}

	defaultSapControlOutputAppSrv = `OK
		0 name: msg_server
		0 dispstatus: GREEN
		0 pid: 111
		1 name: enserver
		1 dispstatus: GREEN
		1 pid: 222
		2 name: enrepserver
		2 dispstatus: GREEN
		2 pid: 333
		3 name: disp+work
		3 dispstatus: GREEN
		3 pid: 444
		4 name: gwrd
		4 dispstatus: GREEN
		4 pid: 555
		5 name: icman
		5 dispstatus: GREEN
		5 pid: 666`

	defaultSapControlOutputJava = `OK
		0 name: msg_server
		0 dispstatus: GREEN
		0 pid: 111
		1 name: enserver
		1 dispstatus: GREEN
		1 pid: 222
		2 name: enrepserver
		2 dispstatus: GREEN
		2 pid: 333
		3 name: jstart
		3 dispstatus: GREEN
		3 pid: 444
		4 name: jcontrol
		4 dispstatus: GREEN
		4 pid: 555`
)

type fakeRunner struct {
	stdOut, stdErr string
	exitCode       int
	err            error
}

func (f *fakeRunner) RunWithEnv() (string, string, int, error) {
	return f.stdOut, f.stdErr, f.exitCode, f.err
}

func TestNWAvailabilityValue(t *testing.T) {
	tests := []struct {
		name             string
		fRunner          sapcontrol.RunnerWithEnv
		wantAvailability int64
	}{
		{
			name: "SapControlFailsTwoProcesses",
			fRunner: &fakeRunner{
				stdOut: `0 name: msg_server
				0 description: Message Server
				0 dispstatus: GREEN
				0 pid: 111
				1 name: enserver
				1 description: EN Server
				1 dispstatus: RED
				1 pid: 222`,
				exitCode: 1,
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSucceedsAppSrv",
			fRunner: &fakeRunner{
				stdOut:   defaultSapControlOutputAppSrv,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSucceedsJava",
			fRunner: &fakeRunner{
				stdOut:   defaultSapControlOutputJava,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSuccessMsg",
			fRunner: &fakeRunner{
				stdOut: `0 name: msg_server
				0 description: msg_server
				0 dispstatus: GREEN
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsEnServer",
			fRunner: &fakeRunner{
				stdOut: `0 name: enserver
				0 description: enserver
				0 dispstatus: RED
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlFailEnRepServer",
			fRunner: &fakeRunner{
				stdOut: `0 name: enrepserver
				0 description: enrepserver
				0 dispstatus: RED
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSuccessEnRepServer",
			fRunner: &fakeRunner{
				stdOut: `0 name: enrepserver
				0 description: enrepserver
				0 dispstatus: GREEN
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlFailsAppSrv",
			fRunner: &fakeRunner{
				stdOut: `0 name: gwrd
				0 description: GWRD
				0 dispstatus: RED
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlFailsJava",
			fRunner: &fakeRunner{
				stdOut: `0 name: jcontrol
				0 description: Java
				0 dispstatus: RED
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAtLeastOneProcessNotGreen,
		},
		{
			name: "SapControlSuccessJava",
			fRunner: &fakeRunner{
				stdOut: `0 name: jstart
				0 description: Java
				0 dispstatus: GREEN
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "SapControlSuccessAppSrv",
			fRunner: &fakeRunner{
				stdOut: `0 name: icman
				0 description: ICMAN
				0 dispstatus: GREEN
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
		{
			name: "InvalidProcess",
			fRunner: &fakeRunner{
				stdOut: `0 name: invalidproc
				0 description: INVALIDPROC
				0 dispstatus: RED
				0 pid: 111`,
				exitCode: 1,
			},
			wantAvailability: systemAllProcessesGreen,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sc := &sapcontrol.Properties{
				Instance: defaultSAPInstance,
			}
			procs, _, err := sc.ProcessList(test.fRunner)
			if err != nil {
				t.Errorf("ProcessList() failed with: %v.", err)
			}
			_, gotAvailability := collectServiceMetrics(defaultInstanceProperties, procs, timestamppb.Now())
			if gotAvailability != test.wantAvailability {
				t.Errorf("Failure in readNetWeaverProcessStatus(), gotAvailability: %d wantAvailability: %d.",
					gotAvailability, test.wantAvailability)
			}
		})
	}
}

func TestNWServiceMetricLabelCount(t *testing.T) {
	// NOTE: metricLabels applies two labels by default
	tests := []struct {
		name        string
		extraLabels map[string]string
		wantLabels  int
	}{
		{
			name:        "DefaultLabels",
			extraLabels: map[string]string{},
			wantLabels:  2,
		},
		{
			name: "TwoExtraLabels",
			extraLabels: map[string]string{
				"a": "test",
				"b": "test_2",
			},
			wantLabels: 4,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			labels := metricLabels(defaultInstanceProperties, test.extraLabels)

			if len(labels) != test.wantLabels {
				t.Errorf("metricLabels(%q) mismatch, got: %v want: %v.", test.extraLabels, len(labels), test.wantLabels)
			}
		})
	}
}

func TestCollectNetWeaverMetrics(t *testing.T) {
	var (
		fRunner = &fakeRunner{
			stdOut:   defaultSapControlOutputJava,
			exitCode: 1,
		}
		wantMetricCount = 6
	)

	metrics := collectNetWeaverMetrics(defaultInstanceProperties, fRunner)
	if len(metrics) != wantMetricCount {
		t.Errorf("collectNetWeaverMetrics() metric count mismatch, got: %v want: %v.", len(metrics), wantMetricCount)
	}
}

func TestCollect(t *testing.T) {
	// Production API returns no metrics in unit test setup.
	metrics := defaultInstanceProperties.Collect()
	if len(metrics) != 0 {
		t.Errorf("Collect() metric count mismatch, got: %v want: 0.", len(metrics))
	}
}

func TestCollectHTTPMetrics(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		emptyURL    bool
		wantCount   int
	}{
		{
			name:        "ICMServer",
			serviceName: "SAP-ICM-ABAP",
			wantCount:   2,
		},
		{
			name:        "MessageServer",
			serviceName: "SAP-CS",
			wantCount:   2,
		},
		{
			name:        "UnknownServer",
			serviceName: "SAP-XYZ",
			wantCount:   0,
		},
		{
			name:      "EmptyURL",
			emptyURL:  true,
			wantCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			defer ts.Close()

			url := ts.URL
			if test.emptyURL {
				url = ""
			}

			p := &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					ServiceName:             test.serviceName,
					NetweaverHealthCheckUrl: url,
				},
			}

			got := collectHTTPMetrics(p)
			if len(got) != test.wantCount {
				t.Errorf("collectHTTPMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}

		})
	}
}

func TestCollectICMPMetrics(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantCount int
	}{
		{
			name:      "Success",
			wantCount: 2,
		},
		{
			name:      "InvalidURL",
			url:       "InvalidURL",
			wantCount: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			defer ts.Close()

			url := ts.URL
			if test.url != "" {
				url = test.url
			}

			got := collectICMMetrics(defaultInstanceProperties, url)
			if len(got) != test.wantCount {
				t.Errorf("collectICMMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectMessageServerMetrics(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		responseBody string
		statusCode   int
		wantCount    int
	}{
		{
			name:      "InvalidURL",
			url:       "InvalidURL",
			wantCount: 0,
		},
		{
			name:       "HTTPGETFailure",
			statusCode: http.StatusInternalServerError,
			wantCount:  2,
		},
		{
			name:         "Success",
			responseBody: `DIAG    testInstance.c.sap-calm.internal 3202    LB=10`,
			statusCode:   http.StatusOK,
			wantCount:    3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.statusCode)
				fmt.Fprint(w, test.responseBody)
			}))
			defer ts.Close()

			url := ts.URL
			if test.url != "" {
				url = test.url
			}

			got := collectMessageServerMetrics(defaultInstanceProperties, url)
			if len(got) != test.wantCount {
				t.Errorf("collectMessageServerMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}
		})
	}
}

func TestParseWorkProcessCount(t *testing.T) {
	tests := []struct {
		name         string
		responseBody string
		want         int
		wantErr      error
	}{
		{
			name: "Success",
			responseBody: `RFC     testInstance.c.sap-calm.internal 3302
			HTTP    testInstance.c.sap-calm.internal 8002
			HTTPS   testInstance.c.sap-calm.internal 44302
			SMTP    testInstance.c.sap-calm.internal 25000
			DIAG    testInstance.c.sap-calm.internal 3202    LB=10`,
			want: 10,
		},
		{
			name:         "WorkProcessCountNotFound",
			responseBody: `DIAG    testInstance.c.sap-calm.internal 3202`,
			wantErr:      cmpopts.AnyError,
		},
		{
			name:         "IntegerOverflow",
			responseBody: `DIAG    testInstance.c.sap-calm.internal 3202  LB=10000000000000000000`,
			wantErr:      cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := io.NopCloser(strings.NewReader(test.responseBody))
			got, err := parseWorkProcessCount(r)

			if cmp.Diff(err, test.wantErr, cmpopts.EquateErrors()) != "" {
				t.Errorf("parseWorkProcessCount(%s) error mismatch, got: %v want: %v.", test.responseBody, err, test.wantErr)
			}

			if got != test.want {
				t.Errorf("parseWorkProcessCount(%s), got: %v want: %v.", test.responseBody, got, test.want)
			}
		})
	}
}

func TestCollectABAPProcessStatus(t *testing.T) {
	tests := []struct {
		name            string
		fRunner         sapcontrol.RunnerWithEnv
		wantMetricCount int
	}{
		{
			name: "Failure",
			fRunner: &fakeRunner{
				err: cmpopts.AnyError,
			},
			wantMetricCount: 0,
		},
		{
			name: "Success",
			fRunner: &fakeRunner{
				stdOut: `No, Typ, Pid, Status, Reason, Start, Err, Sem, Cpu, Time, Program, Client, User, Action, Table
				0, DIA, 7488, Wait, , yes, , , 0:24:54, 4, , , , ,
				1, BTC, 7489, Wait, , yes, , , 0:33:24, , , , , ,`,
			},
			wantMetricCount: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectABAPProcessStatus(defaultInstanceProperties, test.fRunner)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPProcessStatus produced unexpected number of metrics, got: %v want: %v.", len(got), test.wantMetricCount)
			}
		})
	}
}

func TestCollectABAPQueueStats(t *testing.T) {
	tests := []struct {
		name            string
		fRunner         sapcontrol.RunnerWithEnv
		wantMetricCount int
	}{
		{
			name:            "DPMONFailure",
			fRunner:         &fakeRunner{err: cmpopts.AnyError},
			wantMetricCount: 0,
		},
		{
			name: "DPMonFailsWithStdOut",
			fRunner: &fakeRunner{
				stdOut: `ICM/Intern, 0, 7, 6000, 184690, 184690`,
				err:    cmpopts.AnyError,
			},
			wantMetricCount: 0,
		},
		{
			name: "ZeroQueues",
			fRunner: &fakeRunner{
				stdOut: "InvalidOutput",
			},
			wantMetricCount: 0,
		},
		{
			name: "DPMONSuccess",
			fRunner: &fakeRunner{
				stdOut: `Typ, Now, High, Max, Writes, Reads
				ABAP/NOWP, 0, 8, 14000, 270537, 270537
				ABAP/DIA, 0, 10, 14000, 534960, 534960
				ICM/Intern, 0, 7, 6000, 184690, 184690`,
			},
			wantMetricCount: 6,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectABAPQueueStats(defaultInstanceProperties, test.fRunner)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPQueueStats() unexpected metric count, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
		})
	}
}

//go:embed dpmon_output/abap_sessions.txt
var dpmonOutputABAPSessions string

func TestCollectABAPSessionStats(t *testing.T) {
	tests := []struct {
		name            string
		fRunner         sapcontrol.RunnerWithEnv
		wantMetricCount int
	}{
		{
			name:            "DPMONFailure",
			fRunner:         &fakeRunner{err: cmpopts.AnyError},
			wantMetricCount: 0,
		},
		{
			name: "DPMonFailsWithStdOut",
			fRunner: &fakeRunner{
				stdOut: dpmonOutputABAPSessions,
				err:    cmpopts.AnyError,
			},
		},
		{
			name: "ZeroSessions",
			fRunner: &fakeRunner{
				stdOut: "InvalidOutput",
			},
			wantMetricCount: 0,
		},
		{
			name:            "DPMONSuccess",
			fRunner:         &fakeRunner{stdOut: dpmonOutputABAPSessions},
			wantMetricCount: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectABAPSessionStats(defaultInstanceProperties, test.fRunner)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPSessionStats() unexpected metric count, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
		})
	}
}

//go:embed dpmon_output/rfc_connections.txt
var dpmonRFCConnectionsOutput string

func TestCollectRFCConnections(t *testing.T) {
	tests := []struct {
		name            string
		fRunner         sapcontrol.RunnerWithEnv
		wantMetricCount int
	}{
		{
			name: "DPMONSuccess",
			fRunner: &fakeRunner{
				stdOut: dpmonRFCConnectionsOutput,
			},
			wantMetricCount: 4,
		},
		{
			name: "DPMONFailure",
			fRunner: &fakeRunner{
				err: cmpopts.AnyError,
			},
			wantMetricCount: 0,
		},
		{
			name: "DPMONFailsWithStdOut",
			fRunner: &fakeRunner{
				stdOut: dpmonRFCConnectionsOutput,
				err:    cmpopts.AnyError,
			},
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectRFCConnections(defaultInstanceProperties, test.fRunner)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectRFCConnections() unexpected metric count, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
		})
	}
}
