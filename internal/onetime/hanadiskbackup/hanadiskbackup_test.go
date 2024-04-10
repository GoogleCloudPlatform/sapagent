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
	"database/sql"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	cmFake "github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var defaultSnapshot = Snapshot{
	Project:    "my-project",
	Host:       "localhost",
	Port:       "123",
	Sid:        "HDB",
	HanaDBUser: "system",
	Disk:       "pd-1",
	DiskZone:   "us-east1-a",
	Password:   "password",
}

var (
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
}

type fakeSnapshot interface {
	isDiskAttachedToInstance(diskName string) (string, bool, error)
}

func TestSnapshotHandler(t *testing.T) {
	tests := []struct {
		name               string
		snapshot           Snapshot
		fakeNewGCE         gceServiceFunc
		fakeComputeService computeServiceFunc
		want               subcommands.ExitStatus
	}{
		{
			name:     "InvalidParams",
			snapshot: Snapshot{},
			want:     subcommands.ExitFailure,
		},
		{
			name:       "GCEServiceCreationFailure",
			snapshot:   defaultSnapshot,
			fakeNewGCE: func(context.Context) (*gce.GCE, error) { return nil, cmpopts.AnyError },
			want:       subcommands.ExitFailure,
		},
		{
			name:               "ComputeServiceCreationFailure",
			snapshot:           defaultSnapshot,
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return nil, cmpopts.AnyError },
			want:               subcommands.ExitFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.snapshotHandler(context.Background(), test.fakeNewGCE, test.fakeComputeService, defaultCloudProperties)
			if got != test.want {
				t.Errorf("snapshotHandler(%v)=%v want %v", test.name, got, test.want)
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
				labels: "label1,label2",
			},
			want: map[string]string{},
		},
		{
			name: "Success",
			s: Snapshot{
				labels: "label1=value1,label2=value2",
			},
			want: map[string]string{"label1": "value1", "label2": "value2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.parseLabels()
			if !cmp.Equal(got, test.want) {
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
			want:     subcommands.ExitFailure,
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
			want: subcommands.ExitFailure,
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
			name: "SuccessForVersion",
			snapshot: Snapshot{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.snapshot, test.args, got, test.want)
			}
		})
	}
}

func TestValidateParameters(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		os       string
		want     error
	}{
		{
			name: "WindowsUnSupported",
			os:   "windows",
			want: cmpopts.AnyError,
		},
		{
			name:     "EmptyPort",
			snapshot: Snapshot{Port: ""},
			want:     cmpopts.AnyError,
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
			},
			want: nil,
		},
		{
			name:     "EmptySID",
			snapshot: Snapshot{Port: "123", Sid: ""},
			want:     cmpopts.AnyError,
		},
		{
			name:     "EmptyUser",
			snapshot: Snapshot{Port: "123", Sid: "HDB", HanaDBUser: ""},
			want:     cmpopts.AnyError,
		},
		{
			name: "EmptyDisk",
			snapshot: Snapshot{
				Host:       "localhost",
				Port:       "123",
				Sid:        "HDB",
				HanaDBUser: "system",
				Disk:       "",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskZone",
			snapshot: Snapshot{
				Host:       "localhost",
				Port:       "123",
				Sid:        "HDB",
				HanaDBUser: "system",
				Disk:       "pd-1",
				DiskZone:   "",
			},
			want: cmpopts.AnyError,
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
			},
			want: cmpopts.AnyError,
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
			},
			want: cmpopts.AnyError,
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
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.validateParameters(test.os, defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(snapshot=%v, os=%v)=%v, want=%v", test.snapshot, test.os, got, test.want)
			}
		})
	}
}

