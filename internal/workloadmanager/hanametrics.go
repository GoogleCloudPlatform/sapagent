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
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
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

/*
CollectHanaMetrics collects SAP HANA metrics for the Workload Manager and sends them to the wm
channel.
*/
func CollectHanaMetrics(params Parameters, wm chan<- WorkloadMetrics) {
	log.Logger.Info("Collecting workload hana metrics...")
	t := hanaAPILabel
	hanaVal := 0.0

	hanaProcessOrGlobalIni := hanaProcessOrGlobalINI(params.CommandRunner)
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

		log.Logger.Debugf("Could not find gobal.ini file %s", globalINILocationVal)
		wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
		return
	}

	l["fast_restart"] = "disabled"
	if grepKeyInGlobalINI("basepath_persistent_memory_volumes", globalINILocationVal, params.CommandRunner) {
		l["fast_restart"] = "enabled"
	}

	l["ha_sr_hook_configured"] = "no"
	if grepKeyInGlobalINI("ha_dr_provider_SAPHanaSR", globalINILocationVal, params.CommandRunner) {
		l["ha_sr_hook_configured"] = "yes"
	}

	numaCat, _, err := params.CommandRunner("cat", "/proc/sys/kernel/numa_balancing")
	if err != nil {
		log.Logger.Warn("cat /proc/sys/kernel/numa_balancing failed", log.Error(err))
	} else {
		l["numa_balancing"] = "disabled"
		if numaCat == "1" {
			l["numa_balancing"] = "enabled"
		}
	}

	thpCat, _, err := params.CommandRunner("cat", "/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil {
		log.Logger.Warn("cat /sys/kernel/mm/transparent_hugepage/enabled failed", log.Error(err))
	} else {
		l["transparent_hugepages"] = "disabled"
		if strings.Contains(thpCat, "[always]") {
			l["transparent_hugepages"] = "enabled"
		}
	}

	setVolumeLabels(l, diskInfo("basepath_datavolumes", globalINILocationVal, params.CommandRunner, params.InstanceInfoReader), "data")
	setVolumeLabels(l, diskInfo("basepath_logvolumes", globalINILocationVal, params.CommandRunner, params.InstanceInfoReader), "log")
	hanaVal = 1.0
	wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, hanaVal, params.Config)}
}

/*
hanaProcessOrGlobalINI obtains hana cluster data from a running hana process or a "global" INI file
if hana is not running on the current VM.
*/
func hanaProcessOrGlobalINI(runner commandlineexecutor.CommandRunner) string {
	hanaPid, _, _ := runner("pidof", "-s sapstartsrv")
	hanaProcessOrGlobalINI := ""
	if hanaPid != "" {
		hanaProcessOrGlobalINI, _, _ = runner("ps", "-p "+strings.TrimSpace(hanaPid)+" -o cmd --no-headers")
		if hanaProcessOrGlobalINI != "" && !strings.Contains(hanaProcessOrGlobalINI, "HDB") {
			// No HDB services in this process.
			hanaProcessOrGlobalINI = ""
		}
	}
	if hanaProcessOrGlobalINI == "" {
		// check for the global.ini even if the process isn't running
		hanaProcessOrGlobalINI, _, _ = runner("ls", "/usr/sap/*/SYS/global/hdb/custom/config/global.ini")
	}
	return hanaProcessOrGlobalINI
}

/*
setVolumeLabels sets volume labels for the workload metric collector.  These volumes may either be
data disks or logs.
*/
func setVolumeLabels(l map[string]string, diskInfo map[string]string, dataOrLog string) {
	log.Logger.Debugf("diskInfo for %s is empty: %t", dataOrLog, len(diskInfo) == 0)
	if len(diskInfo) > 0 {
		log.Logger.Debugf("Found basepath_%svolumes, adding disk_data labels", dataOrLog)
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
	log.Logger.Debugf("HANA commandOrFileLocation %s", commandOrFileLocation)
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
	log.Logger.Debugf("HANA sid: %s and global.ini file: %s", sid, globalINILocation)
	return globalINILocation
}

/*
grepKeyInGlobalINI determines whether or not a given INI file contains a specific key definition
*/
func grepKeyInGlobalINI(key string, globalIniLocation string, runner commandlineexecutor.CommandRunner) bool {
	grep, _, err := runner("grep", key+" "+globalIniLocation)
	if err != nil {
		return false
	}
	return len(grep) > 0
}

func diskInfo(basepathVolume string, globalINILocation string, runner commandlineexecutor.CommandRunner, iir instanceinfo.Reader) map[string]string {
	diskInfo := map[string]string{}

	volumeGrep, _, err := runner("grep", basepathVolume+" "+globalINILocation)
	// volumeGrep will be of the format /hana/data/HAS (or something similar).
	// In this case the mount point will be /hana/data.
	// A deeper path like /hana/data/ABC/mnt00001 may also be used.
	if err != nil {
		return diskInfo
	}
	vList := strings.Fields(volumeGrep)
	if len(vList) < 3 {
		log.Logger.Debugf("Could not find basepathVolume: %s in global.ini", basepathVolume)
		return diskInfo
	}
	basepathVolumePath := vList[2]
	log.Logger.Debugf("basepathVolumePath: %s", basepathVolumePath)

	// JSON output from lsblk to match the lsblk.proto is produced by the following command:
	// lsblk -p -J -o name,type,mountpoint
	lsblkJSON, _, _ := runner("lsblk", "-b -p -J -o name,type,mountpoint,size")

	lsblk := lsblk{}
	err = json.Unmarshal([]byte(lsblkJSON), &lsblk)

	if err != nil {
		log.Logger.Debug("Invalid lsblk json", log.Error(err))
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
				log.Logger.Debugf("Found matchedBlockDevice: %s, matchedMountPoint: %s, matchedSize: %s", matchedBlockDevice.Name, matchedMountPoint, matchedSize)
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
	log.Logger.Debugf("Checking disk mappings against instance disks, disk array length: %d", len(iir.InstanceProperties().GetDisks()))
	for _, disk := range iir.InstanceProperties().GetDisks() {
		log.Logger.Debugf("Checking disk mapping: %s against matchedBlockDevice: %s", disk.GetMapping(), matchedBlockDevice.Name)
		if strings.HasSuffix(matchedBlockDevice.Name, disk.GetMapping()) {
			matchedBlockDeviceSize := extractSize(matchedBlockDevice.Size)
			log.Logger.Debugf("Found matched disk mapping for mount point: %s, diskType: %s", matchedMountPoint, strings.ToLower(disk.GetDeviceType()))
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
		log.Logger.Errorf("Could not parse size value: %v - %#v", msg, err)
	}

	return size
}
