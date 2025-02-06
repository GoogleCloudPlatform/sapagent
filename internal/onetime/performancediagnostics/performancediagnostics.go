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
	"github.com/shirou/gopsutil/v3/process"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	cfg "github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/backint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configureinstance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/systemdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/computeresources"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"

	s "cloud.google.com/go/storage"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

const (
	bundlePath      = `/tmp/google-cloud-sap-agent/`
	timeStampFormat = time.TimeOnly
	// TODO: Fine tune frequency and data point default values.
	minFrequency           = 5
	minTotalDataPoints     = 1
	defaultTotalDataPoints = 6
)

// Diagnose has args for performance diagnostics OTE subcommands.
type Diagnose struct {
	LogLevel          string `json:"loglevel"`
	OutputFilePath    string `json:"output-file-path"`
	HyperThreading    string `json:"hyper-threading"`
	PrintDiff         bool   `json:"print-diff,string"`
	BackintConfigFile string `json:"backint-config-file"`
	TestBucket        string `json:"test-bucket"`
	OutputBucket      string `json:"output-bucket"`
	Type              string `json:"type"`
	OutputFileName    string `json:"output-file-name"`
	LogPath           string `json:"log-path"`
	Frequency         int    `json:"frequency,string"`    // Collection frequency in seconds for compute data.
	TotalDataPoints   int    `json:"total-points,string"` // Total data points to collect in time-series for compute data.
	help              bool
	oteLogger         *onetime.OTELogger
	runOpts           *onetime.RunOptions
	usageFunc         func()
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
	fs                      filesystem.FileSystem
	exec                    commandlineexecutor.Execute
	client                  storage.Client
	z                       zipper.Zipper
	lp                      log.Parameters
	cp                      *ipb.CloudProperties
	config                  *bpb.BackintConfiguration
	cloudDiscoveryInterface system.CloudDiscoveryInterface
	appsDiscovery           func(context.Context, system.SapSystemDiscoveryInterface) *sappb.SAPInstances
	collectProcesses        func(context.Context, computeresources.Parameters) []*computeresources.ProcessInfo
	newProc                 computeresources.NewProcessWithContextHelper
}

// processStat is a struct to store the CPU, Memory and
// Disk IOPS time series metrics for a process.
type processStat struct {
	processInfo *computeresources.ProcessInfo
	cpuUsage    []*computeresources.Metric // CPU metric
	vms         []*computeresources.Metric // Memory metric
	rss         []*computeresources.Metric // Memory metric
	swap        []*computeresources.Metric // Memory metric
	deltaReads  []*computeresources.Metric // Disk IOPS metric
	deltaWrites []*computeresources.Metric // Disk IOPS metric
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
	return `Usage: performancediagnostics [-type=<all|backup|io|compute>] [-test-bucket=<name of bucket used to run backup]
	[-backint-config-file=<path to backint config file>]	[-output-bucket=<name of bucket to upload the report] [-output-file-name=<diagnostics output name>]
	[-output-file-path=<path to save output file generated>] [-hyper-threading=<default|off|on]
	[-print-diff=<true|false>] [-frequency=<frequency in seconds for compute>] [-total-points=<total data points for compute>]
	[-h] [-loglevel=<debug|info|warn|error]>] [-log-path=<log-path>]` + "\n"
}

// SetFlags implements the subcommand interface for features.
func (d *Diagnose) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&d.Type, "type", "all", "Sets the type for the diagnostic operations. It must be a comma separated string of values. (optional) Values: <backup|io|all|compute>. Default: all")
	fs.StringVar(&d.TestBucket, "test-bucket", "", "Sets the bucket name used to run backup operation. (optional)")
	fs.StringVar(&d.BackintConfigFile, "backint-config-file", "", "Sets the path to backint parameters file. Must be present if type includes backup operation. (optional)")
	fs.StringVar(&d.OutputBucket, "output-bucket", "", "Sets the bucket name to upload the final zipped report to. (optional)")
	fs.StringVar(&d.OutputFileName, "output-file-name", "", "Sets the name for generated output file. (optional) Default: performance-diagnostics-<current_timestamp>")
	fs.StringVar(&d.OutputFilePath, "output-file-path", "", fmt.Sprintf("Sets the path to save the output file. (optional) Default: %v", bundlePath))
	fs.StringVar(&d.HyperThreading, "hyper-threading", "default", "Sets hyper threading settings for X4 machines")
	fs.IntVar(&d.Frequency, "frequency", minFrequency, fmt.Sprintf("Sets the frequency in seconds for HANA compute metrics collection. (optional) (min: %v) Default: %v", minFrequency, minFrequency))
	fs.IntVar(&d.TotalDataPoints, "total-points", defaultTotalDataPoints, fmt.Sprintf("Sets the total data points for HANA compute metrics collection. (optional) (min: %v) Default: %v", minTotalDataPoints, defaultTotalDataPoints))
	fs.BoolVar(&d.PrintDiff, "print-diff", false, "Prints all configuration diffs and log messages for configureinstance to stdout as JSON")
	fs.StringVar(&d.LogLevel, "loglevel", "", "Sets the logging level for the agent configuration file. (optional) Default: info")
	fs.StringVar(&d.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/performancediagnostics.log")
	fs.BoolVar(&d.help, "help", false, "Display help.")
	fs.BoolVar(&d.help, "h", false, "Display help.")
}

