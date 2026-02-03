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
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func (s *Snapshot) runWorkflowForDiskSnapshot(ctx context.Context, run queryFunc, createSnapshot diskSnapshotFunc, cp *ipb.CloudProperties) (err error) {
	if err := s.isDiskAttachedToInstance(ctx, s.Disk, cp); err != nil {
		return err
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

	op, err := s.createDiskSnapshot(ctx, createSnapshot, cp.GetInstanceName())
	if s.FreezeFileSystem {
		if err := hanabackup.UnFreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "Error unfreezing XFS", err)
			return err
		}
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", s.SnapshotName, freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}

	if err != nil {
		log.CtxLogger(ctx).Errorw("Error creating disk snapshot", "error", err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	if s.ConfirmDataSnapshotAfterCreate {
		log.CtxLogger(ctx).Info("Marking HANA snapshot as successful after disk snapshot is created but not yet uploaded.")
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Waiting for disk snapshot to complete uploading.")
	if err := s.gceService.WaitForSnapshotUploadCompletionWithRetry(ctx, op, s.Project, s.DiskZone, s.SnapshotName); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading disk snapshot", "error", err)
		if s.ConfirmDataSnapshotAfterCreate {
			s.oteLogger.LogErrorToFileAndConsole(
				ctx, fmt.Sprintf("Error uploading disk snapshot, HANA snapshot %s is not successful", snapshotID), err,
			)
		}
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.CtxLogger(ctx).Info("Disk snapshot created, marking HANA snapshot as successful.")
	if !s.ConfirmDataSnapshotAfterCreate {
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}

	return nil
}
