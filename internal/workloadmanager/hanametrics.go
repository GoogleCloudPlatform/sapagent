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
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/configurablemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	systempb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

const (
	sapValidationHANA = "workload.googleapis.com/sap/validation/hana"
	timestampLayout   = "2006-01-02T15:04:05-07:00"
	instancesURIPart  = "instances"
)

var instanceURIRegex = regexp.MustCompile("/projects/(.+)/zones/(.+)/instances/(.+)")
var successfulBackupRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[\+\-]\d{2}:\d{2})\s+\S+\s+(\S+)\s+INFO\s+BACKUP\s+(SNAPSHOT|SAVE DATA)\s+finished\s+successfully`)
var backupCommandRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[\+\-]\d{2}:\d{2}\s+\S+\s+(\S+)\s+INFO\s+BACKUP\s+command: (.*)`)

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

type hanaBackupLog struct {
	finishTime time.Time
	backupID   string
	backupType string
}

type hanaBackupType string

const (
	hanaBackupDelta    hanaBackupType = "DELTA"
	hanaBackupFull     hanaBackupType = "FULL"
	hanaBackupSnapshot hanaBackupType = "SNAPSHOT"
)

// CollectHANAMetricsFromConfig collects the HANA metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectHANAMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager HANA metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	l := map[string]string{}
	hanaVal := 0.0

	instance := sapHANAInstance(ctx, params)
	if instance == nil {
		log.CtxLogger(ctx).Debug("Skipping HANA metrics collection, HANA not active on instance")
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}

	sidAdm := instance.GetUser()
	hanaSystemConfigDir := fmt.Sprintf("/usr/sap/%s/SYS/global/hdb/custom/config", instance.GetSapsid())
	globalINIFilePath := hanaSystemConfigDir + "/global.ini"
	// Short-circuit HANA metrics collection if global.ini file is not found.
	// In addition to the metrics contained in the file, global.ini also contains
	// basepath information for the HANA data and log volumes.
	if _, err := params.OSStatReader(globalINIFilePath); err != nil {
		log.CtxLogger(ctx).Debugw("Skipping HANA metrics collection, could not find global.ini file", "location", globalINIFilePath)
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}

	// Per b/484848209, SAP HANA configuration files are case-insensitive for
	// section headers and field labels, but not field values.
	// As a follow-up, b/488401875 to explore metrics affected by this change.
	ignoreCase := true
	hana := params.WorkloadConfig.GetValidationHana()
	for k, v := range configurablemetrics.CollectEvalMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), globalINIFilePath, hana.GetGlobalIniMetrics(), ignoreCase) {
		l[k] = v
	}
	indexserverINIFilePath := hanaSystemConfigDir + "/indexserver.ini"
	for k, v := range configurablemetrics.CollectEvalMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), indexserverINIFilePath, hana.GetIndexserverIniMetrics(), ignoreCase) {
		l[k] = v
	}
	for _, m := range hana.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}
	for _, volume := range hana.GetHanaDiskVolumeMetrics() {
		diskInfo := diskInfo(ctx, volume, globalINIFilePath, sidAdm, params)
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
			case wpb.DiskVariable_BLOCK_DEVICE:
				l[k] = diskInfo["blockdevice"]
			}
		}
	}
	for _, m := range hana.GetHaMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANAHighAvailabilityVariable_HA_IN_SAME_ZONE:
			l[k] = fmt.Sprint(checkHAZones(ctx, params))
		case wpb.HANAHighAvailabilityVariable_HA_INSTANCE_SITE:
			l[k] = instance.GetSite().String()
		}
	}
	for _, m := range hana.GetDrMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANADisasterRecoveryVariable_DR_IN_SAME_REGION:
			l[k] = fmt.Sprint(checkDRRegions(ctx, sidAdm, params))
		}
	}
	hanaBackupMetrics := hanaBackupMetrics(ctx, instance, params.Discovery, params.Execute)
	for _, m := range hana.GetHanaBackupMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANABackupVariable_TENANT_NAME:
			l[k] = hanaBackupMetrics["oldest_backup_tenant_name"]
		case wpb.HANABackupVariable_LAST_BACKUP_TIMESTAMP:
			l[k] = hanaBackupMetrics["oldest_last_backup_timestamp_utc"]
		case wpb.HANABackupVariable_DELTA_TENANT_NAME:
			l[k] = hanaBackupMetrics["oldest_delta_backup_tenant_name"]
		case wpb.HANABackupVariable_LAST_DELTA_BACKUP_TIMESTAMP:
			l[k] = hanaBackupMetrics["oldest_last_delta_backup_timestamp_utc"]
		case wpb.HANABackupVariable_SNAPSHOT_TENANT_NAME:
			l[k] = hanaBackupMetrics["oldest_snapshot_backup_tenant_name"]
		case wpb.HANABackupVariable_LAST_SNAPSHOT_BACKUP_TIMESTAMP:
			l[k] = hanaBackupMetrics["oldest_last_snapshot_backup_timestamp_utc"]
		}
	}
	for _, m := range hana.GetTraceMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANATraceVariable_CHKSRV_HOOK_ACTIVE:
			l[k] = chksrvHookActive(ctx, instance, params.Execute, params.osVendorID)
		}
	}

	hanaVal = 1.0
	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
}

