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

package staginghanadiskrestore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/instantsnapshotgroup"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	cmFake "github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type mockISGService struct {
	newServiceErr error

	createISGError error

	describeInstantSnapshotsResp []instantsnapshotgroup.ISItem
	describeInstantSnapshotsErr  error

	describeStandardSnapshotsResp []*compute.Snapshot
	describeStandardSnapshotsErr  error

	getResponseCallCount int
	getResponseResp      [][]byte
	getResponseErr       []error
}

func (m *mockISGService) CreateISG(ctx context.Context, project, zone string, data []byte) error {
	return m.createISGError
}

func (m *mockISGService) DescribeInstantSnapshots(ctx context.Context, project, zone, isgName string) ([]instantsnapshotgroup.ISItem, error) {
	return m.describeInstantSnapshotsResp, m.describeInstantSnapshotsErr
}

func (m *mockISGService) DescribeStandardSnapshots(ctx context.Context, project, zone, isgName string) ([]*compute.Snapshot, error) {
	return m.describeStandardSnapshotsResp, m.describeStandardSnapshotsErr
}

func (m *mockISGService) GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error) {
	defer func() { m.getResponseCallCount++ }()
	return m.getResponseResp[m.getResponseCallCount], m.getResponseErr[m.getResponseCallCount]
}

func (m *mockISGService) TruncateName(ctx context.Context, src, suffix string) string {
	return ""
}

