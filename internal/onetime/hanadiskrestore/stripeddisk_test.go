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
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
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
				gceService:    &fake.TestGCE{},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService:    &fake.TestGCE{},
			},
			want: cmpopts.AnyError,
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
			name: "GetGroupSnapshotErr",
			r: &Restorer{
				gceService: &fake.TestGCE{},
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
				gceService: &fake.TestGCE{},
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
			name: "RestoreFromSnapshotSuccess",
			r: &Restorer{
				gceService: &fake.TestGCE{},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
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
