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

/*
Package commandlineexecutor creates an interface to streamline execution of shell commands
across multiple platforms
*/
package commandlineexecutor

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

var (
	exitStatusPattern = regexp.MustCompile("exit status ([0-9]+)")
)

type (
	runCmdAsUser func(string, string, string) (string, string, error)
	// CommandRunner is a function to execute command. Production callers
	// to pass commandlineexecutor.ExpandAndExecuteCommand while calling
	// this package's APIs.
	CommandRunner func(string, string) (string, string, error)
	// CommandRunnerNoSpace is a function to execute a command.  Production callers
	// to pass commandlineexecutor.ExecuteCommand while calling this package's APIs.
	CommandRunnerNoSpace func(string, ...string) (string, string, error)
	// CommandExistsRunner is a function to check if command exists.  Production callers
	// to pass commandlineexecutor.CommandExists while calling
	// this package's APIs.
	CommandExistsRunner func(string) bool
	// Runner has the necessary info to run a command.
	Runner struct {
		User, Executable, Args string
		Env                    []string
	}
)

/*
RunWithEnv uses the fields of Runner to execute a command.

Returns StdOut, StdErr, exitCode of command and an error.
Returns 0 if parsing of exit code fails.
*/
func (r *Runner) RunWithEnv() (stdOut string, stdErr string, code int, err error) {
	return r.platformRunWithEnv()
}

/*
ExpandAndExecuteCommand takes a command string and a single argument string containing space
delimited arguments and executes said command after splitting the arguments into a slice.
*/
func ExpandAndExecuteCommand(executable string, args string) (stdOut string, stdErr string, err error) {
	return ExecuteCommand(executable, splitParams(args)...)
}

/*
ExecuteCommand takes a command string and a arbitrarily sized list of arguments as a slice of
strings and executes the command with those arguments.  The function returns a tuple containing the
command's stdOut as a string, the command's stdErr as a string, and an error.  The error will be nil
if the command was successful.
*/
func ExecuteCommand(executable string, args ...string) (stdOut string, stdErr string, err error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exe := exec.Command(executable, args...)

	exe.Stdout = stdout
	exe.Stderr = stderr

	log.Logger.Debugw("Executing command", "executable", executable, "args", args)

	if err := exe.Run(); err != nil {
		log.Logger.Debugw("Could not execute command", "executable", executable, "args", args, "exitcode", ExitCode(err), "error", err, "stdout", stdout.String(), "stderr", stderr.String())
		return stdout.String(), stderr.String(), err
	}

	// Exit code can assumed to be 0
	log.Logger.Debugw("Successfully executed command", "executable", executable, "args", args, "stdout", stdout.String(), "stderr", stderr.String())
	return stdout.String(), stderr.String(), nil

}

/*
ExpandAndExecuteCommandAsUser takes a user string, a command string and a single argument string containing
space delimited arguments and executes said command after splitting the arguments into a slice.
*/
func ExpandAndExecuteCommandAsUser(user, executable, args string) (stdOut string, stdErr string, err error) {
	return ExecuteCommandAsUser(user, executable, splitParams(args)...)
}

/*
ExpandAndExecuteCommandAsUserExitCode takes a user string, a command string and a single argument
string containing space delimited arguments and executes said command after splitting the arguments
into a slice. It returns an additional exitCode which is an outcome of command execution.
Returns 0 if parsing of exit code fails.
*/
func ExpandAndExecuteCommandAsUserExitCode(user, executable, args string) (stdOut string, stdErr string, code int64, err error) {
	return runCommandAsUserExitCode(user, executable, args, ExpandAndExecuteCommandAsUser)
}

// runCommandAsUserExitCode is a testable version of ExpandAndExecuteCommandAsUserExitCode.
func runCommandAsUserExitCode(user, executable, args string, run runCmdAsUser) (stdOut string, stdErr string, code int64, err error) {
	if !CommandExists(executable) {
		log.Logger.Debugw("Command executable not found", "executable", executable)
		msg := fmt.Sprintf("Command executable: %q not found.", executable)
		return "", msg, 0, fmt.Errorf("command executable: %s not found", executable)
	}

	var exitCode int
	stdOut, stdErr, err = run(user, executable, args)
	if err != nil {
		log.Logger.Debugw("Command execution complete", "executable", executable, "args", args, "stdout", stdOut, "stderr", stdErr, "error", err)

		m := exitStatusPattern.FindStringSubmatch(err.Error())
		exitCode, err = strconv.Atoi(m[1])
		if err != nil {
			log.Logger.Debugw("Failed to get command exit code", "executable", executable, "args", args, "error", err)
			return stdOut, stdErr, 0, err
		}
	}
	return stdOut, stdErr, int64(exitCode), nil
}

/*
ExecuteCommandAsUser takes a user string, a command string and a arbitrarily sized list of
arguments as a slice of strings and executes the command with those arguments.
The function returns a tuple containing the command's stdOut as a string, the command's stdErr
as a string, and an error.  The error will be nil if the command was successful.
Note: This is intended for Linux based system only see exe_linux.go.
*/
func ExecuteCommandAsUser(user, executable string, args ...string) (stdOut string, stdErr string, err error) {
	return executeCommandAsUser(user, executable, args...)
}

/*
CommandExists returns whether or not an executable command exists within the current os runtime
environment.
*/
func CommandExists(executable string) bool {
	_, err := exec.LookPath(executable)
	return err == nil
}

/*
ExitCode returns the exit code attached to the error produced by a call to exec.Command or 0 if
the error is null.
*/
func ExitCode(err error) int {
	var exitErr *exec.ExitError
	if err != nil && errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

/*
splitParams performs a custom splitting operation around spaces and substrings contained within
single quotes, exclusively, on command strings in order to parse them into a list of valid shell
arguments for exec.Command structs

ex:
bash -c 'ls $0 $1' /etc /home
becomes:
{"bash", "-c", "ls $0 $1", "/etc", "/home"}
*/
func splitParams(executable string) []string {
	// This regex pattern matches substrings without spaces or those with spaces between single quotes
	pattern := regexp.MustCompile(`[^\s']+|('([^']*)')`)
	arr := pattern.FindAllString(executable, -1)
	// This for loop removes the single quote characters surrounding the matched substring
	for i := range arr {
		// convert "'ls $0 $1'" to "ls $0 $1"
		if arr[i][0] == '\'' && arr[i][len(arr[i])-1] == '\'' {
			arr[i] = arr[i][1 : len(arr[i])-1]
		}
	}
	return arr
}
