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

// Package sapcontrol implements generic sapcontrol functions.
package sapcontrol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

var (
	// Expected format: "(Process ID) name: (Process Name)"
	processNameRegex = regexp.MustCompile(`([0-9]+) name: ([a-z|A-Z|_|\+]+)`)
	// Expected format: "(Process ID) dispstatus: (Display Status)"
	processDisplayStatusRegex = regexp.MustCompile(`([0-9]+) dispstatus: ([a-z|A-Z|_|\+]+)`)
	// Expected format: "(Process ID) pid: (PID)"
	processPIDRegex = regexp.MustCompile(`([0-9]+) pid: ([0-9]+)`)

	sapcontrolStatus = map[int]string{
		0: "Last webmethod call successful.",
		1: "Last webmethod call failed, invalid parameter.",
		2: "StartWait, StopWait, WaitforStarted, WaitforStopped RestartServiceWait timed out. CheckSystemCertificates detected warnings",
		3: "GetProcessList succeeded, all processes running correctly. CheckSystemCertificates detected errors.",
		4: "GetProcessList succeeded, all processes stopped.",
	}
	emptyChars = regexp.MustCompile(`[\s\t\n\r]`)
)

type (
	// Properties is a reciever for sapcontrol functions.
	Properties struct {
		Instance *sapb.SAPInstance
	}

	// ProcessStatus has the sap process status.
	ProcessStatus struct {
		Name          string
		DisplayStatus string
		IsGreen       bool
		PID           string
	}

	// RunnerWithEnv is an interface that implements commandlineexecutor.Runner for testability.
	RunnerWithEnv interface {
		RunWithEnv() (stdOut string, stdErr string, code int, err error)
	}
)

// ProcessList uses the SapControl command to build a map describing the statuses
// of all SAP processes.
// Parameter is a RunnerWithEnv interface with below example usage for production flow.
// Example Usage:
//
//	runner := &commandlineexecutor.Runner{
//		User:       "hdbadm",
//		Executable: "/usr/sap/HDB/HDB00/exe/sapcontrol",
//		Args:       "-nr 00 -function GetProcessList -format script",
//		Env:         []string{"LD_LIBRARY_PATH=/usr/sap/HDB/HDB00/exe/ld_library"},
//	}
//	sc := &sapcontrol.Properties{&sapb.SAPInstance{}}
//	procs, code, err := sc.ProcessList(runner)
//
// Returns:
//   - A map[int]*ProcessStatus where key is the process index as listed by
//     sapcontrol, and the value is an ProcessStatus struct containing process
//     status details.
//   - The exit status returned by sapcontrol command as int.
//   - Error if process detection fails, nil otherwise.
func (p *Properties) ProcessList(r RunnerWithEnv) (map[int]*ProcessStatus, int, error) {
	stdOut, _, exitStatus, err := r.RunWithEnv()
	if err != nil {
		log.Logger.Debug("Failed to get SAP Process Status", log.Error(err))
		return nil, 0, err
	}

	message, ok := sapcontrolStatus[exitStatus]
	if !ok {
		return nil, exitStatus, fmt.Errorf("invalid sapcontrol return code: %d", exitStatus)
	}
	log.Logger.Debugf("Sapcontrol returned status %d : %q.", exitStatus, message)

	names := processNameRegex.FindAllStringSubmatch(stdOut, -1)
	if len(names) == 0 {
		expectedFormat := `0 name: <ProcessName>
		0 dispstatus: <Status>
		0 pid: <ProcessID>`
		return nil, 0, fmt.Errorf("output: %q is not in expected format: %q", stdOut, expectedFormat)
	}

	dss := processDisplayStatusRegex.FindAllStringSubmatch(stdOut, -1)
	pids := processPIDRegex.FindAllStringSubmatch(stdOut, -1)
	if len(names) != len(dss) || len(names) != len(pids) {
		return nil, 0, fmt.Errorf("getProcessList - discrepancy in number of processes: %q", stdOut)
	}

	// Pass 1 - initialize the map and create struct values with process name.
	processes := make(map[int]*ProcessStatus)
	for _, n := range names {
		if len(n) != 3 {
			continue
		}
		id, err := strconv.Atoi(n[1])
		if err != nil {
			log.Logger.Debug("Could not parse the name process index", log.Error(err))
			return nil, exitStatus, err
		}
		processes[id] = &ProcessStatus{Name: n[2]}
	}

	// Pass 2 - iterate dss and pids arrays to build displayStatus, IsGreen and pid into the map.
	for i := range dss {
		d := dss[i]
		p := pids[i]
		if len(d) != 3 || len(p) != 3 {
			continue
		}
		id, err := strconv.Atoi(d[1])
		if err != nil {
			log.Logger.Debug("Could not parse the display status process index", log.Error(err))
			return nil, exitStatus, err
		}

		if _, ok := processes[id]; !ok {
			return nil, 0, fmt.Errorf("getProcessList - discrepancy in number of processes, no name entry for process: %q", id)
		}
		processes[id].DisplayStatus = d[2]
		processes[id].PID = p[2]
		if strings.ToUpper(d[2]) == "GREEN" {
			processes[id].IsGreen = true
		}
	}

	log.Logger.Debugf("Process statuses: %v.", processes)
	return processes, exitStatus, nil
}

