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
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
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
)

func emptyFileReader() MockedFileReader {
	return MockedFileReader{
		expectedDataList: [][]byte{nil, nil, nil},
		expectedErrList:  []error{nil, nil, nil},
	}
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
				executor: func(cmd, args string) (string, string, error) {
					return "", "", nil
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
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					return "", "", errors.New("could not execute command")
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
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   3077\n\nsapstart   9999\n", "", nil
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
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					return "COMMAND           PID\nsystemd             1\nkthreadd            2\nsapstartsrv   9603\n\nsapstart    9204 sample\n", "", nil
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
			got := collectControlProcesses(test.params)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectControlProcesses(%v) returned unexpected diff (-want +got):\n%s", test.params, diff)
			}
		})
	}
}

func TestCollectProcessesForInstance(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   []*ProcessInfo
	}{
		{
			name: "EmptyProcessList",
			params: Parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				runner:           &fakeRunner{stdOut: ""},
			},
			want: nil,
		},
		{
			name: "NilSAPInstance",
			params: Parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      nil,
				runner:           &fakeRunner{stdOut: defaultSapControlOutput},
			},
			want: nil,
		},
		{
			name: "ProcessListCreatedSuccessfully",
			params: Parameters{
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				sapInstance:      defaultSAPInstance,
				runner:           &fakeRunner{stdOut: defaultSapControlOutput},
			},
			want: []*ProcessInfo{
				&ProcessInfo{Name: "hdbdaemon", PID: "111"},
				&ProcessInfo{Name: "hdbcompileserver", PID: "222"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectProcessesForInstance(test.params)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("collectProcessesForInstance(%v) returned unexpected diff (-want +got):\n%s", test.params, diff)
			}
		})
	}
}

func TestCollectCPUPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
	}{
		{
			name: "InvalidCLKTCKS",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "sample\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader:       emptyFileReader(),
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "ZeroCLKTCKS",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "0\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader:       emptyFileReader(),
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "UnableToGetCLKTCKS",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "", "", errors.New("unable to execute command")
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader:       emptyFileReader()},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotReadProcStatFile",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(``),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						os.ErrPermission,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "MalformedProcStatFile",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(`. . . . . . . . . . . . . 3 1 . . 84265`),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						nil,
						nil,
						os.ErrPermission,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotParseUTime",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(`. . . . . . . . . . . . . sample 1 . . . . . . 84265 . . .`),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						nil,
						nil,
						os.ErrPermission,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotParseSTime",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(`. . . . . . . . . . . . . 3 sample . . . . . . 84265 . . .`),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						nil,
						nil,
						os.ErrPermission,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotParseStartTime",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(`. . . . . . . . . . . . . 3 2 . . . . . . sample . . .`),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						nil,
						nil,
						os.ErrPermission,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotReadProcUptimeFile",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(`. . . . . . . . . . . . . 3 1 . . . . . . 84265 . . .`),
						[]byte(``),
						[]byte(``),
					},
					expectedErrList: []error{
						nil,
						nil,
						os.ErrPermission,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "MalformedProcUptimeFile",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
						[]byte("28057.65 218517.53 2133\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "CouldNotParseUpTime",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
						[]byte("sample 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "ErrorFromCalculatePercentageCPU",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {

					return "", "", errors.New("unable to execute command")
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
						[]byte("28057.65 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "FetchCPUPercentageMetricSuccessfully",
			params: Parameters{
				executor: func(cmd, args string) (string, string, error) {
					if cmd == "getconf" {
						return "100\n", "", nil
					}
					return "", "", nil
				},
				config:           defaultConfig,
				client:           &fake.TimeSeriesCreator{},
				cpuMetricPath:    "/sample/test/proc",
				memoryMetricPath: "/sample/test/memory",
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
						[]byte("28057.65 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectCPUPerProcess(test.params, test.processList)
			if len(got) != test.wantCount {
				t.Errorf("Got(%d) != want(%d)", len(got), test.wantCount)
			}
		})
	}
}

func TestCalculatePercentageCPU(t *testing.T) {
	tests := []struct {
		name           string
		xxTime         float64
		upTime         float64
		startTime      float64
		ticks          float64
		wantCPUPercent float64
		wantErr        error
	}{
		{
			name:           "ZeroRunningSeconds",
			xxTime:         23.0,
			upTime:         2087,
			startTime:      208700,
			ticks:          100,
			wantCPUPercent: 0,
			wantErr:        nil,
		},
		{
			name:           "CalculateCPUPercentageSuccessfully",
			xxTime:         6876582.0,
			upTime:         979016.96,
			startTime:      19802,
			ticks:          100,
			wantCPUPercent: 7.03,
			wantErr:        nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculatePercentageCPU(test.xxTime, test.upTime, test.startTime, test.ticks)
			if test.wantErr != nil && err.Error() != test.wantErr.Error() {
				t.Errorf("Got err (%s) != Want err (%s)", err, test.wantErr)
			}
			if got != test.wantCPUPercent {
				t.Errorf("Got CPU per cent (%f) != Want CPU per cent(%f)", got, test.wantCPUPercent)
			}
		})
	}
}

func TestCollectCPUPerProcessLabels(t *testing.T) {
	params := Parameters{
		executor: func(cmd, args string) (string, string, error) {
			if cmd == "getconf" {
				return "100\n", "", nil
			}
			return "", "", nil
		},
		config:           defaultConfig,
		client:           &fake.TimeSeriesCreator{},
		cpuMetricPath:    "/sample/test/proc",
		memoryMetricPath: "/sample/test/memory",
		fileReader: MockedFileReader{
			expectedDataList: [][]byte{
				[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
				[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     24634\n"),
				[]byte("28057.65 218517.53\n"),
			},
			expectedErrList: []error{
				nil,
				nil,
				nil,
			},
		},
		sapInstance: defaultSAPInstance,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := make(map[string]string)
	want["process"] = "hdbdindexserver:9023"
	want["sid"] = "HDB"
	want["instance_nr"] = "001"
	got := collectCPUPerProcess(params, processList)[0].TimeSeries.GetMetric().GetLabels()
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("collectCPUPerProcess(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}

func TestCollectMemoryPerProcess(t *testing.T) {
	tests := []struct {
		name        string
		params      Parameters
		processList []*ProcessInfo
		wantCount   int
	}{
		{
			name: "CouldNotReadProcStatusFile",
			params: Parameters{
				config: defaultConfig,
				client: &fake.TimeSeriesCreator{},
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte(``),
						[]byte("28057.65 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						os.ErrNotExist,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   0,
		},
		{
			name: "MalformedProcStatusFile",
			params: Parameters{
				config: defaultConfig,
				client: &fake.TimeSeriesCreator{},
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:\nVmRSS:   14623\nVmSwap:     2134\n"),
						[]byte("28057.65 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   2,
		},
		{
			name: "CouldNotParseVmSwap",
			params: Parameters{
				config: defaultConfig,
				client: &fake.TimeSeriesCreator{},
				fileReader: MockedFileReader{
					expectedDataList: [][]byte{
						[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
						[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     sample\n"),
						[]byte("28057.65 218517.53\n"),
					},
					expectedErrList: []error{
						nil,
						nil,
						nil,
					},
				},
			},
			processList: []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}},
			wantCount:   2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectMemoryPerProcess(test.params, test.processList)
			if len(got) != test.wantCount {
				t.Errorf("Got (%d) != want(%d)", len(got), test.wantCount)
			}
		})
	}
}

func TestCollectMemoryPerProcessLabels(t *testing.T) {
	params := Parameters{
		config: defaultConfig,
		client: &fake.TimeSeriesCreator{},
		fileReader: MockedFileReader{
			expectedDataList: [][]byte{
				[]byte(". . . . . . . . . . . . . 3 1 . . . . . . 84265 . . ."),
				[]byte("Name:  xxx\nVmSize:   1340\nVmRSS:   14623\nVmSwap:     sample\n"),
				[]byte("28057.65 218517.53\n"),
			},
			expectedErrList: []error{
				nil,
				nil,
				nil,
			},
		},
		sapInstance: defaultSAPInstance,
	}
	processList := []*ProcessInfo{&ProcessInfo{Name: "hdbdindexserver", PID: "9023"}}
	want := make(map[string]string)
	want["process"] = "hdbdindexserver:9023"
	want["memType"] = "VmSize"
	want["sid"] = "HDB"
	want["instance_nr"] = "001"
	got := collectMemoryPerProcess(params, processList)[0].TimeSeries.GetMetric().GetLabels()
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("collectMemoryPerProcess(%v) returned unexpected diff (-want +got):\n%s", params, diff)
	}
}
