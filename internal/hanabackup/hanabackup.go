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

// Package hanabackup implements HANA specific operations for disk backup workflows.
package hanabackup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

// ParseBasePath parses the base path from the global.ini file.
func ParseBasePath(ctx context.Context, pattern string, exec commandlineexecutor.Execute) (string, error) {
	args := `-c 'grep ` + pattern + ` /usr/sap/*/SYS/global/hdb/custom/config/global.ini | cut -d= -f 2'`
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing base path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("ParseBasePath", "stdout", result.StdOut, "stderr", result.StdErr)

	basePath := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Found HANA Base data directory", "hanaDataPath", basePath)
	return basePath, nil
}

// ParseLogicalPath parses the logical path from the base path.
func ParseLogicalPath(ctx context.Context, basePath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf("-c 'df --output=source %s | tail -n 1'", basePath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing logical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("ParseLogicalPath", "stdout", result.StdOut, "stderr", result.StdErr)

	logicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Directory to logical device mapping", "DirectoryPath", basePath, "LogicalDevice", logicalDevice)
	return logicalDevice, nil
}

// ParsePhysicalPath parses the physical path from the logical path.
func ParsePhysicalPath(ctx context.Context, logicalPath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf("-c '/sbin/lvdisplay -m %s | grep \"Physical volume\" | awk \"{print \\$3}\"'", logicalPath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing physical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("ParsePhysicalPath", "stdout", result.StdOut, "stderr", result.StdErr)

	physicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Logical device to physical device mapping", "LogicalDevice", logicalPath, "PhysicalDevice", physicalDevice)
	if physicalDevice == "" {
		return "", fmt.Errorf("physical device is empty")
	}
	return physicalDevice, nil
}

// Unmount unmounts the given directory.
func Unmount(ctx context.Context, path string, exec commandlineexecutor.Execute) error {
	log.CtxLogger(ctx).Infow("Unmount path", "directory", path)
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c 'sync;umount -f %s'", path),
	})
	if result.Error != nil {
		r := exec(ctx, commandlineexecutor.Params{
			Executable:  "bash",
			ArgsToSplit: fmt.Sprintf(" -c 'lsof | grep %s'", path), // NOLINT
		})
		msg := `failure unmounting directory: %s, stderr: %s, err: %s.
		Here are the possible open references to the path, stdout: %s stderr: %s.
		Please ensure these references are cleaned up for unmount to proceed and retry the command`
		return fmt.Errorf(msg, path, result.StdErr, result.Error, r.StdOut, r.StdErr)
	}
	log.CtxLogger(ctx).Debugf("Unmount", "stdout", result.StdOut, "stderr", result.StdErr)

	if result.ExitCode == 0 {
		log.CtxLogger(ctx).Infow("Directory unmounted successfully", "directory", path)
	}
	return nil
}

// FreezeXFS freezes the XFS filesystem.
func FreezeXFS(ctx context.Context, hanaDataPath string, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-f " + hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("FreezeXFS", "stdout", result.StdOut, "stderr", result.StdErr)

	log.CtxLogger(ctx).Infow("Filesystem frozen successfully", "hanaDataPath", hanaDataPath)
	return nil
}

// UnFreezeXFS unfreezes the XFS filesystem.
func UnFreezeXFS(ctx context.Context, hanaDataPath string, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-u " + hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure un freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("UnFreezeXFS", "stdout", result.StdOut, "stderr", result.StdErr)

	log.CtxLogger(ctx).Infow("Filesystem unfrozen successfully", "hanaDataPath", hanaDataPath)
	return nil
}

// CheckDataDir checks if the data directory is valid and has a valid physical volume.
func CheckDataDir(ctx context.Context, exec commandlineexecutor.Execute) (dataPath, logicalDataPath, physicalDataPath string, err error) {
	if dataPath, err = ParseBasePath(ctx, "basepath_datavolumes", exec); err != nil {
		return "", "", "", err
	}
	log.CtxLogger(ctx).Infow("Data volume base path", "path", dataPath)
	if logicalDataPath, err = ParseLogicalPath(ctx, dataPath, exec); err != nil {
		return dataPath, "", "", err
	}
	if !strings.Contains(logicalDataPath, "/dev/mapper") {
		return dataPath, "", "", fmt.Errorf("only data disks using LVM are supported, exiting")
	}
	if physicalDataPath, err = ParsePhysicalPath(ctx, logicalDataPath, exec); err != nil {
		return dataPath, logicalDataPath, "", err
	}
	return dataPath, logicalDataPath, physicalDataPath, nil
}

// CheckLogDir checks if the log directory is valid and has a valid physical volume.
func CheckLogDir(ctx context.Context, exec commandlineexecutor.Execute) (baseLogPath, logicalLogPath, physicalLogPath string, err error) {
	if baseLogPath, err = ParseBasePath(ctx, "basepath_logvolumes", commandlineexecutor.ExecuteCommand); err != nil {
		return "", "", "", err
	}
	log.CtxLogger(ctx).Infow("Log volume base path", "path", baseLogPath)

	if logicalLogPath, err = ParseLogicalPath(ctx, baseLogPath, exec); err != nil {
		return baseLogPath, "", "", err
	}
	if physicalLogPath, err = ParsePhysicalPath(ctx, logicalLogPath, exec); err != nil {
		return baseLogPath, logicalLogPath, "", err
	}
	return baseLogPath, logicalLogPath, physicalLogPath, nil
}

