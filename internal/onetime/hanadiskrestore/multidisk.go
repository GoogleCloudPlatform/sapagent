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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/api/compute/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
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

	var err error
	if r.UseSnapshotGroupWorkflow {
		err = r.groupRestoreWithSGWorkflow(ctx)
	} else {
		err = r.restoreFromGroupSnapshot(ctx, commandlineexecutor.ExecuteCommand, cp, snapShotKey)
	}
	if err != nil {
		r.handleRestoreFailure(ctx, err)
		return err
	}

	hanabackup.RescanVolumeGroups(ctx, commandlineexecutor.ExecuteCommand)
	log.CtxLogger(ctx).Info("HANA restore from group snapshot succeeded.")
	return nil
}

func (r *Restorer) groupRestoreWithSGWorkflow(ctx context.Context) error {
	// Check if snapshot group exists
	sg, err := r.sgService.GetSG(ctx, r.Project, r.GroupSnapshot)
	if err != nil {
		return fmt.Errorf("failed to get snapshot group %s: %w", r.GroupSnapshot, err)
	}
	if sg == nil {
		return fmt.Errorf("snapshot group %s not found", r.GroupSnapshot)
	}

	if err := r.bulkInsertDisksFromSG(ctx); err != nil {
		return err
	}

	return r.attachAndConfigureDisks(ctx)
}

func (r *Restorer) bulkInsertDisksFromSG(ctx context.Context) error {
	// Create mew disks using bulk insert api from snapshot group
	sourceSnapshotGroupURI := fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/%s/global/snapshotGroups/%s", r.Project, r.GroupSnapshot)
	parts := strings.Split(r.NewDiskType, "/")
	diskTypeName := parts[len(parts)-1]
	diskTypeURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/diskTypes/%s", r.Project, r.DataDiskZone, diskTypeName)
	requestBody := map[string]any{
		"snapshotGroupParameters": map[string]string{
			"sourceSnapshotGroup": sourceSnapshotGroupURI,
			"type":                diskTypeURI,
		},
	}
	data, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal bulk insert request body: %w", err)
	}
	log.CtxLogger(ctx).Debugw("Bulk inserting disks from snapshot group", "requestBody", string(data))
	op, err := r.sgService.BulkInsertFromSG(ctx, r.Project, r.DataDiskZone, data)
	if err != nil {
		return fmt.Errorf("failed to bulk insert from snapshot group %s: %w", r.GroupSnapshot, err)
	}

	// Wait for bulk insert operation to complete
	log.CtxLogger(ctx).Debugw("Waiting for bulk insert operation to complete")
	if err = r.gceService.WaitForDiskOpCompletionWithRetry(ctx, op, r.Project, r.DataDiskZone); err != nil {
		return err
	}
	return nil
}

