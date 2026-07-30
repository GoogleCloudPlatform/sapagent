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

	"github.com/Masterminds/semver/v3"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

var (
	//go:embed google-x4.conf
	googleX4Conf []byte

	//go:embed tuned-x4.conf
	googleX4TunedConf []byte

	systemConf       = []string{"DefaultTimeoutStartSec=300s", "DefaultTimeoutStopSec=300s", "DefaultTasksMax=infinity"}
	logindConf       = []string{"UserTasksMax="}
	modprobeConf     = []byte("blacklist idxd\nblacklist hpilo\nblacklist acpi_cpufreq\nblacklist qat_4xxx\nblacklist intel_qat\n")
	grubLinuxDefault = `GRUB_CMDLINE_LINUX_DEFAULT="tsc=nowatchdog add_efi_memmap udev.children-max=512 nmi_watchdog=0 watchdog_thresh=60 workqueue.watchdog_thresh=120 mce=2 console=ttyS0,115200 earlyprintk=ttyS0,115200 uv_nmi.action=kdump bau=0 pci=nobar transparent_hugepage=never numa_balancing=disable clocksource=tsc"`

	versionRegex      = regexp.MustCompile(`VERSION_ID="([^"]*)"`)
	slesMinVersionTHP = semver.MustParse("15.5")
	rhelMinVersionTHP = semver.MustParse("9.2")
	rhelMinVersionBLS = semver.MustParse("9.0")
)

/* LINT.IfChange(x4_apply) */

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
		log.CtxLogger(ctx).Info("Regenerating modprobe by running '/usr/bin/dracut --force'.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "/usr/bin/dracut", ArgsToSplit: "--force", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return false, fmt.Errorf("'/usr/bin/dracut --force' failed, code: %d, stderr: %s, stdout: %s", res.ExitCode, res.StdErr, res.StdOut)
		}
	}
	if c.HyperThreading == hyperThreadingOff && c.Apply {
		log.CtxLogger(ctx).Infow("Hyper threading disabled, appending 'nosmt' to 'GRUB_CMDLINE_LINUX_DEFAULT'.", "machineType", c.MachineType, "hyperThreading", c.HyperThreading)
		grubLinuxDefault = strings.TrimSuffix(grubLinuxDefault, `"`) + ` nosmt"`
	}
	if c.transparentHugePageAdvise(ctx) {
		grubLinuxDefault = strings.ReplaceAll(grubLinuxDefault, "transparent_hugepage=never", "transparent_hugepage=madvise")
	}
	rebootGrub, err := c.checkAndRegenerateLines(ctx, "/etc/default/grub", []string{grubLinuxDefault})
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
			if c.grubBootLoader(ctx) {
				log.CtxLogger(ctx).Info("Updating boot loader specification (BLS) by running 'grub2-mkconfig --update-bls-cmdline'.")
				grubArgs = grubArgs + " --update-bls-cmdline"
			}
			if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "grub2-mkconfig", ArgsToSplit: grubArgs, Timeout: c.TimeoutSec}); res.ExitCode != 0 {
				return false, fmt.Errorf("'grub2-mkconfig %s' failed, code: %d, stderr: %s, stdout: %s", grubArgs, res.ExitCode, res.StdErr, res.StdOut)
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

// transparentHugePageAdvise checks if the OS version is compatible with transparent huge page.
// Returns true if transparent huge page is set to advise, false if it is set to never.
func (c *ConfigureInstance) transparentHugePageAdvise(ctx context.Context) bool {
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

// grubBootLoader checks if boot loader specification (BLS) needs to update.
// Returns true if grub should update the BLS, false if not.
func (c *ConfigureInstance) grubBootLoader(ctx context.Context) bool {
	res := c.ExecuteFunc(ctx, commandlineexecutor.Params{
		Executable:  "grub2-mkconfig",
		ArgsToSplit: "--help",
		Timeout:     c.TimeoutSec,
	})
	return strings.Contains(res.StdOut, "update-bls-cmdline") || strings.Contains(res.StdErr, "update-bls-cmdline")
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
	if err := c.saptuneVerify(ctx); err != nil {
		return false, err
	}
	log.CtxLogger(ctx).Info("SLES specific configurations complete.")
	return solutionReapply || noteReapply, nil
}

// saptuneService checks if saptune service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) saptuneService(ctx context.Context) error {
	// Saptune can take a while to apply changes after a reboot. Alert the
	// user to run configureinstance after a few minutes if saptune is activating.
	if c.Check {
		checkRunning := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "show -p ActiveState saptune", Timeout: c.TimeoutSec})
		if strings.Contains(checkRunning.StdOut, "activating") {
			return fmt.Errorf("saptune is still applying changes after a reboot. Run configureinstance after a few minutes")
		}
	}

	// sapconf must be disabled and stopped before saptune can run.
	sapconfStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status sapconf", Timeout: c.TimeoutSec})
	if sapconfStatus.ExitCode == 0 {
		if c.Check {
			log.CtxLogger(ctx).Info("sapconf service is running")
			return fmt.Errorf("SLES specific configurations are not compliant, please run 'configureinstance -apply' to fix")
		}
		// First, kill the running processes to prevent long timeouts.
		// Do not check exit codes as the process may already be dead or not loaded.
		c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "kill sapconf", Timeout: c.TimeoutSec})
		c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "kill tuned", Timeout: c.TimeoutSec})
		// Remove the lockfile.
		c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "rm", ArgsToSplit: "-f /run/sapconf/active", Timeout: c.TimeoutSec})
		// Takeover sapconf which disables and stops the service.
		sapconfTakeover := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "service takeover", Timeout: c.TimeoutSec})
		if sapconfTakeover.ExitCode != 0 {
			return fmt.Errorf("sapconf service could not be taken over, code: %d, stderr: %s, stdout: %s", sapconfTakeover.ExitCode, sapconfTakeover.StdErr, sapconfTakeover.StdOut)
		}
		log.CtxLogger(ctx).Info("The sapconf service is disabled and stopped.")
	}

	saptuneStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status saptune", Timeout: c.TimeoutSec})
	if saptuneStatus.ExitCode == 4 {
		return fmt.Errorf("saptune service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s, stdout: %s", saptuneStatus.ExitCode, saptuneStatus.StdErr, saptuneStatus.StdOut)
	}
	if saptuneStatus.ExitCode != 0 {
		if c.Check {
			return fmt.Errorf("saptune service is not running, run 'configureinstance -apply' to enable and start it")
		}
		log.CtxLogger(ctx).Info("Attempting to enable and start saptune.")
		// Remove any lingering lockfile.
		c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "rm", ArgsToSplit: "-f /run/sapconf/active", Timeout: c.TimeoutSec})
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

