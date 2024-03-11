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
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const sapValidationHANA = "workload.googleapis.com/sap/validation/hana"

var instanceURIRegex = regexp.MustCompile("/projects/(.+)/zones/(.+)/instances/(.+)")

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

// CollectHANAMetricsFromConfig collects the HANA metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectHANAMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager HANA metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	l := map[string]string{}
	hanaVal := 0.0

	globalINILocationVal := globalINIfromSAPsid(ctx, params)
	if globalINILocationVal == "" {
		log.CtxLogger(ctx).Debug("Skipping HANA metrics collection, HANA not active on instance")
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}
	if _, err := params.OSStatReader(globalINILocationVal); err != nil {
		// Parse out the SID and global.ini location.
		// example process output:
		// /usr/sap/RKT/HDB90/exe/sapstartsrv
		// pf=/usr/sap/RKT/SYS/profile/RKT_HDB90_sap-hana-vm-hma-rev53-rhel -D -u rktadm

		// The global.ini will be in /usr/sap/[SID]/SYS/global/hdb/custom/config/global.ini

		// If the process is not running then the hanaProcessOrGlobalIni will contain the global.ini
		// location similar to: /usr/sap/HAR/SYS/global/hdb/custom/config/global.ini

		log.CtxLogger(ctx).Debugw("Could not find global.ini file", "globalinilocation", globalINILocationVal)
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}

	hana := params.WorkloadConfig.GetValidationHana()
	for k, v := range configurablemetrics.CollectMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), globalINILocationVal, hana.GetGlobalIniMetrics()) {
		l[k] = v
	}
	for _, m := range hana.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}
	for _, volume := range hana.GetHanaDiskVolumeMetrics() {
		diskInfo := diskInfo(ctx, volume.GetBasepathVolume(), globalINILocationVal, params.Execute, params.InstanceInfoReader)
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
	for _, m := range hana.GetHaMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANAHighAvailabilityVariable_HA_IN_SAME_ZONE:
			l[k] = fmt.Sprint(checkHAZones(ctx, params))
		}
	}

	hanaVal = 1.0
	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
}

// globalINIfromSAPsid returns the path to the global.ini file using the
// SAP sid from the discovered HANA instance.
func globalINIfromSAPsid(ctx context.Context, params Parameters) string {
	if params.Discovery == nil {
		log.CtxLogger(ctx).Warn("Discovery has not been initialized, cannot check SAP instances")
		return ""
	}
	sapInstances := params.Discovery.GetSAPInstances().GetInstances()
	if len(sapInstances) == 0 {
		log.CtxLogger(ctx).Debug("No SAP instances found")
		return ""
	}
	for _, instance := range sapInstances {
		if instance.GetType() == sapb.InstanceType_HANA && instance.GetSapsid() != "" {
			log.CtxLogger(ctx).Debugw("Found HANA instance", "sapsid", instance.GetSapsid())
			return fmt.Sprintf("/usr/sap/%s/SYS/global/hdb/custom/config/global.ini", instance.GetSapsid())
		}
	}
	return ""
}

