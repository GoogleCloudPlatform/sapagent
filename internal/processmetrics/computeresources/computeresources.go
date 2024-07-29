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
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// Enum for choosing the metric type to collect.
const (
	collectCPUMetric = iota
	collectMemoryMetric
	collectDiskIOPSMetric
)

const (
	metricURL                 = "workload.googleapis.com"
	linuxProcStatPath         = "/proc/PID/stat"
	linuxMemoryStatusFilePath = "/proc/PID/status"
	thousand                  = 1000.0
	million                   = 1000000.0
)

var (
	memoryTypeRegexList = []string{`\nVmSize:.*\n`, `\nVmRSS:.*\n`, `\nVmSwap:.*\n`}

	multiSpaceChars  = regexp.MustCompile(`\s+`)
	newlineChars     = regexp.MustCompile(`\n`)
	forwardSlashChar = regexp.MustCompile(`\/`)
	dashChars        = regexp.MustCompile(`\-`)
)

type (
	// Parameters struct contains the parameters necessary for computeresources package common methods.
	Parameters struct {
		executor             commandlineexecutor.Execute
		Config               *cnfpb.Configuration
		client               cloudmonitoring.TimeSeriesCreator
		cpuMetricPath        string
		memoryMetricPath     string
		iopsReadsMetricPath  string
		iopsWritesMetricPath string
		SAPInstance          *sapb.SAPInstance
		NewProc              NewProcessWithContextHelper
		getProcessListParams commandlineexecutor.Params
		getABAPWPTableParams commandlineexecutor.Params
		SAPControlClient     sapcontrol.ClientInterface
		LastValue            map[string]*process.IOCountersStat
	}

	// NewProcessWithContextHelper is a strategy which creates a new process type
	// from PSUtil library using the provided context and PID.
	NewProcessWithContextHelper func(context.Context, int32) (UsageReader, error)

	// UsageReader is an interface providing abstraction over PSUtil methods for calculating CPU
	// percentage and memory usage stats for a process and makes them unit testable.
	UsageReader interface {
		CPUPercentWithContext(context.Context) (float64, error)
		MemoryInfoWithContext(context.Context) (*process.MemoryInfoStat, error)
		IOCountersWithContext(context.Context) (*process.IOCountersStat, error)
	}

	// ProcessInfo holds the relevant info for processes, including its name and pid.
	ProcessInfo struct {
		Name string
		PID  string
	}

	// MemoryUsage holds the memory usage metrics for a process.
	MemoryUsage struct {
		VMS  *Metric
		RSS  *Metric
		Swap *Metric
	}

	// DiskUsage holds the disk IOPS metrics for a process.
	DiskUsage struct {
		DeltaReads  *Metric
		DeltaWrites *Metric
	}

	// Metric is a struct to hold the metric value and its metadata.
	Metric struct {
		Value       float64
		ProcessInfo *ProcessInfo
		TimeStamp   *tspb.Timestamp
	}
)

func newProc(ctx context.Context, fn NewProcessWithContextHelper, pid int32) (UsageReader, error) {
	if fn == nil {
		return process.NewProcessWithContext(ctx, pid)
	}
	return fn(ctx, pid)
}

