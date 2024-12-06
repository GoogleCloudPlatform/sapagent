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
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

// diskRestore creates a new data disk restored from a single snapshot and attaches it to the instance.
func (r *Restorer) diskRestore(ctx context.Context, exec commandlineexecutor.Execute, cp *ipb.CloudProperties) error {
	snapShotKey := ""
	if r.CSEKKeyFile != "" {
		r.oteLogger.LogUsageAction(usagemetrics.EncryptedSnapshotRestore)

		snapShotURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/snapshots/%s", r.Project, r.DataDiskZone, r.SourceSnapshot)
		key, err := hanabackup.ReadKey(r.CSEKKeyFile, snapShotURI, os.ReadFile)
		if err != nil {
			r.oteLogger.LogUsageError(usagemetrics.EncryptedSnapshotRestoreFailure)
			return err
		}
		snapShotKey = key
	}

	if err := r.restoreFromSnapshot(ctx, exec, cp, snapShotKey, r.NewdiskName, r.SourceSnapshot); err != nil {
		r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from snapshot failed,", err)
		if attachErr := r.gceService.AttachDisk(ctx, r.DataDiskName, protostruct.ConvertCloudPropertiesToStruct(cp), r.Project, r.DataDiskZone); attachErr != nil {
			log.CtxLogger(ctx).Errorw("reattaching old disk failed", "err", attachErr)
		}
		hanabackup.RescanVolumeGroups(ctx)
		return err
	}

	dev, _, _ := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), r.NewdiskName)
	if r.DataDiskVG != "" {
		cps := protostruct.ConvertCloudPropertiesToStruct(cp)
		if err := r.renameLVM(ctx, exec, cp, dev, r.NewdiskName); err != nil {
			log.CtxLogger(ctx).Info("Removing newly attached restored disk")
			dev, _, _ := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), r.NewdiskName)
			if detachErr := r.gceService.DetachDisk(ctx, cps, r.Project, r.DataDiskZone, r.NewdiskName, dev); detachErr != nil {
				log.CtxLogger(ctx).Errorw("failed to detach newly attached restored disk", "err", detachErr)
			}
			if attachErr := r.gceService.AttachDisk(ctx, r.DataDiskName, cps, r.Project, r.DataDiskZone); attachErr != nil {
				log.CtxLogger(ctx).Errorw("failed to reattach old disk", "err", attachErr)
			}
			return err
		}
	}

	hanabackup.RescanVolumeGroups(ctx)
	log.CtxLogger(ctx).Info("HANA restore from snapshot succeeded.")
	return nil
}
