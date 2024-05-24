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

	"github.com/google/safetext/shsprintf"
	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const (
	sapValidationHANA = "workload.googleapis.com/sap/validation/hana"
	timestampLayout = "2006-01-02T15:04:05-07:00"
)

var instanceURIRegex = regexp.MustCompile("/projects/(.+)/zones/(.+)/instances/(.+)")
var successfulBackupRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[\+\-]\d{2}:\d{2})\s+\S+\s+(\S+)\s+INFO\s+BACKUP\s+(SNAPSHOT|SAVE DATA)\s+finished\s+successfully`)
var backupCommandRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[\+\-]\d{2}:\d{2}\s+\S+\s+(\S+)\s+INFO\s+BACKUP\s+command: (.*)`)
var sapServicesStartsrvPattern = regexp.MustCompile(`startsrv pf=/usr/sap/([A-Z][A-Z|0-9][A-Z|0-9])[/|a-z|A-Z|0-9]+/profile/([A-Z][A-Z|0-9][A-Z|0-9])_([a-z|A-Z]+)([0-9]+)_(\S+)`)

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

type hanaDBTenant struct {
	sid        string
	instanceID string
	tenantName string
}

// CollectHANAMetricsFromConfig collects the HANA metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectHANAMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	log.CtxLogger(ctx).Debugw("Collecting Workload Manager HANA metrics...", "definitionVersion", params.WorkloadConfig.GetVersion())
	l := map[string]string{}
	hanaVal := 0.0

	hanaSystemConfigDir := hanaSystemConfigFromSAPSID(ctx, params)
	if hanaSystemConfigDir == "" {
		log.CtxLogger(ctx).Debug("Skipping HANA metrics collection, HANA not active on instance")
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}

	globalINIFilePath := hanaSystemConfigDir + "/global.ini"
	// Short-circuit HANA metrics collection if global.ini file is not found.
	// In addition to the metrics contained in the file, global.ini also contains
	// basepath information for the HANA data and log volumes.
	if _, err := params.OSStatReader(globalINIFilePath); err != nil {
		log.CtxLogger(ctx).Debugw("Skipping HANA metrics collection, could not find global.ini file", "location", globalINIFilePath)
		return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
	}

	hana := params.WorkloadConfig.GetValidationHana()
	for k, v := range configurablemetrics.CollectMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), globalINIFilePath, hana.GetGlobalIniMetrics()) {
		l[k] = v
	}
	indexserverINIFilePath := hanaSystemConfigDir + "/indexserver.ini"
	for k, v := range configurablemetrics.CollectMetricsFromFile(ctx, configurablemetrics.FileReader(params.ConfigFileReader), indexserverINIFilePath, hana.GetIndexserverIniMetrics()) {
		l[k] = v
	}
	for _, m := range hana.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}
	for _, volume := range hana.GetHanaDiskVolumeMetrics() {
		diskInfo := diskInfo(ctx, volume.GetBasepathVolume(), globalINIFilePath, params.Execute, params.InstanceInfoReader)
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
	tenantName, oldestLastBackupTimestamp := oldestLastBackupTimestamp(ctx, params.Execute)
	for _, m := range hana.GetHanaBackupMetrics() {
		k := m.GetMetricInfo().GetLabel()
		switch m.GetValue() {
		case wpb.HANABackupVariable_LAST_BACKUP_TIMESTAMP:
			l[k] = oldestLastBackupTimestamp
		case wpb.HANABackupVariable_TENANT_NAME:
			l[k] = tenantName
		}
	}

	hanaVal = 1.0
	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationHANA, l, hanaVal, params.Config)}
}

// hanaSystemConfigFromSAPSID returns the path to the directory containing
// SAP HANA configuration files.
//
// File path: /usr/sap/[SID]/SYS/global/hdb/custom/config
func hanaSystemConfigFromSAPSID(ctx context.Context, params Parameters) string {
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
			return fmt.Sprintf("/usr/sap/%s/SYS/global/hdb/custom/config", instance.GetSapsid())
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

// oldestLastBackupTimestamp returns the tenant that has the oldest last backup
// and the UTC timestamp of the backup in ISO 8601 format.
//   - In case of no tenants being found, it returns "", ""
//   - In case of no backups being found for any tenant, it returns
//     the tenant name along with the zero value of time.Time
func oldestLastBackupTimestamp(ctx context.Context, exec commandlineexecutor.Execute) (tenantName, timestamp string) {
	tenants := discoverHANADBTenants(ctx, exec)
	if len(tenants) == 0 {
		log.CtxLogger(ctx).Debug("No HANA DB tenants found")
		return "", ""
	}

	lastBackupTimestamps := map[string]time.Time{}
	for _, tenant := range tenants {
		timestamp, err := fetchLastBackupTimestamp(ctx, tenant, exec)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Could not find last backup timestamp", "error", err)
			return tenant.tenantName, time.Time{}.UTC().Format(time.RFC3339)
		}
		if timestamp.IsZero() {
			// No backup found for this tenant.
			// Return tenant name along with the zero value of time.Time in ISO 8601 format.
			return tenant.tenantName, timestamp.UTC().Format(time.RFC3339)
		}
		lastBackupTimestamps[tenant.tenantName] = timestamp
	}

	return tenantOldestBackupTime(lastBackupTimestamps)
}

