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

package configureinstance

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	//go:embed google-x4.conf
	googleX4Conf []byte

	systemConf       = []string{"DefaultTimeoutStartSec=300s", "DefaultTimeoutStopSec=300s", "DefaultTasksMax=infinity"}
	logindConf       = []string{"UserTasksMax=infinity"}
	modprobeConf     = []byte("blacklist idxd\nblacklist hpilo\nblacklist acpi_cpufreq\nblacklist qat_4xxx\nblacklist intel_qat\n")
	grubLinuxDefault = `GRUB_CMDLINE_LINUX_DEFAULT="tsc=nowatchdog add_efi_memmap udev.children-max=512 nmi_watchdog=0 watchdog_thresh=60 mce=2 console=ttyS0,115200 earlyprintk=ttyS0,115200 uv_nmi.action=kdump bau=0 pci=nobar transparent_hugepage=never numa_balancing=disable"`
	grubLinuxLabel   = "GRUB_ENABLE_LINUX_LABEL=true"
	grubDevice       = `GRUB_DEVICE="LABEL=ROOT"`
)

// configureX4 checks and applies OS settings on X4.
// Returns true if a reboot is required.
func (c *ConfigureInstance) configureX4(ctx context.Context) (bool, error) {
	if c.machineType == "x4-megamem-1920" || c.machineType == "x4-megamem-1440" {
		processingSystem := "OLTP"
		if c.OverrideHyperThreading {
			processingSystem = "OLAP"
		}
		log.CtxLogger(ctx).Infof("%s detected, applying configuration for %s workloads.", c.machineType, processingSystem)
	}
	rebootSLES, err := c.configureX4SLES(ctx)
	if err != nil {
		return false, err
	}

	log.CtxLogger(ctx).Info("Continuing with general X4 configurations.")
	rebootSystemdSystem, err := c.checkAndRegenerateLines(ctx, "/etc/systemd/system.conf", systemConf)
	if err != nil {
		return false, err
	}
	rebootSystemdLogin, err := c.checkAndRegenerateLines(ctx, "/etc/systemd/logind.conf", logindConf)
	if err != nil {
		return false, err
	}
	rebootModprobe, err := c.checkAndRegenerateFile(ctx, "/etc/modprobe.d/google-x4.conf", modprobeConf)
	if err != nil {
		return false, err
	}
	if rebootModprobe && c.Apply {
		log.CtxLogger(ctx).Info("Regenerating modprobe by running 'usr/bin/dracut --force'.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "usr/bin/dracut", ArgsToSplit: "--force"}); res.ExitCode != 0 {
			return false, fmt.Errorf("'usr/bin/dracut --force' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
		}
	}
	if (c.machineType == "x4-megamem-1920" || c.machineType == "x4-megamem-1440") && !c.OverrideHyperThreading {
		log.CtxLogger(ctx).Infof("%s detected, appending 'nosmt' to 'GRUB_CMDLINE_LINUX_DEFAULT'.", c.machineType)
		grubLinuxDefault = strings.TrimSuffix(grubLinuxDefault, `"`) + ` nosmt"`
	}
	rebootGrub, err := c.checkAndRegenerateLines(ctx, "/etc/default/grub", []string{grubLinuxDefault, grubLinuxLabel, grubDevice})
	if err != nil {
		return false, err
	}
	if rebootGrub {
		if c.Check {
			log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to regenerate grub.")
		} else {
			log.CtxLogger(ctx).Info("Regenerating grub by running 'grub2-mkconfig'.")
			if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "grub2-mkconfig", ArgsToSplit: "-o /boot/grub2/grub.cfg"}); res.ExitCode != 0 {
				return false, fmt.Errorf("'grub2-mkconfig -o /boot/grub2/grub.cfg' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
			}
		}
	}

	log.CtxLogger(ctx).Info("General X4 configurations complete.")
	return rebootSLES || rebootSystemdSystem || rebootSystemdLogin || rebootModprobe || rebootGrub, nil
}

