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

// Package configureinstance implements OTE mode for checking and applying
// OS settings to support SAP HANA workloads.
package configureinstance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// writeFileFunc provides a testable replacement for os.WriteFile.
	writeFileFunc func(string, []byte, os.FileMode) error

	// readFileFunc provides a testable replacement for os.ReadFile.
	readFileFunc func(string) ([]byte, error)
)

// ConfigureInstance has args for configureinstance subcommands.
type ConfigureInstance struct {
	check, apply  bool
	machineType   string
	overrideOLAP  bool
	help, version bool

	writeFile writeFileFunc
	readFile  readFileFunc
	execute   commandlineexecutor.Execute
}

// Name implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Name() string { return "configureinstance" }

// Synopsis implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Synopsis() string {
	return "check and apply OS settings to support SAP HANA workloads"
}

// Usage implements the subcommand interface for configureinstance.
func (*ConfigureInstance) Usage() string {
	return `Usage: configureinstance <subcommand> [args]

  Subcommands:
    -check	Check settings and print errors, but do not apply any changes
    -apply	Make changes as necessary to the settings

  Args:
    -overrideType="type"	Bypass the metadata machine type lookup
    -overrideOLAP=true		If true, removes 'nosmt' from the 'GRUB_CMDLINE_LINUX_DEFAULT' in '/etc/default/grub'

  Global options:
    [-h] [-v]` + "\n"
}

// SetFlags implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.check, "check", false, "Check settings and print errors, but do not apply any changes")
	fs.BoolVar(&c.apply, "apply", false, "Apply changes as necessary to the settings")
	fs.StringVar(&c.machineType, "overrideType", "", "Bypass the metadata machine type lookup")
	fs.BoolVar(&c.overrideOLAP, "overrideOLAP", false, "If true, removes 'nosmt' from the 'GRUB_CMDLINE_LINUX_DEFAULT' in '/etc/default/grub'")
	fs.BoolVar(&c.help, "h", false, "Displays help")
	fs.BoolVar(&c.version, "v", false, "Displays the current version of the agent")
}