func (m *mockISGService) NewService() error {
	return nil
}

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
		Project:        "test-project",
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
	sampleSnapshot         = `{
      "kind": "compute#snapshot",
      "id": "123456789",
      "creationTimestamp": "2024-08-26T22:33:27.527-07:00",
      "name": "test-snapshot-standard",
      "status": "READY",
      "sourceDisk": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/disks/test-disk",
      "sourceDiskId": "123456789",
      "diskSizeGb": "577",
      "storageBytes": "99274113216",
      "storageBytesStatus": "UP_TO_DATE",
      "selfLink": "https://www.googleapis.com/compute/v1/projects/test-project/global/snapshots/self-snapshot-standard",
      "selfLinkWithId": "https://www.googleapis.com/compute/v1/projects/test-project/global/snapshots/123456789",
      "storageLocations": [
        "us"
      ],
      "downloadBytes": "99274910732",
      "sourceInstantSnapshot": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/instantSnapshots/instant-snapshot",
      "sourceInstantSnapshotId": "123456789",
      "creationSizeBytes": "99274113216",
      "enableConfidentialCompute": false
    }`
	sampleDisk = `{
		"kind": "compute#disk",
		"id": "123456789",
		"creationTimestamp": "2024-08-26T22:43:10.437-07:00",
		"name": "test-disk",
		"sizeGb": "577",
		"zone": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone",
		"status": "READY",
		"sourceSnapshot": "https://www.googleapis.com/compute/v1/projects/test-project/global/snapshots/test-snapshot-standard",
		"sourceSnapshotId": "3760400430889229256",
		"selfLink": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/disks/self-test-disk",
		"selfLinkWithId": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/disks/123456789",
		"type": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/diskTypes/hyperdisk-balanced",
		"users": [
			"https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/instances/test-instance"
		],
		"physicalBlockSizeBytes": "4096",
		"resourcePolicies": [
			"https://www.googleapis.com/compute/v1/projects/test-project/regions/test-region/resourcePolicies/test-cg"
		],
		"provisionedIops": "6462",
		"provisionedThroughput": "1006",
		"enableConfidentialCompute": false,
		"satisfiesPzi": false,
		"accessMode": "READ_WRITE_SINGLE",
		"locked": false
	}`
	sampleInstance = `{
		"kind": "compute#instance",
		"id": "123456789",
		"creationTimestamp": "2024-08-19T06:03:39.276-07:00",
		"name": "sample-instance",
		"machineType": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/machineTypes/m1-ultramem-40",
		"status": "RUNNING",
		"zone": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone",
		"disks": [
			{
				"kind": "compute#attachedDisk",
				"type": "PERSISTENT",
				"mode": "READ_WRITE",
				"source": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/disks/test-disk",
				"deviceName": "test-disk",
				"index": 2,
				"boot": false,
				"autoDelete": false,
				"interface": "SCSI",
				"diskSizeGb": "577"
			}
		],
		"selfLink": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/instances/sample-instance",
		"selfLinkWithId": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/instances/123456789",
		"lastStartTimestamp": "2024-08-25T22:38:25.897-07:00",
		"lastStopTimestamp": "2024-08-23T06:07:47.064-07:00",
		"satisfiesPzi": false,
		"resourceStatus": {
			"scheduling": {},
			"lastInstanceTerminationDetails": {
				"terminationReason": "USER_TERMINATED"
			}
		}
	}`
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
				Project:                         "test-project",
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
			restorer: Restorer{Project: "test-project"},
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
				Project:        "test-project",
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
				Project:        "test-project",
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
				Project:        "test-project",
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
				Project:        "test-project",
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
		fakeNewGCE         gceServiceFunc
		fakeComputeService computeServiceFunc
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
				},
			},
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "c", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
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
				return "b", "a", "c", nil
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
				return "b", "a", "c", nil
			},
			r: &Restorer{
				DataDiskName:   "test-disk-name",
				DataDiskZone:   "test-zone",
				SourceSnapshot: "test-snapshot",
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
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
				return "b", "a", "c", nil
			},
			r: &Restorer{
				GroupSnapshot: "test-snapshot",
				disks:         defaultRestorer.disks,
				gceService: &fake.TestGCE{
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
				return "b", "a", "c", nil
			},
			r: &Restorer{
				GroupSnapshot: "test-snapshot",
				disks:         defaultRestorer.disks,
				gceService: &fake.TestGCE{
					IsDiskAttached:            false,
					DiskAttachedToInstanceErr: cmpopts.AnyError,
				},
				isGroupSnapshot: true,
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
				return "b", "a", "c", nil
			},
			r: &Restorer{
				disks: defaultRestorer.disks,
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
				isGroupSnapshot: true,
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyNewTypeGroupSnapshot",
			cp:   defaultCloudProperties,
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "a", "b", "a", nil
			},
			checkLogDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "b", "a", "c", nil
			},
			r: &Restorer{
				disks: []*ipb.Disk{&ipb.Disk{DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte(`{"type": "https://www.googleapis.com/compute/v1/projects/test-project/zones/test-zone/diskTypes/hyperdisk-extreme"}`)},
					getResponseErr:  []error{nil},
					describeStandardSnapshotsResp: []*compute.Snapshot{
						{
							Name: "test-isg",
						},
						{
							Name: "test-isg-1",
						},
					},
					describeStandardSnapshotsErr: nil,
				},
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
					ListDisksErr:                     []error{nil},
					GetInstanceErr:                   []error{nil},
					DiskAttachedToInstanceDeviceName: "test-device-name",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
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
				NewDiskType: "hyperdisk-extreme",
				disks:       []*ipb.Disk{&ipb.Disk{DeviceName: "pd-balanced", Type: "PERSISTENT"}},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						{
							Name: "test-isg",
						},
						{
							Name: "test-isg-1",
						},
					},
					describeStandardSnapshotsErr: nil,
				},
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
					ListDisksErr:                     []error{nil},
					GetInstanceErr:                   []error{nil},
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
					DetachDiskErr: cmpopts.AnyError,
				},
				isgService: &mockISGService{
					getResponseResp: [][]byte{
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
					},
					getResponseErr: []error{cmpopts.AnyError, cmpopts.AnyError},
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
		restorer *Restorer
		f        fakeDiskMapper
		want     error
	}{
		{
			name: "Failure",
			restorer: &Restorer{
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
			name: "GroupSnapshot",
			f: fakeDiskMapper{
				deviceName: []string{"disk-device-name"},
			},
			restorer: &Restorer{
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
									Zone:                  "test-zone",
								},
								{
									Name: "disk-device-name",
									Type: "/some/path/device-type",
									Zone: "test-zone",
								},
							},
						},
					},
					ListDisksErr: []error{nil},
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
				NewdiskName:    "test-new-disk-name",
				computeService: nil,
				gceService: &fake.TestGCE{
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
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{},
					describeStandardSnapshotsErr:  cmpopts.AnyError,
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "attachDiskErr",
			r: &Restorer{
				disks: []*ipb.Disk{
					&ipb.Disk{
						DiskName: "test-disk",
					},
				},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						{
							Name:       "test-isg",
							SourceDisk: "test-disk",
						},
					},
					describeStandardSnapshotsErr: nil,
					getResponseResp:              [][]byte{[]byte{}, []byte{}},
					getResponseErr:               []error{cmpopts.AnyError, cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "modifyCGErr",
			r: &Restorer{
				disks: []*ipb.Disk{
					&ipb.Disk{
						DiskName: "test-disk",
					},
				},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						{
							Name:       "test-isg",
							SourceDisk: "test-disk",
						},
					},
					describeStandardSnapshotsErr: nil,
					getResponseResp:              [][]byte{[]byte{}, []byte{}, []byte{}},
					getResponseErr:               []error{nil, nil, cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				GroupSnapshot: "test-group-snapshot",
				gceService:    &fake.TestGCE{},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						{
							Name:       "test-isg",
							SourceDisk: "test-disk",
						},
						{
							Name:       "test-isg-1",
							SourceDisk: "test-disk-1",
						},
					},
					describeStandardSnapshotsErr: nil,
					getResponseResp:              [][]byte{[]byte{}},
					getResponseErr:               []error{nil},
				},
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
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{},
					describeStandardSnapshotsErr:  cmpopts.AnyError,
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
				gceService: &fake.TestGCE{},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{},
					describeStandardSnapshotsErr:  cmpopts.AnyError,
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
			name: "DiskNotAttachedToInstance",
			r: &Restorer{
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{},
					describeStandardSnapshotsErr:  nil,
					getResponseResp:               [][]byte{[]byte{}},
					getResponseErr:                []error{cmpopts.AnyError},
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
			name: "RenameLVMError",
			r: &Restorer{
				isGroupSnapshot: true,
				gceService:      &fake.TestGCE{},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						&compute.Snapshot{
							Name:       "test-isg",
							SourceDisk: "test-disk",
						},
						&compute.Snapshot{
							Name:       "test-isg-1",
							SourceDisk: "test-disk-1",
						},
					},
					describeStandardSnapshotsErr: nil,
					getResponseResp:              [][]byte{[]byte{}},
					getResponseErr:               []error{nil},
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
			name: "RestoreFromSnapshotSuccess",
			r: &Restorer{
				DataDiskVG:      "vg_hana_data",
				isGroupSnapshot: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
					GetInstanceResp: []*compute.Instance{{
						MachineType:       "test-machine-type",
						CpuPlatform:       "test-cpu-platform",
						CreationTimestamp: "test-creation-timestamp",
						Disks: []*compute.AttachedDisk{
							{
								Source:     "/some/path/test-disk",
								DeviceName: "test-disk",
								Type:       "PERSISTENT",
							},
						},
					}},
					GetInstanceErr: []error{nil},
					ListDisksResp: []*compute.DiskList{
						{
							Items: []*compute.Disk{
								{
									Name:                  "test-disk",
									Type:                  "/some/path/device-type",
									ProvisionedIops:       100,
									ProvisionedThroughput: 1000,
								},
								{
									Name: "test-disk",
									Type: "/some/path/device-type",
								},
							},
						},
					},
					ListDisksErr: []error{nil},
				},
				isgService: &mockISGService{
					describeStandardSnapshotsResp: []*compute.Snapshot{
						&compute.Snapshot{
							Name:       "test-isg",
							SourceDisk: "test-disk",
						},
					},
					describeStandardSnapshotsErr: nil,
					getResponseResp: [][]byte{
						[]byte(sampleSnapshot),
						[]byte{},
						[]byte(sampleDisk),
						[]byte{},
						[]byte(sampleInstance),
						[]byte(sampleInstance),
					},
					getResponseErr: []error{nil, nil, nil, nil, nil, nil},
				},
			},
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					Error:    nil,
					StdOut:   "PV         VG    Fmt  Attr PSize   PFree\n/dev/sdd  my_vg lvm2 a--  500.00g 300.00g",
				}
			},
			cp:          defaultCloudProperties,
			snapshotKey: "test-snapshot-key",
			want:        nil,
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
				physicalDataPath: "/dev/sdf\n/dev/sdg",
				gceService: &fake.TestGCE{
					IsDiskAttached:            true,
					DiskAttachedToInstanceErr: nil,
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
	want := "invoke HANA staginghanadiskrestore using workflow to restore from disk snapshot"
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
			policies: []string{"https://www.googleapis.com/invlaid/text/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my"},
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
				isgService: &mockISGService{
					getResponseResp: [][]byte{
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
					},
					getResponseErr: []error{nil},
				},
				DataDiskName: "disk-name",
			},
			wantErr: nil,
			wantCG:  "my-cg",
		},
		{
			name: "Failure",
			r: &Restorer{
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte("")},
					getResponseErr:  []error{cmpopts.AnyError},
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
				disks: []*ipb.Disk{
					{
						DiskName: "disk-name-1",
					},
					{
						DiskName: "disk-name-2",
					},
				},
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte{}, []byte{}},
					getResponseErr:  []error{cmpopts.AnyError, cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "NoCG",
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
						&compute.Disk{}, &compute.Disk{},
					},
					GetDiskErr: []error{nil, nil},
				},
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte(""), []byte("")},
					getResponseErr:  []error{nil, nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DisksBelongToDifferentCGs",
			r: &Restorer{
				disks: []*ipb.Disk{
					{
						DiskName: "disk-name-1",
					},
					{
						DiskName: "disk-name-2",
					},
				},
				// TODO: Update this with better testing when prod APIs are available.
				isgService: &mockISGService{
					getResponseResp: [][]byte{
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg-1"]}`),
					},
					getResponseErr: []error{nil, nil},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			// TODO: Update this with better testing when prod APIs are available.
			name: "DisksBelongToSameCGs",
			r: &Restorer{
				disks: []*ipb.Disk{
					{
						DiskName: "disk-name-1",
					},
					{
						DiskName: "disk-name-2",
					},
				},
				isgService: &mockISGService{
					getResponseResp: [][]byte{
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
						[]byte(`{"resourcePolicies": ["https://www.googleapis.com/compute/v1/projects/test-project/regions/test-zone/resourcePolicies/my-cg"]}`),
					},
					getResponseErr: []error{nil, nil},
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
			name: "addDisk",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte("")},
					getResponseErr:  []error{cmpopts.AnyError},
				},
			},
			diskName: "disk-name",
			add:      true,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "removeDisk",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte("")},
					getResponseErr:  []error{cmpopts.AnyError},
				},
			},
			diskName: "disk-name",
			add:      false,
			wantErr:  cmpopts.AnyError,
		},
		{
			name: "Success",
			r: &Restorer{
				DataDiskZone: "sample-zone-1",
				isgService: &mockISGService{
					getResponseResp: [][]byte{[]byte("")},
					getResponseErr:  []error{nil},
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
