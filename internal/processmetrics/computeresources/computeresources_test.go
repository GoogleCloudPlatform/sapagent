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
	"errors"
	"os"
	"runtime"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultSAPInstance = &sapb.SAPInstance{
		Sapsid:         "HDB",
		InstanceNumber: "001",
	}
	defaultSapControlOutput = `OK
		0 name: hdbdaemon
		0 dispstatus: GREEN
		0 pid: 111
		1 name: hdbcompileserver
		1 dispstatus: GREEN
		1 pid: 222`

	defaultABAPGetWPTableOutput = `No, Typ, Pid, Status, Reason, Start, Err, Sem, Cpu, Time, Program, Client, User, Action, Table
	0, DIA, 7488, Wait, , yes, , , 0:24:54, 4, , , , ,
	1, BTC, 7489, Wait, , yes, , , 0:33:24, , , , , ,`

	defaultGetProcessListResponse = []sapcontrolclient.OSProcess{
		{Name: "hdbdaemon", Dispstatus: "SAPControl-GREEN", Pid: 111},
		{Name: "hdbcompileserver", Dispstatus: "SAPControl-GREEN", Pid: 222},
	}
)

const (
	expectedCPUPercentage float64 = 46
)

type fakeUsageReader struct {
	wantErrMemoryUsage      error
	wantErrForCPUUsageStats error
	wantErrForIOPStats      error
}

func newProcessWithContextHelperTest(ctx context.Context, pid int32) (UsageReader, error) {
	// treating 111 as the PID which results into errors
	if pid == 111 {
		return nil, errors.New("could not create new process")
	}
	if pid == 222 {
		return fakeUsageReader{wantErrForCPUUsageStats: errors.New("could not get CPU percentage stats")}, nil
	}
	if pid == 333 {
		return fakeUsageReader{wantErrMemoryUsage: errors.New("could not get memory usage stats")}, nil
	}
	if pid == 444 {
		return fakeUsageReader{wantErrForIOPStats: errors.New("could not get IOP stats")}, nil
	}
	return fakeUsageReader{}, nil
}

func (ur fakeUsageReader) CPUPercentWithContext(ctx context.Context) (float64, error) {
	if ur.wantErrForCPUUsageStats != nil {
		return 0, ur.wantErrForCPUUsageStats
	}
	return expectedCPUPercentage * float64(runtime.NumCPU()), nil
}

func (ur fakeUsageReader) MemoryInfoWithContext(ctx context.Context) (*process.MemoryInfoStat, error) {
	if ur.wantErrMemoryUsage != nil {
		return nil, ur.wantErrMemoryUsage
	}
	op := &process.MemoryInfoStat{
		RSS:  2000000,
		VMS:  4000000,
		Swap: 6000000,
	}
	return op, nil
}

func (ur fakeUsageReader) IOCountersWithContext(ctx context.Context) (*process.IOCountersStat, error) {
	if ur.wantErrForIOPStats != nil {
		return nil, ur.wantErrForIOPStats
	}
	return &process.IOCountersStat{
		ReadBytes:  12000,
		WriteBytes: 24000,
	}, nil
}

func TestCollectControlProcesses(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   []*ProcessInfo
	}{
		{
			name: "EmptyProcessList",
			params: Parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{}
				},
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
		},
		{
			name: "CommandExecutorReturnsError",
			params: Parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						Error: errors.New("could not execute command"),
					}
				},
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
		},
		{
			name: "ProcessListCreatedSuccessfully",
			params: Parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   3077\n\nsapstart   9999\n",
					}
				},
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "sapstartsrv", PID: "3077"},
				&ProcessInfo{Name: "sapstart", PID: "9999"},
			},
		},
		{
			name: "MalformedOutputFromCommand",
			params: Parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   9603\n\nsapstart    9204 sample\n",
					}
				},
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
			want: []*ProcessInfo{&ProcessInfo{Name: "sapstartsrv", PID: "9603"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectControlProcesses(context.Background(), test.params)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectControlProcesses(%v) returned unexpected diff (-want +got):\n%s", test.params, diff)
			}
		})
	}
}