func sapHANAInstance(ctx context.Context, params Parameters) *sapb.SAPInstance {
	if params.Discovery == nil {
		log.CtxLogger(ctx).Warn("Discovery has not been initialized, cannot check SAP instances")
		return nil
	}
	sapInstances := params.Discovery.GetSAPInstances().GetInstances()
	if len(sapInstances) == 0 {
		log.CtxLogger(ctx).Debug("No SAP instances found")
		return nil
	}
	for _, instance := range sapInstances {
		if instance.GetType() == sapb.InstanceType_HANA && instance.GetSapsid() != "" {
			log.CtxLogger(ctx).Debugw("Found HANA instance", "sapsid", instance.GetSapsid())
			return instance
		}
	}
	return nil
}

func diskInfo(ctx context.Context, volume *wpb.HANADiskVolumeMetric, globalINILocation, sidAdminUser string, params Parameters) map[string]string {
	diskInfo := map[string]string{}
	volumeMountpoint := ""

	switch volume.GetMetricSource() {
	case wpb.HANADiskVolumeMetricSource_GLOBAL_INI:
		volumeMountpoint = fetchVolumeMountPointFromGlobalINI(ctx, volume, globalINILocation, params.Execute, params.InstanceInfoReader)
	case wpb.HANADiskVolumeMetricSource_HDB_INDEXSERVER_PATH:
		volumeMountpoint = fetchVolumeMountPointFromHDBIndexserverPath(ctx, params.Execute, sidAdminUser)
	case wpb.HANADiskVolumeMetricSource_DIR_INSTANCE_ENV:
		volumeMountpoint = fetchVolumeMountPointFromDirInstanceEnv(ctx, params.Execute, sidAdminUser)
	default:
		log.CtxLogger(ctx).Debugw("Unknown disk volume metric source")
		return diskInfo
	}

	if volumeMountpoint == "" {
		log.CtxLogger(ctx).Debugw("Could not find volume mountpoint")
		return diskInfo
	}

	// JSON output from lsblk to match the lsblk.proto is produced by the following command:
	// lsblk -p -J -o name,type,mountpoint
	lsblkresult := params.Execute(ctx, commandlineexecutor.Params{
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
		setDiskInfoForDevice(ctx, diskInfo, &matchedBlockDevice, matchedMountPoint, matchedSize, params.InstanceInfoReader)
	}

	return diskInfo
}

func fetchVolumeMountPointFromGlobalINI(ctx context.Context, volume *wpb.HANADiskVolumeMetric, globalINILocation string, exec commandlineexecutor.Execute, iir instanceinfo.Reader) string {
	basepathVolume := volume.GetBasepathVolume()
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: basepathVolume + " " + globalINILocation,
	})
	// volumeGrep will be of the format /hana/data/HAS (or something similar).
	// In this case the mount point will be /hana/data.
	// A deeper path like /hana/data/ABC/mnt00001 may also be used.
	if result.Error != nil {
		return ""
	}
	vList := strings.Fields(result.StdOut)
	if len(vList) < 3 {
		log.CtxLogger(ctx).Debugw("Could not find basepath volume in global.ini", "basepathvolume", basepathVolume, "globalinilocation", globalINILocation)
		return ""
	}
	basepathVolumePath := vList[2]
	log.CtxLogger(ctx).Debugw("Found basepathVolumePath in global.ini", "basepathvolumepath", basepathVolumePath)

	volumeMountpoint := volumeMountPoint(ctx, basepathVolumePath, exec)
	log.CtxLogger(ctx).Debugw("Found volume mountpoint", "mountpoint", volumeMountpoint, "basepathvolumepath", basepathVolumePath)
	return volumeMountpoint
}