// saptuneSolutions checks if SAPTune solutions and notes are correct.
// Returns true if saptune solution and note reapply is required.
func (c *ConfigureInstance) saptuneSolutions(ctx context.Context) (bool, bool) {
	solutionReapply, noteReapply := false, false
	saptuneSolutions := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "status", Timeout: c.TimeoutSec})
	if match, _ := regexp.MatchString(`enabled Solution:\s*(HANA|NETWEAVER\+HANA|S4HANA\-APP\+DB|S4HANA\-DBSERVER)`, saptuneSolutions.StdOut); !match {
		log.CtxLogger(ctx).Info("Enabled solution is not `(HANA|NETWEAVER+HANA|S4HANA-APP+DB|S4HANA-DBSERVER)`, SAPTune solution re-apply required.")
		solutionReapply = true
	}
	if match, _ := regexp.MatchString(`additional enabled Notes:.*\bgoogle-x4\b`, saptuneSolutions.StdOut); !match {
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
			return fmt.Errorf("'saptune solution change --force HANA' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
	}
	if noteReapply {
		log.CtxLogger(ctx).Info("Executing SAPTune note re-apply.")
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note revert google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note revert google-x4' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
		if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note apply google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
			return fmt.Errorf("'saptune note apply google-x4' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
		}
	}
	return nil
}