func TestCollectProcessesForInstance(t *testing.T) {
	tests := []struct {
		name       string
		params     Parameters
		fakeClient sapcontrolclienttest.Fake
		want       []*ProcessInfo
	}{
		{
			name: "EmptyProcessListWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{},
			},
		},
		{
			name: "NilSAPInstanceWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      nil,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse},
			},
		},
		{
			name: "ErrorInFetchingABAPProcessListWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse, ErrABAPGetWPTable: errors.New("could not parse ABAPGetWPTable")},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
			},
		},
		{
			name: "ProcessListCreatedWithABAPProcessesWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{
					Processes: defaultGetProcessListResponse,
					WorkProcesses: []sapcontrolclient.WorkProcess{
						{No: 0, Type: "DIA", Pid: 7488, Status: "Run", Time: "4", User: ""},
						{No: 1, Type: "BTC", Pid: 7489, Status: "Wait", Time: "", User: ""},
					},
				},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
				&ProcessInfo{Name: "DIA", PID: "7488"},
				&ProcessInfo{Name: "BTC", PID: "7489"},
			},
		},
		{
			name: "ProcessListCreatedSuccessfullyWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
			},
		},
		{
			name: "ErrorInGettingProcessListWebmethod",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				SAPInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{ErrGetProcessList: cmpopts.AnyError},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CollectProcessesForInstance(context.Background(), test.params)
			// Sort by PID since sapcontrol's ProcessList does not guarantee any ordering.
			sort.Slice(got, func(i, j int) bool { return got[i].PID < got[j].PID })
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectProcessesForInstance(%v, %v) returned unexpected diff (-want +got):\n%s", test.params, test, diff)
			}
		})
	}
}