func fetchVolumeMountPointFromHDBIndexserverPath(ctx context.Context, exec commandlineexecutor.Execute, sidAdminUser string) string {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: fmt.Sprintf("-i -u %s whereis hdbindexserver", sidAdminUser),
	})
	if result.Error != nil {
		return ""
	}
	vList := strings.Fields(result.StdOut)
	if len(vList) < 2 {
		log.CtxLogger(ctx).Debugw("Could not find location of hdbindexserver", "Stdout", vList)
		return ""
	}
	basepathVolumePath := vList[1]
	log.CtxLogger(ctx).Debugw("Found basepathVolumePath for hdbindexserver", "basepathvolumepath", basepathVolumePath)

	return volumeMountPoint(ctx, basepathVolumePath, exec)
}

func fetchVolumeMountPointFromDirInstanceEnv(ctx context.Context, exec commandlineexecutor.Execute, sidAdminUser string) string {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: fmt.Sprintf("-i -u %s /bin/sh -c 'echo $DIR_INSTANCE'", sidAdminUser),
	})
	if result.Error != nil {
		return ""
	}
	vList := strings.Fields(result.StdOut)
	if len(vList) < 1 {
		log.CtxLogger(ctx).Debugw("Could not find value of env variable DIR_INSTANCE", "Stdout", vList)
		return ""
	}
	basepathVolumePath := vList[0]
	log.CtxLogger(ctx).Debugw("Found basepathVolumePath from env variable DIR_INSTANCE", "basepathvolumepath", basepathVolumePath)

	return volumeMountPoint(ctx, basepathVolumePath, exec)
}

// Get the exact mount location for the volume basepath
// Expected output:
// Mounted on
// /hana/data
func volumeMountPoint(ctx context.Context, basepathVolumePath string, exec commandlineexecutor.Execute) string {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "df",
		ArgsToSplit: fmt.Sprintf("--output=target %s", basepathVolumePath),
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find volume mountpoint", "basepathvolumepath", basepathVolumePath, "error", result.Error)
		return ""
	}
	lines := strings.Split(strings.TrimSpace(result.StdOut), "\n")
	if len(lines) != 2 {
		log.CtxLogger(ctx).Debugw("Could not find volume mountpoint", "basepathvolumepath", basepathVolumePath, "output", result.StdOut)
		return ""
	}
	volumeMountpoint := strings.TrimSpace(lines[1])
	return volumeMountpoint
}

// setDiskInfoForDevice sets the diskInfo map with the disk information
// for the matched block device.
func setDiskInfoForDevice(ctx context.Context, diskInfo map[string]string, matchedBlockDevice *lsblkdevice, matchedMountPoint string, matchedSize string, iir instanceinfo.Reader) {
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
			diskInfo["blockdevice"] = matchedBlockDevice.Name
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
func extractSize(ctx context.Context, msg []byte) int64 {
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

// checkDRRegions determines if the host instance is serving as the primary
// for system replication, and if so, checks if the replication site for DR
// is in the same region as the primary.
func checkDRRegions(ctx context.Context, sidAdminUser string, params Parameters) string {
	var drSitesSameRegion []string

	// Check if the host instance is serving as the primary.
	result := params.Execute(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: fmt.Sprintf(`-i -u %s hdbnsutil -sr_state | grep "^mode: primary"`, sidAdminUser),
	})
	switch {
	case result.Error != nil && !result.ExitStatusParsed:
		log.CtxLogger(ctx).Debugw("Could not execute hdbnsutil command", "error", result.Error, "sidAdminUser", sidAdminUser)
		return ""
	case result.ExitCode != 0:
		return ""
	}

	// Check if the replication site is in the same region as the primary.
	for _, system := range params.Discovery.GetSAPSystems() {
		for _, site := range system.GetDatabaseLayer().GetReplicationSites() {
			if site.GetComponent().GetRegion() != system.GetDatabaseLayer().GetRegion() {
				continue
			}
			drSitesSameRegion = append(drSitesSameRegion, instanceNameFromResources(site.GetComponent().GetResources()))
		}
	}

	return strings.Join(drSitesSameRegion, ",")
}

// instanceNameFromResources returns the first instance name found in the resources list.
func instanceNameFromResources(resources []*systempb.SapDiscovery_Resource) string {
	for _, resource := range resources {
		if resource.GetResourceKind() != systempb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
			continue
		}
		return clouddiscovery.ExtractFromURI(resource.GetResourceUri(), instancesURIPart)
	}
	return ""
}

