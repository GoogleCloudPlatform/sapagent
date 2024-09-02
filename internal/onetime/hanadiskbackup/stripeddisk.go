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

package hanadiskbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func (s *Snapshot) runWorkflowForInstantSnapshotGroups(ctx context.Context, run queryFunc, createSnapshot diskSnapshotFunc, cp *ipb.CloudProperties) (err error) {
	for _, d := range s.disks {
		if err = s.isDiskAttachedToInstance(ctx, d, cp); err != nil {
			return err
		}
	}

	log.CtxLogger(ctx).Info("Start run HANA Disk based backup workflow")
	if err = s.abandonPreparedSnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	err = s.createInstantSnapshotGroup(ctx)
	if s.FreezeFileSystem {
		if err := hanabackup.UnFreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "error unfreezing XFS", err)
			return err
		}
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", s.groupSnapshotName, freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}

	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error creating instant snapshot group, HANA snapshot %s is not successful", snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	if err = s.convertISGtoSSG(ctx, cp, createSnapshot); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error converting instant snapshots to %s, HANA snapshot %s is not successful", strings.ToLower(s.SnapshotType), snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.CtxLogger(ctx).Info(fmt.Sprintf("Instant snapshot group and %s equivalents created, marking HANA snapshot as successful.", strings.ToLower(s.SnapshotType)))
	if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
		return err
	}

	return nil
}

func (s *Snapshot) createInstantSnapshotGroup(ctx context.Context) error {
	timestamp := time.Now().UTC().UnixMilli()
	s.groupSnapshotName = s.cgPath + fmt.Sprintf("-%d", timestamp)
	log.CtxLogger(ctx).Infow("Creating Instant snapshot group", "disks", s.disks, "disks zone", s.DiskZone, "groupSnapshotName", s.groupSnapshotName)

	if s.DiskKeyFile != "" {
		s.oteLogger.LogUsageAction(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			s.oteLogger.LogUsageError(usagemetrics.EncryptedDiskSnapshotFailure)
			return err
		}
		s.isg.EncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}

	dbFreezeStartTime = time.Now()
	if s.FreezeFileSystem {
		if err := hanabackup.FreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			return err
		}
	}

	if s.gceService == nil {
		return fmt.Errorf("gceService needed to convert Instant Snapshot Group")
	}
	return fmt.Errorf("GroupSnapshot not supported yet")

	// TODO: Update this when prod API is available.
}

func (s *Snapshot) convertISGtoSSG(ctx context.Context, cp *ipb.CloudProperties, createSnapshot diskSnapshotFunc) error {
	if s.gceService == nil {
		return fmt.Errorf("gceService needed to proceed")
	}
	return fmt.Errorf("GroupSnapshot not supported yet")
	// TODO: Update this when prod API is available.
}

// validateDisksBelongToCG validates that the disks belong to the same consistency group.
func (s *Snapshot) validateDisksBelongToCG(ctx context.Context) error {
	disksTraversed := []string{}
	for _, d := range s.disks {
		cg, err := s.readConsistencyGroup(ctx, d)
		if err != nil {
			return err
		}

		if s.cgPath != "" && cg != s.cgPath {
			return fmt.Errorf("all disks should belong to the same consistency group, however disk %s belongs to %s, while other disks %s belong to %s", d, cg, disksTraversed, s.cgPath)
		}
		disksTraversed = append(disksTraversed, d)
		s.cgPath = cg
	}

	return nil
}

// readConsistencyGroup reads the consistency group (CG) from the resource policies of the disk.
func (s *Snapshot) readConsistencyGroup(ctx context.Context, disk string) (string, error) {
	d, err := s.gceService.GetDisk(s.Project, s.DiskZone, disk)
	if err != nil {
		return "", err
	}
	if cgPath := cgPath(d.ResourcePolicies); cgPath != "" {
		log.CtxLogger(ctx).Infow("Found disk to conistency group mapping", "disk", s.Disk, "cg", cgPath)
		return cgPath, nil
	}
	return "", fmt.Errorf("failed to find consistency group for disk %v", disk)
}

// cgPath returns the name of the consistency group (CG) from the resource policies.
func cgPath(policies []string) string {
	// Example policy: https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg
	for _, policyLink := range policies {
		parts := strings.Split(policyLink, "/")
		if len(parts) >= 10 && parts[9] == "resourcePolicies" {
			return parts[8] + "-" + parts[10]
		}
	}
	return ""
}

// createGroupBackupLabels returns the labels to be added for the group snapshot.
func (s *Snapshot) createGroupBackupLabels() map[string]string {
	labels := map[string]string{}
	if !s.groupSnapshot {
		if s.provisionedIops != 0 {
			labels["goog-sapagent-provisioned-iops"] = strconv.FormatInt(s.provisionedIops, 10)
		}
		if s.provisionedThroughput != 0 {
			labels["goog-sapagent-provisioned-throughput"] = strconv.FormatInt(s.provisionedThroughput, 10)
		}
		return labels
	}
	labels["goog-sapagent-version"] = configuration.AgentVersion
	labels["goog-sapagent-cgpath"] = s.cgPath
	labels["goog-sapagent-disk-name"] = s.Disk
	labels["goog-sapagent-timestamp"] = strconv.FormatInt(time.Now().UTC().Unix(), 10)
	labels["goog-sapagent-sha224"] = generateSHA(labels)
	return labels
}

// generateSHA generates a SHA-224 hash of labels starting with "goog-sapagent".
func generateSHA(labels map[string]string) string {
	keys := []string{}
	for k := range labels {
		if strings.HasPrefix(k, "goog-sapagent") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var orderedString string
	for _, k := range keys {
		orderedString += k + labels[k]
	}

	hash := sha256.Sum224([]byte(orderedString))
	return hex.EncodeToString(hash[:])
}
