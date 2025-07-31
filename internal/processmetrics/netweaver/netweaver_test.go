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
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	mpb "google.golang.org/genproto/googleapis/api/metric"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultSAPInstance = &sapb.SAPInstance{
		Sapsid:            "TST",
		InstanceNumber:    "00",
		InstanceId:        "D00",
		ServiceName:       "test-service",
		Type:              sapb.InstanceType_NETWEAVER,
		NetweaverHttpPort: "1234",
	}
	defaultAppInstance = &sapb.SAPInstance{
		Sapsid:            "TST",
		InstanceNumber:    "00",
		InstanceId:        "D00",
		ServiceName:       "test-service",
		Type:              sapb.InstanceType_NETWEAVER,
		NetweaverHttpPort: "1234",
		Kind:              sapb.InstanceKind_APP,
	}
	defaultASCSInstance = &sapb.SAPInstance{
		Sapsid:            "TST",
		InstanceNumber:    "00",
		InstanceId:        "D00",
		ServiceName:       "test-service",
		Type:              sapb.InstanceType_NETWEAVER,
		NetweaverHttpPort: "1234",
		Kind:              sapb.InstanceKind_CS,
	}
	defaultERSInstance = &sapb.SAPInstance{
		Sapsid:            "TST",
		InstanceNumber:    "00",
		InstanceId:        "D00",
		ServiceName:       "test-service",
		Type:              sapb.InstanceType_NETWEAVER,
		NetweaverHttpPort: "1234",
		Kind:              sapb.InstanceKind_ERS,
	}

	defaultConfig = &cpb.Configuration{
		CollectionConfiguration: &cpb.CollectionConfiguration{
			CollectProcessMetrics:   false,
			ProcessMetricsFrequency: 5,
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
	defaultAppInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultAppInstance,
	}
	defaultASCSInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultASCSInstance,
	}
	defaultERSInstanceProperties = &InstanceProperties{
		Config:      defaultConfig,
		SAPInstance: defaultERSInstance,
	}

	defaultAPIInstanceProperties = &InstanceProperties{
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

	defaultSapControlOutputAppSrvAPI = sapcontrolclienttest.Fake{
		Processes: []sapcontrolclient.OSProcess{
			sapcontrolclient.OSProcess{
				Name:       "msg_server",
				Dispstatus: "SAPControl-GREEN",
				Pid:        111,
			},
			sapcontrolclient.OSProcess{
				Name:       "enserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        222,
			},
			sapcontrolclient.OSProcess{
				Name:       "enrepserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        333,
			},
			sapcontrolclient.OSProcess{
				Name:       "disp+work",
				Dispstatus: "SAPControl-GREEN",
				Pid:        444,
			},
			sapcontrolclient.OSProcess{
				Name:       "gwrd",
				Dispstatus: "SAPControl-GREEN",
				Pid:        555,
			},
			sapcontrolclient.OSProcess{
				Name:       "icman",
				Dispstatus: "SAPControl-GREEN",
				Pid:        666,
			},
		},
	}

	defaultSapControlOutputJavaAPI = sapcontrolclienttest.Fake{
		Processes: []sapcontrolclient.OSProcess{
			sapcontrolclient.OSProcess{
				Name:       "msg_server",
				Dispstatus: "SAPControl-GREEN",
				Pid:        111,
			},
			sapcontrolclient.OSProcess{
				Name:       "enserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        222,
			},
			sapcontrolclient.OSProcess{
				Name:       "enrepserver",
				Dispstatus: "SAPControl-GREEN",
				Pid:        333,
			},
			sapcontrolclient.OSProcess{
				Name:       "jstart",
				Dispstatus: "SAPControl-GREEN",
				Pid:        444,
			},
			sapcontrolclient.OSProcess{
				Name:       "jcontrol",
				Dispstatus: "SAPControl-GREEN",
				Pid:        555,
			},
		},
	}
)

func defaultBOPolicy(ctx context.Context) backoff.BackOffContext {
	return cloudmonitoring.LongExponentialBackOffPolicy(ctx, time.Duration(1)*time.Second, 3, 5*time.Minute, 2*time.Minute)
}