// saptuneVerify verifies the solution and note are configured correctly.
func (c *ConfigureInstance) saptuneVerify(ctx context.Context) error {
	verifySolution := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "solution verify HANA", Timeout: c.TimeoutSec})
	if verifySolution.ExitCode != 0 {
		return fmt.Errorf("'saptune solution verify HANA' failed, code: %d, err: %v, stderr: %s, stdout: %s", verifySolution.ExitCode, verifySolution.Error, verifySolution.StdErr, verifySolution.StdOut)
	}
	verifyNote := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "note verify google-x4", Timeout: c.TimeoutSec})
	if verifyNote.ExitCode != 0 {
		return fmt.Errorf("'saptune note verify google-x4' failed, code: %d, err: %v, stderr: %s, stdout: %s", verifyNote.ExitCode, verifyNote.Error, verifyNote.StdErr, verifyNote.StdOut)
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
	tunedActive := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active", Timeout: c.TimeoutSec})
	if !strings.Contains(tunedActive.StdOut, "google-x4") {
		log.CtxLogger(ctx).Info("Active profile is not `google-x4`, Tuned re-apply required.")
		tunedReapply = true
	}
	if err := c.tunedReapply(ctx, tunedReapply); err != nil {
		return false, err
	}

	log.CtxLogger(ctx).Info("Tuned re-apply set the active profile to google-x4, verifying tuned settings.")
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

// tunedService checks if tuned service is running. If it is not running,
// it will attempt to enable and start it through systemctl.
func (c *ConfigureInstance) tunedService(ctx context.Context) error {
	tunedStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "status tuned", Timeout: c.TimeoutSec})
	if tunedStatus.ExitCode == 4 {
		return fmt.Errorf("tuned service could not be found, ensure it is installed before running 'configureinstance', code: %d, stderr: %s, stdout: %s", tunedStatus.ExitCode, tunedStatus.StdErr, tunedStatus.StdOut)
	}
	if tunedStatus.ExitCode != 0 {
		if c.Check {
			return fmt.Errorf("tuned service is not running, run 'configureinstance -apply' to enable and start it")
		}
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

// tunedReapply executes Tuned re-apply by activating the google-x4 profile.
func (c *ConfigureInstance) tunedReapply(ctx context.Context, tunedReapply bool) error {
	if !tunedReapply {
		log.CtxLogger(ctx).Info("Tuned re-apply is not required.")
		return nil
	}
	if c.Check {
		log.CtxLogger(ctx).Info("Run 'configureinstance -apply' to execute Tuned re-apply.")
		return fmt.Errorf("tuned re-apply is required, run 'configureinstance -apply' to execute Tuned re-apply")
	}
	log.CtxLogger(ctx).Info("Executing Tuned re-apply.")
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "profile google-x4", Timeout: c.TimeoutSec}); res.ExitCode != 0 {
		return fmt.Errorf("'tuned-adm profile google-x4' failed, code: %d, err: %v, stderr: %s, stdout: %s", res.ExitCode, res.Error, res.StdErr, res.StdOut)
	}
	tunedActive := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active", Timeout: c.TimeoutSec})
	if !strings.Contains(tunedActive.StdOut, "google-x4") {
		return fmt.Errorf("active profile is not `google-x4`, Tuned re-apply failed")
	}
	return nil
}

/* LINT.ThenChange(:x4_describe) */

