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

package workloadmanager

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

type lsblkdevicechild struct {
	Name       string
	Type       string
	Mountpoint string `json:"mountpoint"`
	Size       json.RawMessage
}

type lsblkdevice struct {
	Name       string
	Type       string
	Mountpoint string `json:"mountpoint"`
	Size       json.RawMessage
	Children   []lsblkdevicechild
}

type lsblk struct {
	BlockDevices []lsblkdevice `json:"blockdevices"`
}

const hanaAPILabel = "workload.googleapis.com/sap/validation/hana"

// CollectHANAMetricsFromConfig collects the HANA metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectHANAMetricsFromConfig(params Parameters) WorkloadMetrics {
	log.Logger.Info("Collecting Workload Manager HANA metrics...")
	t := hanaAPILabel
	hanaVal := 0.0

	hanaProcessOrGlobalIni := hanaProcessOrGlobalINI(params.Execute)
	globalINILocationVal := globalINILocation(hanaProcessOrGlobalIni)

	l := map[string]string{}
	if hanaProcessOrGlobalIni == "" || strings.Contains(hanaProcessOrGlobalIni, "cannot access") {
		// No HANA INI file or processes were identified on the current host.
		log.Logger.Debug("HANA process and global.ini not found, no HANA")
		return WorkloadMetrics{Metrics: createTimeSeries(hanaAPILabel, l, hanaVal, params.Config)}
	}
	if _, err := params.OSStatReader(globalINILocationVal); err != nil {
		// Parse out the SID and global.ini location.
		// example process output:
		// /usr/sap/RKT/HDB90/exe/sapstartsrv
		// pf=/usr/sap/RKT/SYS/profile/RKT_HDB90_sap-hana-vm-hma-rev53-rhel -D -u rktadm

		// The global.ini will be in /usr/sap/[SID]/SYS/global/hdb/custom/config/global.ini

		// If the process is not running then the hanaProcessOrGlobalIni will contain the global.ini
		// location similar to: /usr/sap/HAR/SYS/global/hdb/custom/config/global.ini

		log.Logger.Debugw("Could not find gobal.ini file", "globalinilocation", globalINILocationVal)
		return WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
	}

	hana := params.WorkloadConfig.GetValidationHana()
	for k, v := range configurablemetrics.CollectMetricsFromFile(configurablemetrics.FileReader(params.ConfigFileReader), globalINILocationVal, hana.GetGlobalIniMetrics()) {
		l[k] = v
	}
	for _, m := range hana.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}
	for _, volume := range hana.GetHanaDiskVolumeMetrics() {
		diskInfo := diskInfo(volume.GetBasepathVolume(), globalINILocationVal, params.Execute, params.InstanceInfoReader)
		for _, m := range volume.GetMetrics() {
			k := m.GetMetricInfo().GetLabel()
			switch m.GetValue() {
			case wpb.DiskVariable_TYPE:
				l[k] = diskInfo["instancedisktype"]
			case wpb.DiskVariable_MOUNT:
				l[k] = diskInfo["mountpoint"]
			case wpb.DiskVariable_SIZE:
				l[k] = diskInfo["size"]
			case wpb.DiskVariable_PD_SIZE:
				l[k] = diskInfo["pdsize"]
			}
		}
	}

	hanaVal = 1.0
	return WorkloadMetrics{Metrics: createTimeSeries(hanaAPILabel, l, hanaVal, params.Config)}
}

