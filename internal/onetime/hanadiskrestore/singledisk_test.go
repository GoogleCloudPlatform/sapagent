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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"
)

func TestDiskRestore(t *testing.T) {
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
			name: "SingleSnapshotRestoreError",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Err: cmpopts.AnyError},
				},
				gceService: &fake.TestGCE{
					GetDiskResp:                      []*compute.Disk{nil},
					GetDiskErr:                       []error{&googleapi.Error{Code: http.StatusNotFound}},
					DiskAttachedToInstanceDeviceName: "",
					IsDiskAttached:                   false,
					DiskAttachedToInstanceErr:        cmpopts.AnyError,
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
			name: "SingleSnapshotRestoreSuccessNoVG",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				DataDiskVG:     "",
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 10}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{}, Err: nil},
				},
				gceService: &fake.TestGCE{
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            true,
				},
			},
			exec: successExec,
			want: nil,
		},
		{
			name: "SingleSnapshotRestoreRenameLVMFails",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				DataDiskVG:     "vg",
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 10}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{}, Err: nil},
				},
				gceService: &fake.TestGCE{
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            true,
					DetachDiskErr:             nil,
					GetInstanceErr:            []error{cmpopts.AnyError},
					GetInstanceResp:           defaultGetInstanceResp,
					ListDisksResp:             defaultListDisksResp,
				},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: cmpopts.AnyError}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SingleSnapshotRestoreRescanFails",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				DataDiskVG:     "vg",
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 10}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{}, Err: nil},
				},
				gceService: &fake.TestGCE{
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            true,
					DetachDiskErr:             nil,
					GetInstanceErr:            []error{nil},
					GetInstanceResp:           defaultGetInstanceResp,
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
				},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "/sbin/dmsetup" {
					return commandlineexecutor.Result{Error: cmpopts.AnyError}
				}
				return commandlineexecutor.Result{
					StdOut: "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SingleSnapshotRestoreRenameLVMSucceds",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				DataDiskVG:     "vg",
				computeService: &fakeComputeService{
					GetSnapshotCallResp: &fakeSnapshotsGetCall{Snapshot: &compute.Snapshot{DiskSizeGb: 10}, Err: nil},
					InsertDiskCallResp:  &fakeDisksInsertCall{Op: &compute.Operation{}, Err: nil},
				},
				gceService: &fake.TestGCE{
					DiskOpErr:                 nil,
					AttachDiskErr:             nil,
					DiskAttachedToInstanceErr: nil,
					IsDiskAttached:            true,
					DetachDiskErr:             nil,
					GetInstanceErr:            []error{nil},
					GetInstanceResp:           defaultGetInstanceResp,
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
				},
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		tc.r.oteLogger = onetime.CreateOTELogger(false)
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.diskRestore(context.Background(), tc.exec, defaultCloudProperties)
			if diff := cmp.Diff(got, tc.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("diskRestore() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