// configureX4SLES checks and applies OS settings for X4 running on SLES.
// Returns true if SAPTune re-apply needed to be run.
func (c *ConfigureInstance) configureX4SLES(ctx context.Context) (bool, error) {
	osRelease, err := c.readFile("/etc/os-release")
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(osRelease), "SLES") {
		log.CtxLogger(ctx).Info("SLES OS not detected, skiping specific configurations.")
		return false, nil
	}
	log.CtxLogger(ctx).Info("SLES OS detected, continuing with specific configurations.")

	if err := c.saptuneService(ctx); err != nil {
		return false, err
	}
	sapTuneReapply, err := c.checkAndRegenerateFile(ctx, "/etc/saptune/extra/google-x4.conf", googleX4Conf)
	if err != nil {
		return false, err
	}
	if c.saptuneSolutions(ctx) {
		sapTuneReapply = true
	}
	if err := c.saptuneReapply(ctx, sapTuneReapply); err != nil {
		return false, err
	}

	log.CtxLogger(ctx).Info("SLES specific configurations complete.")
	return sapTuneReapply, nil
}

// saptuneService checks if saptune service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) saptuneService(ctx context.Context) error {
	// sapconf must be disabled and stopped before saptune can run.
	sapconfStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status sapconf"})
	if sapconfStatus.ExitCode != 4 {
		sapconfDisable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "disable sapconf"})
		if sapconfDisable.ExitCode != 0 {
			return fmt.Errorf("sapconf service could not be disabled, code: %d, stderr: %s", sapconfDisable.ExitCode, sapconfDisable.StdErr)
		}
		sapconfStop := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "stop sapconf"})
		if sapconfStop.ExitCode != 0 {
			return fmt.Errorf("sapconf service could not be stopped, code: %d, stderr: %s", sapconfStop.ExitCode, sapconfStop.StdErr)
		}
		log.CtxLogger(ctx).Info("The sapconf service is disabled and stopped.")
	}

	saptuneStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status saptune"})
	if saptuneStatus.ExitCode == 4 {
		return fmt.Errorf("saptune service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s", saptuneStatus.ExitCode, saptuneStatus.StdErr)
	}
	if saptuneStatus.ExitCode != 0 {
		log.CtxLogger(ctx).Info("Attempting to enable and start saptune.")
		saptuneEnable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "enable saptune"})
		if saptuneEnable.ExitCode != 0 {
			return fmt.Errorf("saptune service could not be enabled, code: %d, stderr: %s", saptuneEnable.ExitCode, saptuneEnable.StdErr)
		}
		saptuneStart := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "start saptune"})
		if saptuneStart.ExitCode != 0 {
			return fmt.Errorf("saptune service could not be started, code: %d, stderr: %s", saptuneStart.ExitCode, saptuneStart.StdErr)
		}
	}
	log.CtxLogger(ctx).Info("The saptune service is running.")
	return nil
}

// saptuneSolutions checks if SAPTune solutions and notes are correct.
// Returns true if saptune reapply is required.
func (c *ConfigureInstance) saptuneSolutions(ctx context.Context) bool {
	sapTuneReapply := false
	saptuneSolutions := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "status"})
	if match, _ := regexp.MatchString(`enabled Solution:\s*HANA`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled solution is not `HANA`, SAPTune re-apply required.")
		sapTuneReapply = true
	}
	if match, _ := regexp.MatchString(`additional enabled Notes:\s*google-x4`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled note is not `google-x4`, SAPTune re-apply required.")
		sapTuneReapply = true
	}
	return sapTuneReapply
}

// saptuneReapply executes SAPTune re-apply by applying the
// HANA solution and the google-x4 note.
func (c *ConfigureInstance) saptuneReapply(ctx context.Context, sapTuneReapply bool) error {
	if !sapTuneReapply {
		log.CtxLogger(ctx).Info("SAPTune re-apply is not required.")
		return nil
	}
	if c.Check {
		log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to execute SAPTune re-apply.")
		return nil
	}
	log.CtxLogger(ctx).Info("Executing SAPTune re-apply.")
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "solution revert HANA", Timeout: 120}); res.ExitCode != 0 {
		return fmt.Errorf("'saptune solution revert HANA' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
	}
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "solution apply HANA", Timeout: 120}); res.ExitCode != 0 {
		return fmt.Errorf("'saptune solution apply HANA' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
	}
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note revert google-x4", Timeout: 120}); res.ExitCode != 0 {
		return fmt.Errorf("'saptune note revert google-x4' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
	}
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note apply google-x4", Timeout: 120}); res.ExitCode != 0 {
		return fmt.Errorf("'saptune note apply google-x4' failed, code: %d, stderr: %s", res.ExitCode, res.StdErr)
	}
	return nil
}
