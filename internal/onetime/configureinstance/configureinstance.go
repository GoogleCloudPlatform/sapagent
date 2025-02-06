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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"flag"
	"golang.org/x/exp/slices"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

type (
	// WriteFileFunc provides a testable replacement for os.WriteFile.
	WriteFileFunc func(string, []byte, os.FileMode) error

	// ReadFileFunc provides a testable replacement for os.ReadFile.
	ReadFileFunc func(string) ([]byte, error)

	// MkdirAllFunc provides a testable replacement for os.MkDirAll.
	MkdirAllFunc func(string, os.FileMode) error
)

const (
	hyperThreadingDefault = "default"
	hyperThreadingOn      = "on"
	hyperThreadingOff     = "off"

	dateTimeFormat = "2006-01-02T15:04:05Z"

	operationRegenerateFile   = "REGENERATE_FILE"
	operationRegenerateKeyVal = "REGENERATE_KEY_VALUE"
	operationMissingKeyVal    = "MISSING_KEY_VALUE"
	operationRemoveLine       = "REMOVE_LINE"
	operationRemoveValue      = "REMOVE_VALUE"
	operationLogMessage       = "LOG_MESSAGE"
)

type diff struct {
	Filename  string `json:"filename"`
	Operation string `json:"operation"`
	Got       string `json:"got"`
	Want      string `json:"want"`
}

