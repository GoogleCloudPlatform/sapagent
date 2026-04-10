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
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/snapshotgroup"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
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
			scaleoutw1, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
			`,
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
			rb-scaleup, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
			`,
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
		exec commandlineexecutor.Execute
		want error
	}{
		{
			name: "CSEKKeyFilePresent",
			r: &Restorer{
				CSEKKeyFile: "/path/to/csek-key-file",
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 5,
					Error:    cmpopts.AnyError,
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
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
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
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
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
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
			exec: successExec,
			want: nil,
		},
		{
			name: "SuccessWithSGWorkflow",
			r: &Restorer{
				GroupSnapshot:            "test-group-snapshot",
				UseSnapshotGroupWorkflow: true,
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						nil,
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr:                       []error{nil, &googleapi.Error{Code: http.StatusNotFound}, nil, nil},
					DiskOpErr:                        nil,
					AttachDiskErr:                    nil,
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 100}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{Status: "DONE"}, Err: nil},
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
			},
			exec: successExec,
			want: nil,
		},
		{
			name: "RescanVolumeGroupsFails",
			r: &Restorer{
				GroupSnapshot:            "test-group-snapshot",
				UseSnapshotGroupWorkflow: true,
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						nil,
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr:                       []error{nil, &googleapi.Error{Code: http.StatusNotFound}, nil, nil},
					DiskOpErr:                        nil,
					AttachDiskErr:                    nil,
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 100}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{Status: "DONE"}, Err: nil},
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "/sbin/dmsetup" {
					return commandlineexecutor.Result{Error: cmpopts.AnyError}
				}
				return commandlineexecutor.Result{}
			},
			want: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		tc.r.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.groupRestore(t.Context(), tc.exec, defaultCloudProperties)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("groupRestore() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGroupRestoreWithISGWorkflow(t *testing.T) {
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
				NewDiskSuffix: "test-suffix",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{nil},
					GetDiskErr:  []error{&googleapi.Error{Code: http.StatusNotFound}},
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
			name: "RestoreFromSnapshotErrWithDiskNameLabel",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				NewDiskSuffix: "test-prefix",
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{nil},
					GetDiskErr:  []error{&googleapi.Error{Code: http.StatusNotFound}},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot-1",
								SourceDisk: "https://www.googleapis.com/compute/v1/projects/my-project/zones/my-zone/disks/test-disk-1",
								Labels: map[string]string{
									"goog-sapagent-isg":       "test-group-snapshot",
									"goog-sapagent-disk-name": "test-disk-1",
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
					GetDiskResp: []*compute.Disk{nil},
					GetDiskErr:  []error{&googleapi.Error{Code: http.StatusNotFound}},
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
			name: "LVMRenameErr",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				DataDiskVG:    "test-vg",
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{nil},
					GetDiskErr:  []error{&googleapi.Error{Code: http.StatusNotFound}},
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
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceErr:        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "test-device",
					DetachDiskErr:                    nil,
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

	ctx := t.Context()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.groupRestoreWithISGWorkflow(ctx, tc.exec, tc.cp, tc.snapshotKey)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("groupRestoreWithISGWorkflow() returned diff (-want +got):\n%s", diff)
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

	ctx := t.Context()
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

	ctx := t.Context()
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

	ctx := t.Context()

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

	ctx := t.Context()

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

	ctx := t.Context()

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

	ctx := t.Context()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.scaleoutDisksAttachedToInstance(ctx, tc.cp)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("scaleoutDisksAttachedToInstance()=%v, want=%v, diff=%v", gotErr, tc.wantErr, diff)
			}
		})
	}
}

func TestGroupRestoreWithSGWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		wantErr error
	}{
		{
			name: "GetSGErr",
			r: &Restorer{
				sgService: &fakeSGService{
					GetSGErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SGNotFound",
			r: &Restorer{
				sgService: &fakeSGService{},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "BulkInsertErr",
			r: &Restorer{
				sgService: &fakeSGService{
					GetSGResp:           &snapshotgroup.SGItem{},
					BulkInsertFromSGErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ListDiskErr",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return nil, cmpopts.AnyError
					},
				},
				gceService: &fake.TestGCE{
					DiskOpErr: nil,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoDiskFound",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return nil, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{{}},
					GetDiskErr:  []error{nil},
					DiskOpErr:   nil,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoInstanceName",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "source-disk-1"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
					},
					DiskOpErr: nil,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AttachDiskErr",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "source-disk-1"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
					},
					DiskOpErr:     nil,
					AttachDiskErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CouldNotFindInstance",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "random-disk"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
					},
					DiskOpErr:     nil,
					AttachDiskErr: nil,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DiskAttachedToInstanceErr",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "source-disk-1"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
					},
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DiskNotAttached",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "source-disk-1"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
					},
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            false,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				disks: []*multiDisks{
					{disk: &ipb.Disk{DiskName: "source-disk-1"}, instanceName: "instance-1"},
				},
				sgService: &fakeSGService{
					GetSGResp: &snapshotgroup.SGItem{},
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return []snapshotgroup.DiskItem{
							{Name: "new-disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
						}, nil
					},
				},
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{},
						nil,
						&compute.Disk{},
						&compute.Disk{},
					},
					GetDiskErr: []error{
						nil,
						&googleapi.Error{Code: http.StatusNotFound},
						nil,
						nil,
					},
					DiskOpErr:                        nil,
					AttachDiskErr:                    nil,
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 100}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{Status: "DONE"}, Err: nil},
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.r.oteLogger = onetime.CreateOTELogger(false)
			err := tc.r.groupRestoreWithSGWorkflow(t.Context(), commandlineexecutor.ExecuteCommand, defaultCloudProperties, "")
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("groupRestoreWithSGWorkflow() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFetchLatestDisk(t *testing.T) {
	ctx := t.Context()
	timeNow := time.Now()
	tests := []struct {
		name      string
		snapshot  snapshotgroup.SnapshotItem
		sgService SGInterface
		want      *snapshotgroup.DiskItem
		wantErr   error
	}{
		{
			name:     "ListDisksFails",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return nil, cmpopts.AnyError
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:     "NoDisksFound",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return nil, nil
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:     "EmptyDisksFound",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return []snapshotgroup.DiskItem{}, nil
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:     "OneDiskFound",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return []snapshotgroup.DiskItem{
						{Name: "disk-1", CreationTimestamp: timeNow.Format(time.RFC3339)},
					}, nil
				},
			},
			want:    &snapshotgroup.DiskItem{Name: "disk-1", CreationTimestamp: timeNow.Format(time.RFC3339)},
			wantErr: nil,
		},
		{
			name:     "MultipleDisksFound",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return []snapshotgroup.DiskItem{
						{Name: "disk-1", CreationTimestamp: timeNow.Add(-1 * time.Hour).Format(time.RFC3339)},
						{Name: "disk-2", CreationTimestamp: timeNow.Format(time.RFC3339)},
						{Name: "disk-3", CreationTimestamp: timeNow.Add(-2 * time.Hour).Format(time.RFC3339)},
					}, nil
				},
			},
			want:    &snapshotgroup.DiskItem{Name: "disk-2", CreationTimestamp: timeNow.Format(time.RFC3339)},
			wantErr: nil,
		},
		{
			name:     "MultipleDisksWithInvalidTimestamp",
			snapshot: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			sgService: &fakeSGService{
				ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
					return []snapshotgroup.DiskItem{
						{Name: "disk-1", CreationTimestamp: timeNow.Add(-1 * time.Hour).Format(time.RFC3339)},
						{Name: "disk-2", CreationTimestamp: "invalid-timestamp"},
						{Name: "disk-3", CreationTimestamp: timeNow.Format(time.RFC3339)},
					}, nil
				},
			},
			want:    &snapshotgroup.DiskItem{Name: "disk-3", CreationTimestamp: timeNow.Format(time.RFC3339)},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Restorer{sgService: tc.sgService}
			got, err := r.fetchLatestDisk(ctx, tc.snapshot)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("fetchLatestDisk(%v) returned diff (-want +got):\n%s", tc.snapshot, diff)
			}
			if tc.wantErr == nil {
				if diff := cmp.Diff(got, tc.want); diff != "" {
					t.Errorf("fetchLatestDisk(%v) returned diff (-want +got):\n%s", tc.snapshot, diff)
				}
			}
		})
	}
}

