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

// Package hanapdrestore implements one time execution for HANA Persistent Disk based restore workflow.
package hanapdrestore

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"flag"
	backoff "github.com/cenkalti/backoff/v4"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// computeServiceFunc provides testable replacement for compute.Service API
	computeServiceFunc func(context.Context) (*compute.Service, error)
)

// Restorer has args for hanapdrestore subcommands
type Restorer struct {
	project, sid, user, dataDiskName          string
	dataDiskZone, sourceSnapshot, newDiskType string
	cloudProps                                *ipb.CloudProperties
	computeService                            *compute.Service
	baseDataPath, baseLogPath                 string
	logicalDataPath, logicalLogPath           string
	physicalDataPath, physicalLogPath         string
	help, version                             bool
	logLevel                                  string
	forceStopHANA                             bool
	newdiskName                               string
}

// Name implements the subcommand interface for hanapdrestore.
func (*Restorer) Name() string { return "hanapdrestore" }

// Synopsis implements the subcommand interface for hanapdrestore.
func (*Restorer) Synopsis() string {
	return "invoke HANA hanapdrestore using worklfow to restore from persistent disk snapshot"
}

// Usage implements the subcommand interface for hanapdrestore.
func (*Restorer) Usage() string {
	return `Usage: hanapdrestore -sid=<HANA-sid> -source-snapshot=<snapshot-name>
	-data-disk-name=<PD-name> -data-disk-zone=<PD-zone> [-project=<project-name>]
	[-new-disk-type=<Type of the new PD disk>] [-user=<user-name>]
	[-h] [-v] [loglevel]=<debug|info|warn|error>` + "\n"
}

// SetFlags implements the subcommand interface for hanapdrestore.
func (r *Restorer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&r.dataDiskName, "data-disk-name", "", "Current PD name. (required)")
	fs.StringVar(&r.dataDiskZone, "data-disk-zone", "", "Current PD zone. (required)")
	fs.StringVar(&r.sourceSnapshot, "source-snapshot", "", "Source PD snapshot to restore from. (required)")
	fs.StringVar(&r.project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.StringVar(&r.user, "user", "", "HANA sidadm username. (optional) Default: <sid>adm")
	fs.StringVar(&r.newDiskType, "new-disk-type", "", "Type of the new PD disk. (optional) Default: same type as disk passed in data-disk-name.")
	fs.BoolVar(&r.forceStopHANA, "force-stop-hana", false, "Forcefully stop HANA using `HDB kill` before attempting restore.(optional) Default: false.")
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.BoolVar(&r.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&r.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for hanapdrestore.
func (r *Restorer) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if r.help {
		return onetime.HelpCommand(f)
	}
	if r.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	r.cloudProps, ok = args[2].(*ipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, r.Name(), log.StringLevelToZapcore(r.logLevel))
	return r.restoreHandler(ctx, onetime.NewComputeService)
}

// validateParameters validates the parameters passed to the restore subcommand.
func (r *Restorer) validateParameters(os string) error {
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot restore is only supported on Linux systems")
	case r.sid == "" || r.dataDiskName == "" || r.dataDiskZone == "" || r.sourceSnapshot == "":
		return fmt.Errorf("required arguments not passed. Usage: %s", r.Usage())
	}
	if r.project == "" {
		r.project = r.cloudProps.GetProjectId()
	}
	if r.newDiskType == "" {
		exp := backoff.NewExponentialBackOff()
		bo := backoff.WithMaxRetries(exp, 1)
		r.newDiskType = metadataserver.DiskTypeWithRetry(bo, r.dataDiskName)
	}
	if r.newDiskType == "" {
		return fmt.Errorf("could not read disk type, please pass -new-disk-type=<>")
	}
	if r.user == "" {
		r.user = strings.ToLower(r.sid) + "adm"
	}
	log.Logger.Debug("Parameter validation successful.")

	return nil
}

