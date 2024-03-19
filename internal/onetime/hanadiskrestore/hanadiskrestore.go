/*
Copyright 2023 Google LLC

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

// Package hanadiskrestore implements one time execution for HANA Disk based restore workflow.
package hanadiskrestore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"flag"
	backoff "github.com/cenkalti/backoff/v4"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

type (
	// computeServiceFunc provides testable replacement for compute.Service API
	computeServiceFunc func(context.Context) (*compute.Service, error)
)

const (
	metricPrefix = "workload.googleapis.com/sap/agent/"
)

var (
	workflowStartTime time.Time
)

// Restorer has args for hanadiskrestore subcommands
// TODO: Improve Disk Backup and Restore code coverage and reduce redundancy.
type Restorer struct {
	Project, Sid, HanaSidAdm, DataDiskName, DataDiskDeviceName string
	DataDiskZone, SourceSnapshot, NewDiskType                  string
	LogProperties                                              log.Parameters
	CloudProps                                                 *ipb.CloudProperties
	computeService                                             *compute.Service
	baseDataPath, baseLogPath                                  string
	logicalDataPath, logicalLogPath                            string
	physicalDataPath, physicalLogPath                          string
	timeSeriesCreator                                          cloudmonitoring.TimeSeriesCreator
	help, version                                              bool
	SendToMonitoring                                           bool
	SkipDBSnapshotForChangeDiskType                            bool
	HANAChangeDiskTypeOTEName                                  string
	LogLevel                                                   string
	ForceStopHANA                                              bool
	NewdiskName                                                string
	CSEKKeyFile                                                string
	ProvisionedIops, ProvisionedThroughput, DiskSizeGb         int64
}

// Name implements the subcommand interface for hanadiskrestore.
func (*Restorer) Name() string { return "hanadiskrestore" }

// Synopsis implements the subcommand interface for hanadiskrestore.
func (*Restorer) Synopsis() string {
	return "invoke HANA hanadiskrestore using workflow to restore from disk snapshot"
}

// Usage implements the subcommand interface for hanadiskrestore.
func (*Restorer) Usage() string {
	return `Usage: hanadiskrestore -sid=<HANA-sid> -source-snapshot=<snapshot-name>
	-data-disk-name=<disk-name> -data-disk-zone=<disk-zone> -new-disk-name=<name-less-than-63-chars>
	[-project=<project-name>] [-new-disk-type=<Type of the new disk>] [-force-stop-hana=<true|false>]
	[-hana-sidadm=<hana-sid-user-name>] [-provisioned-iops=<Integer value between 10,000 and 120,000>]
	[-provisioned-throughput=<Integer value between 1 and 7,124>] [-disk-size-gb=<New disk size in GB>]
	[-send-metrics-to-monitoring]=<true|false>
	[csek-key-file]=<path-to-key-file>]
	[-h] [-v] [-loglevel]=<debug|info|warn|error>` + "\n"
}

// SetFlags implements the subcommand interface for hanadiskrestore.
func (r *Restorer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.Sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&r.DataDiskName, "data-disk-name", "", "Current disk name. (required)")
	fs.StringVar(&r.DataDiskZone, "data-disk-zone", "", "Current disk zone. (required)")
	fs.StringVar(&r.SourceSnapshot, "source-snapshot", "", "Source disk snapshot to restore from. (required)")
	fs.StringVar(&r.NewdiskName, "new-disk-name", "", "New disk name. (required) must be less than 63 characters long")
	fs.StringVar(&r.Project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.StringVar(&r.NewDiskType, "new-disk-type", "", "Type of the new disk. (optional) Default: same type as disk passed in data-disk-name.")
	fs.StringVar(&r.HanaSidAdm, "hana-sidadm", "", "HANA sidadm username. (optional) Default: <sid>adm")
	fs.BoolVar(&r.ForceStopHANA, "force-stop-hana", false, "Forcefully stop HANA using `HDB kill` before attempting restore.(optional) Default: false.")
	fs.Int64Var(&r.DiskSizeGb, "disk-size-gb", 0, "New disk size in GB, must not be less than the size of the source (optional)")
	fs.Int64Var(&r.ProvisionedIops, "provisioned-iops", 0, "Number of I/O operations per second that the disk can handle. (optional)")
	fs.Int64Var(&r.ProvisionedThroughput, "provisioned-throughput", 0, "Number of throughput mb per second that the disk can handle. (optional)")
	fs.StringVar(&r.CSEKKeyFile, "csek-key-file", "", `Path to a Customer-Supplied Encryption Key (CSEK) key file for the source snapshot. (required if source snapshot is encrypted)`)
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.BoolVar(&r.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&r.LogLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for hanadiskrestore.
func (r *Restorer) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if r.help {
		return onetime.HelpCommand(f)
	}
	if r.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 && !r.SkipDBSnapshotForChangeDiskType {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	var ok bool
	if !r.SkipDBSnapshotForChangeDiskType {
		r.LogProperties, ok = args[1].(log.Parameters)
		if !ok {
			log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
			return subcommands.ExitUsageError
		}
	}
	if !r.SkipDBSnapshotForChangeDiskType {
		r.CloudProps, ok = args[2].(*ipb.CloudProperties)
		if !ok {
			log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
			return subcommands.ExitUsageError
		}
	}
	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		return subcommands.ExitFailure
	}
	r.timeSeriesCreator = mc
	if r.SkipDBSnapshotForChangeDiskType {
		onetime.SetupOneTimeLogging(r.LogProperties, r.HANAChangeDiskTypeOTEName, log.StringLevelToZapcore(r.LogLevel))
	} else {
		onetime.SetupOneTimeLogging(r.LogProperties, r.Name(), log.StringLevelToZapcore(r.LogLevel))
	}

	return r.restoreHandler(ctx, onetime.NewComputeService)
}

// validateParameters validates the parameters passed to the restore subcommand.
func (r *Restorer) validateParameters(os string) error {
	if r.SkipDBSnapshotForChangeDiskType {
		log.Logger.Debug("Skip DB Snapshot for Change Disk Type")
		return nil
	}
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot restore is only supported on Linux systems")
	case r.Sid == "" || r.DataDiskName == "" || r.DataDiskZone == "" || r.SourceSnapshot == "" || r.NewdiskName == "":
		return fmt.Errorf("required arguments not passed. Usage: %s", r.Usage())
	}
	if len(r.NewdiskName) > 63 {
		return fmt.Errorf("the new-disk-name is longer than 63 chars which is not supported, please provide a shorter name")
	}

	if r.Project == "" {
		r.Project = r.CloudProps.GetProjectId()
	}

	if r.HanaSidAdm == "" {
		r.HanaSidAdm = strings.ToLower(r.Sid) + "adm"
	}

	log.Logger.Debug("Parameter validation successful.")
	log.Logger.Infof("List of parameters to be used: %+v", r)

	return nil
}

// restoreHandler is the main handler for the restore subcommand.
func (r *Restorer) restoreHandler(ctx context.Context, computeServiceCreator computeServiceFunc) subcommands.ExitStatus {
	var err error
	if err = r.validateParameters(runtime.GOOS); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}
	log.CtxLogger(ctx).Infow("Starting HANA disk snapshot restore", "sid", r.Sid)
	onetime.ConfigureUsageMetricsForOTE(r.CloudProps, "", "")
	usagemetrics.Action(usagemetrics.HANADiskRestore)

	if r.computeService, err = computeServiceCreator(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create compute service,", err)
		return subcommands.ExitFailure
	}

	if err := r.checkPreConditions(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Pre-restore check failed,", err)
		return subcommands.ExitFailure
	}
	if !r.SkipDBSnapshotForChangeDiskType {
		if err := r.prepare(ctx); err != nil {
			onetime.LogErrorToFileAndConsole("ERROR: HANA restore prepare failed,", err)
			return subcommands.ExitFailure
		}
	} else {
		if err := r.prepareForHANAChangeDiskType(ctx); err != nil {
			onetime.LogErrorToFileAndConsole("ERROR: HANA restore prepare failed,", err)
			return subcommands.ExitFailure
		}
	}
	workflowStartTime = time.Now()
	if err := r.restoreFromSnapshot(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: HANA restore from snapshot failed,", err)
		// If restore fails, attach the old disk, rescan the volumes and delete the new disk.
		r.attachDisk(ctx, r.DataDiskName)
		r.rescanVolumeGroups(ctx)
		return subcommands.ExitFailure
	}
	workflowDur := time.Since(workflowStartTime)
	defer r.sendDurationToCloudMonitoring(ctx, metricPrefix+r.Name()+"/totaltime", workflowDur, cloudmonitoring.NewDefaultBackOffIntervals())
	onetime.LogMessageToFileAndConsole("SUCCESS: HANA restore from disk snapshot successful. Please refer https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/disk-snapshot-backup-recovery#recover_to_specific_point-in-time for next steps.")
	return subcommands.ExitSuccess
}

// prepare stops HANA, unmounts data directory and detaches old data disk.
func (r *Restorer) prepare(ctx context.Context) error {
	mountPath, err := r.readDataDirMountPath(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		return fmt.Errorf("failed to read data directory mount path: %v", err)
	}
	if err := r.StopHANA(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("failed to stop HANA: %v", err)
	}
	if err := r.WaitForIndexServerToStopWithRetry(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("hdbindexserver process still running after HANA is stopped: %v", err)
	}

	if err := r.unmount(ctx, mountPath, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("failed to unmount data directory: %v", err)
	}

	if err := r.detachDisk(ctx); err != nil {
		// If detach fails, rescan the volume groups to ensure the directories are mounted.
		r.rescanVolumeGroups(ctx)
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}

	log.CtxLogger(ctx).Info("HANA restore prepare succeeded.")
	return nil
}

func (r *Restorer) prepareForHANAChangeDiskType(ctx context.Context) error {
	mountPath, err := r.readDataDirMountPath(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		return fmt.Errorf("failed to read data directory mount path: %v", err)
	}
	if err := r.unmount(ctx, mountPath, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("failed to unmount data directory: %v", err)
	}
	if err := r.detachDisk(ctx); err != nil {
		// If detach fails, rescan the volume groups to ensure the directories are mounted.
		r.rescanVolumeGroups(ctx)
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	log.CtxLogger(ctx).Info("HANA restore prepareForHANAChangeDiskType succeeded.")
	return nil
}

// TODO: Move generic disk related functions from hanadiskbackup.go and hanadiskrestore.go to gce.go.
// detachDisk detaches the old HANA data disk from the instance.
func (r *Restorer) detachDisk(ctx context.Context) error {
	// Detach old HANA data disk.
	log.Logger.Infow("Detatching old HANA disk", "diskName", r.DataDiskName, "deviceName", r.DataDiskDeviceName)
	op, err := r.computeService.Instances.DetachDisk(r.Project, r.DataDiskZone, r.CloudProps.GetInstanceName(), r.DataDiskDeviceName).Do()
	if err != nil {
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("detach data disk operation failed: %v", err)
	}

	_, ok, err := r.isDiskAttachedToInstance(r.DataDiskName)
	if err != nil {
		return fmt.Errorf("failed to check if disk %v is still attached to the instance", r.DataDiskName)
	}
	if ok {
		return fmt.Errorf("Disk %v is still attached to the instance", r.DataDiskName)
	}
	return nil
}

// isDiskAttachedToInstance checks if the given disk is attached to the instance.
func (r *Restorer) isDiskAttachedToInstance(diskName string) (string, bool, error) {
	instance, err := r.computeService.Instances.Get(r.Project, r.DataDiskZone, r.CloudProps.GetInstanceName()).Do()
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: HANA restore from snapshot failed,", err)
		return "", false, fmt.Errorf("failed to get instance: %v", err)
	}
	for _, disk := range instance.Disks {
		onetime.LogMessageToFileAndConsole(fmt.Sprintf("Disk name %s", disk.Source))
		if strings.Contains(disk.Source, diskName) {
			return disk.DeviceName, true, nil
		}
	}
	return "", false, nil
}

// restoreFromSnapshot creates a new HANA data disk and attaches it to the instance.
func (r *Restorer) restoreFromSnapshot(ctx context.Context) error {
	snapShotKey := ""
	if r.CSEKKeyFile != "" {
		usagemetrics.Action(usagemetrics.EncryptedSnapshotRestore)
		snapShotURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/snapshots/%s", r.Project, r.DataDiskZone, r.SourceSnapshot)
		key, err := readKey(r.CSEKKeyFile, snapShotURI, os.ReadFile)
		if err != nil {
			usagemetrics.Error(usagemetrics.EncryptedSnapshotRestoreFailure)
			return err
		}
		snapShotKey = key
	}
	disk := &compute.Disk{
		Name:                        r.NewdiskName,
		Type:                        r.NewDiskType,
		Zone:                        r.DataDiskZone,
		SourceSnapshot:              fmt.Sprintf("projects/%s/global/snapshots/%s", r.Project, r.SourceSnapshot),
		SourceSnapshotEncryptionKey: &compute.CustomerEncryptionKey{RsaEncryptedKey: snapShotKey},
	}
	if r.DiskSizeGb > 0 {
		disk.SizeGb = r.DiskSizeGb
	}
	if r.ProvisionedIops > 0 {
		disk.ProvisionedIops = r.ProvisionedIops
	}
	if r.ProvisionedThroughput > 0 {
		disk.ProvisionedThroughput = r.ProvisionedThroughput
	}
	log.Logger.Infow("Inserting new HANA disk from source snapshot", "diskName", r.NewdiskName, "sourceSnapshot", r.SourceSnapshot)
	op, err := r.computeService.Disks.Insert(r.Project, r.DataDiskZone, disk).Do()
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: HANA restore from snapshot failed,", err)
		return fmt.Errorf("failed to insert new data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		onetime.LogErrorToFileAndConsole("insert data disk failed", err)
		return fmt.Errorf("insert data disk operation failed: %v", err)
	}

	log.Logger.Infow("Attaching new HANA disk", "diskName", r.NewdiskName)
	op, err = r.attachDisk(ctx, r.NewdiskName)
	if err != nil {
		return fmt.Errorf("failed to attach new data disk to instance: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("attach data disk operation failed: %v", err)
	}

	_, ok, err := r.isDiskAttachedToInstance(r.NewdiskName)
	if err != nil {
		return fmt.Errorf("failed to check if new disk %v is attached to the instance", r.NewdiskName)
	}
	if !ok {
		return fmt.Errorf("newly created disk %v is not attached to the instance", r.NewdiskName)
	}

	log.Logger.Info("New disk created from snapshot successfully attached to the instance.")

	r.rescanVolumeGroups(ctx)
	log.CtxLogger(ctx).Info("HANA restore from snapshot succeeded.")
	return nil
}

// attachDisk attaches the disk with the given name to the instance.
func (r *Restorer) attachDisk(ctx context.Context, diskName string) (*compute.Operation, error) {
	log.CtxLogger(ctx).Infow("Attaching disk", "diskName", diskName)
	attachDiskToVM := &compute.AttachedDisk{
		DeviceName: diskName, // Keep the device nam and disk name same.
		Source:     fmt.Sprintf("projects/%s/zones/%s/disks/%s", r.Project, r.DataDiskZone, diskName),
	}
	return r.computeService.Instances.AttachDisk(r.Project, r.DataDiskZone, r.CloudProps.GetInstanceName(), attachDiskToVM).Do()
}

// checkPreConditions checks if the HANA data and log disks are on the same physical disk.
// Also verifies that the data disk is attached to the instance.
func (r *Restorer) checkPreConditions(ctx context.Context) error {
	if err := r.checkDataDir(ctx); err != nil {
		return err
	}
	if err := r.checkLogDir(ctx); err != nil {
		return err
	}

	log.CtxLogger(ctx).Infow("Checking preconditions", "Data directory", r.baseDataPath, "Data file system",
		r.logicalDataPath, "Data physical volume", r.physicalDataPath, "Log directory", r.baseLogPath,
		"Log file system", r.logicalLogPath, "Log physical volume", r.physicalLogPath)

	if r.physicalDataPath == r.physicalLogPath {
		return fmt.Errorf("unsupported: HANA data and HANA log are on the same physical disk - %s", r.physicalDataPath)
	}

	// Verify the disk is attached to the instance.
	dev, ok, err := r.isDiskAttachedToInstance(r.DataDiskName)
	if err != nil {
		return fmt.Errorf("failed to verify if disk %v is attached to the instance", r.DataDiskName)
	}
	if !ok {
		return fmt.Errorf("the disk data-disk-name=%v is not attached to the instance, please pass the current data disk name", r.DataDiskName)
	}
	r.DataDiskDeviceName = dev

	// Verify the snapshot is present.
	if _, err = r.computeService.Snapshots.Get(r.Project, r.SourceSnapshot).Do(); err != nil {
		return fmt.Errorf("failed to check if source-snapshot=%v is present: %v", r.SourceSnapshot, err)
	}

	if r.NewDiskType == "" {
		d, err := r.computeService.Disks.Get(r.Project, r.DataDiskZone, r.DataDiskName).Do()
		if err != nil {
			return fmt.Errorf("failed to read data disk type: %v", err)
		}
		r.NewDiskType = d.Type
		log.CtxLogger(ctx).Infow("New disk type will be same as the data-disk-name", "diskType", r.NewDiskType)
	} else {
		r.NewDiskType = fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", r.Project, r.DataDiskZone, r.NewDiskType)
	}
	return nil
}

// checkDataDir checks if the data directory is valid and has a valid physical volume.
func (r *Restorer) checkDataDir(ctx context.Context) error {
	var err error
	if r.baseDataPath, err = r.parseBasePath(ctx, "basepath_datavolumes", commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Data volume base path", "path", r.baseDataPath)
	if r.logicalDataPath, err = r.parseLogicalPath(ctx, r.baseDataPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	if !strings.HasPrefix(r.logicalDataPath, "/dev/mapper") {
		return fmt.Errorf("only data disks using LVM are supported, exiting")
	}
	if r.physicalDataPath, err = r.parsePhysicalPath(ctx, r.logicalDataPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	return r.checkDataDeviceForStripes(ctx, commandlineexecutor.ExecuteCommand)
}

// checkLogDir checks if the log directory is valid and has a valid physical volume.
func (r *Restorer) checkLogDir(ctx context.Context) error {
	var err error
	if r.baseLogPath, err = r.parseBasePath(ctx, "basepath_logvolumes", commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Log volume base path", "path", r.baseLogPath)

	if r.logicalLogPath, err = r.parseLogicalPath(ctx, r.baseLogPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	if r.physicalLogPath, err = r.parsePhysicalPath(ctx, r.logicalLogPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	return nil
}

// parseBasePath parses the base path from the global.ini file.
func (r *Restorer) parseBasePath(ctx context.Context, pattern string, exec commandlineexecutor.Execute) (string, error) {
	args := `-c 'grep ` + pattern + ` /usr/sap/*/SYS/global/hdb/custom/config/global.ini | cut -d= -f 2'`
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing base path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	return strings.TrimSuffix(result.StdOut, "\n"), nil
}