/*
CollectHanaMetrics collects SAP HANA metrics for the Workload Manager and sends them to the wm
channel.
*/
func CollectHanaMetrics(params Parameters, wm chan<- WorkloadMetrics) {
	log.Logger.Info("Collecting workload hana metrics...")
	t := hanaAPILabel
	hanaVal := 0.0

	hanaProcessOrGlobalIni := hanaProcessOrGlobalINI(params.Execute)
	globalINILocationVal := globalINILocation(hanaProcessOrGlobalIni)

	l := map[string]string{}
	if hanaProcessOrGlobalIni == "" || strings.Contains(hanaProcessOrGlobalIni, "cannot access") {
		// No HANA INI file or processes were identified on the current host.
		log.Logger.Debug("HANA process and global.ini not found, no HANA")
		wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
		return
	}
	if _, err := params.OSStatReader(globalINILocationVal); err != nil {
		// Parse out the SID and global.ini location.
		// example process output:
		// /usr/sap/RKT/HDB90/exe/sapstartsrv
		// pf=/usr/sap/RKT/SYS/profile/RKT_HDB90_sap-hana-vm-hma-rev53-rhel -D -u rktadm

		// The global.ini will be in /usr/sap/[SID]/SYS/global/hdb/custom/config/global.ini

		// If the process is not running then the hanaProcessOrGlobalIni will contain the global.ini
		// location similar to: /usr/sap/HAR/SYS/global/hdb/custom/config/global.ini

		log.Logger.Debugw("Could not find gobal.ini file", "globalinilocation", globalINILocationVal)
		wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
		return
	}

	l["fast_restart"] = "disabled"
	if grepKeyInGlobalINI("basepath_persistent_memory_volumes", globalINILocationVal, params.Execute) {
		l["fast_restart"] = "enabled"
	}

	l["ha_sr_hook_configured"] = "no"
	if grepKeyInGlobalINI("ha_dr_provider_SAPHanaSR", globalINILocationVal, params.Execute) {
		l["ha_sr_hook_configured"] = "yes"
	}

	nbresult := params.Execute(commandlineexecutor.Params{
		Executable: "cat",
		Args:       []string{"/proc/sys/kernel/numa_balancing"},
	})
	if nbresult.Error != nil {
		log.Logger.Warnw("cat /proc/sys/kernel/numa_balancing failed", "error", nbresult.Error)
	} else {
		l["numa_balancing"] = "disabled"
		if nbresult.StdOut == "1" {
			l["numa_balancing"] = "enabled"
		}
	}

	hpresult := params.Execute(commandlineexecutor.Params{
		Executable: "cat",
		Args:       []string{"/sys/kernel/mm/transparent_hugepage/enabled"},
	})
	if hpresult.Error != nil {
		log.Logger.Warnw("cat /sys/kernel/mm/transparent_hugepage/enabled failed", "error", hpresult.Error)
	} else {
		l["transparent_hugepages"] = "disabled"
		if strings.Contains(hpresult.StdOut, "[always]") {
			l["transparent_hugepages"] = "enabled"
		}
	}

	setVolumeLabels(l, diskInfo("basepath_datavolumes", globalINILocationVal, params.Execute, params.InstanceInfoReader), "data")
	setVolumeLabels(l, diskInfo("basepath_logvolumes", globalINILocationVal, params.Execute, params.InstanceInfoReader), "log")
	hanaVal = 1.0
	wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
}

/*
hanaProcessOrGlobalINI obtains hana cluster data from a running hana process or a "global" INI file
if hana is not running on the current VM.
*/
func hanaProcessOrGlobalINI(exec commandlineexecutor.Execute) string {
	presult := exec(commandlineexecutor.Params{
		Executable:  "pidof",
		ArgsToSplit: "-s sapstartsrv",
	})
	hanaProcessOrGlobalINI := ""
	if presult.StdOut != "" {
		hpresult := exec(commandlineexecutor.Params{
			Executable:  "ps",
			ArgsToSplit: fmt.Sprintf("-p %s -o cmd --no-headers", strings.TrimSpace(presult.StdOut)),
		})
		hanaProcessOrGlobalINI = hpresult.StdOut
		if hanaProcessOrGlobalINI != "" && !strings.Contains(hanaProcessOrGlobalINI, "HDB") {
			// No HDB services in this process.
			hanaProcessOrGlobalINI = ""
		}
	}
	if hanaProcessOrGlobalINI == "" {
		// Check for the global.ini even if the process isn't running.
		// Invoke the shell in order to expand the `*` wildcard.
		hpresult := exec(commandlineexecutor.Params{
			Executable:  "/bin/sh",
			ArgsToSplit: "-c 'ls /usr/sap/*/SYS/global/hdb/custom/config/global.ini'",
		})
		hanaProcessOrGlobalINI = hpresult.StdOut
	}
	return hanaProcessOrGlobalINI
}

/*
setVolumeLabels sets volume labels for the workload metric collector.  These volumes may either be
data disks or logs.
*/
func setVolumeLabels(l map[string]string, diskInfo map[string]string, dataOrLog string) {
	log.Logger.Debugw("diskInfo empty check", "dataorlog", dataOrLog, "isempty", len(diskInfo) == 0)
	if len(diskInfo) > 0 {
		log.Logger.Debugw("Found basepath volumes, adding disk_data labels", "basepathvolumes", fmt.Sprintf("basepath_%svolumes", dataOrLog))
		l["disk_"+dataOrLog+"_type"] = diskInfo["instancedisktype"]
		l["disk_"+dataOrLog+"_mount"] = diskInfo["mountpoint"]
		l["disk_"+dataOrLog+"_size"] = diskInfo["size"]
		l["disk_"+dataOrLog+"_pd_size"] = diskInfo["pdsize"]
	}
}

/*
globalINILocation builds an INI path location from an input string consisting of a either a
HANA command or a config file location
*/
func globalINILocation(hanaProcessOrGlobalINI string) string {
	// There is no Hana implementation that we can derive.
	if hanaProcessOrGlobalINI == "" {
		return ""
	}
	pathSplit := strings.Fields(hanaProcessOrGlobalINI)
	sid := ""
	globalINILocation := ""
	commandOrFileLocation := pathSplit[0]
	pathParts := strings.Split(commandOrFileLocation, "/")
	log.Logger.Debugw("HANA commandOrFileLocation", "commandorfilelocation", commandOrFileLocation)
	if len(pathSplit) == 1 {
		// This is just the global.ini path already.
		globalINILocation = strings.TrimSpace(hanaProcessOrGlobalINI)
		// NOMUTANTS--we are only logging the SID so we cannot add a test for it
		for _, pathPart := range pathParts {
			if strings.HasPrefix(pathPart, "SYS") {
				break
			}
			sid = pathPart
		}
	} else {
		for _, pathPart := range pathParts {
			if strings.HasPrefix(pathPart, "HDB") {
				break
			}
			sid = pathPart
			globalINILocation += pathPart + "/"
		}
		globalINILocation += "SYS/global/hdb/custom/config/global.ini"
	}
	log.Logger.Debugw("HANA sid and global.ini file", "sid", sid, "globalinilocation", globalINILocation)
	return globalINILocation
}