func diskInfo(ctx context.Context, basepathVolume string, globalINILocation string, exec commandlineexecutor.Execute, iir instanceinfo.Reader) map[string]string {
	diskInfo := map[string]string{}

	result := exec(ctx, commandlineexecutor.Params{
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
		log.CtxLogger(ctx).Debugw("Could not find basepath volume in global.ini", "basepathvolume", basepathVolume, "globalinilocation", globalINILocation)
		return diskInfo
	}
	basepathVolumePath := vList[2]
	log.CtxLogger(ctx).Debugw("Found basepathVolumePath in global.ini", "basepathvolumepath", basepathVolumePath)

	// Get the exact mount location for the volume basepath
	// Expected output:
	// Mounted on
	// /hana/data
	result = exec(ctx, commandlineexecutor.Params{
		Executable:  "df",
		ArgsToSplit: fmt.Sprintf("--output=target %s", basepathVolumePath),
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find volume mountpoint", "basepathvolumepath", basepathVolumePath, "error", result.Error)
		return diskInfo
	}
	lines := strings.Split(strings.TrimSpace(result.StdOut), "\n")
	if len(lines) != 2 {
		log.CtxLogger(ctx).Debugw("Could not find volume mountpoint", "basepathvolumepath", basepathVolumePath, "output", result.StdOut)
		return diskInfo
	}
	volumeMountpoint := strings.TrimSpace(lines[1])
	log.CtxLogger(ctx).Debugw("Found volume mountpoint", "mountpoint", volumeMountpoint, "basepathvolumepath", basepathVolumePath)

	// JSON output from lsblk to match the lsblk.proto is produced by the following command:
	// lsblk -p -J -o name,type,mountpoint
	lsblkresult := exec(ctx, commandlineexecutor.Params{
		Executable:  "lsblk",
		ArgsToSplit: "-b -p -J -o name,type,mountpoint,size",
	})

	lsblk := lsblk{}
	err := json.Unmarshal([]byte(lsblkresult.StdOut), &lsblk)

	if err != nil {
		log.CtxLogger(ctx).Debugw("Invalid lsblk json", "error", err)
		return diskInfo
	}

	matchedMountPoint := ""
	matchedBlockDevice := lsblkdevice{}
	matchedSize := ""
BlockDeviceLoop:
	for _, blockDevice := range lsblk.BlockDevices {
		children := blockDevice.Children
		// Accommodate direct device mapping where devices do not resolve to
		// /dev/mapper child configurations.
		if blockDevice.Children == nil {
			children = []lsblkdevicechild{{
				Name:       blockDevice.Name,
				Type:       blockDevice.Type,
				Mountpoint: blockDevice.Mountpoint,
				Size:       blockDevice.Size,
			}}
		}
		for _, child := range children {
			if child.Mountpoint == volumeMountpoint {
				matchedBlockDevice = blockDevice
				matchedMountPoint = child.Mountpoint
				childSize := extractSize(ctx, child.Size)
				matchedSize = strconv.FormatInt(childSize, 10)
				break BlockDeviceLoop
			}
		}
	}

	if len(matchedMountPoint) > 0 {
		log.CtxLogger(ctx).Debugw("Found matched block device", "matchedblockdevice", matchedBlockDevice.Name, "matchedmountpoint", matchedMountPoint, "matchedsize", matchedSize)
		setDiskInfoForDevice(ctx, diskInfo, &matchedBlockDevice, matchedMountPoint, matchedSize, iir)
	}

	return diskInfo
}

// setDiskInfoForDevice sets the diskInfo map with the disk information
// for the matched block device.
func setDiskInfoForDevice(
	ctx context.Context,
	diskInfo map[string]string,
	matchedBlockDevice *lsblkdevice,
	matchedMountPoint string,
	matchedSize string,
	iir instanceinfo.Reader,
) {
	log.CtxLogger(ctx).Debugw("Checking disk mappings against instance disks", "numberofdisks", len(iir.InstanceProperties().GetDisks()))
	for _, disk := range iir.InstanceProperties().GetDisks() {
		log.CtxLogger(ctx).Debugw("Checking disk mapping", "mapping", disk.GetMapping(), "matchedblockdevice", matchedBlockDevice.Name)
		if strings.HasSuffix(matchedBlockDevice.Name, disk.GetMapping()) {
			matchedBlockDeviceSize := extractSize(ctx, matchedBlockDevice.Size)
			log.CtxLogger(ctx).Debugw("Found matched disk mapping", "mountpoint", matchedMountPoint, "disktype", strings.ToLower(disk.GetDeviceType()))
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
func extractSize(ctx context.Context,msg []byte) int64 {
	val := strings.Trim(string(msg), `"`)
	size, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Could not parse size value", "sizefield", msg, "error", err)
	}

	return size
}

// checkHAZones determines if the host instance is part of a HA setup which
// shares the same zone as other instance(s) in the same HA grouping.
func checkHAZones(ctx context.Context, params Parameters) string {
	haNodesSameZone := ""
	for _, system := range params.Discovery.GetSAPSystems() {
		var instancesInSameZone []string
		hasHostInstance := false
		log.CtxLogger(ctx).Debugw("SAP System has the following HA hosts", "haHosts", system.GetDatabaseLayer().GetHaHosts())
		for _, hostURI := range system.GetDatabaseLayer().GetHaHosts() {
			uriMatch := instanceURIRegex.FindStringSubmatch(hostURI)
			if len(uriMatch) < 4 {
				continue
			}
			if uriMatch[3] == params.Config.GetCloudProperties().GetInstanceName() {
				hasHostInstance = true
				continue
			}
			if uriMatch[2] == params.Config.GetCloudProperties().GetZone() {
				if !slices.Contains(instancesInSameZone, uriMatch[3]) {
					instancesInSameZone = append(instancesInSameZone, uriMatch[3])
				}
			}
		}
		if hasHostInstance && len(instancesInSameZone) > 0 {
			haNodesSameZone = strings.Join(instancesInSameZone, ",")
			break
		}
	}
	return haNodesSameZone
}
