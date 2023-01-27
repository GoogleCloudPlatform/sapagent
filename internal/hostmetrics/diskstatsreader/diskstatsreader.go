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

// Package diskstatsreader provides functionality for collecting OS disk metrics.
package diskstatsreader

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/exp/maps"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/metricsformatter"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	statspb "github.com/GoogleCloudPlatform/sapagent/protos/stats"
)

const (
	posDeviceName       = 2
	posReadOps          = 3
	posReadSvcTime      = 6
	posWriteOps         = 7
	posWriteSvcTime     = 10
	posQueueLength      = 11
	requiredFieldsCount = 12
)

type (
	// FileReader is a function type matching the signature for os.ReadFile.
	FileReader func(string) ([]byte, error)
	// RunCommand is a function type matching the signature for commandlineexecutor.ExpandAndExecuteCommand.
	RunCommand func(string, ...string) (string, string, error)
	// A Reader is capable of reading disk metrics from the OS.
	//
	// Due to the assignment of required unexported fields, a Reader must be initialized with New()
	// instead of as a struct literal.
	Reader struct {
		os            string
		fileReader    FileReader
		runCommand    RunCommand
		prevDiskStats map[string]*statspb.DiskStats
	}
)

// New instantiates a Reader with the capability to read disk metrics from linux and windows operating systems.
func New(os string, fileReader FileReader, runCommand RunCommand) *Reader {
	return &Reader{
		os:            os,
		fileReader:    fileReader,
		runCommand:    runCommand,
		prevDiskStats: make(map[string]*statspb.DiskStats),
	}
}

// Read reads disk metrics from the OS and returns a collection of disk stats by device mapping.
func (r *Reader) Read(ip *iipb.InstanceProperties) *statspb.DiskStatsCollection {
	var currentDiskStats map[string]*statspb.DiskStats
	switch r.os {
	case "linux":
		currentDiskStats = r.readDiskStatsForLinux(ip)
	case "windows":
		currentDiskStats = r.readDiskStatsForWindows(ip)
	default:
		log.Logger.Errorw("Encountered an unexpected OS value", "value", r.os)
		return nil
	}
	r.prevDiskStats = currentDiskStats
	return &statspb.DiskStatsCollection{DiskStats: maps.Values(currentDiskStats)}
}

/*
 * readDiskStatsForLinux obtains disk metrics from the /proc/diskstats file.
 *
 * Format for /proc/diskstats
 *
 * https://www.kernel.org/doc/Documentation/ABI/testing/procfs-diskstats
 *
 * The /proc/diskstats file displays the I/O statistics of block devices.
 * Each line contains the following 14 fields:
 *
 * 	1 - major number
 * 	2 - minor mumber
 * 	3 - device name
 * 	4 - reads completed successfully
 * 	5 - reads merged
 * 	6 - sectors read
 * 	7 - time spent reading (ms)
 * 	8 - writes completed
 * 	9 - writes merged
 * 	10 - sectors written
 * 	11 - time spent writing (ms)
 * 	12 - I/Os currently in progress
 * 	13 - time spent doing I/Os (ms)
 * 	14 - weighted time spent doing I/Os (ms)
 *
 * Kernel 4.18+ appends four more fields for discard tracking putting the total at 18:
 *
 * 	15 - discards completed successfully
 * 	16 - discards merged
 * 	17 - sectors discarded
 * 	18 - time spent discarding
 *
 * Kernel 5.5+ appends two more fields for flush requests:
 *
 * 	19 - flush requests completed successfully
 * 	20 - time spent flushing
 */