func collectControlProcesses(ctx context.Context, p Parameters) []*ProcessInfo {
	var processInfos []*ProcessInfo
	cmd := "ps"
	args := "-e -o comm,pid"
	result := p.executor(ctx, commandlineexecutor.Params{
		Executable:  cmd,
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command", "command", cmd, "args", args, "error", result.Error)
		return nil
	}

	process := `\nsapstart.*\n`
	processNameWithPIDRegex := regexp.MustCompile(process)
	res := processNameWithPIDRegex.FindAllStringSubmatch(result.StdOut, -1)
	for _, p := range res {
		// Removing all new line chars from the string:
		// `\nhdbindexserver    8921\n` -> `hdbindexserver   8921`.
		val := newlineChars.ReplaceAllString(p[0], "")
		// Removing all multi space chars from the string:
		// `hdbindexserver    8921` --> `hdbindexserver 8921`.
		val = multiSpaceChars.ReplaceAllString(val, " ")
		pnameAndPid := strings.Split(val, " ")
		if len(pnameAndPid) != 2 {
			log.CtxLogger(ctx).Debugw("Could not parse output", "command", cmd+args, "regex", process)
			continue
		}
		processInfos = append(processInfos, &ProcessInfo{Name: pnameAndPid[0], PID: pnameAndPid[1]})
	}
	return processInfos
}

// CollectProcessesForInstance returns the list of
// processes running in an SAPInstance.
func CollectProcessesForInstance(ctx context.Context, p Parameters) []*ProcessInfo {
	if p.SAPInstance == nil {
		log.CtxLogger(ctx).Debug("Error getting ProcessList in computeresources, no sapInstance set.")
		return nil
	}

	var (
		processes    map[int]*sapcontrol.ProcessStatus
		err          error
		processInfos []*ProcessInfo
	)
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	scc := p.SAPControlClient
	processes, err = sc.GetProcessList(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Debug("Error performing GetProcessList web method in computeresources", log.Error(err))
	}
	wpDetails, err := sc.ABAPGetWPTable(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error getting ABAP processes from ABAPGetWPTable web method", log.Error(err))
	} else {
		for pid, proc := range wpDetails.ProcessNameToPID {
			processInfos = append(processInfos, &ProcessInfo{Name: proc, PID: pid})
		}
	}

	for _, process := range processes {
		processInfos = append(processInfos, &ProcessInfo{Name: process.Name, PID: process.PID})
	}
	return processInfos
}

// collectTimeSeriesMetrics collects one time series data point
// per process of the given metric type (CPU, Memory or disk IOPS).
func collectTimeSeriesMetrics(ctx context.Context, p Parameters, processes []*ProcessInfo, metricType int) ([]*mrpb.TimeSeries, error) {
	var metrics []*mrpb.TimeSeries
	var metricsCollectionErr error

	switch metricType {
	case collectCPUMetric:
		// Collect CPU Usage per process.
		cpuUsages, err := CollectCPUPerProcess(ctx, p, processes)
		if err != nil {
			metricsCollectionErr = err
		}
		// Build time series metrics.
		for _, cpuUsage := range cpuUsages {
			// buildMetricLabel can never be nil here
			// as cpuUsage from CollectCPUPerProcess is never nil.
			metrics = append(metrics, createMetrics(p.cpuMetricPath, buildMetricLabel(collectCPUMetric, "", cpuUsage), cpuUsage.Value, p))
		}
	case collectMemoryMetric:
		// Collect Memory Usage per process.
		memoryUsages, err := CollectMemoryPerProcess(ctx, p, processes)
		if err != nil {
			metricsCollectionErr = err
		}
		// Build time series metrics.
		for _, memoryUsage := range memoryUsages {
			// buildMetricLabel can never be nil here
			// as memoryUsage from CollectMemoryPerProcess is never nil.
			metrics = append(metrics, createMetrics(p.memoryMetricPath, buildMetricLabel(collectMemoryMetric, "VmSize", memoryUsage.VMS), memoryUsage.VMS.Value, p))
			metrics = append(metrics, createMetrics(p.memoryMetricPath, buildMetricLabel(collectMemoryMetric, "VmRSS", memoryUsage.RSS), memoryUsage.RSS.Value, p))
			metrics = append(metrics, createMetrics(p.memoryMetricPath, buildMetricLabel(collectMemoryMetric, "VmSwap", memoryUsage.Swap), memoryUsage.Swap.Value, p))
		}
	case collectDiskIOPSMetric:
		// Collect IOPS Usage per process.
		diskUsages, err := CollectIOPSPerProcess(ctx, p, processes)
		if err != nil {
			metricsCollectionErr = err
		}
		// Build time series metrics.
		for _, diskUsage := range diskUsages {
			// buildMetricLabel can never be nil here
			// as diskUsage from CollectIOPSPerProcess is never nil.
			metrics = append(metrics, createMetrics(p.iopsReadsMetricPath, buildMetricLabel(collectDiskIOPSMetric, "", diskUsage.DeltaReads), diskUsage.DeltaReads.Value, p))
			metrics = append(metrics, createMetrics(p.iopsWritesMetricPath, buildMetricLabel(collectDiskIOPSMetric, "", diskUsage.DeltaWrites), diskUsage.DeltaWrites.Value, p))
		}
	default:
		metricsCollectionErr = fmt.Errorf("Invalid metric type: %v", metricType)
	}

	return metrics, metricsCollectionErr
}

// buildMetricLabel builds the metric label for a given metric type.
func buildMetricLabel(metricType int, metricLabel string, metric *Metric) map[string]string {
	if metric == nil {
		return nil
	}
	processLabel := FormatProcessLabel(metric.ProcessInfo.Name, metric.ProcessInfo.PID)
	labels := map[string]string{
		"process": processLabel,
	}
	if metricType == collectMemoryMetric {
		// metricLabel is the memory type: VmSize, VmRSS, VmSwap.
		labels["memType"] = metricLabel
	}
	return labels
}

// CollectCPUPerProcess collects CPU utilization per process for HANA, Netweaver and SAP control processes.
func CollectCPUPerProcess(ctx context.Context, p Parameters, processes []*ProcessInfo) ([]*Metric, error) {
	var cpuUsages []*Metric
	var metricsCollectionErr error
	for _, processInfo := range processes {
		pid, err := strconv.Atoi(processInfo.PID)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not parse PID", "pid", processInfo.PID, "process", processInfo.Name, "error", err)
			continue
		}
		proc, err := newProc(ctx, p.NewProc, int32(pid))
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not create process", "pid", pid, "process", processInfo.Name, "error", err)
			metricsCollectionErr = err
			continue
		}
		cpuUsage, err := proc.CPUPercentWithContext(ctx)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not get process CPU stats", "pid", pid, "error", err)
			metricsCollectionErr = err
			continue
		}
		cpuUsages = append(cpuUsages, &Metric{
			ProcessInfo: processInfo,
			Value:       cpuUsage / float64(runtime.NumCPU()),
			TimeStamp:   tspb.Now(),
		})
	}
	return cpuUsages, metricsCollectionErr
}