// parseLogicalPath parses the logical path from the base path.
func (r *Restorer) parseLogicalPath(ctx context.Context, basePath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf("-c 'df --output=source %s | tail -n 1'", basePath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing logical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	logicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Directory to logical device mapping", "DirectoryPath", basePath, "LogicalDevice", logicalDevice)
	return logicalDevice, nil
}

// parsePhysicalPath parses the physical path from the logical path.
func (r *Restorer) parsePhysicalPath(ctx context.Context, logicalPath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf("-c '/sbin/lvdisplay -m %s | grep \"Physical volume\" | awk \"{print \\$3}\"'", logicalPath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing physical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	phyisicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Logical device to physical device mapping", "LogicalDevice", logicalPath, "PhysicalDevice", phyisicalDevice)
	return phyisicalDevice, nil
}

// checkDataDeviceForStripes checks if the data device is striped.
func (r *Restorer) checkDataDeviceForStripes(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c '/sbin/lvdisplay -m %s | grep Stripes'", r.logicalDataPath),
	})
	if result.ExitCode == 0 {
		return fmt.Errorf("restore of striped HANA data disks are not currently supported, exiting")
	}
	return nil
}

// StopHANA stops the HANA instance.
func (r *Restorer) StopHANA(ctx context.Context, exec commandlineexecutor.Execute) error {
	var cmd string
	if r.ForceStopHANA {
		log.CtxLogger(ctx).Infow("HANA force stopped", "sid", r.Sid)
		cmd = fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && /usr/sap/%s/*/HDB stop'", r.Sid, r.Sid) // NOLINT
	} else {
		log.CtxLogger(ctx).Infow("Stopping HANA", "sid", r.Sid)
		cmd = fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && /usr/sap/%s/*/HDB kill'", r.Sid, r.Sid) // NOLINT
	}
	result := exec(ctx, commandlineexecutor.Params{
		User:        r.HanaSidAdm,
		Executable:  "bash",
		ArgsToSplit: cmd,
		Timeout:     300,
	})
	if result.Error != nil {
		return fmt.Errorf("failure stopping HANA, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("HANA stopped", "sid", r.Sid)
	return nil
}

// readDataDirMountPath reads the data directory mount path.
func (r *Restorer) readDataDirMountPath(ctx context.Context, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c 'df --output=target %s| tail -n 1'", r.baseDataPath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure reading data directory mount path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	return strings.TrimSuffix(result.StdOut, "\n"), nil
}

// unmount unmounts the given directory.
func (r *Restorer) unmount(ctx context.Context, path string, exec commandlineexecutor.Execute) error {
	log.Logger.Infow("Unmount path", "directory", path)
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c 'sync;umount -f %s'", path),
	})
	if result.Error != nil {
		r := exec(ctx, commandlineexecutor.Params{
			Executable:  "bash",
			ArgsToSplit: fmt.Sprintf(" -c 'lsof | grep %s'", path), // NOLINT
		})
		msg := `failure unmounting directory: %s, stderr: %s, err: %s.
		Here are the possible open references to the path, stdout: %s stderr: %s.
		Please ensure these references are cleaned up for unmount to proceed and retry the command`
		return fmt.Errorf(msg, path, result.StdErr, result.Error, r.StdOut, r.StdErr)
	}
	return nil
}

