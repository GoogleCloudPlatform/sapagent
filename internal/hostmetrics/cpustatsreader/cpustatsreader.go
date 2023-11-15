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

// Package cpustatsreader provides functionality for collecting OS cpu metrics.
package cpustatsreader

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// FileReader is a function type matching the signature for os.ReadFile.
	FileReader func(string) ([]byte, error)
	// A Reader is capable of reading cpu statistics from the OS.
	//
	// Due to the assignment of required unexported fields, a Reader must be initialized with New()
	// instead of as a struct literal.
	Reader struct {
		os         string
		fileReader FileReader
		execute    commandlineexecutor.Execute
	}
)

// New instantiates a Reader with the capability to read cpu metrics from linux and windows operating systems.
func New(os string, fileReader FileReader, execute commandlineexecutor.Execute) *Reader {
	return &Reader{
		os:         os,
		fileReader: fileReader,
		execute:    execute,
	}
}

// The Read method reads CPU metrics from the OS and returns a proto for CpuStats.
func (r *Reader) Read(ctx context.Context) *statspb.CpuStats {
	log.CtxLogger(ctx).Debug("Reading CPU stats...")
	var s *statspb.CpuStats
	switch r.os {
	case "linux":
		s = r.readCPUStatsForLinux()
	case "windows":
		s = r.readCPUStatsForWindows(ctx)
	default:
		log.CtxLogger(ctx).Errorw("Encountered an unexpected OS value", "value", r.os)
		return nil
	}

	if s != nil {
		log.CtxLogger(ctx).Debugw("Cpu stats", "type", s.GetProcessorType(), "count", s.GetCpuCount(), "cores", s.GetCpuCores(), "maxmhz", s.GetMaxMhz())
	}
	return s
}

// Reads CPU stats for Windows, uses PowerShell command for the OS values.
func (r *Reader) readCPUStatsForWindows(ctx context.Context) *statspb.CpuStats {
	s := &statspb.CpuStats{}

	// Note: must use separated arguments so the windows go exec does not escape the entire argument list
	args := []string{"-NoProfile", "-NonInteractive", "-Command", "Get-WmiObject Win32_Processor | Select-Object -Property Name, MaxClockSpeed, NumberOfCores, NumberOfLogicalProcessors | ConvertTo-Json"}
	result := r.execute(ctx, commandlineexecutor.Params{
		Executable: "PowerShell",
		Args:       args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not execute PowerShell Get-WmiObject Win32_Processor | select-object Name,MaxClockSpeed,NumberOfCores,NumberOfLogicalProcessors | convertto-json", "stdout", result.StdOut, "stderr", result.StdErr, "error", result.Error)
		return s
	}
	jsonResult := struct {
		Name                      string
		MaxClockSpeed             int64
		NumberOfCores             int64
		NumberOfLogicalProcessors int64
	}{}
	if err := json.Unmarshal([]byte(result.StdOut), &jsonResult); err != nil {
		log.CtxLogger(ctx).Errorw("Could not unmarshall PowerShell output", "stdout", result.StdOut, "jsonResult", jsonResult, "error", err)
		return s
	}
	s.ProcessorType = jsonResult.Name
	s.MaxMhz = jsonResult.MaxClockSpeed
	s.CpuCores = jsonResult.NumberOfCores
	s.CpuCount = jsonResult.NumberOfLogicalProcessors
	return s
}

// Reads CPU stats for Linux, uses /proc/cpuinfo for the OS values
func (r *Reader) readCPUStatsForLinux() *statspb.CpuStats {
	s := &statspb.CpuStats{}
	// Use the contents of /proc/cpuinfo to get the CPU stats
	d, err := r.fileReader("/proc/cpuinfo")
	if err != nil {
		log.Logger.Errorw("Could not read data from /proc/cpuinfo", log.Error(err))
	}
	c := string(d)
	log.Logger.Debugw("/proc/cpuinfo data", "data", c)
	lines := strings.Split(c, "\n")
	cc := int64(0)
	for _, line := range lines {
		tl := strings.TrimSpace(line)
		if strings.HasPrefix(tl, "#") {
			continue
		}
		t := strings.Split(tl, ":")
		if len(t) < 2 {
			continue
		}
		val := strings.TrimSpace(t[1])
		switch strings.TrimSpace(t[0]) {
		case "processor":
			cc++
		case "model name":
			s.ProcessorType = val
		case "cpu MHz":
			n, err := strconv.ParseFloat(val, 64)
			if err != nil {
				log.Logger.Errorw("Could not parse cpu MHz from value", "value", val, "error", err)
				continue
			}
			s.MaxMhz = int64(math.Round(n))
		case "cpu cores":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				log.Logger.Errorw("Could not parse cpu cores from value", "value", val, "error", err)
				continue
			}
			s.CpuCores = n
		}
	}
	s.CpuCount = cc
	return s
}
