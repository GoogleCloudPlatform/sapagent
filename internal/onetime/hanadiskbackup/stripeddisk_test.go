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
	compute "google.golang.org/api/compute/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
)

func TestRunWorkflowForInstantSnapshotGroups(t *testing.T) {
	tests := []struct {
		name           string
		s              *Snapshot
		run            queryFunc
		createSnapshot diskSnapshotFunc
		cp             *ipb.CloudProperties
		wantErr        error
	}{
		{
			name: "CheckValidDiskFailure",
			s: &Snapshot{
				disks: []string{"pd-1", "pd-2"},
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   false,
					DiskAttachedToInstanceErr:        cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "InvalidDisk",
			s: &Snapshot{
				disks: []string{"pd-1", "pd-2"},
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					DiskAttachedToInstanceErr:        nil,
					IsDiskAttached:                   false,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AbandonSnapshotFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateHANASnapshotFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateInstantSnapshotGroupFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
				cgPath:          "test-cg-failure",
			},
			createSnapshot: createDiskSnapshotFail,
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
				gceService:       &fake.TestGCE{IsDiskAttached: true},
				cgPath:           "test-cg-success",
			},
			createSnapshot: createDiskSnapshotSuccess,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ConvertISGToSSGFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
				},
				cgPath: "test-cg-success",
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MarkSnapshotAsSuccessfulFailure",
			s: &Snapshot{
				AbandonPrepared: true,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
				},
				computeService: &compute.Service{},
				cgPath:         "test-cg-success",
			},
			createSnapshot: createDiskSnapshotSuccess,
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
				disks:           []string{"pd-1", "pd-2"},
				AbandonPrepared: true,
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					CreationCompletionErr:            nil,
				},
				computeService: &compute.Service{},
				cgPath:         "test-cg-success",
			},
			createSnapshot: createDiskSnapshotSuccess,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			gotErr := tc.s.runWorkflowForInstantSnapshotGroups(ctx, tc.run, tc.createSnapshot, tc.cp)
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("runWorkflowForInstantSnapshotGroups(%v, %v, %v) returned diff (-want +got):\n%s", tc.run, tc.createSnapshot, tc.cp, diff)
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
			name: "ReadDiskKeyFile",
			s: &Snapshot{
				isg:         &ISG{},
				cgPath:      "test-snapshot-success",
				DiskKeyFile: "/test/disk/key.json",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FreezeFS",
			s: &Snapshot{
				isg:              &ISG{},
				cgPath:           "test-snapshot-success",
				FreezeFileSystem: true,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "emptyGCEService",
			s: &Snapshot{
				cgPath: "test-snapshot-success",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "createISGFailure",
			s: &Snapshot{
				isg:        &ISG{},
				cgPath:     "test-snapshot-failure",
				gceService: &fake.TestGCE{},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "createISGSuccess",
			s: &Snapshot{
				isg: &ISG{
					Disks: []*compute.AttachedDisk{},
				},
				cgPath:     "test-snapshot-success",
				gceService: &fake.TestGCE{},
			},
			want: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got := tc.s.createInstantSnapshotGroup(context.Background())
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("createInstantGroupSnapshot() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertISGtoSSG(t *testing.T) {
	tests := []struct {
		name           string
		s              *Snapshot
		cp             *ipb.CloudProperties
		createSnapshot diskSnapshotFunc
		wantErr        error
	}{
		{
			name:    "emptyGCEService",
			s:       &Snapshot{},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DescribeISGFailure",
			s: &Snapshot{
				gceService: &fake.TestGCE{},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "createBackupError",
			s: &Snapshot{
				isg:        &ISG{},
				gceService: &fake.TestGCE{CreationCompletionErr: nil},
			},
			cp:             defaultCloudProperties,
			createSnapshot: createDiskSnapshotFail,
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "freezeFS",
			s: &Snapshot{
				FreezeFileSystem: true,
				isg:              &ISG{},
				computeService:   &compute.Service{},
				gceService:       &fake.TestGCE{CreationCompletionErr: nil},
			},
			cp:             defaultCloudProperties,
			createSnapshot: createDiskSnapshotSuccess,
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &Snapshot{
				isg:            &ISG{},
				computeService: &compute.Service{},
				gceService:     &fake.TestGCE{CreationCompletionErr: nil},
			},
			cp:             defaultCloudProperties,
			createSnapshot: createDiskSnapshotSuccess,
			wantErr:        cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			gotErr := tc.s.convertISGtoSSG(ctx, tc.cp, tc.createSnapshot)
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("convertIS() returned diff (-want +got):\n%s", diff)
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
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.validateDisksBelongToCG(ctx)
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
			wantCG:  "my-region-my-cg",
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
			want:     "my-region-my-cg",
		},
		{
			name:     "Failure1",
			policies: []string{"https://www.googleapis.com/compute/my-region/resourcePolicies/my-cg"},
			want:     "",
		},
		{
			name:     "Failure2",
			policies: []string{"https://www.googleapis.com/invlaid/text/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my"},
			want:     "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := cgPath(test.policies)
			if got != test.want {
				t.Errorf("cgPath()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestCreateGroupBackupLabels(t *testing.T) {
	tests := []struct {
		name string
		s    *Snapshot
		want map[string]string
	}{
		{
			name: "DiskSnapshot",
			s: &Snapshot{
				cgPath:                "my-region-my-cg",
				Disk:                  "my-disk",
				provisionedIops:       10000,
				provisionedThroughput: 1000000000,
			},
			want: map[string]string{
				"goog-sapagent-provisioned-iops":       "10000",
				"goog-sapagent-provisioned-throughput": "1000000000",
			},
		},
		{
			name: "GroupSnapshot",
			s: &Snapshot{
				groupSnapshot: true,
				cgPath:        "my-region-my-cg",
				Disk:          "my-disk",
			},
			want: map[string]string{
				"goog-sapagent-version":   configuration.AgentVersion,
				"goog-sapagent-cgpath":    "my-region-my-cg",
				"goog-sapagent-disk-name": "my-disk",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got := tc.s.createGroupBackupLabels()
			opts := cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
				return key == "goog-sapagent-timestamp" || key == "goog-sapagent-sha224"
			})
			if !cmp.Equal(got, tc.want, opts) {
				t.Errorf("parseLabels() = %v, want %v", got, tc.want)
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