// Execute implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if c.help {
		return onetime.HelpCommand(f)
	}
	if c.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	cloudProps, ok := args[2].(*ipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, c.Name(), log.StringLevelToZapcore("info"))

	if !c.check && !c.apply {
		fmt.Printf("-check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("-check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if c.check && c.apply {
		fmt.Printf("Only one of -check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("Only one of -check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if c.machineType == "" {
		c.machineType = cloudProps.GetMachineType()
	}

	c.writeFile = os.WriteFile
	c.readFile = os.ReadFile
	c.execute = commandlineexecutor.ExecuteCommand
	exitStatus, err := c.configureInstanceHandler(ctx)
	if err != nil {
		fmt.Println("ConfigureInstance: FAILED, detailed logs are at /var/log/google-cloud-sap-agent/configureinstance.log, err: ", err)
		log.CtxLogger(ctx).Errorw("ConfigureInstance failed", "err", err)
		usagemetrics.Error(usagemetrics.ConfigureInstanceFailure)
	}
	return exitStatus
}

// configureInstanceHandler checks and applies OS settings
// depending on the machine type.
func (c *ConfigureInstance) configureInstanceHandler(ctx context.Context) (subcommands.ExitStatus, error) {
	LogToBoth(ctx, "ConfigureInstance starting")
	usagemetrics.Action(usagemetrics.ConfigureInstanceStarted)
	rebootRequired := false

	log.CtxLogger(ctx).Infof("Using machine type: %s", c.machineType)
	switch {
	// TODO: Add support for X4 customizations
	case strings.HasPrefix(c.machineType, "x4"):
	default:
		return subcommands.ExitUsageError, fmt.Errorf("unsupported machine type: %s", c.machineType)
	}

	usagemetrics.Action(usagemetrics.ConfigureInstanceFinished)
	exitStatus := subcommands.ExitSuccess
	if c.apply || (c.check && !rebootRequired) {
		LogToBoth(ctx, "ConfigureInstance: SUCCESS")
	}
	if c.apply && rebootRequired {
		LogToBoth(ctx, "\nPlease note that a reboot is required for the changes to take effect.")
	}
	if c.check && rebootRequired {
		LogToBoth(ctx, "ConfigureInstance: Your system configuration doesn't match best practice for your instance type. Please run 'configureinstance -apply' to fix.")
		exitStatus = subcommands.ExitFailure
	}
	LogToBoth(ctx, "\nDetailed logs are at /var/log/google-cloud-sap-agent/configureinstance.log")
	return exitStatus, nil
}

// LogToBoth prints to the console and writes an INFO msg to the log file.
func LogToBoth(ctx context.Context, msg string) {
	fmt.Println(msg)
	log.CtxLogger(ctx).Info(msg)
}

// checkAndRegenerateFile verifies the contents of filePath and regenerates the
// entire file if it is out of date. Returns true if the file is regenerated.
func (c *ConfigureInstance) checkAndRegenerateFile(ctx context.Context, filePath string, want []byte) (bool, error) {
	got, err := c.readFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if errors.Is(err, os.ErrNotExist) || !bytes.Equal(got, want) {
		log.CtxLogger(ctx).Infow("File is out of date.", "filePath", filePath, "got", string(got), "want", string(want))
		if c.check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)
			if err := c.writeFile(filePath, want, 0644); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	log.CtxLogger(ctx).Infof("%s is up to date", filePath)
	return false, nil
}

// checkAndRegenerateLines verifies lines of filePath and regenerates the
// lines if they are out of date. Returns true if any line is regenerated.
// wantLines should be formatted as either a single key/value: 'key=value',
// or multiple values for one key: 'key="value1 value2 value3"'.
func (c *ConfigureInstance) checkAndRegenerateLines(ctx context.Context, filePath string, wantLines []string) (bool, error) {
	fileLines, err := c.readFile(filePath)
	if err != nil {
		return false, err
	}
	regenerate := false
	gotLines := strings.Split(string(fileLines), "\n")
	for _, want := range wantLines {
		key := strings.Split(want, "=")[0]
		found := false
		for i, got := range gotLines {
			// The line in the file may be commented out, or have an improper value.
			// Check if the key is anywhere in the line before regenerating.
			if strings.Contains(got, key) {
				found = true
				if got != want {
					var updated bool
					if updated, gotLines[i] = regenerateLine(ctx, got, want); updated == true {
						log.CtxLogger(ctx).Infof("%s is out of date. Got: '%s', want: '%s'", filePath, got, gotLines[i])
						regenerate = true
					}
				}
			}
		}
		// If the key is missing entirely, add it to the end of the file.
		if !found {
			log.CtxLogger(ctx).Infof("%s is out of date. Missing line: '%s'", filePath, want)
			gotLines = append(gotLines, want+"\n")
			regenerate = true
		}
	}

	if regenerate {
		if c.check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)
			if err := c.writeFile(filePath, []byte(strings.Join(gotLines, "\n")), 0644); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	log.CtxLogger(ctx).Infof("%s is up to date", filePath)
	return false, nil
}

// regenerateLine will override values in 'got' provided by 'want,' while
// preserving any original values not present in 'want'.
// 'want' should be formatted as either a single key/value: 'key=value',
// or multiple values for one key: 'key="value1 value2 value3"'.
// Returns true if a substitution occurred.
func regenerateLine(ctx context.Context, got, want string) (bool, string) {
	if got == want {
		return false, got
	}
	// Single value, just replace 'got' with 'want'.
	if !strings.Contains(got, " ") && !strings.Contains(want, " ") {
		return true, want
	}
	// Multiple values will iterate through each, applying changes if necessary.
	split := strings.SplitN(want, "=", 2)
	if len(split) != 2 {
		log.CtxLogger(ctx).Errorf("Invalid format for want: '%s'", want)
		return false, got
	}
	values := strings.Trim(split[1], `"`)
	updated := false
	for _, val := range strings.Split(values, " ") {
		if !strings.Contains(got, val) {
			updated = true
		}
		// Values will overwrite existing occurrences in 'got'.
		key := strings.Split(val, "=")[0]
		re := regexp.MustCompile(key + `[^\s\"]*`)
		got = re.ReplaceAllString(got, val)
		// If not found, append to the end of 'got'.
		if !strings.Contains(got, val) {
			got = strings.TrimSuffix(got, `"`) + fmt.Sprintf(` %s"`, val)
		}
	}
	return updated, got
}
