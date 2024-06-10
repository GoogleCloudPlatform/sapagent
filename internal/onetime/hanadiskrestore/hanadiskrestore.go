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
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// getDataPaths provides testable replacement for hanabackup.CheckDataDir
	getDataPaths func(context.Context, commandlineexecutor.Execute) (string, string, string, error)

	// getLogPaths provides testable replacement for hanabackup.CheckLogDir
	getLogPaths func(context.Context, commandlineexecutor.Execute) (string, string, string, error)

	// waitForIndexServerToStopWithRetry provides testable replacement for hanabackup.WaitForIndexServerToStopWithRetry
	waitForIndexServerToStopWithRetry func(ctx context.Context, user string, exec commandlineexecutor.Execute) error

	// gceServiceFunc provides testable replacement for gce.New API.
	gceServiceFunc func(context.Context) (*gce.GCE, error)

	// computeServiceFunc provides testable replacement for compute.Service API
	computeServiceFunc func(context.Context) (*compute.Service, error)

	// gceInterface is the testable equivalent for gce.GCE for secret manager access.
	gceInterface interface {
		GetInstance(project, zone, instance string) (*compute.Instance, error)
		ListZoneOperations(project, zone, filter string, maxResults int64) (*compute.OperationList, error)
		GetDisk(project, zone, name string) (*compute.Disk, error)
		ListDisks(project, zone, filter string) (*compute.DiskList, error)

		DiskAttachedToInstance(projectID, zone, instanceName, diskName string) (string, bool, error)
		AttachDisk(ctx context.Context, diskName string, cp *ipb.CloudProperties, project, dataDiskZone string) error
		DetachDisk(ctx context.Context, cp *ipb.CloudProperties, project, dataDiskZone, dataDiskName, dataDiskDeviceName string) error
		WaitForDiskOpCompletionWithRetry(ctx context.Context, op *compute.Operation, project, dataDiskZone string) error
		GetGroupSnapshotsService() *gce.GroupSnapshotsService
	}
)

const (
	metricPrefix = "workload.googleapis.com/sap/agent/"
)

var (
	workflowStartTime time.Time
)

