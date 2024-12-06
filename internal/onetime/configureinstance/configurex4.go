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

	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

var (
	//go:embed google-x4.conf
	googleX4Conf []byte

	//go:embed tuned.conf
	googleX4TunedConf []byte

	systemConf       = []string{"DefaultTimeoutStartSec=300s", "DefaultTimeoutStopSec=300s", "DefaultTasksMax=infinity"}
	logindConf       = []string{"UserTasksMax="}
	modprobeConf     = []byte("blacklist idxd\nblacklist hpilo\nblacklist acpi_cpufreq\nblacklist qat_4xxx\nblacklist intel_qat\n")
	grubLinuxDefault = `GRUB_CMDLINE_LINUX_DEFAULT="tsc=nowatchdog add_efi_memmap udev.children-max=512 nmi_watchdog=0 watchdog_thresh=60 mce=2 console=ttyS0,115200 earlyprintk=ttyS0,115200 uv_nmi.action=kdump bau=0 pci=nobar transparent_hugepage=never numa_balancing=disable clocksource=tsc"`
)

// configureX4 checks and applies OS settings on X4.
// Returns true if a reboot is required.
func (c *ConfigureInstance) configureX4(ctx context.Context) (bool, error) {
	log.CtxLogger(ctx).Info("Continuing with general X4 configurations.")
	rebootSystemdSystem, err := c.checkAndRegenerateLines(ctx, "/etc/systemd/system.conf", systemConf)
	if err != nil {
		return false, err
	}
	rebootSystemdLogin, err := c.removeLines(ctx, "/etc/systemd/logind.conf", logindConf)
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
	if c.HyperThreading == hyperThreadingOff {
		log.CtxLogger(ctx).Infow("Hyper threading disabled, appending 'nosmt' to 'GRUB_CMDLINE_LINUX_DEFAULT'.", "machineType", c.MachineType, "hyperThreading", c.HyperThreading)
		grubLinuxDefault = strings.TrimSuffix(grubLinuxDefault, `"`) + ` nosmt"`
	}
	rebootGrub, err := c.checkAndRegenerateLines(ctx, "/etc/default/grub", []string{grubLinuxDefault})
	if err != nil {
		return false, err
	}
	if c.HyperThreading == hyperThreadingOn {
		log.CtxLogger(ctx).Infow("Hyper threading enabled, ensuring 'nosmt' is removed from 'GRUB_CMDLINE_LINUX_DEFAULT'.", "machineType", c.MachineType, "hyperThreading", c.HyperThreading)
		removeNosmt, err := c.removeValues(ctx, "/etc/default/grub", []string{"GRUB_CMDLINE_LINUX_DEFAULT=nosmt"})
		if err != nil {
			return false, err
		}
		rebootGrub = rebootGrub || removeNosmt
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

	rebootSLES, err := c.configureX4SLES(ctx)
	if err != nil {
		return false, fmt.Errorf("general X4 configurations completed, OS specific configurations failed: %v", err)
	}
	rebootRHEL, err := c.configureX4RHEL(ctx)
	if err != nil {
		return false, fmt.Errorf("general X4 configurations completed, OS specific configurations failed: %v", err)
	}
	return rebootSLES || rebootRHEL || rebootSystemdSystem || rebootSystemdLogin || rebootModprobe || rebootGrub, nil
}

// configureX4SLES checks and applies OS settings for X4 running on SLES.
// Returns true if SAPTune re-apply needed to be run.
func (c *ConfigureInstance) configureX4SLES(ctx context.Context) (bool, error) {
	osRelease, err := c.ReadFile("/etc/os-release")
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(osRelease), "SLES") {
		log.CtxLogger(ctx).Info("SLES OS not detected, skipping specific configurations.")
		return false, nil
	}
	log.CtxLogger(ctx).Info("SLES OS detected, continuing with specific configurations.")

	if err := c.saptuneService(ctx); err != nil {
		return false, err
	}
	noteRegenerated, err := c.checkAndRegenerateFile(ctx, "/etc/saptune/extra/google-x4.conf", googleX4Conf)
	if err != nil {
		return false, err
	}
	solutionReapply, noteReapply := c.saptuneSolutions(ctx)
	noteReapply = noteReapply || noteRegenerated
	if err := c.saptuneReapply(ctx, solutionReapply, noteReapply); err != nil {
		return false, err
	}
	log.CtxLogger(ctx).Info("SLES specific configurations complete.")
	return solutionReapply || noteReapply, nil
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
// Returns true if saptune solution and note reapply is required.
func (c *ConfigureInstance) saptuneSolutions(ctx context.Context) (bool, bool) {
	solutionReapply, noteReapply := false, false
	saptuneSolutions := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "status"})
	if match, _ := regexp.MatchString(`enabled Solution:\s*(HANA|NETWEAVER\+HANA|S4HANA\-APP\+DB|S4HANA\-DBSERVER)`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled solution is not `(HANA|NETWEAVER+HANA|S4HANA-APP+DB|S4HANA-DBSERVER)`, SAPTune solution re-apply required.")
		solutionReapply = true
	}
	if match, _ := regexp.MatchString(`additional enabled Notes:\s*google-x4`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled note is not `google-x4`, SAPTune note re-apply required.")
		noteReapply = true
	}
	return solutionReapply, noteReapply
}

