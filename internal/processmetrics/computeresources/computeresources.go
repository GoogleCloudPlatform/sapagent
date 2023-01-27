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

// Package computeresources provides code for collection of compute resources metrics
// like CPU and memory per process for various Hana, Netweaver and SAP Control Processes.
package computeresources

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"

	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

const (
	metricURL                 = "workload.googleapis.com"
	linuxProcStatPath         = "/proc/PID/stat"
	linuxMemoryStatusFilePath = "/proc/PID/status"
)

var (
	memoryTypeRegexList = []string{`\nVmSize:.*\n`, `\nVmRSS:.*\n`, `\nVmSwap:.*\n`}

	multiSpaceChars  = regexp.MustCompile(`\s+`)
	newlineChars     = regexp.MustCompile(`\n`)
	forwardSlashChar = regexp.MustCompile(`\/`)
	dashChars        = regexp.MustCompile(`\-`)
)

type (
	// commandExecutor is a function to execute command. Production callers
	// to pass commandlineexecutor.ExpandAndExecuteCommand while calling
	// this package's APIs.
	commandExecutor func(string, string) (string, string, error)
	// parameters struct contains the parameters necessary for computeresources package common methods.
	parameters struct {
		executor         commandExecutor
		config           *cnfpb.Configuration
		client           cloudmonitoring.TimeSeriesCreator
		cpuMetricPath    string
		memoryMetricPath string
		sapInstance      *sapb.SAPInstance
		runner           sapcontrol.RunnerWithEnv
		newProc          newProcessWithContextHelper
	}

	// newProcessWithContextHelper is a strategy which creates a new process type
	// from PSUtil library using the provided context and PID.
	newProcessWithContextHelper func(context.Context, int32) (usageReader, error)

	// usageReader is an interface providing abstraction over PSUtil methods for calculating CPU
	// percentage and memory usage stats for a process and makes them unit testable.
	usageReader interface {
		CPUPercentWithContext(context.Context) (float64, error)
		MemoryInfoWithContext(context.Context) (*process.MemoryInfoStat, error)
	}

	// ProcessInfo holds the relevant info for processes, including its name and pid.
	ProcessInfo struct {
		Name string
		PID  string
	}
)

func newProc(ctx context.Context, fn newProcessWithContextHelper, pid int32) (usageReader, error) {
	if fn == nil {
		return process.NewProcessWithContext(ctx, pid)
	}
	return fn(ctx, pid)
}

func collectControlProcesses(p parameters) []*ProcessInfo {
	var processInfos []*ProcessInfo
	cmd := "ps"
	args := "-e -o comm,pid"
	stdout, _, err := p.executor(cmd, args)
	if err != nil {
		log.Logger.Debugw("Error while executing command", "command", cmd, "args", args, "error", err)
		return nil
	}

	process := `\nsapstart.*\n`
	processNameWithPIDRegex := regexp.MustCompile(process)
	res := processNameWithPIDRegex.FindAllStringSubmatch(stdout, -1)
	for _, p := range res {
		// Removing all new line chars from the string:
		// `\nhdbindexserver    8921\n` -> `hdbindexserver   8921`.
		val := newlineChars.ReplaceAllString(p[0], "")
		// Removing all multi space chars from the string:
		// `hdbindexserver    8921` --> `hdbindexserver 8921`.
		val = multiSpaceChars.ReplaceAllString(val, " ")
		pnameAndPid := strings.Split(val, " ")
		if len(pnameAndPid) != 2 {
			log.Logger.Errorw("Could not parse output", "command", cmd+args, "regex", process)
			continue
		}
		processInfos = append(processInfos, &ProcessInfo{Name: pnameAndPid[0], PID: pnameAndPid[1]})
	}
	return processInfos
}

func collectProcessesForInstance(p parameters) []*ProcessInfo {
	if p.sapInstance == nil {
		log.Logger.Error("Error getting ProcessList in computeresources, no sapInstance set.")
		return nil
	}

	sc := &sapcontrol.Properties{p.sapInstance}
	processes, _, err := sc.ProcessList(p.runner)
	if err != nil {
		log.Logger.Errorw("Error getting ProcessList in computeresources", log.Error(err))
		return nil
	}

	var processInfos []*ProcessInfo
	for _, process := range processes {
		processInfos = append(processInfos, &ProcessInfo{Name: process.Name, PID: process.PID})
	}
	return processInfos
}