// hanaBackupMetrics gathers information about the backups for the HANA DB.
//
// Metrics are collected for primary/standalone HANA DB instances only.
// The following metrics are returned:
//   - oldest_backup_tenant_name: Always empty string (""); retained for schema
//     compatibility.
//   - oldest_last_backup_timestamp_utc: The UTC timestamp of the oldest last
//     full backup across SYSTEMDB and tenant DBs in ISO 8601 format, or "" if
//     not found.
//   - oldest_delta_backup_tenant_name: Always empty string (""); retained for
//     schema compatibility.
//   - oldest_last_delta_backup_timestamp_utc: The UTC timestamp of the oldest
//     last delta backup across SYSTEMDB and tenant DBs in ISO 8601 format, or
//     "" if not found.
//   - oldest_snapshot_backup_tenant_name: Always empty string (""); retained
//     for schema compatibility.
//   - oldest_last_snapshot_backup_timestamp_utc: The UTC timestamp of the
//     oldest last snapshot backup across SYSTEMDB and tenant DBs in ISO 8601
//     format, or "" if not found.
func hanaBackupMetrics(ctx context.Context, instance *sapb.SAPInstance, discovery discoveryInterface, exec commandlineexecutor.Execute) map[string]string {
	results := map[string]string{
		"oldest_backup_tenant_name":                 "",
		"oldest_last_backup_timestamp_utc":          "",
		"oldest_delta_backup_tenant_name":           "",
		"oldest_last_delta_backup_timestamp_utc":    "",
		"oldest_snapshot_backup_tenant_name":        "",
		"oldest_last_snapshot_backup_timestamp_utc": "",
	}
	if instance.GetSite() == sapb.InstanceSite_HANA_SECONDARY || instance.GetSite() == sapb.InstanceSite_HANA_DR {
		log.CtxLogger(ctx).Debugw("Skipping HANA backup metrics collection on non-primary/non-standalone node", "site", instance.GetSite().String())
		return results
	}
	traceDirs := discoverHANADBTraceDirs(ctx, instance, discovery)
	if len(traceDirs) == 0 {
		log.CtxLogger(ctx).Debug("No HANA DB trace directories found")
		return results
	}

	var timestampFull, timestampDelta, timestampSnapshot time.Time
	for i, dirPath := range traceDirs {
		full, delta, snapshot, err := fetchLastBackupTimestamps(ctx, dirPath, exec)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not find last backup timestamps", "dirPath", dirPath, "error", err)
			return results
		}
		if i == 0 || full.Before(timestampFull) {
			timestampFull = full
		}
		if i == 0 || delta.Before(timestampDelta) {
			timestampDelta = delta
		}
		if i == 0 || snapshot.Before(timestampSnapshot) {
			timestampSnapshot = snapshot
		}
	}

	if !timestampFull.IsZero() {
		results["oldest_last_backup_timestamp_utc"] = timestampFull.UTC().Format(time.RFC3339)
	}
	if !timestampDelta.IsZero() {
		results["oldest_last_delta_backup_timestamp_utc"] = timestampDelta.UTC().Format(time.RFC3339)
	}
	if !timestampSnapshot.IsZero() {
		results["oldest_last_snapshot_backup_timestamp_utc"] = timestampSnapshot.UTC().Format(time.RFC3339)
	}
	return results
}

