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

// Package restore implements one time execution for HANA Disk based restore.
package restore

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

// Restorer has args for restore subcommands
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
}

// Name implements the subcommand interface for restore.
func (*Restorer) Name() string { return "restore" }

// Synopsis implements the subcommand interface for restore.
func (*Restorer) Synopsis() string { return "invoke HANA restore using persistent disk snapshot" }

// Usage implements the subcommand interface for restore.
func (*Restorer) Usage() string {
	return `Usage: restore -project=<project-name> -sid=<HANA-SID> -user=<user-name> -source-snapshot=<snapshot-name>
	-data-disk-name=<PD-name> -source-disk-zone=<PD-zone> -new-disk-type=<Type of the new PD disk>` + "\n"
}

// SetFlags implements the subcommand interface for restore.
func (r *Restorer) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.project, "project", "", "GCP project. (required)")
	fs.StringVar(&r.sid, "sid", "", "HANA SID. (required)")
	fs.StringVar(&r.user, "user", "", "HANA username. (required)")
	fs.StringVar(&r.dataDiskName, "data-disk-name", "", "PD name. (required)")
	fs.StringVar(&r.dataDiskZone, "data-disk-zone", "", "PD zone. (required)")
	fs.StringVar(&r.sourceSnapshot, "source-snapshot", "", "Source snapshot. (required)")
	fs.StringVar(&r.newDiskType, "new-disk-type", "", "Type of the new PD disk. (optional) Default: type of disk passed in data-disk-name.")
	fs.BoolVar(&r.help, "h", false, "Displays help")
	fs.BoolVar(&r.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&r.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for restore.
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

func (r *Restorer) validateParameters(os string) error {
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot restore is only supported on Linux systems")
	case r.project == "" || r.sid == "" || r.user == "" || r.dataDiskName == "" || r.dataDiskZone == "" || r.sourceSnapshot == "":
		return fmt.Errorf("required arguments not passed. Usage: %s", r.Usage())
	}
	if r.newDiskType == "" {
		exp := backoff.NewExponentialBackOff()
		bo := backoff.WithMaxRetries(exp, 1)
		r.newDiskType = metadataserver.DiskTypeWithRetry(bo, r.dataDiskName)
	}
	if r.newDiskType == "" {
		return fmt.Errorf("could not read disk type, please pass -new-disk-type=<>")
	}
	log.Logger.Debug("Parameter validation successful.")
	return nil
}

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
		return subcommands.ExitFailure
	}
	log.Print("SUCCESS: HANA restore from persistent disk snapshot successful.")
	return subcommands.ExitSuccess
}

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
	// Detach old HANA data disk
	op, err := r.computeService.Instances.DetachDisk(r.project, r.dataDiskZone, r.cloudProps.GetInstanceName(), r.dataDiskName).Do()
	if err != nil {
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("detach data disk operation failed: %v", err)
	}
	log.CtxLogger(ctx).Info("HANA restore prepare succeeded.")
	return nil
}

func (r *Restorer) restoreFromSnapshot(ctx context.Context) error {
	newdiskName := fmt.Sprintf("%s-%s", r.cloudProps.GetInstanceName(), r.sourceSnapshot)
	disk := &compute.Disk{
		Name:           newdiskName,
		Type:           fmt.Sprintf("projects/%s/zones/%s/diskTypes/%s", r.project, r.dataDiskZone, r.newDiskType),
		Zone:           r.dataDiskZone,
		SourceSnapshot: fmt.Sprintf("projects/%s/global/snapshots/%s", r.project, r.sourceSnapshot),
	}
	op, err := r.computeService.Disks.Insert(r.project, r.dataDiskZone, disk).Do()
	if err != nil {
		return fmt.Errorf("failed to insert new data disk: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("insert data disk operation failed: %v", err)
	}

	attachDiskToVM := &compute.AttachedDisk{
		Source: fmt.Sprintf("projects/%s/zones/%s/disks/%s", r.project, r.dataDiskZone, newdiskName),
	}
	op, err = r.computeService.Instances.AttachDisk(r.project, r.dataDiskZone, r.cloudProps.GetInstanceName(), attachDiskToVM).Do()
	if err != nil {
		return fmt.Errorf("failed to attach new data disk to instance: %v", err)
	}
	if err := r.waitForCompletionWithRetry(ctx, op); err != nil {
		return fmt.Errorf("attach data disk operation failed: %v", err)
	}

	if err := r.rescanVolumeGroups(ctx); err != nil {
		return fmt.Errorf("failure rescanning volume groups, logical volumes: %v", err)
	}
	log.CtxLogger(ctx).Info("HANA restore from snapshot succeeded.")
	return nil
}

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

func (r *Restorer) checkLogDir(ctx context.Context) error {
	var err error
	if r.baseLogPath, err = r.parseBasePath(ctx, "basepath_logvolumes", commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Log volume base path", "path", r.baseDataPath)

	if r.logicalLogPath, err = r.parseLogicalPath(ctx, r.baseLogPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	if r.physicalLogPath, err = r.parsePhysicalPath(ctx, r.logicalLogPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	return nil
}

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

func (r *Restorer) stopHANA(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		User:        r.user,
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf("-c '/usr/sap/%s/*/HDB stop'", r.sid),
	})
	if result.Error != nil {
		return fmt.Errorf("failure stopping HANA, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("HANA stopped", "sid", r.sid)
	return nil
}

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

func (r *Restorer) unmount(ctx context.Context, path string, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "umount",
		ArgsToSplit: path,
	})
	if result.Error != nil {
		return fmt.Errorf("failure unmounting directory: %s, stderr: %s, err: %s", path, result.StdErr, result.Error)
	}
	return nil
}

func (r *Restorer) rescanVolumeGroups(ctx context.Context) error {
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/dmsetup",
		ArgsToSplit: "remove_all",
	})
	if result.Error != nil {
		return result.Error
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgscan",
		ArgsToSplit: "-v --mknodes",
	})
	if result.Error != nil {
		return result.Error
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgchange",
		ArgsToSplit: "-ay",
	})
	if result.Error != nil {
		return result.Error
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable: "/sbin/lvscan",
	})
	if result.Error != nil {
		return result.Error
	}
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "mount",
		ArgsToSplit: "-av",
	})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

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
