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
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"google3/third_party/sapagent/shared/log"
)

var (
	exitStatusPattern                     = regexp.MustCompile("exit status ([0-9]+)")
	exists                                = CommandExists
	exitCode                              = commandExitCode
	run               Run                 = nil
	exeForPlatform    SetupExeForPlatform = nil
)

type (
	// Execute is a function to execute a command. Production callers
	// to pass commandlineexecutor.ExecuteCommand while calling this package's APIs.
	Execute func(context.Context, Params) Result

	// Exists is a function to check if a command exists.  Production callers
	// to pass commandlineexecutor.CommandExists while calling this package's APIs.
	Exists func(string) bool

	// ExitCode is a function to get the exit code from an error.  Production callers
	// to pass commandlineexecutor.CommandExitCode while calling this package's APIs.
	ExitCode func(err error) int

	// Run is a testable version of the exec.Run method.  Should only be used during testing.
	Run func() error

	// SetupExeForPlatform is a testable version of the setupExeForPlatform call.
	// Should only be used during testing.
	SetupExeForPlatform func(exe *exec.Cmd, params Params) error

	// Params encapsulates the parameters used by the Exec* and RunWithEnv funcs.
	Params struct {
		Executable string
		// One of ArgsToSplit or Args should be defined on the Params.
		// ArgsToSplit should be preferred when issuing commands with a subshell and using "-c".
		// An example would be an invocation like:
		//   Executable: "/bin/sh"
		//   ArgsToSplit: "-c 'ls /usr/sap/*/SYS/global/hdb/custom/config/global.ini'"
		// In this case ArgsToSplit will be split up correctly as:
		//   []string{"-c", "'ls /usr/sap/*/SYS/global/hdb/custom/config/global.ini'"}
		ArgsToSplit string
		Args        []string
		Timeout     int // defaults to 60, so timeout will occur in 60 seconds
		User        string
		Env         []string
	}

	// Result holds the stdout, stderr, exit code, and error from the execution.
	Result struct {
		StdOut, StdErr   string
		ExitCode         int
		Error            error
		ExecutableFound  bool
		ExitStatusParsed bool // Will be true if "exit status ([0-9]+)" is in the error result
	}
)

/*
ExecuteCommand takes Params and returns a Result.

If the params.Executable does not exist it will return early with the Result.Error filled
If the Params ArgsToSplit is not empty then it will be split into an arguments array
Else the Args will be used as the arguments array
If the User is not empty then the command will be executed as that user
If Env is defined then that environment will be used to execute the command

The returned Result will contain the standard out, standard error, the exit code and an error if
one was encountered during execution.
*/
func ExecuteCommand(ctx context.Context, params Params) Result {
	if !exists(params.Executable) {
		log.Logger.Debugw("Command executable not found", "executable", params.Executable)
		msg := fmt.Sprintf("Command executable: %q not found.", params.Executable)
		return Result{"", msg, 0, fmt.Errorf("command executable: %s not found", params.Executable), false, false}
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	// Timeout the command at 60 seconds by default.
	timeout := 60 * time.Second
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
	}
	// Context tctx has a Timeout while running the commands.
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := params.Args
	if params.ArgsToSplit != "" {
		args = splitParams(params.ArgsToSplit)
	}
	exe := exec.CommandContext(tctx, params.Executable, args...)

	exe.Stdout = stdout
	exe.Stderr = stderr
	var err error
	if exeForPlatform != nil {
		err = exeForPlatform(exe, params)
	} else {
		// We pass ctx because this calls back into ExecuteCommand which adds the timeout before running the command.
		err = setupExeForPlatform(ctx, exe, params, ExecuteCommand)
	}
	if err != nil {
		log.Logger.Debugw("Could not setup the executable environment", "executable", params.Executable, "args", args, "error", err)
		return Result{stdout.String(), stderr.String(), 0, err, true, false}
	}

	log.Logger.Debugw("Executing command", "executable", params.Executable, "args", args,
		"timeout", timeout, "user", params.User, "env", params.Env)

	if run != nil {
		err = run()
	} else {
		err = exe.Run()
	}
	if err != nil {
		// Set the exit code based on the error first, then see if we can get it from the error message.
		exitCode := exitCode(err)
		m := exitStatusPattern.FindStringSubmatch(err.Error())
		exitStatusParsed := false
		if len(m) > 0 {
			atoi, serr := strconv.Atoi(m[1])
			if serr != nil {
				log.Logger.Debugw("Failed to get command exit code from string match", "executable", params.Executable,
					"args", args, "error", serr)
			} else {
				// This is the case where we expect to have an Error but want the exit code from the "exit status #" string
				exitCode = atoi
				exitStatusParsed = true
			}
		} else {
			log.Logger.Debugw("Error encountered when executing command", "executable", params.Executable,
				"args", args, "exitcode", exitCode, "error", err, "stdout", stdout.String(),
				"stderr", stderr.String())
		}
		return Result{stdout.String(), stderr.String(), exitCode, err, true, exitStatusParsed}
	}

	// Exit code can assumed to be 0
	log.Logger.Debugw("Successfully executed command", "executable", params.Executable, "args", args,
		"stdout", stdout.String(), "stderr", stderr.String())
	return Result{stdout.String(), stderr.String(), 0, nil, true, false}
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
commandExitCode returns the exit code attached to the error produced by a call to exec.Command or 0
if the error is null.
*/
func commandExitCode(err error) int {
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
