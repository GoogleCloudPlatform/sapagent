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

package diskstatsreader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultInstanceProperties = &iipb.InstanceProperties{
		Disks: []*iipb.Disk{
			&iipb.Disk{
				Mapping: "sda",
			},
		},
	}
	defaultWindowsInstanceProperties = &iipb.InstanceProperties{
		Disks: []*iipb.Disk{
			&iipb.Disk{
				DeviceName: "Windows",
				Mapping:    "PhysicalDrive0",
			},
		},
	}
)

func TestRead(t *testing.T) {
	tests := []struct {
		name     string
		os       string
		reader   FileReader
		fakeExec commandlineexecutor.Execute
		ip       *iipb.InstanceProperties
		want     *statspb.DiskStatsCollection
	}{
		{
			name: "linuxSingleResult",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("\n\n   8       0 sda 147832 46485 13764152 2093154 859169 501771 53906928 8908778 0 1912548 11024066 0 0 0 0 219344 22133\n"), nil
			},
			ip: defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "sda",
						ReadOpsCount:                   147832,
						ReadSvcTimeMillis:              2093154,
						WriteOpsCount:                  859169,
						WriteSvcTimeMillis:             8908778,
						QueueLength:                    0,
						AverageReadResponseTimeMillis:  metricsformatter.Unavailable,
						AverageWriteResponseTimeMillis: metricsformatter.Unavailable,
					},
				},
			},
		},
		{
			name: "linuxMultipleResults",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("   8       0 sda 147832 46485 13764152 2093154 859169 501771 53906928 8908778 0 1912548 11024066 0 0 0 0 219344 22133\n   8       1 sda1 1488 64 328962 54305 4 1 40 92 0 28856 54398 0 0 0 0 0 0"), nil
			},
			ip: &iipb.InstanceProperties{
				Disks: []*iipb.Disk{
					&iipb.Disk{
						Mapping: "sda",
					},
					&iipb.Disk{
						Mapping: "sda1",
					},
				},
			},
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "sda",
						ReadOpsCount:                   147832,
						ReadSvcTimeMillis:              2093154,
						WriteOpsCount:                  859169,
						WriteSvcTimeMillis:             8908778,
						QueueLength:                    0,
						AverageReadResponseTimeMillis:  metricsformatter.Unavailable,
						AverageWriteResponseTimeMillis: metricsformatter.Unavailable,
					},
					&statspb.DiskStats{
						DeviceName:                     "sda1",
						ReadOpsCount:                   1488,
						ReadSvcTimeMillis:              54305,
						WriteOpsCount:                  4,
						WriteSvcTimeMillis:             92,
						QueueLength:                    0,
						AverageReadResponseTimeMillis:  metricsformatter.Unavailable,
						AverageWriteResponseTimeMillis: metricsformatter.Unavailable,
					},
				},
			},
		},
		{
			name: "linuxErrReadFile",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return nil, errors.New("Read File error")
			},
			ip:   defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "linuxIgnoreCommentLines",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("#8       0 sda 147832 46485 13764152 2093154 859169 501771 53906928 8908778 0 1912548 11024066 0 0 0 0 219344 22133"), nil
			},
			ip:   defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "linuxBadFileFormat",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("   8       0 sda 147832"), nil
			},
			ip:   defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "linuxNoMapping",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("   8       0 unknownMapping 147832 46485 13764152 2093154 859169 501771 53906928 8908778 0 1912548 11024066 0 0 0 0 219344 22133"), nil
			},
			ip:   defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "linuxErrParse",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("   8       0 sda readOpsCount 46485 13764152 readSvcTimeMillis writeOpsCount 501771 53906928 writeSvcTimeMillis queueLength 1912548 11024066 0 0 0 0 219344 22133"), nil
			},
			ip: defaultInstanceProperties,
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "sda",
						ReadOpsCount:                   metricsformatter.Unavailable,
						ReadSvcTimeMillis:              metricsformatter.Unavailable,
						WriteOpsCount:                  metricsformatter.Unavailable,
						WriteSvcTimeMillis:             metricsformatter.Unavailable,
						QueueLength:                    metricsformatter.Unavailable,
						AverageReadResponseTimeMillis:  metricsformatter.Unavailable,
						AverageWriteResponseTimeMillis: metricsformatter.Unavailable,
					},
				},
			},
		},
		{
			name: "windowsSingleResult",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				argStr := strings.Join(params.Args, " ")
				if argStr != `-command $(Get-Counter '\PhysicalDisk(0*)\Avg. Disk sec/Read').CounterSamples[0].CookedValue;Write-Host ';';$(Get-Counter '\PhysicalDisk(0*)\Avg. Disk sec/Write').CounterSamples[0].CookedValue;Write-Host ';';$(Get-Counter '\PhysicalDisk(0*)\Current Disk Queue Length').CounterSamples[0].CookedValue` {
					return commandlineexecutor.Result{
						Error: fmt.Errorf("Bad arguments for execute command %q", argStr),
					}
				}
				return commandlineexecutor.Result{
					StdOut: "111;\n\r222;\n\r333\n\r",
				}
			},
			ip: defaultWindowsInstanceProperties,
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "PhysicalDrive0",
						AverageReadResponseTimeMillis:  111000,
						AverageWriteResponseTimeMillis: 222000,
						QueueLength:                    333,
					},
				},
			},
		},
		{
			name: "windowsMultipleResults",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "111;222;333",
				}
			},
			ip: &iipb.InstanceProperties{
				Disks: []*iipb.Disk{
					&iipb.Disk{
						Mapping: "PhysicalDrive0",
					},
					&iipb.Disk{
						Mapping: "PhysicalDrive1",
					},
				},
			},
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "PhysicalDrive0",
						AverageReadResponseTimeMillis:  111000,
						AverageWriteResponseTimeMillis: 222000,
						QueueLength:                    333,
					},
					&statspb.DiskStats{
						DeviceName:                     "PhysicalDrive1",
						AverageReadResponseTimeMillis:  111000,
						AverageWriteResponseTimeMillis: 222000,
						QueueLength:                    333,
					},
				},
			},
		},
		{
			name: "windowsNoDiskNumber",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "111;222;333",
				}
			},
			ip: &iipb.InstanceProperties{
				Disks: []*iipb.Disk{
					&iipb.Disk{
						Mapping: "UnknownMapping",
					},
				},
			},
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "windowsBadDiskNumber",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "111;222;333",
				}
			},
			ip: &iipb.InstanceProperties{
				Disks: []*iipb.Disk{
					&iipb.Disk{
						Mapping: "PhysicalDriveBadMapping",
					},
				},
			},
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "windowsErrRunCommand",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "111;222;333",
					Error:  errors.New("Run Command Error"),
				}
			},
			ip:   defaultWindowsInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "windowsBadRunCommandOutput",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "111222333",
				}
			},
			ip:   defaultWindowsInstanceProperties,
			want: &statspb.DiskStatsCollection{},
		},
		{
			name: "windowsErrParse",
			os:   "windows",
			fakeExec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "one;two;three",
				}
			},
			ip: defaultWindowsInstanceProperties,
			want: &statspb.DiskStatsCollection{
				DiskStats: []*statspb.DiskStats{
					&statspb.DiskStats{
						DeviceName:                     "PhysicalDrive0",
						AverageReadResponseTimeMillis:  metricsformatter.Unavailable,
						AverageWriteResponseTimeMillis: metricsformatter.Unavailable,
						QueueLength:                    metricsformatter.Unavailable,
					},
				},
			},
		},
		{
			name: "unknownOS",
			os:   "mac",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := New(test.os, test.reader, test.fakeExec)
			got := r.Read(context.Background(), test.ip)
			sortDiskStats := protocmp.SortRepeatedFields(&statspb.DiskStatsCollection{}, "disk_stats")
			if d := cmp.Diff(test.want, got, protocmp.Transform(), sortDiskStats); d != "" {
				t.Errorf("Read() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestAverageReadResponseTime(t *testing.T) {
	defaultPrevDiskStats := map[string]*statspb.DiskStats{
		"sda": &statspb.DiskStats{
			ReadSvcTimeMillis: 1000,
			ReadOpsCount:      10,
		},
	}

	tests := []struct {
		name          string
		deviceName    string
		readSvcTime   int64
		readOpsCount  int64
		prevDiskStats map[string]*statspb.DiskStats
		want          int64
	}{
		{
			name:          "unavailablePrevDiskStats",
			deviceName:    "sda",
			readSvcTime:   1000,
			readOpsCount:  10,
			prevDiskStats: make(map[string]*statspb.DiskStats),
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "unavailableReadSvcTime",
			deviceName:    "sda",
			readSvcTime:   metricsformatter.Unavailable,
			readOpsCount:  10,
			prevDiskStats: defaultPrevDiskStats,
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "unavailableReadOpsCount",
			deviceName:    "sda",
			readSvcTime:   1000,
			readOpsCount:  metricsformatter.Unavailable,
			prevDiskStats: defaultPrevDiskStats,
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "noDelta",
			deviceName:    "sda",
			readSvcTime:   1000,
			readOpsCount:  11,
			prevDiskStats: defaultPrevDiskStats,
			want:          0,
		},
		{
			name:          "success",
			deviceName:    "sda",
			readSvcTime:   2000,
			readOpsCount:  20,
			prevDiskStats: defaultPrevDiskStats,
			want:          100,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Reader{prevDiskStats: test.prevDiskStats}
			got := r.averageReadResponseTime(test.deviceName, test.readSvcTime, test.readOpsCount)
			if got != test.want {
				t.Errorf("averageReadResponseTime(%q, %d, %d) got=%d, want=%d", test.deviceName, test.readSvcTime, test.readOpsCount, got, test.want)
			}
		})
	}
}

func TestAverageWriteResponseTime(t *testing.T) {
	defaultPrevDiskStats := map[string]*statspb.DiskStats{
		"sda": &statspb.DiskStats{
			WriteSvcTimeMillis: 1000,
			WriteOpsCount:      10,
		},
	}

	tests := []struct {
		name          string
		deviceName    string
		writeSvcTime  int64
		writeOpsCount int64
		prevDiskStats map[string]*statspb.DiskStats
		want          int64
	}{
		{
			name:          "unavailablePrevDiskStats",
			deviceName:    "sda",
			writeSvcTime:  1000,
			writeOpsCount: 10,
			prevDiskStats: make(map[string]*statspb.DiskStats),
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "unavailableWriteSvcTime",
			deviceName:    "sda",
			writeSvcTime:  metricsformatter.Unavailable,
			writeOpsCount: 10,
			prevDiskStats: defaultPrevDiskStats,
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "unavailableWriteOpsCount",
			deviceName:    "sda",
			writeSvcTime:  1000,
			writeOpsCount: metricsformatter.Unavailable,
			prevDiskStats: defaultPrevDiskStats,
			want:          metricsformatter.Unavailable,
		},
		{
			name:          "noDelta",
			deviceName:    "sda",
			writeSvcTime:  2000,
			writeOpsCount: 10,
			prevDiskStats: defaultPrevDiskStats,
			want:          0,
		},
		{
			name:          "success",
			deviceName:    "sda",
			writeSvcTime:  2000,
			writeOpsCount: 20,
			prevDiskStats: defaultPrevDiskStats,
			want:          100,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Reader{prevDiskStats: test.prevDiskStats}
			got := r.averageWriteResponseTime(test.deviceName, test.writeSvcTime, test.writeOpsCount)
			if got != test.want {
				t.Errorf("averageWriteResponseTime(%q, %d, %d) got=%d, want=%d", test.deviceName, test.writeSvcTime, test.writeOpsCount, got, test.want)
			}
		})
	}
}