// CheckDataDeviceForStripes checks if the data device is striped.
func CheckDataDeviceForStripes(ctx context.Context, logicalDataPath string, exec commandlineexecutor.Execute) (bool, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf(" -c '/sbin/lvdisplay -m %s | grep Stripes'", logicalDataPath),
	})
	log.CtxLogger(ctx).Debugf("CheckDataDeviceForStripes", "stdout", result.StdOut, "stderr", result.StdErr)

	if result.Error != nil {
		return false, fmt.Errorf("failure checking if data device is striped, stderr: %s, err: %s", result.StdErr, result.Error)
	} else if result.ExitCode == 0 {
		return true, nil
	}

	return false, nil
}

// ReadDataDirMountPath reads the data directory mount path.
func ReadDataDirMountPath(ctx context.Context, baseDataPath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c 'df --output=target %s| tail -n 1'", baseDataPath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure reading data directory mount path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("ReadDataDirMountPath", "stdout", result.StdOut, "stderr", result.StdErr)

	return strings.TrimSuffix(result.StdOut, "\n"), nil
}

// RescanVolumeGroups rescans all volume groups and mounts them.
func RescanVolumeGroups(ctx context.Context) error {
	result := commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/dmsetup",
		ArgsToSplit: "remove_all",
	})
	if result.Error != nil {
		return fmt.Errorf("failure removing device definitions from the Device Mapper driver, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("RescanVolumeGroups", "stdout", result.StdOut, "stderr", result.StdErr)

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgscan",
		ArgsToSplit: "-v --mknodes",
	})
	if result.Error != nil {
		return fmt.Errorf("failure scanning volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("RescanVolumeGroups", "stdout", result.StdOut, "stderr", result.StdErr)

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "/sbin/vgchange",
		ArgsToSplit: "-ay",
	})
	if result.Error != nil {
		return fmt.Errorf("failure changing volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("RescanVolumeGroups", "stdout", result.StdOut, "stderr", result.StdErr)

	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable: "/sbin/lvscan",
	})
	if result.Error != nil {
		return fmt.Errorf("failure scanning volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("RescanVolumeGroups", "stdout", result.StdOut, "stderr", result.StdErr)

	time.Sleep(5 * time.Second)
	result = commandlineexecutor.ExecuteCommand(ctx, commandlineexecutor.Params{
		Executable:  "mount",
		ArgsToSplit: "-av",
	})
	if result.Error != nil {
		return fmt.Errorf("failure mounting volume groups, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("RescanVolumeGroups", "stdout", result.StdOut, "stderr", result.StdErr)

	return nil
}

// StopHANA stops the HANA instance.
func StopHANA(ctx context.Context, force bool, user, sid string, exec commandlineexecutor.Execute) error {
	var cmd string
	if force {
		log.CtxLogger(ctx).Infow("HANA force stop requested", "sid", sid)
		cmd = fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && /usr/sap/%s/*/HDB stop'", sid, sid) // NOLINT
	} else {
		log.CtxLogger(ctx).Infow("Stopping HANA", "sid", sid)
		cmd = fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && /usr/sap/%s/*/HDB kill'", sid, sid) // NOLINT
	}
	result := exec(ctx, commandlineexecutor.Params{
		User:        user,
		Executable:  "bash",
		ArgsToSplit: cmd,
		Timeout:     300,
	})
	if result.Error != nil {
		return fmt.Errorf("failure stopping HANA, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("StopHANA", "stdout", result.StdOut, "stderr", result.StdErr)

	log.CtxLogger(ctx).Infow("HANA stopped successfully", "sid", sid)
	return nil
}

// waitForIndexServerToStop() waits for the hdb index server to stop.
func waitForIndexServerToStop(ctx context.Context, user string, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: `-c 'ps x | grep hdbindexs | grep -v grep'`,
		User:        user,
	})

	if result.ExitCode == 0 {
		return fmt.Errorf("failure waiting for index server to stop, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Debugf("waitForIndexServerToStop", "stdout", result.StdOut, "stderr", result.StdErr)

	return nil
}

// WaitForIndexServerToStopWithRetry waits for the index server to stop with retry.
// We sleep for 10s between retries a total 90 time => max_wait_duration =  10*90 = 15 minutes
func WaitForIndexServerToStopWithRetry(ctx context.Context, user string, exec commandlineexecutor.Execute) error {
	constantBackoff := backoff.NewConstantBackOff(10 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 90), ctx)
	return backoff.Retry(func() error { return waitForIndexServerToStop(ctx, user, exec) }, bo)
}

// Key defines the contents of each entry in the encryption key file.
// Reference: https://cloud.google.com/compute/docs/disks/customer-supplied-encryption#key_file
type Key struct {
	URI     string `json:"uri"`
	Key     string `json:"key"`
	KeyType string `json:"key-type"`
}

// ReadKey reads the encryption key from the key file.
func ReadKey(file, diskURI string, read configuration.ReadConfigFile) (string, error) {
	var keys []Key
	fileContent, err := read(file)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(fileContent, &keys); err != nil {
		return "", err
	}

	for _, k := range keys {
		if k.URI == diskURI {
			return k.Key, nil
		}
	}
	return "", fmt.Errorf("no matching key for the disk")
}
