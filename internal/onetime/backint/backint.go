/*
Copyright 2023 Google LLC

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

// Package backint interacts with GCS to backup, restore, inquire or delete SAP HANA backups.
package backint

import (
	"context"
	"os"

	"flag"
	s "cloud.google.com/go/storage"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/backup"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/delete"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/diagnose"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/inquire"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/restore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const userAgent = "Backint for GCS"

// Backint has args for backint subcommands.
type Backint struct {
	user, function             string
	inFile, outFile, paramFile string
	backupID, backupLevel      string
	count                      int64
	version, help              bool
	logLevel                   string
}

// Name implements the subcommand interface for backint.
func (*Backint) Name() string { return "backint" }

// Synopsis implements the subcommand interface for backint.
func (*Backint) Synopsis() string { return "backup, restore, inquire, or delete SAP HANA backups" }

// Usage implements the subcommand interface for backint.
func (*Backint) Usage() string {
	return `Usage: backint -function=<backup|restore|inquire|delete|diagnose>
	-paramfile=<path-to-file> [-v] [-h] -user=<DBNAME@SID> [-input=<path-to-file>]
	[-output=<path-to-file>] [-backupid=<database-backup-id>] [-count=<number-of-objects>]
	[-level=<backup-level>] [-loglevel=<debug|info|warn|error>]` + "\n"
}

// SetFlags implements the subcommand interface for backint.
func (b *Backint) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&b.user, "user", "", "User consists of database name and SID of the HANA instance")
	fs.StringVar(&b.user, "u", "", "User consists of database name and SID of the HANA instance")
	fs.StringVar(&b.function, "function", "", "The requested function")
	fs.StringVar(&b.function, "f", "", "The requested function")
	fs.StringVar(&b.inFile, "input", "", "Input file for corresponding function (-f). If not set, input is read from stdin")
	fs.StringVar(&b.inFile, "i", "", "Input file for corresponding function (-f). If not set, input is read from stdin")
	fs.StringVar(&b.outFile, "output", "", "File where return values and messages are written. If not set, output is written to stdout")
	fs.StringVar(&b.outFile, "o", "", "File where return values and messages are written. If not set, output is written to stdout")
	fs.StringVar(&b.paramFile, "paramfile", "", "Parameter file required for GCS integration")
	fs.StringVar(&b.paramFile, "p", "", "Parameter file required for GCS integration")
	fs.StringVar(&b.backupID, "backupid", "", "Database backup id, only usable if the function (-f) is backup")
	fs.StringVar(&b.backupID, "s", "", "Database backup id, only usable if the function (-f) is backup")
	fs.Int64Var(&b.count, "count", 0, "Total number of database objects associated to the backup id specified (-s)")
	fs.Int64Var(&b.count, "c", 0, "Total number of database objects associated to the backup id specified (-s)")
	fs.StringVar(&b.backupLevel, "level", "", "The type of backup, only usable if the function (-f) is backup")
	fs.StringVar(&b.backupLevel, "l", "", "The type of backup, only usable if the function (-f) is backup")
	fs.BoolVar(&b.version, "v", false, "Display the version of the agent")
	fs.BoolVar(&b.help, "h", false, "Display help")
	fs.StringVar(&b.logLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for backint.
func (b *Backint) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp, cloudProps, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:     b.Name(),
		Help:     b.help,
		Version:  b.version,
		LogLevel: b.logLevel,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	return b.backintHandler(ctx, lp, cloudProps, s.NewClient)
}

func (b *Backint) backintHandler(ctx context.Context, lp log.Parameters, cloudProps *ipb.CloudProperties, client storage.Client) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("Backint starting")
	p := configuration.Parameters{
		User:        b.user,
		Function:    b.function,
		InFile:      b.inFile,
		OutFile:     b.outFile,
		ParamFile:   b.paramFile,
		BackupID:    b.backupID,
		BackupLevel: b.backupLevel,
		Count:       b.count,
	}
	config, ok := p.ParseArgsAndValidateConfig(os.ReadFile)
	if !ok {
		return subcommands.ExitUsageError
	}
	lp.LogToCloud = config.GetLogToCloud().GetValue()
	onetime.SetupOneTimeLogging(lp, b.Name(), configuration.LogLevelToZapcore(config.GetLogLevel()))
	log.CtxLogger(ctx).Infow("Args parsed and config validated", "config", config)

	connectParams := &storage.ConnectParameters{
		StorageClient:    client,
		ServiceAccount:   config.GetServiceAccountKey(),
		BucketName:       config.GetBucket(),
		UserAgentSuffix:  userAgent,
		VerifyConnection: true,
		MaxRetries:       config.GetRetries(),
		Endpoint:         config.GetClientEndpoint(),
	}
	if _, ok := storage.ConnectToBucket(ctx, connectParams); !ok {
		return subcommands.ExitUsageError
	}

	usagemetrics.Action(usagemetrics.BackintRunning)
	if ok := run(ctx, config, connectParams, cloudProps); !ok {
		return subcommands.ExitUsageError
	}

	log.CtxLogger(ctx).Info("Backint finished")
	return subcommands.ExitSuccess
}

// run opens the input file and creates the output file then selects which Backint function
// to execute based on the configuration. Issues with file operations or config will return false.
func run(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, cloudProps *ipb.CloudProperties) bool {
	usagemetrics.Action(usagemetrics.BackintRunning)
	log.CtxLogger(ctx).Infow("Executing Backint function", "function", config.GetFunction().String(), "inFile", config.GetInputFile(), "outFile", config.GetOutputFile())
	inFile, err := os.Open(config.GetInputFile())
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error opening input file", "fileName", config.GetInputFile(), "err", err)
		return false
	}
	defer inFile.Close()
	if fileInfo, err := inFile.Stat(); err != nil || fileInfo.Mode()&0222 == 0 {
		log.CtxLogger(ctx).Errorw("Input file does not have readable permissions", "fileName", config.GetInputFile(), "err", err)
		return false
	}
	outFile, err := os.Create(config.GetOutputFile())
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error opening output file", "fileName", config.GetOutputFile(), "err", err)
		return false
	}
	defer outFile.Close()
	if fileInfo, err := outFile.Stat(); err != nil || fileInfo.Mode()&0444 == 0 {
		log.CtxLogger(ctx).Errorw("Output file does not have writable permissions", "fileName", config.GetOutputFile(), "err", err)
		return false
	}

	switch config.GetFunction() {
	case bpb.Function_BACKUP:
		return backup.Execute(ctx, config, connectParams, inFile, outFile, cloudProps)
	case bpb.Function_INQUIRE:
		return inquire.Execute(ctx, config, connectParams, inFile, outFile, cloudProps)
	case bpb.Function_DELETE:
		return delete.Execute(ctx, config, connectParams, inFile, outFile, cloudProps)
	case bpb.Function_RESTORE:
		return restore.Execute(ctx, config, connectParams, inFile, outFile, cloudProps)
	case bpb.Function_DIAGNOSE:
		return diagnose.Execute(ctx, config, connectParams, outFile, cloudProps)
	default:
		log.CtxLogger(ctx).Errorw("Unsupported Backint function", "function", config.GetFunction().String())
		return false
	}
}
