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

package hanadiskrestore

import (
	"context"
	"fmt"
	"os"

	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// groupRestore creates several new HANA data disks from snapshots belonging to given group snapshot and attaches them to the instance.
func (r *Restorer) groupRestore(ctx context.Context, cp *ipb.CloudProperties) error {
	snapShotKey := ""
	if r.CSEKKeyFile != "" {
		r.oteLogger.LogUsageAction(usagemetrics.EncryptedSnapshotRestore)

		snapShotURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/snapshots/%s", r.Project, r.DataDiskZone, r.GroupSnapshot)
		key, err := hanabackup.ReadKey(r.CSEKKeyFile, snapShotURI, os.ReadFile)
		if err != nil {
			r.oteLogger.LogUsageError(usagemetrics.EncryptedSnapshotRestoreFailure)
			return err
		}
		snapShotKey = key
	}

	if err := r.restoreFromGroupSnapshot(ctx, commandlineexecutor.ExecuteCommand, cp, snapShotKey); err != nil {
		r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from group snapshot failed,", err)
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

// restoreFromGroupSnapshot creates several new HANA data disks from snapshots belonging
// to given group snapshot and attaches them to the instance.
func (r *Restorer) restoreFromGroupSnapshot(ctx context.Context, exec commandlineexecutor.Execute, cp *ipb.CloudProperties, snapshotKey string) error {
	return fmt.Errorf("GroupSnapshot not supported yet")
	// TODO: Update this when prod API is available.
}
