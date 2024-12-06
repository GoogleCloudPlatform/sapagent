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

package hanadiskbackup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring"
	cmFake "github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultOTELogger = onetime.CreateOTELogger(false)

	defaultSnapshot = Snapshot{
		Project:    "my-project",
		Host:       "localhost",
		Port:       "123",
		Sid:        "HDB",
		HanaDBUser: "system",
		Disk:       "pd-1",
		DiskZone:   "us-east1-a",
		Password:   "password",
		oteLogger:  defaultOTELogger,
	}

	testCommandExecute = func(stdout, stderr string, err error) commandlineexecutor.Execute {
		return func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			exitCode := 0
			var exitErr *exec.ExitError
			if err != nil && errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return commandlineexecutor.Result{
				StdOut:   stdout,
				StdErr:   stderr,
				Error:    err,
				ExitCode: exitCode,
			}
		}
	}

	testCommandExecuteWithExitCode = func(stdout, stderr string, exitCode int, err error) commandlineexecutor.Execute {
		return func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			var exitErr *exec.ExitError
			if err != nil && errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return commandlineexecutor.Result{
				StdOut:   stdout,
				StdErr:   stderr,
				Error:    err,
				ExitCode: exitCode,
			}
		}
	}
)

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId:    "default-project",
	InstanceName: "default-instance",
	Zone:         "default-zone",
}

type fakeSnapshot interface {
	isDiskAttachedToInstance(diskName string) (string, bool, error)
}

type mockDiskCreateSnapshot struct {
	doErr     error
	operation *compute.Operation
}

func (m *mockDiskCreateSnapshot) Context(ctx context.Context) *compute.DisksCreateSnapshotCall {
	return &compute.DisksCreateSnapshotCall{}
}

func (m *mockDiskCreateSnapshot) Do(...googleapi.CallOption) (*compute.Operation, error) {
	return &compute.Operation{}, m.doErr
}

func (m *mockDiskCreateSnapshot) Fields(...googleapi.Field) *compute.DisksCreateSnapshotCall {
	return &compute.DisksCreateSnapshotCall{}
}

func (m *mockDiskCreateSnapshot) GuestFlush(bool) *compute.DisksCreateSnapshotCall {
	return &compute.DisksCreateSnapshotCall{}
}

func (m *mockDiskCreateSnapshot) Header() http.Header {
	return nil
}

func (m *mockDiskCreateSnapshot) RequestId(string) *compute.DisksCreateSnapshotCall {
	return &compute.DisksCreateSnapshotCall{}
}

func createDiskSnapshotFail(*compute.Snapshot) fakeDiskCreateSnapshotCall {
	return &mockDiskCreateSnapshot{doErr: cmpopts.AnyError}
}

func createDiskSnapshotSuccess(*compute.Snapshot) fakeDiskCreateSnapshotCall {
	return &mockDiskCreateSnapshot{doErr: nil, operation: &compute.Operation{}}
}

func TestSnapshotHandler(t *testing.T) {
	tests := []struct {
		name               string
		snapshot           Snapshot
		fakeNewGCE         onetime.GCEServiceFunc
		fakeComputeService onetime.ComputeServiceFunc
		checkDataDir       checkDataDirFunc
		want               subcommands.ExitStatus
	}{
		{
			name:       "GCEServiceCreationFailure",
			snapshot:   defaultSnapshot,
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return nil, cmpopts.AnyError },
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", cmpopts.AnyError
			},
			want: subcommands.ExitFailure,
		},
		{
			name:               "ComputeServiceCreationFailure",
			snapshot:           defaultSnapshot,
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return nil, cmpopts.AnyError },
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", nil
			},
			want: subcommands.ExitFailure,
		},
		{
			name:               "CheckDataDirFailure",
			snapshot:           defaultSnapshot,
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return &compute.Service{}, nil },
			checkDataDir: func(context.Context, commandlineexecutor.Execute) (string, string, string, error) {
				return "", "", "", cmpopts.AnyError
			},
			want: subcommands.ExitFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			_, got := test.snapshot.snapshotHandler(context.Background(), test.fakeNewGCE, test.fakeComputeService, test.checkDataDir, defaultCloudProperties)
			if got != test.want {
				t.Errorf("snapshotHandler(%v)=%v want %v", test.name, got, test.want)
			}
		})
	}
}

