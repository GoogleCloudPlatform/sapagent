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

// Package cpustatsreader tests
package cpustatsreader

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestRead(t *testing.T) {
	tests := []struct {
		name   string
		os     string
		reader FileReader
		exec   commandlineexecutor.Execute
		want   *statspb.CpuStats
	}{
		{
			name: "linuxSuccess",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("#some comment\nprocessor       : 0\nmodel name      : Intel(R) Xeon(R) CPU @ 2.20GHz\ncpu MHz         : 2200.58\ncpu cores       : 2\nprocessor       : 0\n\n"), nil
			},
			want: &statspb.CpuStats{
				CpuCount:      2,
				MaxMhz:        2201,
				CpuCores:      2,
				ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
			},
		},
		{
			name: "linuxErrReadFile",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return nil, errors.New("Read File error")
			},
			want: &statspb.CpuStats{},
		},
		{
			name: "linuxNoResults",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte(""), nil
			},
			want: &statspb.CpuStats{},
		},
		{
			name: "linuxParseErrors",
			os:   "linux",
			reader: func(string) ([]byte, error) {
				return []byte("#some comment\nprocessor       : 0\nmodel name      : Intel(R) Xeon(R) CPU @ 2.20GHz\ncpu MHz         : not-a-float\ncpu cores       : 2.5\nprocessor       : 0\n\n"), nil
			},
			want: &statspb.CpuStats{
				CpuCount:      2,
				ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
			},
		},
		{
			name: "windowsSuccess",
			os:   "windows",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `{
					"Name":  "Intel(R) Xeon(R) CPU @ 2.20GHz",
					"MaxClockSpeed":  2200,
					"NumberOfCores":  4,
					"NumberOfLogicalProcessors":  8
					}`,
					StdErr: "",
				}
			},
			want: &statspb.CpuStats{
				CpuCount:      8,
				MaxMhz:        2200,
				CpuCores:      4,
				ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
			},
		},
		{
			name: "windowsErrExecuteCommand",
			os:   "windows",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "stdError output",
					Error:  errors.New("Execute Command error"),
				}
			},
			want: &statspb.CpuStats{},
		},
		{
			name: "windowsNoResults",
			os:   "windows",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{}
			},
			want: &statspb.CpuStats{},
		},
		{
			name: "windowsParseErors",
			os:   "windows",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: `{\n"Name":  "Intel(R) Xeon(R) CPU @ 2.20GHz",\n"MaxClockSpeed":  "not-a-float",\n"NumberOfCores":  4.5,\n"NumberOfLogicalProcessors":  8.5\n}`,
					StdErr: "",
				}
			},
			want: &statspb.CpuStats{},
		},
		{
			name: "unknownOS",
			os:   "mac",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := New(test.os, test.reader, test.exec)
			got := r.Read(context.Background())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Read() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