func TestDefaultProject(t *testing.T) {
	s := Snapshot{
		Port:           "123",
		Sid:            "HDB",
		Project:        "",
		HanaDBUser:     "system",
		Disk:           "pd-1",
		DiskZone:       "us-east1-a",
		PasswordSecret: "secret",
	}
	got := s.validateParameters("linux", defaultCloudProperties)
	if !cmp.Equal(got, nil, cmpopts.EquateErrors()) {
		t.Errorf("validateParameters(linux=%v)=%v, want=%v", got, got, nil)
	}
	if s.Project != "default-project" {
		t.Errorf("project = %v, want = %v", s.Project, "default-project")
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

func TestRunWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		run      queryFunc
		want     error
	}{
		{
			name: "CheckValidDiskFailure",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{DiskAttachedToInstanceErr: cmpopts.AnyError},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "InvalidDisk",
			snapshot: Snapshot{
				gceService: &fake.TestGCE{IsDiskAttached: false},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "AbandonSnapshotFailure",
			snapshot: Snapshot{
				AbandonPrepared: true,
				gceService:      &fake.TestGCE{IsDiskAttached: true},
			},
			run: func(h *sql.DB, q string) (string, error) {
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
			run: func(h *sql.DB, q string) (string, error) {
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
			run: func(h *sql.DB, q string) (string, error) {
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
			run: func(h *sql.DB, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.runWorkflow(context.Background(), test.run, defaultCloudProperties)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("runWorkflow()=%v, want=%v", got, test.want)
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
			run: func(*sql.DB, string) (string, error) {
				return "", cmpopts.AnyError
			},
			want: cmpopts.AnyError,
		},
		{
			name: "NoPreparedSnaphot",
			run: func(*sql.DB, string) (string, error) {
				return "", nil
			},
			want: nil,
		},
		{name: "PreparedSnapshotPresentAbandonFalse",
			run: func(*sql.DB, string) (string, error) {
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{AbandonPrepared: false},
			want:     cmpopts.AnyError,
		},
		{name: "PreparedSnapshotPresentAbandonTrue",
			run: func(*sql.DB, string) (string, error) {
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{AbandonPrepared: true},
			want:     nil,
		},
		{
			name: "AbandonSnapshotFailure",
			run: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CLOSE") {
					return "", cmpopts.AnyError
				}
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{AbandonPrepared: true},
			want:     cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.abandonPreparedSnapshot(test.run)
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
		"snapshot-name", "source-disk", "source-disk-zone", "source-disk-key-file",
		"snapshot-description", "send-metrics-to-monitoring", "storage-location"}
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
			name: "CreateSnapshotFailure",
			run: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ReadSnapshotIDError",
			run: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "SELECT BACKUP_ID FROM M_BACKUP_CATALOG") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptySnapshotID",
			run: func(*sql.DB, string) (string, error) {
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			run: func(*sql.DB, string) (string, error) {
				return "stale-snapshot", nil
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := test.snapshot.createNewHANASnapshot(test.run)
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
			got := test.snapshot.sendStatusToMonitoring(context.Background(), cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond), defaultCloudProperties)
			if got != test.want {
				t.Errorf("sendStatusToMonitoring()=%v, want=%v", got, test.want)
			}
		})
	}
}

func TestReadKey(t *testing.T) {

	tests := []struct {
		name       string
		diskURI    string
		fakeReader configuration.ReadConfigFile
		wantKey    string
		wantErr    error
	}{
		{
			name:    "Success",
			diskURI: "https://www.googleapis.com/compute/v1/projects/myproject/zones/us-central1-a/disks/example-disk",
			fakeReader: func(string) ([]byte, error) {
				testKeyFileText := []byte(`[
					{
					"uri": "https://www.googleapis.com/compute/v1/projects/myproject/zones/us-central1-a/disks/example-disk",
					"key": "acXTX3rxrKAFTF0tYVLvydU1riRZTvUNC4g5I11NY+c=",
					"key-type": "raw"
					},
					{
					"uri": "https://www.googleapis.com/compute/v1/projects/myproject/global/snapshots/my-private-snapshot",
					"key": "ieCx/NcW06PcT7Ep1X6LUTc/hLvUDYyzSZPPVCVPTV=",
					"key-type": "rsa-encrypted"
					}
				]`)
				return testKeyFileText, nil
			},
			wantKey: `acXTX3rxrKAFTF0tYVLvydU1riRZTvUNC4g5I11NY+c=`,
		},
		{
			name:       "RedFileFailure",
			fakeReader: func(string) ([]byte, error) { return nil, cmpopts.AnyError },
			wantErr:    cmpopts.AnyError,
		},
		{
			name:       "MalformedJSON",
			fakeReader: func(string) ([]byte, error) { return []byte(`[[]}`), nil },
			wantErr:    cmpopts.AnyError,
		},
		{
			name:    "NoMatchingKey",
			diskURI: "https://www.googleapis.com/compute/v1/projects/myproject/zones/us-central1-a/disks/example-disk",
			fakeReader: func(string) ([]byte, error) {
				testKeyFileText := []byte(`[
					{
					"uri": "https://www.googleapis.com/compute/v1/projects/myproject/global/snapshots/my-private-snapshot",
					"key": "ieCx/NcW06PcT7Ep1X6LUTc/hLvUDYyzSZPPVCVPTV=",
					"key-type": "rsa-encrypted"
					}
				]`)
				return testKeyFileText, nil
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readKey("", test.diskURI, test.fakeReader)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("readKey()=%v, want=%v", err, test.wantErr)
			}
			if got != test.wantKey {
				t.Errorf("readKey()=%v, want=%v", got, test.wantKey)
			}
		})
	}
}

func TestCheckDataDeviceForStripes(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     error
	}{
		{
			name:     "SpripesPresent",
			fakeExec: testCommandExecute("", "", nil),
			want:     cmpopts.AnyError,
		},
		{
			name:     "StripesNotPresent",
			fakeExec: testCommandExecute("", "exit code:1", &exec.ExitError{}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Snapshot{}
			got := r.checkDataDeviceForStripes(context.Background(), test.fakeExec)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("checkDataDeviceForStripes() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseBasePath(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     string
		wantErr  error
	}{
		{
			name:     "Failure",
			fakeExec: testCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: testCommandExecute("/hana/data/ABC", "", nil),
			want:     "/hana/data/ABC",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Snapshot{}
			got, gotErr := r.parseBasePath(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parseBasePath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("parseBasePath() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParsePhysicalPath(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     string
		wantErr  error
	}{
		{
			name:     "Failure",
			fakeExec: testCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: testCommandExecute("/dev/sdb", "", nil),
			want:     "/dev/sdb",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Snapshot{}
			got, gotErr := r.parsePhysicalPath(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parsePhysicalPath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("parsePhysicalPath() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseLogicalPath(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     string
		wantErr  error
	}{
		{
			name:     "Failure",
			fakeExec: testCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: testCommandExecute("/dev/mapper/vg-volume-1", "", nil),
			want:     "/dev/mapper/vg-volume-1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Snapshot{}
			got, gotErr := r.parseLogicalPath(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parseLogicalPath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("parseLogicalPath() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSendDurationToCloudMonitoring(t *testing.T) {
	tests := []struct {
		name  string
		mtype string
		s     *Snapshot
		dur   time.Duration
		bo    *cloudmonitoring.BackOffIntervals
		want  bool
	}{
		{
			name:  "Success",
			mtype: "Snapshot",
			s: &Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			dur:  time.Millisecond,
			bo:   cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
			want: true,
		},
		{
			name:  "Failure",
			mtype: "Snapshot",
			s: &Snapshot{
				SendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{Err: cmpopts.AnyError},
			},
			dur:  time.Millisecond,
			bo:   cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond),
			want: false,
		},
		{
			name:  "sendStatusFalse",
			mtype: "Snapshot",
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
			got := tc.s.sendDurationToCloudMonitoring(ctx, tc.mtype, tc.dur, tc.bo, defaultCloudProperties)
			if got != tc.want {
				t.Errorf("sendDurationToCloudMonitoring(%v, %v, %v) = %v, want: %v", tc.mtype, tc.dur, tc.bo, got, tc.want)
			}
		})
	}
}