// Restorer has args for hanadiskrestore subcommands
type Restorer struct {
	Project, Sid, HanaSidAdm, DataDiskName, DataDiskDeviceName string
	DataDiskZone, SourceSnapshot, GroupSnapshot, NewDiskType   string
	disks                                                      []*ipb.Disk
	gceService                                                 gceInterface
	computeService                                             *compute.Service
	baseDataPath, baseLogPath                                  string
	logicalDataPath, logicalLogPath                            string
	physicalDataPath, physicalLogPath                          string
	labelsOnDetachedDisk                                       string
	timeSeriesCreator                                          cloudmonitoring.TimeSeriesCreator
	help, version                                              bool
	SendToMonitoring                                           bool
	SkipDBSnapshotForChangeDiskType                            bool
	HANAChangeDiskTypeOTEName                                  string
	LogLevel                                                   string
	ForceStopHANA                                              bool
	isGroupSnapshot                                            bool
	NewdiskName                                                string
	CSEKKeyFile                                                string
	ProvisionedIops, ProvisionedThroughput, DiskSizeGb         int64
	IIOTEParams                                                *onetime.InternallyInvokedOTE
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
  [-h] [-v] [-loglevel=<debug|info|warn|error>]` + "\n"
}

// SetFlags implements the subcommand interface for hanadiskrestore.
func (r *Restorer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.Sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&r.DataDiskName, "data-disk-name", "", "Current disk name. (required)")
	fs.StringVar(&r.DataDiskZone, "data-disk-zone", "", "Current disk zone. (required)")
	fs.StringVar(&r.SourceSnapshot, "source-snapshot", "", "Source disk snapshot to restore from. (optional) either source-snapshot or backup-id must be provided")
	fs.StringVar(&r.GroupSnapshot, "backup-id", "", "Backup ID of group snapshot to restore from. (optional) either source-snapshot or backup-id must be provided")
	fs.StringVar(&r.NewdiskName, "new-disk-name", "", "New disk name. (required) must be less than 63 characters long")
	fs.StringVar(&r.Project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.StringVar(&r.NewDiskType, "new-disk-type", "", "Type of the new disk. (optional) Default: same type as disk passed in data-disk-name.")
	fs.StringVar(&r.HanaSidAdm, "hana-sidadm", "", "HANA sidadm username. (optional) Default: <sid>adm")
	fs.StringVar(&r.labelsOnDetachedDisk, "labels-on-detached-disk", "", "Labels to be appended to detached disks. (optional) Default: empty. Accepts comma separated key-value pairs, like \"key1=value1,key2=value2\"")
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
	// Help and version will return before the args are parsed.
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:    r.Name(),
		Help:    r.help,
		Version: r.version,
		Fs:      f,
		IIOTE:   r.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		return subcommands.ExitFailure
	}
	r.timeSeriesCreator = mc
	return r.restoreHandler(ctx, gce.NewGCEClient, onetime.NewComputeService, cp, hanabackup.CheckDataDir, hanabackup.CheckLogDir)
}

// validateParameters validates the parameters passed to the restore subcommand.
func (r *Restorer) validateParameters(os string, cp *ipb.CloudProperties) error {
	if r.SkipDBSnapshotForChangeDiskType {
		log.Logger.Debug("Skip DB Snapshot for Change Disk Type")
		return nil
	}
	if os == "windows" {
		return fmt.Errorf("disk snapshot restore is only supported on Linux systems")
	}

	// Checking if sufficient arguments are passed for either group snapshot or single snapshot.
	// Only SID is required for restoring from groupSnapshot.
	// DataDiskName and NewdiskNames are fetched and respectively created
	// from individual snapshots mapped to groupSnapshot.
	restoreFromGroupSnapshot := !(r.Sid == "" || r.GroupSnapshot == "")
	// TODO: Enable auto discovery for restore
	restoreFromSingleSnapshot := !(r.Sid == "" || r.DataDiskName == "" || r.DataDiskZone == "" || r.SourceSnapshot == "" || r.NewdiskName == "")

	if restoreFromGroupSnapshot == true && restoreFromSingleSnapshot == true {
		return fmt.Errorf("either source-snapshot or backup-id must be provided, not both. Usage: %s", r.Usage())
	} else if restoreFromGroupSnapshot == false && restoreFromSingleSnapshot == false {
		return fmt.Errorf("required arguments not passed. Usage: %s", r.Usage())
	}
	if len(r.NewdiskName) > 63 {
		return fmt.Errorf("the new-disk-name is longer than 63 chars which is not supported, please provide a shorter name")
	}

	if r.Project == "" {
		r.Project = cp.GetProjectId()
	}
	if r.HanaSidAdm == "" {
		r.HanaSidAdm = strings.ToLower(r.Sid) + "adm"
	}

	if restoreFromGroupSnapshot {
		r.isGroupSnapshot = true
	}

	log.Logger.Debug("Parameter validation successful.")
	log.Logger.Infof("List of parameters to be used: %+v", r)

	return nil
}

// restoreHandler is the main handler for the restore subcommand.
func (r *Restorer) restoreHandler(ctx context.Context, gceServiceCreator gceServiceFunc, computeServiceCreator computeServiceFunc, cp *ipb.CloudProperties, checkDataDir getDataPaths, checkLogDir getLogPaths) subcommands.ExitStatus {
	var err error
	if err = r.validateParameters(runtime.GOOS, cp); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}

	r.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: Failed to create GCE service", err)
		return subcommands.ExitFailure
	}

	log.CtxLogger(ctx).Infow("Starting HANA disk snapshot restore", "sid", r.Sid)
	usagemetrics.Action(usagemetrics.HANADiskRestore)

	if r.computeService, err = computeServiceCreator(ctx); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: Failed to create compute service,", err)
		return subcommands.ExitFailure
	}

	if r.isGroupSnapshot {
		if err := r.readDiskMapping(ctx, cp); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "ERROR: Failed to read disks backing /hana/data,", err)
			return subcommands.ExitFailure
		}
	}

	if err := r.checkPreConditions(ctx, cp, checkDataDir, checkLogDir); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: Pre-restore check failed,", err)
		return subcommands.ExitFailure
	}
	if !r.SkipDBSnapshotForChangeDiskType {
		if err := r.prepare(ctx, cp, hanabackup.WaitForIndexServerToStopWithRetry, commandlineexecutor.ExecuteCommand); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore prepare failed,", err)
			return subcommands.ExitFailure
		}
	} else {
		if err := r.prepareForHANAChangeDiskType(ctx, cp); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore prepare failed,", err)
			return subcommands.ExitFailure
		}
	}
	workflowStartTime = time.Now()
	if !r.isGroupSnapshot {
		if err := r.diskRestore(ctx, cp); err != nil {
			return subcommands.ExitFailure
		}
	} else {
		if err := r.groupRestore(ctx, cp); err != nil {
			return subcommands.ExitFailure
		}
	}
	workflowDur := time.Since(workflowStartTime)
	defer r.sendDurationToCloudMonitoring(ctx, metricPrefix+r.Name()+"/totaltime", workflowDur, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	onetime.LogMessageToFileAndConsole(ctx, "SUCCESS: HANA restore from disk snapshot successful. Please refer https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/disk-snapshot-backup-recovery#recover_to_specific_point-in-time for next steps.")
	if r.labelsOnDetachedDisk != "" {
		if !r.isGroupSnapshot {
			if err := r.appendLabelsToDetachedDisk(ctx, r.DataDiskName); err != nil {
				return subcommands.ExitFailure
			}
		} else {
			for _, d := range r.disks {
				if err := r.appendLabelsToDetachedDisk(ctx, d.DiskName); err != nil {
					return subcommands.ExitFailure
				}
			}
		}
	}
	return subcommands.ExitSuccess
}

// prepare stops HANA, unmounts data directory and detaches old data disk.
func (r *Restorer) prepare(ctx context.Context, cp *ipb.CloudProperties, waitForIndexServerStop waitForIndexServerToStopWithRetry, exec commandlineexecutor.Execute) error {
	mountPath, err := hanabackup.ReadDataDirMountPath(ctx, r.baseDataPath, exec)
	if err != nil {
		return fmt.Errorf("failed to read data directory mount path: %v", err)
	}
	if err := hanabackup.StopHANA(ctx, r.ForceStopHANA, r.HanaSidAdm, r.Sid, exec); err != nil {
		return fmt.Errorf("failed to stop HANA: %v", err)
	}
	if err := waitForIndexServerStop(ctx, r.HanaSidAdm, exec); err != nil {
		return fmt.Errorf("hdbindexserver process still running after HANA is stopped: %v", err)
	}

	if err := hanabackup.Unmount(ctx, mountPath, exec); err != nil {
		return fmt.Errorf("failed to unmount data directory: %v", err)
	}

	if !r.isGroupSnapshot {
		if err := r.gceService.DetachDisk(ctx, cp, r.Project, r.DataDiskZone, r.DataDiskName, r.DataDiskDeviceName); err != nil {
			// If detach fails, rescan the volume groups to ensure the directories are mounted.
			hanabackup.RescanVolumeGroups(ctx)
			return fmt.Errorf("failed to detach old data disk: %v", err)
		}
	} else {
		disksDetached := []*ipb.Disk{}
		for _, d := range r.disks {
			if err := r.gceService.DetachDisk(ctx, cp, r.Project, r.DataDiskZone, d.DiskName, d.DeviceName); err != nil {
				log.CtxLogger(ctx).Error("failed to detach old data disk: %v", err)
				// Reattaching detached disks.
				for _, disk := range disksDetached {
					if err := r.gceService.AttachDisk(ctx, disk.DiskName, cp, r.Project, r.DataDiskZone); err != nil {
						return fmt.Errorf("failed to attach old data disk that was detached earlier: %v", err)
					}
				}

				// If detach fails, rescan the volume groups to ensure the directories are mounted.
				hanabackup.RescanVolumeGroups(ctx)
				return fmt.Errorf("failed to detach old data disk: %v", err)
			}
			disksDetached = append(disksDetached, d)
		}
	}

	log.CtxLogger(ctx).Info("HANA restore prepare succeeded.")
	return nil
}

func (r *Restorer) prepareForHANAChangeDiskType(ctx context.Context, cp *ipb.CloudProperties) error {
	mountPath, err := hanabackup.ReadDataDirMountPath(ctx, r.baseDataPath, commandlineexecutor.ExecuteCommand)
	if err != nil {
		return fmt.Errorf("failed to read data directory mount path: %v", err)
	}
	if err := hanabackup.Unmount(ctx, mountPath, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("failed to unmount data directory: %v", err)
	}
	if err := r.gceService.DetachDisk(ctx, cp, r.Project, r.DataDiskZone, r.DataDiskName, r.DataDiskDeviceName); err != nil {
		// If detach fails, rescan the volume groups to ensure the directories are mounted.
		hanabackup.RescanVolumeGroups(ctx)
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	log.CtxLogger(ctx).Info("HANA restore prepareForHANAChangeDiskType succeeded.")
	return nil
}

// diskRestore creates a new data disk restored from a single snapshot and attaches it to the instance.
func (r *Restorer) diskRestore(ctx context.Context, cp *ipb.CloudProperties) error {
	snapShotKey := ""
	if r.CSEKKeyFile != "" {
		usagemetrics.Action(usagemetrics.EncryptedSnapshotRestore)

		snapShotURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/snapshots/%s", r.Project, r.DataDiskZone, r.SourceSnapshot)
		key, err := hanabackup.ReadKey(r.CSEKKeyFile, snapShotURI, os.ReadFile)
		if err != nil {
			usagemetrics.Error(usagemetrics.EncryptedSnapshotRestoreFailure)
			return err
		}
		snapShotKey = key
	}

	if err := r.restoreFromSnapshot(ctx, cp, snapShotKey, r.NewdiskName, r.SourceSnapshot); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from snapshot failed,", err)
		r.gceService.AttachDisk(ctx, r.DataDiskName, cp, r.Project, r.DataDiskZone)
		hanabackup.RescanVolumeGroups(ctx)
		return err
	}

	hanabackup.RescanVolumeGroups(ctx)
	log.CtxLogger(ctx).Info("HANA restore from snapshot succeeded.")
	return nil
}

// groupRestore creates several new HANA data disks from snapshots belonging to given group snapshot and attaches them to the instance.
func (r *Restorer) groupRestore(ctx context.Context, cp *ipb.CloudProperties) error {
	snapShotKey := ""
	if r.CSEKKeyFile != "" {
		usagemetrics.Action(usagemetrics.EncryptedSnapshotRestore)

		snapShotURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/snapshots/%s", r.Project, r.DataDiskZone, r.GroupSnapshot)
		key, err := hanabackup.ReadKey(r.CSEKKeyFile, snapShotURI, os.ReadFile)
		if err != nil {
			usagemetrics.Error(usagemetrics.EncryptedSnapshotRestoreFailure)
			return err
		}
		snapShotKey = key
	}

	if err := r.restoreFromGroupSnapshot(ctx, cp, snapShotKey); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from group snapshot failed,", err)
		for _, d := range r.disks {
			r.gceService.AttachDisk(ctx, d.DiskName, cp, r.Project, r.DataDiskZone)
		}
		hanabackup.RescanVolumeGroups(ctx)
		return err
	}

	hanabackup.RescanVolumeGroups(ctx)
	log.CtxLogger(ctx).Info("HANA restore from group snapshot succeeded.")
	return nil
}

// restoreFromSnapshot creates a new HANA data disk and attaches it to the instance.
func (r *Restorer) restoreFromSnapshot(ctx context.Context, cp *ipb.CloudProperties, snapshotKey, newDiskName, sourceSnapshot string) error {
	disk := &compute.Disk{
		Name:                        newDiskName,
		Type:                        r.NewDiskType,
		Zone:                        r.DataDiskZone,
		SourceSnapshot:              fmt.Sprintf("projects/%s/global/snapshots/%s", r.Project, sourceSnapshot),
		SourceSnapshotEncryptionKey: &compute.CustomerEncryptionKey{RsaEncryptedKey: snapshotKey},
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

	if r.computeService == nil {
		return fmt.Errorf("compute service is nil")
	}
	op, err := r.computeService.Disks.Insert(r.Project, r.DataDiskZone, disk).Do()
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from snapshot failed,", err)
		return fmt.Errorf("failed to insert new data disk: %v", err)
	}
	if err := r.gceService.WaitForDiskOpCompletionWithRetry(ctx, op, r.Project, r.DataDiskZone); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "insert data disk failed", err)
		return fmt.Errorf("insert data disk operation failed: %v", err)
	}

	log.Logger.Infow("Attaching new HANA disk", "diskName", r.NewdiskName)
	if err := r.gceService.AttachDisk(ctx, r.NewdiskName, cp, r.Project, r.DataDiskZone); err != nil {
		return fmt.Errorf("failed to attach new data disk to instance: %v", err)
	}

	_, ok, err := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), r.NewdiskName)
	if err != nil {
		return fmt.Errorf("failed to check if new disk %v is attached to the instance", r.NewdiskName)
	}
	if !ok {
		return fmt.Errorf("newly created disk %v is not attached to the instance", r.NewdiskName)
	}

	log.Logger.Info("New disk created from snapshot successfully attached to the instance.")
	return nil
}

// restoreFromGroupSnapshot creates several new HANA data disks from snapshots belonging
// to given group snapshot and attaches them to the instance.
func (r *Restorer) restoreFromGroupSnapshot(ctx context.Context, cp *ipb.CloudProperties, snapshotKey string) error {
	groupSnapshot, err := r.gceService.GetGroupSnapshotsService().Get(r.Project, r.GroupSnapshot).Do()
	if err != nil {
		return fmt.Errorf("failed to get group snapshot: %v", err)
	}

	for _, snapshot := range groupSnapshot.Snapshots {
		timestamp := time.Now().Unix()
		if err := r.restoreFromSnapshot(ctx, cp, snapshotKey, fmt.Sprintf("%s-%d", snapshot.SourceDisk, timestamp), snapshot.Name); err != nil {
			return err
		}
	}
	return nil
}

// checkPreConditions checks if the HANA data and log disks are on the same physical disk.
// Also verifies that the data disk is attached to the instance.
func (r *Restorer) checkPreConditions(ctx context.Context, cp *ipb.CloudProperties, checkDataDir getDataPaths, checkLogDir getLogPaths) error {
	var err error
	if r.baseDataPath, r.logicalDataPath, r.physicalDataPath, err = checkDataDir(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	if r.baseLogPath, r.logicalLogPath, r.physicalLogPath, err = checkLogDir(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Checking preconditions", "Data directory", r.baseDataPath, "Data file system",
		r.logicalDataPath, "Data physical volume", r.physicalDataPath, "Log directory", r.baseLogPath,
		"Log file system", r.logicalLogPath, "Log physical volume", r.physicalLogPath)

	if r.physicalDataPath == r.physicalLogPath {
		return fmt.Errorf("unsupported: HANA data and HANA log are on the same physical disk - %s", r.physicalDataPath)
	}

	// Verify the disk is attached to the instance.
	if !r.isGroupSnapshot {
		dev, ok, err := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), r.DataDiskName)
		if err != nil {
			return fmt.Errorf("failed to verify if disk %v is attached to the instance", r.DataDiskName)
		}
		if !ok {
			return fmt.Errorf("the disk data-disk-name=%v is not attached to the instance, please pass the current data disk name", r.DataDiskName)
		}
		r.DataDiskDeviceName = dev
	} else {
		for _, d := range r.disks {
			_, ok, err := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), d.GetDiskName())
			if err != nil {
				return fmt.Errorf("failed to verify if disk %v is attached to the instance", d.GetDiskName())
			}
			if !ok {
				return fmt.Errorf("the disk data-disk-name=%v is not attached to the instance", d.GetDiskName())
			}
		}
	}

	// Verify the snapshot is present.
	if !r.isGroupSnapshot {
		if _, err = r.computeService.Snapshots.Get(r.Project, r.SourceSnapshot).Do(); err != nil {
			return fmt.Errorf("failed to check if source-snapshot=%v is present: %v", r.SourceSnapshot, err)
		}
	} else {
		if _, err = r.gceService.GetGroupSnapshotsService().Get(r.Project, r.GroupSnapshot).Do(); err != nil {
			return fmt.Errorf("failed to check if group-snapshot=%v is present: %v", r.GroupSnapshot, err)
		}
	}

	if r.NewDiskType == "" {
		if !r.isGroupSnapshot {
			d, err := r.computeService.Disks.Get(r.Project, r.DataDiskZone, r.DataDiskName).Do()
			if err != nil {
				return fmt.Errorf("failed to read data disk type: %v", err)
			}
			r.NewDiskType = d.Type
			log.CtxLogger(ctx).Infow("New disk type will be same as the data-disk-name", "diskType", r.NewDiskType)
		} else {
			r.NewDiskType = r.disks[0].GetDeviceType()
			log.CtxLogger(ctx).Infow("New disk type will be same as the data-disk-name", "diskType", r.NewDiskType)
		}
	} else {
		r.NewDiskType = fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", r.Project, r.DataDiskZone, r.NewDiskType)
	}
	return nil
}

func (r *Restorer) sendDurationToCloudMonitoring(ctx context.Context, mtype string, dur time.Duration, bo *cloudmonitoring.BackOffIntervals, cp *ipb.CloudProperties) bool {
	if !r.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Sending HANA disk snapshot duration to cloud monitoring", "duration", dur)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildFloat64(timeseries.Params{
			CloudProp:    cp,
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

func (r *Restorer) readDiskMapping(ctx context.Context, cp *ipb.CloudProperties) error {
	var instanceProperties *ipb.InstanceProperties
	var err error

	instanceInfoReader := instanceinfo.New(&instanceinfo.PhysicalPathReader{OS: runtime.GOOS}, r.gceService)
	if _, instanceProperties, err = instanceInfoReader.ReadDiskMapping(ctx, &cpb.Configuration{CloudProperties: cp}); err != nil {
		return err
	}

	log.CtxLogger(ctx).Debugw("Reading disk mapping", "ip", instanceProperties)
	for _, d := range instanceProperties.GetDisks() {
		if strings.Contains(r.physicalDataPath, d.GetMapping()) {
			log.CtxLogger(ctx).Debugw("Found disk mapping", "physicalPath", r.physicalDataPath, "diskName", d.GetDiskName())
			r.disks = append(r.disks, d)
			r.DataDiskZone = cp.GetZone()
		}
	}
	return nil
}

// appendLabelsToDetachedDisk appends and sets labels to the detached disk.
func (r *Restorer) appendLabelsToDetachedDisk(ctx context.Context, diskName string) (err error) {
	var disk *compute.Disk
	if disk, err = r.computeService.Disks.Get(r.Project, r.DataDiskZone, diskName).Do(); err != nil {
		return fmt.Errorf("failed to get disk: %v", err)
	}
	labelFingerprint := disk.LabelFingerprint
	labels, err := r.appendLabels(disk.Labels)
	if err != nil {
		return err
	}

	setLabelRequest := &compute.ZoneSetLabelsRequest{
		LabelFingerprint: labelFingerprint,
		Labels:           labels,
	}

	op, err := r.computeService.Disks.SetLabels(r.Project, r.DataDiskZone, diskName, setLabelRequest).Do()
	if err != nil {
		return fmt.Errorf("failed to append labels on detached disk: %v", err)
	}
	if err = r.gceService.WaitForDiskOpCompletionWithRetry(ctx, op, r.Project, r.DataDiskZone); err != nil {
		return fmt.Errorf("failed to append labels on detached disk: %v", err)
	}
	return nil
}

func (r *Restorer) appendLabels(labels map[string]string) (map[string]string, error) {
	pairs := strings.Split(strings.ReplaceAll(r.labelsOnDetachedDisk, " ", ""), ",")

	for _, pair := range pairs {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed to parse labels on detached disk: %v", pair)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		labels[key] = value
	}

	return labels, nil
}
