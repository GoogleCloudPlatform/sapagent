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
	"context"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mstatspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

type (
	// FileReader is a function type matching the signature for os.ReadFile.
	FileReader func(string) ([]byte, error)
	// A Reader is capable of reading memory statistics from the OS.
	//
	// Due to the assignment of required unexported fields, a Reader must be initialized with New()
	// instead of as a struct literal.
	Reader struct {
		os         string
		fileReader FileReader
		execute    commandlineexecutor.Execute
	}
)

// New instantiates a Reader with the capability to read memory metrics from linux and windows operating systems.
func New(os string, fileReader FileReader, execute commandlineexecutor.Execute) *Reader {
	return &Reader{
		os:         os,
		fileReader: fileReader,
		execute:    execute,
	}
}

// MemoryStats reads metrics from the OS for Memory and returns a MemoryStats.
func (r *Reader) MemoryStats(ctx context.Context) *mstatspb.MemoryStats {
	log.Logger.Debug("Getting memory metrics...")
	var ms *mstatspb.MemoryStats
	if r.os == "windows" {
		log.Logger.Debug("Geting memory stats for Windows")
		ms = r.readMemoryStatsForWindows(ctx)
	} else {
		log.Logger.Debug("Geting memory stats for Linux")
		ms = r.readMemoryStatsForLinux()
	}
	if ms.GetTotal() > 0 && ms.GetFree() > 0 {
		ms.Used = ms.GetTotal() - ms.GetFree()
	}
	if ms != nil {
		log.Logger.Debugw("Memory stats", "total", ms.GetTotal(), "free", ms.GetFree(), "used", ms.GetUsed())
	}
	return ms
}

// readMemoryStatsForWindows Reads memory stats for Windows, uses wmic command for the OS values
func (r *Reader) readMemoryStatsForWindows(ctx context.Context) *mstatspb.MemoryStats {
	ms := &mstatspb.MemoryStats{}
	result := r.execute(ctx, commandlineexecutor.Params{
		Executable: "wmic",
		Args:       []string{"computersystem", "get", "TotalPhysicalMemory/Format:List"},
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not execute wmic get TotalPhysicalMemory/Format:List", "stdout", result.StdOut, "stderr", result.StdErr, "error", result.Error)
		ms.Total = -1
	} else {
		// NOMUTANTS--precision is the same when dividend 1024*1024 is mutated to (1024*1024)-1
		ms.Total = mbValueFromWmicOutput(result.StdOut, "TotalPhysicalMemory", 1024*1024)
	}
	result = r.execute(ctx, commandlineexecutor.Params{
		Executable: "wmic",
		Args:       []string{"OS", "get", "FreePhysicalMemory/Format:List"},
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not execute wmic get FreePhysicalMemory/Format:List", "stdout", result.StdOut, "stderr", result.StdErr, "error", result.Error)
		ms.Free = -1
	} else {
		ms.Free = mbValueFromWmicOutput(result.StdOut, "FreePhysicalMemory", 1024)
	}
	return ms
}

// procMemInfo supplies the /proc/meminfo data for Linux, will be overridden by tests
func (r *Reader) procMemInfo() string {
	if r.os != "linux" {
		return ""
	}

	d, err := r.fileReader("/proc/meminfo")
	if err != nil {
		log.Logger.Errorw("Could not read data from /proc/meminfo", log.Error(err))
		return ""
	}
	return string(d)
}

// readMemoryStatsForLinux reads memory status for Linux, uses /proc/meminfo for the data
func (r *Reader) readMemoryStatsForLinux() *mstatspb.MemoryStats {
	m := r.procMemInfo()
	log.Logger.Debugw("/proc/meminfo data", "data", m)
	lines := strings.Split(m, "\n")
	ms := &mstatspb.MemoryStats{}
	for _, line := range lines {
		tl := strings.TrimSpace(line)
		log.Logger.Debugw("trimmed line", "line", tl)
		if tl == "" || strings.HasPrefix(tl, "#") {
			continue
		}
		t := strings.Fields(tl)
		// stats file is in kb
		if t[0] == "MemTotal:" {
			n, err := strconv.ParseInt(t[1], 10, 64)
			if err != nil {
				log.Logger.Errorw("Could not parse MemTotal", "value", t[1], "error", err)
				continue
			}
			ms.Total = n / 1024
		} else if t[0] == "MemFree:" {
			n, err := strconv.ParseInt(t[1], 10, 64)
			if err != nil {
				log.Logger.Errorw("Could not parse MemFree", "value", t[1], "error", err)
				continue
			}
			ms.Free = n / 1024
		}
	}
	return ms
}

// mbValueFromWmicOutput converts the output from Windows wmic commands values into MB
func mbValueFromWmicOutput(s string, n string, d int64) int64 {
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		l := strings.Split(line, "=")
		if len(l) == 2 && l[0] == n {
			// value will be in bytes so we have to convert to MB
			s := strings.TrimSpace(l[1])
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				log.Logger.Errorw("Could not parse wmic output", "output", s, "value", n, "error", err)
				return -1
			}
			return v / d
		}
	}
	return -1
}
