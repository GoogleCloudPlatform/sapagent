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

	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func (s *Snapshot) markSnapshotAsSuccessful(ctx context.Context, run queryFunc, snapshotID string) error {
	if _, err := run(ctx, s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, s.SnapshotName)); err != nil {
		log.CtxLogger(ctx).Errorw("Error marking HANA snapshot as SUCCESSFUL")
		s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotDoneDBNotComplete)
		return err
	}
	return nil
}

func (s *Snapshot) abandonHANASnapshot(ctx context.Context, run queryFunc, snapshotID string) error {
	_, err := run(ctx, s.db, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`)
	return err
}

// createNewHANASnapshot creates a new HANA snapshot with the given name and return its ID.
func (s *Snapshot) createNewHANASnapshot(ctx context.Context, run queryFunc) (snapshotID string, err error) {
	log.Logger.Infow("Creating new HANA snapshot", "comment", s.SnapshotName)
	_, err = run(ctx, s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT '%s'", s.SnapshotName))
	if err != nil {
		return "", err
	}
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	if snapshotID, err = run(ctx, s.db, snapshotIDQuery); err != nil {
		return "", err
	}
	if snapshotID == "" {
		return "", fmt.Errorf("could not read ID of the newly created snapshot")
	}
	log.Logger.Infow("Snapshot created", "snapshotid", snapshotID, "comment", s.SnapshotName)
	return snapshotID, nil
}

func (s *Snapshot) abandonPreparedSnapshot(ctx context.Context, run queryFunc) error {
	// Read the already prepared snapshot.
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	snapshotID, err := run(ctx, s.db, snapshotIDQuery)
	if err != nil {
		return err
	}
	if snapshotID == "" {
		log.Logger.Info("No prepared snapshot found")
		return nil
	}

	log.Logger.Infow("Found prepared snapshot", "snapshotid", snapshotID)
	if !s.AbandonPrepared {
		return fmt.Errorf("a HANA data snapshot is already prepared or is in progress, rerun with <-abandon-prepared=true> to abandon this snapshot")
	}
	if err = s.abandonHANASnapshot(ctx, run, snapshotID); err != nil {
		return err
	}
	log.Logger.Info("Snapshot abandoned", "snapshotID", snapshotID)
	return nil
}
