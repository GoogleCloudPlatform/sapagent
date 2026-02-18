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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
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

func TestRescanVolumeGroups(t *testing.T) {
	tests := []struct {
		name          string
		mockResults   []commandlineexecutor.Result
		wantErr       error
		wantExecCalls int
	}{
		{
			name: "Success",
			mockResults: []commandlineexecutor.Result{
				{ /* dmsetup remove_all */ },
				{ /* vgscan */ },
				{ /* vgchange */ },
				{ /* lvscan */ },
				{ /* mount -av */ },
			},
			wantErr:       nil,
			wantExecCalls: 5,
		},
		{
			name: "DmsetupRemoveAllFails",
			mockResults: []commandlineexecutor.Result{
				{Error: errors.New("dmsetup failed"), ExitCode: 1},
			},
			wantErr:       cmpopts.AnyError,
			wantExecCalls: 1,
		},
		{
			name: "VgscanFails",
			mockResults: []commandlineexecutor.Result{
				{ /* dmsetup remove_all */ },
				{Error: errors.New("vgscan failed"), ExitCode: 1},
			},
			wantErr:       cmpopts.AnyError,
			wantExecCalls: 2,
		},
		{
			name: "VgchangeFails",
			mockResults: []commandlineexecutor.Result{
				{ /* dmsetup remove_all */ },
				{ /* vgscan */ },
				{Error: errors.New("vgchange failed"), ExitCode: 1},
			},
			wantErr:       cmpopts.AnyError,
			wantExecCalls: 3,
		},
		{
			name: "LvscanFails",
			mockResults: []commandlineexecutor.Result{
				{ /* dmsetup remove_all */ },
				{ /* vgscan */ },
				{ /* vgchange */ },
				{Error: errors.New("lvscan failed"), ExitCode: 1},
			},
			wantErr:       cmpopts.AnyError,
			wantExecCalls: 4,
		},
		{
			name: "MountAvFails",
			mockResults: []commandlineexecutor.Result{
				{ /* dmsetup remove_all */ },
				{ /* vgscan */ },
				{ /* vgchange */ },
				{ /* lvscan */ },
				{Error: errors.New("mount failed"), ExitCode: 1},
			},
			wantErr:       cmpopts.AnyError,
			wantExecCalls: 5,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			mockExec := func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				if callCount >= len(tc.mockResults) {
					t.Fatalf("Execute called more times than expected. Call: %d, Expected max: %d", callCount+1, len(tc.mockResults))
				}
				res := tc.mockResults[callCount]
				callCount++
				return res
			}

			err := RescanVolumeGroups(ctx, mockExec)

			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("RescanVolumeGroups() error = %v, wantErr %v", err, tc.wantErr)
			}

			if callCount != tc.wantExecCalls {
				t.Errorf("RescanVolumeGroups() mockExec was called %d times, want %d", callCount, tc.wantExecCalls)
			}
		})
	}
}

func TestWaitForIndexServerToStopWithRetry(t *testing.T) {
	tests := []struct {
		name                string
		sid                 string
		maxRetries          int
		execResultExitCodes []int
		wantErr             error
		wantExecCalls       int
	}{
		{
			name:                "SuccessOnFirstAttempt",
			sid:                 "TST",
			execResultExitCodes: []int{1},
			wantErr:             nil,
			wantExecCalls:       1,
		},
		{
			name:                "SuccessOnThirdAttempt",
			sid:                 "TST",
			execResultExitCodes: []int{0, 0, 1},
			wantErr:             nil,
			wantExecCalls:       3,
		},
		{
			name:                "MaxRetriesIsOne_Success",
			sid:                 "TST",
			execResultExitCodes: []int{1},
			wantErr:             nil,
			wantExecCalls:       1,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			mockExec := func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				if callCount >= len(tc.execResultExitCodes) {
					t.Fatalf("Execute called more times than specified in execResultExitCodes. Call: %d, Expected max: %d", callCount+1, len(tc.execResultExitCodes))
				}
				currentExitCode := tc.execResultExitCodes[callCount]
				callCount++

				var cmdErr error
				if currentExitCode == 1 {
					cmdErr = &exec.ExitError{}
				} else if currentExitCode == 0 {
					cmdErr = nil
				}
				return commandlineexecutor.Result{
					Error:    cmdErr,
					ExitCode: currentExitCode,
				}
			}
			err := WaitForIndexServerToStopWithRetry(ctx, tc.sid, mockExec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("WaitForIndexServerToStopWithRetry() error = %v, wantErr %v", err, tc.wantErr)
			}
			if callCount != tc.wantExecCalls {
				t.Errorf("WaitForIndexServerToStopWithRetry() mockExec was called %d times, want %d", callCount, tc.wantExecCalls)
			}
		})
	}
}

