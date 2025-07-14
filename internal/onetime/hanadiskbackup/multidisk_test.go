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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/instantsnapshotgroup"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"
)

// TODO: Replace mocks with real implementations using httptest
type mockISGService struct {
	newServiceErr error

	createISGError error

	listInstantSnapshotGroupsResp []instantsnapshotgroup.ISGItem
	listInstantSnapshotGroupsErr  error

	describeInstantSnapshotsResp []instantsnapshotgroup.ISItem
	describeInstantSnapshotsErr  error

	deleteISGErr error

	getResponseResp []byte
	getResponseErr  error

	waitForISGUploadCompletionWithRetryErr error
}

type mockSGService struct {
	newServiceErr                         error
	createSGErr                           error
	waitForSGUploadCompletionWithRetryErr error
	waitForSGCreationWithRetryErr         error
}

func (m *mockSGService) NewService() error {
	return m.newServiceErr
}

func (m *mockSGService) CreateSG(ctx context.Context, project string, data []byte) error {
	return m.createSGErr
}

func (m *mockSGService) WaitForSGUploadCompletionWithRetry(ctx context.Context, project, sgName string) error {
	return m.waitForSGUploadCompletionWithRetryErr
}

func (m *mockSGService) WaitForSGCreationWithRetry(ctx context.Context, project, sgName string) error {
	return m.waitForSGCreationWithRetryErr
}

func (m *mockISGService) CreateISG(ctx context.Context, project, zone string, data []byte) error {
	return m.createISGError
}

func (m *mockISGService) ListInstantSnapshotGroups(ctx context.Context, project, zone string) ([]instantsnapshotgroup.ISGItem, error) {
	return m.listInstantSnapshotGroupsResp, m.listInstantSnapshotGroupsErr
}

func (m *mockISGService) DescribeInstantSnapshots(ctx context.Context, project, zone, isgName string) ([]instantsnapshotgroup.ISItem, error) {
	return m.describeInstantSnapshotsResp, m.describeInstantSnapshotsErr
}

func (m *mockISGService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	return m.getResponseResp, m.getResponseErr
}

func (m *mockISGService) DeleteISG(ctx context.Context, project, zone, isgName string) error {
	return m.deleteISGErr
}

func (m *mockISGService) NewService() error {
	return m.newServiceErr
}

func (m *mockISGService) WaitForISGUploadCompletionWithRetry(ctx context.Context, baseURL string) error {
	return m.waitForISGUploadCompletionWithRetryErr
}