// discoverHANADBTraceDirs returns the trace directory paths for SYSTEMDB and
// each tenant database belonging to the given HANA instance.
func discoverHANADBTraceDirs(ctx context.Context, instance *sapb.SAPInstance, discovery discoveryInterface) []string {
	if instance.GetSapsid() == "" || instance.GetInstanceNumber() == "" || instance.GetHostname() == "" {
		log.CtxLogger(ctx).Debugw("Incomplete HANA instance metadata, cannot determine backup trace directories", "sapsid", instance.GetSapsid(), "instanceNumber", instance.GetInstanceNumber(), "hostname", instance.GetHostname())
		return nil
	}
	baseTraceDir := fmt.Sprintf("/usr/sap/%s/HDB%s/%s/trace/", instance.GetSapsid(), instance.GetInstanceNumber(), instance.GetHostname())
	traceDirs := []string{baseTraceDir}

	var tenantNames []string
	if discovery != nil {
		for _, system := range discovery.GetSAPSystems() {
			dbLayer := system.GetDatabaseLayer()
			dbProps := dbLayer.GetDatabaseProperties()
			if dbProps.GetDatabaseSid() != instance.GetSapsid() || dbProps.GetInstanceNumber() != instance.GetInstanceNumber() {
				continue
			}
			tenant := dbLayer.GetSid()
			if tenant == "" || strings.EqualFold(tenant, "SYSTEMDB") || slices.Contains(tenantNames, tenant) {
				continue
			}
			tenantNames = append(tenantNames, tenant)
		}
	}
	if len(tenantNames) == 0 {
		tenantNames = []string{instance.GetSapsid()}
	}
	for _, tenant := range tenantNames {
		traceDirs = append(traceDirs, fmt.Sprintf("%sDB_%s/", baseTraceDir, tenant))
	}
	return traceDirs
}

// fetchLastBackupTimestamps fetches the following timestamps from backup logs
// in dirPath:
//   - The timestamp of the latest successful full backup.
//   - The timestamp of the latest incremental or differential backup.
//   - The timestamp of the latest successful snapshot backup.
func fetchLastBackupTimestamps(ctx context.Context, dirPath string, exec commandlineexecutor.Execute) (full, delta, snapshot time.Time, err error) {
	full, delta, snapshot = time.Time{}, time.Time{}, time.Time{}

	// Fetch a list of successful backups and process them from most to least
	// recent to find a timestamp for each backup type (full, delta, snapshot).
	backups, err := fetchSuccessfulBackups(ctx, dirPath, exec)
	if err != nil {
		return full, delta, snapshot, err
	}
	if len(backups) == 0 {
		log.CtxLogger(ctx).Debugw("No successful HANA backups found", "dirPath", dirPath)
		return full, delta, snapshot, nil
	}
	backupTypeMap, err := fetchBackupThreadIDs(ctx, dirPath, exec)
	if err != nil {
		return full, delta, snapshot, err
	}
	if len(backupTypeMap) == 0 {
		log.CtxLogger(ctx).Debugw("No HANA backup commands found", "dirPath", dirPath)
		return full, delta, snapshot, nil
	}

	sort.Slice(backups, func(i, j int) bool { return backups[i].finishTime.After(backups[j].finishTime) })
	for _, backup := range backups {
		if !delta.IsZero() && !full.IsZero() && !snapshot.IsZero() {
			break
		}
		if backupType, ok := backupTypeMap[backup.backupID]; ok {
			switch {
			case full.IsZero() && backupType == hanaBackupFull:
				// This is the latest successful full backup for this database.
				full = backup.finishTime
			case delta.IsZero() && backupType == hanaBackupDelta:
				// This is the latest delta backup for this database.
				delta = backup.finishTime
			case snapshot.IsZero() && backupType == hanaBackupSnapshot:
				// This is the latest snapshot backup for this database.
				snapshot = backup.finishTime
			}
		}
	}

	if full.IsZero() {
		log.CtxLogger(ctx).Debugw("No full backup found", "dirPath", dirPath)
	}
	if delta.IsZero() {
		log.CtxLogger(ctx).Debugw("No delta backup found", "dirPath", dirPath)
	}
	if snapshot.IsZero() {
		log.CtxLogger(ctx).Debugw("No snapshot backup found", "dirPath", dirPath)
	}
	return full, delta, snapshot, nil
}

