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
	"runtime"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient/test/sapcontrolclienttest"

	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

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

	defaultABAPGetWPTableOuput = `No, Typ, Pid, Status, Reason, Start, Err, Sem, Cpu, Time, Program, Client, User, Action, Table
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

type (
	fakeUsageReader struct {
		wantErrMemoryUsage      error
		wantErrForCPUUsageStats error
		wantErrForIOPStats      error
	}
)

func newProcessWithContextHelperTest(ctx context.Context, pid int32) (usageReader, error) {
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
		params parameters
		want   []*ProcessInfo
	}{
		{
			name: "EmptyProcessList",
			params: parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{}
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
			want: nil,
		},
		{
			name: "CommandExecutorReturnsError",
			params: parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						Error: errors.New("could not execute command"),
					}
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
			want: nil,
		},
		{
			name: "ProcessListCreatedSuccessfully",
			params: parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   3077\n\nsapstart   9999\n",
					}
				},
				config:           defaultConfig,
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
			params: parameters{
				executor: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   9603\n\nsapstart    9204 sample\n",
					}
				},
				config:           defaultConfig,
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
		params     parameters
		fakeClient sapcontrolclienttest.Fake
		want       []*ProcessInfo
	}{
		{
			name: "EmptyProcessListWebmethod",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{},
			},
			want: nil,
		},
		{
			name: "NilSAPInstanceWebmethod",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      nil,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse},
			},
			want: nil,
		},
		{
			name: "ErrorInFetchingABAPProcessListWebmethod",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse, ErrABAPGetWPTable: errors.New("could not parse ABAPGetWPTable")},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
			},
		},
		{
			name: "ProcessListCreatedWithABAPProcessesWebmethod",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
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
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{Processes: defaultGetProcessListResponse},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
			},
		},
		{
			name: "ErrorInGettingProcessListWebmethod",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				SAPControlClient: sapcontrolclienttest.Fake{ErrGetProcessList: cmpopts.AnyError},
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectProcessesForInstance(context.Background(), test.params)
			// Sort by PID since sapcontrol's ProcessList does not guarantee any ordering.
			sort.Slice(got, func(i, j int) bool { return got[i].PID < got[j].PID })
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectProcessesForInstance(%v, %v) returned unexpected diff (-want +got):\n%s", test.params, test, diff)
			}
		})
	}
}

func TestCollectCPUPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      parameters
		processList []*ProcessInfo
		wantCount   int
		wantError   error
	}{
		{
			name: "FetchCPUPercentageMetricSuccessfully",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
			wantError:   nil,
		},
		{
			name: "CPUMetricsNotFetched",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
			},
			// For 64-bit systems, pid_max is 2^22. Set to max int32 to ensure it does not exist.
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "2147483647"}},
			wantCount:   0,
			wantError:   cmpopts.AnyError,
		},
		{
			name: "InvalidPID",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
			wantCount:   0,
			wantError:   nil,
		},
		{
			name: "ErrorInNewProcessPsUtil",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantCount:   0,
			wantError:   cmpopts.AnyError,
		},
		{
			name: "ErrorInCPUPercentage",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "222"}},
			wantCount:   0,
			wantError:   cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectCPUPerProcess(context.Background(), test.params, test.processList)
			if len(got) != test.wantCount {
				t.Errorf("collectCPUPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantError, cmpopts.EquateErrors()) {
				t.Errorf("collectCPUPerProcess(%v, %v) = %v, want %v", test.params, test.processList, gotErr, test.wantError)
			}
		})
	}
}

func TestCollectCPUPerProcessValues(t *testing.T) {
	params := parameters{
		config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		sapInstance: defaultSAPInstance,
		newProc:     newProcessWithContextHelperTest,
	}
	ProcessList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := float64(expectedCPUPercentage)
	got, _ := collectCPUPerProcess(context.Background(), params, ProcessList)
	gotVal := got[0].GetPoints()[0].GetValue().GetDoubleValue()
	if gotVal != want {
		t.Errorf("collectCPUPerProcess(%v, %v) = %f , want %f", params, ProcessList, gotVal, want)
	}
}

func TestCollectMemoryPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      parameters
		processList []*ProcessInfo
		wantCount   int
		wantErr     error
	}{
		{
			name: "FetchMemoryUsageMetricSuccessfully",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   3,
		},
		{
			name: "InvalidPID",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
			wantCount:   0,
		},
		{
			name: "ErrorInNewProcessPsUtil",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "ErrorInMemeoryUsage",
			params: parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				newProc:          newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "333"}},
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectMemoryPerProcess(context.Background(), test.params, test.processList)
			if len(got) != test.wantCount {
				t.Errorf("collectMemoryPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectMemoryPerProcess(%v, %v) = %v, want %v", test.params, test.processList, gotErr, test.wantErr)
			}
		})
	}
}

func TestMemoryPerProcessValues(t *testing.T) {
	params := parameters{
		config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		sapInstance: defaultSAPInstance,
		newProc:     newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := map[string]float64{
		"VmRSS":  2,
		"VmSize": 4,
		"VmSwap": 6,
	}
	memoryUtilMap := make(map[string]float64)
	got, _ := collectMemoryPerProcess(context.Background(), params, processList)
	for _, item := range got {
		key := item.GetMetric().GetLabels()["memType"]
		val := item.GetPoints()[0].GetValue().GetDoubleValue()
		memoryUtilMap[key] = val
	}
	if diff := cmp.Diff(want, memoryUtilMap, protocmp.Transform()); diff != "" {
		t.Errorf("collectMemoryPerProcess(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestCollectMemoryPerProcessLabels(t *testing.T) {
	params := parameters{
		config:      defaultConfig,
		client:      &fake.TimeSeriesCreator{},
		sapInstance: defaultSAPInstance,
		newProc:     newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := make(map[string]string)
	want["process"] = "hdbdindexserver:9023"
	want["memType"] = "VmSize"
	want["sid"] = "HDB"
	want["instance_nr"] = "001"
	got, _ := collectMemoryPerProcess(context.Background(), params, processList)
	gotVal := got[0].GetMetric().GetLabels()
	if diff := cmp.Diff(want, gotVal, protocmp.Transform()); diff != "" {
		t.Errorf("collectMemoryPerProcess(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestCollectIOPSPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      parameters
		processList []*ProcessInfo
		wantCount   int
		wantErr     error
	}{
		{
			name: "FetchIOPSMetricSuccessfully",
			params: parameters{
				config:               defaultConfig,
				client:               &fake.TimeSeriesCreator{},
				iopsReadsMetricPath:  "/sample/test/iops_reads",
				iopsWritesMetricPath: "/sample/test/iops_writes",
				lastValue: map[string]*process.IOCountersStat{
					"hdbdindexserver:9023": &process.IOCountersStat{
						ReadCount:  50,
						WriteCount: 500,
					},
				},
				newProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   2,
		},
		{
			name: "ErrorInCreatingNewProcess",
			params: parameters{
				config:               defaultConfig,
				client:               &fake.TimeSeriesCreator{},
				iopsReadsMetricPath:  "/sample/test/iops_reads",
				iopsWritesMetricPath: "/sample/test/iops_writes",
				newProc:              newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "111"}},
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
		{
			name: "InvalidPID",
			params: parameters{
				config:               defaultConfig,
				client:               &fake.TimeSeriesCreator{},
				iopsReadsMetricPath:  "/sample/test/iops_reads",
				iopsWritesMetricPath: "/sample/test/iops_writes",
				newProc:              newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "abc"}},
			wantCount:   0,
			wantErr:     nil,
		},
		{
			name: "ErrorInFetchingIOPSMetric",
			params: parameters{
				config:               defaultConfig,
				client:               &fake.TimeSeriesCreator{},
				iopsReadsMetricPath:  "/sample/test/iops_reads",
				iopsWritesMetricPath: "/sample/test/iops_writes",
				lastValue: map[string]*process.IOCountersStat{
					"hdbdindexserver:444": &process.IOCountersStat{
						ReadCount:  50,
						WriteCount: 500,
					},
				},
				newProc: newProcessWithContextHelperTest,
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "444"}},
			wantCount:   0,
			wantErr:     cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := collectIOPSPerProcess(context.Background(), test.params, test.processList)
			if len(got) != test.wantCount {
				t.Errorf("collectIOPSPerProcess(%v, %v) = %d , want %d", test.params, test.processList, len(got), test.wantCount)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("collectIOPSPerProcess(%v, %v) = %v, want %v", test.params, test.processList, gotErr, test.wantErr)
			}
		})
	}
}

func TestCollectIOPSPerProcessValues(t *testing.T) {
	params := parameters{
		config:               defaultConfig,
		client:               &fake.TimeSeriesCreator{},
		iopsReadsMetricPath:  "/sample/test/iops_reads",
		iopsWritesMetricPath: "/sample/test/iops_writes",
		lastValue: map[string]*process.IOCountersStat{
			"hdbdindexserver:9023": &process.IOCountersStat{
				ReadBytes:  10000,
				WriteBytes: 20000,
			},
		},
		newProc: newProcessWithContextHelperTest,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	got, _ := collectIOPSPerProcess(context.Background(), params, processList)
	want := []float64{0.4, 0.8}
	for i, item := range got {
		if item.GetPoints()[0].GetValue().GetDoubleValue() != want[i] {
			t.Errorf("collectIOPSPerProcess(%v, %v) = %v, want %v", params, processList, item.GetPoints()[0].GetValue().GetDoubleValue(), want[i])
		}
	}
}