func (r *Restorer) attachAndConfigureDisks(ctx context.Context) error {
	// Creating a map of disk names to their respective instances.
	// This map is used to retrieve the instance name to which the original disk was attached.
	diskToInstanceMap := make(map[string]string)
	for _, d := range r.disks {
		diskToInstanceMap[d.disk.DiskName] = d.instanceName
	}

	for _, snapshotItem := range r.snapshotItems {
		// Get the new disk created using ListDisksFromSnapshot api request
		disks, err := r.sgService.ListDisksFromSnapshot(ctx, r.Project, r.DataDiskZone, snapshotItem.Name)
		if err != nil {
			return fmt.Errorf("failed to list disks from snapshot %s: %w", snapshotItem.Name, err)
		}

		if disks == nil || len(disks) == 0 {
			return fmt.Errorf("no disks found for snapshot %s", snapshotItem.Name)
		}

		// Initialize latestDisk with the first disk
		latestDisk := disks[0]
		// If there's more than one disk, iterate and find the latest
		if len(disks) > 1 {
			latestTimestamp, err := time.Parse(time.RFC3339, latestDisk.CreationTimestamp)
			if err != nil {
				return fmt.Errorf("failed to parse timestamp for initial disk %s: %w", latestDisk.Name, err)
			}

			for _, disk := range disks {
				currentTimestamp, err := time.Parse(time.RFC3339, disk.CreationTimestamp)
				if err != nil {
					continue // Skip this disk and try the next one
				}

				// If the current disk's timestamp is after the latestTimestamp found so far, update latestDisk
				if currentTimestamp.After(latestTimestamp) {
					latestDisk = disk
					latestTimestamp = currentTimestamp
				}
			}
		}

		newDisk := latestDisk

		sourceDiskURI := snapshotItem.SourceDisk
		if sourceDiskURI == "" {
			return fmt.Errorf("SourceDisk field is empty for snapshot item %s", snapshotItem.Name)
		}
		uriParts := strings.Split(sourceDiskURI, "/")
		sourceDiskName := uriParts[len(uriParts)-1]

		instanceName, ok := diskToInstanceMap[sourceDiskName]
		if !ok {
			return fmt.Errorf("could not find instance for original disk: %s", sourceDiskName)
		}

		log.CtxLogger(ctx).Debugw("Attaching disk to instance", "diskName", newDisk.Name, "instanceName", instanceName)
		if err := r.gceService.AttachDisk(ctx, newDisk.Name, instanceName, r.Project, r.DataDiskZone); err != nil {
			return fmt.Errorf("failed to attach new data disk %s to instance %s: %w", newDisk.Name, instanceName, err)
		}

		_, ok, err = r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, instanceName, newDisk.Name)
		if err != nil {
			return fmt.Errorf("failed to check if new disk %v is attached to the instance %s: %w", newDisk.Name, instanceName, err)
		}
		if !ok {
			return fmt.Errorf("newly created disk %v is not attached to the instance %s", newDisk.Name, instanceName)
		}
		time.Sleep(5 * time.Second)

		if err := r.modifyDiskInCG(ctx, newDisk.Name, true); err != nil {
			log.CtxLogger(ctx).Warnw("failed to add newly attached disk to consistency group", "disk", newDisk.Name)
		}
		log.CtxLogger(ctx).Infow("Disk added to consistency group", "diskName", newDisk.Name)
	}

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

	// Creating a map of disk names to their respective instances.
	diskToInstanceMap := make(map[string]string)
	for _, d := range r.disks {
		diskToInstanceMap[d.disk.DiskName] = d.instanceName
	}
	var lastDiskName string
	var numOfDisksRestored int
	for _, snapshot := range snapshotList.Items {
		if snapshot.Labels["goog-sapagent-isg"] == r.GroupSnapshot {
			timestamp := time.Now().Unix()
			newDiskName := truncateName(ctx, snapshot.Name, fmt.Sprintf("%d", timestamp))
			if r.NewDiskPrefix != "" {
				newDiskName = fmt.Sprintf("%s-%s", r.NewDiskPrefix, fmt.Sprintf("%d", numOfDisksRestored+1))
			}
			lastDiskName = newDiskName

			log.CtxLogger(ctx).Debugw("Restoring snapshot", "new Disk", newDiskName, "source disk", snapshot.Labels["goog-sapagent-disk-name"])
			instanceName := diskToInstanceMap[snapshot.Labels["goog-sapagent-disk-name"]]
			if err := r.restoreFromSnapshot(ctx, exec, instanceName, snapshotKey, newDiskName, snapshot.Name); err != nil {
				return err
			}
			if err := r.modifyDiskInCG(ctx, newDiskName, true); err != nil {
				log.CtxLogger(ctx).Warnw("failed to add newly attached disk to consistency group", "disk", newDiskName)
			}

			numOfDisksRestored++
		}
	}

	if numOfDisksRestored != len(r.disks) {
		return fmt.Errorf("required number of disks did not get restored, wanted: %v, got: %v", len(r.disks), numOfDisksRestored)
	}

	if !r.isScaleout {
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
				if detachErr := r.gceService.DetachDisk(ctx, cp.GetInstanceName(), r.Project, r.DataDiskZone, lastDiskName, dev); detachErr != nil {
					log.CtxLogger(ctx).Info("Failed to detach newly attached restored disk: %v", detachErr)
					return detachErr
				}
				return err
			}
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

		cg, err = r.readConsistencyGroup(ctx, d.disk.DiskName)
		if err != nil {
			return err
		}

		if r.cgName != "" && cg != r.cgName {
			return fmt.Errorf("all disks should belong to the same consistency group, however disk %s belongs to %s, while other disks %s belong to %s", d.disk.DiskName, cg, disksTraversed, r.cgName)
		}
		disksTraversed = append(disksTraversed, d.disk.DiskName)
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

	snapshotName := src
	snapshotNameMaxLength := maxLength - (len(suffix) + 1)
	if len(src) > snapshotNameMaxLength {
		snapshotName = src[:snapshotNameMaxLength]
	}

	return snapshotName + "-" + suffix
}

// multiDisksAttachedToInstance checks if all data backing disks are attached
// to their respective instance or not. These multidisks could be either
// scaleout or non-scaleout(striped).
func (r *Restorer) multiDisksAttachedToInstance(ctx context.Context, cp *ipb.CloudProperties, exec commandlineexecutor.Execute) (bool, error) {
	var err error
	if r.isScaleout, err = hanabackup.CheckTopology(ctx, exec, r.Sid); err != nil {
		return false, fmt.Errorf("failed to check topology: %w", err)
	} else if r.isScaleout {
		if len(r.SourceDisks) == 0 {
			return false, fmt.Errorf("cannot perform autodiscovery of disks for scaleout, please pass -source-disks parameter")
		}
		if err := r.scaleoutDisksAttachedToInstance(ctx, cp); err != nil {
			return false, fmt.Errorf("failed to verify if disks are attached to the instance: %w", err)
		}
	} else {
		if len(r.SourceDisks) > 0 {
			if err := r.scaleupDisksAttachedToInstance(ctx, cp); err != nil {
				return false, fmt.Errorf("failed to verify if disks are attached to the instance: %w", err)
			}
		}
	}
	return true, nil
}

