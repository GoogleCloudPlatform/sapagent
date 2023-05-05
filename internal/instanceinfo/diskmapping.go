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

package instanceinfo

import (
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

/*
The production library uses the ExecuteCommand function from commandlineexecutor and the
EvalSymlinks function from filepath.  We need to be able to mock these functions in our unit tests.
*/
var (
	executeCommand = commandlineexecutor.ExecuteCommand
	symLinkCommand = filepath.EvalSymlinks
)

// A PhysicalPathReader reads disk mapping information from the OS.
type PhysicalPathReader struct {
	OS string
}

const winPsPath = `C:\\Program Files\Google\google-cloud-sap-agent\google-cloud-sap-agent-diskmapping.ps1`

/*
ForDeviceName returns the phsyical device name of the disk mapped to "deviceName".
*/
func (r *PhysicalPathReader) ForDeviceName(deviceName string) (string, error) {
	if r.OS == "windows" {
		return forWindows(deviceName)
	}
	return forLinux(deviceName)
}

/*
forWindows returns the name of the Windows physical disk mapped to "deviceName".

Note:
google-cloud-sap-agent-diskmapping.ps1 is packaged with the gcagent binary and tested individually with its own
integration tests.
*/
func forWindows(deviceName string) (string, error) {
	result := executeCommand(commandlineexecutor.Params{
		Executable: "cmd",
		Args:       []string{"/C", "Powershell", "-File", winPsPath, deviceName},
	})
	if result.Error != nil {
		log.Logger.Warnw("Could not get disk mapping for device", "name", deviceName)
		log.Logger.Debugw("Execution error", "executable", winPsPath, "stdout", result.StdOut, "stderror", result.StdErr, "error", result.Error)
		return "", result.Error
	}
	m := strings.Trim(result.StdOut, "\r\n")
	log.Logger.Debugw("Mapping for device", "name", deviceName, "mapping", m)
	return m, nil
}

/*
forLinux returns the name of the Linux physical disk mapped to "deviceName". (sda1, hda1, sdb1,
etc...)
*/
func forLinux(deviceName string) (string, error) {
	path, err := symLinkCommand("/dev/disk/by-id/google-" + deviceName)
	if err != nil {
		return "", err
	}

	if path != "" {
		path = strings.TrimSuffix(filepath.Base(path), "\n")
	}
	log.Logger.Debugw("Mapping for device", "name", deviceName, "mapping", path)
	return path, nil
}
