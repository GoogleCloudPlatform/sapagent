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

package onetime

import (
	"context"
	"database/sql"
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
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var defaultSnapshot = Snapshot{
	project:  "my-project",
	host:     "localhost",
	port:     "123",
	sid:      "HDB",
	user:     "system",
	disk:     "pd-1",
	diskZone: "us-east1-a",
	password: "password",
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
		{
			name:               "runWorkflowFailure",
			snapshot:           defaultSnapshot,
			fakeNewGCE:         func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			fakeComputeService: func(context.Context) (*compute.Service, error) { return &compute.Service{}, nil },
			want:               subcommands.ExitFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.snapshotHandler(context.Background(), test.fakeNewGCE, test.fakeComputeService)
			if got != test.want {
				t.Errorf("snapshotHandler(%v)=%v want %v", test.name, got, test.want)
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.Execute(context.Background(), &flag.FlagSet{}, test.args...)
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
			name:     "EmptyHost",
			snapshot: Snapshot{host: ""},
			want:     cmpopts.AnyError,
		},
		{
			name:     "EmptyPort",
			snapshot: Snapshot{host: "localhost", port: ""},
			want:     cmpopts.AnyError,
		},
		{
			name:     "EmptySID",
			snapshot: Snapshot{host: "localhost", port: "123", sid: ""},
			want:     cmpopts.AnyError,
		},
		{
			name:     "EmptyUser",
			snapshot: Snapshot{host: "localhost", port: "123", sid: "HDB", user: ""},
			want:     cmpopts.AnyError,
		},
		{
			name: "EmptyDisk",
			snapshot: Snapshot{
				host: "localhost",
				port: "123",
				sid:  "HDB",
				user: "system",
				disk: "",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskZone",
			snapshot: Snapshot{
				host:     "localhost",
				port:     "123",
				sid:      "HDB",
				user:     "system",
				disk:     "pd-1",
				diskZone: "",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyPasswordAndSecret",
			snapshot: Snapshot{
				host:           "localhost",
				port:           "123",
				sid:            "HDB",
				user:           "system",
				disk:           "pd-1",
				diskZone:       "us-east1-a",
				password:       "",
				passwordSecret: "",
			},
			want: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.validateParameters(test.os)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(snapshot=%v, os=%v)=%v, want=%v", test.snapshot, test.os, got, test.want)
			}
		})
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
			name: "AbandonSnapshotFailure",
			run: func(h *sql.DB, q string) (string, error) {
				return "", cmpopts.AnyError
			},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateHANASnapshotFailure",
			run: func(h *sql.DB, q string) (string, error) {
				if strings.HasPrefix(q, "BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT") {
					return "", cmpopts.AnyError
				}
				return "", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name:     "CreatePDSnapshotFailure",
			snapshot: Snapshot{abandonPrepared: true},
			run: func(h *sql.DB, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
		{
			name:     "CreateEncryptedPDSnapshotFailure",
			snapshot: Snapshot{abandonPrepared: true, diskKeyFile: "test.json"},
			run: func(h *sql.DB, q string) (string, error) {
				return "1234", nil
			},
			want: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.runWorkflow(context.Background(), test.run)
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
			snapshot: Snapshot{abandonPrepared: false},
			want:     cmpopts.AnyError,
		},
		{name: "PreparedSnapshotPresentAbandonTrue",
			run: func(*sql.DB, string) (string, error) {
				return "stale-snapshot", nil
			},
			snapshot: Snapshot{abandonPrepared: true},
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
			snapshot: Snapshot{abandonPrepared: true},
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
	flags := []string{"project", "host", "port", "sid", "user", "password", "password-secret",
		"snapshot-name", "source-disk", "source-disk-zone", "source-disk-key-file",
		"snapshot-description", "send-status-to-monitoring", "csek-key-file", "storage-location"}
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
				sendToMonitoring:  false,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
		},
		{
			name: "SendMetricsEnabled",
			snapshot: Snapshot{
				sendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{},
			},
			want: true,
		},
		{
			name: "SendMetricsFailure",
			snapshot: Snapshot{
				sendToMonitoring:  true,
				timeSeriesCreator: &cmFake.TimeSeriesCreator{Err: cmpopts.AnyError},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.snapshot.sendStatusToMonitoring(context.Background(), cloudmonitoring.NewBackOffIntervals(time.Millisecond, time.Millisecond))
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