// rescanVolumeGroups rescans all volume groups and mounts them.
func (r *Restorer) rescanVolumeGroups(ctx context.Context) error {
	log.CtxLogger(ctx).Infow("Rescanning volume groups", "sid", r.Sid)
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/dmsetup",
		ArgsToSplit: "remove_all",
	})
	if result.Error != nil {
		return fmt.Errorf("failure removing device definitions from the Device Mapper driver, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgscan",
		ArgsToSplit: "-v --mknodes",
	})
	if result.Error != nil {
		return fmt.Errorf("failure scanning volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgchange",
		ArgsToSplit: "-ay",
	})
	if result.Error != nil {
		return fmt.Errorf("failure changing volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable: "/sbin/lvscan",
	})
	if result.Error != nil {
		return fmt.Errorf("failure scanning volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	time.Sleep(5 * time.Second)
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "mount",
		ArgsToSplit: "-av",
	})
	if result.Error != nil {
		return fmt.Errorf("failure mounting volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	return nil
}

// waitForCompletion waits for the given compute operation to complete.
func (r *Restorer) waitForCompletion(op *compute.Operation) error {
	zos := compute.NewZoneOperationsService(r.computeService)
	tracker, err := zos.Wait(r.Project, r.DataDiskZone, op.Name).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("Compute operation is not DONE yet")
	}
	return nil
}