func TestCollectServiceMetrics(t *testing.T) {
	tests := []struct {
		name       string
		fakeClient sapcontrolclienttest.Fake
		wantCount  int
	}{
		{
			name: "SapControlFailsTwoProcesses",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "msg_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
					sapcontrolclient.OSProcess{
						Name:       "enserver",
						Dispstatus: "SAPControl-RED",
						Pid:        222,
					},
				},
			},
			wantCount: 2,
		},
		{
			name:       "SapControlSucceedsAppSrv",
			fakeClient: defaultSapControlOutputAppSrvAPI,
			wantCount:  6,
		},
		{
			name:       "SapControlSucceedsJava",
			fakeClient: defaultSapControlOutputJavaAPI,
			wantCount:  5,
		},
		{
			name: "SapControlSuccessMsg",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "msg_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailsEnServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enserver",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailEnRepServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enrepserver",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlSuccessEnRepServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enrepserver",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailsAppSrv",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "gwrd",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailsJava",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "jcontrol",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlSuccessJava",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "jstart",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlSuccessAppSrv",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "icman",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "InvalidProcess",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "invalidproc",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlSuccessEnqReplicator",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_replicator",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailsEnqReplicator",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_replicator",
						Dispstatus: "SAPControl-RED",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlSuccessEnqServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_server",
						Dispstatus: "SAPControl-GREEN",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "SapControlFailsEnqServer",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "enq_server",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "WebDispatctherGrey",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "sapwebdisp",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "gwrdGrey",
			fakeClient: sapcontrolclienttest.Fake{
				Processes: []sapcontrolclient.OSProcess{
					sapcontrolclient.OSProcess{
						Name:       "gwrd",
						Dispstatus: "SAPControl-GRAY",
						Pid:        111,
					},
				},
			},
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sc := &sapcontrol.Properties{
				Instance: defaultSAPInstance,
			}
			procs, err := sc.GetProcessList(context.Background(), test.fakeClient)
			if err != nil {
				t.Errorf("ProcessList() failed with: %v.", err)
			}
			got := collectServiceMetrics(context.Background(), defaultInstanceProperties, procs, timestamppb.Now())
			if len(got) != test.wantCount {
				t.Errorf("Failure in collectNWServiceMetrics(), got: %d want: %d.",
					len(got), test.wantCount)
			}
		})
	}
}

