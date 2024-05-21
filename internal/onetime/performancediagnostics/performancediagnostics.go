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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"flag"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/backint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configureinstance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	s "cloud.google.com/go/storage"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const (
	bundlePath = `/tmp/google-cloud-sap-agent/`
)

// Diagnose has args for performance diagnostics OTE subcommands.
type Diagnose struct {
	logLevel, path                      string
	hyperThreading                      string
	paramFile, testBucket, resultBucket string
	scope, bundleName                   string
	help, version                       bool
}

// ReadConfigFile abstracts os.ReadFile function for testability.
type ReadConfigFile func(string) ([]byte, error)

// moveFiles is a struct to store the old and new path of a file.
type moveFiles struct {
	oldPath, newPath string
}

// connectToBucket abstracts storage.ConnectToBucket function for testability.
type connectToBucket func(ctx context.Context, p *storage.ConnectParameters) (attributes, bool)

// getReaderWriter is a function to get the reader writer for uploading the file.
type getReaderWriter func(rw storage.ReadWriter) uploader

// attributes interface provides abstraction for ease of testing.
type attributes interface {
	Attrs(ctx context.Context) (attrs *s.BucketAttrs, err error)
}

// uploader interface provides abstraction for ease of testing.
type uploader interface {
	Upload(ctx context.Context) (int64, error)
}

// options is a struct to store the parameters required for the OTE subcommand.
type options struct {
	fs     filesystem.FileSystem
	exec   commandlineexecutor.Execute
	client storage.Client
	z      zipper.Zipper
	lp     log.Parameters
	cp     *ipb.CloudProperties
	config *bpb.BackintConfiguration
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
	return `Usage: performancediagnostics [-scope=<all|backup|io>] [-test-bucket=<name of bucket used to run backup]
	[-param-file=<path to backint parameters file>]	[-result-bucket=<name of bucket to upload the report] [-bundle_name=<diagnostics bundle name>]
	[-path=<path to save bundle>] [-h] [-v] [-loglevel=<debug|info|warn|error]>]` + "\n"
}

// SetFlags implements the subcommand interface for features.
func (d *Diagnose) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&d.scope, "scope", "all", "Sets the scope for the diagnostic operations. It must be a comma seperated string of values. (optional) Values: <backup|io|all>. Default: all")
	fs.StringVar(&d.testBucket, "test-bucket", "", "Sets the bucket name used to run backup operation. (optional)")
	fs.StringVar(&d.paramFile, "param-file", "", "Sets the path to backint parameters file. Must be present if scope includes backup operation. (optional)")
	fs.StringVar(&d.resultBucket, "result-bucket", "", "Sets the bucket name to upload the final zipped report to. (optional)")
	fs.StringVar(&d.bundleName, "bundle-name", "", "Sets the name for generated bundle. (optional) Default: performance-diagnostics-<current_timestamp>")
	fs.StringVar(&d.path, "path", "", "Sets the path to save the bundle. (optional) Default: /tmp/google-cloud-sap-agent/")
	fs.StringVar(&d.hyperThreading, "hyper-threading", "default", "Sets hyper threading settings for X4 machines")
	fs.StringVar(&d.logLevel, "loglevel", "", "Sets the logging level for the agent configuration file. (optional) Default: info")
	fs.BoolVar(&d.help, "help", false, "Display help.")
	fs.BoolVar(&d.help, "h", false, "Display help.")
	fs.BoolVar(&d.version, "version", false, "Print the agent version.")
	fs.BoolVar(&d.version, "v", false, "Print the agent version.")
}

// Execute implements the subcommand interface for feature.
func (d *Diagnose) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp, cp, exitStatus, completed := onetime.Init(
		ctx,
		onetime.Options{
			Name:     d.Name(),
			Help:     d.help,
			Version:  d.version,
			LogLevel: d.logLevel,
			Fs:       fs,
		},
		args...,
	)
	if !completed {
		return exitStatus
	}

	opts := &options{
		fs:     filesystem.Helper{},
		client: s.NewClient,
		exec:   commandlineexecutor.ExecuteCommand,
		z:      zipperHelper{},
		lp:     lp,
		cp:     cp,
	}
	return d.diagnosticsHandler(ctx, fs, opts)
}

