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

// Package memorymetricreader provides functionality for collecting OS memory metrics
package memorymetricreader

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	cli "github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	mstatspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

// MemoryMetricReader for reading memory statistics from the OS
type MemoryMetricReader struct {
	OS string
}

var (
	readFile = os.ReadFile
	wmicExec = wmicExecute
)

// MemoryStats reads metrics from the OS for Memory and returns a MemoryStats.
func (r *MemoryMetricReader) MemoryStats() *mstatspb.MemoryStats {
	log.Logger.Debug("Getting memory metrics...")
	var ms *mstatspb.MemoryStats
	if r.OS == "windows" {
		log.Logger.Debug("Geting memory stats for Windows")
		ms = readMemoryStatsForWindows()
	} else if r.OS == "linux" {
		log.Logger.Debug("Geting memory stats for Linux")
		ms = readMemoryStatsForLinux()
	}
	if ms != nil && ms.GetTotal() > 0 && ms.GetFree() > 0 {
		ms.Used = ms.GetTotal() - ms.GetFree()
	}
	if ms != nil {
		log.Logger.Debugf("Memory total: %d", ms.GetTotal())
		log.Logger.Debugf("Memory free: %d", ms.GetFree())
		log.Logger.Debugf("Memory used: %d", ms.GetUsed())
	}
	return ms
}

// Executes wmic on Windows, will be overridden by tests
func wmicExecute(args ...string) (string, string, error) {
	return cli.ExecuteCommand("wmic", args...)
}

// Reads memory stats for Windows, usees wmic command for the OS values
func readMemoryStatsForWindows() *mstatspb.MemoryStats {
	ms := &mstatspb.MemoryStats{}
	o, e, err := wmicExec("computersystem", "get", "TotalPhysicalMemory/Format:List")
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not execute wmic, stderr: %s", e), log.Error(err))
		ms.Total = -1
	} else {
		// NOMUTANTS--precision is the same when dividend 1024*1024 is mutated to (1024*1024)-1
		ms.Total = mbValueFromWmicOutput(o, "TotalPhysicalMemory", 1024*1024)
	}
	o, e, err = wmicExec("OS", "get", "FreePhysicalMemory/Format:List")
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not execute wmic, stderr: %s", e), log.Error(err))
		ms.Free = -1
	} else {
		ms.Free = mbValueFromWmicOutput(o, "FreePhysicalMemory", 1024)
	}
	return ms
}

// Supplies the /proc/meminfo data for Linux, will be overridden by tests
func procMemInfo() string {
	d, err := readFile("/proc/meminfo")
	if err != nil {
		log.Logger.Error("Could not read data from /proc/meminfo", log.Error(err))
		return ""
	}
	return string(d)
}

// Reads memory status for Linux, uses /proc/meminfo for the data
func readMemoryStatsForLinux() *mstatspb.MemoryStats {
	m := procMemInfo()
	log.Logger.Debugf("/proc/meminfo data: %s", m)
	lines := strings.Split(m, "\n")
	ms := &mstatspb.MemoryStats{}
	for _, line := range lines {
		tl := strings.TrimSpace(line)
		log.Logger.Debugf("trimmed line: %s", tl)
		if tl == "" || strings.HasPrefix(tl, "#") {
			continue
		}
		t := strings.Fields(tl)
		// stats file is in kb
		if t[0] == "MemTotal:" {
			n, err := strconv.ParseInt(t[1], 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse MemTotal from %q", t[1]), log.Error(err))
				continue
			}
			ms.Total = n / 1024
		} else if t[0] == "MemFree:" {
			n, err := strconv.ParseInt(t[1], 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse MemFree from %q", t[1]), log.Error(err))
				continue
			}
			ms.Free = n / 1024
		}
	}
	return ms
}

// Converts the output from Windows wmic commands values into MB
func mbValueFromWmicOutput(s string, n string, d int64) int64 {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		l := strings.Split(line, "=")
		if len(l) == 2 && l[0] == n {
			// value will be in bytes so we have to convert to MB
			s := strings.TrimSpace(l[1])
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse wmic output %q for %s", s, n), log.Error(err))
				return -1
			}
			return v / d
		}
	}
	return -1
}