/*
grepKeyInGlobalINI determines whether or not a given INI file contains a specific key definition
*/
func grepKeyInGlobalINI(key string, globalIniLocation string, exec commandlineexecutor.Execute) bool {
	result := exec(commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: key + " " + globalIniLocation,
	})
	if result.Error != nil {
		return false
	}
	return len(result.StdOut) > 0
}

func diskInfo(basepathVolume string, globalINILocation string, exec commandlineexecutor.Execute, iir instanceinfo.Reader) map[string]string {
	diskInfo := map[string]string{}

	result := exec(commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: basepathVolume + " " + globalINILocation,
	})
	// volumeGrep will be of the format /hana/data/HAS (or something similar).
	// In this case the mount point will be /hana/data.
	// A deeper path like /hana/data/ABC/mnt00001 may also be used.
	if result.Error != nil {
		return diskInfo
	}
	vList := strings.Fields(result.StdOut)
	if len(vList) < 3 {
		log.Logger.Debugw("Could not find basepath volume in global.ini", "basepathvolume", basepathVolume, "globalinilocation", globalINILocation)
		return diskInfo
	}
	basepathVolumePath := vList[2]
	log.Logger.Debugw("basepathVolumePath from string field", "basepathvolumepath", basepathVolumePath)

	// JSON output from lsblk to match the lsblk.proto is produced by the following command:
	// lsblk -p -J -o name,type,mountpoint
	lsblkresult := exec(commandlineexecutor.Params{
		Executable:  "lsblk",
		ArgsToSplit: "-b -p -J -o name,type,mountpoint,size",
	})

	lsblk := lsblk{}
	err := json.Unmarshal([]byte(lsblkresult.StdOut), &lsblk)

	if err != nil {
		log.Logger.Debugw("Invalid lsblk json", "error", err)
		return diskInfo
	}

	matchedMountPoint := ""
	matchedBlockDevice := lsblkdevice{}
	matchedSize := ""
	for _, blockDevice := range lsblk.BlockDevices {
		if blockDevice.Children == nil {
			continue
		}
		for _, child := range blockDevice.Children {
			if strings.HasPrefix(basepathVolumePath, child.Mountpoint) && len(child.Mountpoint) > len(matchedMountPoint) {
				matchedBlockDevice = blockDevice
				matchedMountPoint = child.Mountpoint
				childSize := extractSize(child.Size)
				matchedSize = strconv.FormatInt(childSize, 10)
				log.Logger.Debugw("Found matched block device", "matchedblockdevice", matchedBlockDevice.Name, "matchedmountpoint", matchedMountPoint, "matchedsize", matchedSize)
				break
			}
		}
	}
	if len(matchedMountPoint) > 0 {
		setDiskInfoForDevice(diskInfo, &matchedBlockDevice, matchedMountPoint, matchedSize, iir)
	}

	return diskInfo
}

func setDiskInfoForDevice(
	diskInfo map[string]string,
	matchedBlockDevice *lsblkdevice,
	matchedMountPoint string,
	matchedSize string,
	iir instanceinfo.Reader,
) {
	log.Logger.Debugw("Checking disk mappings against instance disks", "numberofdisks", len(iir.InstanceProperties().GetDisks()))
	for _, disk := range iir.InstanceProperties().GetDisks() {
		log.Logger.Debugw("Checking disk mapping", "mapping", disk.GetMapping(), "matchedblockdevice", matchedBlockDevice.Name)
		if strings.HasSuffix(matchedBlockDevice.Name, disk.GetMapping()) {
			matchedBlockDeviceSize := extractSize(matchedBlockDevice.Size)
			log.Logger.Debugw("Found matched disk mapping", "mountpoint", matchedMountPoint, "disktype", strings.ToLower(disk.GetDeviceType()))
			diskInfo["mountpoint"] = matchedMountPoint
			diskInfo["instancedisktype"] = strings.ToLower(disk.GetDeviceType())
			diskInfo["size"] = matchedSize
			diskInfo["pdsize"] = strconv.FormatInt(matchedBlockDeviceSize, 10)
			break
		}
	}
}

/*
extractSize converts any "size" field of "lsblk" JSON formatted disk inventory to an int64 value

the input value must be a string encoded 64 bit integer which may or may not be enclosed with quotes

"\"42\"" -> 42
"42" -> 42
*/
func extractSize(msg []byte) int64 {
	val := strings.Trim(string(msg), `"`)
	size, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		log.Logger.Errorw("Could not parse size value", "sizefield", msg, "error", err)
	}

	return size
}