// fetchSuccessfulBackups fetches the list of successful backups from the
// backup.log files directly inside the given trace directory.
func fetchSuccessfulBackups(ctx context.Context, dirPath string, exec commandlineexecutor.Execute) ([]hanaBackupLog, error) {
	var backupList []hanaBackupLog
	result := exec(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args: []string{
			"find", dirPath, "-maxdepth", "1", "-name", "backup.*log",
			"-exec", "grep", "--no-filename", "-E", "(SNAPSHOT|SAVE DATA) finished successfully", "{}", "+",
		},
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find HANA backups", "error", result.Error)
		return backupList, result.Error
	}
	result.StdOut = strings.TrimSuffix(result.StdOut, "\n")
	backupLogs := strings.Split(result.StdOut, "\n")
	for _, backupLog := range backupLogs {
		matches := successfulBackupRegex.FindStringSubmatch(backupLog)
		if len(matches) != 4 {
			log.CtxLogger(ctx).Debugw("Could not parse backup success log")
			continue
		}
		backupTimestamp, err := time.Parse(timestampLayout, matches[1])
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not parse backup timestamp", "error", err)
			continue
		}
		threadID := matches[2]
		backupType := matches[3]
		backupList = append(backupList, hanaBackupLog{
			finishTime: backupTimestamp,
			backupID:   threadID,
			backupType: backupType,
		})
	}
	return backupList, nil
}

// fetchBackupThreadIDs parses backup logs directly inside the given trace
// directory to return a map of backup thread IDs to backup type.
func fetchBackupThreadIDs(ctx context.Context, dirPath string, exec commandlineexecutor.Execute) (map[string]hanaBackupType, error) {
	backups := make(map[string]hanaBackupType)
	result := exec(ctx, commandlineexecutor.Params{
		Executable: "sudo",
		Args: []string{
			"find", dirPath, "-maxdepth", "1", "-name", "backup.*log",
			"-exec", "grep", "--no-filename", "-E", "INFO\\s+BACKUP\\s+command:", "{}", "+",
		},
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find commands for HANA backups", "error", result.Error)
		return backups, result.Error
	}
	result.StdOut = strings.TrimSuffix(result.StdOut, "\n")
	backupCommandLogs := strings.Split(result.StdOut, "\n")
	for _, backupCommandLog := range backupCommandLogs {
		matches := backupCommandRegex.FindStringSubmatch(backupCommandLog)
		if len(matches) != 3 {
			log.CtxLogger(ctx).Debugw("Could not get backup command from backup log")
			continue
		}
		threadID := matches[1]
		backupCommand := strings.ToLower(matches[2])
		switch {
		case strings.Contains(backupCommand, "differential"):
			backups[threadID] = hanaBackupDelta
		case strings.Contains(backupCommand, "incremental"):
			backups[threadID] = hanaBackupDelta
		case strings.Contains(backupCommand, "snapshot"):
			backups[threadID] = hanaBackupSnapshot
		default:
			backups[threadID] = hanaBackupFull
		}
	}
	return backups, nil
}

// chksrvHookActive checks if the chksrv hook is active for the given instance.
func chksrvHookActive(ctx context.Context, instance *sapb.SAPInstance, exec commandlineexecutor.Execute, osVendorID string) string {
	filename := "nameserver_chksrv.trc"
	if osVendorID == "sles" {
		filename = "nameserver_suschksrv.trc"
	}
	path := fmt.Sprintf("/usr/sap/%s/%s/%s/trace/%s", instance.GetSapsid(), instance.GetInstanceId(), instance.GetHostname(), filename)
	result := exec(ctx, commandlineexecutor.Params{
		Executable: "test",
		Args:       []string{"-f", path},
	})
	if result.Error != nil {
		return "false"
	}
	return "true"
}