// ConfigureInstance has args for configureinstance subcommands.
type ConfigureInstance struct {
	Apply          bool   `json:"apply,string"`
	Check          bool   `json:"check,string"`
	MachineType    string `json:"overrideType"`
	HyperThreading string `json:"hyperThreading"`
	PrintDiff      bool   `json:"printDiff,string"`
	Help           bool   `json:"help,string"`
	LogPath        string `json:"log-path"`
	TimeoutSec     int    `json:"timeoutSec"`

	WriteFile   WriteFileFunc
	ReadFile    ReadFileFunc
	MkdirAll    MkdirAllFunc
	ExecuteFunc commandlineexecutor.Execute
	IIOTEParams *onetime.InternallyInvokedOTE
	diffs       []diff
	oteLogger   *onetime.OTELogger
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
    [-hyperThreading="on"]	Sets hyper threading settings for X4 machines
                              	Possible values: ["on", "off"]
    [-printDiff=false]		If true, prints all configuration diffs and log messages to stdout as JSON
    [-log-path="/var/log/google-cloud-sap-agent/configureinstance.log"]			The full linux log path to write the log file (optional).
		                            Default value is /var/log/google-cloud-sap-agent/configureinstance.log

  Global options:
    [-h]` + "\n"
}

// SetFlags implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.Check, "check", false, "Check settings and print errors, but do not apply any changes")
	fs.BoolVar(&c.Apply, "apply", false, "Apply changes as necessary to the settings")
	fs.BoolVar(&c.PrintDiff, "printDiff", false, "Prints all configuration diffs and log messages to stdout as JSON")
	fs.StringVar(&c.MachineType, "overrideType", "", "Bypass the metadata machine type lookup")
	fs.StringVar(&c.HyperThreading, "hyperThreading", "on", "Sets hyper threading settings for X4 machines")
	fs.StringVar(&c.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/configureinstance.log")
	fs.IntVar(&c.TimeoutSec, "timeoutSec", 300, "The timeout in seconds for long-running command line executions")
	fs.BoolVar(&c.Help, "h", false, "Displays help")
}

// Execute implements the subcommand interface for configureinstance.
func (c *ConfigureInstance) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cloudProps, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     c.Name(),
		Help:     c.Help,
		Fs:       f,
		LogLevel: "info",
		LogPath:  c.LogPath,
		IIOTE:    c.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}
	if c.WriteFile == nil {
		c.WriteFile = os.WriteFile
	}
	if c.ReadFile == nil {
		c.ReadFile = os.ReadFile
	}
	if c.MkdirAll == nil {
		c.MkdirAll = os.MkdirAll
	}
	if c.ExecuteFunc == nil {
		c.ExecuteFunc = commandlineexecutor.ExecuteCommand
	}

	status, msg := c.Run(ctx, onetime.CreateRunOptions(cloudProps, false))
	if msg != "" {
		c.LogToBoth(ctx, msg)
	}
	if status == subcommands.ExitFailure {
		c.oteLogger.LogUsageError(usagemetrics.ConfigureInstanceFailure)
	}
	c.LogToBoth(ctx, fmt.Sprintf("\nDetailed logs are at %s", onetime.LogFilePath(c.Name(), c.IIOTEParams)))
	if c.PrintDiff {
		if jsonDiffs, err := json.MarshalIndent(c.diffs, "", "  "); err != nil {
			message := "ConfigureInstance failed to marshal diffs"
			c.LogToBoth(ctx, message)
		} else {
			c.oteLogger.LogMessageToConsole(string(jsonDiffs))
		}
	}
	return status
}

// IsSupportedMachineType checks if the configureinstance subcommand provides support for the machine type.
func (c *ConfigureInstance) IsSupportedMachineType() bool {
	return strings.HasPrefix(c.MachineType, "x4")
}

// Run performs the functionality specified by the configureinstance subcommand.
//
// Return values:
//   - subcommands.ExitStatus: The exit status of the subcommand.
//   - string: A message providing additional details about the exit status.
func (c *ConfigureInstance) Run(ctx context.Context, opts *onetime.RunOptions) (subcommands.ExitStatus, string) {
	c.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	if !c.Check && !c.Apply {
		return subcommands.ExitUsageError, "ConfigureInstance Usage Error: -check or -apply must be specified"
	}
	if c.Check && c.Apply {
		return subcommands.ExitUsageError, "ConfigureInstance Usage Error: only one of -check or -apply must be specified"
	}
	if !slices.Contains([]string{hyperThreadingDefault, hyperThreadingOn, hyperThreadingOff}, c.HyperThreading) {
		return subcommands.ExitUsageError, `ConfigureInstance Usage Error: hyperThreading must be one of ["on", "off"]`
	}
	if c.MachineType == "" {
		c.MachineType = opts.CloudProperties.MachineType
	}
	c.setDefaults()
	return c.configureInstanceHandler(ctx)
}

// setDefaults sets default values. These will not be set by the flag defaults if coming from guestactions.
func (c *ConfigureInstance) setDefaults() {
	if c.HyperThreading == "" {
		c.HyperThreading = hyperThreadingOn
	}
	if c.TimeoutSec == 0 {
		c.TimeoutSec = 300
	}
}

// configureInstanceHandler checks and applies OS settings
// depending on the machine type.
func (c *ConfigureInstance) configureInstanceHandler(ctx context.Context) (subcommands.ExitStatus, string) {
	c.LogToBoth(ctx, "ConfigureInstance starting")
	c.oteLogger.LogUsageAction(usagemetrics.ConfigureInstanceStarted)
	rebootRequired := false
	var err error

	log.CtxLogger(ctx).Infof("Using machine type: %s", c.MachineType)
	switch {
	case c.IsSupportedMachineType():
		if rebootRequired, err = c.configureX4(ctx); err != nil {
			return subcommands.ExitFailure, err.Error()
		}
	default:
		return subcommands.ExitUsageError, fmt.Sprintf("ConfigureInstance Usage Error: this machine type (%s) is not currently supported for automatic configuration", c.MachineType)
	}

	c.oteLogger.LogUsageAction(usagemetrics.ConfigureInstanceFinished)
	if c.Check {
		c.oteLogger.LogUsageAction(usagemetrics.ConfigureInstanceCheckFinished)
	}
	if c.Apply {
		c.oteLogger.LogUsageAction(usagemetrics.ConfigureInstanceApplyFinished)
	}
	exitStatus := subcommands.ExitSuccess
	if c.Apply || (c.Check && !rebootRequired) {
		message := "ConfigureInstance: SUCCESS"
		c.LogToBoth(ctx, message)
	}
	if c.Apply && rebootRequired {
		message := "\nPlease note that a reboot is required for the changes to take effect."
		c.LogToBoth(ctx, message)
	}
	if c.Check && rebootRequired {
		message := "ConfigureInstance: Your system configuration doesn't match best practice for your instance type. Please run 'configureinstance -apply' to fix."
		c.LogToBoth(ctx, message)
		exitStatus = subcommands.ExitFailure
	}
	return exitStatus, ""
}

// LogToBoth prints to the console and writes an INFO msg to the log file.
func (c *ConfigureInstance) LogToBoth(ctx context.Context, msg string) {
	if c.PrintDiff {
		c.diffs = append(c.diffs, diff{Filename: "", Got: msg, Want: "", Operation: operationLogMessage})
	} else {
		c.oteLogger.LogMessageToConsole(msg)
	}
	log.CtxLogger(ctx).Info(msg)
}

// backupAndWriteFile stores a backup of the file with a timestamp and writes
// the new contents to the file, creating any directories that don't exist.
func (c *ConfigureInstance) backupAndWriteFile(ctx context.Context, filePath string, data []byte, perm os.FileMode) error {
	if err := c.MkdirAll(path.Dir(filePath), perm); err != nil {
		c.LogToBoth(ctx, fmt.Sprintf("Failed to create directories %s: %v", path.Dir(filePath), err))
		return err
	}
	backup := fmt.Sprintf("%s-old-%s", filePath, time.Now().Format(dateTimeFormat))
	if res := c.ExecuteFunc(ctx, commandlineexecutor.Params{Executable: "cp", ArgsToSplit: fmt.Sprintf("%s %s", filePath, backup)}); res.ExitCode != 0 {
		log.CtxLogger(ctx).Infof("'cp %s %s' failed, continuing with write, code: %d, stderr: %s", filePath, backup, res.ExitCode, res.StdErr)
	}
	return c.WriteFile(filePath, data, perm)
}

// removeLines verifies lines of filePath and comments out any lines containing
// the substrings present in removeLines. Returns true if any line is removed.
// removeLines should be formatted with the longest substring key to be
// removed to avoid removing other lines. If the line is already commented out,
// it is not removed.
func (c *ConfigureInstance) removeLines(ctx context.Context, filePath string, removeLines []string) (bool, error) {
	fileLines, err := c.ReadFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	regenerate := false
	gotLines := strings.Split(string(fileLines), "\n")
	for _, remove := range removeLines {
		for i, got := range gotLines {
			if strings.Contains(got, remove) && !strings.HasPrefix(got, "#") {
				log.CtxLogger(ctx).Infof("%s is out of date. Line: '%s' should be commented out", filePath, got)
				gotLines[i] = "#" + got
				regenerate = true
				c.diffs = append(c.diffs, diff{Filename: filePath, Got: got, Want: gotLines[i], Operation: operationRemoveLine})
			}
		}
	}

	if regenerate {
		if c.Check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)

			if err := c.backupAndWriteFile(ctx, filePath, []byte(strings.Join(gotLines, "\n")), 0644); err != nil {
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
	fileLines, err := c.ReadFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
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
				c.diffs = append(c.diffs, diff{Filename: filePath, Got: got, Want: gotLines[i], Operation: operationRemoveValue})
			}
		}
	}

	if regenerate {
		if c.Check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)
			if err := c.backupAndWriteFile(ctx, filePath, []byte(strings.Join(gotLines, "\n")), 0644); err != nil {
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
	got, err := c.ReadFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if errors.Is(err, os.ErrNotExist) || !bytes.Equal(got, want) {
		log.CtxLogger(ctx).Infow("File is out of date.", "filePath", filePath, "got", string(got), "want", string(want))
		c.diffs = append(c.diffs, diff{Filename: filePath, Got: string(got), Want: string(want), Operation: operationRegenerateFile})
		if c.Check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)
			if err := c.backupAndWriteFile(ctx, filePath, want, 0644); err != nil {
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
	fileLines, err := c.ReadFile(filePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
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
						c.diffs = append(c.diffs, diff{Filename: filePath, Got: got, Want: gotLines[i], Operation: operationRegenerateKeyVal})
					}
				}
			}
		}
		// If the key is missing entirely, add it to the end of the file.
		if !found {
			log.CtxLogger(ctx).Infof("%s is out of date. Missing line: '%s'", filePath, want)
			gotLines = append(gotLines, want+"\n")
			regenerate = true
			c.diffs = append(c.diffs, diff{Filename: filePath, Got: "", Want: want, Operation: operationMissingKeyVal})
		}
	}

	if regenerate {
		if c.Check {
			log.CtxLogger(ctx).Infof("To regenerate %s, run 'configureinstance -apply'.", filePath)
		} else {
			log.CtxLogger(ctx).Infof("Regenerating %s.", filePath)
			if err := c.backupAndWriteFile(ctx, filePath, []byte(strings.Join(gotLines, "\n")), 0644); err != nil {
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