// tenantOldestBackupTime returns the tenant that has the oldest last backup
// with the backup timestamp in epoch.
func tenantOldestBackupTime(instanceTimestamps map[string]time.Time) (string, string) {
	earliestTenant := ""
	earliestTime := time.Time{}

	for key, timestamp := range instanceTimestamps {
		if earliestTenant == "" || timestamp.Before(earliestTime) {
			earliestTenant = key
			earliestTime = timestamp
		}
	}

	return earliestTenant, earliestTime.UTC().Format(time.RFC3339)
}

// discoverHANADBTenants returns the list of HANA DB tenants running on the host.
func discoverHANADBTenants(ctx context.Context, exec commandlineexecutor.Execute) []hanaDBTenant {
	var instances []hanaDBTenant
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "'pf=' /usr/sap/sapservices",
	})

	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find HANA tenants", "error", result.Error)
		return instances
	}

	lines := strings.Split(strings.TrimSuffix(result.StdOut, "\n"), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			log.CtxLogger(ctx).Debugw("Not processing the commented entry", "line", line)
			continue
		}
		match := sapServicesStartsrvPattern.FindStringSubmatch(line)
		if len(match) != 6 || match[3] != "HDB" {
			continue
		}

		instances = append(instances, hanaDBTenant{
			sid:        match[1],
			instanceID: match[4],
			tenantName: match[5],
		})
	}
	return instances
}

// fetchLastBackupTimestamp fetches the timestamp of the latest successful full
// or snapshot backup for a given tenant.
func fetchLastBackupTimestamp(ctx context.Context, dbTenant hanaDBTenant, exec commandlineexecutor.Execute) (time.Time, error) {
	dirPath := fmt.Sprintf("/usr/sap/%s/HDB%s/%s/trace/", dbTenant.sid, dbTenant.instanceID, dbTenant.tenantName)

	// Fetch both successful backups and full/snapshot backups. Then process the
	// successful backups from most to least recent and return the first full or
	// snapshot backup's time.
	backups, err := fetchSuccessfulBackups(ctx, dirPath, exec)
	if err != nil {
		return time.Time{}, err
	}
	if len(backups) == 0 {
		log.CtxLogger(ctx).Debugw("No successful HANA backups found for tenant")
		return time.Time{}, nil
	}
	backupTypeMap, err := fetchFullOrSnapshotBackups(ctx, dirPath, exec)
	if err != nil {
		return time.Time{}, err
	}
	if len(backups) == 0 {
		log.CtxLogger(ctx).Debugw("No full or snapshot HANA backups found for tenant")
		return time.Time{}, nil
	}

	sort.Slice(backups, func(i, j int) bool { return backups[i].finishTime.After(backups[j].finishTime) })
	for _, backup := range backups {
		if value, _ := backupTypeMap[backup.backupID]; value {
			// This is the latest successful full or snapshot backup for this tenant.
			return backup.finishTime, nil
		}
	}

	// Return the zero value of time.Time.
	// This is the oldest possible time and represents that no backup was found.
	log.CtxLogger(ctx).Debugw("No successful full or snapshot backup found for tenant")
	return time.Time{}, nil
}

// fetchSuccessfulBackups fetches the list of successful backups from the given
// tenant directory.
func fetchSuccessfulBackups(ctx context.Context, dirPath string, exec commandlineexecutor.Execute) ([]hanaBackupLog, error) {
	var backupList []hanaBackupLog
	args, _ := shsprintf.Sprintf(`sh -c 'find %s -name "backup.*log" -exec grep --no-filename -E "(SNAPSHOT|SAVE DATA) finished successfully" {} + '`, dirPath)
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: args,
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

// fetchFullOrSnapshotBackups parses backup logs in given tenant directory to
// return the full/snapshot backup thread IDs.
func fetchFullOrSnapshotBackups(ctx context.Context, dirPath string, exec commandlineexecutor.Execute) (map[string]bool, error) {
	backups := make(map[string]bool)
	deltaBackupTypes := []string{"incremental", "differential"}
	args, _ := shsprintf.Sprintf(`sh -c 'find %s -name "backup.*log" -exec grep --no-filename -E "INFO\s+BACKUP\s+command:" {} + '`, dirPath)
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: args,
	})

	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not find full or snapshot HANA backups", "error", result.Error)
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
		fullOrSnapshot := true
		for _, deltaBackupType := range deltaBackupTypes {
			if strings.Contains(backupCommand, deltaBackupType) {
				fullOrSnapshot = false
				break
			}
		}
		if fullOrSnapshot {
			backups[threadID] = true
		}
	}
	return backups, nil
}
