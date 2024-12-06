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

// Package memorymetricreader tests
package memorymetricreader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	mstatspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestMemoryStats(t *testing.T) {
	for _, v := range []struct {
		readFile    func(string) ([]byte, error)
		wmicExecute func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		os          string
		want        *mstatspb.MemoryStats
	}{
		{
			readFile: func(string) ([]byte, error) {
				return []byte("#some comment line\nMemTotal:       32880828 kB\nMemFree:         3625472 kB\n"), nil
			},
			os: "linux",
			want: &mstatspb.MemoryStats{
				Total: 32880828 / 1024,
				Free:  3625472 / 1024,
				Used:  (32880828 / 1024) - (3625472 / 1024),
			},
		},
		{
			readFile: func(string) ([]byte, error) {
				return []byte("#some comment line\nMemTotal:       32880828.123 kB\nMemFree:         3625472.123 kB\n"), nil
			},
			os:   "linux",
			want: &mstatspb.MemoryStats{},
		},
		{
			readFile: func(string) ([]byte, error) {
				return []byte(""), nil
			},
			os:   "linux",
			want: &mstatspb.MemoryStats{},
		},
		{
			readFile: func(string) ([]byte, error) {
				return nil, errors.New("error")
			},
			os:   "linux",
			want: &mstatspb.MemoryStats{},
		},
		{
			wmicExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				argStr := strings.Join(params.Args, " ")
				if argStr != "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" && argStr != "-Command (Get-WmiObject Win32_OperatingSystem).FreePhysicalMemory/1kb -as [Int]" {
					return commandlineexecutor.Result{
						Error: fmt.Errorf("bad arguments for execute command %q", argStr),
					}
				}
				if argStr == "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" {
					return commandlineexecutor.Result{
						StdOut: "12345\r\n",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "12340\r\n",
				}
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: 12345,
				Free:  12340,
				Used:  5,
			},
		},
		{
			wmicExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: errors.New("error"),
				}
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: -1,
				Free:  -1,
			},
		},
		{
			wmicExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				argStr := strings.Join(params.Args, " ")
				if argStr != "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" && argStr != "-Command (Get-WmiObject Win32_OperatingSystem).FreePhysicalMemory/1kb -as [Int]" {
					return commandlineexecutor.Result{
						Error: fmt.Errorf("bad arguments for execute command %q", argStr),
					}
				}
				if argStr == "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" {
					return commandlineexecutor.Result{
						StdOut: "12345\r\n",
					}
				}
				return commandlineexecutor.Result{
					Error: errors.New("error"),
				}
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: 12345,
				Free:  -1,
			},
		},
		{
			wmicExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				argStr := strings.Join(params.Args, " ")
				if argStr != "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" && argStr != "-Command (Get-WmiObject Win32_OperatingSystem).FreePhysicalMemory/1kb -as [Int]" {
					return commandlineexecutor.Result{
						Error: fmt.Errorf("bad arguments for execute command %q", argStr),
					}
				}
				if argStr == "-Command (Get-WmiObject Win32_OperatingSystem).TotalVisibleMemorySize/1kb -as [Int]" {
					return commandlineexecutor.Result{
						StdOut: "12345.123",
					}
				}
				return commandlineexecutor.Result{}
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: -1,
				Free:  -1,
			},
		},
		{
			wmicExecute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					Error: errors.New("error"),
				}
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: -1,
				Free:  -1,
			},
		},
	} {
		m := New(v.os, v.readFile, v.wmicExecute)
		got := m.MemoryStats(context.Background())
		if diff := cmp.Diff(v.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("MemoryStats for Windows returned unexpected diff (-want +got):\n%s", diff)
		}
	}
}

func TestPaseInt(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int64
	}{
		{
			name: "Empty",
			s:    "",
			want: -1,
		},
		{
			name: "Int",
			s:    "12",
			want: 12,
		},
		{
			name: "Float",
			s:    "12.123",
			want: -1,
		},
		{
			name: "String",
			s:    "abc",
			want: -1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseInt(test.s, "")
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("parseInt() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProcMemInfo(t *testing.T) {
	tests := []struct {
		name       string
		os         string
		fileReader FileReader
		want       string
	}{
		{
			name:       "Test 1",
			os:         "linux",
			fileReader: func(string) ([]byte, error) { return []byte(""), errors.New("error") },
			want:       "",
		},
		{
			name:       "Test 2",
			os:         "linux",
			fileReader: func(string) ([]byte, error) { return []byte("memory info"), nil },
			want:       "memory info",
		},
		{
			name:       "Test 2",
			os:         "windows",
			fileReader: func(string) ([]byte, error) { return []byte("memory info"), nil },
			want:       "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := New(test.os, test.fileReader, nil)
			got := r.procMemInfo()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("procMemInfo() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
