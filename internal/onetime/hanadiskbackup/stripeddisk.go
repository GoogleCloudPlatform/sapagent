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
	"encoding/json"
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
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/instantsnapshotgroup"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func (s *Snapshot) runWorkflowForInstantSnapshotGroups(ctx context.Context, run queryFunc, cp *ipb.CloudProperties) (err error) {
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

	var ssOps []*snapshotOp
	if ssOps, err = s.convertISGInstantSnapshots(ctx, cp); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error converting instant snapshots to %s, HANA snapshot %s is not successful", strings.ToLower(s.SnapshotType), snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		if err := s.isgService.DeleteISG(ctx, s.Project, s.DiskZone, s.groupSnapshotName); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error deleting created instant snapshot group"), err)
		}
		return err
	}

	if s.ConfirmDataSnapshotAfterCreate {
		log.CtxLogger(ctx).Info("Marking HANA snapshot as successful after disk snapshots are created but not yet uploaded.")
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			if err := s.isgService.DeleteISG(ctx, s.Project, s.DiskZone, s.groupSnapshotName); err != nil {
				s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error deleting created instant snapshot group"), err)
			}
			return err
		}
	}

	s.oteLogger.LogMessageToFileAndConsole(ctx, "Waiting for disk snapshots to complete uploading.")
	for _, ssOp := range ssOps {
		if err := s.gceService.WaitForInstantSnapshotConversionCompletionWithRetry(ctx, ssOp.op, s.Project, s.DiskZone, ssOp.name); err != nil {
			log.CtxLogger(ctx).Errorw("Error uploading disk snapshot", "error", err)
			if s.ConfirmDataSnapshotAfterCreate {
				s.oteLogger.LogErrorToFileAndConsole(
					ctx, fmt.Sprintf("Error uploading disk snapshot, HANA snapshot %s is not successful", snapshotID), err,
				)
			}
			s.diskSnapshotFailureHandler(ctx, run, snapshotID)

			if err := s.isgService.DeleteISG(ctx, s.Project, s.DiskZone, s.groupSnapshotName); err != nil {
				s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error deleting created instant snapshot group"), err)
			}
			return err
		}
	}
	if err := s.isgService.DeleteISG(ctx, s.Project, s.DiskZone, s.groupSnapshotName); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error deleting instant snapshot group, HANA snapshot %s is not successful", snapshotID), err)
		return err
	}

	log.CtxLogger(ctx).Info(fmt.Sprintf("Instant snapshot group and %s equivalents created, marking HANA snapshot as successful.", strings.ToLower(s.SnapshotType)))
	if !s.ConfirmDataSnapshotAfterCreate {
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Snapshot) createInstantSnapshotGroup(ctx context.Context) error {
	if s.groupSnapshotName == "" {
		t := time.Now()
		s.groupSnapshotName = fmt.Sprintf("%s-%d%02d%02d-%02d%02d%02d",
			s.cgName, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
	log.CtxLogger(ctx).Infow("Creating Instant snapshot group", "disks", s.disks, "disks zone", s.DiskZone, "groupSnapshotName", s.groupSnapshotName)

	parts := strings.Split(s.DiskZone, "-")
	if len(parts) < 3 {
		return fmt.Errorf("invalid zone, cannot fetch region from it: %s", s.DiskZone)
	}
	region := strings.Join(parts[:len(parts)-1], "-")

	groupSnapshot := map[string]any{
		"name":                   s.groupSnapshotName,
		"sourceConsistencyGroup": fmt.Sprintf("projects/%s/regions/%s/resourcePolicies/%s", s.Project, region, s.cgName),
		"description":            s.Description,
	}

	if s.DiskKeyFile != "" {
		s.oteLogger.LogUsageAction(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			s.oteLogger.LogUsageError(usagemetrics.EncryptedDiskSnapshotFailure)
			return err
		}
		groupSnapshot["sourceDiskEncryptionKey"] = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}

	dbFreezeStartTime = time.Now()
	if s.FreezeFileSystem {
		if err := hanabackup.FreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			return err
		}
	}

	data, err := json.Marshal(groupSnapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal json, err: %w", err)
	}
	if err := s.isgService.CreateISG(ctx, s.Project, s.DiskZone, data); err != nil {
		return err
	}
	baseURL := fmt.Sprintf("https://www.googleapis.com/compute/alpha/projects/%s/zones/%s/instantSnapshotGroups/%s", s.Project, s.DiskZone, s.groupSnapshotName)
	if err := s.isgService.WaitForISGUploadCompletionWithRetry(ctx, baseURL); err != nil {
		return err
	}
	return nil
}

func (s *Snapshot) convertISGInstantSnapshots(ctx context.Context, cp *ipb.CloudProperties) ([]*snapshotOp, error) {
	log.CtxLogger(ctx).Info(fmt.Sprintf("Converting Instant Snapshot Group to %s snapshots", strings.ToLower(s.SnapshotType)))
	instantSnapshots, err := s.isgService.DescribeInstantSnapshots(ctx, s.Project, s.DiskZone, s.groupSnapshotName)
	if err != nil {
		return nil, err
	}

	errors := []error{}
	ssOps := []*snapshotOp{}
	for _, is := range instantSnapshots {
		if err := s.createGroupBackup(ctx, is, &ssOps); err != nil {
			errors = append(errors, err)
		}
	}
	if len(errors) == 0 {
		return ssOps, nil
	}

	for _, err := range errors {
		if err != nil {
			log.CtxLogger(ctx).Errorw(fmt.Sprintf("Error converting Instant Snapshot Group to %s snapshots", strings.ToLower(s.SnapshotType)), "error", err)
		}
	}
	return nil, fmt.Errorf("Error converting Instant Snapshot Group to %s snapshots, latest error: %w", strings.ToLower(s.SnapshotType), errors[0])
}

func (s *Snapshot) createGroupBackup(ctx context.Context, instantSnapshot instantsnapshotgroup.ISItem, ssOps *[]*snapshotOp) error {
	log.CtxLogger(ctx).Debugw(fmt.Sprintf("Converting instant snapshot to %s snapshot", strings.ToLower(s.SnapshotType)), "instantSnapshot", instantSnapshot)
	isName := instantSnapshot.Name
	timestamp := time.Now().UTC().UnixNano()
	snapshotName := s.createSnapshotName(isName, fmt.Sprintf("%d", timestamp))
	snapshot := &compute.Snapshot{
		Name:                  snapshotName,
		SourceInstantSnapshot: fmt.Sprintf("projects/%s/zones/%s/instantSnapshots/%s", s.Project, s.DiskZone, isName),
		Labels:                s.parseLabels(),
		Description:           s.Description,
		SnapshotType:          strings.ToUpper(s.SnapshotType),
	}

	// In case customer is taking a snapshot from an encrypted disk, the snapshot created from it also
	// needs to be encrypted. For simplicity we support the use case in which disk encryption and
	// snapshot encryption key are the same.
	if s.DiskKeyFile != "" {
		s.oteLogger.LogUsageAction(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			s.oteLogger.LogUsageError(usagemetrics.EncryptedDiskSnapshotFailure)
			return fmt.Errorf("failed to create %s snapshot for instant snapshot %s: %w", strings.ToLower(s.SnapshotType), isName, err)
		}
		snapshot.SourceDiskEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
		snapshot.SnapshotEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}

	log.CtxLogger(ctx).Debugw(fmt.Sprintf("Creating %s snapshot", strings.ToLower(s.SnapshotType)), "snapshot", snapshot)
	op, err := s.gceService.CreateSnapshot(ctx, s.Project, snapshot)
	if err != nil {
		return fmt.Errorf("failed to create %s snapshot for instant snapshot %s: %w", strings.ToLower(s.SnapshotType), isName, err)
	}

	if err := s.gceService.WaitForSnapshotCreationCompletionWithRetry(ctx, op, s.Project, s.DiskZone, snapshotName); err != nil {
		return fmt.Errorf("failed to create %s snapshot for instant snapshot %s: %w", strings.ToLower(s.SnapshotType), isName, err)
	}
	*ssOps = append(*ssOps, &snapshotOp{op: op, name: snapshotName})
	return nil
}

func (s *Snapshot) createSnapshotName(instantSnapshotName string, timestamp string) string {
	maxLength := 63
	suffix := fmt.Sprintf("-%s-%s", timestamp, strings.ToLower(s.SnapshotType))

	snapshotName := instantSnapshotName
	snapshotNameMaxLength := maxLength - len(suffix)
	if len(instantSnapshotName) > snapshotNameMaxLength {
		snapshotName = instantSnapshotName[:snapshotNameMaxLength]
	}

	snapshotName += suffix
	return snapshotName
}

// validateDisksBelongToCG validates that the disks belong to the same consistency group.
func (s *Snapshot) validateDisksBelongToCG(ctx context.Context) error {
	disksTraversed := []string{}
	for _, d := range s.disks {
		cg, err := s.readConsistencyGroup(ctx, d)
		if err != nil {
			return err
		}

		if s.cgName != "" && cg != s.cgName {
			return fmt.Errorf("all disks should belong to the same consistency group, however disk %s belongs to %s, while other disks %s belong to %s", d, cg, disksTraversed, s.cgName)
		}
		disksTraversed = append(disksTraversed, d)
		s.cgName = cg
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
			return parts[10]
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
	parts := strings.Split(s.DiskZone, "-")
	region := strings.Join(parts[:len(parts)-1], "-")

	labels["goog-sapagent-version"] = strings.ReplaceAll(configuration.AgentVersion, ".", "_")
	labels["goog-sapagent-isg"] = s.groupSnapshotName
	labels["goog-sapagent-cgpath"] = region + "-" + s.cgName
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
