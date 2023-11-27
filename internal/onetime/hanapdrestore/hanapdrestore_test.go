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

package hanapdrestore

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	defaultRestorer = Restorer{
		project:        "my-project",
		sid:            "my-sid",
		user:           "my-user",
		dataDiskName:   "data-disk",
		dataDiskZone:   "data-zone",
		newDiskType:    "pd-ssd",
		sourceSnapshot: "my-snapshot",
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
			name:     "Emptyproject",
			restorer: Restorer{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "Emptysid",
			restorer: Restorer{project: "my-project"},
			want:     cmpopts.AnyError,
		},
		{
			name: "EmptyDiskType",
			restorer: Restorer{
				project:        "my-project",
				sid:            "my-sid",
				user:           "my-user",
				dataDiskName:   "data-disk",
				dataDiskZone:   "data-zone",
				sourceSnapshot: "snapshot",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "newDiskTypeSet",
			restorer: Restorer{
				project:        "my-project",
				sid:            "my-sid",
				user:           "my-user",
				dataDiskName:   "data-disk",
				dataDiskZone:   "data-zone",
				sourceSnapshot: "snapshot",
				newDiskType:    "pd-ssd",
			},
		},
		{
			name: "Emptyproject",
			restorer: Restorer{
				sid:            "tst",
				dataDiskName:   "data-disk",
				dataDiskZone:   "data-zone",
				sourceSnapshot: "snapshot",
				newDiskType:    "pd-ssd",
			},
		},
		{
			name: "Emptyuser",
			restorer: Restorer{
				project:        "my-project",
				sid:            "tst",
				dataDiskName:   "data-disk",
				dataDiskZone:   "data-zone",
				sourceSnapshot: "snapshot",
				newDiskType:    "pd-ssd",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.restorer.validateParameters(test.os)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(%q) = %v, want %v", test.os, got, test.want)
			}
		})
	}
}

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId: "default-project",
}

func TestDefaultValues(t *testing.T) {
	r := Restorer{
		sid:            "hdb",
		sourceSnapshot: "source-snapshot",
		dataDiskName:   "data-disk-name",
		dataDiskZone:   "data-disk-zone",
		newDiskType:    "new-disk-type",
		project:        "",
		cloudProps:     defaultCloudProperties,
	}
	got := r.validateParameters("linux")
	if got != nil {
		t.Errorf("validateParameters()=%v, want=%v", got, nil)
	}
	if r.project != "default-project" {
		t.Errorf("project = %v, want = %v", r.project, "default-project")
	}
	if r.user != "hdbadm" {
		t.Errorf("user = %v, want = %v", r.user, "hdbadm")
	}
}

func TestRestoreHandler(t *testing.T) {
	tests := []struct {
		name               string
		restorer           Restorer
		fakeComputeService computeServiceFunc
		want               subcommands.ExitStatus
	}{
		{
			name:     "InvalidParameters",
			restorer: Restorer{},
			want:     subcommands.ExitFailure,
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
			got := test.restorer.restoreHandler(context.Background(), test.fakeComputeService)
			if got != test.want {
				t.Errorf("restoreHandler() = %v, want %v", got, test.want)
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
			r := &Restorer{}
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
			r := &Restorer{}
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
			r := &Restorer{}
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
			r := &Restorer{}
			got := r.checkDataDeviceForStripes(context.Background(), test.fakeExec)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("checkDataDeviceForStripes() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStopHANA(t *testing.T) {
	tests := []struct {
		name     string
		r        *Restorer
		fakeExec commandlineexecutor.Execute
		want     error
	}{
		{
			name:     "Failure",
			r:        &Restorer{},
			fakeExec: testCommandExecute("", "", &exec.ExitError{}),
			want:     cmpopts.AnyError,
		},
		{
			name:     "StopSuccess",
			r:        &Restorer{},
			fakeExec: testCommandExecute("", "", nil),
		},
		{
			name:     "ForceStopSuccess",
			r:        &Restorer{forceStopHANA: true},
			fakeExec: testCommandExecute("", "", nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.stopHANA(context.Background(), test.fakeExec)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("stopHANA() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestReadDataDirMountPath(t *testing.T) {
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
			r := &Restorer{}
			got, gotErr := r.readDataDirMountPath(context.Background(), test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("readDataDirMountPath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("readDataDirMountPath() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUnmount(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     error
	}{
		{
			name:     "Failure",
			fakeExec: testCommandExecute("", "", &exec.ExitError{}),
			want:     cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: testCommandExecute("", "", nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &Restorer{}
			got := r.unmount(context.Background(), "", test.fakeExec)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("unmount() = %v, want %v", got, test.want)
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

func TestSynopsisForRestorer(t *testing.T) {
	want := "invoke HANA hanapdrestore using worklfow to restore from persistent disk snapshot"
	snapshot := Restorer{}
	got := snapshot.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForSnapshot(t *testing.T) {
	snapshot := Restorer{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"sid", "source-snapshot", "data-disk-name", "data-disk-zone", "project", "new-disk-type", "user"}
	snapshot.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}
