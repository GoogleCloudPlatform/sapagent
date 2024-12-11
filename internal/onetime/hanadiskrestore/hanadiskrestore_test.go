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
	"fmt"
	"os"
	"testing"
	"time"

	"flag"
	"cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	cmFake "github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

type fakeDiskMapper struct {
	deviceName []string
	callCount  int
}

func (f *fakeDiskMapper) ForDeviceName(ctx context.Context, deviceName string) (string, error) {
	defer func() { f.callCount++ }()
	return f.deviceName[f.callCount], nil
}

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
		NewdiskName:    "new-data-disk",
		disks:          []*ipb.Disk{&ipb.Disk{}},
	}
	defaultCloudProperties = &ipb.CloudProperties{ProjectId: "default-project"}
	defaultGetInstanceResp = []*compute.Instance{{
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
	}}
	defaultListDisksResp = []*compute.DiskList{
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
	}
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
		{
			name: "InvalidNewDiskPrefix",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "tst",
				HanaSidAdm:     "my-user",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewDiskType:    "pd-ssd",
				NewdiskName:    "new-disk-name",
				NewDiskPrefix:  "invalid-new-disk-prefix-which-is-much-much-longer-than-sixty-one-charecters",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			restorer: Restorer{
				Project:        "my-project",
				Sid:            "tst",
				HanaSidAdm:     "my-user",
				DataDiskName:   "data-disk",
				DataDiskZone:   "data-zone",
				SourceSnapshot: "snapshot",
				NewDiskType:    "pd-ssd",
				NewdiskName:    "new-disk-name",
				NewDiskPrefix:  "new-disk-prefix",
			},
			want: nil,
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

func TestExtractLabels(t *testing.T) {
	tests := []struct {
		name           string
		r              *Restorer
		snapshot       *compute.Snapshot
		wantIOPS       int64
		wantThroughput int64
	}{
		{
			name: "Success",
			r:    &Restorer{},
			snapshot: &compute.Snapshot{
				Labels: map[string]string{
					"goog-sapagent-provisioned-iops":       "100",
					"goog-sapagent-provisioned-throughput": "1000",
				},
			},
			wantIOPS:       100,
			wantThroughput: 1000,
		},
		{
			name: "InvalidLabels",
			r:    &Restorer{},
			snapshot: &compute.Snapshot{
				Labels: map[string]string{
					"goog-sapagent-provisioned-iops":       "B00",
					"goog-sapagent-provisioned-throughput": "XRT000",
				},
			},
		},
		{
			name: "ParamOverride",
			r: &Restorer{
				ProvisionedIops:       999,
				ProvisionedThroughput: 9999,
			},
			snapshot: &compute.Snapshot{
				Labels: map[string]string{
					"goog-sapagent-provisioned-iops":       "100",
					"goog-sapagent-provisioned-throughput": "1000",
				},
			},
			wantIOPS:       999,
			wantThroughput: 9999,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.r.extractLabels(context.Background(), test.snapshot)
			if test.r.ProvisionedIops != test.wantIOPS {
				t.Errorf("extractLabels()=%v, want=%v", test.r.ProvisionedIops, test.wantIOPS)
			}
			if test.r.ProvisionedThroughput != test.wantThroughput {
				t.Errorf("extractLabels()=%v, want=%v", test.r.ProvisionedThroughput, test.wantThroughput)
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
		fakeMetricClient   metricClientCreator
		fakeNewGCE         onetime.GCEServiceFunc
		fakeComputeService onetime.ComputeServiceFunc
		want               subcommands.ExitStatus
	}{
		{
			name:     "InvalidParameters",
			restorer: Restorer{},
			want:     subcommands.ExitUsageError,
		},
		{
			name:     "MetricClientCreationFailure",
			restorer: defaultRestorer,
			fakeMetricClient: func(context.Context, ...option.ClientOption) (*monitoring.MetricClient, error) {
				return nil, cmpopts.AnyError
			},
			want: subcommands.ExitFailure,
		},
		{
			name:     "GCEServiceCreationFailure",
			restorer: defaultRestorer,
			fakeMetricClient: func(context.Context, ...option.ClientOption) (*monitoring.MetricClient, error) {
				return &monitoring.MetricClient{}, nil
			},
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return nil, cmpopts.AnyError },
			want:       subcommands.ExitFailure,
		},
		{
			name:     "ComputeServiceCreateFailure",
			restorer: defaultRestorer,
			fakeMetricClient: func(context.Context, ...option.ClientOption) (*monitoring.MetricClient, error) {
				return &monitoring.MetricClient{}, nil
			},
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return nil, cmpopts.AnyError },
			want:               subcommands.ExitFailure,
		},
		{
			name:     "checkPreconditionFailure",
			restorer: defaultRestorer,
			fakeMetricClient: func(context.Context, ...option.ClientOption) (*monitoring.MetricClient, error) {
				return &monitoring.MetricClient{}, nil
			},
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return &compute.Service{}, nil },
			want:               subcommands.ExitFailure,
		},
	}

	checkDir := func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
		return "", "", "", nil
	}
	for _, test := range tests {
		test.restorer.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.restoreHandler(context.Background(), test.fakeMetricClient, test.fakeNewGCE, test.fakeComputeService, defaultCloudProperties, checkDir, checkDir)
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
			want: subcommands.ExitUsageError,
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

func TestCheckPreConditions(t *testing.T) {
	tests := []struct {
		name         string
		r            *Restorer
		cp           *ipb.CloudProperties
		checkDataDir getDataPaths
		checkLogDir  getLogPaths
		wantErr      error
	}{
		{
			name: "CheckDataDirErr",
			cp:   defaultCloudProperties,
			r:    &Restorer{},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				fmt.Println("here")
				return "", "", "", cmpopts.AnyError
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CheckLogDirErr",
			cp:   defaultCloudProperties,
			r:    &Restorer{},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", cmpopts.AnyError
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DataAndLogOnSameDisk1",
			cp:   defaultCloudProperties,
			r:    &Restorer{},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "DataAndLogOnSameDisk2",
			cp:   defaultCloudProperties,
			r:    &Restorer{},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c\nd", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ReadDiskMappingErr",
			cp:   defaultCloudProperties,
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					GetInstanceErr:  []error{cmpopts.AnyError},
				},
			},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SingleSnapshotDiskAttachedErr",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				DataDiskName:    "test-disk-name",
				DataDiskZone:    "test-zone",
				SourceSnapshot:  "test-snapshot",
				gceService:      &fake.TestGCE{DiskAttachedToInstanceErr: cmpopts.AnyError},
				isGroupSnapshot: false,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SingleSnapshotDiskAttachedFalse",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				DataDiskName:   "test-disk-name",
				DataDiskZone:   "test-zone",
				SourceSnapshot: "test-snapshot",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
				},
				isGroupSnapshot: false,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotDiskAttachedErr",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				GroupSnapshot: "test-snapshot",
				disks:         defaultRestorer.disks,
				gceService: &fake.TestGCE{
					GetInstanceResp:           defaultGetInstanceResp,
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
					GetInstanceErr:            []error{nil},
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
				isGroupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotDiskAttachedFalse",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				GroupSnapshot: "test-snapshot",
				disks:         defaultRestorer.disks,
				gceService: &fake.TestGCE{
					GetInstanceResp:           defaultGetInstanceResp,
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
					GetInstanceErr:            []error{nil},
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: nil,
				},
				isGroupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SourceSnapshotAbsent",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				DataDiskName:   "test-disk-name",
				DataDiskZone:   "test-zone",
				SourceSnapshot: "test-snapshot",
				gceService: &fake.TestGCE{
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					DiskAttachedToInstanceDeviceName: "test-device-name",
				},
				isGroupSnapshot: false,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotAbsent",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				disks: defaultRestorer.disks,
				gceService: &fake.TestGCE{
					GetInstanceResp:                  defaultGetInstanceResp,
					ListDisksResp:                    defaultListDisksResp,
					ListDisksErr:                     []error{nil},
					GetInstanceErr:                   []error{nil},
					SnapshotList:                     nil,
					SnapshotListErr:                  cmpopts.AnyError,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
				isGroupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "numOfSnapshotsNotEqualToNumOfDisks",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				disks: defaultRestorer.disks,
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
					GetInstanceErr:  []error{nil},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{},
					},
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
				isGroupSnapshot: true,
				GroupSnapshot:   "test-group-snapshot",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "groupSnapshotPresent",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "d", nil
			},
			r: &Restorer{
				disks: defaultRestorer.disks,
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
					GetInstanceErr:  []error{nil},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot",
								SourceDisk: "test-disk",
								Labels: map[string]string{
									"isg": "test-group-snapshot",
								},
							},
						},
					},
					SnapshotListErr:                  cmpopts.AnyError,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
				isGroupSnapshot: true,
				GroupSnapshot:   "test-group-snapshot",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyNewTypeGroupSnapshotErr",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "a", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			r: &Restorer{
				disks:         []*ipb.Disk{&ipb.Disk{DiskName: "test-disk-name", DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
					GetInstanceErr:  []error{nil},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot",
								SourceDisk: "test-disk",
								Labels: map[string]string{
									"isg": "test-group-snapshot",
								},
							},
						},
					},
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					GetDiskResp:                      []*compute.Disk{{}},
					GetDiskErr:                       []error{cmpopts.AnyError},
				},
				isGroupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyNewTypeGroupSnapshotNoErr",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "a", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			r: &Restorer{
				disks:         []*ipb.Disk{&ipb.Disk{DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
					GetInstanceErr:  []error{nil},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot",
								SourceDisk: "test-disk",
								Labels: map[string]string{
									"goog-sapagent-isg": "test-group-snapshot",
								},
							},
						},
					},
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
					GetDiskResp: []*compute.Disk{
						{
							Name: "test-disk",
						},
					},
					GetDiskErr: []error{nil},
				},
				isGroupSnapshot: true,
			},
			wantErr: nil,
		},
		{
			name: "NewTypePresent",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "a", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			r: &Restorer{
				NewDiskType:   "hyperdisk-extreme",
				disks:         []*ipb.Disk{&ipb.Disk{DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				GroupSnapshot: "test-group-snapshot",
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
					GetInstanceErr:  []error{nil},
					SnapshotList: &compute.SnapshotList{
						Items: []*compute.Snapshot{
							{
								Name:       "test-snapshot",
								SourceDisk: "test-disk",
								Labels: map[string]string{
									"goog-sapagent-isg": "test-group-snapshot",
								},
							},
						},
					},
					SnapshotListErr:                  nil,
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
				isGroupSnapshot: true,
			},
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.checkPreConditions(context.Background(), test.cp, test.checkDataDir, test.checkLogDir)
			if diff := cmp.Diff(got, test.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("checkPreConditions() returned diff (-want +got):\n%s", diff)
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
			name: "FetchVGErr",
			r: &Restorer{
				isGroupSnapshot: false,
			},
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd   lvm2 a--  500.00g 300.00g",
				}
			},
			want: cmpopts.AnyError,
		},
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
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd lvm2 a--  500.00g 300.00g",
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SingleSnapshotSuccess",
			r: &Restorer{
				isGroupSnapshot: false,
				gceService: &fake.TestGCE{
					DetachDiskErr: nil,
				},
				DataDiskVG: "temp_vg",
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: nil,
		},
		{
			name: "GroupSnapshotValidateCGErr",
			r: &Restorer{
				disks: []*ipb.Disk{
					{
						DiskName: "disk-name-1",
					},
					{
						DiskName: "disk-name-2",
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
				isGroupSnapshot: true,
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotDetachDiskErr",
			r: &Restorer{
				isGroupSnapshot: true,
				disks: []*ipb.Disk{
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-1",
					},
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-2",
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
					GetDiskErr:    []error{nil, nil},
					DetachDiskErr: cmpopts.AnyError,
				},
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshotModifyCGErr",
			r: &Restorer{
				isGroupSnapshot: true,
				disks: []*ipb.Disk{
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-1",
					},
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-2",
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
					GetDiskErr:    []error{nil, nil},
					DetachDiskErr: nil,
				},
				DataDiskZone: "invalid-zone",
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: nil,
		},
		{
			name: "GroupSnapshotSuccess",
			r: &Restorer{
				isGroupSnapshot: true,
				disks: []*ipb.Disk{
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-1",
					},
					{
						DeviceName: "pd-balanced",
						Type:       "PERSISTENT",
						DiskName:   "disk-name-2",
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
					GetDiskErr:             []error{nil, nil},
					DetachDiskErr:          nil,
					AddResourcePoliciesOp:  &compute.Operation{Status: "DONE"},
					AddResourcePoliciesErr: nil,
					DiskOpErr:              nil,
				},
				DataDiskZone: "valid-zone-1",
			},
			cp: defaultCloudProperties,
			waitForIndexServerStop: func(context.Context, string, commandlineexecutor.Execute) error {
				return nil
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.r.oteLogger = onetime.CreateOTELogger(true)
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
		restorer *Restorer
		f        fakeDiskMapper
		want     error
	}{
		{
			name: "Failure",
			restorer: &Restorer{
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					GetInstanceErr:  []error{cmpopts.AnyError},
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "GroupSnapshot",
			f: fakeDiskMapper{
				deviceName: []string{"disk-device-name"},
			},
			restorer: &Restorer{
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					GetInstanceErr:  []error{nil},
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
				},
				isGroupSnapshot:  true,
				physicalDataPath: "disk-device-name",
				DataDiskZone:     "test-zone-1",
			},
			want: nil,
		},
		{
			name: "DataDiskProvided",
			f: fakeDiskMapper{
				deviceName: []string{"disk-device-name"},
			},
			restorer: &Restorer{
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					GetInstanceErr:  []error{nil},
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
				},
				DataDiskName:     "test-data-disk-name-1",
				physicalDataPath: "disk-device-name",
			},
			want: nil,
		},
		{
			name: "Success",
			f: fakeDiskMapper{
				deviceName: []string{"disk-device-name"},
			},
			restorer: &Restorer{
				gceService: &fake.TestGCE{
					GetInstanceResp: defaultGetInstanceResp,
					GetInstanceErr:  []error{nil},
					ListDisksResp:   defaultListDisksResp,
					ListDisksErr:    []error{nil},
				},
				physicalDataPath: "disk-device-name",
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.readDiskMapping(context.Background(), defaultCloudProperties, &test.f)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("readDiskMapping()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestFetchVG(t *testing.T) {
	tests := []struct {
		name    string
		r       *Restorer
		cp      *ipb.CloudProperties
		exec    commandlineexecutor.Execute
		wantVG  string
		wantErr error
	}{
		{
			name: "Fail",
			cp:   defaultCloudProperties,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0, StdErr: "fail", Error: cmpopts.AnyError,
				}
			},
			wantVG:  "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoVG1",
			cp:   defaultCloudProperties,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd   lvm2 a--  500.00g 300.00g",
				}
			},
			wantVG:  "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoVG2",
			cp:   defaultCloudProperties,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree",
				}
			},
			wantVG:  "",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			cp:   defaultCloudProperties,
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			wantVG:  "my_vg",
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotVG, err := tc.r.fetchVG(ctx, tc.cp, tc.exec, "/dev/sda\n/dev/sdb")
			if gotVG != tc.wantVG {
				t.Errorf("fetchVG() = %v, want %v", gotVG, tc.wantVG)
			}
			if cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()) != "" {
				t.Errorf("fetchVG() returned diff (-want +got):\n%s", cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()))
			}
		})
	}
}