// ParseABAPGetWPTable runs and parses the output of sapcontrol function ABAPGetWPTable.
// Returns:
//   - processes - A map with key->worker_process_type and value->total_process_count.
//   - busyProcesses - A map with key->worker_process_type and value->busy_process_count.
func (p *Properties) ParseABAPGetWPTable(r RunnerWithEnv) (processes, busyProcesses map[string]int, err error) {
	const (
		numberOfColumns = 15
		typeColumn      = 1
		timeColumn      = 9
	)

	stdOut, _, _, err := r.RunWithEnv()
	if err != nil {
		log.Logger.Debug("Failed to run ABAPGetWPTable", log.Error(err))
		return nil, nil, err
	}

	processes = make(map[string]int)
	busyProcesses = make(map[string]int)
	lines := strings.Split(stdOut, "\n")
	for _, line := range lines {
		line = emptyChars.ReplaceAllString(line, "")
		row := strings.Split(line, ",")
		if len(row) != numberOfColumns || row[typeColumn] == "Typ" {
			continue
		}
		workProcessType := row[typeColumn]
		processes[workProcessType]++
		if row[timeColumn] != "" {
			busyProcesses[workProcessType]++
		}
	}

	log.Logger.Debugf("Found ABAP Process counts: %v, busy ABAP Processes: %v.", processes, busyProcesses)
	return processes, busyProcesses, nil
}

// ParseQueueStats runs and parses the output of sapcontrol function GetQueueStatistic.
// Returns:
//   - currentQueueUsage - A map with key->queue_type and value->current_queue_usage.
//   - peakQueueUsage - A map with key->queue_type and value->peak_queue_usage.
func (p *Properties) ParseQueueStats(r RunnerWithEnv) (currentQueueUsage, peakQueueUsage map[string]int, err error) {
	const (
		numberOfColumns         = 6
		typeColumn              = 0
		currentQueueUsageColumn = 1
		peakQueueUsageColumn    = 2
	)

	stdOut, _, _, err := r.RunWithEnv()
	if err != nil {
		log.Logger.Debug("Failed to run GetQueueStatistic", log.Error(err))
		return nil, nil, err
	}

	currentQueueUsage = make(map[string]int)
	peakQueueUsage = make(map[string]int)
	lines := strings.Split(stdOut, "\n")
	for _, line := range lines {
		line = emptyChars.ReplaceAllString(line, "")
		row := strings.Split(line, ",")
		if len(row) != numberOfColumns || row[typeColumn] == "Typ" {
			continue
		}

		queue, current, peak := row[typeColumn], row[currentQueueUsageColumn], row[peakQueueUsageColumn]
		currentVal, err := strconv.Atoi(current)
		if err != nil {
			log.Logger.Debug("Could not parse current queue usage", log.Error(err))
			continue
		}
		currentQueueUsage[queue] = currentVal

		peakVal, err := strconv.Atoi(peak)
		if err != nil {
			log.Logger.Debug("Could not parse peak queue usage", log.Error(err))
			continue
		}
		peakQueueUsage[queue] = peakVal
	}

	log.Logger.Debugf("Found Queue stats - current queue usage: %v, peak queue usage: %v.", currentQueueUsage, peakQueueUsage)
	return currentQueueUsage, peakQueueUsage, nil
}