func TestRecreateDisk(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		name         string
		r            *Restorer
		snapshotItem snapshotgroup.SnapshotItem
		newDiskName  string
		instanceName string
		snapshotKey  string
		wantErr      error
	}{
		{
			name: "RestoreFromSnapshotError",
			r: &Restorer{
				gceService: &fake.TestGCE{},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Err: cmpopts.AnyError},
				},
				oteLogger: onetime.CreateOTELogger(false),
			},
			snapshotItem: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			newDiskName:  "disk-1",
			instanceName: "instance-1",
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				gceService: &fake.TestGCE{
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            true,
				},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 10}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{}, Err: nil},
				},
				oteLogger: onetime.CreateOTELogger(false),
			},
			snapshotItem: snapshotgroup.SnapshotItem{Name: "snapshot-1"},
			newDiskName:  "disk-1",
			instanceName: "instance-1",
			wantErr:      nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.recreateDisk(ctx, scaleupExec, tc.snapshotItem, tc.newDiskName, tc.instanceName, tc.snapshotKey)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("recreateDisk() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenameLVMForScaleup(t *testing.T) {
	tests := []struct {
		name      string
		r         *Restorer
		exec      commandlineexecutor.Execute
		cp        *ipb.CloudProperties
		newDiskPV string
		wantErr   error
	}{
		{
			name: "isScaleout",
			r: &Restorer{
				isScaleout: true,
			},
			wantErr: nil,
		},
		{
			name: "EmptyDataDiskVG",
			r: &Restorer{
				isScaleout: false,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
				},
				DataDiskVG: "",
			},
			cp:      defaultCloudProperties,
			wantErr: nil,
		},
		{
			name: "renameLVMErr",
			r: &Restorer{
				isScaleout: false,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
					GetInstanceResp: []*compute.Instance{{
						Disks: []*compute.AttachedDisk{
							{DeviceName: "dev", Source: "projects/p/zones/z/disks/d"},
						},
					}},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{{
						Items: []*compute.Disk{
							{Name: "d"},
						},
					}},
					ListDisksErr: []error{nil},
				},
				DataDiskVG: "vg",
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: cmpopts.AnyError}
			},
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				isScaleout: false,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
					GetInstanceResp: []*compute.Instance{{
						Disks: []*compute.AttachedDisk{
							{DeviceName: "dev", Source: "projects/p/zones/z/disks/d"},
						},
					}},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{{
						Items: []*compute.Disk{
							{Name: "d"},
						},
					}},
					ListDisksErr: []error{nil},
				},
				DataDiskVG: "vg",
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  vg lvm2 a--  500.00g 300.00g",
				}
			},
			cp:        defaultCloudProperties,
			wantErr:   nil,
			newDiskPV: "dev",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.renameLVMForScaleup(t.Context(), tc.exec, tc.cp, tc.newDiskPV)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("renameLVMForScaleup() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAttachAndConfigureDisks(t *testing.T) {
	ctx := t.Context()
	listDisksFn := func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
		return []snapshotgroup.DiskItem{
			{Name: "disk-1", CreationTimestamp: time.Now().Format(time.RFC3339)},
		}, nil
	}
	fakeCS := &fakeComputeService{
		GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 100}, Err: nil},
		InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{Status: "DONE"}, Err: nil},
	}
	tests := []struct {
		name    string
		r       *Restorer
		exec    commandlineexecutor.Execute
		cp      *ipb.CloudProperties
		wantErr bool
	}{
		{
			name: "FetchLatestDiskError",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: func(ctx context.Context, project, zone, snapshot string) ([]snapshotgroup.DiskItem, error) {
						return nil, cmpopts.AnyError
					},
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:  []error{nil},
					GetDiskResp: []*compute.Disk{{}},
				},
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "FetchOriginalDiskNameError",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:  []error{cmpopts.AnyError},
					GetDiskResp: []*compute.Disk{{}},
				},
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "NewDiskNamesSetRecreateDiskFails",
			r: &Restorer{
				NewDiskNames: "new-disk-1",
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{},
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Err: cmpopts.AnyError},
				},
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "NewDiskNamesSetDeleteOldDiskFails",
			r: &Restorer{
				NewDiskNames: "new-disk-1",
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				DataDiskZone: "us-central1-a",
				cgName:       "cg",
				gceService: &fake.TestGCE{
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{cmpopts.AnyError}, // This makes deleteOldDisk fail
					DiskOpErr:                        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: false,
		},
		{
			name: "NewDiskNamesSetSuccess",
			r: &Restorer{
				NewDiskNames: "new-disk-1",
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				DataDiskZone: "us-central1-a",
				cgName:       "cg",
				gceService: &fake.TestGCE{
					DeleteDiskResp:                   []*compute.Operation{&compute.Operation{Status: "DONE"}},
					DeleteDiskErr:                    []error{nil}, // deleteOldDisk succeeds
					DiskOpErr:                        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: false,
		},
		{
			name: "EmptyInstanceNameError",
			r: &Restorer{
				isScaleout: true,
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:  []error{nil},
					GetDiskResp: []*compute.Disk{{}},
				},
			},
			exec:    scaleoutExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "AttachDiskFails",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:    []error{nil},
					GetDiskResp:   []*compute.Disk{{}},
					AttachDiskErr: cmpopts.AnyError,
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "RecreateDiskError",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:    []error{nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:   []*compute.Disk{{}, nil},
					AttachDiskErr: cmpopts.AnyError,
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "DeleteOldDiskFails",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:                       []error{nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:                      []*compute.Disk{{}, nil},
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{cmpopts.AnyError},
					DiskOpErr:                        cmpopts.AnyError,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: false,
		},
		{
			name: "ModifyDiskInCGFails",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				DataDiskZone: "us-central1-a",
				gceService: &fake.TestGCE{
					GetDiskErr:                       []error{nil, nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:                      []*compute.Disk{{}, {}, nil},
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					DiskOpErr:                        nil,
					AddResourcePoliciesErr:           cmpopts.AnyError,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: fakeCS,
			},
			exec:    scaleupExec,
			cp:      defaultCloudProperties,
			wantErr: false,
		},
		{
			name: "RenameLVMError",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				DataDiskVG: "vg",
				gceService: &fake.TestGCE{
					GetDiskErr:                       []error{nil, nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:                      []*compute.Disk{{}, {}, nil},
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					DiskOpErr:                        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "dev",
					GetInstanceResp: []*compute.Instance{{
						Disks: []*compute.AttachedDisk{
							{DeviceName: "dev", Source: "projects/p/zones/z/disks/d"},
						},
					}},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{{
						Items: []*compute.Disk{
							{Name: "d"},
						},
					}},
					ListDisksErr: []error{nil},
				},
				computeService: fakeCS,
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: cmpopts.AnyError}
			},
			cp:      defaultCloudProperties,
			wantErr: true,
		},
		{
			name: "SuccessScaleup",
			r: &Restorer{
				snapshotItems: []snapshotgroup.SnapshotItem{
					{Name: "snapshot-1", SourceDisk: "source-disk-1"},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				DataDiskVG: "vg",
				gceService: &fake.TestGCE{
					GetDiskErr:                       []error{nil, nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:                      []*compute.Disk{{}, {}, nil},
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					DiskOpErr:                        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "dev",
					GetInstanceResp: []*compute.Instance{{
						Disks: []*compute.AttachedDisk{
							{DeviceName: "dev", Source: "projects/p/zones/z/disks/d"},
						},
					}},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{{
						Items: []*compute.Disk{
							{Name: "d"},
						},
					}},
					ListDisksErr: []error{nil},
				},
				computeService: fakeCS,
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  vg lvm2 a--  500.00g 300.00g",
				}
			},
			cp:      defaultCloudProperties,
			wantErr: false,
		},
		{
			name: "SuccessScaleout",
			r: &Restorer{
				isScaleout: true,
				snapshotItems: []snapshotgroup.SnapshotItem{
					{
						Name:       "snapshot-1",
						SourceDisk: "source-disk-1",
						Labels:     map[string]string{"goog-sapagent-instance-name": "instance-1"},
					},
				},
				sgService: &fakeSGService{
					ListDisksFromSnapshotFn: listDisksFn,
				},
				disks: []*multiDisks{
					&multiDisks{
						disk: &ipb.Disk{
							DiskName: "test-disk-1",
						},
					},
				},
				gceService: &fake.TestGCE{
					GetDiskErr:                       []error{nil, nil, &googleapi.Error{Code: http.StatusNotFound}},
					GetDiskResp:                      []*compute.Disk{{}, {}, nil},
					DeleteDiskResp:                   []*compute.Operation{nil},
					DeleteDiskErr:                    []error{nil},
					DiskOpErr:                        nil,
					IsDiskAttached:                   true,
					DiskAttachedToInstanceDeviceName: "dev",
				},
				computeService: fakeCS,
			},
			exec:    scaleoutExec,
			cp:      defaultCloudProperties,
			wantErr: false,
		},
	}
	for _, tc := range tests {
		tc.r.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.attachAndConfigureDisks(ctx, tc.exec, tc.cp, "snapshotKey")
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("attachAndConfigureDisks() got error %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestDeleteOldDisk(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		name    string
		r       *Restorer
		wantErr bool
	}{
		{
			name: "DeleteDiskError",
			r: &Restorer{
				gceService: &fake.TestGCE{
					DeleteDiskResp: []*compute.Operation{nil},
					DeleteDiskErr:  []error{cmpopts.AnyError},
				},
			},
			wantErr: true,
		},
		{
			name: "WaitError",
			r: &Restorer{
				gceService: &fake.TestGCE{
					DeleteDiskResp: []*compute.Operation{nil},
					DeleteDiskErr:  []error{nil},
					DiskOpErr:      cmpopts.AnyError,
				},
			},
			wantErr: true,
		},
		{
			name: "Success",
			r: &Restorer{
				gceService: &fake.TestGCE{
					DeleteDiskResp: []*compute.Operation{nil},
					DeleteDiskErr:  []error{nil},
					DiskOpErr:      nil,
				},
			},
			wantErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.deleteOldDisk(ctx, "disk-1")
			if gotErr := (err != nil); gotErr != tc.wantErr {
				t.Errorf("deleteOldDisk() got error: %v, wantErr: %v", err, tc.wantErr)
			}
		})
	}
}

func TestHandleRestoreFailure(t *testing.T) {
	ctx := t.Context()
	tests := []struct {
		name string
		r    *Restorer
	}{
		{
			name: "Success",
			r: &Restorer{
				newAttachedDisks: []multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "new-disk-1",
							DeviceName: "dev-1",
						},
						instanceName: "instance-1",
					},
				},
				disks: []*multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "old-disk-1",
							DeviceName: "dev-2",
						},
						instanceName: "instance-1",
					},
				},
				gceService: &fake.TestGCE{
					DetachDiskErr:          nil,
					AttachDiskErr:          nil,
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              nil,
				},
				DataDiskZone: "us-central1-a",
			},
		},
		{
			name: "DetachDiskFails",
			r: &Restorer{
				newAttachedDisks: []multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "new-disk-1",
							DeviceName: "dev-1",
						},
						instanceName: "instance-1",
					},
				},
				disks: []*multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "old-disk-1",
							DeviceName: "dev-2",
						},
						instanceName: "instance-1",
					},
				},
				gceService: &fake.TestGCE{
					DetachDiskErr:          cmpopts.AnyError,
					AttachDiskErr:          nil,
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              nil,
				},
				DataDiskZone: "us-central1-a",
			},
		},
		{
			name: "AttachDiskFails",
			r: &Restorer{
				newAttachedDisks: []multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "new-disk-1",
							DeviceName: "dev-1",
						},
						instanceName: "instance-1",
					},
				},
				disks: []*multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "old-disk-1",
							DeviceName: "dev-2",
						},
						instanceName: "instance-1",
					},
				},
				gceService: &fake.TestGCE{
					DetachDiskErr:          nil,
					AttachDiskErr:          cmpopts.AnyError,
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              nil,
				},
				DataDiskZone: "us-central1-a",
			},
		},
		{
			name: "AddResourcePoliciesFails",
			r: &Restorer{
				newAttachedDisks: []multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "new-disk-1",
							DeviceName: "dev-1",
						},
						instanceName: "instance-1",
					},
				},
				disks: []*multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "old-disk-1",
							DeviceName: "dev-2",
						},
						instanceName: "instance-1",
					},
				},
				gceService: &fake.TestGCE{
					DetachDiskErr:          nil,
					AttachDiskErr:          nil,
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: cmpopts.AnyError,
					DiskOpErr:              nil,
				},
				DataDiskZone: "us-central1-a",
			},
		},
		{
			name: "RemoveResourcePoliciesFails",
			r: &Restorer{
				newAttachedDisks: []multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "new-disk-1",
							DeviceName: "dev-1",
						},
						instanceName: "instance-1",
					},
				},
				disks: []*multiDisks{
					{
						disk: &ipb.Disk{
							DiskName:   "old-disk-1",
							DeviceName: "dev-2",
						},
						instanceName: "instance-1",
					},
				},
				gceService: &fake.TestGCE{
					DetachDiskErr:             nil,
					AttachDiskErr:             nil,
					RemoveResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					RemoveResourcePoliciesErr: cmpopts.AnyError,
					DiskOpErr:                 nil,
				},
				DataDiskZone: "us-central1-a",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.r.oteLogger = onetime.CreateOTELogger(false)
			tc.r.handleRestoreFailure(ctx, successExec, cmpopts.AnyError)
		})
	}
}