// scaleupDisksAttachedToInstance checks if all the disks provided
// in the -source-disks parameter are attached to the instance or not.
func (r *Restorer) scaleupDisksAttachedToInstance(ctx context.Context, cp *ipb.CloudProperties) error {
	log.CtxLogger(ctx).Infow("Validating disks provided by the user are attached to the instance", "disks", r.SourceDisks)
	r.disks = []*multiDisks{}
	for _, disk := range strings.Split(r.SourceDisks, ",") {
		disk = strings.TrimSpace(disk)
		dev, ok, err := r.gceService.DiskAttachedToInstance(r.Project, cp.GetZone(), cp.GetInstanceName(), disk)
		if err != nil {
			log.CtxLogger(ctx).Errorf("failed to check if disk %v is attached to the instance: %v", disk, err)
			return err
		} else if !ok {
			log.CtxLogger(ctx).Errorf("disk %v is not attached to the instance", disk)
			return fmt.Errorf("disk %v is not attached to the instance", disk)
		}

		r.DataDiskZone = cp.GetZone()
		r.disks = append(r.disks, &multiDisks{
			disk: &ipb.Disk{
				DiskName:   disk,
				DeviceName: dev,
			},
			instanceName: cp.GetInstanceName(),
		})
	}

	return nil
}

// scaleoutDisksAttachedToInstance checks if all data backing disks are attached
// to their respective instance or not.
// For disks belonging to the current instance, it verifies if the disks provided in the -source-disks parameter are attached to the instance.
// For disks not belonging to the current instance, it verifies if the disks are attached to any instance.
func (r *Restorer) scaleoutDisksAttachedToInstance(ctx context.Context, cp *ipb.CloudProperties) error {
	currentNodeDisks := make(map[string]bool)
	for _, d := range r.disks {
		currentNodeDisks[d.disk.DiskName] = false
	}

	disks := strings.Split(r.SourceDisks, ",")
	for _, d := range disks {
		d = strings.TrimSpace(d)
		if _, ok := currentNodeDisks[d]; ok {
			_, ok, err := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, cp.GetInstanceName(), d)
			if err != nil {
				return fmt.Errorf("failed to verify if disk %v is attached to the instance: %v", d, err)
			}
			if !ok {
				return fmt.Errorf("disk %v is not attached to the instance", d)
			}

			delete(currentNodeDisks, d)
		} else {
			disk, err := r.gceService.GetDisk(r.Project, r.DataDiskZone, d)
			if err != nil {
				return fmt.Errorf("failed to get disk %v: %v", d, err)
			}

			if len(disk.Users) == 0 {
				return fmt.Errorf("disk %v is not attached to any instance", d)
			}
			parts := strings.Split(disk.Users[0], "/")
			instanceName := parts[len(parts)-1]
			log.CtxLogger(ctx).Debugw("Disk %v is attached to instance %v", d, instanceName)

			dev, _, _ := r.gceService.DiskAttachedToInstance(r.Project, r.DataDiskZone, instanceName, d)
			r.disks = append(r.disks, &multiDisks{
				disk: &ipb.Disk{
					DiskName:   d,
					DeviceName: dev,
				},
				instanceName: instanceName,
			})
		}
	}

	if len(currentNodeDisks) == 0 {
		return nil
	}

	leftoverDisks := []string{}
	for disk := range currentNodeDisks {
		leftoverDisks = append(leftoverDisks, disk)
	}
	return fmt.Errorf("-source-disks does not contain disks: [%s] that also back up /hana/data", strings.Join(leftoverDisks, ","))
}

// handleRestoreFailure handles the failure of the restore process.
// It reattaches the old disks to the instance and adds them to the consistency group.
func (r *Restorer) handleRestoreFailure(ctx context.Context, err error) {
	r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: HANA restore from group snapshot failed,", err)
	for _, d := range r.disks {
		if attachDiskErr := r.gceService.AttachDisk(ctx, d.disk.DiskName, d.instanceName, r.Project, r.DataDiskZone); attachDiskErr != nil {
			r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: Reattaching old disk failed,", attachDiskErr)
		} else {
			if modifyCGErr := r.modifyDiskInCG(ctx, d.disk.DiskName, true); modifyCGErr != nil {
				log.CtxLogger(ctx).Warnw("failed to add old disk to consistency group", "disk", d.disk.DiskName)
				r.oteLogger.LogErrorToFileAndConsole(ctx, "ERROR: Adding old disk to consistency group failed,", modifyCGErr)
			}
		}
	}

	hanabackup.RescanVolumeGroups(ctx, commandlineexecutor.ExecuteCommand)
	return
}
