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

package restore

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"google3/third_party/sapagent/shared/log"
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
		name     string
		restorer Restorer
		os       string
		want     error
	}{
		{
			name: "WindowsUnSupported",
			os:   "windows",
			want: cmpopts.AnyError,
		},
		{
			name:     "EmptyProject",
			restorer: Restorer{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "EmptySID",
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
			name: "NewDiskTypeSet",
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
			got := r.stopHANA(context.Background(), test.fakeExec)
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Execute(context.Background(), nil, test.testArgs...)
			if got != test.want {
				t.Errorf("Execute() = %v, want %v", got, test.want)
			}
		})
	}
}