func (r *Reader) readDiskStatsForLinux(instanceProps *iipb.InstanceProperties) map[string]*statspb.DiskStats {
	contents, err := r.fileReader("/proc/diskstats")
	log.Logger.Debugw("File /proc/diskstats contains the following data", "data", string(contents))
	if err != nil {
		log.Logger.Errorw("Could not read data from /proc/diskstats", log.Error(err))
		return nil
	}

	diskStats := make(map[string]*statspb.DiskStats)
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		tl := strings.TrimSpace(line)
		// Skip comment lines in the file.
		if strings.HasPrefix(tl, "#") {
			continue
		}
		tokens := strings.Fields(tl)
		if len(tokens) < requiredFieldsCount {
			if len(tokens) > 0 {
				log.Logger.Warnw("Unexpected disk stats file format in /proc/diskstats", "requiredfieldscount", requiredFieldsCount, "fieldscount", len(tokens))
			}
			continue
		}
		deviceName := tokens[posDeviceName]
		if !deviceMappingExists(deviceName, instanceProps.GetDisks()) {
			// These are expected, just logging as debug
			log.Logger.Debugw("No device mapping found for disk", "devicename", deviceName)
			continue
		}
		log.Logger.Debugw("Adding disk stats for device", "devicename", deviceName)

		readOpsCount, err := strconv.ParseInt(tokens[posReadOps], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse read ops count for device", "devicename", deviceName, "error", err)
			readOpsCount = metricsformatter.Unavailable
		}
		readSvcTimeMillis, err := strconv.ParseInt(tokens[posReadSvcTime], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse read svc time for device", "devicename", deviceName, "error", err)
			readSvcTimeMillis = metricsformatter.Unavailable
		}
		writeOpsCount, err := strconv.ParseInt(tokens[posWriteOps], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse write ops count for device", "devicename", deviceName, "error", err)
			writeOpsCount = metricsformatter.Unavailable
		}
		writeSvcTimeMillis, err := strconv.ParseInt(tokens[posWriteSvcTime], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse write svc time for device", "devicename", deviceName, "error", err)
			writeSvcTimeMillis = metricsformatter.Unavailable
		}
		queueLength, err := strconv.ParseInt(tokens[posQueueLength], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse queue length for device", "devicename", deviceName, "error", err)
			queueLength = metricsformatter.Unavailable
		}

		diskStats[deviceName] = &statspb.DiskStats{
			DeviceName:                     deviceName,
			ReadOpsCount:                   readOpsCount,
			ReadSvcTimeMillis:              readSvcTimeMillis,
			WriteOpsCount:                  writeOpsCount,
			WriteSvcTimeMillis:             writeSvcTimeMillis,
			QueueLength:                    queueLength,
			AverageReadResponseTimeMillis:  r.averageReadResponseTime(deviceName, readSvcTimeMillis, readOpsCount),
			AverageWriteResponseTimeMillis: r.averageWriteResponseTime(deviceName, writeSvcTimeMillis, writeOpsCount),
		}
		log.Logger.Debugw("Disk stats", "diskstats", diskStats[deviceName])
	}

	return diskStats
}

// readDiskStatsForWindows obtains disk metrics from the command line.
func (r *Reader) readDiskStatsForWindows(instanceProps *iipb.InstanceProperties) map[string]*statspb.DiskStats {
	diskStats := make(map[string]*statspb.DiskStats)
	for _, disk := range instanceProps.GetDisks() {
		diskNumber, ok := parseWindowsDiskNumber(disk.GetMapping())
		if !ok {
			log.Logger.Infow("Could not get disk number from device mapping", "mapping", disk.GetMapping())
			continue
		}
		// Note: must use separated arguments so the windows go exec does not escape the entire argument list
		var args []string
		args = append(args, "-command")
		args = append(args, "$(Get-Counter")
		args = append(args, fmt.Sprintf(`'\PhysicalDisk(%d*)\Avg.`, diskNumber))
		args = append(args, "Disk")
		args = append(args, "sec/Read').CounterSamples[0].CookedValue;Write-Host")
		args = append(args, "';';$(Get-Counter")
		args = append(args, fmt.Sprintf(`'\PhysicalDisk(%d*)\Avg.`, diskNumber))
		args = append(args, "Disk")
		args = append(args, "sec/Write').CounterSamples[0].CookedValue;Write-Host")
		args = append(args, "';';$(Get-Counter")
		args = append(args, fmt.Sprintf(`'\PhysicalDisk(%d*)\Current`, diskNumber))
		args = append(args, "Disk")
		args = append(args, "Queue")
		args = append(args, "Length').CounterSamples[0].CookedValue")

		stdOut, stdErr, err := r.runCommand("powershell", args...)
		stdOut = strings.Replace(strings.Replace(stdOut, "\n", "", -1), "\r", "", -1)
		log.Logger.Debugw("PowerShell command returned data", "stdout", stdOut, "stderr", stdErr, "error", err)
		if err != nil {
			log.Logger.Warnw("Could not get stats for disk", "devicename", disk.GetDeviceName(), "mapping", disk.GetMapping(), "number", diskNumber)
			continue
		}
		values := strings.Split(stdOut, ";")
		if len(values) != 3 {
			log.Logger.Warnw("Unexpected output format when fetching disk stats", "stdout", stdOut, "devicename", disk.GetDeviceName(), "mapping", disk.GetMapping(), "number", diskNumber, "valueslength", len(values))
			continue
		}

		averageReadResponseTime := int64(metricsformatter.Unavailable)
		averageRead, err := strconv.ParseFloat(values[0], 64)
		if err != nil {
			log.Logger.Warnw("Could not parse average read response time from output", "output", values[0])
		} else {
			averageReadResponseTime = int64(math.Round(averageRead * 1000))
		}
		averageWriteResponseTime := int64(metricsformatter.Unavailable)
		averageWrite, err := strconv.ParseFloat(values[1], 64)
		if err != nil {
			log.Logger.Warnw("Could not parse average write response time from output", "output", values[1])
		} else {
			averageWriteResponseTime = int64(math.Round(averageWrite * 1000))
		}
		queueLength, err := strconv.ParseInt(values[2], 10, 64)
		if err != nil {
			log.Logger.Warnw("Could not parse queue length from output", "output", values[2])
			queueLength = metricsformatter.Unavailable
		}

		diskStats[disk.GetMapping()] = &statspb.DiskStats{
			DeviceName:                     disk.GetMapping(),
			AverageReadResponseTimeMillis:  averageReadResponseTime,
			AverageWriteResponseTimeMillis: averageWriteResponseTime,
			QueueLength:                    queueLength,
		}
		log.Logger.Debugw("Disk stats", "diskstats", diskStats[disk.GetMapping()])
	}

	return diskStats
}

