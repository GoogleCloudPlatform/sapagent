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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestExpandAndExecuteCommand(t *testing.T) {
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
			stdOut, stdErr, err := ExpandAndExecuteCommand(test.cmd, test.args)
			if err != nil {
				t.Fatalf("ExpandAndExecuteCommand returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(test.wantOut, stdOut); diff != "" {
				t.Fatalf("ExpandAndExecuteCommand returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, stdErr); diff != "" {
				t.Fatalf("ExpandAndExecuteCommand returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExpandAndExecuteCommandAsUserSucceeds(t *testing.T) {
	stdOut, _, err := ExpandAndExecuteCommandAsUser("", "echo", "hello, world")
	if err != nil {
		t.Fatalf(`ExpandAndExecuteCommandAsUser("", "echo", "hello, world") returned unexpected error, got: %v, want: nil`, err)
	}
	if wantOut := "hello, world\n"; wantOut != stdOut {
		t.Errorf(`ExpandAndExecuteCommandAsUser("", "echo", "hello, world") = %s, want: %s`, stdOut, wantOut)
	}
}

func TestExpandAndExecuteCommandAsUserFails(t *testing.T) {
	if _, _, err := ExpandAndExecuteCommandAsUser("user-does-not-exist", "echo", "hello, world"); err == nil {
		t.Errorf(`ExpandAndExecuteCommandAsUser("user-does-not-exist", "echo", "hello, world") did not return an error`)
	}
}

func TestExecuteCommand(t *testing.T) {
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
			stdOut, stdErr, err := ExecuteCommand(test.cmd, test.args...)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.wantOut, stdOut); diff != "" {
				t.Fatalf("ExecuteCommand returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, stdErr); diff != "" {
				t.Fatalf("ExecuteCommand returned unexpected diff (-want +got):\n%s", diff)
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
			if got := CommandExists(test.cmd); got != test.exists {
				t.Fatalf("CommandExists returned unexpected result, got: %t want: %t", got, test.exists)
			}
		})
	}
}

func TestRunCommandAsUserExitCode(t *testing.T) {
	tests := []struct {
		name         string
		cmd          string
		fakeRun      runCmdAsUser
		wantExitCode int64
		wantErr      error
	}{
		{
			name: "ExistingCmd",
			cmd:  "ls",
			fakeRun: func(user string, executable string, args string) (string, string, error) {
				return "", "", nil
			},
			wantErr: nil,
		},
		{
			name: "NonExistingCmd",
			cmd:  "encrypt",
			fakeRun: func(user string, executable string, args string) (string, string, error) {
				return "", "", fmt.Errorf("command executable: encrypt not found")
			},
			wantExitCode: 0,
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "ExitCode15",
			cmd:  "ls",
			fakeRun: func(user string, executable string, args string) (string, string, error) {
				return "", "", fmt.Errorf("exit status 15")
			},
			wantExitCode: 15,
			wantErr:      nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, gotExitCode, gotErr := runCommandAsUserExitCode("test-user", test.cmd, "", test.fakeRun)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("runCommandAsUserExitCode() got error: %v, want: %v", gotErr, test.wantErr)
			}
			if test.wantExitCode != gotExitCode {
				t.Fatalf("runCommandAsUserExitCode() got unexpected exit code: %d, want: %d", gotExitCode, test.wantExitCode)
			}
		})

	}

}

func TestRunWithEnv(t *testing.T) {
	tests := []struct {
		name         string
		runner       Runner
		wantStdOut   string
		wantExitCode int
		wantErr      error
	}{
		{
			name: "ExistingCmd",
			runner: Runner{
				Executable: "echo",
				Args:       "test",
			},
			wantStdOut:   "test\n",
			wantExitCode: 0,
			wantErr:      nil,
		},
		{
			name: "NonExistingCmd",
			runner: Runner{
				Executable: "encrypt",
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "CommandFailure",
			runner: Runner{
				Executable: "cat",
				Args:       "nonexisting.txtjson",
			},
			wantExitCode: 1,
		},
		{
			name: "InvalidUser",
			runner: Runner{
				Executable: "ls",
				User:       "invalidUser",
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantStdOut, _, gotExitCode, gotErr := test.runner.RunWithEnv()

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("RunWithEnv() got error: %v, want: %v", gotErr, test.wantErr)
			}
			if test.wantExitCode != gotExitCode {
				t.Errorf("RunWithEnv() got exit code: %d, want: %d", gotExitCode, test.wantExitCode)
			}
			if diff := cmp.Diff(test.wantStdOut, wantStdOut); diff != "" {
				t.Errorf("RunWithEnv() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