// restoreHandler is the main handler for the restore subcommand.
func (r *Restorer) restoreHandler(ctx context.Context, computeServiceCreator computeServiceFunc) subcommands.ExitStatus {
	var err error
	if err = r.validateParameters(runtime.GOOS); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}
	log.CtxLogger(ctx).Infow("Starting HANA disk snapshot restore", "sid", r.sid)
	onetime.ConfigureUsageMetricsForOTE(r.cloudProps, "", "")
	usagemetrics.Action(usagemetrics.HANADiskRestore)

	if r.computeService, err = computeServiceCreator(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create compute service,", err)
		return subcommands.ExitFailure
	}

	if err := r.checkPreConditions(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Pre-restore check failed,", err)
		return subcommands.ExitFailure
	}
	if err := r.prepare(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: HANA restore prepare failed,", err)
		return subcommands.ExitFailure
	}
	if err := r.restoreFromSnapshot(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: HANA restore from snapshot failed,", err)
		// If restore fails, attach the old disk, rescan the volumes and delete the new disk.
		r.attachDisk(ctx, r.dataDiskName)
		r.rescanVolumeGroups(ctx)
		r.deleteDisk(ctx, r.newdiskName)
		return subcommands.ExitFailure
	}
	log.Print("SUCCESS: HANA restore from persistent disk snapshot successful.")
	return subcommands.ExitSuccess
}

// prepare stops HANA, unmounts data directory and detaches old data disk.
func (r *Restorer) prepare(ctx context.Context) error {
	if err := r.stopHANA(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return fmt.Errorf("failed to stop HANA: %v", err)
	}
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

	log.CtxLogger(ctx).Info("HANA restore prepare succeeded.")
	return nil
}

// TODO: Move generic PD related functions from hanapdbackup.go and hanapdrestore.go to gce.go.
// detachDisk detaches the old HANA data disk from the instance.
func (r *Restorer) detachDisk(ctx context.Context) error {
	// Verify the disk is attached to the instance.
	deviceName, ok, err := r.isDiskAttachedToInstance(r.dataDiskName)
	if err != nil {
		return fmt.Errorf("failed to verify if disk %v is attached to the instance", r.dataDiskName)
	}
	if !ok {
		return fmt.Errorf("the disk data-disk-name=%v is not attached to the instance, please pass the current data disk name", r.dataDiskName)
	}

	// Detach old HANA data disk.
	log.Logger.Infow("Detatching old HANA PD disk", "diskName", r.dataDiskName, "deviceName", deviceName)
	op, err := r.computeService.Instances.DetachDisk(r.project, r.dataDiskZone, r.cloudProps.GetInstanceName(), deviceName).Do()
	if err != nil {
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("detach data disk operation failed: %v", err)
	}

	if _, ok, err = r.isDiskAttachedToInstance(r.dataDiskName); err != nil {
		return fmt.Errorf("failed to check if disk %v is still attached to the instance", r.dataDiskName)
	}
	if ok {
		return fmt.Errorf("Disk %v is still attached to the instance", r.dataDiskName)
	}
	return nil
}

// isDiskAttachedToInstance checks if the given disk is attached to the instance.
func (r *Restorer) isDiskAttachedToInstance(diskName string) (string, bool, error) {
	instance, err := r.computeService.Instances.Get(r.project, r.dataDiskZone, r.cloudProps.GetInstanceName()).Do()
	if err != nil {
		return "", false, fmt.Errorf("failed to get instance: %v", err)
	}
	for _, disk := range instance.Disks {
		if strings.Contains(disk.Source, diskName) {
			return disk.DeviceName, true, nil
		}
	}
	return "", false, nil
}