/* LINT.IfChange(x4_describe) */
// describeX4 collects and outputs configuration rules for X4 machines.
func (c *ConfigureInstance) describeX4(ctx context.Context) (subcommands.ExitStatus, string) {
	var rules []ConfigRule

	// Systemd rules
	for _, line := range systemConf {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		exp := strings.TrimSpace(parts[1])
		curr := c.readCurrentLineValue("/etc/systemd/system.conf", key)
		rules = append(rules, ConfigRule{
			Category:       "Systemd",
			TargetFile:     "/etc/systemd/system.conf",
			ParameterKey:   key,
			ExpectedValue:  exp,
			CurrentValue:   curr,
			RebootRequired: true,
		})
	}
	currUserTasks := c.readCurrentLineValue("/etc/systemd/logind.conf", "UserTasksMax")
	rules = append(rules, ConfigRule{
		Category:       "Systemd",
		TargetFile:     "/etc/systemd/logind.conf",
		ParameterKey:   "UserTasksMax",
		ExpectedValue:  "",
		CurrentValue:   currUserTasks,
		RebootRequired: true,
	})

	// Modprobe rules
	for _, line := range strings.Split(string(modprobeConf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		curr := c.readCurrentLineValue("/etc/modprobe.d/google-x4.conf", line)
		rules = append(rules, ConfigRule{
			Category:       "Modprobe",
			TargetFile:     "/etc/modprobe.d/google-x4.conf",
			ParameterKey:   line,
			ExpectedValue:  line,
			CurrentValue:   curr,
			RebootRequired: true,
		})
	}

	// Grub rules
	currGrub := c.readCurrentLineValue("/etc/default/grub", "GRUB_CMDLINE_LINUX_DEFAULT")
	rules = append(rules, ConfigRule{
		Category:       "Grub",
		TargetFile:     "/etc/default/grub",
		ParameterKey:   "GRUB_CMDLINE_LINUX_DEFAULT",
		ExpectedValue:  grubLinuxDefault,
		CurrentValue:   currGrub,
		RebootRequired: true,
	})

	// Sysctl rules from embedded google-x4.conf
	for _, line := range strings.Split(string(googleX4Conf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			curr := c.readCurrentLineValue("/etc/sysctl.d/google-x4.conf", key)
			rules = append(rules, ConfigRule{
				Category:       "Sysctl",
				TargetFile:     "/etc/sysctl.d/google-x4.conf",
				ParameterKey:   key,
				ExpectedValue:  val,
				CurrentValue:   curr,
				RebootRequired: false,
			})
		}
	}

	// Services & OS-specific rules (SLES / RHEL)
	osRelease := c.readCurrentFileContent("/etc/os-release")
	if strings.Contains(osRelease, "SLES") {
		sapconfStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "show -p ActiveState sapconf", Timeout: c.TimeoutSec})
		sapconfVal := strings.TrimSpace(sapconfStatus.StdOut)
		if sapconfVal == "" {
			sapconfVal = "NOT_SET"
		}
		rules = append(rules, ConfigRule{
			Category:       "Service",
			TargetFile:     "sapconf",
			ParameterKey:   "ActiveState",
			ExpectedValue:  "inactive",
			CurrentValue:   sapconfVal,
			RebootRequired: false,
		})

		saptuneStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "show -p ActiveState saptune", Timeout: c.TimeoutSec})
		saptuneVal := strings.TrimSpace(saptuneStatus.StdOut)
		if saptuneVal == "" {
			saptuneVal = "NOT_SET"
		}
		rules = append(rules, ConfigRule{
			Category:       "Service",
			TargetFile:     "saptune",
			ParameterKey:   "ActiveState",
			ExpectedValue:  "active",
			CurrentValue:   saptuneVal,
			RebootRequired: false,
		})

		saptuneProf := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "saptune", ArgsToSplit: "status", Timeout: c.TimeoutSec})
		profVal := strings.TrimSpace(saptuneProf.StdOut)
		if profVal == "" {
			profVal = "NOT_SET"
		}
		rules = append(rules, ConfigRule{
			Category:       "SaptuneProfile",
			TargetFile:     "saptune",
			ParameterKey:   "EnabledSolution",
			ExpectedValue:  "HANA (or NETWEAVER+HANA / S4HANA)",
			CurrentValue:   profVal,
			RebootRequired: false,
		})
	} else if strings.Contains(osRelease, "Red Hat Enterprise Linux") {
		tunedStatus := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "systemctl", ArgsToSplit: "show -p ActiveState tuned", Timeout: c.TimeoutSec})
		tunedVal := strings.TrimSpace(tunedStatus.StdOut)
		if tunedVal == "" {
			tunedVal = "NOT_SET"
		}
		rules = append(rules, ConfigRule{
			Category:       "Service",
			TargetFile:     "tuned",
			ParameterKey:   "ActiveState",
			ExpectedValue:  "active",
			CurrentValue:   tunedVal,
			RebootRequired: false,
		})

		tunedProf := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "tuned-adm", ArgsToSplit: "active", Timeout: c.TimeoutSec})
		profVal := strings.TrimSpace(tunedProf.StdOut)
		if profVal == "" {
			profVal = "NOT_SET"
		}
		rules = append(rules, ConfigRule{
			Category:       "TunedProfile",
			TargetFile:     "tuned-adm",
			ParameterKey:   "ActiveProfile",
			ExpectedValue:  "google-x4",
			CurrentValue:   profVal,
			RebootRequired: false,
		})
	}

	return c.formatDescribeOutput("X4", rules)
}

/* LINT.ThenChange(:x4_apply) */