// Execute implements the subcommand interface for feature.
func (d *Diagnose) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp, cp, exitStatus, completed := onetime.Init(
		ctx,
		onetime.InitOptions{
			Name:     d.Name(),
			Help:     d.help,
			LogLevel: d.LogLevel,
			LogPath:  d.LogPath,
			Fs:       fs,
		},
		args...,
	)
	if !completed {
		return exitStatus
	}
	d.usageFunc = fs.Usage
	_, exitStatus = d.Run(ctx, lp, onetime.CreateRunOptions(cp, false), commandlineexecutor.ExecuteCommand)
	return exitStatus
}

// Run executes the OTE and returns message and exit status.
func (d *Diagnose) Run(ctx context.Context, lp log.Parameters, runOpts *onetime.RunOptions, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	d.oteLogger = onetime.CreateOTELogger(runOpts.DaemonMode)
	d.runOpts = runOpts
	opts := &options{
		fs:               filesystem.Helper{},
		client:           s.NewClient,
		exec:             exec,
		z:                zipperHelper{},
		lp:               lp,
		cp:               runOpts.CloudProperties,
		collectProcesses: computeresources.CollectProcessesForInstance,
	}
	return d.diagnosticsHandler(ctx, opts)
}

// diagnosticsHandler is the main handler for the performance diagnostics OTE subcommand.
func (d *Diagnose) diagnosticsHandler(ctx context.Context, opts *options) (string, subcommands.ExitStatus) {
	if err := d.validateParams(ctx); err != nil {
		return err.Error(), subcommands.ExitUsageError
	}

	destFilesPath := path.Join(d.OutputFilePath, d.OutputFileName)
	if err := opts.fs.MkdirAll(destFilesPath, 0770); err != nil {
		errMessage := fmt.Sprintf("error while creating a new directory: %s", destFilesPath)
		d.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting Performance Diagnostics Report for Agent for SAP...")
	d.oteLogger.LogUsageAction(usagemetrics.PerformanceDiagnostics)
	errs := d.performDiagnosticsOps(ctx, opts)

	oteLog := moveFiles{
		oldPath: fmt.Sprintf("/var/log/google-cloud-sap-agent/%s.log", d.Name()),
		newPath: path.Join(d.OutputFilePath, d.OutputFileName, fmt.Sprintf("%s.log", d.Name())),
	}
	if d.LogPath != "" {
		oteLog.oldPath = d.LogPath
	}
	if err := d.addToBundle(ctx, []moveFiles{oteLog}, opts.fs); err != nil {
		errs = append(errs, fmt.Errorf("failure in adding performance diagnostics OTE to bundle, failed with error %v", err))
	}

	zipFile := fmt.Sprintf("%s/%s.zip", destFilesPath, d.OutputFileName)
	if err := zipSource(destFilesPath, zipFile, opts.fs, opts.z); err != nil {
		d.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error while zipping destination folder %s", zipFile), err)
		errs = append(errs, err)
	} else {
		d.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Zipped destination performance diagnostics bundle at %s", zipFile))
	}

	if d.OutputBucket != "" {
		if err := d.uploadZip(ctx, zipFile, storage.ConnectToBucket, getReadWriter, opts); err != nil {
			d.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error while uploading zip %s", zipFile), err)
			errs = append(errs, fmt.Errorf("failure in uploading zip, failed with error %v", err))
		} else {
			if err := d.removeDestinationFolder(ctx, destFilesPath, opts.fs); err != nil {
				d.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error while removing folder %s", destFilesPath), err)
				errs = append(errs, fmt.Errorf("failure in removing folder %s, failed with error %v", destFilesPath, err))
			}
		}
	}
	if len(errs) > 0 {
		d.oteLogger.LogUsageAction(usagemetrics.PerformanceDiagnosticsFailure)
		d.oteLogger.LogMessageToFileAndConsole(ctx, "Performance Diagnostics Report collection ran into following errors\n")
		var errMessages []string
		for _, err := range errs {
			errMessages = append(errMessages, err.Error())
			d.oteLogger.LogErrorToFileAndConsole(ctx, "Error: ", err)
		}
		message := strings.Join(errMessages, ", ")
		return message, subcommands.ExitFailure
	}
	return "Performance Diagnostics Report collection completed successfully", subcommands.ExitSuccess
}

// performDiagnosticsOps performs the operations requested by the user.
func (d *Diagnose) performDiagnosticsOps(ctx context.Context, opts *options) []error {
	errs := []error{}
	// ConfigureInstance OTE subcommand needs to be run in every case.
	exitStatus := d.runConfigureInstanceOTE(ctx, opts)
	if exitStatus != subcommands.ExitSuccess {
		errs = append(errs, fmt.Errorf("failure in executing ConfigureInstance OTE, failed with exit status %d", exitStatus))
	}
	// Performance diagnostics operations.
	ops := listOperations(ctx, strings.Split(d.Type, ","))
	for op := range ops {
		switch op {
		case "all":
			// Perform all operations.
			if err := d.backup(ctx, opts); len(err) > 0 {
				d.oteLogger.LogUsageError(usagemetrics.PerformanceDiagnosticsBackupFailure)
				errs = append(errs, err...)
			}
			if err := d.runFIOCommands(ctx, opts); len(err) > 0 {
				d.oteLogger.LogUsageError(usagemetrics.PerformanceDiagnosticsFIOFailure)
				errs = append(errs, err...)
			}
			if err := d.computeData(ctx, opts); err != nil {
				errs = append(errs, err)
			}
		case "backup":
			// Perform backup operation.
			if err := d.backup(ctx, opts); len(err) > 0 {
				d.oteLogger.LogUsageError(usagemetrics.PerformanceDiagnosticsBackupFailure)
				errs = append(errs, err...)
			}
		case "io":
			// Perform IO Operation.
			if err := d.runFIOCommands(ctx, opts); len(err) > 0 {
				d.oteLogger.LogUsageError(usagemetrics.PerformanceDiagnosticsFIOFailure)
				errs = append(errs, err...)
			}
		case "compute":
			// Perform System Discovery and collect compute metrics.
			if err := d.computeData(ctx, opts); err != nil {
				errs = append(errs, err)
			}
		default:
			log.CtxLogger(ctx).Debugw("Invalid operation provided", "operation", op)
		}
	}
	return errs
}

// validateParams checks if the parameters provided to the OTE subcommand are valid.
func (d *Diagnose) validateParams(ctx context.Context) error {
	typeValues := map[string]bool{
		"all":     false,
		"backup":  false,
		"io":      false,
		"compute": false,
	}

	types := strings.Split(d.Type, ",")
	for _, tp := range types {
		tp = strings.TrimSpace(tp)
		if _, ok := typeValues[tp]; !ok {
			err := fmt.Errorf("invalid flag usage, incorrect value of type. Please check usage")
			d.oteLogger.LogErrorToFileAndConsole(ctx, err.Error(), err)
			if d.usageFunc != nil {
				d.usageFunc()
			}
			return err
		}
		typeValues[tp] = true
	}

	if typeValues["backup"] || typeValues["all"] {
		if d.BackintConfigFile == "" && d.TestBucket == "" {
			err := fmt.Errorf("test bucket cannot be empty to perform backup operation")
			d.oteLogger.LogErrorToFileAndConsole(ctx, "invalid flag usage", err)
			return err
		}
	}
	if typeValues["compute"] || typeValues["all"] {
		// Only logging the error as the parameters are optional.
		if d.Frequency < minFrequency {
			err := fmt.Errorf("frequency must be at least %v seconds and hence using default value of %v", minFrequency, minFrequency)
			d.oteLogger.LogErrorToFileAndConsole(ctx, "invalid flag usage", err)
			d.Frequency = minFrequency
		}
		if d.TotalDataPoints < minTotalDataPoints {
			err := fmt.Errorf("total data points must be at least %v and hence using default value of %v", minTotalDataPoints, defaultTotalDataPoints)
			d.oteLogger.LogErrorToFileAndConsole(ctx, "invalid flag usage", err)
			d.TotalDataPoints = defaultTotalDataPoints
		}
	}
	if d.OutputFilePath == "" {
		log.CtxLogger(ctx).Debugw("No path for bundle provided. Setting bundle path to default", "path", bundlePath)
		d.OutputFilePath = bundlePath
	}
	if d.OutputFileName == "" {
		timestamp := time.Now().Unix()
		d.OutputFileName = "performance-diagnostics-" + strconv.FormatInt(timestamp, 10)
		log.CtxLogger(ctx).Debugw("No name for bundle provided. Setting bundle name using timestamp", "bundle_name", timestamp)
	}
	return nil
}

func (d *Diagnose) runConfigureInstanceOTE(ctx context.Context, opts *options) subcommands.ExitStatus {
	ci := &configureinstance.ConfigureInstance{
		Check:          true,
		ExecuteFunc:    opts.exec,
		HyperThreading: d.HyperThreading,
		PrintDiff:      d.PrintDiff,
	}
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Executing ConfigureInstance")
	defer d.oteLogger.LogMessageToFileAndConsole(ctx, "Finished invoking ConfigureInstance")
	d.oteLogger.LogUsageAction(usagemetrics.PerformanceDiagnosticsConfigureInstance)
	if !d.runOpts.DaemonMode {
		onetime.SetupOneTimeLogging(opts.lp, "performance-diagnostics-configure-instance", log.StringLevelToZapcore(d.LogLevel))
	}
	res, _ := ci.Run(ctx, d.runOpts)
	if !d.runOpts.DaemonMode {
		onetime.SetupOneTimeLogging(opts.lp, d.Name(), log.StringLevelToZapcore(d.LogLevel))
	}
	ciPath := moveFiles{
		oldPath: "/var/log/google-cloud-sap-agent/performance-diagnostics-configure-instance.log",
		newPath: path.Join(d.OutputFilePath, d.OutputFileName, "performance-diagnostics-configure-instance.log"),
	}
	if d.LogPath != "" {
		ciPath.oldPath = d.LogPath
	}
	paths := []moveFiles{ciPath}
	if res != subcommands.ExitSuccess {
		d.oteLogger.LogMessageToFileAndConsole(ctx, "Error while executing ConfigureInstance OTE")
	}
	if err := d.addToBundle(ctx, paths, opts.fs); err != nil {
		d.oteLogger.LogErrorToFileAndConsole(ctx, "Error while adding ConfigureInstance OTE logs to bundle", err)
		return subcommands.ExitFailure
	}
	return res
}

// computeData collects the compute metrics of the
// HANA processes running in the HANA instances discovered.
func (d *Diagnose) computeData(ctx context.Context, opts *options) error {
	var err error

	// Run system discovery OTE to get the HANA instances.
	hanaInstances, err := d.runSystemDiscoveryOTE(ctx, opts)
	if err != nil {
		return err
	}
	d.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Found %d HANA instances: %v", len(hanaInstances), hanaInstances))

	// Collect HANA processes running in the HANA instances.
	instanceWiseHANAProcesses, err := fetchAllProcesses(ctx, opts, hanaInstances)
	if err != nil {
		return err
	}

	// Create a new directory for "compute".
	computePath := path.Join(d.OutputFilePath, d.OutputFileName, "compute")
	if err := opts.fs.MkdirAll(computePath, 0770); err != nil {
		return fmt.Errorf("error while creating a new directory: %v", err)
	}

	// Create a new file to write the report. If the file already
	// exists, truncate it to ensure only the latest data is written.
	f, err := opts.fs.OpenFile(path.Join(computePath, "hana-compute-resources.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0660)
	if err != nil {
		return fmt.Errorf("error while opening file: %v", err)
	}
	defer f.Close()
	if _, err := opts.fs.WriteStringToFile(f, "HANA Compute Resources Usage Report\n"); err != nil {
		return fmt.Errorf("error while writing to file: %v", err)
	}

	// Collect process-wise time-series CPU, Memory, IOPS metrics.
	//
	// err from here is the most recent error encountered and
	// its done this way to collect as many metrics as possible.
	for i, ip := range instanceWiseHANAProcesses {
		d.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Collecting metrics for the %v processes in instance: %v", len(instanceWiseHANAProcesses[i]), hanaInstances[i].GetSapsid()))
		instanceReport := fmt.Sprintf("\nInstance: %v\n", hanaInstances[i])
		ptsm, metricsCollectionErr := d.collectMetrics(ctx, opts, hanaInstances[i], ip)
		if metricsCollectionErr != nil {
			err = metricsCollectionErr
		}
		instanceReport += fmt.Sprintf("%v\n", buildReport(ptsm))
		d.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Finished collecting metrics for the %v processes in instance: %v", len(instanceWiseHANAProcesses[i]), hanaInstances[i].GetSapsid()))
		// Write report to the File.
		if _, fileWriteErr := opts.fs.WriteStringToFile(f, instanceReport); fileWriteErr != nil {
			err = fileWriteErr
		}
	}

	return err
}

// runSystemDiscoveryOTE fetches the HANA instances by
// invoking the system discovery OTE.
func (d *Diagnose) runSystemDiscoveryOTE(ctx context.Context, opts *options) ([]*sappb.SAPInstance, error) {
	// Disabling cloud logging of system discovery data
	// here as that's not the goal here.
	systemDiscovery := &systemdiscovery.SystemDiscovery{
		CloudLogInterface:       nil,
		CloudDiscoveryInterface: opts.cloudDiscoveryInterface,
		AppsDiscovery:           opts.appsDiscovery,
		ConfigPath:              "",
	}

	d.oteLogger.LogMessageToFileAndConsole(ctx, "Executing system discovery OTE")
	defer d.oteLogger.LogMessageToFileAndConsole(ctx, "Finished system discovery OTE")
	discovery, status := systemDiscovery.Run(ctx, d.runOpts)
	if status != subcommands.ExitSuccess {
		return nil, fmt.Errorf("system discovery did not succeed. subcommand exit status is: %v", status)
	}

	// Filter HANA instances from the SAPInstances discovered.
	hanaInstances := filterHANAInstances(discovery.GetSAPInstances())
	if hanaInstances == nil || len(hanaInstances) == 0 {
		return nil, fmt.Errorf("no HANA instances found")
	}

	return hanaInstances, nil
}

// filterHANAInstances filters the HANA instances from SAPInstances.
func filterHANAInstances(sapInstances *sappb.SAPInstances) []*sappb.SAPInstance {
	if sapInstances == nil {
		return nil
	}

	var hanaInstances []*sappb.SAPInstance
	for _, instance := range sapInstances.Instances {
		if instance.Type == sappb.InstanceType_HANA {
			hanaInstances = append(hanaInstances, instance)
		}
	}

	return hanaInstances
}

// fetchAllProcesses collects the HANA processes
// running in the HANA instances. Returns a list of lists where
// kth list contains the processes running in kth HANA instance.
func fetchAllProcesses(ctx context.Context, opts *options, HANAInstances []*sappb.SAPInstance) ([][]*computeresources.ProcessInfo, error) {
	var instanceWiseHANAProcesses [][]*computeresources.ProcessInfo
	hanaProcessFound := false

	for _, instance := range HANAInstances {
		params := computeresources.Parameters{
			SAPInstance:      instance,
			SAPControlClient: sapcontrolclient.New(instance.GetInstanceNumber()),
		}
		instanceProcesses := opts.collectProcesses(ctx, params)
		instanceWiseHANAProcesses = append(instanceWiseHANAProcesses, instanceProcesses)

		if !hanaProcessFound && len(instanceProcesses) > 0 {
			hanaProcessFound = true
		}
	}

	// If no process is found, then no metrics can be collected,
	// hence, returns an error to stop computeMetrics.
	if !hanaProcessFound {
		return nil, fmt.Errorf("no HANA processes found")
	}

	return instanceWiseHANAProcesses, nil
}

// initializeCollectMetrics initializes the map to store the
// process-wise time-series metrics and params to collect metrics.
func (d *Diagnose) initializeCollectMetrics(ctx context.Context, opts *options, instance *sappb.SAPInstance, processes []*computeresources.ProcessInfo) (map[string]*processStat, computeresources.Parameters) {
	// ptsm stores the process-wise time-series metrics.
	ptsm := make(map[string]*processStat)

	// Initialize ptsm to avoid key lookup errors.
	for _, process := range processes {
		// Metric arrays have fixed size of d.totalDataPoints
		// to make it simple to identify missed process metrics
		// after collectMetrics which is useful for final report.
		ptsm[computeresources.FormatProcessLabel(process.Name, process.PID)] = &processStat{
			processInfo: process,
			cpuUsage:    make([]*computeresources.Metric, d.TotalDataPoints),
			vms:         make([]*computeresources.Metric, d.TotalDataPoints),
			rss:         make([]*computeresources.Metric, d.TotalDataPoints),
			swap:        make([]*computeresources.Metric, d.TotalDataPoints),
			deltaReads:  make([]*computeresources.Metric, d.TotalDataPoints),
			deltaWrites: make([]*computeresources.Metric, d.TotalDataPoints),
		}
	}

	// Set up parameters to collect metrics.
	params := computeresources.Parameters{
		SAPInstance: instance,
		LastValue:   make(map[string]*process.IOCountersStat),
		NewProc:     opts.newProc,
		Config: &cpb.Configuration{
			CloudProperties: opts.cp,
			CollectionConfiguration: &cpb.CollectionConfiguration{
				ProcessMetricsFrequency: int64(d.Frequency),
			},
		},
	}

	return ptsm, params
}

// collectMetrics collects the time-series CPU, Memory and
// IOPS metrics for the given processes.
func (d *Diagnose) collectMetrics(ctx context.Context, opts *options, instance *sappb.SAPInstance, processes []*computeresources.ProcessInfo) (map[string]*processStat, error) {
	// ptsm stores the process-wise time-series metrics.
	ptsm, params := d.initializeCollectMetrics(ctx, opts, instance, processes)

	var err error
	for itr := 0; itr < d.TotalDataPoints; itr++ {
		// Collect CPU Metrics.
		cpuUsages, cpuErr := computeresources.CollectCPUPerProcess(ctx, params, processes)
		if cpuErr != nil {
			err = cpuErr
		}
		for _, metric := range cpuUsages {
			processLabel := computeresources.FormatProcessLabel(metric.ProcessInfo.Name, metric.ProcessInfo.PID)
			ptsm[processLabel].cpuUsage[itr] = metric
		}

		// Collect Memory Metrics.
		memoryUsages, memErr := computeresources.CollectMemoryPerProcess(ctx, params, processes)
		if memErr != nil {
			err = memErr
		}
		for _, metric := range memoryUsages {
			processLabel := computeresources.FormatProcessLabel(metric.VMS.ProcessInfo.Name, metric.VMS.ProcessInfo.PID)
			ptsm[processLabel].vms[itr] = metric.VMS
			ptsm[processLabel].rss[itr] = metric.RSS
			ptsm[processLabel].swap[itr] = metric.Swap
		}

		// Collect disk IOPS metrics.
		diskUsages, diskErr := computeresources.CollectIOPSPerProcess(ctx, params, processes)
		if diskErr != nil {
			err = diskErr
		}
		for _, metric := range diskUsages {
			processLabel := computeresources.FormatProcessLabel(metric.DeltaReads.ProcessInfo.Name, metric.DeltaReads.ProcessInfo.PID)
			ptsm[processLabel].deltaReads[itr] = metric.DeltaReads
			ptsm[processLabel].deltaWrites[itr] = metric.DeltaWrites
		}

		// Proceed to next iteration after `frequency` seconds.
		select {
		case <-ctx.Done():
			return ptsm, context.Cause(ctx)
		case <-time.After(time.Duration(d.Frequency) * time.Second):
		}
	}
	return ptsm, err
}

// buildReport returns a report string for the given process-wise
// time-series metrics formatted as table with uniform column width.
func buildReport(ptsm map[string]*processStat) string {
	var report string

	// Find the maximum width from metric values to have a uniform
	// column width in the report for better readability.
	maxWidth := len(timeStampFormat)
	for _, process := range ptsm {
		maxWidth = max(maxWidth, maxWidthOf(process.cpuUsage))
		maxWidth = max(maxWidth, maxWidthOf(process.vms))
		maxWidth = max(maxWidth, maxWidthOf(process.rss))
		maxWidth = max(maxWidth, maxWidthOf(process.swap))
		maxWidth = max(maxWidth, maxWidthOf(process.deltaReads))
		maxWidth = max(maxWidth, maxWidthOf(process.deltaWrites))
	}

	for _, process := range ptsm {
		report += fmt.Sprintf("\n\n----\tProcess: %v:%v\t----\n", process.processInfo.PID, process.processInfo.Name)

		// Add a row of TimeStamps in HH:MM:SS format.
		report += fmt.Sprint("| TimeStamp(HH:MM:SS)\t | ")
		for _, metric := range process.cpuUsage {
			if metric == nil {
				report += fmt.Sprintf("%s | ", strings.Repeat("-", maxWidth))
				continue
			}
			formattedTime := time.Unix(metric.TimeStamp.GetSeconds(), int64(metric.TimeStamp.GetNanos())).Format(timeStampFormat)
			report += fmt.Sprintf("%s%s | ", strings.Repeat(" ", maxWidth-len(formattedTime)), formattedTime)
		}

		// Add rows with time-series metrics for each metric type.
		report += buildMetricReport("CPU(usage in percent)", process.cpuUsage, maxWidth)
		report += buildMetricReport("Memory(VMS in MB)", process.vms, maxWidth)
		report += buildMetricReport("Memory(RSS in MB)", process.rss, maxWidth)
		report += buildMetricReport("Memory(Swap in MB)", process.swap, maxWidth)
		report += buildMetricReport("IOPS(DeltaReads /s)", process.deltaReads, maxWidth)
		report += buildMetricReport("IOPS(DeltaWrites /s)", process.deltaWrites, maxWidth)
	}
	return report
}

// buildMetricReport returns a row with the metric name
// values of given metrics separated by `|`.
func buildMetricReport(metricName string, metrics []*computeresources.Metric, maxWidth int) string {
	var report string
	for _, metric := range metrics {
		if metric == nil {
			report += fmt.Sprintf("%s | ", strings.Repeat("-", maxWidth))
			continue
		}
		formattedValue := strconv.FormatFloat(metric.Value, 'f', 4, 64)
		report += fmt.Sprintf("%s%s | ", strings.Repeat(" ", int(maxWidth)-len(formattedValue)), formattedValue)
	}
	return fmt.Sprintf("\n| %s\t | %s", metricName, report)
}

// maxWidthOf returns the maximum possible width needed to display
// the metric values from the given list of data points.
func maxWidthOf(metrics []*computeresources.Metric) int {
	var maxWidth int
	for _, metric := range metrics {
		if metric != nil {
			maxWidth = max(maxWidth, len(strconv.FormatFloat(metric.Value, 'f', 4, 64)))
		}
	}
	return maxWidth
}

// listOperations returns a map of operations to be performed.
func listOperations(ctx context.Context, operations []string) map[string]struct{} {
	ops := make(map[string]struct{})
	for _, op := range operations {
		op = strings.TrimSpace(op)
		if _, ok := ops[op]; ok {
			log.CtxLogger(ctx).Debugw("Duplicate operation already listed", "operation", op)
		}
		switch op {
		case "all":
			return map[string]struct{}{"all": {}}
		case "backup":
			ops[op] = struct{}{}
		case "io":
			ops[op] = struct{}{}
		case "compute":
			ops[op] = struct{}{}
		default:
			log.CtxLogger(ctx).Debugw("Invalid operation provided", "operation", op)
		}
	}
	return ops
}

// backup performs the backup operation.
func (d *Diagnose) backup(ctx context.Context, opts *options) []error {
	var errs []error
	d.oteLogger.LogUsageAction(usagemetrics.PerformanceDiagnosticsBackup)
	backupPath := path.Join(d.OutputFilePath, d.OutputFileName, "backup")
	if err := opts.fs.MkdirAll(backupPath, 0770); err != nil {
		errs = append(errs, err)
		return errs
	}

	config, err := d.setBackintConfig(ctx, opts.fs, opts.fs.ReadFile)
	if err != nil {
		d.oteLogger.LogErrorToFileAndConsole(ctx, "error while unmarshalling backint config, failed with error %v", err)
		errs = append(errs, err)
		return errs
	}
	opts.config = config

	// Check for retention
	if err := d.checkRetention(ctx, s.NewClient, getBucket, opts.config); err != nil {
		errs = append(errs, err)
		return errs
	}
	// Run backint.Diagnose function
	if err := d.runBackint(ctx, opts); err != nil {
		errs = append(errs, err)
	}

	// Run gsutil perfdiag function
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
	if d.TestBucket != "" {
		bucket = d.TestBucket
	}
	if bucket == "" {
		d.oteLogger.LogErrorToFileAndConsole(ctx, "error determining bucket", fmt.Errorf("no bucket provided, either in param-file or as test-bucket"))
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
		UserAgent:        cfg.StorageAgentName(),
	}

	var bucketHandle attributes
	var ok bool
	if bucketHandle, ok = ctb(ctx, connectParams); !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		d.oteLogger.LogErrorToFileAndConsole(ctx, "failed connecting to bucket", err)
		return err
	}

	attrs, err := bucketHandle.Attrs(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("failed to get bucket attributes, ensure the bucket exists and you have permission to access it", "error", err)
		return err
	}
	if attrs.RetentionPolicy != nil {
		err := errors.New("retention policy is set, backup operation will fail; please provide a bucket without retention policy")
		d.oteLogger.LogErrorToFileAndConsole(ctx, "Error performing backup operation", err)
		return err
	}
	return nil
}

// runBackint runs the diagnose function of Backint OTE.
func (d *Diagnose) runBackint(ctx context.Context, opts *options) error {
	backintParams := &backint.Backint{
		Function:  "diagnose",
		User:      "perf-diag-user",
		ParamFile: d.BackintConfigFile,
		OutFile:   path.Join(d.OutputFilePath, d.OutputFileName, "backup/backint-output.log"),
	}
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Running backint...")
	defer d.oteLogger.LogMessageToFileAndConsole(ctx, "Finished running backint.")
	if !d.runOpts.DaemonMode {
		onetime.SetupOneTimeLogging(opts.lp, "performance-diagnostics-backint", log.StringLevelToZapcore(d.LogLevel))
	}
	_, res := backintParams.Run(ctx, d.runOpts)
	if !d.runOpts.DaemonMode {
		onetime.SetupOneTimeLogging(opts.lp, d.Name(), log.StringLevelToZapcore(d.LogLevel))
	}
	if res != subcommands.ExitSuccess {
		d.oteLogger.LogMessageToFileAndConsole(ctx, "Error while executing backint")
	}

	backintPath := moveFiles{
		oldPath: "/var/log/google-cloud-sap-agent/performance-diagnostics-backint.log",
		newPath: path.Join(d.OutputFilePath, d.OutputFileName, "backup/performance-diagnostics-backint.log"),
	}

	if d.LogPath != "" {
		backintPath.oldPath = d.LogPath
	}
	paths := []moveFiles{backintPath}

	if err := d.addToBundle(ctx, paths, opts.fs); err != nil {
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
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Running gsutil perfdiag...")
	targetPath := path.Join(d.OutputFilePath, d.OutputFileName)
	targetBucket := d.TestBucket
	if targetBucket == "" {
		targetBucket = opts.config.GetBucket()
	}
	cmd := "sudo"
	args := []string{
		fmt.Sprintf("gsutil perfdiag -n 12 -c 12 -s 100m -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_100m_12p.json gs://%s", targetPath, targetBucket),
		fmt.Sprintf("gsutil perfdiag -n 12 -c 12 -s 1G -t wthru,wthru_file -d /hana/data/ -o %s/backup/out_1g_12p.json gs://%s", targetPath, targetBucket),
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
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Finished running gsutil perfdiag.")
	return errs
}

// runFIOCommands runs the FIO commands in order to simulate HANA IO operations.
func (d *Diagnose) runFIOCommands(ctx context.Context, opts *options) []error {
	var errs []error
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Running FIO commands...")
	d.oteLogger.LogUsageAction(usagemetrics.PerformanceDiagnosticsFIO)
	targetPath := path.Join(d.OutputFilePath, d.OutputFileName, "io")
	if err := opts.fs.MkdirAll(targetPath, 0770); err != nil {
		errs = append(errs, fmt.Errorf("error while creating a new directory: %s, error %s", targetPath, err.Error()))
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
	d.oteLogger.LogMessageToFileAndConsole(ctx, "Finished running FIO commands.")
	return errs
}

// execAndWriteToFile executes the command and writes the output to the file.
func execAndWriteToFile(ctx context.Context, opFile, targetPath string, params commandlineexecutor.Params, opts *options) error {
	res := opts.exec(ctx, params)
	if res.ExitCode != 0 && res.StdErr != "" {
		return fmt.Errorf("error while executing %s command %v", res.StdErr, res.ExitCode)
	}
	f, err := opts.fs.OpenFile(path.Join(targetPath, opFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0660)
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
func (d *Diagnose) addToBundle(ctx context.Context, paths []moveFiles, fs filesystem.FileSystem) error {
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
			d.oteLogger.LogErrorToFileAndConsole(ctx, "error while renaming file: %v", err)
			hasErrors = true
		}
	}

	if hasErrors {
		return fmt.Errorf("error while adding files to bundle")
	}
	return nil
}

func (d *Diagnose) setBackintConfig(ctx context.Context, fs filesystem.FileSystem, read ReadConfigFile) (*bpb.BackintConfiguration, error) {
	if d.TestBucket != "" {
		if err := d.createTempParamFile(ctx, fs, read); err != nil {
			return nil, err
		}
	} else {
		if d.BackintConfigFile == "" {
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
	bp.Config.OutputFile = path.Join(d.OutputFilePath, d.OutputFileName, "backup/backint-output.log")
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
	config.Bucket = d.TestBucket

	// Creating a temporary parameter file for backint config
	content, err := protojson.Marshal(config)
	if err != nil {
		return err
	}

	if d.BackintConfigFile != "" {
		// In case the param file provided is a .txt file, we need a .json file
		// to write the new config.
		paramFile := strings.Split(d.getParamFileName(), ".")
		d.BackintConfigFile = path.Join(d.OutputFilePath, d.OutputFileName, fmt.Sprintf("%s.json", paramFile[len(paramFile)-2]))
	} else {
		d.BackintConfigFile = path.Join(d.OutputFilePath, d.OutputFileName, "backup/backint-config.json")
	}

	f, err := fs.Create(d.BackintConfigFile)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return err
	}
	log.CtxLogger(ctx).Debugw("Created temporary parameter file", "paramFile", d.BackintConfigFile)
	return nil
}

func (d *Diagnose) unmarshalBackintConfig(ctx context.Context, read ReadConfigFile) (*bpb.BackintConfiguration, error) {
	if d.BackintConfigFile == "" {
		return nil, nil
	}

	content, err := read(d.BackintConfigFile)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error reading parameters file", "err", err)
		return nil, err
	}
	if len(content) == 0 {
		log.CtxLogger(ctx).Debug("Params file contents are empty")
		return nil, nil
	}

	config, err := configuration.Unmarshal(d.BackintConfigFile, content)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error unmarshalling parameters file", "err", err)
		return nil, err
	}
	return config, nil
}

// getParamFileName extracts the name of the parameter file from the path provided.
func (d *Diagnose) getParamFileName() string {
	if d.BackintConfigFile == "" {
		return ""
	}
	parts := strings.Split(d.BackintConfigFile, "/")
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
func (d *Diagnose) removeDestinationFolder(ctx context.Context, path string, fu filesystem.FileSystem) error {
	if err := fu.RemoveAll(path); err != nil {
		d.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error while removing folder %s", path), err)
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
		BucketName:       d.OutputBucket,
		UserAgentSuffix:  "Performance Diagnostics",
		VerifyConnection: true,
		MaxRetries:       opts.config.GetRetries(),
		Endpoint:         opts.config.GetClientEndpoint(),
		UserAgent:        cfg.StorageAgentName(),
	}
	bucketHandle, ok := ctb(ctx, connectParams)
	if !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		log.CtxLogger(ctx).Errorw("failed connecting to bucket", "err", err)
		return err
	}

	objectName := fmt.Sprintf("%s/%s.zip", d.Name(), d.OutputFileName)
	fileSize := fileInfo.Size()
	readWriter := storage.ReadWriter{
		Reader:       f,
		Copier:       io.Copy,
		BucketHandle: bucketHandle,
		BucketName:   d.OutputBucket,
		ObjectName:   objectName,
		TotalBytes:   fileSize,
		VerifyUpload: true,
	}

	rw := grw(readWriter)
	bytesWritten, err := rw.Upload(ctx)
	if err != nil {
		return err
	}

	log.CtxLogger(ctx).Infow("File uploaded", "bucket", d.OutputBucket, "bytesWritten", bytesWritten, "fileSize", fileSize)
	fmt.Println(fmt.Sprintf("Bundle uploaded to bucket %s", d.OutputBucket))
	return nil
}
