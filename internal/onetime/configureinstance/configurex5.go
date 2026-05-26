/*
Copyright 2026 Google LLC

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

	"github.com/Masterminds/semver/v3"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

var (
	//go:embed google-x5.conf
	googleX5Conf []byte

	//go:embed tuned.conf
	googleX5TunedConf []byte

	// TODO: Verify if X5 system requirements alter execution/timeouts.
	systemConfX5 = []string{"DefaultTimeoutStartSec=300s", "DefaultTimeoutStopSec=300s", "DefaultTasksMax=infinity"}
	logindConfX5 = []string{"UserTasksMax="}

	// TODO: Verify which kernel modules are blacklisted/required for X5 series.
	modprobeConfX5 = []byte("blacklist idxd\nblacklist hpilo\nblacklist acpi_cpufreq\nblacklist qat_4xxx\nblacklist intel_qat\n")

	// TODO: Verify if any specific X5 core arguments need to be appended or removed from CMDLINE.
	grubLinuxDefaultX5 = `GRUB_CMDLINE_LINUX_DEFAULT="tsc=nowatchdog add_efi_memmap udev.children-max=512 nmi_watchdog=0 watchdog_thresh=60 workqueue.watchdog_thresh=120 mce=2 console=ttyS0,115200 earlyprintk=ttyS0,115200 uv_nmi.action=kdump bau=0 pci=nobar transparent_hugepage=never numa_balancing=disable clocksource=tsc"`
)

// configureX5 checks and applies OS settings on X5 systems.
// Returns true if a reboot is required.
func (c *ConfigureInstance) configureX5(ctx context.Context) (bool, error) {
	log.CtxLogger(ctx).Info("Continuing with general X5 configurations.")
	rebootSystemdSystem, err := c.checkAndRegenerateLines(ctx, "/etc/systemd/system.conf", systemConfX5)
	if err != nil {
		return false, err
	}
	rebootSystemdLogin, err := c.removeLines(ctx, "/etc/systemd/logind.conf", logindConfX5)
	if err != nil {
		return false, err
	}
	rebootModprobe, err := c.checkAndRegenerateFile(ctx, "/etc/modprobe.d/google-x5.conf", modprobeConfX5)
	if err != nil {
		return false, err
	}
	if rebootModprobe && c.Apply {
		log.CtxLogger(ctx).Info("Regenerating modprobe by running 'usr/bin/dracut --force'.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "usr/bin/dracut", ArgsToSplit: "--force", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return false, fmt.Errorf("'usr/bin/dracut --force' failed, code: %d, stderr: %s, stdout: %s", res.ExitCode, res.StdErr, res.StdOut)
		}
	}
	if c.HyperThreading == hyperThreadingOff && c.Apply {
		log.CtxLogger(ctx).Infow("Hyper threading disabled, appending 'nosmt' to 'GRUB_CMDLINE_LINUX_DEFAULT'.", "machineType", c.MachineType, "hyperThreading", c.HyperThreading)
		grubLinuxDefaultX5 = strings.TrimSuffix(grubLinuxDefaultX5, `"`) + ` nosmt"`
	}
	if c.transparentHugePageAdviseX5(ctx) {
		grubLinuxDefaultX5 = strings.ReplaceAll(grubLinuxDefaultX5, "transparent_hugepage=never", "transparent_hugepage=madvise")
	}
	rebootGrub, err := c.checkAndRegenerateLines(ctx, "/etc/default/grub", []string{grubLinuxDefaultX5})
	if err != nil {
		return false, err
	}
	if c.HyperThreading == hyperThreadingOn && c.Apply {
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
			grubArgs := "-o /boot/grub2/grub.cfg"
			if c.grubBootLoaderX5(ctx) {
				log.CtxLogger(ctx).Info("Updating boot loader specification (BLS) by running 'grub2-mkconfig --update-bls-cmdline'.")
				grubArgs = grubArgs + " --update-bls-cmdline"
			}
			if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "grub2-mkconfig", ArgsToSplit: grubArgs, Timeout: c.TimeoutSec}); res.ExitCode != 0 {
				return false, fmt.Errorf("'grub2-mkconfig %s' failed, code: %d, stderr: %s, stdout: %s", grubArgs, res.ExitCode, res.StdErr, res.StdOut)
			}
		}
	}
	log.CtxLogger(ctx).Info("General X5 configurations complete.")

	rebootSLES, err := c.configureX5SLES(ctx)
	if err != nil {
		return false, fmt.Errorf("general X5 configurations completed, OS specific configurations failed: %v", err)
	}
	rebootRHEL, err := c.configureX5RHEL(ctx)
	if err != nil {
		return false, fmt.Errorf("general X5 configurations completed, OS specific configurations failed: %v", err)
	}
	return rebootSLES || rebootRHEL || rebootSystemdSystem || rebootSystemdLogin || rebootModprobe || rebootGrub, nil
}

// transparentHugePageAdviseX5 checks if the OS version is compatible with transparent huge page.
// Returns true if transparent huge page is set to advise, false if it is set to never.
func (c *ConfigureInstance) transparentHugePageAdviseX5(ctx context.Context) bool {
	osRelease, err := c.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	matches := versionRegex.FindStringSubmatch(string(osRelease))
	if len(matches) != 2 || matches[1] == "" {
		return false
	}
	version, err := semver.NewVersion(matches[1])
	if err != nil {
		return false
	}

	if strings.Contains(string(osRelease), "SLES") {
		return version.GreaterThanEqual(slesMinVersionTHP)
	}
	if strings.Contains(string(osRelease), "Red Hat Enterprise Linux") {
		return version.GreaterThanEqual(rhelMinVersionTHP)
	}
	return false
}

// grubBootLoaderX5 checks if boot loader specification (BLS) needs to update.
// Returns true if grub should update the BLS, false if not.
func (c *ConfigureInstance) grubBootLoaderX5(ctx context.Context) bool {
	osRelease, err := c.ReadFile("/etc/os-release")
	log.CtxLogger(ctx).Infof("osRelease: %s", string(osRelease))
	if err != nil {
		return false
	}
	matches := versionRegex.FindStringSubmatch(string(osRelease))
	if len(matches) != 2 || matches[1] == "" {
		return false
	}
	version, err := semver.NewVersion(matches[1])
	if err != nil {
		return false
	}

	// This is only needed for RHEL 9.0 and higher.
	if strings.Contains(string(osRelease), "Red Hat Enterprise Linux") {
		return version.GreaterThanEqual(rhelMinVersionBLS)
	}
	return false
}

// configureX5SLES checks and applies OS settings for X5 running on SLES.
// Returns true if SAPTune re-apply needed to be run.
func (c *ConfigureInstance) configureX5SLES(ctx context.Context) (bool, error) {
	osRelease, err := c.ReadFile("/etc/os-release")
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(osRelease), "SLES") {
		log.CtxLogger(ctx).Info("SLES OS not detected, skipping specific configurations.")
		return false, nil
	}
	log.CtxLogger(ctx).Info("SLES OS detected, continuing with specific configurations.")

	if err := c.saptuneServiceX5(ctx); err != nil {
		return false, err
	}

	// TODO: Verify if the google-x5 note/conf differs from google-x4 under SLES.
	noteRegenerated, err := c.checkAndRegenerateFile(ctx, "/etc/saptune/extra/google-x5.conf", googleX5Conf)
	if err != nil {
		return false, err
	}
	solutionReapply, noteReapply := c.saptuneSolutionsX5(ctx)
	noteReapply = noteReapply || noteRegenerated
	if err := c.saptuneReapplyX5(ctx, solutionReapply, noteReapply); err != nil {
		return false, err
	}
	log.CtxLogger(ctx).Info("SLES specific configurations complete.")
	return solutionReapply || noteReapply, nil
}

// saptuneServiceX5 checks if saptune service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) saptuneServiceX5(ctx context.Context) error {
	// sapconf must be disabled and stopped before saptune can run.
	sapconfStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status sapconf", Timeout: c.TimeoutSec})
	if sapconfStatus.ExitCode != 4 {
		sapconfDisable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "disable sapconf", Timeout: c.TimeoutSec})
		if sapconfDisable.ExitCode != 0 {
			return fmt.Errorf("sapconf service could not be disabled, code: %d, stderr: %s, stdout: %s", sapconfDisable.ExitCode, sapconfDisable.StdErr, sapconfDisable.StdOut)
		}
		sapconfStop := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "stop sapconf", Timeout: c.TimeoutSec})
		if sapconfStop.ExitCode != 0 {
			return fmt.Errorf("sapconf service could not be stopped, code: %d, stderr: %s, stdout: %s", sapconfStop.ExitCode, sapconfStop.StdErr, sapconfStop.StdOut)
		}
		log.CtxLogger(ctx).Info("The sapconf service is disabled and stopped.")
	}

	saptuneStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status saptune", Timeout: c.TimeoutSec})
	if saptuneStatus.ExitCode == 4 {
		return fmt.Errorf("saptune service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s, stdout: %s", saptuneStatus.ExitCode, saptuneStatus.StdErr, saptuneStatus.StdOut)
	}
	if saptuneStatus.ExitCode != 0 {
		log.CtxLogger(ctx).Info("Attempting to enable and start saptune.")
		saptuneEnable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "enable saptune", Timeout: c.TimeoutSec})
		if saptuneEnable.ExitCode != 0 {
			return fmt.Errorf("saptune service could not be enabled, code: %d, stderr: %s, stdout: %s", saptuneEnable.ExitCode, saptuneEnable.StdErr, saptuneEnable.StdOut)
		}
		saptuneStart := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "start saptune", Timeout: c.TimeoutSec})
		if saptuneStart.ExitCode != 0 {
			return fmt.Errorf("saptune service could not be started, code: %d, stderr: %s, stdout: %s", saptuneStart.ExitCode, saptuneStart.StdErr, saptuneStart.StdOut)
		}
	}
	log.CtxLogger(ctx).Info("The saptune service is running.")
	return nil
}

// saptuneSolutionsX5 checks if SAPTune solutions and notes are correct for X5.
// Returns true if saptune solution and note reapply is required.
func (c *ConfigureInstance) saptuneSolutionsX5(ctx context.Context) (bool, bool) {
	solutionReapply, noteReapply := false, false
	saptuneSolutions := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "status", Timeout: c.TimeoutSec})
	if match, _ := regexp.MatchString(`enabled Solution:\s*(HANA|NETWEAVER\+HANA|S4HANA\-APP\+DB|S4HANA\-DBSERVER)`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled solution is not `(HANA|NETWEAVER+HANA|S4HANA-APP+DB|S4HANA-DBSERVER)`, SAPTune solution re-apply required.")
		solutionReapply = true
	}
	// TODO: Update saptune solutions check note when a custom X5 note is developed.
	if match, _ := regexp.MatchString(`additional enabled Notes:\s*google-x5`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled note is not `google-x5`, SAPTune note re-apply required.")
		noteReapply = true
	}
	return solutionReapply, noteReapply
}

// saptuneReapplyX5 executes SAPTune re-apply by applying the
// HANA solution and the google-x5 note.
func (c *ConfigureInstance) saptuneReapplyX5(ctx context.Context, solutionReapply, noteReapply bool) error {
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
			return fmt.Errorf("'saptune solution change --force HANA' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
	}
	if noteReapply {
		log.CtxLogger(ctx).Info("Executing SAPTune note re-apply.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note revert google-x5", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note revert google-x5' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note apply google-x5", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note apply google-x5' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
	}
	return nil
}

// configureX5RHEL checks and applies OS settings for X5 running on RHEL.
// Returns true if tuned reapply needed to be run.
func (c *ConfigureInstance) configureX5RHEL(ctx context.Context) (bool, error) {
	osRelease, err := c.ReadFile("/etc/os-release")
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(osRelease), "Red Hat Enterprise Linux") {
		log.CtxLogger(ctx).Info("RHEL OS not detected, skipping specific configurations.")
		return false, nil
	}
	log.CtxLogger(ctx).Info("RHEL OS detected, continuing with specific configurations.")

	if err := c.tunedServiceX5(ctx); err != nil {
		return false, err
	}

	// TODO: Verify if custom tuned parameters for X5 series require changes in the tuned profile.
	tunedReapply, err := c.checkAndRegenerateFile(ctx, "/etc/tuned/google-x5/tuned.conf", googleX5TunedConf)
	if err != nil {
		return false, err
	}
	tunedActive := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active", Timeout: c.TimeoutSec})
	if !strings.Contains(tunedActive.StdOut, "google-x5") {
		log.CtxLogger(ctx).Info("Active profile is not `google-x5`, Tuned re-apply required.")
		tunedReapply = true
	}
	if err := c.tunedReapplyX5(ctx, tunedReapply); err != nil {
		return false, err
	}

	log.CtxLogger(ctx).Info("Tuned re-apply set the active profile to google-x5, verifying tuned settings.")
	if c.Apply {
		tunedRestart := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "restart tuned", Timeout: c.TimeoutSec})
		if tunedRestart.ExitCode != 0 {
			return false, fmt.Errorf("tuned service could not be re-started, code: %d, stderr: %s, stdout: %s", tunedRestart.ExitCode, tunedRestart.StdErr, tunedRestart.StdOut)
		}
	}
	tunedVerify := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "verify", Timeout: c.TimeoutSec})
	if tunedVerify.ExitCode != 0 {
		return false, fmt.Errorf("'tuned-adm verify' failed, current system settings differ from the preset profile. Reboot the system after running 'configureinstance -apply' to ensure the changes have taken effect")
	}

	log.CtxLogger(ctx).Info("RHEL specific configurations complete.")
	return tunedReapply, nil
}

// tunedServiceX5 checks if tuned service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) tunedServiceX5(ctx context.Context) error {
	tunedStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status tuned", Timeout: c.TimeoutSec})
	if tunedStatus.ExitCode == 4 {
		return fmt.Errorf("tuned service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s, stdout: %s", tunedStatus.ExitCode, tunedStatus.StdErr, tunedStatus.StdOut)
	}
	if tunedStatus.ExitCode != 0 {
		log.CtxLogger(ctx).Info("Attempting to enable and start tuned.")
		tunedEnable := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "enable tuned", Timeout: c.TimeoutSec})
		if tunedEnable.ExitCode != 0 {
			return fmt.Errorf("tuned service could not be enabled, code: %d, stderr: %s, stdout: %s", tunedEnable.ExitCode, tunedEnable.StdErr, tunedEnable.StdOut)
		}
		tunedStart := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "start tuned", Timeout: c.TimeoutSec})
		if tunedStart.ExitCode != 0 {
			return fmt.Errorf("tuned service could not be started, code: %d, stderr: %s, stdout: %s", tunedStart.ExitCode, tunedStart.StdErr, tunedStart.StdOut)
		}
	}
	log.CtxLogger(ctx).Info("The tuned service is running.")
	return nil
}

// tunedReapplyX5 executes Tuned re-apply by activating the google-x5 profile.
func (c *ConfigureInstance) tunedReapplyX5(ctx context.Context, tunedReapply bool) error {
	if !tunedReapply {
		log.CtxLogger(ctx).Info("Tuned re-apply is not required.")
		return nil
	}
	if c.Check {
		log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to execute Tuned re-apply.")
		return fmt.Errorf("tuned re-apply is required, run 'configureinstance -apply' to execute Tuned re-apply")
	}
	log.CtxLogger(ctx).Info("Executing Tuned re-apply.")
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "profile google-x5", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
		return fmt.Errorf("'tuned-adm profile google-x5' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
	}
	tunedActive := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active", Timeout: c.TimeoutSec})
	if !strings.Contains(tunedActive.StdOut, "google-x5") {
		return fmt.Errorf("active profile is not `google-x5`, Tuned re-apply failed")
	}
	return nil
}