func TestDeleteStaleISGs(t *testing.T) {
	tests := []struct {
		name string
		s    *Snapshot
		want error
	}{
		{
			name: "ListInstantSnapshotGroupsFailure",
			s: &Snapshot{
				isgService: &mockISGService{
					listInstantSnapshotGroupsErr: cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DeleteISGFailure",
			s: &Snapshot{
				isgService: &mockISGService{
					listInstantSnapshotGroupsResp: []instantsnapshotgroup.ISGItem{
						{
							Name:   "test-isg",
							Status: "READY",
						},
						{
							Name:   "test-isg-1",
							Status: "READY",
						},
					},
					listInstantSnapshotGroupsErr: nil,
					deleteISGErr:                 cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DeleteISGSuccess",
			s: &Snapshot{
				isgService: &mockISGService{
					listInstantSnapshotGroupsResp: []instantsnapshotgroup.ISGItem{
						{
							Name: "test-isg",
						},
					},
					listInstantSnapshotGroupsErr: nil,
					deleteISGErr:                 nil,
				},
			},
			want: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		tc.s.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			got := tc.s.deleteStaleISGs(ctx)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("deleteStaleISGs() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunWorkflowForInstantSnapshotGroups(t *testing.T) {
	tests := []struct {
		name    string
		s       *Snapshot
		run     queryFunc
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "DiskAttachErr",
			s: &Snapshot{
				disks: []string{"pd-1", "pd-2"},
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   false,
					DiskAttachedToInstanceErr:        cmpopts.AnyError,
				},
				isgService: &mockISGService{
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DiskAttachFalse",
			s: &Snapshot{
				disks: []string{"pd-1", "pd-2"},
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					DiskAttachedToInstanceErr:        nil,
					IsDiskAttached:                   false,
				},
				isgService: &mockISGService{
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AbandonSnapshotFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
				},
				isgService: &mockISGService{
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateHANASnapshotFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
				},
				isgService: &mockISGService{
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateInstantSnapshotGroupFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				DiskZone:        "invalid-zone",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
				},
				cgName: "test-cg-failure",
				isgService: &mockISGService{
					createISGError:                cmpopts.AnyError,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FreezeFS",
			s: &Snapshot{
				FreezeFileSystem: true,
				AbandonPrepared:  true,
				DiskZone:         "europe-west1-b",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
				},
				cgName: "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ConvertISGToSSFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				DiskZone:        "europe-west1-b",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
				},
				cgName: "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					describeInstantSnapshotsResp:  nil,
					describeInstantSnapshotsErr:   cmpopts.AnyError,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MarkSnapshotAsSuccessfulFailure",
			s: &Snapshot{
				AbandonPrepared:                true,
				DiskZone:                       "europe-west1-b",
				ConfirmDataSnapshotAfterCreate: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:     nil,
					CreationCompletionErr: nil,
				},
				computeService: &compute.Service{},
				cgName:         "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "test-isg",
						},
						{
							Name: "test-isg-1",
						},
					},
					describeInstantSnapshotsErr: nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DeleteISGFailure",
			s: &Snapshot{
				AbandonPrepared:                true,
				DiskZone:                       "europe-west1-b",
				ConfirmDataSnapshotAfterCreate: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:     nil,
					CreationCompletionErr: nil,
				},
				computeService: &compute.Service{},
				cgName:         "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "test-isg",
						},
						{
							Name: "test-isg-1",
						},
					},
					describeInstantSnapshotsErr: nil,
					deleteISGErr:                cmpopts.AnyError,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: nil,
		},
		{
			name: "SnapshotUploadFailure",
			s: &Snapshot{
				AbandonPrepared:                true,
				DiskZone:                       "europe-west1-b",
				ConfirmDataSnapshotAfterCreate: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:                      nil,
					CreationCompletionErr:                  nil,
					InstantSnapshotConversionCompletionErr: cmpopts.AnyError,
				},
				computeService: &compute.Service{},
				cgName:         "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "test-isg",
						},
					},
					describeInstantSnapshotsErr: nil,
					deleteISGErr:                nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MarkSnapshotFailureAfterDelete",
			s: &Snapshot{
				AbandonPrepared: true,
				DiskZone:        "europe-west1-b",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:                      nil,
					CreationCompletionErr:                  nil,
					InstantSnapshotConversionCompletionErr: nil,
				},
				computeService: &compute.Service{},
				cgName:         "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "test-isg",
						},
					},
					describeInstantSnapshotsErr: nil,
					deleteISGErr:                nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "RunWorkflowForInstantSnapshotGroupsSuccess",
			s: &Snapshot{
				AbandonPrepared: true,
				DiskZone:        "europe-west1-b",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "pd-1",
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:                      nil,
					CreationCompletionErr:                  nil,
					InstantSnapshotConversionCompletionErr: nil,
				},
				computeService: &compute.Service{},
				cgName:         "test-cg-success",
				isgService: &mockISGService{
					createISGError:                nil,
					listInstantSnapshotGroupsErr:  nil,
					listInstantSnapshotGroupsResp: nil,
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "test-isg",
						},
					},
					describeInstantSnapshotsErr: nil,
					deleteISGErr:                nil,
				},
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		tc.s.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.s.runWorkflowForInstantSnapshotGroups(ctx, tc.run, tc.cp)
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("runWorkflowForInstantSnapshotGroups(%v, %v) returned diff (-want +got):\n%s", tc.run, tc.cp, diff)
			}
		})
	}
}