func TestNWServiceMetricLabelCount(t *testing.T) {
	// NOTE: metricLabels applies three labels by default
	tests := []struct {
		name        string
		extraLabels map[string]string
		wantLabels  int
	}{
		{
			name:        "DefaultLabels",
			extraLabels: map[string]string{},
			wantLabels:  3,
		},
		{
			name: "TwoExtraLabels",
			extraLabels: map[string]string{
				"a": "test",
				"b": "test_2",
			},
			wantLabels: 5,
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
	tests := []struct {
		name               string
		fakeClient         sapcontrolclienttest.Fake
		wantMetricCount    int
		wantErr            error
		instanceProperties *InstanceProperties
	}{
		{
			name: "SuccessWebmethod",
			fakeClient: sapcontrolclienttest.Fake{Processes: []sapcontrolclient.OSProcess{
				{Name: "msgserver", Dispstatus: "SAPControl-GREEN", Pid: 9609},
				{Name: "enserver", Dispstatus: "SAPControl-GREEN", Pid: 9972},
				{Name: "gwrd", Dispstatus: "SAPControl-GREEN", Pid: 10013},
				{Name: "disp+work", Dispstatus: "SAPControl-GREEN", Pid: 9642},
				{Name: "icman", Dispstatus: "SAPControl-GREEN", Pid: 9975},
			},
			},
			wantMetricCount:    5,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name:               "FailureWebmethod",
			fakeClient:         sapcontrolclienttest.Fake{ErrGetProcessList: cmpopts.AnyError},
			wantMetricCount:    1,
			instanceProperties: defaultAPIInstanceProperties,
			wantErr:            cmpopts.AnyError,
		},
		{
			name:            "MetricsSkipped",
			fakeClient:      sapcontrolclienttest.Fake{},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwServicePath},
					},
				},
				SkippedMetrics: map[string]bool{nwServicePath: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics, gotErr := collectNetWeaverMetrics(context.Background(), test.instanceProperties, test.fakeClient)
			if len(metrics) != test.wantMetricCount {
				t.Errorf("collectNetWeaverMetrics() metric count mismatch, got: %v want: %v.", len(metrics), test.wantMetricCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectNetWeaverMetrics() error mismatch, got: %v want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	// Production API returns only nw/service metric in unit tests.
	metrics, _ := defaultInstanceProperties.Collect(context.Background())
	if len(metrics) != 1 {
		t.Errorf("Collect() metric count mismatch, got: %v want: 1.", len(metrics))
	}
}

func TestCollectHTTPMetrics(t *testing.T) {
	tests := []struct {
		name           string
		config         *cpb.Configuration
		skippedMetrics map[string]bool
		serviceName    string
		emptyURL       bool
		wantCount      int
		wantErr        error
	}{
		{
			name:        "ICMServer",
			config:      defaultConfig,
			serviceName: "SAP-ICM-ABAP",
			wantCount:   2,
		},
		{
			name:        "MessageServer",
			config:      defaultConfig,
			serviceName: "SAP-CS",
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "UnknownServer",
			config:      defaultConfig,
			serviceName: "SAP-XYZ",
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
		{
			name:      "EmptyURL",
			config:    defaultConfig,
			emptyURL:  true,
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
		},
		{
			name:        "SkipNWICMRMetrics",
			serviceName: "SAP-ICM-ABAP",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{nwICMRCodePath},
				},
			},
			skippedMetrics: map[string]bool{nwICMRCodePath: true},
			wantCount:      0,
		},
		{
			name:        "SkipNWMessageServerMetrics",
			serviceName: "SAP-CS",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{nwMSResponseCodePath},
				},
			},
			skippedMetrics: map[string]bool{nwMSResponseCodePath: true},
			wantCount:      0,
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
				Config: test.config,
				SAPInstance: &sapb.SAPInstance{
					ServiceName:             test.serviceName,
					NetweaverHealthCheckUrl: url,
				},
				SkippedMetrics: test.skippedMetrics,
			}

			got, gotErr := collectHTTPMetrics(context.Background(), p)
			if len(got) != test.wantCount {
				t.Errorf("collectHTTPMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectHTTPMetrics() error mismatch, got: %v want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollectICMPMetrics(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantCount int
		wantErr   error
	}{
		{
			name:      "Success",
			wantCount: 2,
		},
		{
			name:      "InvalidURL",
			url:       "InvalidURL",
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
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

			got, gotErr := collectICMMetrics(context.Background(), defaultInstanceProperties, url)
			if len(got) != test.wantCount {
				t.Errorf("collectICMMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectICMMetrics() error mismatch, got: %v want: %v.", gotErr, test.wantErr)
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
		wantErr      error
	}{
		{
			name:      "InvalidURL",
			url:       "InvalidURL",
			wantCount: 0,
			wantErr:   cmpopts.AnyError,
		},
		{
			name:       "HTTPGETFailure",
			statusCode: http.StatusInternalServerError,
			wantCount:  0,
			wantErr:    cmpopts.AnyError,
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

			got, gotErr := collectMessageServerMetrics(context.Background(), defaultInstanceProperties, url)
			if len(got) != test.wantCount {
				t.Errorf("collectMessageServerMetrics() metric count mismatch, got: %v want: %v.", len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectMessageServerMetrics() error mismatch, got: %v want: %v.", gotErr, test.wantErr)
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
		name               string
		fakeClient         sapcontrolclienttest.Fake
		wantMetricCount    int
		wantErr            error
		instanceProperties *InstanceProperties
	}{
		{
			name:               "FailureWebmethod",
			fakeClient:         sapcontrolclienttest.Fake{ErrABAPGetWPTable: cmpopts.AnyError},
			wantMetricCount:    0,
			instanceProperties: defaultAPIInstanceProperties,
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "SuccessWebmethod",
			fakeClient: sapcontrolclienttest.Fake{WorkProcesses: []sapcontrolclient.WorkProcess{
				{No: 0, Type: "DIA", Pid: 7488, Status: "Run", Time: "4", User: ""},
				{No: 1, Type: "BTC", Pid: 7489, Status: "Wait", Time: "", User: ""},
			}},
			wantMetricCount:    8,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "SkipNwABAPProcCount",
			fakeClient: sapcontrolclienttest.Fake{WorkProcesses: []sapcontrolclient.WorkProcess{
				{No: 0, Type: "DIA", Pid: 7488, Status: "Run", Time: "4", User: ""},
				{No: 1, Type: "BTC", Pid: 7489, Status: "Wait", Time: "", User: ""},
			}},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPProcCountPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPProcCountPath: true},
			},
		},
		{
			name: "SkipNwABAPProcBusy",
			fakeClient: sapcontrolclienttest.Fake{WorkProcesses: []sapcontrolclient.WorkProcess{
				{No: 0, Type: "DIA", Pid: 7488, Status: "Run", Time: "4", User: ""},
				{No: 1, Type: "BTC", Pid: 7489, Status: "Wait", Time: "", User: ""},
			}},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPProcBusyPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPProcBusyPath: true},
			},
		},
		{
			name: "SkipNwABAPProcUtil",
			fakeClient: sapcontrolclienttest.Fake{WorkProcesses: []sapcontrolclient.WorkProcess{
				{No: 0, Type: "DIA", Pid: 7488, Status: "Run", Time: "4", User: ""},
				{No: 1, Type: "BTC", Pid: 7489, Status: "Wait", Time: "", User: ""},
			}},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPProcUtilPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPProcUtilPath: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := collectABAPProcessStatus(context.Background(), test.instanceProperties, test.fakeClient)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPProcessStatus produced unexpected number of metrics, got: %v want: %v.", len(got), test.wantMetricCount)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectABAPProcessStatus produced unexpected error, got: %v want: %v.", err, test.wantErr)
			}
		})
	}
}

func TestCollectABAPQueueStats(t *testing.T) {
	tests := []struct {
		name               string
		fakeClient         sapcontrolclienttest.Fake
		wantMetricCount    int
		wantErr            error
		instanceProperties *InstanceProperties
	}{
		{
			name:               "DPMONFailureWebmethod",
			fakeClient:         sapcontrolclienttest.Fake{ErrGetQueueStatistic: cmpopts.AnyError},
			wantMetricCount:    0,
			wantErr:            cmpopts.AnyError,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "DPMonFailsWithTasksWebmethod",
			fakeClient: sapcontrolclienttest.Fake{
				TaskQueues: []sapcontrolclient.TaskHandlerQueue{
					{
						Type: "ICM/Intern",
						Now:  0,
						High: 7,
					},
				}, ErrGetQueueStatistic: cmpopts.AnyError},
			wantMetricCount:    0,
			wantErr:            cmpopts.AnyError,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name:               "ZeroQueuesWebmethod",
			fakeClient:         sapcontrolclienttest.Fake{},
			wantMetricCount:    0,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "DPMONSuccessWebmethod",
			fakeClient: sapcontrolclienttest.Fake{TaskQueues: []sapcontrolclient.TaskHandlerQueue{
				{Type: "ABAP/NOWP", Now: 0, High: 8},
				{Type: "ABAP/DIA", Now: 0, High: 10},
				{Type: "ICM/Intern", Now: 0, High: 7},
			}},
			wantMetricCount:    6,
			instanceProperties: defaultAPIInstanceProperties,
		},
		{
			name: "SkipNwABAPProcQueueCurrentPath",
			fakeClient: sapcontrolclienttest.Fake{TaskQueues: []sapcontrolclient.TaskHandlerQueue{
				{Type: "ABAP/NOWP", Now: 0, High: 8},
				{Type: "ABAP/DIA", Now: 0, High: 10},
				{Type: "ICM/Intern", Now: 0, High: 7}}},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPProcQueueCurrentPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPProcQueueCurrentPath: true},
			},
		},
		{
			name: "SkipNwABAPProcQueuePeakPath",
			fakeClient: sapcontrolclienttest.Fake{TaskQueues: []sapcontrolclient.TaskHandlerQueue{
				{Type: "ABAP/NOWP", Now: 0, High: 8},
				{Type: "ABAP/DIA", Now: 0, High: 10},
				{Type: "ICM/Intern", Now: 0, High: 7},
			}},
			wantMetricCount: 0,
			instanceProperties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPProcQueuePeakPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPProcQueuePeakPath: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectABAPQueueStats(context.Background(), test.instanceProperties, test.fakeClient)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPQueueStats() unexpected metric count using webmethod, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectABAPQueueStats() unexpected error, got: %v, want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

//go:embed dpmon_output/abap_sessions.txt
var dpmonOutputABAPSessions string

func TestCollectABAPSessionStats(t *testing.T) {
	tests := []struct {
		name            string
		properties      *InstanceProperties
		fakeExec        commandlineexecutor.Execute
		wantMetricCount int
		wantErr         error
	}{
		{
			name:       "DPMONFailure",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: cmpopts.AnyError,
				}
			},
			wantMetricCount: 0,
			wantErr:         cmpopts.AnyError,
		},
		{
			name:       "DPMonFailsWithStdOut",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonOutputABAPSessions,
					Error:  cmpopts.AnyError,
				}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:       "ZeroSessions",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "InvalidOutput",
				}
			},
			wantMetricCount: 0,
			wantErr:         cmpopts.AnyError,
		},
		{
			name:       "DPMONSuccess",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonOutputABAPSessions,
				}
			},
			wantMetricCount: 4,
		},
		{
			name: "SkipABAPSessionStats",
			properties: &InstanceProperties{
				SAPInstance: defaultAPIInstanceProperties.SAPInstance,
				Config: &cpb.Configuration{CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{nwABAPSessionsPath},
				}},
				SkippedMetrics: map[string]bool{nwABAPSessionsPath: true},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonOutputABAPSessions,
				}
			},

			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectABAPSessionStats(context.Background(), test.properties, test.fakeExec, commandlineexecutor.Params{})

			if len(got) != test.wantMetricCount {
				t.Errorf("collectABAPSessionStats() unexpected metric count, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectABAPSessionStats() unexpected error, got: %v, want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

//go:embed dpmon_output/rfc_connections.txt
var dpmonRFCConnectionsOutput string

func TestCollectRFCConnections(t *testing.T) {
	tests := []struct {
		name            string
		properties      *InstanceProperties
		fakeExec        commandlineexecutor.Execute
		wantMetricCount int
		wantErr         error
	}{
		{
			name:       "DPMONSuccess",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonRFCConnectionsOutput,
				}
			},
			wantMetricCount: 4,
		},
		{
			name:       "DPMONFailure",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: cmpopts.AnyError,
				}
			},
			wantMetricCount: 0,
			wantErr:         cmpopts.AnyError,
		},
		{
			name:       "DPMONFailsWithStdOut",
			properties: defaultAPIInstanceProperties,
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonRFCConnectionsOutput,
					Error:  cmpopts.AnyError,
				}
			},
			wantMetricCount: 0,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "SkipRFCConnectionsMetric",
			properties: &InstanceProperties{
				Config: &cpb.Configuration{
					CollectionConfiguration: &cpb.CollectionConfiguration{
						ProcessMetricsToSkip: []string{nwABAPRFCPath},
					},
				},
				SkippedMetrics: map[string]bool{nwABAPRFCPath: true},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: dpmonRFCConnectionsOutput,
				}
			},
			wantMetricCount: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectRFCConnections(context.Background(), test.properties, test.fakeExec, commandlineexecutor.Params{})

			if len(got) != test.wantMetricCount {
				t.Errorf("collectRFCConnections() unexpected metric count, got: %d, want: %d.", len(got), test.wantMetricCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectRFCConnections() unexpected error, got: %v, want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollectEnqLockMetrics(t *testing.T) {
	tests := []struct {
		name            string
		props           *InstanceProperties
		fakeExec        commandlineexecutor.Execute
		fakeClient      sapcontrolclienttest.Fake
		wantMetricCount int
		wantErr         error
	}{
		{
			name: "ASCSInstanceSuccess",
			props: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					InstanceId: "ASCS11",
				},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, SAP*, SU01, E_USR04, FALSE",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{
				EnqLocks: []sapcontrolclient.EnqLock{
					sapcontrolclient.EnqLock{
						LockName:        "USR04",
						LockArg:         "000DDIC",
						LockMode:        "E",
						Owner:           "dnwh75ldbci",
						OwnerVB:         "dnwh75ldbci",
						UseCountOwner:   1,
						UseCountOwnerVB: 1,
						Client:          "000",
						User:            "SAP*",
						Transaction:     "SU01",
						Object:          "E_USR04",
						Backup:          "FALSE",
					},
				},
			},
			wantMetricCount: 1,
		},
		{
			name: "ERSInstanceSuccess",
			props: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					InstanceId: "ERS01",
				},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, SAP*, SU01, E_USR04, FALSE",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{
				EnqLocks: []sapcontrolclient.EnqLock{
					sapcontrolclient.EnqLock{
						LockName:        "USR04",
						LockArg:         "000DDIC",
						LockMode:        "E",
						Owner:           "dnwh75ldbci",
						OwnerVB:         "dnwh75ldbci",
						UseCountOwner:   1,
						UseCountOwnerVB: 1,
						Client:          "000",
						User:            "SAP*",
						Transaction:     "SU01",
						Object:          "E_USR04",
						Backup:          "FALSE",
					},
				},
			},
			wantMetricCount: 1,
		},
		{
			name: "UseGetEnqLockTableAPISuccess",
			props: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					InstanceId: "ERS01",
				},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, SAP*, SU01, E_USR04, FALSE",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{
				EnqLocks: []sapcontrolclient.EnqLock{
					sapcontrolclient.EnqLock{
						LockName:        "USR04",
						LockArg:         "000DDIC",
						LockMode:        "E",
						Owner:           "dnwh75ldbci",
						OwnerVB:         "dnwh75ldbci",
						UseCountOwner:   1,
						UseCountOwnerVB: 1,
						Client:          "000",
						User:            "SAP*",
						Transaction:     "SU01",
						Object:          "E_USR04",
						Backup:          "FALSE",
					},
				},
			},
			wantMetricCount: 1,
		},
		{
			name: "UseGetEnqLockTableAPIError",
			props: &InstanceProperties{
				Config: defaultConfig,
				SAPInstance: &sapb.SAPInstance{
					InstanceId: "ERS01",
				},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, SAP*, SU01, E_USR04, FALSE",
				}
			},
			fakeClient: sapcontrolclienttest.Fake{
				ErrEnqGetLockTable: cmpopts.AnyError,
			},
			wantMetricCount: 0,
			wantErr:         cmpopts.AnyError,
		},
		{
			name: "SkipEnqLockMetrics",
			props: &InstanceProperties{
				Config: &cpb.Configuration{CollectionConfiguration: &cpb.CollectionConfiguration{
					ProcessMetricsToSkip: []string{nwEnqLocksPath},
				}},
				SAPInstance: &sapb.SAPInstance{
					InstanceId: "ERS01",
				},
				SkippedMetrics: map[string]bool{nwEnqLocksPath: true},
			},
			fakeExec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "USR04, 000DDIC, E, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, SAP*, SU01, E_USR04, FALSE",
				}
			},
			wantMetricCount: 0,
			fakeClient: sapcontrolclienttest.Fake{
				EnqLocks: []sapcontrolclient.EnqLock{
					sapcontrolclient.EnqLock{
						LockName:        "USR04",
						LockArg:         "000DDIC",
						LockMode:        "E",
						Owner:           "dnwh75ldbci",
						OwnerVB:         "dnwh75ldbci",
						UseCountOwner:   1,
						UseCountOwnerVB: 1,
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectEnqLockMetrics(context.Background(), test.props, test.fakeExec, commandlineexecutor.Params{}, test.fakeClient)

			if len(got) != test.wantMetricCount {
				t.Errorf("collectEnqLockMetrics()=%d, want: %d.", len(got), test.wantMetricCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectEnqLockMetrics() unexpected error, got: %v, want: %v.", gotErr, test.wantErr)
			}
		})
	}
}

