/*
Copyright 2024 Google LLC

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

// Package hanachangedisktype implements one time execution mode for HANA change disk type workflow.
// This includes the following steps
// 1) Validate all parameters for the command usage
// 2) Stop HANA DB
// 3) Create Disk Snapshot
// 4) Detach the disk upon successful disk snapshot
// 5) Create the new disk using the created snapshot
// 6) Attach the newly created HANA Data disk
// 7) Initiate rescan of volume groups and logical volumes and mount the File System
package hanachangedisktype

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskrestore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// HanaChangeDiskType has args for changedisktype subcommands.
type HanaChangeDiskType struct {
	project, host, sid, hanaSidAdm                     string
	disk, diskZone                                     string
	newDiskType                                        string
	diskKeyFile, storageLocation                       string
	snapshotName, snapshotType, description            string
	abandonPrepared                                    bool
	forceStopHANA                                      bool
	skipDBSnapshotForChangeDiskType                    bool
	newdiskName                                        string
	provisionedIops, provisionedThroughput, diskSizeGb int64
	help                                               bool
	logLevel, logPath                                  string
	IIOTEParams                                        *onetime.InternallyInvokedOTE
}

// Name implements the subcommand interface for hanachangedisktype.
func (*HanaChangeDiskType) Name() string { return "hanachangedisktype" }

// Synopsis implements the subcommand interface for hanachangedisktype.
func (*HanaChangeDiskType) Synopsis() string { return "invoke HANA change disk type workflow." }

// Usage implements the subcommand interface for hanachangedisktype.
func (*HanaChangeDiskType) Usage() string {
	return `Usage: hanachangedisktype -sid=<HANA-sid> -hana_db_user=<HANA DB User>
	-source-disk=<disk-name> -source-disk-zone=<disk-zone> [-host=<hostname>] [-project=<project-name>]
	-new-disk-name=<name-less-than-63-chars>
	[-new-disk-type=<Type of the new disk>] [-force-stop-hana=<true|false>]
	[-password=<passwd> | -password-secret=<secret-name>]
	[-hana-sidadm=<hana-sid-user-name>] [-provisioned-iops=<Integer value between 10,000 and 120,000>]
	[-provisioned-throughput=<Integer value between 1 and 7,124>] [-disk-size-gb=<New disk size in GB>]
	[skip-db-snapshot-for-change-disk-type=<true|false>]
	[-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for changedisktype.
func (c *HanaChangeDiskType) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&c.hanaSidAdm, "hana-sidadm", "", "HANA sidadm username. (optional) Default: <sid>adm")
	fs.StringVar(&c.disk, "source-disk", "", "name of the disk from which you want to create a snapshot (required)")
	fs.StringVar(&c.diskZone, "source-disk-zone", "", "zone of the disk from which you want to create a snapshot. (required)")
	fs.StringVar(&c.host, "host", "localhost", "HANA host. (optional)")
	fs.StringVar(&c.project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.BoolVar(&c.skipDBSnapshotForChangeDiskType, "skip-db-snapshot-for-change-disk-type", false, "Skip DB snapshot for change disk type, (optional) Default: false)")
	fs.StringVar(&c.snapshotName, "snapshot-name", "", "Snapshot name override.(Optional - defaults to 'hana-sid-snapshot-yyyymmdd-hhmmss')")
	fs.StringVar(&c.description, "description", "", "Description of the new snapshot(optional)")
	fs.StringVar(&c.diskKeyFile, "source-disk-key-file", "", `Path to the customer-supplied encryption key of the source disk. (optional)\n (required if the source disk is protected by a customer-supplied encryption key.)`)
	fs.StringVar(&c.storageLocation, "storage-location", "", "Cloud Storage multi-region or the region where you want to store your snapshot. (optional) Default: nearby regional or multi-regional location automatically chosen.")
	fs.StringVar(&c.newdiskName, "new-disk-name", "", "New disk name. (required) must be less than 63 characters")
	fs.StringVar(&c.newDiskType, "new-disk-type", "", "Type of the new disk. (optional) Default: same type as disk passed in data-disk-name.")
	fs.BoolVar(&c.forceStopHANA, "force-stop-hana", false, "Forcefully stop HANA using `HDB kill` before attempting restore.(optional) Default: false.")
	fs.Int64Var(&c.diskSizeGb, "disk-size-gb", 0, "New disk size in GB, must not be less than the size of the source (optional)")
	fs.Int64Var(&c.provisionedIops, "provisioned-iops", 0, "Number of I/O operations per second that the disk can handle. (optional)")
	fs.Int64Var(&c.provisionedThroughput, "provisioned-throughput", 0, "Number of throughput mb per second that the disk can handle. (optional)")
	fs.StringVar(&c.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/hanachangedisktype.log")
	fs.BoolVar(&c.help, "h", false, "Displays help")
	fs.StringVar(&c.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for hanadiskbackup.
func (c *HanaChangeDiskType) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     c.Name(),
		Help:     c.help,
		LogLevel: c.logLevel,
		LogPath:  c.logPath,
		Fs:       f,
		IIOTE:    c.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	if err := c.validateParams(runtime.GOOS, cp); err != nil {
		log.Print(err.Error())
		return subcommands.ExitUsageError
	}
	return c.changeDiskTypeHandler(ctx, f, lp, cp)
}

func (c *HanaChangeDiskType) validateParams(os string, cp *ipb.CloudProperties) error {
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot is only supported on Linux systems")
	case c.sid == "" || c.disk == "" || c.diskZone == "":
		return fmt.Errorf("required arguments not passed. Usage:" + c.Usage())
	case c.newdiskName == "":
		return fmt.Errorf("required arguments not passed. Usage: %s", c.Usage())
	}
	if c.project == "" {
		c.project = cp.GetProjectId()
	}
	if len(c.newdiskName) > 63 {
		return fmt.Errorf("the new-disk-name is longer than 63 chars which is not supported, please provide a shorter name")
	}
	if c.snapshotName == "" {
		t := time.Now()
		c.snapshotName = fmt.Sprintf("snapshot-%s-%d%02d%02d-%02d%02d%02d",
			c.disk, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
	if c.description == "" {
		c.description = fmt.Sprintf("HANAChangeDiskType workflow created by Agent for SAP for HANA sid: %q", c.sid)
	}
	if c.hanaSidAdm == "" {
		c.hanaSidAdm = strings.ToLower(c.sid) + "adm"
	}
	log.Logger.Debug("Parameter validation successful.")
	return nil
}

func (c *HanaChangeDiskType) changeDiskTypeHandler(ctx context.Context, f *flag.FlagSet, lp log.Parameters, cp *ipb.CloudProperties) subcommands.ExitStatus {
	s := &hanadiskbackup.Snapshot{
		Project:         c.project,
		Host:            c.host,
		Sid:             c.sid,
		HanaSidAdm:      c.hanaSidAdm,
		Disk:            c.disk,
		DiskZone:        c.diskZone,
		DiskKeyFile:     c.diskKeyFile,
		StorageLocation: c.storageLocation,
		SnapshotName:    c.snapshotName,
		SnapshotType:    "STANDARD",
		Description:     c.description,
		IIOTEParams: &onetime.InternallyInvokedOTE{
			Lp:        lp,
			Cp:        cp,
			InvokedBy: c.Name(),
		},
		LogLevel:                        c.logLevel,
		SkipDBSnapshotForChangeDiskType: c.skipDBSnapshotForChangeDiskType,
	}
	onetime.LogMessageToFileAndConsole(ctx, "Starting with Snapshot workflow")
	exitStatus := s.Execute(ctx, f)
	if exitStatus != subcommands.ExitSuccess {
		log.CtxLogger(ctx).Errorf("Failed to execute snapshot: %v", exitStatus)
		return exitStatus
	}
	r := &hanadiskrestore.Restorer{
		Project:                         c.project,
		Sid:                             c.sid,
		HanaSidAdm:                      c.hanaSidAdm,
		NewDiskType:                     c.newDiskType,
		NewdiskName:                     c.newdiskName,
		DiskSizeGb:                      c.diskSizeGb,
		ProvisionedIops:                 c.provisionedIops,
		ProvisionedThroughput:           c.provisionedThroughput,
		SourceSnapshot:                  c.snapshotName,
		DataDiskName:                    c.disk,
		DataDiskZone:                    c.diskZone,
		LogLevel:                        c.logLevel,
		SkipDBSnapshotForChangeDiskType: c.skipDBSnapshotForChangeDiskType,
		IIOTEParams: &onetime.InternallyInvokedOTE{
			Lp:        lp,
			Cp:        cp,
			InvokedBy: c.Name(),
		},
	}
	exitStatus = r.Execute(ctx, f)
	if exitStatus != subcommands.ExitSuccess {
		log.CtxLogger(ctx).Errorf("Failed to execute restore: %v", exitStatus)
		return exitStatus
	}
	// TODO: Add delete snapshot step in the end of this workflow.
	return exitStatus
}