func TestFreezeXFS(t *testing.T) {
	tests := []struct {
		name    string
		exec    commandlineexecutor.Execute
		wantErr error
	}{
		{
			name: "Success",
			exec: fakeCommandExecute("", "", nil),
		},
		{
			name:    "Failure",
			exec:    fakeCommandExecute("", "error freezing", &exec.ExitError{}),
			wantErr: cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := FreezeXFS(context.Background(), "/hana/data", tc.exec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("FreezeXFS() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestUnFreezeXFS(t *testing.T) {
	tests := []struct {
		name    string
		exec    commandlineexecutor.Execute
		wantErr error
	}{
		{
			name: "Success",
			exec: fakeCommandExecute("", "", nil),
		},
		{
			name:    "Failure",
			exec:    fakeCommandExecute("", "error unfreezing", &exec.ExitError{}),
			wantErr: cmpopts.AnyError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := UnFreezeXFS(context.Background(), "/hana/data", tc.exec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("UnFreezeXFS() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestCheckDataDir(t *testing.T) {
	tests := []struct {
		name                 string
		exec                 commandlineexecutor.Execute
		wantDataPath         string
		wantLogicalDataPath  string
		wantPhysicalDataPath string
		wantErr              error
	}{
		{
			name: "ParseBasePathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{Error: errors.New("grep failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ParseLogicalPathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/data/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{Error: errors.New("df failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantDataPath: "/hana/data/SID",
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "NotLVM",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/data/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{StdOut: "/dev/sda1"}
				}
				return commandlineexecutor.Result{}
			},
			wantDataPath: "/hana/data/SID",
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "ParsePhysicalPathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/data/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{StdOut: "/dev/mapper/vg-lv"}
				}
				if strings.Contains(params.ArgsToSplit, "lvdisplay -m") {
					return commandlineexecutor.Result{Error: errors.New("lvdisplay failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantDataPath:        "/hana/data/SID",
			wantLogicalDataPath: "/dev/mapper/vg-lv",
			wantErr:             cmpopts.AnyError,
		},
		{
			name: "Success",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/data/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{StdOut: "/dev/mapper/vg-lv"}
				}
				if strings.Contains(params.ArgsToSplit, "lvdisplay -m") {
					return commandlineexecutor.Result{StdOut: "/dev/sdb"}
				}
				return commandlineexecutor.Result{}
			},
			wantDataPath:         "/hana/data/SID",
			wantLogicalDataPath:  "/dev/mapper/vg-lv",
			wantPhysicalDataPath: "/dev/sdb",
			wantErr:              nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dp, ldp, pdp, err := CheckDataDir(context.Background(), tc.exec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("CheckDataDir() error = %v, wantErr %v", err, tc.wantErr)
			}
			if dp != tc.wantDataPath {
				t.Errorf("CheckDataDir() dataPath = %s, want %s", dp, tc.wantDataPath)
			}
			if ldp != tc.wantLogicalDataPath {
				t.Errorf("CheckDataDir() logicalDataPath = %s, want %s", ldp, tc.wantLogicalDataPath)
			}
			if pdp != tc.wantPhysicalDataPath {
				t.Errorf("CheckDataDir() physicalDataPath = %s, want %s", pdp, tc.wantPhysicalDataPath)
			}
		})
	}
}

func TestCheckLogDir(t *testing.T) {
	tests := []struct {
		name                string
		exec                commandlineexecutor.Execute
		wantBasePath        string
		wantLogicalLogPath  string
		wantPhysicalLogPath string
		wantErr             error
	}{
		{
			name: "ParseBasePathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{Error: errors.New("grep failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantBasePath:        "",
			wantLogicalLogPath:  "",
			wantPhysicalLogPath: "",
			wantErr:             cmpopts.AnyError,
		},
		{
			name: "ParseLogicalPathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/log/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{Error: errors.New("df failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantBasePath: "/hana/log/SID",
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "ParsePhysicalPathFails",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/log/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{StdOut: "/dev/mapper/vg-lv"}
				}
				if strings.Contains(params.ArgsToSplit, "lvdisplay -m") {
					return commandlineexecutor.Result{Error: errors.New("lvdisplay failed"), ExitCode: 1}
				}
				return commandlineexecutor.Result{}
			},
			wantBasePath:       "/hana/log/SID",
			wantLogicalLogPath: "/dev/mapper/vg-lv",
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "Success",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if strings.Contains(params.ArgsToSplit, "global.ini") {
					return commandlineexecutor.Result{StdOut: "/hana/log/SID"}
				}
				if strings.Contains(params.ArgsToSplit, "df --output=source") {
					return commandlineexecutor.Result{StdOut: "/dev/mapper/vg-lv"}
				}
				if strings.Contains(params.ArgsToSplit, "lvdisplay -m") {
					return commandlineexecutor.Result{StdOut: "/dev/sdc"}
				}
				return commandlineexecutor.Result{}
			},
			wantBasePath:        "/hana/log/SID",
			wantLogicalLogPath:  "/dev/mapper/vg-lv",
			wantPhysicalLogPath: "/dev/sdc",
			wantErr:             nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bpl, llp, plp, err := CheckLogDir(context.Background(), tc.exec)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("CheckLogDir() error = %v, wantErr %v", err, tc.wantErr)
			}
			if bpl != tc.wantBasePath {
				t.Errorf("CheckLogDir() baseLogPath = %s, want %s", bpl, tc.wantBasePath)
			}
			if llp != tc.wantLogicalLogPath {
				t.Errorf("CheckLogDir() logicalLogPath = %s, want %s", llp, tc.wantLogicalLogPath)
			}
			if plp != tc.wantPhysicalLogPath {
				t.Errorf("CheckLogDir() physicalLogPath = %s, want %s", plp, tc.wantPhysicalLogPath)
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
		{
			name:     "EmptyPhysicalDevice",
			fakeExec: fakeCommandExecute("", "", nil),
			want:     "",
			wantErr:  cmpopts.AnyError,
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

func TestCheckTopology(t *testing.T) {
	tests := []struct {
		name           string
		exec           commandlineexecutor.Execute
		sid            string
		isSidadmUser   bool
		instanceNumber string
		want           bool
		wantErr        error
	}{
		{
			name: "InstanceNotFound",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "",
					Error:    cmpopts.AnyError,
					ExitCode: 1,
				}
			},
			sid:     "SID",
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "SapcontrolCommandError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "grep" {
					return commandlineexecutor.Result{
						StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
						StdErr:   "",
						Error:    nil,
						ExitCode: 0,
					}
				}
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "sapcontrol failed",
					Error:    errors.New("sapcontrol execution error"),
					ExitCode: 1,
				}
			},
			sid:            "SID",
			instanceNumber: "00",
			want:           false,
			wantErr:        cmpopts.AnyError,
		},
		{
			name: "NoSAPInstancesFound",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "grep" {
					return commandlineexecutor.Result{
						StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
						StdErr:   "",
						Error:    nil,
						ExitCode: 0,
					}
				}
				return commandlineexecutor.Result{
					StdOut: `
					17.11.2024 07:57:08 GetSystemInstanceList
					OK
					hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus`,
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			sid:     "SID",
			want:    false,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScaleoutTopology",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "grep" {
					return commandlineexecutor.Result{
						StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
						StdErr:   "",
						Error:    nil,
						ExitCode: 0,
					}
				}
				return commandlineexecutor.Result{
					StdOut: `
					17.11.2024 07:57:08
					GetSystemInstanceList
					OK
					hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
					rb-scaleout, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
					rb-scaleoutw1, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
					`,
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			sid:     "SID",
			want:    true,
			wantErr: nil,
		},
		{
			name:         "ScaleupTopology",
			isSidadmUser: true,
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "grep" {
					return commandlineexecutor.Result{
						StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
						StdErr:   "",
						Error:    nil,
						ExitCode: 0,
					}
				}
				return commandlineexecutor.Result{
					StdOut: `
					17.11.2024 07:57:08
					GetSystemInstanceList
					OK
					hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
					rb-scaleout, 12, 51213, 51214, 0.3, HDB|HDB_WORKER, GREEN
					`,
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			sid:     "SID",
			want:    false,
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckTopology(ctx, tc.exec, tc.sid, tc.isSidadmUser, tc.instanceNumber)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("CheckTopology(%v, %q) = %v, want: %v", tc.exec, tc.sid, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("CheckTopology(%v, %q) = %v, want: %v", tc.exec, tc.sid, got, tc.want)
			}
		})
	}
}

func TestGetInstanceNumber(t *testing.T) {
	tests := []struct {
		name               string
		exec               commandlineexecutor.Execute
		SID                string
		wantInstanceNumber string
		wantErr            error
	}{
		{
			name: "Failure",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "",
					Error:    cmpopts.AnyError,
					ExitCode: 1,
				}
			},
			SID:     "SID",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoInstanceFound",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			SID:                "SID",
			wantInstanceNumber: "",
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "Success",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "systemctl --no-ask-password start SAPSID_00 # sapstartsrv pf=/usr/sap/SID/SYS/profile/SID_HDB00_my-instance\n",
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			SID:                "SID",
			wantInstanceNumber: "00",
			wantErr:            nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getInstanceNumber(ctx, tc.exec, tc.SID)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getInstanceNumber(%v, %q) = %v, want: %v", tc.exec, tc.SID, err, tc.wantErr)
			}
			if got != tc.wantInstanceNumber {
				t.Errorf("getInstanceNumber(%v, %q) = %v, want: %v", tc.exec, tc.SID, got, tc.wantInstanceNumber)
			}
		})
	}
}

