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

package hanabackup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	fakeCommandExecute = func(stdout, stderr string, err error) commandlineexecutor.Execute {
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

	fakeCommandExecuteWithExitCode = func(stdout, stderr string, exitCode int, err error) commandlineexecutor.Execute {
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

func TestParseBasePath(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     string
		wantErr  error
	}{
		{
			name:     "Failure",
			fakeExec: fakeCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: fakeCommandExecute("/hana/data/ABC", "", nil),
			want:     "/hana/data/ABC",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ParseBasePath(context.Background(), "", test.fakeExec)
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
			fakeExec: fakeCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: fakeCommandExecute("/dev/sdb", "", nil),
			want:     "/dev/sdb",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ParsePhysicalPath(context.Background(), "", test.fakeExec)
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
			fakeExec: fakeCommandExecute("", "", &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: fakeCommandExecute("/dev/mapper/vg-volume-1", "", nil),
			want:     "/dev/mapper/vg-volume-1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ParseLogicalPath(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("parseLogicalPath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("parseLogicalPath() = %v, want %v", got, test.want)
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
			got, err := ReadKey("", test.diskURI, test.fakeReader)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ReadKey()=%v, want=%v", err, test.wantErr)
			}
			if got != test.wantKey {
				t.Errorf("ReadKey()=%v, want=%v", got, test.wantKey)
			}
		})
	}
}

func TestCheckDataDeviceForStripes(t *testing.T) {
	tests := []struct {
		name        string
		fakeExec    commandlineexecutor.Execute
		wantStriped bool
		wantErr     error
	}{
		{
			name:        "StripesErr",
			fakeExec:    fakeCommandExecuteWithExitCode("", "", 1, &exec.ExitError{}),
			wantStriped: false,
			wantErr:     cmpopts.AnyError,
		},
		{
			name:        "StripesPresent",
			fakeExec:    fakeCommandExecuteWithExitCode("", "", 0, nil),
			wantStriped: true,
			wantErr:     nil,
		},
		{
			name:        "StripesNotPresent",
			fakeExec:    fakeCommandExecuteWithExitCode("", "", 1, nil),
			wantStriped: false,
			wantErr:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotStriped, gotErr := CheckDataDeviceForStripes(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("checkDataDeviceForStripes() = %v, want %v", gotErr, test.wantErr)
			}
			if gotStriped != test.wantStriped {
				t.Errorf("checkDataDeviceForStripes() = %v, want %v", gotStriped, test.wantStriped)
			}
		})
	}
}

func TestWaitForIndexServerToStop(t *testing.T) {
	tests := []struct {
		name     string
		fakeExec commandlineexecutor.Execute
		want     error
	}{
		{
			name:     "ProcessRunning",
			fakeExec: fakeCommandExecuteWithExitCode("", "", 0, nil),
			want:     cmpopts.AnyError,
		},
		{
			name:     "ProcessStopped",
			fakeExec: fakeCommandExecuteWithExitCode("", "", 1, nil),
			want:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := waitForIndexServerToStop(context.Background(), "SID", test.fakeExec)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("waitForIndexServerToStop() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStopHANA(t *testing.T) {
	tests := []struct {
		name      string
		forceStop bool
		fakeExec  commandlineexecutor.Execute
		want      error
	}{
		{
			name:     "Failure",
			fakeExec: fakeCommandExecuteWithExitCode("", "", 1, &exec.ExitError{}),
			want:     cmpopts.AnyError,
		},
		{
			name:     "StopSuccess",
			fakeExec: fakeCommandExecuteWithExitCode("", "", 0, nil),
		},
		{
			name:      "ForceStopSuccess",
			forceStop: true,
			fakeExec:  fakeCommandExecuteWithExitCode("", "", 0, nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := StopHANA(context.Background(), test.forceStop, "sidadm", "sid", test.fakeExec)
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
			fakeExec: fakeCommandExecuteWithExitCode("", "", 1, &exec.ExitError{}),
			wantErr:  cmpopts.AnyError,
		},
		{
			name:     "Success",
			fakeExec: fakeCommandExecuteWithExitCode("/hana/data/ABC", "", 0, nil),
			want:     "/hana/data/ABC",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ReadDataDirMountPath(context.Background(), "", test.fakeExec)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ReadDataDirMountPath() = %v, want %v", gotErr, test.wantErr)
			}
			if got != test.want {
				t.Errorf("ReadDataDirMountPath() = %v, want %v", got, test.want)
			}
		})
	}
}
