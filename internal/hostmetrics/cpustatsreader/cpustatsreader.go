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
//
// TODO(b/265431344): Collect CPU stats using the gopsutil library.
package cpustatsreader

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

type (
	// FileReader is a function type matching the signature for os.ReadFile.
	FileReader func(string) ([]byte, error)
	// RunCommand is a function type matching the signature for commandlineexecutor.ExecuteCommand.
	RunCommand func(string, ...string) (string, string, error)
	// A Reader is capable of reading cpu statistics from the OS.
	//
	// Due to the assignment of required unexported fields, a Reader must be initialized with New()
	// instead of as a struct literal.
	Reader struct {
		os         string
		fileReader FileReader
		runCommand RunCommand
	}
)

// New instantiates a Reader with the capability to read cpu metrics from linux and windows operating systems.
func New(os string, fileReader FileReader, runCommand RunCommand) *Reader {
	return &Reader{
		os:         os,
		fileReader: fileReader,
		runCommand: runCommand,
	}
}

// The Read method reads CPU metrics from the OS and returns a proto for CpuStats.
func (r *Reader) Read() *statspb.CpuStats {
	log.Logger.Debugf("Reading CPU stats...")
	var s *statspb.CpuStats
	switch r.os {
	case "linux":
		s = r.readCPUStatsForLinux()
	case "windows":
		s = r.readCPUStatsForWindows()
	default:
		log.Logger.Errorf("Encountered an unexpected OS value: %s.", r.os)
		return nil
	}

	if s != nil {
		log.Logger.Debugf("Cpu processor type: %s", s.GetProcessorType())
		log.Logger.Debugf("Cpu count: %d", s.GetCpuCount())
		log.Logger.Debugf("Cpu cores: %d", s.GetCpuCores())
		log.Logger.Debugf("Cpu max mhz: %d", s.GetMaxMhz())
	}
	return s
}

// Reads CPU stats for Windows, usees wmic command for the OS values
func (r *Reader) readCPUStatsForWindows() *statspb.CpuStats {
	s := &statspb.CpuStats{}
	// Use wmic to get the CPU stats
	// Note: must use separated arguments so the windows go exec does not escape the entire argument list
	args := []string{"cpu", "get", "Name,", "MaxClockSpeed,", "NumberOfCores,", "NumberOfLogicalProcessors/Format:List"}
	o, e, err := r.runCommand("wmic", args...)
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

// Reads CPU stats for Linux, uses /proc/cpuinfo for the OS values
func (r *Reader) readCPUStatsForLinux() *statspb.CpuStats {
	s := &statspb.CpuStats{}
	// Use the contents of /proc/cpuinfo to get the CPU stats
	d, err := r.fileReader("/proc/cpuinfo")
	if err != nil {
		log.Logger.Errorf("Could not read data from /proc/cpuinfo", log.Error(err))
	}
	c := string(d)
	log.Logger.Debugf("/proc/cpuinfo data: %q", c)
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
