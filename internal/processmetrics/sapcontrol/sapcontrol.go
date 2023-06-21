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
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"

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
	// Properties is a receiver for sapcontrol functions.
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

	// EnqLock has the attributes returned by sapcontrol's EnqGetLockTable function.
	EnqLock struct {
		LockName, LockArg, LockMode, Owner, OwnerVB string
		UserCountOwner, UserCountOwnerVB            int64
		Client, User, Transaction, Object, Backup   string
	}
)

// ProcessList uses the SapControl command to build a map describing the statuses
// of all SAP processes.
// Parameters are a commandlineexecutor.Execute and commandlineexecutor.Params
// Example Usage:
//
//	params := commandlineexecutor.Params{
//		User:        "hdbadm",
//		Executable:  "/usr/sap/HDB/HDB00/exe/sapcontrol",
//		ArgsToSplit: "-nr 00 -function GetProcessList -format script",
//		Env:         []string{"LD_LIBRARY_PATH=/usr/sap/HDB/HDB00/exe/ld_library"},
//	}
//	sc := &sapcontrol.Properties{&sapb.SAPInstance{}}
//	procs, code, err := sc.ProcessList(commandlineexecutor.ExecuteCommand, params)
//
// Returns:
//   - A map[int]*ProcessStatus where key is the process index as listed by
//     sapcontrol, and the value is an ProcessStatus struct containing process
//     status details.
//   - The exit status returned by sapcontrol command as int.
//   - Error if process detection fails, nil otherwise.
func (p *Properties) ProcessList(ctx context.Context, exec commandlineexecutor.Execute, params commandlineexecutor.Params) (map[int]*ProcessStatus, int, error) {
	result := exec(ctx, params)
	if result.Error != nil && !result.ExitStatusParsed {
		log.Logger.Debugw("Failed to get SAP Process Status", log.Error(result.Error))
		return nil, 0, result.Error
	}

	message, ok := sapcontrolStatus[result.ExitCode]
	if !ok {
		return nil, result.ExitCode, fmt.Errorf("invalid sapcontrol return code: %d", result.ExitCode)
	}
	log.Logger.Debugw("Sapcontrol GetProcessList", "status", result.ExitCode, "message", message, "stdout", result.StdOut)

	names := processNameRegex.FindAllStringSubmatch(result.StdOut, -1)
	if len(names) == 0 {
		expectedFormat := `0 name: <ProcessName>
		0 dispstatus: <Status>
		0 pid: <ProcessID>`
		return nil, 0, fmt.Errorf("output: %q is not in expected format: %q", result.StdOut, expectedFormat)
	}

	dss := processDisplayStatusRegex.FindAllStringSubmatch(result.StdOut, -1)
	pids := processPIDRegex.FindAllStringSubmatch(result.StdOut, -1)
	if len(names) != len(dss) || len(names) != len(pids) {
		return nil, 0, fmt.Errorf("getProcessList - discrepancy in number of processes: %q", result.StdOut)
	}

	return createProcessMap(result, names, dss, pids)
}