func TestUnmount(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		exec       commandlineexecutor.Execute
		isScaleout bool
		wantErr    error
	}{
		{
			name: "Failure",
			path: "/hana/data",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "any error",
					Error:    cmpopts.AnyError,
					ExitCode: 1,
				}
			},
			isScaleout: false,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "ScaleupAlreadyUnmounted",
			path: "/hana/data",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "umount: /hana/data: not mounted.",
					Error:    cmpopts.AnyError,
					ExitCode: 32,
				}
			},
			isScaleout: false,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "ScaleoutAlreadyUnmounted",
			path: "/hana/data",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "umount: /hana/data: not mounted.",
					Error:    cmpopts.AnyError,
					ExitCode: 32,
				}
			},
			isScaleout: true,
			wantErr:    nil,
		},
		{
			name: "ScaleoutUnmountError",
			path: "/hana/data",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "umount: /hana/data: device target busy.",
					Error:    cmpopts.AnyError,
					ExitCode: 32,
				}
			},
			isScaleout: true,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "Success",
			path: "/hana/data",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "",
					StdErr:   "",
					Error:    nil,
					ExitCode: 0,
				}
			},
			isScaleout: false,
			wantErr:    nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := Unmount(ctx, tc.path, tc.exec, tc.isScaleout)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("unmount(%q, %v, %v) = %v, want: %v", tc.path, tc.exec, tc.isScaleout, gotErr, tc.wantErr)
			}
		})
	}
}