func TestCreateInstantGroupSnapshot(t *testing.T) {
	tests := []struct {
		name string
		s    *Snapshot
		want error
	}{
		{
			name: "InvalidZone",
			s: &Snapshot{
				DiskZone: "invalid-zone",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ReadDiskKeyFile",
			s: &Snapshot{
				DiskZone:    "europe-west1-b",
				cgName:      "test-snapshot-success",
				DiskKeyFile: "/test/disk/key.json",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FreezeFS",
			s: &Snapshot{
				groupSnapshotName: "test-group-snapshot",
				DiskZone:          "europe-west1-b",
				cgName:            "test-snapshot-success",
				FreezeFileSystem:  true,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "createISGFailure",
			s: &Snapshot{
				DiskZone: "europe-west1-b",
				cgName:   "test-snapshot-failure",
				isgService: &mockISGService{
					createISGError: cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "createISGSuccess",
			s: &Snapshot{
				groupSnapshotName: "test-group-snapshot",
				DiskZone:          "europe-west1-b",
				cgName:            "test-snapshot-success",
				isgService: &mockISGService{
					createISGError: nil,
				},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		tc.s.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			got := tc.s.createInstantSnapshotGroup(context.Background())
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createInstantGroupSnapshot() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertISGtoSS(t *testing.T) {
	tests := []struct {
		name    string
		s       *Snapshot
		wantErr error
	}{
		{
			name: "DescribeISGFailure",
			s: &Snapshot{
				isgService: &mockISGService{
					describeInstantSnapshotsResp: nil,
					describeInstantSnapshotsErr:  cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "createGroupBackupFailure",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				gceService: &fake.TestGCE{
					CreateSnapshotOp:  nil,
					CreateSnapshotErr: cmpopts.AnyError,
				},
				isgService: &mockISGService{
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "instant-snapshot-1",
						},
						{
							Name: "instant-snapshot-2",
						},
					},
					describeInstantSnapshotsErr: nil,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				gceService: &fake.TestGCE{
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:     nil,
					CreationCompletionErr: nil,
				},
				isgService: &mockISGService{
					describeInstantSnapshotsResp: []instantsnapshotgroup.ISItem{
						{
							Name: "instant-snapshot-1",
						},
					},
					describeInstantSnapshotsErr: nil,
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, gotErr := tc.s.convertISGInstantSnapshots(context.Background(), defaultCloudProperties)
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("ConvertISGtoSS() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateGroupBackup(t *testing.T) {
	tests := []struct {
		name string
		s    *Snapshot
		want error
	}{
		{
			name: "DiskKeyFileProvided",
			s: &Snapshot{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						{
							Name:  "disk-name",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
					GetDiskErr: []error{nil},
				},
				DiskKeyFile: "/test/disk/key.json",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateSnapshotFailure",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				gceService: &fake.TestGCE{
					CreateSnapshotOp:  nil,
					CreateSnapshotErr: cmpopts.AnyError,
					GetDiskResp: []*compute.Disk{
						{
							Name:  "disk-name",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
					GetDiskErr: []error{nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "WaitForSnapshotCreationFailure",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				gceService: &fake.TestGCE{
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:     nil,
					CreationCompletionErr: cmpopts.AnyError,
					GetDiskResp: []*compute.Disk{
						{
							Name:  "disk-name",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
					GetDiskErr: []error{nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				gceService: &fake.TestGCE{
					CreateSnapshotOp: &compute.Operation{
						Status: "DONE",
					},
					CreateSnapshotErr:     nil,
					CreationCompletionErr: nil,
					GetDiskResp: []*compute.Disk{
						{
							Name:  "disk-name",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
					GetDiskErr: []error{nil},
				},
			},
			want: nil,
		},
	}

	var ssOps []*snapshotOp
	for _, tc := range tests {
		tc.s.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.s.createGroupBackup(context.Background(), instantsnapshotgroup.ISItem{}, &ssOps)

			if diff := cmp.Diff(tc.want, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createGroupBackup() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidateDisksBelongToCG(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		disks    []string
		want     error
	}{
		{
			name: "ReadDiskError",
			snapshot: Snapshot{
				disks: []string{"disk-name-1", "disk-name-2"},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{&compute.Disk{}},
					GetDiskErr:  []error{cmpopts.AnyError},
				},
			},
			disks: []string{"disk-name"},
			want:  cmpopts.AnyError,
		},
		{
			name: "NoCG",
			snapshot: Snapshot{
				disks: []string{"disk-name-1", "disk-name-2"},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{}, &compute.Disk{},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			disks: []string{"disk-name"},
			want:  cmpopts.AnyError,
		},
		{
			name: "DisksBelongToDifferentCGs",
			snapshot: Snapshot{
				disks: []string{"disk-name-1", "disk-name-2"},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
						},
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg-1"},
						},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			disks: []string{"disk-name"},
			want:  cmpopts.AnyError,
		},
		{
			name: "DisksBelongToSameCGs",
			snapshot: Snapshot{
				disks: []string{"disk-name-1", "disk-name-2"},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
						},
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
						},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			disks: []string{"disk-name"},
			want:  nil,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.validateDisksBelongToCG(ctx, test.snapshot.disks)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateDisksBelongToCG()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestReadConsistencyGroup(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		wantErr  error
		wantCG   string
	}{
		{
			name: "Success",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{
					GetInstanceResp: []*compute.Instance{{
						MachineType:       "test-machine-type",
						CpuPlatform:       "test-cpu-platform",
						CreationTimestamp: "test-creation-timestamp",
						Disks: []*compute.AttachedDisk{
							{
								Source:     "/some/path/disk-name",
								DeviceName: "disk-device-name",
								Type:       "PERSISTENT",
							},
						},
					}},
					GetDiskResp: []*compute.Disk{
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
						},
					},
					GetDiskErr:     []error{nil},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{
						{
							Items: []*compute.Disk{
								{
									Name:                  "disk-name",
									Type:                  "/some/path/device-type",
									ProvisionedIops:       100,
									ProvisionedThroughput: 1000,
								},
								{
									Name: "disk-device-name",
									Type: "/some/path/device-type",
								},
							},
						},
					},
					ListDisksErr: []error{nil},
				},
				Disk: "disk-name",
			},
			wantErr: nil,
			wantCG:  "my-cg",
		},
		{
			name: "Failure",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceErr: cmpopts.AnyError,
					GetInstanceResp: []*compute.Instance{{
						MachineType:       "test-machine-type",
						CpuPlatform:       "test-cpu-platform",
						CreationTimestamp: "test-creation-timestamp",
						Disks: []*compute.AttachedDisk{
							{
								Source:     "/some/path/disk-name",
								DeviceName: "disk-device-name",
								Type:       "PERSISTENT",
							},
						},
					}},
					GetDiskResp: []*compute.Disk{
						{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
						},
					},
					GetDiskErr:     []error{cmpopts.AnyError},
					GetInstanceErr: []error{cmpopts.AnyError},
				},
				Disk: "disk-name",
			},
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			gotCG, gotErr := test.snapshot.readConsistencyGroup(ctx, test.snapshot.Disk)
			if diff := cmp.Diff(gotErr, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("readConsistencyGroup()=%v, want=%v, diff=%v", gotErr, test.wantErr, diff)
			}
			if gotCG != test.wantCG {
				t.Errorf("readConsistencyGroup()=%v, want=%v", gotCG, test.wantCG)
			}
		})
	}
}

func TestCGPath(t *testing.T) {
	tests := []struct {
		name     string
		policies []string
		want     string
	}{
		{
			name:     "Success",
			policies: []string{"https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg"},
			want:     "my-cg",
		},
		{
			name:     "Failure1",
			policies: []string{"https://www.googleapis.com/compute/my-region/resourcePolicies/my-cg"},
			want:     "",
		},
		{
			name:     "Failure2",
			policies: []string{"https://www.googleapis.com/invalid/text/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my"},
			want:     "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cgPath(test.policies)
			if got != test.want {
				t.Errorf("cgName()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestCreateGroupBackupLabels(t *testing.T) {
	tests := []struct {
		name       string
		s          *Snapshot
		wantLabels map[string]string
		wantErr    error
	}{
		{
			name: "DiskSnapshot",
			s: &Snapshot{
				cgName:                "my-region-my-cg",
				Disk:                  "my-disk",
				provisionedIops:       10000,
				provisionedThroughput: 1000000000,
			},
			wantLabels: map[string]string{
				"goog-sapagent-provisioned-iops":       "10000",
				"goog-sapagent-provisioned-throughput": "1000000000",
			},
		},
		{
			name: "GroupSnapshotSuccess",
			s: &Snapshot{
				groupSnapshotName: "group-snapshot-name",
				groupSnapshot:     true,
				DiskZone:          "my-region-1",
				cgName:            "my-cg",
				Disk:              "my-disk",
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						{
							Name:  "disk-name",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
					GetDiskErr: []error{nil},
				},
			},
			wantLabels: map[string]string{
				"goog-sapagent-isg":       "group-snapshot-name",
				"goog-sapagent-version":   strings.ReplaceAll(configuration.AgentVersion, ".", "_"),
				"goog-sapagent-cgpath":    "my-region-my-cg",
				"goog-sapagent-disk-name": "my-disk",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.s.createGroupBackupLabels(tc.s.Disk)
			opts := cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
				return key == "goog-sapagent-timestamp" || key == "goog-sapagent-sha224"
			})
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createGroupBackupLabels() returned diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(got, tc.wantLabels, opts) {
				t.Errorf("parseLabels() = %v, want %v", got, tc.wantLabels)
			}
		})
	}
}

func TestGenerateSHA(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "Empty",
			labels: map[string]string{},
			want:   "d14a028c2a3a2bc9476102bb288234c415a2b01f828ea62ac5b3e42f",
		},
		{
			name: "SampleKeysWithoutGoogSapagent",
			labels: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			want: "d14a028c2a3a2bc9476102bb288234c415a2b01f828ea62ac5b3e42f",
		},
		{
			name: "SampleKeysWithGoogSapagent",
			labels: map[string]string{
				"goog-sapagent-cgpath":    "value1",
				"goog-sapagent-disk-path": "value2",
			},
			want: "1a9a2e346a5e7f18888bb9387e0ae3ddd2e4e7ffb2a1fe5cacb60e17",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := generateSHA(tc.labels)
			if got != tc.want {
				t.Errorf("generateSHA(%v) = %q, want: %q", tc.labels, got, tc.want)
			}
		})
	}
}

func TestCreateSnapshotGroupFromISG(t *testing.T) {
	tests := []struct {
		name    string
		s       *Snapshot
		wantErr error
	}{
		{
			name: "Success",
			s: &Snapshot{
				groupSnapshotName: "test-sg",
				Project:           "test-project",
				DiskZone:          "test-zone",
				Description:       "test-description",
				sgService: &mockSGService{
					createSGErr:                   nil,
					waitForSGCreationWithRetryErr: nil,
				},
				groupSnapshot: true,
			},
			wantErr: nil,
		},
		{
			name: "CreateSGFailure",
			s: &Snapshot{
				groupSnapshotName: "test-sg",
				Project:           "test-project",
				DiskZone:          "test-zone",
				Description:       "test-description",
				sgService: &mockSGService{
					createSGErr: cmpopts.AnyError,
				},
				groupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "WaitForSGCreationFailure",
			s: &Snapshot{
				groupSnapshotName: "test-sg",
				Project:           "test-project",
				DiskZone:          "test-zone",
				Description:       "test-description",
				sgService: &mockSGService{
					waitForSGCreationWithRetryErr: cmpopts.AnyError,
				},
				groupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.s.createSnapshotGroupFromISG(context.Background())
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createSnapshotGroupFromISG() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateSnapshotName(t *testing.T) {
	tests := []struct {
		name                string
		s                   *Snapshot
		instantSnapshotName string
		timestamp           string
		want                string
	}{
		{
			name: "withoutTruncate",
			s: &Snapshot{
				SnapshotType: "STANDARD",
			},
			instantSnapshotName: "instant-snapshot-name",
			timestamp:           "1725745441298732387",
			want:                "instant-snapshot-name-1725745441298732387-standard",
		},
		{
			name: "withTruncate",
			s: &Snapshot{
				SnapshotType: "ARCHIVE",
			},
			instantSnapshotName: "instant-snapshot-name-12345678912345678912345678912345678912345",
			timestamp:           "1725745441298732387",
			want:                "instant-snapshot-name-1234567891234-1725745441298732387-archive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.s.createSnapshotName(tc.instantSnapshotName, tc.timestamp)
			if got != tc.want {
				t.Errorf("CreateSnapshotName(%q) = %q, want: %q", tc.instantSnapshotName, got, tc.want)
			}
		})
	}
}
