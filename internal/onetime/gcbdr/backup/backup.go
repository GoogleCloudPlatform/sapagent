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
	"regexp"
	"strings"

	"flag"
	"github.com/google/safetext/shsprintf"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
)

const (
	prepareScriptPath   = "/act/custom_apps/act_saphana_prepare.sh"
	freezeScriptPath    = "/act/custom_apps/act_saphana_pre.sh"
	unfreezeScriptPath  = "/act/custom_apps/act_saphana_post.sh"
	logbackupScriptPath = "/act/custom_apps/act_saphana_logbackup.sh"
	logpurgeScriptPath  = "/act/custom_apps/act_saphana_logdelete.sh"
)

type operationHandler func(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus)

// Backup contains the parameters for the gcbdr-backup command.
type Backup struct {
	OperationType               string `json:"operation-type"`
	SID                         string `json:"sid"`
	HDBUserstoreKey             string `json:"hdbuserstore-key"`
	JobName                     string `json:"job-name"`
	SnapshotStatus              string `json:"snapshot-status"`
	SnapshotType                string `json:"snapshot-type"`
	CatalogBackupRetentionDays  int64  `json:"catalog-backup-retention-days,string"`
	ProductionLogRetentionHours int64  `json:"production-log-retention-hours,string"`
	LogBackupEndPIT             string `json:"log-backup-end-pit"`
	LastBackedUpDBNames         string `json:"last-backed-up-db-names"`
	UseSystemDBKey              bool   `json:"use-systemdb-key,string"`
	LogLevel                    string `json:"loglevel"`
	LogPath                     string `json:"log-path"`
	help                        bool
	hanaVersion                 string
}

// Name implements the subcommand interface for Backup.
func (b *Backup) Name() string { return "gcbdr-backup" }

// Synopsis implements the subcommand interface for Backup.
func (b *Backup) Synopsis() string { return "invoke GCBDR CoreAPP backup script" }

