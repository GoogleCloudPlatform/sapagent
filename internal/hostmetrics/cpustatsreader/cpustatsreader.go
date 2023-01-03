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

// Package cpustatsreader provides functionality for collecting OS cpu metrics
package cpustatsreader

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	cli "github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	statspb "github.com/GoogleCloudPlatform/sap-agent/protos/stats"
)

// CPUMetricReader for reading cpu statistics from the OS
type CPUMetricReader struct {
	OS string
}

var (
	readFile     = os.ReadFile
	clistExecute = wmicClistExecute
)

// CPUStats reads metrics from the OS for CPU and returns a CpuStats.
func (r *CPUMetricReader) CPUStats() *statspb.CpuStats {
	log.Logger.Debugf("Getting cpu metrics...")
	var s *statspb.CpuStats
	if r.OS == "windows" {
		log.Logger.Debugf("Geting cpu stats for Windows")
		s = readCPUStatsForWindows()
	} else if r.OS == "linux" {
		log.Logger.Debugf("Geting cpu stats for Linux")
		s = readCPUStatsForLinux()
	}
	if s != nil {
		log.Logger.Debugf("Cpu processor type: %s", s.GetProcessorType())
		log.Logger.Debugf("Cpu count: %d", s.GetCpuCount())
		log.Logger.Debugf("Cpu cores: %d", s.GetCpuCores())
		log.Logger.Debugf("Cpu max mhz: %d", s.GetMaxMhz())
	}
	return s
}

// Executes the wmic for CPU on Windows, will be overridden by tests
func wmicClistExecute(args ...string) (string, string, error) {
	// Note: must use separated arguments so the windows go exec does not escape the entire argument list
	return cli.ExecuteCommand("wmic", args...)
}

// Reads CPU stats for Windows, usees wmic command for the OS values
func readCPUStatsForWindows() *statspb.CpuStats {
	s := &statspb.CpuStats{}
	// Using wmic to get the CPU info
	o, e, err := clistExecute("cpu", "get", "Name,", "MaxClockSpeed,", "NumberOfCores,", "NumberOfLogicalProcessors/Format:List")
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not execute wmic, stderr: %s", e), log.Error(err))
		return s
	}
	o = strings.ReplaceAll(o, "\r", "")
	lines := strings.Split(o, "\n")
	for _, line := range lines {
		l := strings.Split(line, "=")
		if len(l) < 2 {
			continue
		}
		switch l[0] {
		case "Name":
			s.ProcessorType = l[1]
		case "MaxClockSpeed":
			n, err := strconv.ParseFloat(l[1], 64)
			if err != nil {
				log.Logger.Errorf(fmt.Sprintf("Could not parse NumberOfLogicalProcessors from %q", l[1]), log.Error(err))
				continue
			}
			s.MaxMhz = int64(math.Round(n))
		case "NumberOfCores":
			n, err := strconv.ParseInt(l[1], 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse NumberOfLogicalProcessors from %q", l[1]), log.Error(err))
				continue
			}
			s.CpuCores = n
		case "NumberOfLogicalProcessors":
			n, err := strconv.ParseInt(l[1], 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse NumberOfLogicalProcessors from %q", l[1]), log.Error(err))
				continue
			}
			s.CpuCount = n
		}
	}
	return s
}

// Supplies the /proc/cpuinfo data for Linux, will be overridden by tests
func procCPUInfo() string {
	d, err := readFile("/proc/cpuinfo")
	if err != nil {
		log.Logger.Errorf("Could not read data from /proc/cpuinfo", log.Error(err))
		return ""
	}
	return string(d)
}

// Reads CPU stats for Linux, usees /proc/cpuinfo for the OS values
func readCPUStatsForLinux() *statspb.CpuStats {
	s := &statspb.CpuStats{}
	c := procCPUInfo()
	log.Logger.Debugf("/proc/cpuinfo data: %s", c)
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
				log.Logger.Error(fmt.Sprintf("Could not parse cpu MHz from %q", val), log.Error(err))
				continue
			}
			s.MaxMhz = int64(math.Round(n))
		case "cpu cores":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				log.Logger.Error(fmt.Sprintf("Could not parse cpu cores from %q", val), log.Error(err))
				continue
			}
			s.CpuCores = n
		}
	}
	s.CpuCount = cc
	return s
}