// diagnosticsHandler is the main handler for the performance diagnostics OTE subcommand.
func (d *Diagnose) diagnosticsHandler(ctx context.Context, flagSet *flag.FlagSet, opts *options) subcommands.ExitStatus {
	if !d.validateParams(ctx, flagSet) {
		return subcommands.ExitUsageError
	}

	destFilesPath := path.Join(d.path, d.bundleName)
	if err := opts.fs.MkdirAll(destFilesPath, 0777); err != nil {
		onetime.LogErrorToFileAndConsole("error while making directory: "+destFilesPath, err)
		return subcommands.ExitFailure
	}
	onetime.LogMessageToFileAndConsole("Collecting Performance Diagnostics Report for Agent for SAP...")
	usagemetrics.Action(usagemetrics.PerformanceDiagnostics)
	errs := performDiagnosticsOps(ctx, d, flagSet, opts)

	oteLog := moveFiles{
		oldPath: fmt.Sprintf("/var/log/google-cloud-sap-agent/%s.log", d.Name()),
		newPath: path.Join(d.path, d.bundleName, fmt.Sprintf("%s.log", d.Name())),
	}
	if err := addToBundle(ctx, []moveFiles{oteLog}, opts.fs); err != nil {
		errs = append(errs, fmt.Errorf("failure in adding performance diagnostics OTE to bundle, failed with error %v", err))
	}

	zipFile := fmt.Sprintf("%s/%s.zip", destFilesPath, d.bundleName)
	if err := zipSource(destFilesPath, zipFile, opts.fs, opts.z); err != nil {
		onetime.LogErrorToFileAndConsole(fmt.Sprintf("error while zipping destination folder %s", zipFile), err)
		errs = append(errs, err)
	} else {
		onetime.LogMessageToFileAndConsole(fmt.Sprintf("Zipped destination performance diagnostics bundle at %s", zipFile))
	}

	if d.resultBucket != "" {
		if err := d.uploadZip(ctx, zipFile, storage.ConnectToBucket, getReadWriter, opts); err != nil {
			onetime.LogErrorToFileAndConsole(fmt.Sprintf("error while uploading zip %s", zipFile), err)
			errs = append(errs, fmt.Errorf("failure in uploading zip, failed with error %v", err))
		} else {
			if err := removeDestinationFolder(destFilesPath, opts.fs); err != nil {
				onetime.LogErrorToFileAndConsole(fmt.Sprintf("error while removing folder %s", destFilesPath), err)
				errs = append(errs, fmt.Errorf("failure in removing folder %s, failed with error %v", destFilesPath, err))
			}
		}
	}
	if len(errs) > 0 {
		usagemetrics.Action(usagemetrics.PerformanceDiagnosticsFailure)
		onetime.LogMessageToFileAndConsole("Performance Diagnostics Report collection ran into following errors\n")
		for _, err := range errs {
			onetime.LogErrorToFileAndConsole("Error: ", err)
		}
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

// performDiagnosticsOps performs the operations requested by the user.
func performDiagnosticsOps(ctx context.Context, d *Diagnose, flagSet *flag.FlagSet, opts *options) []error {
	errs := []error{}
	// ConfigureInstance OTE subcommand needs to be run in every case.
	exitStatus := d.runConfigureInstanceOTE(ctx, flagSet, opts)
	if exitStatus != subcommands.ExitSuccess {
		errs = append(errs, fmt.Errorf("failure in executing ConfigureInstance OTE, failed with exist status %d", exitStatus))
	}
	// Performance diagnostics operations.
	ops := listOperations(ctx, strings.Split(d.scope, ","))
	for op := range ops {
		if op == "all" {
			// Perform all operations
			if err := d.backup(ctx, opts); len(err) > 0 {
				usagemetrics.Error(usagemetrics.PerformanceDiagnosticsBackupFailure)
				errs = append(errs, err...)
			}
			if err := d.runFIOCommands(ctx, opts); len(err) > 0 {
				usagemetrics.Error(usagemetrics.PerformanceDiagnosticsFIOFailure)
				errs = append(errs, err...)
			}
		} else if op == "backup" {
			// Perform backup operation
			if err := d.backup(ctx, opts); len(err) > 0 {
				usagemetrics.Error(usagemetrics.PerformanceDiagnosticsBackupFailure)
				errs = append(errs, err...)
			}
		} else if op == "io" {
			// Perform IO Operation
			if err := d.runFIOCommands(ctx, opts); len(err) > 0 {
				usagemetrics.Error(usagemetrics.PerformanceDiagnosticsFIOFailure)
				errs = append(errs, err...)
			}
		}
	}
	return errs
}

// validateParams checks if the parameters provided to the OTE subcommand are valid.
func (d *Diagnose) validateParams(ctx context.Context, flagSet *flag.FlagSet) bool {
	scopeValues := map[string]bool{
		"all":    false,
		"backup": false,
		"io":     false,
	}

	scopes := strings.Split(d.scope, ",")
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if _, ok := scopeValues[scope]; !ok {
			onetime.LogMessageToFileAndConsole("Invalid flag usage, incorrect value of scope. Please check usage.")
			flagSet.Usage()
			return false
		}
		scopeValues[scope] = true
	}

	if scopeValues["backup"] || scopeValues["all"] {
		if d.paramFile == "" && d.testBucket == "" {
			onetime.LogErrorToFileAndConsole("invalid flag usage", errors.New("test bucket cannot be empty to perform backup operation"))
			return false
		}
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

func (d *Diagnose) runConfigureInstanceOTE(ctx context.Context, f *flag.FlagSet, opts *options) subcommands.ExitStatus {
	ciOTEParams := &onetime.InternallyInvokedOTE{
		Lp:        opts.lp,
		Cp:        opts.cp,
		InvokedBy: "performance-diagnostics-configure-instance",
	}
	ci := &configureinstance.ConfigureInstance{
		Check:          true,
		ExecuteFunc:    opts.exec,
		HyperThreading: d.hyperThreading,
		IIOTEParams:    ciOTEParams,
	}
	onetime.LogMessageToFileAndConsole("Executing ConfigureInstance")
	defer onetime.LogMessageToFileAndConsole("Finished invoking ConfigureInstance")
	usagemetrics.Action(usagemetrics.PerformanceDiagnosticsConfigureInstance)
	res := ci.Execute(ctx, f)
	onetime.SetupOneTimeLogging(opts.lp, d.Name(), log.StringLevelToZapcore(d.logLevel))
	paths := []moveFiles{
		{
			oldPath: "/var/log/google-cloud-sap-agent/performance-diagnostics-configure-instance.log",
			newPath: path.Join(d.path, d.bundleName, "performance-diagnostics-configure-instance.log"),
		},
	}
	if res != subcommands.ExitSuccess {
		onetime.LogMessageToFileAndConsole("Error while executing ConfigureInstance OTE")
	}
	if err := addToBundle(ctx, paths, opts.fs); err != nil {
		onetime.LogErrorToFileAndConsole("Error while adding ConfigureInstance OTE logs to bundle", err)
		return subcommands.ExitFailure
	}
	return res
}

// listOperations returns a map of operations to be performed.
func listOperations(ctx context.Context, operations []string) map[string]struct{} {
	ops := make(map[string]struct{})
	for _, op := range operations {
		op = strings.TrimSpace(op)
		if _, ok := ops[op]; ok {
			log.CtxLogger(ctx).Debugw("Duplicate operation already listed", "operation", op)
		}
		if op == "all" {
			return map[string]struct{}{"all": {}}
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

// backup performs the backup operation.
func (d *Diagnose) backup(ctx context.Context, opts *options) []error {
	var errs []error
	usagemetrics.Action(usagemetrics.PerformanceDiagnosticsBackup)
	backupPath := path.Join(d.path, d.bundleName, "backup")
	if err := opts.fs.MkdirAll(backupPath, 0777); err != nil {
		errs = append(errs, err)
		return errs
	}

	config, err := d.setBackintConfig(ctx, opts.fs, opts.fs.ReadFile)
	if err != nil {
		onetime.LogErrorToFileAndConsole("error while unmarshalling backint config, failed with error %v", err)
		errs = append(errs, err)
		return errs
	}
	opts.config = config

	// Check for retention
	if err := d.checkRetention(ctx, s.NewClient, getBucket, opts.config); err != nil {
		errs = append(errs, err)
		return errs
	}
	// run backint.Diagnose function
	if err := d.runBackint(ctx, opts); err != nil {
		errs = append(errs, err)
	}

	// run gsutil perfdiag function
	if err := d.runPerfDiag(ctx, opts); len(err) != 0 {
		errs = append(errs, err...)
	}

	log.CtxLogger(ctx).Info("Backup operation completed")
	return errs
}

// getBucket is an abstraction of storage.ConnectToBucket() for ease of testing.
func getBucket(ctx context.Context, connectParams *storage.ConnectParameters) (attributes, bool) {
	return storage.ConnectToBucket(ctx, connectParams)
}

func getReadWriter(rw storage.ReadWriter) uploader {
	return &rw
}

func (d *Diagnose) checkRetention(ctx context.Context, client storage.Client, ctb connectToBucket, config *bpb.BackintConfiguration) error {
	bucket := config.GetBucket()
	if d.testBucket != "" {
		bucket = d.testBucket
	}
	if bucket == "" {
		onetime.LogErrorToFileAndConsole("error determining bucket", fmt.Errorf("no bucket provided, either in param-file or as test-bucket"))
		return fmt.Errorf("no bucket provided, either in param-file or as test-bucket")
	}

	connectParams := &storage.ConnectParameters{
		StorageClient:    client,
		ServiceAccount:   config.GetServiceAccountKey(),
		BucketName:       bucket,
		UserAgentSuffix:  "Performance Diagnostics",
		VerifyConnection: true,
		MaxRetries:       config.GetRetries(),
		Endpoint:         config.GetClientEndpoint(),
	}

	var bucketHandle attributes
	var ok bool
	if bucketHandle, ok = ctb(ctx, connectParams); !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		onetime.LogErrorToFileAndConsole("failed connecting to bucket", err)
		return err
	}

	attrs, err := bucketHandle.Attrs(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("failed to get bucket attributes, ensure the bucket exists and you have permission to access it", "error", err)
		return err
	}
	if attrs.RetentionPolicy != nil {
		err := errors.New("retention policy is set, backup operation will fail; please provide a bucket without retention policy")
		onetime.LogErrorToFileAndConsole("Error performing backup operation", err)
		return err
	}
	return nil
}

// runBackint runs the diagnose function of Backint OTE.
func (d *Diagnose) runBackint(ctx context.Context, opts *options) error {
	backintParams := &backint.Backint{
		Function:  "diagnose",
		User:      "perf-diag-user",
		ParamFile: d.paramFile,
		OutFile:   path.Join(d.path, d.bundleName, "backup/backint-output.log"),
		IIOTEParams: &onetime.InternallyInvokedOTE{
			InvokedBy: "performance-diagnostics-backint",
			Lp:        opts.lp,
			Cp:        opts.cp,
		},
	}
	onetime.LogMessageToFileAndConsole("Running backint...")
	defer onetime.LogMessageToFileAndConsole("Finished running backint.")
	res := backintParams.Execute(ctx, &flag.FlagSet{})
	onetime.SetupOneTimeLogging(opts.lp, d.Name(), log.StringLevelToZapcore(d.logLevel))
	if res != subcommands.ExitSuccess {
		onetime.LogMessageToFileAndConsole("Error while executing backint")
	}

	paths := []moveFiles{
		{
			oldPath: "/var/log/google-cloud-sap-agent/performance-diagnostics-backint.log",
			newPath: path.Join(d.path, d.bundleName, "backup/performance-diagnostics-backint.log"),
		},
	}
	if err := addToBundle(ctx, paths, opts.fs); err != nil {
		return err
	}

	if res != subcommands.ExitSuccess {
		return fmt.Errorf("error while executing backint command %v", res)
	}

	return nil
}

// runPerfDiag runs the gsutil perfdiag command in order to check the performance of the test bucket
// provided.
func (d *Diagnose) runPerfDiag(ctx context.Context, opts *options) []error {
	var errs []error
	onetime.LogMessageToFileAndConsole("Running gsutil perfdiag...")
	targetPath := path.Join(d.path, d.bundleName)
	targetBucket := d.testBucket
	if targetBucket == "" {
		targetBucket = opts.config.GetBucket()
	}
	cmd := "sudo"
	args := []string{
		fmt.Sprintf("gsutil perfdiag -s 100m -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_100m_12p.json gs://%s", targetPath, targetBucket),
		fmt.Sprintf("gsutil perfdiag -s 1G -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_1g_12p.json gs://%s", targetPath, targetBucket),
		fmt.Sprintf("gsutil perfdiag -s 100m -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_100m_12p_default.json gs://%s", targetPath, targetBucket),
		fmt.Sprintf("gsutil perfdiag -s 1G -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_1g_12p_default.json gs://%s", targetPath, targetBucket),
	}

	for _, args := range args {
		res := opts.exec(ctx, commandlineexecutor.Params{
			Executable:  cmd,
			ArgsToSplit: args,
			Timeout:     200,
		})
		if res.ExitCode != int(subcommands.ExitSuccess) {
			errs = append(errs, fmt.Errorf("error while executing gsutil perfdiag command %v", res.StdErr))
		}
	}
	onetime.LogMessageToFileAndConsole("Finished running gsutil perfdiag.")
	return errs
}

// runFIOCommands runs the FIO commands in order to simulate HANA IO operations.
func (d *Diagnose) runFIOCommands(ctx context.Context, opts *options) []error {
	var errs []error
	onetime.LogMessageToFileAndConsole("Running FIO commands...")
	usagemetrics.Action(usagemetrics.PerformanceDiagnosticsFIO)
	targetPath := path.Join(d.path, d.bundleName, "io")
	if err := opts.fs.MkdirAll(targetPath, 0777); err != nil {
		errs = append(errs, fmt.Errorf("error while making directory: %s, error %s", targetPath, err.Error()))
		return errs
	}
	fileNames := []string{"data_randread_256K_100G", "data_randread_512K_100G", "data_randread_1M_100G", "data_randread_64M_100G"}
	cmd := "sudo"
	args := []string{
		"fio --name=data_randread_256K_100G --filename=/hana/data/testfile --filesize=100G --time_based --ramp_time=2s --runtime=1m --ioengine=libaio --direct=1 --verify=0 --randrepeat=0 --bs=256K --iodepth=256 --rw=randread",
		"fio --name=data_randread_512K_100G --filename=/hana/data/testfile --filesize=100G --time_based --ramp_time=2s --runtime=1m --ioengine=libaio --direct=1 --verify=0 --randrepeat=0 --bs=512K --iodepth=256 --rw=randread",
		"fio --name=data_randread_1M_100G   --filename=/hana/data/testfile --filesize=100G --time_based --ramp_time=2s --runtime=1m --ioengine=libaio --direct=1 --verify=0 --randrepeat=0 --bs=1M   --iodepth=64 --rw=randread",
		"fio --name=data_randread_64M_100G  --filename=/hana/data/testfile --filesize=100G --time_based --ramp_time=2s --runtime=1m --ioengine=libaio --direct=1 --verify=0 --randrepeat=0 --bs=64M  --iodepth=64 --rw=randread",
	}
	for i, arg := range args {
		p := commandlineexecutor.Params{
			Executable:  cmd,
			ArgsToSplit: arg,
			Timeout:     220,
		}
		if err := execAndWriteToFile(ctx, fileNames[i], targetPath, p, opts); err != nil {
			errs = append(errs, err)
		}
	}
	onetime.LogMessageToFileAndConsole("Finished running FIO commands.")
	return errs
}

// execAndWriteToFile executes the command and writes the output to the file.
func execAndWriteToFile(ctx context.Context, opFile, targetPath string, params commandlineexecutor.Params, opts *options) error {
	res := opts.exec(ctx, params)
	if res.ExitCode != 0 && res.StdErr != "" {
		return fmt.Errorf("error while executing %s command %v", res.StdErr, res.ExitCode)
	}
	f, err := opts.fs.OpenFile(path.Join(targetPath, opFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := opts.fs.WriteStringToFile(f, res.StdOut); err != nil {
		return err
	}
	return nil
}

// addToBundle adds the files to performance diagnostics bundle.
func addToBundle(ctx context.Context, paths []moveFiles, fs filesystem.FileSystem) error {
	var hasErrors bool
	for _, p := range paths {
		if p.oldPath == "" || p.newPath == "" {
			log.CtxLogger(ctx).Errorw("no path provided for file", "oldPath", p.oldPath, "newPath", p.newPath)
			hasErrors = true
			continue
		}
		var err error
		var srcFD, dstFD *os.File

		if srcFD, err = fs.Open(p.oldPath); err != nil {
			return err
		}
		defer srcFD.Close()
		if dstFD, err = fs.Create(p.newPath); err != nil {
			return err
		}
		defer dstFD.Close()

		if _, err := fs.Copy(dstFD, srcFD); err != nil {
			onetime.LogErrorToFileAndConsole("error while renaming file: %v", err)
			hasErrors = true
		}
	}

	if hasErrors {
		return fmt.Errorf("error while adding files to bundle")
	}
	return nil
}

func (d *Diagnose) setBackintConfig(ctx context.Context, fs filesystem.FileSystem, read ReadConfigFile) (*bpb.BackintConfiguration, error) {
	if d.testBucket != "" {
		if err := d.createTempParamFile(ctx, fs, read); err != nil {
			return nil, err
		}
	} else {
		if d.paramFile == "" {
			return nil, errors.New("bucket is required for backup, but param file is empty and no test-bucket is provided")
		}
	}

	config, err := d.unmarshalBackintConfig(ctx, read)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("no bucket provided in param file")
	}

	bp := &configuration.Parameters{Config: config}
	bp.ApplyDefaults(int64(runtime.NumCPU()))
	if bp.Config.GetDiagnoseFileMaxSizeGb() == 0 {
		bp.Config.DiagnoseFileMaxSizeGb = 1
	}
	bp.Config.OutputFile = path.Join(d.path, d.bundleName, "backup/backint-output.log")
	return bp.Config, nil
}

func (d *Diagnose) createTempParamFile(ctx context.Context, fs filesystem.FileSystem, read ReadConfigFile) (err error) {
	config, err := d.unmarshalBackintConfig(ctx, read)
	if err != nil {
		return err
	}
	if config == nil {
		config = &bpb.BackintConfiguration{}
	}
	config.Bucket = d.testBucket

	// Creating a temporary parameter file for backint config
	content, err := protojson.Marshal(config)
	if err != nil {
		return err
	}

	if d.paramFile != "" {
		// In case the param file provided is a .txt file, we need a .json file
		// to write the new config.
		paramFile := strings.Split(d.getParamFileName(), ".")
		d.paramFile = path.Join(d.path, d.bundleName, fmt.Sprintf("%s.json", paramFile[len(paramFile)-2]))
	} else {
		d.paramFile = path.Join(d.path, d.bundleName, "backup/backint-config.json")
	}

	f, err := fs.Create(d.paramFile)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return err
	}
	log.CtxLogger(ctx).Debugw("Created temporary parameter file", "paramFile", d.paramFile)
	return nil
}

func (d *Diagnose) unmarshalBackintConfig(ctx context.Context, read ReadConfigFile) (*bpb.BackintConfiguration, error) {
	if d.paramFile == "" {
		return nil, nil
	}

	content, err := read(d.paramFile)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error reading parameters file", "err", err)
		return nil, err
	}
	if len(content) == 0 {
		log.CtxLogger(ctx).Debug("Params file contents are empty")
		return nil, nil
	}

	config, err := configuration.Unmarshal(d.paramFile, content)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error unmarshalling parameters file", "err", err)
		return nil, err
	}
	return config, nil
}

// getParamFileName extracts the name of the parameter file from the path provided.
func (d *Diagnose) getParamFileName() string {
	if d.paramFile == "" {
		return ""
	}
	parts := strings.Split(d.paramFile, "/")
	return parts[len(parts)-1]
}

// zipSource zips the source folder and saves it at the target location.
func zipSource(source, target string, fs filesystem.FileSystem, z zipper.Zipper) error {
	// Creating zip at a temporary location to prevent a blank zip file from
	// being created in the final bundle.
	tempZip := "/tmp/temp-performance-diagnostics.zip"
	f, err := fs.Create(tempZip)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := z.NewWriter(f)
	defer z.Close(writer)

	err = fs.WalkAndZip(source, z, writer)
	if err != nil {
		return err
	}
	return fs.Rename(tempZip, target)
}

// removeDestinationFolder removes the destination folder.
func removeDestinationFolder(path string, fu filesystem.FileSystem) error {
	if err := fu.RemoveAll(path); err != nil {
		onetime.LogErrorToFileAndConsole(fmt.Sprintf("error while removing folder %s", path), err)
		return err
	}
	return nil
}

// uploadZip uploads the zip file to the bucket provided.
func (d *Diagnose) uploadZip(ctx context.Context, destFilesPath string, ctb storage.BucketConnector, grw getReaderWriter, opts *options) error {
	f, err := opts.fs.Open(destFilesPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while opening file", "fileName", destFilesPath, "err", err)
		return err
	}
	defer f.Close()

	fileInfo, err := opts.fs.Stat(destFilesPath)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while statting file", "fileName", destFilesPath, "err", err)
		return err
	}

	connectParams := &storage.ConnectParameters{
		StorageClient:    opts.client,
		ServiceAccount:   opts.config.GetServiceAccountKey(),
		BucketName:       d.resultBucket,
		UserAgentSuffix:  "Performance Diagnostics",
		VerifyConnection: true,
		MaxRetries:       opts.config.GetRetries(),
		Endpoint:         opts.config.GetClientEndpoint(),
	}
	bucketHandle, ok := ctb(ctx, connectParams)
	if !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		log.CtxLogger(ctx).Errorw("failed connecting to bucket", "err", err)
		return err
	}

	objectName := fmt.Sprintf("%s/%s.zip", d.Name(), d.bundleName)
	fileSize := fileInfo.Size()
	readWriter := storage.ReadWriter{
		Reader:       f,
		Copier:       io.Copy,
		BucketHandle: bucketHandle,
		BucketName:   d.resultBucket,
		ObjectName:   objectName,
		TotalBytes:   fileSize,
		VerifyUpload: true,
	}

	rw := grw(readWriter)
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		return err
	}

	log.CtxLogger(ctx).Infow("File uploaded", "bucket", d.resultBucket, "bytesWritten", bytesWritten, "fileSize", fileSize)
	fmt.Println(fmt.Sprintf("Bundle uploaded to bucket %s", d.resultBucket))
	return nil
}