// restoreFromSnapshot creates a new HANA data disk and attaches it to the instance.
func (r *Restorer) restoreFromSnapshot(ctx context.Context) error {
	t := time.Now()
	r.newdiskName = fmt.Sprintf("%s-%d%02d%02d-%02d%02d%02d",
		r.dataDiskName, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())

	disk := &compute.Disk{
		Name:           r.newdiskName,
		Type:           fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", r.project, r.dataDiskZone, r.newDiskType),
		Zone:           r.dataDiskZone,
		SourceSnapshot: fmt.Sprintf("projects/%s/global/snapshots/%s", r.project, r.sourceSnapshot),
	}
	log.Logger.Infow("Inserting new HANA PD disk from source snapshot", "diskName", r.newdiskName, "sourceSnapshot", r.sourceSnapshot)
	op, err := r.computeService.Disks.Insert(r.project, r.dataDiskZone, disk).Do()
	if err != nil {
		return fmt.Errorf("failed to insert new data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("insert data disk operation failed: %v", err)
	}

	log.Logger.Infow("Attaching new HANA PD disk", "diskName", r.newdiskName)
	op, err = r.attachDisk(ctx, r.newdiskName)
	if err != nil {
		return fmt.Errorf("failed to attach new data disk to instance: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("attach data disk operation failed: %v", err)
	}

	_, ok, err := r.isDiskAttachedToInstance(r.newdiskName)
	if err != nil {
		return fmt.Errorf("failed to check if new disk %v is attached to the instance", r.newdiskName)
	}
	if !ok {
		return fmt.Errorf("newly created disk %v is not attached to the instance", r.newdiskName)
	}

	log.Logger.Info("New disk created from snapshot successfully attached to the instance.")

	r.rescanVolumeGroups(ctx)
	log.CtxLogger(ctx).Info("HANA restore from snapshot succeeded.")
	return nil
}

// deleteDisk deletes the disk with the given name.
func (r *Restorer) deleteDisk(ctx context.Context, diskName string) {
	log.CtxLogger(ctx).Infow("Deleting Persistent disk", "diskName", diskName)
	if err := r.computeService.Disks.Delete(r.project, r.dataDiskZone, diskName); err != nil {
		log.CtxLogger(ctx).Errorf("failed to delete Persistent disk: %v", err)
	}
}

// attachDisk attaches the disk with the given name to the instance.
func (r *Restorer) attachDisk(ctx context.Context, diskName string) (*compute.Operation, error) {
	log.CtxLogger(ctx).Infow("Attaching Persistent disk", "diskName", diskName)
	attachDiskToVM := &compute.AttachedDisk{
		DeviceName: r.dataDiskName, // Keep the original device name.
		Source:     fmt.Sprintf("projects/%s/zones/%s/disks/%s", r.project, r.dataDiskZone, diskName),
	}
	return r.computeService.Instances.AttachDisk(r.project, r.dataDiskZone, r.cloudProps.GetInstanceName(), attachDiskToVM).Do()
}

// checkPreConditions checks if the HANA data and log disks are on the same physical disk.
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
		Executable:  "/bin/sh",
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
		Executable:  "/bin/sh",
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
		Executable:  "/bin/sh",
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
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf(" -c '/sbin/lvdisplay -m %s | grep Stripes'", r.logicalDataPath),
	})
	if result.ExitCode == 0 {
		return fmt.Errorf("restore of striped HANA data disks are not currently supported, exiting")
	}
	return nil
}

// stopHANA stops the HANA instance.
func (r *Restorer) stopHANA(ctx context.Context, exec commandlineexecutor.Execute) error {
	var cmd string
	if r.forceStopHANA {
		log.CtxLogger(ctx).Infow("HANA force stopped", "sid", r.sid)
		cmd = fmt.Sprintf("-c '/usr/sap/%s/*/HDB kill'", r.sid)
	} else {
		log.CtxLogger(ctx).Infow("Stopping HANA", "sid", r.sid)
		cmd = fmt.Sprintf("-c '/usr/sap/%s/*/HDB stop'", r.sid)
	}
	result := exec(ctx, commandlineexecutor.Params{
		User:        r.user,
		Executable:  "/bin/sh",
		ArgsToSplit: cmd,
	})
	if result.Error != nil {
		return fmt.Errorf("failure stopping HANA, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("HANA stopped", "sid", r.sid)
	return nil
}

// readDataDirMountPath reads the data directory mount path.
func (r *Restorer) readDataDirMountPath(ctx context.Context, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
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
		Executable:  "umount",
		ArgsToSplit: "-f " + path,
	})
	if result.Error != nil {
		return fmt.Errorf("failure unmounting directory: %s, stderr: %s, err: %s", path, result.StdErr, result.Error)
	}
	return nil
}

// rescanVolumeGroups rescans all volume groups and mounts them.
func (r *Restorer) rescanVolumeGroups(ctx context.Context) error {
	log.CtxLogger(ctx).Infow("Rescanning volume groups", "sid", r.sid)
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
	tracker, err := zos.Wait(r.project, r.dataDiskZone, op.Name).Do()
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
// TODO: change timeout depending on PD limits
func (r *Restorer) waitForCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 10), ctx)
	return backoff.Retry(func() error { return r.waitForCompletion(op) }, bo)
}