func createProcessMap(result commandlineexecutor.Result, names, dss, pids [][]string) (map[int]*ProcessStatus, int, error) {
	// Pass 1 - initialize the map and create struct values with process name.
	processes := make(map[int]*ProcessStatus)
	for _, n := range names {
		if len(n) != 3 {
			continue
		}
		id, err := strconv.Atoi(n[1])
		if err != nil {
			log.Logger.Debugw("Could not parse the name process index", log.Error(err))
			return nil, result.ExitCode, err
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
			log.Logger.Debugw("Could not parse the display status process index", log.Error(err))
			return nil, result.ExitCode, err
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

	log.Logger.Debugw("Process statuses", "statuses", processes)
	return processes, result.ExitCode, nil
}

// ClientInterface contains API methods and has been created for easy testing of the API
type ClientInterface interface {
	GetProcessList() ([]sapcontrolclient.OSProcess, error)
}

// GetProcessList uses the SapControl web API to build a map describing the statuses
// of all SAP processes.
// Parameter is a ClientInterface
// Example Usage:
//
//	scdc := sapcontrolclient.New("02") // returns a Client that implements ClientInterface
//	scp := &sapcontrol.Properties{&sapb.SAPInstance{}}
//	processes, err := scp.GetProcessList(scdc)
//
// Returns:
//   - A map[int]*ProcessStatus where key is the process index according to the process position
//     in the returned process list, and the value is an ProcessStatus struct containing process
//     status details.
//   - Error if sapcontrolclient.GetProcessList fails, nil otherwise.
func (p *Properties) GetProcessList(c ClientInterface) (map[int]*ProcessStatus, error) {
	processes, err := c.GetProcessList()
	if err != nil {
		log.Logger.Debugw("Failed to get SAP Process Status via API call", log.Error(err))
		return nil, err
	}
	log.Logger.Debugw("Sapcontrol GetProcessList", "API response", processes)

	return createProcessMapFromAPIResp(processes)
}

func createProcessMapFromAPIResp(resp []sapcontrolclient.OSProcess) (map[int]*ProcessStatus, error) {
	processes := make(map[int]*ProcessStatus)
	for i, p := range resp {
		if p.Name == "" || p.Dispstatus == "" || p.Pid == 0 {
			continue
		}
		// Sample p.Dispstatus := "SAPControl-GREEN". We want to extract only the status "GREEN".
		splitDs := strings.Split(p.Dispstatus, "-")
		if len(splitDs) != 2 {
			continue
		}
		processes[i] = &ProcessStatus{
			Name:          p.Name,
			DisplayStatus: splitDs[1],
			PID:           fmt.Sprintf("%d", p.Pid),
			IsGreen:       strings.ToUpper(splitDs[1]) == "GREEN",
		}
	}

	log.Logger.Debugw("Process statuses", "statuses", processes)
	return processes, nil
}

// ParseABAPGetWPTable runs and parses the output of sapcontrol function ABAPGetWPTable.
// Returns:
//   - processes - A map with key->worker_process_type and value->total_process_count.
//   - busyProcesses - A map with key->worker_process_type and value->busy_process_count.
func (p *Properties) ParseABAPGetWPTable(ctx context.Context, exec commandlineexecutor.Execute, params commandlineexecutor.Params) (processes, busyProcesses map[string]int, processNameToPID map[string]string, err error) {
	const (
		numberOfColumns = 15
		typeColumn      = 1
		pidColumn       = 2
		timeColumn      = 9
	)

	result := exec(ctx, params)
	if result.Error != nil && !result.ExitStatusParsed {
		log.Logger.Debugw("Failed to run ABAPGetWPTable", log.Error(result.Error))
		return nil, nil, nil, result.Error
	}

	log.Logger.Debugw("Sapcontrol ABAPGetWPTable", "stdout", result.StdOut)

	processes = make(map[string]int)
	busyProcesses = make(map[string]int)
	processNameToPID = make(map[string]string)
	lines := strings.Split(result.StdOut, "\n")
	for _, line := range lines {
		line = emptyChars.ReplaceAllString(line, "")
		row := strings.Split(line, ",")
		if len(row) != numberOfColumns || row[typeColumn] == "Typ" {
			continue
		}
		workProcessType := row[typeColumn]
		processes[workProcessType]++
		processNameToPID[row[pidColumn]] = workProcessType
		if row[timeColumn] != "" {
			busyProcesses[workProcessType]++
		}
	}

	log.Logger.Debugw("Found ABAP Processes", "processcount", processes, "busyprocesses", busyProcesses, "pidMap", processNameToPID)
	return processes, busyProcesses, processNameToPID, nil
}

// ParseQueueStats runs and parses the output of sapcontrol function GetQueueStatistic.
// Returns:
//   - currentQueueUsage - A map with key->queue_type and value->current_queue_usage.
//   - peakQueueUsage - A map with key->queue_type and value->peak_queue_usage.
func (p *Properties) ParseQueueStats(ctx context.Context, exec commandlineexecutor.Execute, params commandlineexecutor.Params) (currentQueueUsage, peakQueueUsage map[string]int, err error) {
	const (
		numberOfColumns         = 6
		typeColumn              = 0
		currentQueueUsageColumn = 1
		peakQueueUsageColumn    = 2
	)

	result := exec(ctx, params)
	if result.Error != nil && !result.ExitStatusParsed {
		log.Logger.Debugw("Failed to run GetQueueStatistic", log.Error(result.Error))
		return nil, nil, result.Error
	}

	currentQueueUsage = make(map[string]int)
	peakQueueUsage = make(map[string]int)
	lines := strings.Split(result.StdOut, "\n")
	for _, line := range lines {
		line = emptyChars.ReplaceAllString(line, "")
		row := strings.Split(line, ",")
		if len(row) != numberOfColumns || row[typeColumn] == "Typ" {
			continue
		}

		queue, current, peak := row[typeColumn], row[currentQueueUsageColumn], row[peakQueueUsageColumn]
		currentVal, err := strconv.Atoi(current)
		if err != nil {
			log.Logger.Debugw("Could not parse current queue usage", log.Error(err))
			continue
		}
		currentQueueUsage[queue] = currentVal

		peakVal, err := strconv.Atoi(peak)
		if err != nil {
			log.Logger.Debugw("Could not parse peak queue usage", log.Error(err))
			continue
		}
		peakQueueUsage[queue] = peakVal
	}

	log.Logger.Debugw("Found Queue stats", "currentqueueusage", currentQueueUsage, "peakqueueusage", peakQueueUsage)
	return currentQueueUsage, peakQueueUsage, nil
}

// EnqGetLockTable parses the output of sapcontrol function EnqGetLockTable.
// Returns:
//   - A slice of EnqLock structs containing lock details.
//   - Error if sapcontrol fails, nil otherwise.
func (p *Properties) EnqGetLockTable(ctx context.Context, exec commandlineexecutor.Execute, params commandlineexecutor.Params) (EnqLocks []*EnqLock, err error) {
	const numberOfColumns = 12
	const (
		lockName = iota
		lockArg
		lockMode
		owner
		ownerVB
		UserCountOwner
		UserCountOwnerVB
		client
		user
		transaction
		object
		backup
	)
	result := exec(ctx, params)
	if result.Error != nil && !result.ExitStatusParsed {
		log.Logger.Debugw("Failed to get SAP Process Status", log.Error(result.Error))
		return nil, result.Error
	}

	message, ok := sapcontrolStatus[result.ExitCode]
	if !ok {
		return nil, fmt.Errorf("invalid sapcontrol return code: %d", result.ExitCode)
	}
	log.Logger.Debugw("Sapcontrol EnqGetLockTable", "status", result.ExitCode, "message", message, "stdout", result.StdOut)

	lines := strings.Split(result.StdOut, "\n")
	for _, line := range lines {
		line = emptyChars.ReplaceAllString(line, "")
		row := strings.Split(line, ",")
		if len(row) != numberOfColumns || row[lockName] == "lock_name" {
			continue
		}

		uco, err := strconv.Atoi(row[UserCountOwner])
		if err != nil {
			log.Logger.Debugw("Could not parse UserCountOwner field in EnqGetLockTable", log.Error(err))
			return nil, err
		}

		ucoVB, err := strconv.Atoi(row[UserCountOwnerVB])
		if err != nil {
			log.Logger.Debugw("Could not parse UserCountOwnerVB Field in EnqGetLockTable", log.Error(err))
			return nil, err
		}

		lock := &EnqLock{
			LockName:         row[lockName],
			LockArg:          row[lockArg],
			LockMode:         row[lockMode],
			Owner:            row[owner],
			OwnerVB:          row[ownerVB],
			UserCountOwner:   int64(uco),
			UserCountOwnerVB: int64(ucoVB),
			Client:           row[client],
			User:             row[user],
			Transaction:      row[transaction],
			Object:           row[object],
			Backup:           row[backup],
		}
		EnqLocks = append(EnqLocks, lock)
	}

	log.Logger.Debugw("EnqLocks successfully parsed", "EnqLocks", EnqLocks)
	return EnqLocks, nil
}
