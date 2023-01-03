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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	mstatspb "github.com/GoogleCloudPlatform/sap-agent/protos/stats"
)

func TestMemoryStats(t *testing.T) {
	for _, v := range []struct {
		readFile    func(string) ([]byte, error)
		wmicExecute func(args ...string) (string, string, error)
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
				return []byte(""), nil
			},
			os:   "linux",
			want: &mstatspb.MemoryStats{},
		},
		{
			wmicExecute: func(args ...string) (string, string, error) {
				argStr := strings.Join(args, " ")
				if argStr != "computersystem get TotalPhysicalMemory/Format:List" && argStr != "OS get FreePhysicalMemory/Format:List" {
					return "", "", fmt.Errorf("Bad arguments for execute command %q", argStr)
				}
				if argStr == "computersystem get TotalPhysicalMemory/Format:List" {
					return "\n\nTotalPhysicalMemory=34356005437 \n   \n    \n", "", nil
				}
				return "\n\nFreePhysicalMemory=31442364 \n   \n    \n", "", nil
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: 34356005437 / (1024 * 1024),
				Free:  31442364 / 1024,
				Used:  (34356005437 / (1024 * 1024)) - (31442364 / 1024),
			},
		},
		{
			wmicExecute: func(...string) (string, string, error) {
				return "", "", errors.New("error")
			},
			os: "windows",
			want: &mstatspb.MemoryStats{
				Total: -1,
				Free:  -1,
			},
		},
	} {
		readFile = v.readFile
		wmicExec = v.wmicExecute
		m := MemoryMetricReader{OS: v.os}
		got := m.MemoryStats()
		if diff := cmp.Diff(v.want, got, protocmp.Transform()); diff != "" {
			t.Errorf("MemoryStats for Windows returned unexpected diff (-want +got):\n%s", diff)
		}
	}
}