// averageReadResponseTime calculates the average response time, calculated as (read service time / read ops count).
//
// For a linux system, the read service time and read ops count are stored as rolling totals.
// A calculation of the average response time over the duration of the metric collection period
// must strip away the previous values so that we are left with a delta between previous and current.
func (r *Reader) averageReadResponseTime(deviceName string, currReadSvcTime, currReadOpsCount int64) int64 {
	if currReadSvcTime == metricsformatter.Unavailable || currReadOpsCount == metricsformatter.Unavailable {
		return metricsformatter.Unavailable
	}
	prev, ok := r.prevDiskStats[deviceName]
	if !ok {
		return metricsformatter.Unavailable
	}
	return calculateAverage(currReadSvcTime-prev.GetReadSvcTimeMillis(), currReadOpsCount-prev.GetReadOpsCount())
}

// averageWriteResponseTime calculates the average response time, calculated as (write service time / write ops count).
//
// For a linux system, the write service time and write ops count are stored as rolling totals.
// A calculation of the average response time over the duration of the metric collection period
// must strip away the previous values so that we are left with a delta between previous and current.
func (r *Reader) averageWriteResponseTime(deviceName string, currWriteSvcTime, currWriteOpsCount int64) int64 {
	if currWriteSvcTime == metricsformatter.Unavailable || currWriteOpsCount == metricsformatter.Unavailable {
		return metricsformatter.Unavailable
	}
	prev, ok := r.prevDiskStats[deviceName]
	if !ok {
		return metricsformatter.Unavailable
	}
	return calculateAverage(currWriteSvcTime-prev.GetWriteSvcTimeMillis(), currWriteOpsCount-prev.GetWriteOpsCount())
}

func calculateAverage(svcTimeDelta, opsCountDelta int64) int64 {
	if svcTimeDelta == 0 || opsCountDelta == 0 {
		return 0
	}
	return svcTimeDelta / opsCountDelta
}

func deviceMappingExists(diskName string, disks []*iipb.Disk) bool {
	for _, disk := range disks {
		if disk.GetMapping() == diskName {
			return true
		}
	}
	return false
}

// parseWindowsDiskNumber extracts the disk number from the name of the device mapping.
//
// Eligible device mappings are of the form: "PhysicalDrive\d+".
func parseWindowsDiskNumber(deviceMapping string) (diskNumber int64, ok bool) {
	if !strings.HasPrefix(deviceMapping, "PhysicalDrive") {
		return 0, false
	}
	diskNumber, err := strconv.ParseInt(deviceMapping[13:], 10, 64)
	if err != nil {
		log.Logger.Debugw("Unexpected device mapping encountered", "mapping", deviceMapping, "error", err)
		return 0, false
	}
	return diskNumber, true
}