// Usage implements the subcommand interface for Backup.
func (b *Backup) Usage() string {
	return `Usage: gcbdr-backup -operation-type=<prepare|freeze|unfreeze|logbackup|logpurge>
	-sid=<HANA-sid> -hdbuserstore-key=<userstore-key>
	[-job-name=<job-name>] [-snapshot-type=<snapshot-type>] [-snapshot-name=<snapshot-name>]
	[-catalog-backup-retention-days=<retention-days>] [-production-log-retention-hours=<retention-hours>]
	[-log-backup-end-pit=<datetime with format YYYY-MM-DD HH:MM:SS>]
	[-use-systemdb-key=<true|false>] [-last-backed-up-db-names=<db-names>]
	[-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for Backup.
func (b *Backup) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&b.OperationType, "operation-type", "", "Operation type. (required)")
	fs.StringVar(&b.SID, "sid", "", "HANA sid. (required)")
	fs.StringVar(&b.HDBUserstoreKey, "hdbuserstore-key", "", "HANA userstore key specific to HANA instance. (required for prepare)")
	fs.StringVar(&b.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/gcbdr-backup.log")
	fs.StringVar(&b.JobName, "job-name", "", "Job name. (required for unfreeze operation)")
	fs.StringVar(&b.SnapshotStatus, "snapshot-status", "", "Snapshot status. (Optional - defaults to 'SUCCESSFUL')")
	fs.StringVar(&b.SnapshotType, "snapshot-type", "", "Snapshot type. (Optional - defaults to 'LVM')")
	fs.StringVar(&b.LogBackupEndPIT, "log-backup-end-pit", "", "Log backup end PIT. (required for logpurge operation)")
	fs.StringVar(&b.LastBackedUpDBNames, "last-backed-up-db-names", "", "Last backed up DB names. (Optional)")
	fs.Int64Var(&b.CatalogBackupRetentionDays, "catalog-backup-retention-days", 7, "Catalog backup retention days. (Optional - defaults to 7)")
	fs.Int64Var(&b.ProductionLogRetentionHours, "production-log-retention-hours", 0, "Production log retention hours. (Optional - defaults to 1)")
	fs.BoolVar(&b.UseSystemDBKey, "use-systemdb-key", false, "Use system DB key. (Optional - defaults to false)")
	fs.BoolVar(&b.help, "h", false, "Display help")
	fs.StringVar(&b.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for Backup.
func (b *Backup) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     b.Name(),
		Help:     b.help,
		LogLevel: b.LogLevel,
		LogPath:  b.LogPath,
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
	if err := b.validateParams(); err != nil {
		return err.Error(), subcommands.ExitUsageError
	}
	operationHandlers := map[string]operationHandler{
		"prepare":   b.prepareHandler,
		"freeze":    b.freezeHandler,
		"unfreeze":  b.unfreezeHandler,
		"logbackup": b.logbackupHandler,
		"logpurge":  b.logpurgeHandler,
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
	cmd := fmt.Sprintf("%s %s %s", prepareScriptPath, b.SID, b.HDBUserstoreKey)
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 || res.Error != nil {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for prepare operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	return "GCBDR CoreAPP script for prepare operation executed successfully", subcommands.ExitSuccess
}

// freezeHandler executes the GCBDR CoreAPP script for freeze operation.
func (b *Backup) freezeHandler(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	if err := b.extractHANAVersion(ctx, exec); err != nil {
		return fmt.Sprintf("failed to extract HANA version: %v", err), subcommands.ExitFailure
	}
	scriptCMD := fmt.Sprintf("%s %s %s %s", freezeScriptPath, b.SID, b.HDBUserstoreKey, b.hanaVersion)
	cmd := fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && %s'", strings.ToUpper(b.SID), scriptCMD)
	sidAdm := fmt.Sprintf("%sadm", strings.ToLower(b.SID))
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		User:        sidAdm,
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 || res.Error != nil {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for freeze operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	return "GCBDR CoreAPP script for freeze operation executed successfully", subcommands.ExitSuccess
}

// unfreezeHandler executes the GCBDR CoreAPP script for unfreeze operation.
func (b *Backup) unfreezeHandler(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	if b.JobName == "" {
		return "job-name is required for unfreeze operation", subcommands.ExitUsageError
	}
	if err := b.extractHANAVersion(ctx, exec); err != nil {
		return fmt.Sprintf("failed to extract HANA version: %v", err), subcommands.ExitFailure
	}
	scriptCMD := fmt.Sprintf("%s %s %s %s %s %s %s", unfreezeScriptPath, b.SID, b.HDBUserstoreKey, b.hanaVersion, b.JobName, b.SnapshotStatus, b.SnapshotType)
	cmd := fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && %s'", strings.ToUpper(b.SID), scriptCMD)
	sidAdm := fmt.Sprintf("%sadm", strings.ToLower(b.SID))
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		User:        sidAdm,
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 || res.Error != nil {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for unfreeze operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	return "GCBDR CoreAPP script for unfreeze operation executed successfully", subcommands.ExitSuccess
}

// logbackupHandler executes the GCBDR CoreAPP script for logbackup operation.
func (b *Backup) logbackupHandler(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	scriptCMD := fmt.Sprintf("%s %s %s", logbackupScriptPath, b.SID, b.HDBUserstoreKey)
	cmd := fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && %s'", strings.ToUpper(b.SID), scriptCMD)
	sidAdm := fmt.Sprintf("%sadm", strings.ToLower(b.SID))
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		User:        sidAdm,
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 || res.Error != nil {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for logbackup operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	return "GCBDR CoreAPP script for logbackup operation executed successfully", subcommands.ExitSuccess
}

// logpurgeHandler executes the GCBDR CoreAPP script for logpurge operation.
func (b *Backup) logpurgeHandler(ctx context.Context, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	if b.LogBackupEndPIT == "" {
		return "log-backup-end-pit is required for logpurge operation", subcommands.ExitUsageError
	}
	if err := b.extractHANAVersion(ctx, exec); err != nil {
		return fmt.Sprintf("failed to extract HANA version: %v", err), subcommands.ExitFailure
	}
	scriptCMD := fmt.Sprintf("%s %s %s %s %d %s %t %s %d %s %s", logpurgeScriptPath, b.SID, b.HDBUserstoreKey, b.HDBUserstoreKey, b.CatalogBackupRetentionDays, b.hanaVersion, b.UseSystemDBKey, b.LogBackupEndPIT, b.ProductionLogRetentionHours, b.LastBackedUpDBNames, b.SnapshotType)
	cmd := fmt.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && %s'", strings.ToUpper(b.SID), scriptCMD)
	sidAdm := fmt.Sprintf("%sadm", strings.ToLower(b.SID))
	args := commandlineexecutor.Params{
		Executable:  "/bin/bash",
		User:        sidAdm,
		ArgsToSplit: cmd,
	}
	res := exec(ctx, args)
	if res.ExitCode != 0 || res.Error != nil {
		errMessage := fmt.Sprintf("failed to execute GCBDR CoreAPP script for logpurge operation: %v", res.StdErr)
		return errMessage, subcommands.ExitFailure
	}
	return "GCBDR CoreAPP script for logpurge operation executed successfully", subcommands.ExitSuccess
}

func (b *Backup) validateParams() error {
	if b.OperationType == "" {
		return fmt.Errorf("operation-type is required")
	}
	if b.SID == "" {
		return fmt.Errorf("SID is required for gcbdr-backup")
	}
	if b.HDBUserstoreKey == "" {
		return fmt.Errorf("HDBUserstoreKey is required for gcbdr-backup")
	}
	return nil
}

func (b *Backup) extractHANAVersion(ctx context.Context, exec commandlineexecutor.Execute) error {
	sidUpper := strings.ToUpper(b.SID)
	cmd, err := shsprintf.Sprintf("-c 'source /usr/sap/%s/home/.sapenv.sh && /usr/sap/%s/*/HDB version'", sidUpper, sidUpper)
	if err != nil {
		return err
	}
	sidAdm := fmt.Sprintf("%sadm", strings.ToLower(b.SID))
	p := commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: cmd,
		User:        sidAdm,
	}
	res := exec(ctx, p)
	if res.Error != nil {
		return res.Error
	}
	hanaVersionRegex := regexp.MustCompile(`version:\s+(([0-9]+\.?)+)`)
	match := hanaVersionRegex.FindStringSubmatch(res.StdOut)
	if len(match) < 2 {
		return errors.New("unable to identify HANA version")
	}
	b.hanaVersion = match[1]
	return nil
}
