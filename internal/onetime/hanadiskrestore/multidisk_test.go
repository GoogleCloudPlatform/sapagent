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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/fake"
)

var (
	scaleoutExec = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		if params.Executable == "grep" {
			return commandlineexecutor.Result{
				StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
				StdErr:   "",
				Error:    nil,
				ExitCode: 0,
			}
		}
		return commandlineexecutor.Result{
			StdOut: `
			17.11.2024 07:57:08
			GetSystemInstanceList
			OK
			hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
			scaleout, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
			scaleoutw1, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN`,
			StdErr:   "",
			Error:    nil,
			ExitCode: 0,
		}
	}

	scaleupExec = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		if params.Executable == "grep" {
			return commandlineexecutor.Result{
				StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
				StdErr:   "",
				Error:    nil,
				ExitCode: 0,
			}
		}
		return commandlineexecutor.Result{
			StdOut: `
			17.11.2024 07:57:08
			GetSystemInstanceList
			OK
			hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
			rb-scaleup, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN`,
			StdErr:   "",
			Error:    nil,
			ExitCode: 0,
		}
	}
)

func TestGroupRestore(t *testing.T) {
	tests := []struct {
		name string
		r    *Restorer
		want error
	}{
		{
			name: "CSEKKeyFilePresent",
			r: &Restorer{
				CSEKKeyFile: "/path/to/csek-key-file",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "attachDiskErr",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk",
						},
					},
				},
				gceService: &fake.TestGCE{
					SnapshotListErr: cmpopts.AnyError,
					AttachDiskErr:   cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "modifyCGErr",
			r: &Restorer{
				DataDiskZone: "invalid-zone",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk",
						},
					},
				},
				gceService: &fake.TestGCE{
					SnapshotListErr: cmpopts.AnyError,
					AttachDiskErr:   nil,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
							},
						},
					},
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceDeviceName: "test-device",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		tc.r.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.groupRestore(context.Background(), defaultCloudProperties)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("groupRestore() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRestoreFromGroupSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		r           *Restorer
		exec        commandlineexecutor.Execute
		cp          *ipb.CloudProperties
		snapshotKey string
		want        error
	}{
		{
			name: "GCEInvalid",
			r: &Restorer{
				gceService: nil,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SnapshotListErr",
			r: &Restorer{
				gceService: &fake.TestGCE{
					SnapshotListErr: cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "NumOfDisksRestoredNotMatching",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-2",
						},
					},
				},
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
							},
						},
					},
					SnapshotListErr: nil,
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        cmpopts.AnyError,
		},
		{
			name: "RestoreFromSnapshotErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				NewDiskPrefix: "test-prefix",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
								Labels: map[string]string{
									"goog-sapagent-isg": "test-group-snapshot",
								},
							},
						},
					},
					SnapshotListErr: nil,
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        cmpopts.AnyError,
		},
		{
			name: "DiskAttachedToInstanceErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
							},
						},
					},
					SnapshotListErr:           nil,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
					IsDiskAttached:            false,
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        cmpopts.AnyError,
		},
		{
			name: "LVMRenameErrDetachErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
							},
						},
					},
					SnapshotListErr:           nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            false,
					DetachDiskErr:             cmpopts.AnyError,
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        cmpopts.AnyError,
		},
		{
			name: "LVMRenameErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "test-disk-1",
							},
						},
					},
					SnapshotListErr:           nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            false,
					DetachDiskErr:             nil,
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.restoreFromGroupSnapshot(ctx, tc.exec, tc.cp, tc.snapshotKey)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("restoreFromGroupSnapshot() returned diff (-want +got):\n%s", diff)
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
			policies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"},
			want:     "my-cg",
		},
		{
			name:     "Failure1",
			policies: []string{"https://www.googleapis.com/compute/test-zone/resourcePolicies/my-cg"},
			want:     "",
		},
		{
			name:     "Failure2",
			policies: []string{"https://www.googleapis.com/invalid/text/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my"},
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

func TestReadConsistencyGroup(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		wantErr error
		wantCG  string
	}{
		{
			name: "Success",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"},
						},
					},
					GetDiskErr: []error{nil},
				},
				DataDiskName: "disk-name",
			},
			wantErr: nil,
			wantCG:  "my-cg",
		},
		{
			name: "Failure",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
					},
					GetDiskErr: []error{nil},
				},
				DataDiskName: "disk-name",
			},
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCG, gotErr := test.r.readConsistencyGroup(ctx, test.r.DataDiskName)
			if diff := cmp.Diff(gotErr, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("readConsistencyGroup()=%v, want=%v, diff=%v", gotErr, test.wantErr, diff)
			}
			if gotCG != test.wantCG {
				t.Errorf("readConsistencyGroup()=%v, want=%v", gotCG, test.wantCG)
			}
		})
	}
}

