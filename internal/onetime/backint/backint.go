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
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/config"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
)

// Backint has args for backint subcommands.
type Backint struct {
	user, function             string
	inFile, outFile, paramFile string
	backupID, backupLevel      string
	count                      int64
}

// Name implements the subcommand interface for backint.
func (*Backint) Name() string { return "backint" }

// Synopsis implements the subcommand interface for backint.
func (*Backint) Synopsis() string { return "backup, restore, inquire, or delete SAP HANA backups" }

// Usage implements the subcommand interface for backint.
func (*Backint) Usage() string {
	return `backint -user=<DBNAME@SID> -function=<backup|restore|inquire|delete>
	-paramfile=<path-to-file> [-input=<path-to-file>] [-output=<path-to-file>]
	[-backupid=<database-backup-id>] [-count=<number-of-objects>] [-level=<backup-level>]
`
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
}

// Execute implements the subcommand interface for backint.
func (b *Backint) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	log.SetupOneTimeLogging(lp, b.Name())

	return b.backintHandler(ctx)
}

func (b *Backint) backintHandler(ctx context.Context) subcommands.ExitStatus {
	p := config.Parameters{
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
	log.Logger.Debugw("Args parsed and config validated", "config", config)

	_, ok = storage.ConnectToBucket(ctx, s.NewClient, config)
	if !ok {
		return subcommands.ExitUsageError
	}
	log.Logger.Infow("Connected to bucket", "bucket", config.GetBucket())
	usagemetrics.Action(usagemetrics.BackintRunning)

	return subcommands.ExitSuccess
}
