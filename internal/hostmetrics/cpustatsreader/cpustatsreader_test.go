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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

func TestCPUStats_Linux(t *testing.T) {
	// override readFile that reads from the OS using os.ReadFile
	defer func(f func(string) ([]byte, error)) { readFile = f }(readFile)
	readFile = func(string) ([]byte, error) {
		return []byte("#some comment\nprocessor       : 0\nmodel name      : Intel(R) Xeon(R) CPU @ 2.20GHz\ncpu MHz         : 2200.58\ncpu cores       : 2\nprocessor       : 0\n\n"), nil
	}

	want := &statspb.CpuStats{
		CpuCount:      2,
		MaxMhz:        2201,
		CpuCores:      2,
		ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
	}

	c := CPUMetricReader{OS: "linux"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for Linux returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_LinuxNoRows(t *testing.T) {
	// override readFile that reads from the OS using os.ReadFile
	defer func(f func(string) ([]byte, error)) { readFile = f }(readFile)
	readFile = func(string) ([]byte, error) {
		return []byte(""), nil
	}

	want := &statspb.CpuStats{}

	c := CPUMetricReader{OS: "linux"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for LinuxNoRows returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_LinuxError(t *testing.T) {
	// override readFile that reads from the OS using os.ReadFile
	defer func(f func(string) ([]byte, error)) { readFile = f }(readFile)
	readFile = func(string) ([]byte, error) {
		return nil, errors.New("Could not read from file")
	}

	want := &statspb.CpuStats{}

	c := CPUMetricReader{OS: "linux"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for LinuxError returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_LinuxParseError(t *testing.T) {
	// override readFile that reads from the OS using os.ReadFile
	defer func(f func(string) ([]byte, error)) { readFile = f }(readFile)
	readFile = func(string) ([]byte, error) {
		return []byte("#some comment\nprocessor       : 0\nmodel name      : Intel(R) Xeon(R) CPU @ 2.20GHz\ncpu MHz         : not-a-float\ncpu cores       : 2.1\nprocessor       : 0\n\n"), nil
	}

	want := &statspb.CpuStats{
		CpuCount:      2,
		ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
	}

	c := CPUMetricReader{OS: "linux"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for LinuxParseError returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_Windows(t *testing.T) {
	// override clistExecute for the wmic command execution
	defer func(f func(args ...string) (string, string, error)) { clistExecute = f }(clistExecute)
	clistExecute = func(args ...string) (string, string, error) {
		argStr := strings.Join(args, " ")
		if argStr != "cpu get Name, MaxClockSpeed, NumberOfCores, NumberOfLogicalProcessors/Format:List" {
			return "", "", fmt.Errorf("bad arguments for execute command %q", argStr)
		}
		return "\n\r\n\r\nMaxClockSpeed=2200\nName=Intel(R) Xeon(R) CPU @ 2.20GHz\r\nNumberOfCores=4\nNumberOfLogicalProcessors=8\n\r\n\r\n", "", nil
	}

	want := &statspb.CpuStats{
		CpuCount:      8,
		MaxMhz:        2200,
		CpuCores:      4,
		ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
	}

	c := CPUMetricReader{OS: "windows"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for Windows returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_WindowsNoResults(t *testing.T) {
	// override clistExecute for the wmic command execution
	defer func(f func(args ...string) (string, string, error)) { clistExecute = f }(clistExecute)
	clistExecute = func(...string) (string, string, error) {
		return "", "", nil
	}

	want := &statspb.CpuStats{}

	c := CPUMetricReader{OS: "windows"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for WindowsNoResults returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_WindowsError(t *testing.T) {
	// override clistExecute for the wmic command execution
	defer func(f func(args ...string) (string, string, error)) { clistExecute = f }(clistExecute)
	clistExecute = func(...string) (string, string, error) {
		return "", "some error output", errors.New("Could not execute command")
	}

	want := &statspb.CpuStats{}

	c := CPUMetricReader{OS: "windows"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for WindowsError returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestCPUStats_WindowsParseError(t *testing.T) {
	// override clistExecute for the wmic command execution
	defer func(f func(args ...string) (string, string, error)) { clistExecute = f }(clistExecute)
	clistExecute = func(args ...string) (string, string, error) {
		argStr := strings.Join(args, " ")
		if argStr != "cpu get Name, MaxClockSpeed, NumberOfCores, NumberOfLogicalProcessors/Format:List" {
			return "", "", fmt.Errorf("bad arguments for execute command %q", argStr)
		}
		return "\n\r\n\r\nMaxClockSpeed=not-a-float\nName=Intel(R) Xeon(R) CPU @ 2.20GHz\r\nNumberOfCores=4.1\nNumberOfLogicalProcessors=8.1\n\r\n\r\n", "", nil
	}

	want := &statspb.CpuStats{
		ProcessorType: "Intel(R) Xeon(R) CPU @ 2.20GHz",
	}

	c := CPUMetricReader{OS: "windows"}
	got := c.CPUStats()

	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("CPUStats for WindowsParseError returned unexpected diff (-want +got):\n%s", diff)
	}
}