func TestCollectWithRetry(t *testing.T) {
	c := context.Background()
	p := &InstanceProperties{
		SAPInstance:     defaultSAPInstance,
		PMBackoffPolicy: defaultBOPolicy(c),
		Config:          defaultConfig,
	}
	_, err := p.CollectWithRetry(c)
	if err == nil {
		t.Errorf("CollectWithRetry() unexpected success, want error.")
	}
}

func TestCollectRoleMetrics(t *testing.T) {
	tests := []struct {
		name    string
		p       *InstanceProperties
		exec    commandlineexecutor.Execute
		want    *mrpb.TimeSeries
		wantErr error
	}{{
		name: "noRoles",
		p: &InstanceProperties{
			SAPInstance: &sapb.SAPInstance{
				InstanceId: "ASCS11",
			},
		},
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "usr04, 000ddic, e, dnwh75ldbci, dnwh75ldbci, 1, 1, 000, sap*, su01, e_usr04, false",
			}
		},
	}, {
		name: "commandError",
		p: &InstanceProperties{
			SAPInstance: &sapb.SAPInstance{
				InstanceId: "ASCS11",
			},
		},
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				Error: cmpopts.AnyError,
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "justASCSMsEnq",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enq.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11
tstadm   13447 13436  0 Apr26 ?        00:01:10 ms.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "true",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justASCSMsEnq",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enq.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11
tstadm   13447 13436  0 Apr26 ?        00:01:10 ms.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "true",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justSCSMsEnq",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 en.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11
tstadm   13447 13436  0 Apr26 ?        00:01:10 ms.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "true",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justSCSMsEq",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 en.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11
tstadm   13447 13436  0 Apr26 ?        00:01:10 ms.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "true",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justERSEnqr",
		p:    defaultERSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enqr.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"ers":           "true",
					"ascs":          "false",
					"app":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justERSEr",
		p:    defaultERSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 er.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"ers":           "true",
					"ascs":          "false",
					"app":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justJERSEnqr",
		p:    defaultERSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enqr.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"ers":           "true",
					"ascs":          "false",
					"app":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justJERSEr",
		p:    defaultERSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 er.sapTST_SCS00 pf=/usr/sap/TST/SYS/profile/TST_SCS00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"ers":           "true",
					"ascs":          "false",
					"app":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justAppJava",
		p:    defaultAppInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 /usr/sap/PTJ/J00/work/jc.sapTST_J02 pf=/usr/sap/TST/SYS/profile/TST_J00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "true",
					"ascs":          "false",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "justAppABAP",
		p:    defaultAppInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 dw.sapTST_D00 pf=/usr/sap/TST/SYS/profile/TST_D00_alidascs11`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "true",
					"ascs":          "false",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "msNoEnq",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 ms.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11
		`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "false",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "enqNoMs",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enq.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11
		`,
			}
		},
		want: &mrpb.TimeSeries{
			Metric: &mpb.Metric{
				Type: "workload.googleapis.com/sap/nw/instance/role",
				Labels: map[string]string{
					"app":           "false",
					"ascs":          "false",
					"ers":           "false",
					"instance_nr":   "00",
					"sid":           "TST",
					"instance_name": "test-instance",
				},
			},
		},
	}, {
		name: "appMissing",
		p:    defaultAppInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
			`,
			}
		},
	}, {
		name: "ersMissing",
		p:    defaultERSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
				`,
			}
		},
	}, {
		name: "ignoresNonSIDProcess",
		p:    defaultASCSInstanceProperties,
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
	ed1adm   13448 13436  0 Apr26 ?        00:10:50 enq.sapED1_ASCS12 pf=/usr/sap/ED1/SYS/profile/ED1_ASCS12_alidascs11
	ed1adm   13447 13436  0 Apr26 ?        00:01:10 ms.sapED1_ASCS12 pf=/usr/sap/ED1/SYS/profile/ED1_ASCS12_alidascs11`,
			}
		},
	}, {
		name: "skipRoleMetric",
		p: &InstanceProperties{
			SAPInstance: &sapb.SAPInstance{
				InstanceId: "ASCS11",
				Sapsid:     "TST",
			},
			SkippedMetrics: map[string]bool{nwInstanceRolePath: true},
		},
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `
tstadm   13448 13436  0 Apr26 ?        00:10:50 enq.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11
tstadm   13447 13436  0 Apr26 ?        00:01:10 ms.sapTST_ASCS00 pf=/usr/sap/TST/SYS/profile/TST_ASCS00_alidascs11`,
			}
		},
		want: nil,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collectRoleMetrics(ctx, tc.p, tc.exec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectRoleMetrics(%v, %v) returned an unexpected error: %v", tc.p, tc.exec, err)
			}

			if diff := cmp.Diff(tc.want, got, protocmp.Transform(), protocmp.IgnoreFields(&mrpb.TimeSeries{}, "metric_kind", "points", "resource")); diff != "" {
				t.Errorf("collectRoleMetrics(%v, %v) returned an unexpected diff (-want +got): %v", tc.p, tc.exec, diff)
			}
		})
	}
}
