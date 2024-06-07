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
	"golang.org/x/exp/slices"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// writeFileFunc provides a testable replacement for os.WriteFile.
	writeFileFunc func(string, []byte, os.FileMode) error

	// readFileFunc provides a testable replacement for os.ReadFile.
	readFileFunc func(string) ([]byte, error)
)

const (
	hyperThreadingDefault = "default"
	hyperThreadingOn      = "on"
	hyperThreadingOff     = "off"
)

// ConfigureInstance has args for configureinstance subcommands.
type ConfigureInstance struct {
	Check, Apply   bool
	machineType    string
	HyperThreading string
	help, version  bool

	writeFile   writeFileFunc
	readFile    readFileFunc
	ExecuteFunc commandlineexecutor.Execute
	IIOTEParams *onetime.InternallyInvokedOTE
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

  Args (optional):
    [-overrideType="type"]	Override the machine type (by default this is retrieved from metadata)
    [-hyperThreading="default"]	Sets hyper threading settings for X4 machines
                              	Possible values: ["default", "on", "off"]

  Global options:
    [-h] [-v]` + "\n"
}

// SetFlags implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.Check, "check", false, "Check settings and print errors, but do not apply any changes")
	fs.BoolVar(&c.Apply, "apply", false, "Apply changes as necessary to the settings")
	fs.StringVar(&c.machineType, "overrideType", "", "Bypass the metadata machine type lookup")
	fs.StringVar(&c.HyperThreading, "hyperThreading", "default", "Sets hyper threading settings for X4 machines")
	fs.BoolVar(&c.help, "h", false, "Displays help")
	fs.BoolVar(&c.version, "v", false, "Displays the current version of the agent")
}

// Execute implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cloudProps, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:     c.Name(),
		Help:     c.help,
		Version:  c.version,
		Fs:       f,
		LogLevel: "info",
		IIOTE:    c.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	if !c.Check && !c.Apply {
		fmt.Printf("-check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("-check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if c.Check && c.Apply {
		fmt.Printf("Only one of -check or -apply must be specified.\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorf("Only one of -check or -apply must be specified")
		return subcommands.ExitUsageError
	}
	if !slices.Contains([]string{hyperThreadingDefault, hyperThreadingOn, hyperThreadingOff}, c.HyperThreading) {
		fmt.Printf(`hyperThreading must be one of: ["default", "on", "off"]`+"\n%s\n", c.Usage())
		log.CtxLogger(ctx).Errorw(`hyperThreading must be one of: ["default", "on", "off"]`, "hyperThreading", c.HyperThreading)
		return subcommands.ExitUsageError
	}
	if c.machineType == "" {
		c.machineType = cloudProps.GetMachineType()
	}

	c.writeFile = os.WriteFile
	c.readFile = os.ReadFile
	c.ExecuteFunc = commandlineexecutor.ExecuteCommand
	exitStatus, err := c.configureInstanceHandler(ctx)
	if err != nil {
		if exitStatus == subcommands.ExitUsageError {
			fmt.Println(fmt.Sprintf("ConfigureInstance: This machine type (%s) is not currently supported for automatic configuration, detailed logs are at %s", c.machineType, onetime.LogFilePath(c.Name(), c.IIOTEParams)))
			log.CtxLogger(ctx).Infow("ConfigureInstance: This machine type is not currently supported for automatic configuration", "machineType", c.machineType)
		} else {
			fmt.Println(fmt.Sprintf("ConfigureInstance: FAILED, detailed logs are at %s", onetime.LogFilePath(c.Name(), c.IIOTEParams))+" err: ", err)
			log.CtxLogger(ctx).Errorw("ConfigureInstance failed", "err", err)
			// TODO: b/342113969 - Add usage metrics for configureinstance failures.
			// usagemetrics.Error(usagemetrics.ConfigureInstanceFailure)
		}
	}
	return exitStatus
}

// configureInstanceHandler checks and applies OS settings
// depending on the machine type.
func (c *ConfigureInstance) configureInstanceHandler(ctx context.Context) (subcommands.ExitStatus, error) {
	LogToBoth(ctx, "ConfigureInstance starting")
	// TODO: b/342113969 - Add usage metrics for configureinstance failures.
	// usagemetrics.Action(usagemetrics.ConfigureInstanceStarted)
	rebootRequired := false
	var err error

	log.CtxLogger(ctx).Infof("Using machine type: %s", c.machineType)
	switch {
	case strings.HasPrefix(c.machineType, "x4"):
		if rebootRequired, err = c.configureX4(ctx); err != nil {
			return subcommands.ExitFailure, err
		}
	default:
		return subcommands.ExitUsageError, fmt.Errorf("unsupported machine type: %s", c.machineType)
	}

	// TODO: b/342113969 - Add usage metrics for configureinstance failures.
	// usagemetrics.Action(usagemetrics.ConfigureInstanceFinished)
	// if c.Check {
	// 	usagemetrics.Action(usagemetrics.ConfigureInstanceCheckFinished)
	// }
	// if c.Apply {
	// 	usagemetrics.Action(usagemetrics.ConfigureInstanceApplyFinished)
	// }
	exitStatus := subcommands.ExitSuccess
	if c.Apply || (c.Check && !rebootRequired) {
		LogToBoth(ctx, "ConfigureInstance: SUCCESS")
	}
	if c.Apply && rebootRequired {
		LogToBoth(ctx, "\nPlease note that a reboot is required for the changes to take effect.")
	}
	if c.Check && rebootRequired {
		LogToBoth(ctx, "ConfigureInstance: Your system configuration doesn't match best practice for your instance type. Please run 'configureinstance -apply' to fix.")
		exitStatus = subcommands.ExitFailure
	}
	LogToBoth(ctx, fmt.Sprintf("\nDetailed logs are at /var/log/google-cloud-sap-agent/%s", onetime.LogFilePath(c.Name(), c.IIOTEParams)))
	return exitStatus, nil
}

// LogToBoth prints to the console and writes an INFO msg to the log file.
func LogToBoth(ctx context.Context, msg string) {
	fmt.Println(msg)
	log.CtxLogger(ctx).Info(msg)
}

// removeLines verifies lines of filePath and removes any lines containing
// the substrings present in removeLines. Returns true if any line is removed.
// removeLines should be formatted with the longest substring key to be
// removed to avoid removing other lines.
func (c *ConfigureInstance) removeLines(ctx context.Context, filePath string, removeLines []string) (bool, error) {
	fileLines, err := c.readFile(filePath)
	if err != nil {
		return false, err
	}
	regenerate := false
	gotLines := strings.Split(string(fileLines), "\n")
	for _, remove := range removeLines {
		for i, got := range gotLines {
			if strings.Contains(got, remove) {
				log.CtxLogger(ctx).Infof("%s is out of date. Line: '%s' should be removed", filePath, got)
				gotLines[i] = ""
				regenerate = true
			}
		}
	}

	if regenerate {
		if c.Check {
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

// removeValues verifies lines of filePath and removes any values from the
// key if they are present. Returns true if any line is regenerated.
// removeLines should be formatted as a single key/value: 'key=value',
// where the value will be removed from the key.
func (c *ConfigureInstance) removeValues(ctx context.Context, filePath string, removeLines []string) (bool, error) {
	fileLines, err := c.readFile(filePath)
	if err != nil {
		return false, err
	}
	regenerate := false
	gotLines := strings.Split(string(fileLines), "\n")
	for _, remove := range removeLines {
		split := strings.SplitN(remove, "=", 2)
		if len(split) != 2 {
			return false, fmt.Errorf("removeLines should be formatted as 'key=value', got: '%s'", remove)
		}
		key := split[0]
		value := split[1]
		for i, got := range gotLines {
			if strings.Contains(got, key) && strings.Contains(got, value) {
				log.CtxLogger(ctx).Infof("%s is out of date. Value: '%s' should be removed from Key: '%s', Got: %s", filePath, value, key, got)
				// Handle the replace if it's the first value or later in the list.
				gotLines[i] = strings.ReplaceAll(gotLines[i], " "+value, "")
				gotLines[i] = strings.ReplaceAll(gotLines[i], "="+value, "=")
				regenerate = true
			}
		}
	}

	if regenerate {
		if c.Check {
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

// checkAndRegenerateFile verifies the contents of filePath and regenerates the
// entire file if it is out of date. Returns true if the file is regenerated.
func (c *ConfigureInstance) checkAndRegenerateFile(ctx context.Context, filePath string, want []byte) (bool, error) {
	got, err := c.readFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if errors.Is(err, os.ErrNotExist) || !bytes.Equal(got, want) {
		log.CtxLogger(ctx).Infow("File is out of date.", "filePath", filePath, "got", string(got), "want", string(want))
		if c.Check {
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
		if c.Check {
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
