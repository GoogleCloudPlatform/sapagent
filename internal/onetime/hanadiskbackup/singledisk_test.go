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
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"
)

func TestRunWorkflowForDiskSnapshot_Extensive(t *testing.T) {
	tests := []struct {
		name           string
		snapshot       Snapshot
		run            queryFunc
		createSnapshot diskSnapshotFunc
		exec           commandlineexecutor.Execute
		wantErr        error
	}{
		{
			name: "DiskAttachedFalse",
			snapshot: Snapshot{
				oteLogger:  defaultOTELogger,
				gceService: &fake.TestGCE{IsDiskAttached: false},
			},
			run:     func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) { return "", nil },
			exec:    testCommandExecute("", "", nil),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AbandonPreparedFailure",
			snapshot: Snapshot{
				oteLogger:       defaultOTELogger,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
				AbandonPrepared: true,
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.Contains(q, "UNSUCCESSFUL") {
					return "", errors.New("err")
				}
				return "stale-snapshot", nil
			},
			exec:    testCommandExecute("", "", nil),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateNewSnapshotFailure",
			snapshot: Snapshot{
				oteLogger:  defaultOTELogger,
				gceService: &fake.TestGCE{IsDiskAttached: true},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.Contains(q, "CREATE SNAPSHOT") {
					return "", errors.New("err")
				}
				return "", nil
			},
			exec:    testCommandExecute("", "", nil),
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateDiskSnapshotFailure",
			snapshot: Snapshot{
				oteLogger:      defaultOTELogger,
				gceService:     &fake.TestGCE{IsDiskAttached: true},
				computeService: &fakeComputeService{},
				DiskKeyFile:    "/test/fake/key",
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "snapshot-id", nil
			},
			createSnapshot: createDiskSnapshotFail,
			exec:           testCommandExecute("", "", nil),
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "UnFreezeXFSFailure",
			snapshot: Snapshot{
				oteLogger:        defaultOTELogger,
				gceService:       &fake.TestGCE{IsDiskAttached: true},
				computeService:   &fakeComputeService{},
				FreezeFileSystem: true,
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "snapshot-id", nil
			},
			createSnapshot: createDiskSnapshotSuccess,
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "xfs_freeze" && len(params.Args) > 0 && params.Args[0] == "-u" {
					return commandlineexecutor.Result{Error: errors.New("freeze err"), ExitCode: 1}
				}
				return commandlineexecutor.Result{Error: nil, ExitCode: 0}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UploadCompletionFailure",
			snapshot: Snapshot{
				oteLogger:      defaultOTELogger,
				gceService:     &fake.TestGCE{IsDiskAttached: true, UploadCompletionErr: errors.New("err")},
				computeService: &fakeComputeService{},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "snapshot-id", nil
			},
			createSnapshot: createDiskSnapshotSuccess,
			exec:           testCommandExecute("", "", nil),
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "SuccessNoConfirm",
			snapshot: Snapshot{
				oteLogger:      defaultOTELogger,
				gceService:     &fake.TestGCE{IsDiskAttached: true},
				computeService: &fakeComputeService{},
			},
			run: func() queryFunc {
				selectCalls := 0
				return func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
					if strings.Contains(q, "SELECT BACKUP_ID") {
						selectCalls++
						if selectCalls == 1 {
							return "", nil // For abandonPreparedSnapshot
						}
						return "snapshot-id", nil // For createNewHANASnapshot
					}
					return "snapshot-id", nil
				}
			}(),
			createSnapshot: createDiskSnapshotSuccess,
			exec:           testCommandExecute("", "", nil),
			wantErr:        nil,
		},
		{
			name: "FailureNoConfirmMarkAsSuccessful",
			snapshot: Snapshot{
				oteLogger:      defaultOTELogger,
				gceService:     &fake.TestGCE{IsDiskAttached: true},
				computeService: &fakeComputeService{},
			},
			run: func() queryFunc {
				selectCalls := 0
				return func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
					if strings.Contains(q, "SELECT BACKUP_ID") {
						selectCalls++
						if selectCalls == 1 {
							return "", nil
						}
						return "snapshot-id", nil
					}
					if strings.Contains(q, "SUCCESSFUL") {
						return "", errors.New("err")
					}
					return "snapshot-id", nil
				}
			}(),
			createSnapshot: createDiskSnapshotSuccess,
			exec:           testCommandExecute("", "", nil),
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "SuccessWithConfirm",
			snapshot: Snapshot{
				oteLogger:                      defaultOTELogger,
				gceService:                     &fake.TestGCE{IsDiskAttached: true},
				computeService:                 &fakeComputeService{},
				ConfirmDataSnapshotAfterCreate: true,
			},
			run: func() queryFunc {
				selectCalls := 0
				return func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
					if strings.Contains(q, "SELECT BACKUP_ID") {
						selectCalls++
						if selectCalls == 1 {
							return "", nil // For abandonPreparedSnapshot
						}
						return "snapshot-id", nil // For createNewHANASnapshot
					}
					return "snapshot-id", nil
				}
			}(),
			createSnapshot: createDiskSnapshotSuccess,
			exec:           testCommandExecute("", "", nil),
			wantErr:        nil,
		},
	}
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.snapshot.runWorkflowForDiskSnapshot(ctx, tc.run, tc.createSnapshot, tc.exec, defaultCloudProperties)
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("runWorkflowForDiskSnapshot() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