// CollectMemoryPerProcess collects memory utilization per process
// in megabytes for HANA, Netweaver and SAP control processes.
func CollectMemoryPerProcess(ctx context.Context, p Parameters, processes []*ProcessInfo) ([]*MemoryUsage, error) {
	var memoryUsages []*MemoryUsage
	var metricsCollectionErr error
	for _, processInfo := range processes {
		pid, err := strconv.Atoi(processInfo.PID)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not parse PID", "pid", processInfo.PID, "process", processInfo.Name, "error", err)
			continue
		}
		proc, err := newProc(ctx, p.NewProc, int32(pid))
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not create process", "pid", pid, "process", processInfo.Name, "error", err)
			metricsCollectionErr = err
			continue
		}
		memoryUsage, err := proc.MemoryInfoWithContext(ctx)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not get process memory stats", "pid", pid, "error", err)
			metricsCollectionErr = err
			continue
		}
		memoryUsages = append(memoryUsages, &MemoryUsage{
			VMS:  &Metric{ProcessInfo: processInfo, Value: float64(memoryUsage.VMS) / million, TimeStamp: tspb.Now()},
			RSS:  &Metric{ProcessInfo: processInfo, Value: float64(memoryUsage.RSS) / million, TimeStamp: tspb.Now()},
			Swap: &Metric{ProcessInfo: processInfo, Value: float64(memoryUsage.Swap) / million, TimeStamp: tspb.Now()},
		})
	}
	return memoryUsages, metricsCollectionErr
}

// CollectIOPSPerProcess is responsible for collecting IOPS per process using gopsutil IOCounters data
// and computing the delta between current value and last value of bytes read and written.
func CollectIOPSPerProcess(ctx context.Context, p Parameters, processes []*ProcessInfo) ([]*DiskUsage, error) {
	var diskUsages []*DiskUsage
	var metricsCollectionErr error
	for _, processInfo := range processes {
		pid, err := strconv.Atoi(processInfo.PID)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not parse PID", "pid", processInfo.PID, "process", processInfo.Name, "error", err)
			continue
		}
		proc, err := newProc(ctx, p.NewProc, int32(pid))
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not create process", "pid", pid, "process", processInfo.Name, "error", err)
			metricsCollectionErr = err
			continue
		}
		currVal, err := proc.IOCountersWithContext(ctx)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not get process IOPS stats", "pid", pid, "process", processInfo.Name, "error", err)
			metricsCollectionErr = err
			continue
		}
		key := fmt.Sprintf("%s:%s", processInfo.Name, processInfo.PID)
		if _, ok := p.LastValue[key]; !ok {
			log.CtxLogger(ctx).Debugw("not creating metric since last value is not updated for IOPS stats", "pid", pid)
			p.LastValue[key] = currVal
			continue
		}
		deltaReads := float64(currVal.ReadBytes-p.LastValue[key].ReadBytes) / thousand
		deltaWrites := float64(currVal.WriteBytes-p.LastValue[key].WriteBytes) / thousand
		p.LastValue[key] = currVal
		freq := p.Config.GetCollectionConfiguration().GetProcessMetricsFrequency()
		diskUsages = append(diskUsages, &DiskUsage{
			DeltaReads:  &Metric{ProcessInfo: processInfo, Value: deltaReads / float64(freq), TimeStamp: tspb.Now()},
			DeltaWrites: &Metric{ProcessInfo: processInfo, Value: deltaWrites / float64(freq), TimeStamp: tspb.Now()},
		})
	}
	return diskUsages, metricsCollectionErr
}

func createMetrics(mPath string, labels map[string]string, val float64, p Parameters) *mrpb.TimeSeries {
	if p.SAPInstance != nil {
		labels["sid"] = p.SAPInstance.GetSapsid()
		labels["instance_nr"] = p.SAPInstance.GetInstanceNumber()
	}
	ts := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		Float64Value: val,
		BareMetal:    p.Config.BareMetal,
	}
	log.Logger.Debugw("Creating metric for instance", "metric", mPath, "value", val, "instancenumber", p.SAPInstance.GetInstanceNumber(), "labels", labels)
	return timeseries.BuildFloat64(ts)
}

// FormatProcessLabel creates a unique label for a process.
func FormatProcessLabel(pname, pid string) string {
	result := forwardSlashChar.ReplaceAllString(pname, "_")
	result = dashChars.ReplaceAllString(result, "_")
	return result + ":" + pid
}