func TestRenameLVM(t *testing.T) {
	tests := []struct {
		name     string
		r        *Restorer
		exec     commandlineexecutor.Execute
		cp       *ipb.CloudProperties
		diskName string
		wantErr  error
	}{
		{
			name: "InstanceReadError",
			r: &Restorer{
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetInstanceResp:           defaultGetInstanceResp,
					GetInstanceErr:            []error{cmpopts.AnyError},
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{cmpopts.AnyError},
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:       defaultCloudProperties,
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "FetchVGFail",
			r: &Restorer{
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetInstanceResp:           defaultGetInstanceResp,
					GetInstanceErr:            []error{nil},
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 1,
					Error:    cmpopts.AnyError,
				}
			},
			cp:       defaultCloudProperties,
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "SameVG",
			r: &Restorer{
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetInstanceResp:           defaultGetInstanceResp,
					GetInstanceErr:            []error{nil},
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
				},
				DataDiskVG: "my_vg",
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			cp:       defaultCloudProperties,
			diskName: "test-disk-name",
			wantErr:  nil,
		},
		{
			name: "RenameVG",
			r: &Restorer{
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetInstanceResp:           defaultGetInstanceResp,
					GetInstanceErr:            []error{nil},
					ListDisksResp:             defaultListDisksResp,
					ListDisksErr:              []error{nil},
				},
				DataDiskVG: "vg_1",
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			cp:       defaultCloudProperties,
			diskName: "test-disk-name",
			wantErr:  nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.renameLVM(ctx, tc.exec, tc.cp, "disk-device-name", tc.diskName)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("renameLVM() returned diff (-want +got):\n%s", diff)
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
	flags := []string{"sid", "source-snapshot", "data-disk-name", "data-disk-zone", "project", "new-disk-type", "source-snapshot", "hana-sidadm", "force-stop-hana", "group-snapshot-name", "new-disk-prefix"}
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

func TestAppendLabelsToDetachedDisk(t *testing.T) {
	tests := []struct {
		name     string
		r        *Restorer
		diskName string
		wantErr  error
	}{
		{
			name: "getDiskErr",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{nil},
					GetDiskErr:  []error{cmpopts.AnyError},
				},
			},
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "appendLabelsErr",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							LabelFingerprint: "test-label-fingerprint",
						},
					},
					GetDiskErr: []error{nil},
				},
				labelsOnDetachedDisk: "key1, key2=value2",
			},
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "setLabelsErr",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Labels:           map[string]string{"sample-key": "sample-value"},
							LabelFingerprint: "test-label-fingerprint",
						},
					},
					GetDiskErr:   []error{nil},
					SetLabelsOp:  &compute.Operation{Status: "DONE"},
					SetLabelsErr: cmpopts.AnyError,
				},
				labelsOnDetachedDisk: "key1=value1, key2=value2",
			},
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "diskOpErr",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							Labels:           map[string]string{"sample-key": "sample-value"},
							LabelFingerprint: "test-label-fingerprint",
						},
					},
					GetDiskErr:   []error{nil},
					SetLabelsOp:  &compute.Operation{Status: "DONE"},
					SetLabelsErr: nil,
					DiskOpErr:    cmpopts.AnyError,
				},
				labelsOnDetachedDisk: "key1=value1, key2=value2",
			},
			diskName: "test-disk-name",
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				gceService: &fake.TestGCE{
					GetDiskResp: []*compute.Disk{
						&compute.Disk{
							LabelFingerprint: "test-label-fingerprint",
						},
					},
					GetDiskErr:   []error{nil},
					SetLabelsOp:  &compute.Operation{Status: "DONE"},
					SetLabelsErr: nil,
					DiskOpErr:    nil,
				},
				labelsOnDetachedDisk: "key1=value1, key2=value2",
			},
			diskName: "test-disk-name",
			wantErr:  nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.r.appendLabelsToDetachedDisk(ctx, tc.diskName)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("appendLabelsToDetachedDisk() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAppendLabels(t *testing.T) {
	tests := []struct {
		name       string
		r          *Restorer
		labels     map[string]string
		wantLabels map[string]string
		wantErr    error
	}{
		{
			name: "Failure",
			r: &Restorer{
				labelsOnDetachedDisk: "key1, key2=value2",
			},
			labels:     map[string]string{"test-key": "test-value"},
			wantLabels: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				labelsOnDetachedDisk: "key1=value1, key2=value2",
			},
			labels: map[string]string{"test-key": "test-value"},
			wantLabels: map[string]string{
				"test-key": "test-value",
				"key1":     "value1",
				"key2":     "value2",
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.r.appendLabels(tc.labels)
			if diff := cmp.Diff(got, tc.wantLabels, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("appendLabels() returned diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("appendLabels() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