// saptuneReapply executes SAPTune re-apply by applying the
// HANA solution and the google-x4 note.
func (c *ConfigureInstance) saptuneReapply(ctx context.Context, solutionReapply, noteReapply bool) error {
	if !solutionReapply && !noteReapply {
		log.CtxLogger(ctx).Info("SAPTune re-apply is not required.")
		return nil
	}
	if c.Check {
		log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to execute SAPTune re-apply.")
		return nil
	}
	if solutionReapply {
		log.CtxLogger(ctx).Info("Executing SAPTune solution re-apply.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "solution change --force HANA", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune solution change --force HANA' failed, code: %d, err: %v, stderr: %s", res.ExitCode, res.Error, res.StdErr)
		}
	}
	if noteReapply {
		log.CtxLogger(ctx).Info("Executing SAPTune note re-apply.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note revert google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note revert google-x4' failed, code: %d, err: %v, stderr: %s", res.ExitCode, res.Error, res.StdErr)
		}
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note apply google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note apply google-x4' failed, code: %d, err: %v, stderr: %s", res.ExitCode, res.Error, res.StdErr)
		}
	}
	return nil
}

// configureX4RHEL checks and applies OS settings for X4 running on RHEL.
// Returns true if tuned reapply needed to be run.
func (c *ConfigureInstance) configureX4RHEL(ctx context.Context) (bool, error) {
	osRelease, err := c.ReadFile("/etc/os-release")
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(osRelease), "Red Hat Enterprise Linux") {
		log.CtxLogger(ctx).Info("RHEL OS not detected, skipping specific configurations.")
		return false, nil
	}
	log.CtxLogger(ctx).Info("RHEL OS detected, continuing with specific configurations.")

	if err := c.tunedService(ctx); err != nil {
		return false, err
	}
	tunedReapply, err := c.checkAndRegenerateFile(ctx, "/etc/tuned/google-x4/tuned.conf", googleX4TunedConf)
	if err != nil {
		return false, err
	}
	tunedActive := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active"})
	if !strings.Contains(tunedActive.StdOut, "google-x4") {
		log.CtxLogger(ctx).Info("Active profile is not `google-x4`, Tuned re-apply required.")
		tunedReapply = true
	}
	if err := c.tunedReapply(ctx, tunedReapply); err != nil {
		return false, err
	}

	log.CtxLogger(ctx).Info("RHEL specific configurations complete.")
	return tunedReapply, nil
}

// tunedService checks if tuned service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) tunedService(ctx context.Context) error {
	tunedStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status tuned"})
	if tunedStatus.ExitCode == 4 {
		return fmt.Errorf("tuned service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s", tunedStatus.ExitCode, tunedStatus.StdErr)
	}
	if tunedStatus.ExitCode != 0 {
		log.CtxLogger(ctx).Info("Attempting to enable and start tuned.")
		tunedEnable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "enable tuned"})
		if tunedEnable.ExitCode != 0 {
			return fmt.Errorf("tuned service could not be enabled, code: %d, stderr: %s", tunedEnable.ExitCode, tunedEnable.StdErr)
		}
		tunedStart := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "start tuned"})
		if tunedStart.ExitCode != 0 {
			return fmt.Errorf("tuned service could not be started, code: %d, stderr: %s", tunedStart.ExitCode, tunedStart.StdErr)
		}
	}
	log.CtxLogger(ctx).Info("The tuned service is running.")
	return nil
}

// tunedReapply executes Tuned re-apply by activating the google-x4 profile.
func (c *ConfigureInstance) tunedReapply(ctx context.Context, tunedReapply bool) error {
	if !tunedReapply {
		log.CtxLogger(ctx).Info("Tuned re-apply is not required.")
		return nil
	}
	if c.Check {
		log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to execute Tuned re-apply.")
		return nil
	}
	log.CtxLogger(ctx).Info("Executing Tuned re-apply.")
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "profile google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
		return fmt.Errorf("'tuned-adm profile google-x4' failed, code: %d, err: %v, stderr: %s", res.ExitCode, res.Error, res.StdErr)
	}
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "verify", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
		return fmt.Errorf("'tuned-adm verify' failed, code: %d, err: %v, stderr: %s", res.ExitCode, res.Error, res.StdErr)
	}
	return nil
}
