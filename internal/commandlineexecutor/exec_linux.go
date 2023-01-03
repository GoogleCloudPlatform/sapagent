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
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

func (r *Runner) platformRunWithEnv() (stdOut, stdErr string, code int, err error) {
	if !CommandExists(r.Executable) {
		return "", "", 0, fmt.Errorf("command executable: %s not found", r.Executable)
	}

	exe := exec.Command(r.Executable, splitParams(r.Args)...)
	exe.Env = append(exe.Environ(), r.Env...)
	log.Logger.Debugf("Environment: %s.", exe.Environ())

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exe.Stdout = stdout
	exe.Stderr = stderr

	if r.User != "" {
		uid, err := getUID(r.User)
		if err != nil {
			return "", "", 0, err
		}
		exe.SysProcAttr = &syscall.SysProcAttr{}
		exe.SysProcAttr.Credential = &syscall.Credential{Uid: uid}
	}

	log.Logger.Debugf("Executing command %q %q as user %q.", r.Executable, r.Args, r.User)
	if err := exe.Run(); err != nil {
		log.Logger.Debugf("Running as User: %q. Could not execute %q. StdOut: %s, StdErr: %s.", r.User, r.Executable, stdout.String(), stderr.String())

		m := exitStatusPattern.FindStringSubmatch(err.Error())
		if len(m) == 2 {
			code, err = strconv.Atoi(m[1])
			if err != nil {
				log.Logger.Debug("Failed to get command exit code.", log.Error(err))
				return stdout.String(), stderr.String(), 0, err
			}
		}
	}

	log.Logger.Debugf("Running as User: %q. Exit code: 0 stdout: %q", r.User, stdout.String())
	return stdout.String(), stderr.String(), code, nil
}

// TODO(b/258685099): Remove executeCommandAsUser after migrating all its callers to Runner.Run*().
func executeCommandAsUser(user, executable string, args ...string) (stdOut string, stdErr string, err error) {
	uid, err := getUID(user)
	if err != nil {
		return "", "", err
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	exe := exec.Command(executable, args...)
	exe.SysProcAttr = &syscall.SysProcAttr{}
	exe.SysProcAttr.Credential = &syscall.Credential{Uid: uid}
	exe.Stdout = stdout
	exe.Stderr = stderr

	log.Logger.Debugf("Executing command %q %q as user %q", executable, args, user)

	if err := exe.Run(); err != nil {
		log.Logger.Debugf("Running as User: %q. Could not execute %q.  Result exit code: %d", user, executable, ExitCode(err))
		return stdout.String(), stderr.String(), err
	}

	// Exit code can assumed to be 0
	log.Logger.Debugf("Running as User: %q. Result exit code: 0 stdout: %q", user, stdout.String())
	return stdout.String(), stderr.String(), nil
}

/*
getUID takes user string and returns the numeric LINUX UserId and an Error.
Returns (0, error) in case of failure, and (uid, nil) when successful.
Note: This is intended for Linux based system only.
*/
func getUID(user string) (uint32, error) {
	o, e, err := ExpandAndExecuteCommand("id", fmt.Sprintf("-u %s", user))
	if err != nil {
		return 0, fmt.Errorf("getUID failed with: %s. StdErr: %s", err, e)
	}
	uid, err := strconv.Atoi(strings.TrimSuffix(o, "\n"))
	if err != nil {
		return 0, fmt.Errorf("could not parse UID from StdOut: %s", o)
	}
	return uint32(uid), nil
}
