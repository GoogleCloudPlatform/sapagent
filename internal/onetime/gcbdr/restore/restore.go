/*
Copyright 2025 Google LLC

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

// Package restore is the module containing one time execution for HANA GCBDR restore.
package restore

import (
	"context"
	"errors"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
)

const (
	scriptPath = "/etc/google-cloud-sap-agent/gcbdr/CustomApp_SAPHANA.sh"
)

type operationHandler func(ctx context.Context, exec commandlineexecutor.Execute) *gpb.CommandResult

// Restore contains the parameters for the gcbdr-restore command.
type Restore struct {
	OperationType    string `json:"operation-type"`
	SID              string `json:"database_sid"`
	HANAVersion      string `json:"hana_version"`
	HDBUserstoreKey  string `json:"hdbuserstore_key"`
	SourceDataVolume string `json:"source_data_volume"`
	SourceLogVolume  string `json:"source_log_volume"`
	NewTarget        bool   `json:"new_target"`
	DataVGName       string `json:"data_vg_name"`
	LogVGName        string `json:"log_vg_name"`
	DataMnt          string `json:"data_mnt"`
	LogBackupMnt     string `json:"log_backup_mnt"`
	LogLevel         string `json:"loglevel"`
	LogPath          string `json:"log-path"`
	help             bool
	oteLogger        *onetime.OTELogger
}

// Name implements the subcommand interface for Restore.
func (r *Restore) Name() string { return "gcbdr-restore" }

// Synopsis implements the subcommand interface for Restore.
func (r *Restore) Synopsis() string { return "invoke GCBDR CoreAPP restore script" }

// Usage implements the subcommand interface for Restore.
func (r *Restore) Usage() string {
	return `gcbdr-restore -operation-type=<restore|restore-log>
	-database_sid=<HANA-sid> -hana_version=<HANA-version> -hdbuserstore_key=<userstore-key>
	[-source_data_volume=<source-data-volume>] [-source_log_volume=<source-log-volume>]
	[-new_target=<true|false>] [-data_vg_name=<data-vg-name>] [-log_vg_name=<log-vg-name>]
	[-data_mnt=<data-mnt>] [-log_backup_mnt=<log-backup-mnt>] [-loglevel=<debug|info|warn|error>]
	[-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for Restore.
func (r *Restore) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.OperationType, "operation-type", "", "Operation type. (required)")
	fs.StringVar(&r.SID, "database_sid", "", "HANA sid. (required)")
	fs.StringVar(&r.HANAVersion, "hana_version", "", "HANA version. (required)")
	fs.StringVar(&r.HDBUserstoreKey, "hdbuserstore_key", "", "HDBUserstoreKey. (required)")
	fs.StringVar(&r.SourceDataVolume, "source_data_volume", "", "Source data volume. (required)")
	fs.StringVar(&r.SourceLogVolume, "source_log_volume", "", "Source log volume. (required)")
	fs.BoolVar(&r.NewTarget, "new_target", false, "New target. (required)")
	fs.StringVar(&r.DataVGName, "data_vg_name", "", "Data VG name. (required)")
	fs.StringVar(&r.LogVGName, "log_vg_name", "", "Log VG name. (required)")
	fs.StringVar(&r.DataMnt, "data_mnt", "", "Data Mnt. (required)")
	fs.StringVar(&r.LogBackupMnt, "log_backup_mnt", "", "Log Backup Mnt. (required)")
	fs.StringVar(&r.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&r.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/gcbdr-restore.log")
	fs.BoolVar(&r.help, "h", false, "Display help")
}

// Execute implements the subcommand interface for Restore.
func (r *Restore) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     r.Name(),
		LogLevel: r.LogLevel,
		LogPath:  r.LogPath,
		Help:     r.help,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}
	result := r.Run(ctx, commandlineexecutor.ExecuteCommand, onetime.CreateRunOptions(nil, false))
	exitStatus = subcommands.ExitSuccess
	if result.GetExitCode() != 0 {
		exitStatus = subcommands.ExitFailure
	}
	switch exitStatus {
	case subcommands.ExitUsageError:
		r.oteLogger.LogErrorToFileAndConsole(ctx, "GCBDR-restore Usage Error:", errors.New(result.GetStderr()))
	case subcommands.ExitFailure:
		r.oteLogger.LogErrorToFileAndConsole(ctx, "GCBDR-restore Failure:", errors.New(result.GetStderr()))
	case subcommands.ExitSuccess:
		r.oteLogger.LogMessageToFileAndConsole(ctx, result.GetStdout())
	}
	return exitStatus
}

// Run performs the functionality specified by the gcbdr-restore subcommand.
func (r *Restore) Run(ctx context.Context, exec commandlineexecutor.Execute, runOpts *onetime.RunOptions) *gpb.CommandResult {
	r.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	operationHandlers := map[string]operationHandler{
		"restore-preflight": r.preflightHandler,
	}
	handler, ok := operationHandlers[r.OperationType]
	if !ok {
		errMessage := fmt.Sprintf("invalid opration type: %s", r.OperationType)
		return &gpb.CommandResult{
			ExitCode: -1,
			Stdout:   errMessage,
			Stderr:   errMessage,
		}
	}
	result := handler(ctx, exec)
	return result
}

func (r *Restore) preflightHandler(ctx context.Context, exec commandlineexecutor.Execute) *gpb.CommandResult {
	// TODO: (b/290725500) - Implement preflight handler.
	return &gpb.CommandResult{
		ExitCode: 0,
		Stdout:   "GCBDR CoreAPP script for preflight operation executed successfully",
	}
}