// Each waitForCompletion() returns immediately, we sleep for 120s between
// retries a total 10 times => max_wait_duration = 10*120 = 20 Minutes
// TODO: change timeout depending on disk snapshot limits
func (r *Restorer) waitForCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 10), ctx)
	return backoff.Retry(func() error { return r.waitForCompletion(op) }, bo)
}

// waitForIndexServerToStop() waits for the hdb index server to stop.
func (r *Restorer) waitForIndexServerToStop(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: `-c 'ps x | grep hdbindexs | grep -v grep'`,
		User:        r.HanaSidAdm,
	})

	if result.ExitCode == 0 {
		return fmt.Errorf("failure waiting for index server to stop, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	return nil
}

// WaitForIndexServerToStopWithRetry waits for the index server to stop with retry.
// We sleep for 10s between retries a total 60 time => max_wait_duration =  10*60 = 10 minutes
func (r *Restorer) WaitForIndexServerToStopWithRetry(ctx context.Context, exec commandlineexecutor.Execute) error {
	constantBackoff := backoff.NewConstantBackOff(10 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 60), ctx)
	return backoff.Retry(func() error { return r.waitForIndexServerToStop(ctx, exec) }, bo)
}

func (r *Restorer) sendDurationToCloudMonitoring(ctx context.Context, mtype string, dur time.Duration, bo *cloudmonitoring.BackOffIntervals) bool {
	if !r.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Sending HANA disk snapshot duration to cloud monitoring", "duration", dur)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildFloat64(timeseries.Params{
			CloudProp:    r.CloudProps,
			MetricType:   mtype,
			Timestamp:    tspb.Now(),
			Float64Value: dur.Seconds(),
			MetricLabels: map[string]string{
				"sid":           r.Sid,
				"snapshot_name": r.SourceSnapshot,
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, r.timeSeriesCreator, bo, r.Project); err != nil {
		log.CtxLogger(ctx).Debugw("Error sending duration metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}

// Key defines the contents of each entry in the encryption key file.
// Reference: https://cloud.google.com/compute/docs/disks/customer-supplied-encryption#key_file
type Key struct {
	URI     string `json:"uri"`
	Key     string `json:"key"`
	KeyType string `json:"key-type"`
}

func readKey(file, diskURI string, read configuration.ReadConfigFile) (string, error) {
	var keys []Key
	fileContent, err := read(file)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(fileContent, &keys); err != nil {
		return "", err
	}

	for _, k := range keys {
		if k.URI == diskURI {
			return k.Key, nil
		}
	}
	return "", fmt.Errorf("no matching key for for the disk")
}
