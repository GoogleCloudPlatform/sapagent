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
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
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
			if attachDiskErr := r.gceService.AttachDisk(ctx, d.DiskName, cp, r.Project, r.DataDiskZone); attachDiskErr != nil {
				r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: Reattaching old disk failed,", attachDiskErr)
			} else {
				if modifyCGErr := r.modifyDiskInCG(ctx, d.DiskName, true); modifyCGErr != nil {
					log.CtxLogger(ctx).Warnw("failed to add old disk to consistency group", "disk", d.DiskName)
					r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: Adding old disk to consistency group failed,", modifyCGErr)
				}
			}
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
	if r.gceService == nil {
		return fmt.Errorf("gce service is nil")
	}
	snapshotList, err := r.gceService.ListSnapshots(ctx, r.Project)
	if err != nil {
		return err
	}

	var lastDiskName string
	var numOfDisksRestored int
	for _, snapshot := range snapshotList.Items {
		if snapshot.Labels["isg"] == r.GroupSnapshot {
			timestamp := time.Now().Unix()
			sourceDiskName := truncateName(ctx, snapshot.Name, fmt.Sprintf("%d", timestamp))
			lastDiskName = sourceDiskName

			if err := r.restoreFromSnapshot(ctx, exec, cp, snapshotKey, sourceDiskName, snapshot.Name); err != nil {
				return err
			}
			if err := r.modifyDiskInCG(ctx, sourceDiskName, true); err != nil {
				log.CtxLogger(ctx).Warnw("failed to add newly attached disk to consistency group", "disk", sourceDiskName)
			}

			numOfDisksRestored++
		}
	}

	if numOfDisksRestored != len(r.disks) {
		return fmt.Errorf("required number of disks did not get restored, wanted: %v, got: %v", len(r.disks), numOfDisksRestored)
	}

	dev, ok, err := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), lastDiskName)
	if err != nil {
		return fmt.Errorf("failed to check if the source-disk=%v is attached to the instance", lastDiskName)
	}
	if !ok {
		return fmt.Errorf("source-disk=%v is not attached to the instance", lastDiskName)
	}

	if r.DataDiskVG != "" {
		if err := r.renameLVM(ctx, exec, cp, dev, lastDiskName); err != nil {
			log.CtxLogger(ctx).Info("Removing newly attached restored disk")
			if detachErr := r.gceService.DetachDisk(ctx, cp, r.Project, r.DataDiskZone, lastDiskName, dev); detachErr != nil {
				log.CtxLogger(ctx).Info("Failed to detach newly attached restored disk: %v", detachErr)
				return detachErr
			}
			return err
		}
	}
	return nil
}

func (r *Restorer) modifyDiskInCG(ctx context.Context, diskName string, add bool) (err error) {
	var op *compute.Operation

	parts := strings.Split(r.DataDiskZone, "-")
	if len(parts) < 3 {
		return fmt.Errorf("invalid zone, cannot fetch region from it: %s", r.DataDiskZone)
	}
	region := strings.Join(parts[:len(parts)-1], "-")
	resourcePolicies := []string{fmt.Sprintf("projects/%s/regions/%s/resourcePolicies/%s", r.Project, region, r.cgName)}

	if r.gceService == nil {
		return fmt.Errorf("gce service is nil")
	}
	if add {
		op, err = r.gceService.AddResourcePolicies(ctx, r.Project, r.DataDiskZone, diskName, resourcePolicies)
		if err != nil {
			return err
		}
	} else {
		op, err = r.gceService.RemoveResourcePolicies(ctx, r.Project, r.DataDiskZone, diskName, resourcePolicies)
		if err != nil {
			return err
		}
	}
	if err = r.gceService.WaitForDiskOpCompletionWithRetry(ctx, op, r.Project, r.DataDiskZone); err != nil {
		return err
	}
	return nil
}

func (r *Restorer) validateDisksBelongToCG(ctx context.Context) error {
	disksTraversed := []string{}
	for _, d := range r.disks {
		var cg string
		var err error

		cg, err = r.readConsistencyGroup(ctx, d.DiskName)
		if err != nil {
			return err
		}

		if r.cgName != "" && cg != r.cgName {
			return fmt.Errorf("all disks should belong to the same consistency group, however disk %s belongs to %s, while other disks %s belong to %s", d, cg, disksTraversed, r.cgName)
		}
		disksTraversed = append(disksTraversed, d.DiskName)
		r.cgName = cg
	}

	return nil
}

func (r *Restorer) readConsistencyGroup(ctx context.Context, diskName string) (string, error) {
	d, err := r.gceService.GetDisk(r.Project, r.DataDiskZone, diskName)
	if err != nil {
		return "", err
	}
	if cgPath := cgPath(d.ResourcePolicies); cgPath != "" {
		log.CtxLogger(ctx).Infow("Found disk to conistency group mapping", "disk", d, "cg", cgPath)
		return cgPath, nil
	}
	return "", fmt.Errorf("failed to find consistency group for disk %v", diskName)
}

// cgPath returns the name of the consistency group (CG) from the resource policies.
func cgPath(policies []string) string {
	// Example policy: https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg
	for _, policyLink := range policies {
		parts := strings.Split(policyLink, "/")
		if len(parts) >= 10 && parts[9] == "resourcePolicies" {
			return parts[10]
		}
	}
	return ""
}

func truncateName(ctx context.Context, src, suffix string) string {
	const maxLength = 63

	standardSnapshotName := src
	snapshotNameMaxLength := maxLength - (len(suffix) + 1)
	if len(src) > snapshotNameMaxLength {
		standardSnapshotName = src[:snapshotNameMaxLength]
	}

	return standardSnapshotName + "-" + suffix
}
