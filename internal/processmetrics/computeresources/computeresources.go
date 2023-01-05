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
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
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
	// Parameters struct contains the parameters necessary for computeresources package common methods.
	Parameters struct {
		executor         commandExecutor
		config           *cnfpb.Configuration
		client           cloudmonitoring.TimeSeriesCreator
		cpuMetricPath    string
		memoryMetricPath string
		fileReader       maintenance.FileReader
		sapInstance      *sapb.SAPInstance
		runner           sapcontrol.RunnerWithEnv
	}

	// cpuTimes is a struct representing the various time values required for calculating
	// CPU consumption:
	// user: Time elapsed by a process in user mode.
	// kernel: Time elapsed by a process in kernel mode.
	// start: Time since a process started.
	cpuTimes struct {
		user   float64
		kernel float64
		start  float64
	}

	// ProcessInfo holds the relevant info for processes, including its name and pid.
	ProcessInfo struct {
		Name string
		PID  string
	}
)

func collectControlProcesses(p Parameters) []*ProcessInfo {
	var processInfos []*ProcessInfo
	cmd := "ps"
	args := "-e -o comm,pid"
	stdout, _, err := p.executor(cmd, args)
	if err != nil {
		log.Logger.Debug(fmt.Sprintf("Error while executing command: %s args: %s", cmd, args), log.Error(err))
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
			log.Logger.Errorf("Could not parse output of %s, for regex: %s", cmd+args, process)
			continue
		}
		processInfos = append(processInfos, &ProcessInfo{Name: pnameAndPid[0], PID: pnameAndPid[1]})
	}
	return processInfos
}

func 	collectProcessesForInstance(p Parameters) []*ProcessInfo {
	if p.sapInstance == nil {
		log.Logger.Error("Error getting ProcessList in computeresources, no sapInstance set.")
		return nil
	}

	sc := &sapcontrol.Properties{p.sapInstance}
	processes, _, err := sc.ProcessList(p.runner)
	if err != nil {
		log.Logger.Error("Error getting ProcessList in computeresources", log.Error(err))
		return nil
	}

	var processInfos []*ProcessInfo
	for _, process := range processes {
		processInfos = append(processInfos, &ProcessInfo{Name: process.Name, PID: process.PID})
	}
	return processInfos
}

// collectCPUPerProcess collects CPU utilization per process for HANA, Netweaver and SAP control processes.
func collectCPUPerProcess(p Parameters, processes []*ProcessInfo) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	ticks, err := clockTicks(p)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Invalid value for CLK_TCK: %f", ticks), log.Error(err))
		return nil
	}
	for _, process := range processes {
		procStatFilePath := strings.Replace(linuxProcStatPath, "PID", process.PID, 1)
		content, err := p.fileReader.Read(procStatFilePath)
		if err != nil {
			log.Logger.Error(fmt.Sprintf("Could not read file: %s due to error", procStatFilePath), log.Error(err))
			continue
		}
		outputSlice := strings.Split(string(content), " ")

		times, err := processCPUTimeValues(process.PID, outputSlice)
		if err != nil {
			log.Logger.Error(log.Error(err))
			continue
		}

		content, err = p.fileReader.Read("/proc/uptime")
		if err != nil {
			log.Logger.Error("Could not read file: /proc/uptime due to error", log.Error(err))
			continue
		}
		outputSlice = strings.Split(string(content), " ")
		if len(outputSlice) != 2 {
			log.Logger.Error("/proc/uptime content could not be parsed correctly.")
			continue
		}
		uptime, err := strconv.ParseFloat(outputSlice[0], 64)
		if err != nil {
			log.Logger.Error(fmt.Sprintf("Could not parse uptime for PID: %s", process.PID), log.Error(err))
			continue
		}
		log.Logger.Debugf("utime: %f, stime: %f, starttime: %f, uptime: %f for PID: %s", times.user, times.kernel, times.start, uptime, process.PID)

		userCPU, userCPUErr := calculatePercentageCPU(times.user, uptime, times.start, ticks)
		kernelCPU, kernelCPUErr := calculatePercentageCPU(times.kernel, uptime, times.start, ticks)

		if userCPUErr != nil && kernelCPUErr != nil {
			log.Logger.Errorf("Could not calculate userCPU: Error(%s), kernelCPU: Error(%s).", userCPUErr, kernelCPUErr)
			continue
		}
		labels := map[string]string{
			"process": formatProcessName(process.Name) + ":" + process.PID,
		}
		if p.sapInstance != nil {
			labels["sid"] = p.sapInstance.GetSapsid()
			labels["instance_nr"] = p.sapInstance.GetInstanceNumber()
		}
		log.Logger.Debugf("Creating CPU utilization metric for Process: %s PID: %s", process.Name, process.PID)
		params := timeseries.Params{
			CloudProp:    p.config.CloudProperties,
			MetricType:   metricURL + p.cpuMetricPath,
			MetricLabels: labels,
			Timestamp:    tspb.Now(),
			Float64Value: userCPU + kernelCPU,
			BareMetal:    p.config.BareMetal,
		}
		ts := timeseries.BuildFloat64(params)
		metrics = append(metrics, &sapdiscovery.Metrics{TimeSeries: ts})
	}
	return metrics
}

