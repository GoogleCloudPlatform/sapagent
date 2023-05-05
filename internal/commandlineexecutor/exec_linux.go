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
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// setupExeForPlatform sets up the env and user if provided in the params.
// returns an error if it could not be setup
func setupExeForPlatform(exe *exec.Cmd, params Params) error {
	// set the execution environment if params Env exists
	if len(params.Env) > 0 {
		exe.Env = append(exe.Environ(), params.Env...)
	}

	// if params.User exists run as the user
	if params.User != "" {
		uid, err := getUID(params.User)
		if err != nil {
			return err
		}
		exe.SysProcAttr = &syscall.SysProcAttr{}
		exe.SysProcAttr.Credential = &syscall.Credential{Uid: uid}
	}
	return nil
}

/*
getUID takes user string and returns the numeric LINUX UserId and an Error.
Returns (0, error) in case of failure, and (uid, nil) when successful.
Note: This is intended for Linux based system only.
*/
func getUID(user string) (uint32, error) {
	result := ExecuteCommand(Params{
		Executable:  "id",
		ArgsToSplit: fmt.Sprintf("-u %s", user),
	})
	if result.Error != nil {
		return 0, fmt.Errorf("getUID failed with: %s. StdErr: %s", result.Error, result.StdErr)
	}
	uid, err := strconv.Atoi(strings.TrimSuffix(result.StdOut, "\n"))
	if err != nil {
		return 0, fmt.Errorf("could not parse UID from StdOut: %s", result.StdOut)
	}
	return uint32(uid), nil
}
