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

// Package backup is the module containing one time execution for HANA GCBDR backup.
package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
)

const (
	prepareScriptPath = "/act/custom_apps/act_saphana_prepare.sh"
)

type operationHandler func(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus)

// Backup contains the parameters for the gcbdr-backup command.
type Backup struct {
	OperationType   string `json:"operation-type"`
	SID             string `json:"sid"`
	HDBUserstoreKey string `json:"hdbuserstore-key"`
	LogLevel        string `json:"loglevel"`
	help            bool
	version         bool
}

// Name implements the subcommand interface for Backup.
func (b *Backup) Name() string { return "gcbdr-backup" }

// Synopsis implements the subcommand interface for Backup.
func (b *Backup) Synopsis() string { return "invoke GCBDR CoreAPP backup script" }

// Usage implements the subcommand interface for Backup.
func (b *Backup) Usage() string {
	return `Usage: gcbdr-backup -operation-type=<prepare|freeze|unfreeze|logbackup|logpurge>
	[-sid=<HANA-sid>] [-hdbuserstore-key=<userstore-key>]
	[-h] [-v] [-loglevel=<debug|info|warn|error>]` + "\n"
}

// SetFlags implements the subcommand interface for Backup.
func (b *Backup) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&b.OperationType, "operation-type", "", "Operation type. (required)")
	fs.StringVar(&b.SID, "sid", "", "HANA sid. (required for prepare)")
	fs.StringVar(&b.HDBUserstoreKey, "hdbuserstore-key", "", "HANA userstore key specific to HANA instance. (required for prepare)")
	fs.BoolVar(&b.help, "h", false, "Display help")
	fs.BoolVar(&b.version, "v", false, "Display the version of the agent")
	fs.StringVar(&b.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for Backup.
func (b *Backup) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     b.Name(),
		Help:     b.help,
		Version:  b.version,
		LogLevel: b.LogLevel,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	message, exitStatus := b.Run(ctx, commandlineexecutor.ExecuteCommand)
	switch exitStatus {
	case subcommands.ExitUsageError:
		onetime.LogErrorToFileAndConsole(ctx, "GCBDR-backup Usage Error:", errors.New(message))
	case subcommands.ExitFailure:
		onetime.LogErrorToFileAndConsole(ctx, "GCBDR-backup Failure:", errors.New(message))
	case subcommands.ExitSuccess:
		onetime.LogMessageToFileAndConsole(ctx, message)
	}
	return exitStatus
}

// Run performs the functionality specified by the gcbdr-backup subcommand.
func (b *Backup) Run(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	// TODO: b/349947544 - Add support for other operations.
	operationHandlers := map[string]operationHandler{
		"prepare": b.prepareHandler,
	}
	b.OperationType = strings.ToLower(b.OperationType)
	handler, ok := operationHandlers[b.OperationType]
	if !ok {
		errMessage := fmt.Sprintf("invalid operation type: %s", b.OperationType)
		return errMessage, subcommands.ExitUsageError
	}
	return handler(ctx, exec)
}

// prepareHandler executes the GCBDR CoreAPP script for prepare operation.
func (b *Backup) prepareHandler(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	if err := b.validateParamsForPrepare(); err != nil {
		return err.Error(), subcommands.ExitUsageError
	}
	cmd := fmt.Sprintf("%s %s %s", prepareScriptPath, b.SID, b.HDBUserstoreKey)
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for prepare operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	successMessage := fmt.Sprintf("GCBDR CoreAPP script for prepare operation executed successfully")
	return successMessage, subcommands.ExitSuccess
}

func (b *Backup) validateParamsForPrepare() error {
	if b.SID == "" {
		return fmt.Errorf("SID is required for prepare operation")
	}
	if b.HDBUserstoreKey == "" {
		return fmt.Errorf("HDBUserstoreKey is required for prepare operation")
	}
	return nil
}
