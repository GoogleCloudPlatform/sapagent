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
	"strings"

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
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const userAgent = "Backint for GCS"

// Backint has args for backint subcommands.
type Backint struct {
	IIOTEParams *onetime.InternallyInvokedOTE `json:"-"`
	User        string                        `json:"user"`
	Function    string                        `json:"function"`
	InFile      string                        `json:"input"`
	OutFile     string                        `json:"output"`
	ParamFile   string                        `json:"paramfile"`
	BackupID    string                        `json:"backupid"`
	BackupLevel string                        `json:"level"`
	Count       int64                         `json:"count,string"`
	LogLevel    string                        `json:"loglevel"`
	LogPath     string                        `json:"log-path"`
	help        bool
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
	[-level=<backup-level>] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for backint.
func (b *Backint) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&b.User, "user", "", "User consists of database name and SID of the HANA instance")
	fs.StringVar(&b.User, "u", "", "User consists of database name and SID of the HANA instance")
	fs.StringVar(&b.Function, "function", "", "The requested function")
	fs.StringVar(&b.Function, "f", "", "The requested function")
	fs.StringVar(&b.InFile, "input", "", "Input file for corresponding function (-f). If not set, input is read from stdin")
	fs.StringVar(&b.InFile, "i", "", "Input file for corresponding function (-f). If not set, input is read from stdin")
	fs.StringVar(&b.OutFile, "output", "", "File where return values and messages are written. If not set, output is written to stdout")
	fs.StringVar(&b.OutFile, "o", "", "File where return values and messages are written. If not set, output is written to stdout")
	fs.StringVar(&b.ParamFile, "paramfile", "", "Parameter file required for GCS integration")
	fs.StringVar(&b.ParamFile, "p", "", "Parameter file required for GCS integration")
	fs.StringVar(&b.BackupID, "backupid", "", "Database backup id, only usable if the function (-f) is backup")
	fs.StringVar(&b.BackupID, "s", "", "Database backup id, only usable if the function (-f) is backup")
	fs.Int64Var(&b.Count, "count", 0, "Total number of database objects associated to the backup id specified (-s)")
	fs.Int64Var(&b.Count, "c", 0, "Total number of database objects associated to the backup id specified (-s)")
	fs.StringVar(&b.BackupLevel, "level", "", "The type of backup, only usable if the function (-f) is backup")
	fs.StringVar(&b.BackupLevel, "l", "", "The type of backup, only usable if the function (-f) is backup")
	fs.StringVar(&b.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/backint.log")
	fs.BoolVar(&b.help, "h", false, "Display help")
	fs.StringVar(&b.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute implements the subcommand interface for backint.
func (b *Backint) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp, cloudProps, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     b.Name(),
		Help:     b.help,
		LogLevel: b.LogLevel,
		LogPath:  b.LogPath,
		Fs:       f,
		IIOTE:    b.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, exitStatus = b.ExecuteAndGetMessage(ctx, f, lp, cloudProps)
	return exitStatus
}

// ExecuteAndGetMessage executes the backint command and returns the message and exit status.
func (b *Backint) ExecuteAndGetMessage(ctx context.Context, f *flag.FlagSet, lp log.Parameters, cloudProps *ipb.CloudProperties) (string, subcommands.ExitStatus) {
	return b.backintHandler(ctx, f, lp, cloudProps, s.NewClient)
}

func (b *Backint) backintHandler(ctx context.Context, f *flag.FlagSet, lp log.Parameters, cloudProps *ipb.CloudProperties, client storage.Client) (string, subcommands.ExitStatus) {
	log.CtxLogger(ctx).Info("Backint starting")
	p := configuration.Parameters{
		User:        b.User,
		Function:    b.Function,
		InFile:      b.InFile,
		OutFile:     b.OutFile,
		ParamFile:   b.ParamFile,
		BackupID:    b.BackupID,
		BackupLevel: b.BackupLevel,
		Count:       b.Count,
	}
	config, err := p.ParseArgsAndValidateConfig(os.ReadFile, os.ReadFile)
	if err != nil {
		return err.Error(), subcommands.ExitUsageError
	}
	lp.LogToCloud = config.GetLogToCloud().GetValue()
	log.CtxLogger(ctx).Infow("Args parsed and config validated", "config", configuration.ConfigToPrint(config))

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
		return "Failed to connect to bucket", subcommands.ExitFailure
	}

	usagemetrics.Action(usagemetrics.BackintRunning)
	if ok := run(ctx, config, connectParams, f, lp, cloudProps); !ok {
		supportbundle.CollectAgentSupport(ctx, f, lp, cloudProps, b.Name())
		return "Failed to run backint", subcommands.ExitFailure
	}

	message := "Backint finished"
	log.CtxLogger(ctx).Info(message)
	return message, subcommands.ExitSuccess
}

// run opens the input file and creates the output file then selects which Backint function
// to execute based on the configuration. Issues with file operations or config will return false.
func run(ctx context.Context, config *bpb.BackintConfiguration, connectParams *storage.ConnectParameters, f *flag.FlagSet, lp log.Parameters, cloudProps *ipb.CloudProperties) bool {
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

	// If an error occurs during an operation, collect the support bundle.
	defer func() {
		if config.GetFunction() == bpb.Function_DIAGNOSE || config.GetOutputFile() == "/dev/stdout" {
			return
		}
		outFileContents, _ := os.ReadFile(config.GetOutputFile())
		if strings.Contains(string(outFileContents), "#ERROR") {
			log.CtxLogger(ctx).Info("Collecting agent support bundle due to Backint error.")
			supportbundle.CollectAgentSupport(ctx, f, lp, cloudProps, config.GetFunction().String())
		}
	}()

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