// collectMemoryPerProcess is a function responsible for collecting memory utilization
// per process for Hana, Netweaver and SAP control processes.
func collectMemoryPerProcess(p Parameters, processes []*ProcessInfo) []*sapdiscovery.Metrics {
	var metrics []*sapdiscovery.Metrics
	for _, process := range processes {
		content, err := p.fileReader.Read(strings.Replace(linuxMemoryStatusFilePath, "PID", process.PID, 1))
		if err != nil {
			log.Logger.Error(fmt.Sprintf("Could not read file: %s due to error", "/proc/"+process.PID+"/status"), log.Error(err))
			return nil
		}
		output := string(content)
		for _, memoryRegex := range memoryTypeRegexList {
			rexp := regexp.MustCompile(memoryRegex)
			expr := rexp.FindString(output)
			val := newlineChars.ReplaceAllString(expr, "")
			val = strings.ReplaceAll(val, ":", "")
			val = multiSpaceChars.ReplaceAllString(val, " ")
			memoryTypeAndVal := strings.Split(val, " ")
			if len(memoryTypeAndVal) < 2 {
				log.Logger.Error("/proc/" + process.PID + "/status file content could not be parsed correctly.")
				continue
			}
			memory, err := strconv.ParseFloat(memoryTypeAndVal[1], 64)

			if err != nil {
				log.Logger.Errorf("Could not parse memory type: %s, from: %s to float64.", memoryTypeAndVal[0], memoryTypeAndVal[1])
				continue
			}

			labels := map[string]string{
				"process": formatProcessName(process.Name) + ":" + process.PID,
				"memType": memoryTypeAndVal[0],
			}
			if p.sapInstance != nil {
				labels["sid"] = p.sapInstance.GetSapsid()
				labels["instance_nr"] = p.sapInstance.GetInstanceNumber()
			}
			params := timeseries.Params{
				CloudProp:    p.config.CloudProperties,
				MetricType:   metricURL + p.memoryMetricPath,
				MetricLabels: labels,
				Timestamp:    tspb.Now(),
				Float64Value: memory / 1024,
				BareMetal:    p.config.BareMetal,
			}
			ts := timeseries.BuildFloat64(params)
			metrics = append(metrics, &sapdiscovery.Metrics{TimeSeries: ts})
		}
	}
	return metrics
}

// calculatePercentageCPU calculates the percentage CPU using upTime, startTime and uTime or sTime.
func calculatePercentageCPU(xxTime, upTime, startTime, ticks float64) (float64, error) {
	var percentCPU float64 = 0
	// runningSeconds represents the duration which a process has run.
	runningSeconds := upTime - (startTime / ticks)
	if runningSeconds == 0 {
		log.Logger.Debug("Got runningSeconds as 0")
		return 0, errors.New("invalid value for runningSeconds: 0")
	}
	percentCPU = metricsformatter.ToPercentage(((xxTime / ticks) / runningSeconds), 4)
	return percentCPU, nil
}

// processCPUTimeValues is responsible for retrieving the cpuTimes from the corrresponding process
// proc/pid/stat file.
func processCPUTimeValues(pid string, procStatFileSlice []string) (cpuTimes, error) {
	if len(procStatFileSlice) < 22 {
		return cpuTimes{}, errors.New("invalid content from /proc/" + pid + "/stat")
	}
	uTime, err := strconv.ParseFloat(procStatFileSlice[13], 64)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not parse uTime for PID: %s", pid), log.Error(err))
		return cpuTimes{}, err
	}
	sTime, err := strconv.ParseFloat(procStatFileSlice[14], 64)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not parse sTime for PID: %s", pid), log.Error(err))
		return cpuTimes{}, err
	}
	startTime, err := strconv.ParseFloat(procStatFileSlice[21], 64)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not parse startTime for PID: %s", pid), log.Error(err))
		return cpuTimes{}, err
	}
	return cpuTimes{uTime, sTime, startTime}, nil
}

// clockTicks is responsible for fetching CPU clock tick(CLK_TCK). In case of a error a 0 value for
// CLK_TCK and error is returned.
func clockTicks(p Parameters) (float64, error) {
	cmd := "getconf"
	args := "CLK_TCK"
	stdout, _, err := p.executor(cmd, args)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Error while executing command: %s %s", cmd, args), log.Error(err))
		return 0, err
	}
	ticks, err := strconv.ParseFloat(newlineChars.ReplaceAllLiteralString(stdout, ""), 64)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Error while parsing output for clock ticks: %s", stdout), log.Error(err))
		return 0, err
	}
	if ticks == 0 {
		log.Logger.Errorf("Invalid value for CLK_TCK: %f", ticks)
		return 0, errors.New("invalid value for CLK_TCK: 0")
	}
	return ticks, nil
}

func formatProcessName(pname string) string {
	result := forwardSlashChar.ReplaceAllString(pname, "_")
	result = dashChars.ReplaceAllString(result, "_")
	return result
}