func TestValidateDisksBelongToCG(t *testing.T) {
	tests := []struct {
		name  string
		r     *Restorer
		disks []*ipb.Disk
		want  error
	}{
		{
			name: "ReadDiskError",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-1",
						},
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-2",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{}, &compute.Disk{},
					},
					GetDiskErr: []error{nil, cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "NoCG",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-1",
						},
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-2",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{}, &compute.Disk{},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DisksBelongToDifferentCGs",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-1",
						},
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-2",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"},
						},
						&compute.Disk{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg-1"},
						},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DisksBelongToSameCGs",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-1",
						},
						instanceName: "test-instance-1",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-name-2",
						},
						instanceName: "test-instance-2",
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"},
						},
						&compute.Disk{
							ResourcePolicies: []string{"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"},
						},
					},
					GetDiskErr: []error{nil, nil},
				},
			},
			want: nil,
		},
	}

	ctx := context.Background()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.validateDisksBelongToCG(ctx)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateDisksBelongToCG()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestModifyDiskInCGErrors(t *testing.T) {
	tests := []struct {
		name     string
		r        *Restorer
		diskName string
		add      bool
		wantErr  error
	}{
		{
			name: "invalidZone",
			r: &Restorer{
				DataDiskZone: "invalid-zone",
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "nilGCE",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "addErr",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				gceService: &fake.TestGCE{
					AddResourcePoliciesOp:  nil,
					AddResourcePoliciesErr: cmpopts.AnyError,
				},
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "removeErr",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				gceService: &fake.TestGCE{
					RemoveResourcePoliciesOp:  nil,
					RemoveResourcePoliciesErr: cmpopts.AnyError,
				},
			},
			diskName: "disk-name",
			add:      false,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "DiskOPErr",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				gceService: &fake.TestGCE{
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              cmpopts.AnyError,
				},
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				gceService: &fake.TestGCE{
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              nil,
				},
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.modifyDiskInCG(ctx, tc.diskName, tc.add)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("modifyDiskInCG()=%v, want=%v", gotErr, tc.wantErr)
			}
		})
	}
}

func TestMultiDisksAttachedToInstance(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		cp      *ipb.CloudProperties
		exec    commandlineexecutor.Execute
		want    bool
		wantErr error
	}{
		{
			name: "CheckTopologyErr",
			r:    &Restorer{},
			cp:   defaultCloudProperties,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: cmpopts.AnyError}
			},
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScaleoutEmptySourceDisks",
			r: &Restorer{
				SourceDisks: "",
			},
			cp:      defaultCloudProperties,
			exec:    scaleoutExec,
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScaleoutDisksAttachedFail",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
			},
			cp:      defaultCloudProperties,
			exec:    scaleoutExec,
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScaleoutSuccess",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,master-disk-2,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetDiskErr:                []error{nil},
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Name:  "worker-disk-1",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
				},
			},
			cp:      defaultCloudProperties,
			exec:    scaleoutExec,
			want:    true,
			wantErr: nil,
		},
		{
			name: "ScaleupAutodiscovery",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-2",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-3",
						},
						instanceName: "test-instance",
					},
				},
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			exec:    scaleupExec,
			want:    true,
			wantErr: nil,
		},
		{
			name: "ScaleupDisksAttachedFail",
			r: &Restorer{
				SourceDisks: "disk-5,disk-2",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-2",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-3",
						},
						instanceName: "test-instance",
					},
				},
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
			},
			cp:      defaultCloudProperties,
			exec:    scaleupExec,
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScaleupSuccess",
			r: &Restorer{
				SourceDisks: "disk-1,disk-2,disk-3",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-2",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "disk-3",
						},
						instanceName: "test-instance",
					},
				},
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			exec:    scaleupExec,
			want:    true,
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.r.multiDisksAttachedToInstance(ctx, tc.cp, tc.exec)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("multiDisksAttachedToInstance()=%v, want=%v, diff=%v", err, tc.wantErr, diff)
			}
			if got != tc.want {
				t.Errorf("multiDisksAttachedToInstance()=%v, want=%v", got, tc.want)
			}
		})
	}
}

func TestScaleupDisksAttachedToInstanceErrors(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "DiskAttachedToInstanceFailure",
			r: &Restorer{
				SourceDisks: "disk-1,disk-2,disk-3",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DiskAttachedToInstanceFalse",
			r: &Restorer{
				SourceDisks: "disk-1,disk-2,disk-3",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				SourceDisks: "disk-1,disk-2,disk-3",
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.scaleupDisksAttachedToInstance(ctx, tc.cp)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("scaleupDisksAttachedToInstance()=%v, want=%v, diff=%v", gotErr, tc.wantErr, diff)
			}
		})
	}
}

func TestScaleoutDisksAttachedToInstanceErrors(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "IsDiskAttachedFailure",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "IsDiskAttachedFalse",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GetDiskErr",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
					GetDiskErr:                []error{cmpopts.AnyError},
					GetDiskResp:               []*compute.Disk{nil},
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "WorkerNodeNoInstance",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
					GetDiskErr:                []error{nil},
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Name: "worker-disk-1",
						},
					},
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "IncompleteSourceDisks",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
					GetDiskErr:                []error{nil},
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Name:  "worker-disk-1",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-1",
						},
						instanceName: "test-instance",
					},
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "master-disk-2",
						},
						instanceName: "test-instance",
					},
				},
				SourceDisks: "master-disk-1,master-disk-2,worker-disk-1,worker-disk-2",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
					GetDiskErr:                []error{nil},
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Name:  "worker-disk-1",
							Users: []string{"https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/instances/my-instance"},
						},
					},
				},
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.scaleoutDisksAttachedToInstance(ctx, tc.cp)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("scaleoutDisksAttachedToInstance()=%v, want=%v, diff=%v", gotErr, tc.wantErr, diff)
			}
		})
	}
}