// collectCPUPerProcess collects CPU utilization per process for HANA, Netweaver and SAP control processes.
func collectCPUPerProcess(ctx context.Context, p parameters, processes []*ProcessInfo) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	for _, processInfo := range processes {
		pid, err := strconv.Atoi(processInfo.PID)
		if err != nil {
			log.Logger.Errorw("Could not parse PID", "pid", processInfo.PID, "process", processInfo.Name, "error", err)
			continue
		}
		proc, err := newProc(ctx, p.newProc, int32(pid))
		if err != nil {
			log.Logger.Errorw("Could not create process", "pid", pid, "process", processInfo.Name, "error", err)
			continue
		}
		labels := map[string]string{
			"process": formatProcesLabel(processInfo.Name, processInfo.PID),
		}
		cpuusage, err := proc.CPUPercentWithContext(ctx)
		if err != nil {
			log.Logger.Errorw("Could not get process CPU stats", "pid", pid, "error", err)
			continue
		}
		metrics = append(metrics, createMetrics(p.cpuMetricPath, labels, cpuusage, p))
	}
	return metrics
}

// collectMemoryPerProcess is a function responsible for collecting memory utilization
// per process for Hana, Netweaver and SAP control processes.
func collectMemoryPerProcess(ctx context.Context, p parameters, processes []*ProcessInfo) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	for _, processInfo := range processes {
		pid, err := strconv.Atoi(processInfo.PID)
		if err != nil {
			log.Logger.Debugw("Could not parse PID", "pid", processInfo.PID, "process", processInfo.Name, "error", err)
			continue
		}
		proc, err := newProc(ctx, p.newProc, int32(pid))
		if err != nil {
			log.Logger.Debugw("Could not create process", "pid", pid, "process", processInfo.Name, "error", err)
			continue
		}
		memoryUsage, err := proc.MemoryInfoWithContext(ctx)
		if err != nil {
			log.Logger.Debugw("Could not get process memory stats", "pid", pid, "error", err)
			continue
		}
		vmSizeLables := map[string]string{
			"process": formatProcesLabel(processInfo.Name, processInfo.PID),
			"memType": "VmSize",
		}
		vmSizeMetrics := createMetrics(p.memoryMetricPath, vmSizeLables, float64(memoryUsage.VMS), p)
		rSSLables := map[string]string{
			"process": formatProcesLabel(processInfo.Name, processInfo.PID),
			"memType": "VmRSS",
		}
		rSSMetrics := createMetrics(p.memoryMetricPath, rSSLables, float64(memoryUsage.RSS), p)
		swapLables := map[string]string{
			"process": formatProcesLabel(processInfo.Name, processInfo.PID),
			"memType": "VmSwap",
		}
		swapMetrics := createMetrics(p.memoryMetricPath, swapLables, float64(memoryUsage.Swap), p)
		metrics = append(metrics, vmSizeMetrics, rSSMetrics, swapMetrics)
	}
	return metrics
}

func createMetrics(mPath string, labels map[string]string, val float64, p parameters) *sapdiscovery.Metrics {
	if p.sapInstance != nil {
		labels["sid"] = p.sapInstance.GetSapsid()
		labels["instance_nr"] = p.sapInstance.GetInstanceNumber()
	}
	ts := timeseries.Params{
		CloudProp:    p.config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		Float64Value: val,
		BareMetal:    p.config.BareMetal,
	}
	log.Logger.Debugw("Creating metric for instance", "metric", mPath, "value", val, "instancenumber", p.sapInstance.GetInstanceNumber(), "labels", labels)
	return &sapdiscovery.Metrics{TimeSeries: timeseries.BuildFloat64(ts)}
}

func formatProcesLabel(pname, pid string) string {
	result := forwardSlashChar.ReplaceAllString(pname, "_")
	result = dashChars.ReplaceAllString(result, "_")
	return result + ":" + pid
}