func TestReadDiskMapping(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		want     error
	}{
		{
			name: "Failure",
			snapshot: Snapshot{
				SnapshotName: "snapshot",
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
					GetInstanceErr: []error{cmpopts.AnyError},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			snapshot: Snapshot{
				SnapshotName: "sample-snapshot",
				Disk:         "disk-name",
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
		{
			name: "SuccessDefaultSnapshotName",
			snapshot: Snapshot{
				physicalDataPath: "unknown",
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
			oldSnapshot := test.snapshot.SnapshotName
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.readDiskMapping(context.Background(), defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("readDiskMapping()=%v, want=%v", got, test.want)
			}
			if oldSnapshot == "" {
				pattern := "^snapshot-disk-name-\\d{4}\\d{2}\\d{2}-\\d{2}\\d{2}\\d{2}$"
				regex, _ := regexp.Compile(pattern)
				if !regex.MatchString(test.snapshot.SnapshotName) {
					t.Errorf("readDiskMapping(), default snapshot created does not match, got=%v, want=%v", test.snapshot.SnapshotName, pattern)
				}
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name string
		s    Snapshot
		want map[string]string
	}{
		{
			name: "Invalidlabel",
			s: Snapshot{
				Labels: "label1,label2",
			},
			want: map[string]string{},
		},
		{
			name: "Success",
			s: Snapshot{
				Labels: "label1=value1,label2=value2",
			},
			want: map[string]string{"label1": "value1", "label2": "value2"},
		},
		{
			name: "GroupSnapshot",
			s: Snapshot{
				groupSnapshotName: "group-snapshot-name",
				groupSnapshot:     true,
				DiskZone:          "my-region-1",
				cgName:            "my-cg",
				Labels:            "label1=value1,label2=value2",
				Disk:              "pd-1",
			},
			want: map[string]string{
				"goog-sapagent-isg":       "group-snapshot-name",
				"goog-sapagent-version":   strings.ReplaceAll(configuration.AgentVersion, ".", "_"),
				"goog-sapagent-cgpath":    "my-region-my-cg",
				"goog-sapagent-disk-name": "pd-1",
				"label1":                  "value1",
				"label2":                  "value2",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.parseLabels()
			opts := cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
				return key == "goog-sapagent-timestamp" || key == "goog-sapagent-sha224"
			})
			if !cmp.Equal(got, test.want, opts) {
				t.Errorf("parseLabels() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExecuteSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		want     subcommands.ExitStatus
		args     []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailAssertSecondArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name:     "SuccessfullyParseArgs",
			snapshot: Snapshot{},
			want:     subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "InternallyInvoked",
			snapshot: Snapshot{
				IIOTEParams: &onetime.InternallyInvokedOTE{
					Lp:        log.Parameters{},
					Cp:        defaultCloudProperties,
					InvokedBy: "test",
				},
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "SuccessForAgentVersion",
			snapshot: Snapshot{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			snapshot: Snapshot{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name:     "InvalidParams",
			snapshot: Snapshot{},
			want:     subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.snapshot, test.args, got, test.want)
			}
		})
	}
}

var snapshotComparer = cmp.Comparer(func(a, b Snapshot) bool {
	validSnapshotName := false
	// This succeeds when user overrides the snapshot name.
	if a.SnapshotName != "" && a.SnapshotName == b.SnapshotName {
		validSnapshotName = true
	}
	// This succeeds when user does not override the snapshot name.
	if strings.HasPrefix(a.SnapshotName, "snapshot-") || strings.HasPrefix(b.SnapshotName, "snapshot-") {
		validSnapshotName = true
	}
	return validSnapshotName && a.Sid == b.Sid
})

func TestValidateParameters(t *testing.T) {
	tests := []struct {
		name         string
		snapshot     Snapshot
		os           string
		wantErr      error
		wantSnapshot Snapshot
	}{
		{
			name:    "WindowsUnSupported",
			os:      "windows",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyPort",
			snapshot: Snapshot{
				Port: "",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ChangeDiskTypeWorkflow",
			snapshot: Snapshot{
				Host:                            "localhost",
				Port:                            "123",
				Sid:                             "HDB",
				HanaDBUser:                      "system",
				Disk:                            "pd-1",
				DiskZone:                        "us-east1-a",
				Password:                        "password",
				PasswordSecret:                  "secret",
				SkipDBSnapshotForChangeDiskType: true,
				SnapshotType:                    "STANDARD",
			},
			wantErr: nil,
			wantSnapshot: Snapshot{
				Sid:          "HDB",
				SnapshotName: "snapshot-pd-1-time-stamp",
			},
		},
		{
			name: "EmptySID",
			snapshot: Snapshot{
				Port:         "123",
				Sid:          "",
				SnapshotType: "STANDARD",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyUser",
			snapshot: Snapshot{
				Port:         "123",
				Sid:          "HDB",
				HanaDBUser:   "",
				SnapshotType: "STANDARD",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyDisk",
			snapshot: Snapshot{
				Host:         "localhost",
				Port:         "123",
				Sid:          "HDB",
				HanaDBUser:   "system",
				Disk:         "",
				SnapshotType: "STANDARD",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskZone",
			snapshot: Snapshot{
				Host:         "localhost",
				Port:         "123",
				Sid:          "HDB",
				HanaDBUser:   "system",
				Disk:         "pd-1",
				DiskZone:     "",
				SnapshotType: "STANDARD",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyPasswordAndSecret",
			snapshot: Snapshot{
				Host:           "localhost",
				Port:           "123",
				Sid:            "HDB",
				HanaDBUser:     "system",
				Disk:           "pd-1",
				DiskZone:       "us-east1-a",
				Password:       "",
				PasswordSecret: "",
				SnapshotType:   "STANDARD",
			},
			wantErr: cmpopts.AnyError,
			wantSnapshot: Snapshot{
				Sid: "HDB",
			},
		},
		{
			name: "EmptyPortAndInstanceID",
			snapshot: Snapshot{
				Host:           "localhost",
				Port:           "",
				InstanceID:     "",
				Sid:            "HDB",
				HanaDBUser:     "system",
				Disk:           "pd-1",
				DiskZone:       "us-east1-a",
				PasswordSecret: "secret",
				SnapshotType:   "ARCHIVE",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Emptyhost",
			snapshot: Snapshot{
				Port:           "123",
				Sid:            "HDB",
				Host:           "",
				HanaDBUser:     "system",
				Disk:           "pd-1",
				DiskZone:       "us-east1-a",
				PasswordSecret: "secret",
				SnapshotType:   "STANDARD",
			},
			wantSnapshot: Snapshot{
				Sid:          "HDB",
				SnapshotName: "snapshot-time-stamp",
			},
		},
		{
			name: "Emptyproject",
			snapshot: Snapshot{
				Port:           "123",
				Sid:            "HDB",
				Project:        "",
				HanaDBUser:     "system",
				Disk:           "pd-1",
				DiskZone:       "us-east1-a",
				PasswordSecret: "secret",
				SnapshotType:   "ARCHIVE",
			},
			wantSnapshot: Snapshot{
				Sid:          "HDB",
				SnapshotName: "snapshot-time-stamp",
			},
		},
		{
			name: "HDBUserstoreConfig",
			snapshot: Snapshot{
				Sid:             "HDB",
				HDBUserstoreKey: "hdbuserstore-key",
				Disk:            "pd-1",
				DiskZone:        "us-east1-a",
				SnapshotType:    "STANDARD",
			},
			wantSnapshot: Snapshot{
				Sid:             "HDB",
				HDBUserstoreKey: "hdbuserstore-key",
				Disk:            "pd-1",
				DiskZone:        "us-east1-a",
				SnapshotName:    "snapshot-pd-1-time-stamp",
			},
		},
		{
			name: "EmptySnapshotNameEmptyDisk",
			snapshot: Snapshot{
				Port:           "123",
				Sid:            "HDB",
				Project:        "",
				HanaDBUser:     "system",
				DiskZone:       "us-east1-a",
				PasswordSecret: "secret",
				SnapshotType:   "STANDARD",
			},
			wantSnapshot: Snapshot{
				Sid:          "HDB",
				SnapshotName: "snapshot-time-stamp",
			},
		},
		{
			name: "InvalidSnapshotType",
			snapshot: Snapshot{
				Port:           "123",
				Sid:            "HDB",
				Project:        "",
				HanaDBUser:     "system",
				DiskZone:       "us-east1-a",
				PasswordSecret: "secret",
				SnapshotType:   "invalid",
			},
			wantErr: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.validateParameters(test.os, defaultCloudProperties)
			if !cmp.Equal(got, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(snapshot=%v, os=%v)=%v, want=%v", test.snapshot, test.os, got, test.wantErr)
			}
			if diff := cmp.Diff(test.wantSnapshot, test.snapshot, snapshotComparer); test.wantErr == nil && diff != "" {
				t.Errorf("validateParameters(snapshot=%v, os=%v) returned diff (-want +got):\n%s", test.snapshot, test.os, diff)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	s := Snapshot{
		Port:           "123",
		Sid:            "HDB",
		Project:        "",
		HanaDBUser:     "system",
		PasswordSecret: "secret",
		SnapshotType:   "STANDARD",
	}
	got := s.validateParameters("linux", defaultCloudProperties)
	if !cmp.Equal(got, nil, cmpopts.EquateErrors()) {
		t.Errorf("validateParameters(linux=%v)=%v, want=%v", got, got, nil)
	}
	if s.Project != "default-project" {
		t.Errorf("project = %v, want = %v", s.Project, "default-project")
	}

	if s.DiskZone != "default-zone" {
		t.Errorf("diskZone = %v, want = %v", s.DiskZone, "default-zone")
	}
}

func TestPortValue(t *testing.T) {
	s := Snapshot{
		Sid:            "HDB",
		InstanceID:     "00",
		HanaDBUser:     "system",
		Disk:           "pd-1",
		DiskZone:       "us-east1-a",
		PasswordSecret: "secret",
	}
	got := s.portValue()
	if got != "30013" {
		t.Errorf("portValue()=%v, want = %v", got, "0")
	}
}

func TestIsDiskAttachedToInstance(t *testing.T) {
	tests := []struct {
		name    string
		disk    string
		s       *Snapshot
		cp      *ipb.CloudProperties
		wantErr error
	}{
		{
			name: "AttachedDisk",
			s: &Snapshot{
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   true,
					DiskAttachedToInstanceErr:        nil,
				},
			},
			disk:    "pd-1",
			cp:      defaultCloudProperties,
			wantErr: nil,
		},
		{
			name: "NotAttachedDisk",
			s: &Snapshot{
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   false,
					DiskAttachedToInstanceErr:        nil,
				},
			},
			disk:    "pd-1",
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "AttachedDiskFailure",
			s: &Snapshot{
				gceService: &fake.TestGCE{
					DiskAttachedToInstanceDeviceName: "pd-1",
					IsDiskAttached:                   false,
					DiskAttachedToInstanceErr:        cmpopts.AnyError,
				},
			},
			disk:    "pd-1",
			cp:      defaultCloudProperties,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		tc.s.oteLogger = defaultOTELogger
		gotErr := tc.s.isDiskAttachedToInstance(context.Background(), tc.disk, tc.cp)
		if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
			t.Errorf("isDiskAttachedToInstance(%v, %v) returned diff (-want +got):\n%s", tc.disk, tc.cp, diff)
		}
	}
}

func TestRunWorkflowForDiskSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		snapshot       Snapshot
		run            queryFunc
		createSnapshot diskSnapshotFunc
		want           error
	}{
		{
			name: "CheckValidDiskFailure",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{DiskAttachedToInstanceErr: cmpopts.AnyError},
			},
			createSnapshot: createDiskSnapshotFail,
			want:           cmpopts.AnyError,
		},
		{
			name: "InvalidDisk",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{IsDiskAttached: false},
			},
			createSnapshot: createDiskSnapshotFail,
			want:           cmpopts.AnyError,
		},
		{
			name: "AbandonSnapshotFailure",
			snapshot: Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateHANASnapshotFailure",
			snapshot: Snapshot{
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
			want: cmpopts.AnyError,
		},
		{
			name: "CreateDiskSnapshotFailure",
			snapshot: Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateEncryptedDiskSnapshotFailure",
			snapshot: Snapshot{
				AbandonPrepared: true,
				DiskKeyFile:     "test.json",
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			createSnapshot: createDiskSnapshotFail,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ConfirmDataSnapshot",
			snapshot: Snapshot{
				AbandonPrepared:                true,
				ConfirmDataSnapshotAfterCreate: true,
				gceService: &fake.TestGCE{
					IsDiskAttached:      true,
					UploadCompletionErr: cmpopts.AnyError,
				},
				computeService: &compute.Service{},
			},
			createSnapshot: createDiskSnapshotSuccess,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "DoNotConfirmSnapshotAfterCreate",
			snapshot: Snapshot{
				AbandonPrepared:                true,
				ConfirmDataSnapshotAfterCreate: false,
				gceService: &fake.TestGCE{
					IsDiskAttached:      true,
					UploadCompletionErr: cmpopts.AnyError,
				},
				computeService: &compute.Service{},
			},
			createSnapshot: createDiskSnapshotSuccess,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "UploadSnapshotSuccess",
			snapshot: Snapshot{
				AbandonPrepared:                true,
				ConfirmDataSnapshotAfterCreate: true,
				gceService: &fake.TestGCE{
					IsDiskAttached: true,
				},
				computeService: &compute.Service{},
			},
			createSnapshot: createDiskSnapshotSuccess,
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "1234", nil
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.runWorkflowForDiskSnapshot(context.Background(), test.run, test.createSnapshot, defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("runWorkflow()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestCreateBackup(t *testing.T) {
	tests := []struct {
		name           string
		s              *Snapshot
		snapshot       *compute.Snapshot
		createSnapshot diskSnapshotFunc
		wantOp         *compute.Operation
		wantErr        error
	}{
		{
			name: "DiskKeyFile",
			s: &Snapshot{
				DiskKeyFile: "/test/disk/key.json",
			},
			wantOp:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "EmptyComputeService",
			s:       &Snapshot{},
			wantOp:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "FreezeFS",
			s: &Snapshot{
				computeService:   &compute.Service{},
				FreezeFileSystem: true,
			},
			wantOp:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CreateSnapshotFailure",
			s: &Snapshot{
				computeService: &compute.Service{},
				gceService:     &fake.TestGCE{CreationCompletionErr: cmpopts.AnyError},
			},
			createSnapshot: createDiskSnapshotFail,
			wantOp:         nil,
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "CreateSnapshotSuccess",
			s: &Snapshot{
				computeService: &compute.Service{},
				gceService:     &fake.TestGCE{CreationCompletionErr: nil},
			},
			createSnapshot: createDiskSnapshotSuccess,
			wantOp:         &compute.Operation{},
			wantErr:        nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got, err := tc.s.createBackup(ctx, tc.snapshot, tc.createSnapshot)
			if diff := cmp.Diff(tc.wantOp, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("buildSnapshot() returned diff (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("buildSnapshot() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAbandonPreparedSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		run      queryFunc
		snapshot Snapshot
		want     error
	}{
		{
			name: "ReadSnapshotIDError",
			run: func(context.Context, *databaseconnector.DBHandle, string) (string, error) {
				return "", cmpopts.AnyError
			},
			want: cmpopts.AnyError,
		},
		{
			name: "NoPreparedSnapshot",
			run: func(context.Context, *databaseconnector.DBHandle, string) (string, error) {
				return "", nil
			},
			want: nil,
		},
		{name: "PreparedSnapshotPresentAbandonFalse",
			run: func(context.Context, *databaseconnector.DBHandle, string) (string, error) {
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{
				AbandonPrepared: false,
			},
			want: cmpopts.AnyError,
		},
		{name: "PreparedSnapshotPresentAbandonTrue",
			run: func(context.Context, *databaseconnector.DBHandle, string) (string, error) {
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{
				AbandonPrepared: true,
			},
			want: nil,
		},
		{
			name: "AbandonSnapshotFailure",
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CLOSE") {
					return "", cmpopts.AnyError
				}
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{
				AbandonPrepared: true,
			},
			want: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.abandonPreparedSnapshot(context.Background(), test.run)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("abandonPreparedSnapshot()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestSynopsisForSnapshot(t *testing.T) {
	want := "invoke HANA backup using disk snapshots"
	snapshot := Snapshot{}
	got := snapshot.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForSnapshot(t *testing.T) {
	snapshot := Snapshot{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"project", "host", "port", "sid", "hana-db-user", "password", "password-secret",
		"hdbuserstore-key", "snapshot-name", "source-disk", "source-disk-zone", "source-disk-key-file", "group-snapshot-name",
		"snapshot-description", "send-metrics-to-monitoring", "storage-location", "confirm-data-snapshot-after-create"}
	snapshot.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestCreateNewHANASnapshot(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		run      queryFunc
		want     error
	}{
		{
			name: "EmptySourceSnapshot",
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateSnapshotFailure",
			snapshot: Snapshot{
				groupSnapshotName: "sample-group-snapshot",
			},
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ReadSnapshotIDError",
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				if strings.HasPrefix(q, "SELECT BACKUP_ID FROM M_BACKUP_CATALOG") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptySnapshotID",
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			run: func(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
				return "stale-snapshot", nil
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			_, got := test.snapshot.createNewHANASnapshot(context.Background(), test.run)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("createNewHANASnapshot()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestSendStatusToMonitoring(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		want     bool
	}{
		{
			name: "SendMetricsDisabled",
			snapshot: Snapshot{
				SendToMonitoring:  false,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
		},
		{
			name: "SendMetricsEnabled",
			snapshot: Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			want: true,
		},
		{
			name: "SendMetricsFailure",
			snapshot: Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{Err: cmpopts.AnyError},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.snapshot.oteLogger = defaultOTELogger
			got := test.snapshot.sendStatusToMonitoring(context.Background(), cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond), defaultCloudProperties)
			if got != test.want {
				t.Errorf("sendStatusToMonitoring()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestSendDurationToCloudMonitoring(t *testing.T) {
	tests := []struct {
		name         string
		mtype        string
		snapshotName string
		s            *Snapshot
		dur          time.Duration
		bo           *cloudmonitoring.BackOffIntervals
		want         bool
	}{
		{
			name:         "Success",
			mtype:        "Snapshot",
			snapshotName: "snapshot-name",
			s: &Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			dur:  time.Millisecond,
			bo:   cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
			want: true,
		},
		{
			name:         "Failure",
			mtype:        "Snapshot",
			snapshotName: "snapshot-name",
			s: &Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{Err: cmpopts.AnyError},
			},
			dur:  time.Millisecond,
			bo:   cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
			want: false,
		},
		{
			name:         "sendStatusFalse",
			mtype:        "Snapshot",
			snapshotName: "snapshot-name",
			s: &Snapshot{
				SendToMonitoring:  false,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			dur:  time.Millisecond,
			bo:   cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
			want: false,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got := tc.s.sendDurationToCloudMonitoring(ctx, tc.mtype, tc.snapshotName, tc.dur, tc.bo, defaultCloudProperties)
			if got != tc.want {
				t.Errorf("sendDurationToCloudMonitoring(%v, %v, %v) = %v, want: %v", tc.mtype, tc.dur, tc.bo, got, tc.want)
			}
		})
	}
}
