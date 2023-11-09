/*
Copyright 2022 Google LLC

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

package commandlineexecutor

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func setDefaults() {
	exists = CommandExists
	exitCode = commandExitCode
	run = nil
	exeForPlatform = nil
}

func TestExecuteCommandWithArgsToSplit(t *testing.T) {
	input := []struct {
		name    string
		cmd     string
		args    string
		wantOut string
		wantErr string
	}{
		{
			name:    "echo",
			cmd:     "echo",
			args:    "hello, world",
			wantOut: "hello, world\n",
			wantErr: "",
		},
		{
			name:    "bashMd5sum",
			cmd:     "bash",
			args:    "-c 'echo $0 | md5sum' 'test hashing functions'",
			wantOut: "664ff1cd20fea05ce7bbbf36ef0be984  -\n",
			wantErr: "",
		},
		{
			name:    "bashSha1sum",
			cmd:     "bash",
			args:    "-c 'echo test | sha1sum'",
			wantOut: "4e1243bd22c66e76c2ba9eddc1f91394e57f9f83  -\n",
			wantErr: "",
		},
	}
	for _, test := range input {
		t.Run(test.name, func(t *testing.T) {
			setDefaults()
			result := ExecuteCommand(context.Background(), Params{
				Executable:  test.cmd,
				ArgsToSplit: test.args,
			})
			if result.Error != nil {
				t.Fatalf("ExecuteCommand with argstosplit returned unexpected error: %v", result.Error)
			}
			if diff := cmp.Diff(test.wantOut, result.StdOut); diff != "" {
				t.Fatalf("ExecuteCommand with argstosplit returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, result.StdErr); diff != "" {
				t.Fatalf("ExecuteCommand with argstosplit returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExecuteCommandWithArgs(t *testing.T) {
	input := []struct {
		name    string
		cmd     string
		args    []string
		wantOut string
		wantErr string
	}{
		{
			name:    "echo",
			cmd:     "echo",
			args:    []string{"hello, world"},
			wantOut: "hello, world\n",
			wantErr: "",
		},
		{
			name:    "env",
			cmd:     "env",
			args:    []string{"--", "echo", "test | sha1sum"},
			wantOut: "test | sha1sum\n",
			wantErr: "",
		},
		{
			name:    "bashSha1sum",
			cmd:     "bash",
			args:    []string{"-c", "echo $0$1$2 | sha1sum", "section1,", "section2,", "section3"},
			wantOut: "28b0c645455ccefe28044c7e24eaeb8e80aa40d4  -\n",
			wantErr: "",
		},
		{
			name:    "shMd5sum",
			cmd:     "sh",
			args:    []string{"-c", "echo $0 | md5sum", "test hashing functions with sh"},
			wantOut: "664a07d6f2be061a942820847888865d  -\n",
			wantErr: "",
		},
	}
	for _, test := range input {
		t.Run(test.name, func(t *testing.T) {
			setDefaults()
			result := ExecuteCommand(context.Background(), Params{
				Executable: test.cmd,
				Args:       test.args,
			})
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if diff := cmp.Diff(test.wantOut, result.StdOut); diff != "" {
				t.Fatalf("ExecuteCommand with args returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, result.StdErr); diff != "" {
				t.Fatalf("ExecuteCommand with args returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandExists(t *testing.T) {
	input := []struct {
		name   string
		cmd    string
		exists bool
	}{
		{
			name:   "echoExists",
			cmd:    "echo",
			exists: true,
		},
		{
			name:   "lsExists",
			cmd:    "ls",
			exists: true,
		},
		{
			name:   "encryptDoesNotExist",
			cmd:    "encrypt",
			exists: false,
		},
	}
	for _, test := range input {
		t.Run(test.name, func(t *testing.T) {
			setDefaults()
			if got := CommandExists(test.cmd); got != test.exists {
				t.Fatalf("CommandExists returned unexpected result, got: %t want: %t", got, test.exists)
			}
		})
	}
}

func TestExecuteCommandAsUser(t *testing.T) {
	tests := []struct {
		name         string
		cmd          string
		fakeExists   Exists
		fakeRun      Run
		fakeExitCode ExitCode
		fakeSetupExe SetupExeForPlatform
		wantExitCode int64
		wantErr      error
	}{
		{
			name:         "ExistingCmd",
			cmd:          "ls",
			fakeExists:   func(string) bool { return true },
			fakeRun:      func() error { return nil },
			fakeSetupExe: func(exe *exec.Cmd, params Params) error { return nil },
			wantErr:      nil,
		},
		{
			name:         "NonExistingCmd",
			cmd:          "encrypt",
			fakeExists:   func(string) bool { return false },
			wantExitCode: 0,
			wantErr:      cmpopts.AnyError,
		},
		{
			name:         "ExitCode15",
			cmd:          "ls",
			fakeExists:   func(string) bool { return true },
			fakeRun:      func() error { return fmt.Errorf("some failure") },
			fakeExitCode: func(error) int { return 15 },
			fakeSetupExe: func(exe *exec.Cmd, params Params) error { return nil },
			wantExitCode: 15,
			wantErr:      cmpopts.AnyError,
		},
		{
			name:       "NoExitCodeDoNotPanic",
			cmd:        "echo",
			fakeExists: func(string) bool { return true },
			fakeRun: func() error {
				return fmt.Errorf("exit status no-num")
			},
			fakeExitCode: func(error) int { return 1 },
			fakeSetupExe: func(exe *exec.Cmd, params Params) error { return nil },
			wantErr:      cmpopts.AnyError,
			wantExitCode: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exists = test.fakeExists
			exitCode = test.fakeExitCode
			run = test.fakeRun
			exeForPlatform = test.fakeSetupExe
			result := ExecuteCommand(context.Background(), Params{
				Executable: test.cmd,
				User:       "test-user",
			})

			if !cmp.Equal(result.Error, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("ExecuteCommand with user got an error: %v, want: %v", result.Error, test.wantErr)
			}
			if test.wantExitCode != int64(result.ExitCode) {
				t.Fatalf("ExecuteCommand with user got an unexpected exit code: %d, want: %d", result.ExitCode, test.wantExitCode)
			}
		})
	}
}

func TestExecuteWithEnv(t *testing.T) {
	tests := []struct {
		name         string
		params       Params
		wantStdOut   string
		wantExitCode int
		wantErr      error
	}{
		{
			name: "ExistingCmd",
			params: Params{
				Executable:  "echo",
				ArgsToSplit: "test",
			},
			wantStdOut:   "test\n",
			wantExitCode: 0,
			wantErr:      nil,
		},
		{
			name: "NonExistingCmd",
			params: Params{
				Executable: "encrypt",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CommandFailure",
			params: Params{
				Executable:  "cat",
				ArgsToSplit: "nonexisting.txtjson",
			},
			wantExitCode: 1,
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "InvalidUser",
			params: Params{
				Executable: "ls",
				User:       "invalidUser",
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setDefaults()

			result := ExecuteCommand(context.Background(), test.params)

			if !cmp.Equal(result.Error, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ExecuteCommand with env got error: %v, want: %v", result.Error, test.wantErr)
			}
			if test.wantExitCode != result.ExitCode {
				t.Errorf("ExecuteCommand with env got exit code: %d, want: %d", result.ExitCode, test.wantExitCode)
			}
			if diff := cmp.Diff(test.wantStdOut, result.StdOut); diff != "" {
				t.Errorf("ExecuteCommand with env returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetupExeForPlatform(t *testing.T) {
	tests := []struct {
		name           string
		params         Params
		executeCommand Execute
		want           error
	}{
		{
			name: "NoUserWithEnv",
			params: Params{
				Env: []string{"test-env"},
			},
			executeCommand: ExecuteCommand,
			want:           nil,
		},
		{
			name: "UserNotFound",
			params: Params{
				User: "test-user",
			},
			executeCommand: ExecuteCommand,
			want:           cmpopts.AnyError,
		},
		{
			name: "UserFailedToParse",
			params: Params{
				User: "test-user",
			},
			executeCommand: func(context.Context, Params) Result {
				return Result{}
			},
			want: cmpopts.AnyError,
		},
		{
			name: "UserFound",
			params: Params{
				User: "test-user",
			},
			executeCommand: func(context.Context, Params) Result {
				return Result{StdOut: "123"}
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setDefaults()
			got := setupExeForPlatform(context.Background(), &exec.Cmd{}, test.params, test.executeCommand)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("setupExeForPlatform(%#v) = %v, want: %v", test.params, got, test.want)
			}
		})
	}
}

func TestSplitParams(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantOut []string
	}{
		{
			name:    "echo",
			args:    "echo hello, world",
			wantOut: []string{"echo", "hello,", "world"},
		},
		{
			name:    "bashMd5sum",
			args:    "-c 'echo $0 | md5sum' 'test hashing functions'",
			wantOut: []string{"-c", "echo $0 | md5sum", "test hashing functions"},
		},
		{
			name:    "tcpFiltering",
			args:    "-c 'lsof -nP -p $(pidof hdbnameserver) | grep LISTEN | grep -v 127.0.0.1 | grep -Eo `(([0-9]{1,3}\\.){1,3}[0-9]{1,3})|(\\*)\\:[0-9]{3,5}`'",
			wantOut: []string{"-c", "lsof -nP -p $(pidof hdbnameserver) | grep LISTEN | grep -v 127.0.0.1 | grep -Eo '(([0-9]{1,3}\\.){1,3}[0-9]{1,3})|(\\*)\\:[0-9]{3,5}'"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			splitArgs := splitParams(test.args)
			if diff := cmp.Diff(test.wantOut, splitArgs); diff != "" {
				t.Fatalf("splitParams returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
