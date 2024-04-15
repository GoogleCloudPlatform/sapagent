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

// Package performancediagnostics performs a series of operations
// and bundles their results in a report which provides
// an insightful assessment of the performance of the system.
package performancediagnostics

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	bundlePath = `/tmp/google-cloud-sap-agent/`
)

// Diagnose has args for performance diagnostics OTE subcommands.
type Diagnose struct {
	logLevel, path           string
	testBucket, resultBucket string
	scope, bundleName        string
	help, version            bool
}

type zipperHelper struct{}

// NewWriter is testable version of zip.NewWriter method.
func (h zipperHelper) NewWriter(w io.Writer) *zip.Writer {
	return zip.NewWriter(w)
}

// FileInfoHeader is testable version of zip.FileInfoHeader method.
func (h zipperHelper) FileInfoHeader(f fs.FileInfo) (*zip.FileHeader, error) {
	return zip.FileInfoHeader(f)
}

// CreateHeader is testable version of CreateHeader method.
func (h zipperHelper) CreateHeader(w *zip.Writer, zfh *zip.FileHeader) (io.Writer, error) {
	return w.CreateHeader(zfh)
}

func (h zipperHelper) Close(w *zip.Writer) error {
	return w.Close()
}

// Name implements the subcommand interface for features.
func (*Diagnose) Name() string {
	return "performancediagnostics"
}

// Synopsis implements the subcommand interface for features.
func (*Diagnose) Synopsis() string {
	return `run a series of diagnostic operations and bundle them in a zip for support team`
}

// Usage implements the subcommand interface for features.
func (*Diagnose) Usage() string {
	return `Usage: performancediagnostics [-scope=<all|backup|io>] [-test-bucket=<name of bucket used to run backup>]
	[-result-bucket=<name of bucket to upload the report] [-bundle_name=<diagnostics bundle name>]
	[-path=<path to save bundle>] [-h] [-v] [-loglevel=<debug|info|warn|error]>]` + "\n"
}

// SetFlags implements the subcommand interface for features.
func (d *Diagnose) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&d.scope, "scope", "all", "Sets the scope for the diagnostic operations. It must be a comma seperated string of values. (optional) Values: <backup|io|all>. Default: all")
	fs.StringVar(&d.testBucket, "test-bucket", "", "Sets the bucket name for running the backint and gsutil operations on. (optional)")
	fs.StringVar(&d.resultBucket, "result-bucket", "", "Sets the bucket name to upload the final zipped report to. (optional)")
	fs.StringVar(&d.bundleName, "bundle-name", "", "Sets the name for generated bundle. (optional) Default: performance-diagnostics-<current_timestamp>")
	fs.StringVar(&d.path, "path", "", "Sets the path to save the bundle. (optional) Default: /tmp/google-cloud-sap-agent/")
	fs.StringVar(&d.logLevel, "loglevel", "", "Sets the logging level for the agent configuration file. (optional) Default: info")
	fs.BoolVar(&d.help, "help", false, "Display help.")
	fs.BoolVar(&d.help, "h", false, "Display help.")
	fs.BoolVar(&d.version, "version", false, "Print the agent version.")
	fs.BoolVar(&d.version, "v", false, "Print the agent version.")
}

// Execute implements the subcommand interface for feature.
func (d *Diagnose) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// this check will never be hit when executing the command line
	if len(args) < 2 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	// This check will never be hit when executing the command line
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}

	if d.help {
		fs.Usage()
		return subcommands.ExitSuccess
	}
	if d.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}

	onetime.SetupOneTimeLogging(lp, d.Name(), log.StringLevelToZapcore("info"))
	return d.diagnosticsHandler(ctx, fs, commandlineexecutor.ExecuteCommand, filesystem.Helper{}, zipperHelper{})
}

// diagnosticsHandler is the main handler for the performance diagnostics OTE subcommand.
func (d *Diagnose) diagnosticsHandler(ctx context.Context, flagSet *flag.FlagSet, exec commandlineexecutor.Execute, fs filesystem.FileSystem, z zipper.Zipper) subcommands.ExitStatus {
	if !d.validateParams(ctx, flagSet) {
		return subcommands.ExitUsageError
	}
	destFilesPath := path.Join(d.path, d.bundleName)
	if err := fs.MkdirAll(destFilesPath, 0777); err != nil {
		onetime.LogErrorToFileAndConsole("Error while making directory: "+destFilesPath, err)
		return subcommands.ExitFailure
	}
	onetime.LogMessageToFileAndConsole("Collecting Performance Diagnostics Report for Agent for SAP...")

	// Performance diagnostics operations.
	ops := d.listOperations(ctx, strings.Split(d.scope, ","))
	for op := range ops {
		if op == "all" {
			// Perform all operations
		} else if op == "backup" {
			// Perform backup operation
		} else if op == "io" {
			// Perform IO Operation
		}
	}

	return subcommands.ExitSuccess
}

// validateParams checks if the parameters provided to the OTE subcommand are valid.
func (d *Diagnose) validateParams(ctx context.Context, flagSet *flag.FlagSet) bool {
	if (strings.Contains(d.scope, "backup") || strings.Contains(d.scope, "all")) && d.testBucket == "" {
		onetime.LogErrorToFileAndConsole("invalid flag usage", errors.New("test bucket cannot be empty to perform backup operation"))
		flagSet.Usage()
		return false
	}
	if d.path == "" {
		log.CtxLogger(ctx).Debugw("No path for bundle provided. Setting bundle path to default", "path", bundlePath)
		d.path = bundlePath
	}
	if d.bundleName == "" {
		timestamp := time.Now().Unix()
		d.bundleName = "performance-diagnostics-" + strconv.FormatInt(timestamp, 10)
		log.CtxLogger(ctx).Debugw("No name for bundle provided. Setting bundle name using timestamp", "bundle_name", timestamp)
	}
	return true
}

// listOperations returns a map of operations to be performed.
func (d *Diagnose) listOperations(ctx context.Context, operations []string) map[string]struct{} {
	ops := make(map[string]struct{})
	for _, op := range operations {
		if _, ok := ops[op]; ok {
			log.CtxLogger(ctx).Debugw("Duplicate operation already listed", "operation", op)
		}
		if op == "all" {
			if len(ops) > 1 {
				ops = map[string]struct{}{"all": {}}
				break
			}
		} else if op == "backup" {
			ops[op] = struct{}{}
		} else if op == "io" {
			ops[op] = struct{}{}
		} else {
			log.CtxLogger(ctx).Debugw("Invalid operation provided", "operation", op)
		}
	}
	return ops
}