func TestCollectTimeSeriesMetrics(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
		metricType  int
		wantError   bool
	}{
		{
			name: "FetchCPUPercentageMetricSuccessfully",
			params: Parameters{
				Config:        defaultConfig,
				client:        &fake.TimeSeriesCreator{},
				cpuMetricPath: "/sample/test/proc",
				NewProc:       newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
			metricType:  collectCPUMetric,
		},
		{
			name: "FetchMemoryUsageMetricSuccessfully",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				NewProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			metricType:  collectMemoryMetric,
			wantCount:   3,
		},
		{
			name: "FetchIOPSMetricSuccessfully",
			params: Parameters{
				Config:               defaultConfig,
				client:               &fake.TimeSeriesCreator{},
				iopsReadsMetricPath:  "/sample/test/iops_reads",
				iopsWritesMetricPath: "/sample/test/iops_writes",
				LastValue: map[string]*process.IOCountersStat{
					"hdbdindexserver:9023": &process.IOCountersStat{
						ReadCount:  50,
						WriteCount: 500,
					},
				},
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			metricType:  collectDiskIOPSMetric,
			wantCount:   2,
		},
		{
			name: "FailInvalidMetricType",
			params: Parameters{
				Config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				NewProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			metricType:  3,
			wantError:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := collectTimeSeriesMetrics(context.Background(), test.params, test.processList, test.metricType)
			if gotErr := err != nil; gotErr != test.wantError {
				t.Errorf("collectTimeSeriesMetrics(%v, %v, %v) returned error: %v, want error presence: %v", test.params, test.processList, test.metricType, err, test.wantError)
			}
			if len(got) != test.wantCount {
				t.Errorf("collectTimeSeriesMetrics(%v, %v, %v) = %d , want %d", test.params, test.processList, test.metricType, len(got), test.wantCount)
			}
		})
	}
}

func TestCollectTimeSeriesMetricsCPUValues(t *testing.T) {
	params := Parameters{
		Config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		SAPInstance: defaultSAPInstance,
		NewProc:     newProcessWithContextHelperTest,
	}
	ProcessList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := float64(expectedCPUPercentage)
	got, err := collectTimeSeriesMetrics(context.Background(), params, ProcessList, collectCPUMetric)
	if err != nil {
		t.Errorf("collectTimeSeriesMetrics(%v, %v) returned unexpected error: %v", params, ProcessList, err)
	}
	gotVal := got[0].GetPoints()[0].GetValue().GetDoubleValue()
	if gotVal != want {
		t.Errorf("collectTimeSeriesMetrics(%v, %v) = %f , want %f", params, ProcessList, gotVal, want)
	}
}

func TestCollectTimeSeriesMetricsMemoryValues(t *testing.T) {
	params := Parameters{
		Config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		SAPInstance: defaultSAPInstance,
		NewProc:     newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := map[string]float64{
		"VmRSS":  2,
		"VmSize": 4,
		"VmSwap": 6,
	}
	memoryUtilMap := make(map[string]float64)
	got, err := collectTimeSeriesMetrics(context.Background(), params, processList, collectMemoryMetric)
	if err != nil {
		t.Errorf("collectTimeSeriesMetrics(%v) returned unexpected error: %v", params, err)
	}
	for _, item := range got {
		key := item.GetMetric().GetLabels()["memType"]
		val := item.GetPoints()[0].GetValue().GetDoubleValue()
		memoryUtilMap[key] = val
	}
	if diff := cmp.Diff(want, memoryUtilMap, protocmp.Transform()); diff != "" {
		t.Errorf("collectTimeSeriesMetrics(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestCollectTimeSeriesMetricsMemoryLabels(t *testing.T) {
	params := Parameters{
		Config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		SAPInstance: defaultSAPInstance,
		NewProc:     newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := make(map[string]string)
	want["process"] = "hdbdindexserver:9023"
	want["memType"] = "VmSize"
	want["sid"] = "HDB"
	want["instance_nr"] = "001"
	got, err := collectTimeSeriesMetrics(context.Background(), params, processList, collectMemoryMetric)
	if err != nil {
		t.Errorf("collectTimeSeriesMetrics(%v) returned unexpected error: %v", params, err)
	}
	gotVal := got[0].GetMetric().GetLabels()
	if diff := cmp.Diff(want, gotVal, protocmp.Transform()); diff != "" {
		t.Errorf("collectTimeSeriesMetrics(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestCollectTimeSeriesMetricsIOPSValues(t *testing.T) {
	params := Parameters{
		Config:               defaultConfig,
		client:               &fake.TimeSeriesCreator{},
		iopsReadsMetricPath:  "/sample/test/iops_reads",
		iopsWritesMetricPath: "/sample/test/iops_writes",
		LastValue: map[string]*process.IOCountersStat{
			"hdbdindexserver:9023": &process.IOCountersStat{
				ReadBytes:  10000,
				WriteBytes: 20000,
			},
		},
		NewProc: newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := []float64{0.4, 0.8}
	got, err := collectTimeSeriesMetrics(context.Background(), params, processList, collectDiskIOPSMetric)
	if err != nil {
		t.Errorf("collectTimeSeriesMetrics(%v, %v) returned unexpected error: %v", params, processList, err)
	}
	for i, item := range got {
		if item.GetPoints()[0].GetValue().GetDoubleValue() != want[i] {
			t.Errorf("collectTimeSeriesMetrics(%v, %v) = %v, want %v", params, processList, item.GetPoints()[0].GetValue().GetDoubleValue(), want[i])
		}
	}
}

func TestCollectCPUPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
		wantErr     bool
	}{
		{
			name: "FetchCPUPercentageMetricSuccessfully",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
		},
		{
			name: "CPUMetricsNotFetched",
			params: Parameters{
				Config: defaultConfig,
			},
			// For 64-bit systems, pid_max is 2^22. Set to max int32 to ensure it does not exist.
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "2147483647"}},
			wantErr:     true,
		},
		{
			name: "InvalidPID",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
		},
		{
			name: "ErrorInNewProcessPsUtil",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantErr:     true,
		},
		{
			name: "ErrorInCPUPercentage",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "222"}},
			wantErr:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CollectCPUPerProcess(context.Background(), test.params, test.processList)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("CollectCPUPerProcess(%v, %v) returned unexpected error: %v, want error presence: %v", test.params, test.processList, err, test.wantErr)
			}
			if len(got) != test.wantCount {
				t.Errorf("CollectCPUPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
		})
	}
}

func TestCollectCPUPerProcessValues(t *testing.T) {
	params := Parameters{
		Config:      defaultConfig,
		SAPInstance: defaultSAPInstance,
		NewProc:     newProcessWithContextHelperTest,
	}
	ProcessList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := float64(expectedCPUPercentage)
	got, err := CollectCPUPerProcess(context.Background(), params, ProcessList)
	if err != nil {
		t.Errorf("CollectCPUPerProcess(%v, %v) returned unexpected error: %v", params, ProcessList, err)
	}
	gotVal := got[0].Value
	if gotVal != want {
		t.Errorf("CollectCPUPerProcess(%v, %v) = %f , want %f", params, ProcessList, gotVal, want)
	}
}

func TestCollectMemoryPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
		wantErr     bool
	}{
		{
			name: "FetchMemoryUsageMetricSuccessfully",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
		},
		{
			name: "InvalidPID",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
		},
		{
			name: "ErrorInNewProcessPsUtil",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantErr:     true,
		},
		{
			name: "ErrorInMemoryUsage",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "333"}},
			wantErr:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CollectMemoryPerProcess(context.Background(), test.params, test.processList)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("CollectMemoryPerProcess(%v, %v) returned unexpected error: %v, want error presence: %v", test.params, test.processList, err, test.wantErr)
			}
			if len(got) != test.wantCount {
				t.Errorf("CollectMemoryPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
		})
	}
}

