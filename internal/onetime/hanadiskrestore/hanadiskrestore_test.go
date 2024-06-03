/*
Copyright 2023 Google LLC

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
	"os"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	cmFake "github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultRestorer = Restorer{
		Project:        "my-project",
		Sid:            "my-sid",
		HanaSidAdm:     "my-user",
		DataDiskName:   "data-disk",
		DataDiskZone:   "data-zone",
		NewDiskType:    "pd-ssd",
		SourceSnapshot: "my-snapshot",
		disks:          []*ipb.Disk{&ipb.Disk{}},
	}
	defaultCloudProperties = &ipb.CloudProperties{ProjectId: "default-project"}
)

func TestValidateParameters(t *testing.T) {
	tests := []struct {
		name         string
		restorer     Restorer
		os           string
		want         error
		wantRestorer *Restorer
	}{
		{
			name: "WindowsUnSupported",
			os:   "windows",
			want: cmpopts.AnyError,
		},
		{
			name: "ChangeDiskTypeWorkflow",
			restorer: Restorer{
				Project:                         "my-project",
				Sid:                             "my-sid",
				HanaSidAdm:                      "my-user",
				DataDiskName:                    "data-disk",
				DataDiskZone:                    "data-zone",
				SourceSnapshot:                  "snapshot",
				NewdiskName:                     "new-disk",
				NewDiskType:                     "pd-ssd",
				SkipDBSnapshotForChangeDiskType: true,
			},
			want: nil,
		},
		{
			name:     "Emptyproject",
			restorer: Restorer{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "Emptysid",
			restorer: Restorer{Project: "my-project"},
			want:     cmpopts.AnyError,
		},
		{
			name: "EmptyDataDiskName",
			restorer: Restorer{
				Sid: "my-sid",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyDataDiskZone",
			restorer: Restorer{
				Sid:          "my-sid",
				DataDiskName: "data-disk",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptySourceSnapshot",
			restorer: Restorer{
				Sid:          "my-sid",
				DataDiskName: "data-disk",
				DataDiskZone: "data-zone",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyNewDiskName",
			restorer: Restorer{
				Sid:            "my-sid",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskType",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "my-sid",
				HanaSidAdm:     "my-user",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "newDiskTypeSet",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "my-sid",
				HanaSidAdm:     "my-user",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				NewdiskName:    "new-disk",
				SourceSnapshot: "snapshot",
				NewDiskType:    "pd-ssd",
			},
		},
		{
			name: "Emptyproject",
			restorer: Restorer{
				Sid:            "tst",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewdiskName:    "new-disk",
				NewDiskType:    "pd-ssd",
			},
		},
		{
			name: "Emptyuser",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "tst",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewdiskName:    "new-disk",
				NewDiskType:    "pd-ssd",
			},
		},
		{
			name:     "GroupSnapshotNoSID",
			restorer: Restorer{GroupSnapshot: "group-snapshot"},
			want:     cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotAllArgs",
			restorer: Restorer{
				Sid:           "my-sid",
				GroupSnapshot: "group-snapshot",
			},
			want: nil,
		},
		{
			name: "BothGroupSnapshotAndSingleSnapshotPresent",
			restorer: Restorer{
				Sid:            "my-sid",
				GroupSnapshot:  "group-snapshot",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewdiskName:    "new-disk",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "BothGroupSnapshotAndSingleSnapshotAbsent",
			restorer: Restorer{
				Sid:            "my-sid",
				GroupSnapshot:  "group-snapshot",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewdiskName:    "new-disk",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "InvalidNewDiskName",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "tst",
				HanaSidAdm:     "my-user",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewDiskType:    "pd-ssd",
				NewdiskName:    "new-disk-name-which-is-much-much-longer-than-sixty-three-charecters",
			},
			want: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.validateParameters(test.os, defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(%q) = %v, want %v", test.os, got, test.want)
			}
		})
	}
}

func TestDefaultValues(t *testing.T) {
	r := Restorer{
		Sid:            "hdb",
		SourceSnapshot: "source-snapshot",
		DataDiskName:   "data-disk-name",
		DataDiskZone:   "data-disk-zone",
		NewdiskName:    "new-disk-name",
		NewDiskType:    "new-disk-type",
		Project:        "",
	}
	got := r.validateParameters("linux", defaultCloudProperties)
	if got != nil {
		t.Errorf("validateParameters()=%v, want=%v", got, nil)
	}
	if r.Project != "default-project" {
		t.Errorf("project = %v, want = %v", r.Project, "default-project")
	}
	if r.HanaSidAdm != "hdbadm" {
		t.Errorf("user = %v, want = %v", r.HanaSidAdm, "hdbadm")
	}
}

func TestRestoreHandler(t *testing.T) {
	tests := []struct {
		name               string
		restorer           Restorer
		fakeNewGCE         gceServiceFunc
		fakeComputeService computeServiceFunc
		want               subcommands.ExitStatus
	}{
		{
			name:     "InvalidParameters",
			restorer: Restorer{},
			want:     subcommands.ExitFailure,
		},
		{
			name:       "GCEServiceCreationFailure",
			restorer:   defaultRestorer,
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return nil, cmpopts.AnyError },
			want:       subcommands.ExitFailure,
		},
		{
			name:               "ComputeServiceCreateFailure",
			restorer:           defaultRestorer,
			fakeComputeService: func(context.Context) (*compute.Service, error) { return nil, cmpopts.AnyError },
			want:               subcommands.ExitFailure,
		},
		{
			name:               "checkPreconditionFailure",
			restorer:           defaultRestorer,
			fakeComputeService: func(context.Context) (*compute.Service, error) { return &compute.Service{}, nil },
			want:               subcommands.ExitFailure,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.restoreHandler(context.Background(), test.fakeNewGCE, test.fakeComputeService, defaultCloudProperties)
			if got != test.want {
				t.Errorf("restoreHandler() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name     string
		r        Restorer
		testArgs []any
		want     subcommands.ExitStatus
	}{
		{
			name:     "NotEnoughArgs",
			r:        defaultRestorer,
			testArgs: []any{""},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "NoLogParams",
			r:        defaultRestorer,
			testArgs: []any{"subcommdand_name", "2", "3"},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "NoCloudProperties",
			r:        defaultRestorer,
			testArgs: []any{"subcommdand_name", log.Parameters{}, "3"},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "CorrectArgs",
			r:        defaultRestorer,
			testArgs: []any{"subcommdand_name", log.Parameters{}, &ipb.CloudProperties{}},
			want:     subcommands.ExitFailure,
		},
		{
			name: "InternallyInvoked",
			r: Restorer{
				IIOTEParams: &onetime.InternallyInvokedOTE{
					InvokedBy: "test",
					Lp:        log.Parameters{},
					Cp:        defaultCloudProperties,
				},
			},
			want: subcommands.ExitFailure,
		},
		{
			name:     "Version",
			r:        Restorer{version: true},
			testArgs: []any{"subcommdand_name", log.Parameters{}, &ipb.CloudProperties{}},
			want:     subcommands.ExitSuccess,
		},
		{
			name:     "Help",
			r:        Restorer{help: true},
			testArgs: []any{"subcommdand_name", log.Parameters{}, &ipb.CloudProperties{}},
			want:     subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.testArgs...)
			if got != test.want {
				t.Errorf("Execute() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPrepare(t *testing.T) {
	tests := []struct {
		name                   string
		r                      *Restorer
		cp                     *ipb.CloudProperties
		waitForIndexServerStop waitForIndexServerToStopWithRetry
		exec                   commandlineexecutor.Execute
		want                   error
	}{
		{
			name: "SingleSnapshotDetachDiskErr",
			r: &Restorer{
				SourceSnapshot: "test-snapshot",
				gceService: &fake.TestGCE{
					DetachDiskErr: cmpopts.AnyError,
				},
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0, StdErr: "success", Error: nil,
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotDetachDiskErr",
			r: &Restorer{
				disks: []*ipb.Disk{&ipb.Disk{DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				gceService: &fake.TestGCE{
					DetachDiskErr: cmpopts.AnyError,
				},
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0, StdErr: "success", Error: nil,
				}
			},
			want: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.prepare(context.Background(), test.cp, test.waitForIndexServerStop, test.exec)
			if diff := cmp.Diff(got, test.want, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("prepare() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadDiskMapping(t *testing.T) {
	tests := []struct {
		name     string
		restorer Restorer
		want     error
	}{
		{
			name: "Failure",
			restorer: Restorer{
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
					GetInstanceErr: []error{cmpopts.AnyError},
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
					ListDisksErr: []error{cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			restorer: Restorer{
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
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.readDiskMapping(context.Background(), defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("readDiskMapping()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestSynopsisForRestorer(t *testing.T) {
	want := "invoke HANA hanadiskrestore using workflow to restore from disk snapshot"
	snapshot := Restorer{}
	got := snapshot.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForSnapshot(t *testing.T) {
	snapshot := Restorer{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"sid", "source-snapshot", "data-disk-name", "data-disk-zone", "project", "new-disk-type", "source-snapshot", "hana-sidadm", "force-stop-hana"}
	snapshot.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestSendDurationToCloudMonitoring(t *testing.T) {
	tests := []struct {
		name  string
		mtype string
		r     *Restorer
		dur   time.Duration
		bo    *cloudmonitoring.BackOffIntervals
		want  bool
	}{
		{
			name:  "Success",
			mtype: "restore",
			r: &Restorer{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			dur:  time.Second,
			bo:   &cloudmonitoring.BackOffIntervals{LongExponential: time.Millisecond, ShortConstant: time.Millisecond},
			want: true,
		},
		{
			name:  "Failure",
			mtype: "restore",
			r: &Restorer{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{Err: cmpopts.AnyError},
			},
			dur:  time.Second,
			bo:   &cloudmonitoring.BackOffIntervals{},
			want: false,
		},
		{
			name:  "SendStatusFalse",
			mtype: "restore",
			r: &Restorer{
				SendToMonitoring:  false,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			dur:  time.Second,
			bo:   &cloudmonitoring.BackOffIntervals{},
			want: false,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.sendDurationToCloudMonitoring(ctx, tc.mtype, tc.dur, tc.bo, defaultCloudProperties)
			if got != tc.want {
				t.Errorf("sendDurationToCloudMonitoring(%v, %v, %v) = %v, want: %v", tc.mtype, tc.dur, tc.bo, got, tc.want)
			}
		})
	}
}