func TestMemoryPerProcessValues(t *testing.T) {
	params := Parameters{
		Config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		SAPInstance: defaultSAPInstance,
		NewProc:     newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := []MemoryUsage{
		{
			VMS:  &Metric{ProcessInfo: processList[0], Value: 4, TimeStamp: tspb.Now()},
			RSS:  &Metric{ProcessInfo: processList[0], Value: 2, TimeStamp: tspb.Now()},
			Swap: &Metric{ProcessInfo: processList[0], Value: 6, TimeStamp: tspb.Now()},
		},
	}
	got, err := CollectMemoryPerProcess(context.Background(), params, processList)
	if err != nil {
		t.Errorf("CollectMemoryPerProcess(%v, %v) returned unexpected error: %v", params, processList, err)
	}
	for i, item := range got {
		if item.VMS.Value != want[i].VMS.Value || item.RSS.Value != want[i].RSS.Value || item.Swap.Value != want[i].Swap.Value {
			t.Errorf("CollectMemoryPerProcess(%v) = %v, want %v", params, got, want)
		}
	}
}

func TestCollectIOPSPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
		wantErr     bool
	}{
		{
			name: "FetchIOPSMetricSuccessfully",
			params: Parameters{
				Config: defaultConfig,
				LastValue: map[string]*process.IOCountersStat{
					"hdbdindexserver:9023": &process.IOCountersStat{
						ReadCount:  50,
						WriteCount: 500,
					},
				},
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
		},
		{
			name: "NoIOPSMetricsAsThisIsTheFirstCall",
			params: Parameters{
				Config:    defaultConfig,
				LastValue: make(map[string]*process.IOCountersStat),
				NewProc:   newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
		},
		{
			name: "ErrorInCreatingNewProcess",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantErr:     true,
		},
		{
			name: "InvalidPID",
			params: Parameters{
				Config:  defaultConfig,
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
		},
		{
			name: "ErrorInFetchingIOPSMetric",
			params: Parameters{
				Config: defaultConfig,
				LastValue: map[string]*process.IOCountersStat{
					"hdbdindexserver:444": &process.IOCountersStat{
						ReadCount:  50,
						WriteCount: 500,
					},
				},
				NewProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "444"}},
			wantErr:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CollectIOPSPerProcess(context.Background(), test.params, test.processList)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("CollectIOPSPerProcess(%v, %v) returned unexpected error: %v", test.params, test.processList, err)
			}
			if len(got) != test.wantCount {
				t.Errorf("CollectIOPSPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
		})
	}
}

func TestCollectIOPSPerProcessValues(t *testing.T) {
	params := Parameters{
		Config:  defaultConfig,
		NewProc: newProcessWithContextHelperTest,
		LastValue: map[string]*process.IOCountersStat{
			"hdbdindexserver:9023": &process.IOCountersStat{
				ReadBytes:  10000,
				WriteBytes: 20000,
			},
		},
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := []DiskUsage{
		{
			DeltaReads:  &Metric{ProcessInfo: processList[0], Value: 0.4, TimeStamp: tspb.Now()},
			DeltaWrites: &Metric{ProcessInfo: processList[0], Value: 0.8, TimeStamp: tspb.Now()},
		},
	}
	got, err := CollectIOPSPerProcess(context.Background(), params, processList)
	if err != nil {
		t.Errorf("CollectIOPSPerProcess(%v, %v) returned unexpected error: %v", params, processList, err)
	}
	for i, item := range got {
		if item.DeltaReads.Value != want[i].DeltaReads.Value || item.DeltaWrites.Value != want[i].DeltaWrites.Value {
			t.Errorf("CollectIOPSPerProcess(%v) = %v, want %v", params, got, want)
		}
	}
}

func TestBuildMetricLabel(t *testing.T) {
	tests := []struct {
		name        string
		metricType  int
		metricLabel string
		metric      *Metric
		wantLabels  map[string]string
	}{
		{
			name:       "SuccessMetricLabelForCPUMetric",
			metricType: collectCPUMetric,
			metric:     &Metric{ProcessInfo: &ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantLabels: map[string]string{"process": "hdbdindexserver:9023"},
		},
		{
			name:        "SuccessMetricLabelForMemoryMetric",
			metricType:  collectMemoryMetric,
			metricLabel: "VmSize",
			metric:      &Metric{ProcessInfo: &ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantLabels:  map[string]string{"process": "hdbdindexserver:9023", "memType": "VmSize"},
		},
		{
			name: "FailMetricIsNil",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotLabels := buildMetricLabel(test.metricType, test.metricLabel, test.metric)
			if diff := cmp.Diff(test.wantLabels, gotLabels, protocmp.Transform()); diff != "" {
				t.Errorf("buildMetricLabel(%v, %v, %v) returned unexpected diff (-want +got):\n%s", test.metricType, test.metricLabel, test.metric, diff)
			}
		})
	}
}
