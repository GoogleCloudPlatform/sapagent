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

// Package hanadiskbackup implements one time execution mode for HANA Disk based backup workflow.
package hanadiskbackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"flag"
	backoff "github.com/cenkalti/backoff/v4"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskrestore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// queryFunc provides testable replacement to the SQL API.
	queryFunc func(*sql.DB, string) (string, error)

	// gceServiceFunc provides testable replacement for gce.New API.
	gceServiceFunc func(context.Context) (*gce.GCE, error)

	// computeServiceFunc provides testable replacement for compute.Service API
	computeServiceFunc func(context.Context) (*compute.Service, error)

	// gceInterface is the testable equivalent for gce.GCE for secret manager access.
	gceInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
		DiskAttachedToInstance(projectID, zone, instanceName, diskName string) (string, bool, error)
	}
)

const (
	metricPrefix = "workload.googleapis.com/sap/agent/"
)

var (
	dbFreezeStartTime, workflowStartTime time.Time
)

// Snapshot has args for snapshot subcommands.
// TODO: Improve Disk Backup and Restore code coverage and reduce redundancy.
type Snapshot struct {
	Project, Host, Port, Sid, HanaSidAdm, InstanceID string
	HanaDBUser, Password, PasswordSecret             string
	Disk, DiskZone                                   string

	DiskKeyFile, StorageLocation                        string
	SnapshotName, SnapshotType, Description             string
	AbandonPrepared, SendToMonitoring, freezeFileSystem bool

	db                                *sql.DB
	gceService                        gceInterface
	computeService                    *compute.Service
	status                            bool
	timeSeriesCreator                 cloudmonitoring.TimeSeriesCreator
	help, version                     bool
	SkipDBSnapshotForChangeDiskType   bool
	HANAChangeDiskTypeOTEName         string
	ForceStopHANA                     bool
	LogLevel                          string
	hanaDataPath                      string
	logicalDataPath, physicalDataPath string
	labels                            string
	IIOTEParams                       *onetime.InternallyInvokedOTE
}

// Name implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Name() string { return "hanadiskbackup" }

// Synopsis implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Usage() string {
	return `Usage: hanadiskbackup -port=<port-number> -sid=<HANA-sid> -hana_db_user=<HANA DB User>
	-source-disk=<disk-name> -source-disk-zone=<disk-zone> [-host=<hostname>] [-project=<project-name>]
	[-password=<passwd> | -password-secret=<secret-name>] [-abandon-prepared=<true|false>]
	[-send-status-to-monitoring]=<true|false> [-source-disk-key-file=<path-to-key-file>]
	[-storage-location=<storage-location>]
	[-snapshot-description=<description>] [-snapshot-name=<snapshot-name>]
	[-snapshot-type=<snapshot-type>] [-freeze-file-system=<true|false>]
	[-labels="label1=value1,label2=value2"]
	[-h] [-v] [-loglevel]=<debug|info|warn|error>
	` + "\n"
}

// SetFlags implements the subcommand interface for hanadiskbackup.
func (s *Snapshot) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.Port, "port", "", "HANA port. (optional - Either port or instance-id must be provided)")
	fs.StringVar(&s.Sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&s.InstanceID, "instance-id", "", "HANA instance ID. (optional - Either port or instance-id must be provided)")
	fs.StringVar(&s.HanaDBUser, "hana-db-user", "", "HANA DB Username. (required)")
	fs.StringVar(&s.Password, "password", "", "HANA password. (discouraged - use password-secret instead)")
	fs.StringVar(&s.PasswordSecret, "password-secret", "", "Secret Manager secret name that holds HANA password.")
	fs.StringVar(&s.Disk, "source-disk", "", "name of the disk from which you want to create a snapshot (required)")
	fs.StringVar(&s.DiskZone, "source-disk-zone", "", "zone of the disk from which you want to create a snapshot. (required)")
	fs.BoolVar(&s.freezeFileSystem, "freeze-file-system", false, "Freeze file system. (optional) Default: false")
	fs.StringVar(&s.Host, "host", "localhost", "HANA host. (optional)")
	fs.StringVar(&s.Project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.BoolVar(&s.AbandonPrepared, "abandon-prepared", false, "Abandon any prepared HANA snapshot that is in progress, (optional) Default: false)")
	fs.BoolVar(&s.SkipDBSnapshotForChangeDiskType, "skip-db-snapshot-for-change-disk-type", false, "Skip DB snapshot for change disk type, (optional) Default: false")
	fs.StringVar(&s.SnapshotName, "snapshot-name", "", "Snapshot name override.(Optional - deafaults to 'snapshot-diskname-yyyymmdd-hhmmss'.)")
	fs.StringVar(&s.SnapshotType, "snapshot-type", "STANDARD", "Snapshot type override.(Optional - deafaults to 'STANDARD', use 'ARCHIVE' for archive snapshots.)")
	fs.StringVar(&s.DiskKeyFile, "source-disk-key-file", "", `Path to the customer-supplied encryption key of the source disk. (optional)\n (required if the source disk is protected by a customer-supplied encryption key.)`)
	fs.StringVar(&s.StorageLocation, "storage-location", "", "Cloud Storage multi-region or the region where you want to store your snapshot. (optional) Default: nearby regional or multi-regional location automatically chosen.")
	fs.StringVar(&s.Description, "snapshot-description", "", "Description of the new snapshot(optional)")
	fs.BoolVar(&s.SendToMonitoring, "send-metrics-to-monitoring", true, "Send backup related metrics to cloud monitoring. (optional) Default: true")
	fs.BoolVar(&s.help, "h", false, "Displays help")
	fs.BoolVar(&s.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&s.LogLevel, "loglevel", "info", "Sets the logging level")
	fs.StringVar(&s.labels, "labels", "", "Labels to be added to the disk snapshot")
}

// Execute implements the subcommand interface for hanadiskbackup.
func (s *Snapshot) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// Help and version will return before the args are parsed.
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:     s.Name(),
		Help:     s.help,
		Version:  s.version,
		LogLevel: s.LogLevel,
		Fs:       f,
		IIOTE:    s.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create Cloud Monitoring metric client", err)
		return subcommands.ExitFailure
	}
	s.timeSeriesCreator = mc

	p, err := s.parseBasePath(ctx, "basepath_datavolumes", commandlineexecutor.ExecuteCommand)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to parse HANA data path", err)
		return subcommands.ExitFailure
	}
	s.hanaDataPath = p
	return s.snapshotHandler(ctx, gce.NewGCEClient, onetime.NewComputeService, cp)
}

func (s *Snapshot) snapshotHandler(ctx context.Context, gceServiceCreator gceServiceFunc, computeServiceCreator computeServiceFunc, cp *ipb.CloudProperties) subcommands.ExitStatus {
	var err error
	s.status = false
	if err = s.validateParameters(runtime.GOOS, cp); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}

	defer s.sendStatusToMonitoring(ctx, cloudmonitoring.NewDefaultBackOffIntervals(), cp)

	s.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create GCE service", err)
		return subcommands.ExitFailure
	}

	if err := s.checkPreConditions(ctx); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to check preconditions", err)
		return subcommands.ExitFailure
	}

	log.CtxLogger(ctx).Infow("Starting disk snapshot for HANA", "sid", s.Sid)
	usagemetrics.Action(usagemetrics.HANADiskSnapshot)
	dbp := databaseconnector.Params{
		Username:       s.HanaDBUser,
		Password:       s.Password,
		PasswordSecret: s.PasswordSecret,
		Host:           s.Host,
		Port:           s.Port,
		GCEService:     s.gceService,
		Project:        s.Project,
	}
	if s.SkipDBSnapshotForChangeDiskType {
		onetime.LogMessageToFileAndConsole("Skipping connecting to HANA Database in case of changedisktype workflow.")
	} else if s.db, err = databaseconnector.Connect(ctx, dbp); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to connect to database", err)
		return subcommands.ExitFailure
	}

	s.computeService, err = computeServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create compute service", err)
		return subcommands.ExitFailure
	}

	workflowStartTime := time.Now()
	if s.SkipDBSnapshotForChangeDiskType {
		err := s.runWorkflowForChangeDiskType(ctx, runQuery, cp)
		if err != nil {
			onetime.LogErrorToFileAndConsole("Error: Failed to run HANA disk snapshot workflow", err)
			return subcommands.ExitFailure
		}
	} else if err = s.runWorkflow(ctx, runQuery, cp); err != nil {
		onetime.LogErrorToFileAndConsole("Error: Failed to run HANA disk snapshot workflow", err)
		return subcommands.ExitFailure
	}
	workflowDur := time.Since(workflowStartTime)
	s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/totaltime", workflowDur, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	log.Print("SUCCESS: HANA backup and disk snapshot creation successful.")
	s.status = true
	return subcommands.ExitSuccess
}

func (s *Snapshot) validateParameters(os string, cp *ipb.CloudProperties) error {
	if s.SkipDBSnapshotForChangeDiskType {
		log.Logger.Debug("Skipping parameter validation for change disk type workflow.")
		return nil
	}
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot is only supported on Linux systems")
	case s.Sid == "" || s.HanaDBUser == "" || s.Disk == "" || s.DiskZone == "":
		return fmt.Errorf("required arguments not passed. Usage:" + s.Usage())
	case s.Port == "" && s.InstanceID == "":
		return fmt.Errorf("either -port or -instance-id is required. Usage:" + s.Usage())
	case s.Password == "" && s.PasswordSecret == "":
		return fmt.Errorf("either -password or -password-secret is required. Usage:" + s.Usage())
	}
	if s.Project == "" {
		s.Project = cp.GetProjectId()
	}
	if s.SnapshotName == "" {
		t := time.Now()
		s.SnapshotName = fmt.Sprintf("snapshot-%s-%d%02d%02d-%02d%02d%02d",
			s.Disk, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
	if s.Description == "" {
		s.Description = fmt.Sprintf("Snapshot created by Agent for SAP for HANA sid: %q", s.Sid)
	}
	s.Port = s.portValue()
	log.Logger.Debug("Parameter validation successful.")
	return nil
}

func (s *Snapshot) portValue() string {
	if s.Port == "" {
		log.Logger.Debug("Building port number of the system database from instance ID", "instanceID", s.InstanceID)
		return fmt.Sprintf("3%s13", s.InstanceID)
	}
	return s.Port
}

func runQuery(h *sql.DB, q string) (string, error) {
	rows, err := h.Query(q)
	if err != nil {
		return "", err
	}
	val := ""
	for rows.Next() {
		if err := rows.Scan(&val); err != nil {
			return "", err
		}
	}
	return val, nil
}

func (s *Snapshot) runWorkflow(ctx context.Context, run queryFunc, cp *ipb.CloudProperties) (err error) {
	_, ok, err := s.gceService.DiskAttachedToInstance(s.Project, s.DiskZone, cp.GetInstanceName(), s.Disk)
	if err != nil {
		onetime.LogErrorToFileAndConsole(fmt.Sprintf("ERROR: Failed to check if the source-disk=%v is attached to the instance", s.Disk), err)
		return fmt.Errorf("failed to check if the source-disk=%v is attached to the instance", s.Disk)
	}
	if !ok {
		return fmt.Errorf("source-disk=%v is not attached to the instance", s.Disk)
	}
	log.CtxLogger(ctx).Info("Start run HANA Disk based backup workflow")
	if err = s.abandonPreparedSnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}
	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}
	op, err := s.createDiskSnapshot(ctx)
	s.unFreezeXFS(ctx, commandlineexecutor.ExecuteCommand)
	if s.freezeFileSystem {
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}

	if err != nil {
		log.CtxLogger(ctx).Errorw("Error creating disk snapshot", "error", err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	onetime.LogMessageToFileAndConsole("Waiting for disk snapshot to complete uploading.")
	if err := s.waitForUploadCompletionWithRetry(ctx, op); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading disk snapshot", "error", err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.Logger.Info("Disk snapshot created, marking HANA snapshot as successful.")
	if _, err = run(s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, s.SnapshotName)); err != nil {
		log.CtxLogger(ctx).Errorw("Error marking HANA snapshot as SUCCESSFUL")
		usagemetrics.Error(usagemetrics.DiskSnapshotDoneDBNotComplete)
		return err
	}
	return nil
}

func (s *Snapshot) runWorkflowForChangeDiskType(ctx context.Context, run queryFunc, cp *ipb.CloudProperties) (err error) {
	err = s.prepareForChangeDiskTypeWorkflow(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Error preparing for change disk type workflow", err)
		return err
	}
	_, ok, err := s.gceService.DiskAttachedToInstance(s.Project, s.DiskZone, cp.GetInstanceName(), s.Disk)
	if err != nil {
		return fmt.Errorf("failed to check if the source-disk=%v is attached to the instance", s.Disk)
	}
	if !ok {
		return fmt.Errorf("source-disk=%v is not attached to the instance", s.Disk)
	}
	op, err := s.createDiskSnapshot(ctx)
	s.unFreezeXFS(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error creating disk snapshot", "error", err)
		return err
	}

	log.Logger.Info("Waiting for disk snapshot to complete uploading.")
	if err := s.waitForUploadCompletionWithRetry(ctx, op); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading disk snapshot", "error", err)
		return err
	}

	log.Logger.Info("Disk snapshot created.")
	return nil
}

func (s *Snapshot) prepareForChangeDiskTypeWorkflow(ctx context.Context, exec commandlineexecutor.Execute) (err error) {
	onetime.LogMessageToFileAndConsole("Stopping HANA")
	hdr := hanadiskrestore.Restorer{}
	err = hdr.StopHANA(ctx, exec)
	if err != nil {
		return err
	}
	err = hdr.WaitForIndexServerToStopWithRetry(ctx, exec)
	if err != nil {
		return err
	}
	return nil
}

// waitForIndexServerToStop() waits for the hdb index server to stop.
func (s *Snapshot) waitForIndexServerToStop(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: `-c 'ps x | grep hdbindexs | grep -v grep'`,
		User:        s.HanaSidAdm,
	})

	if result.ExitCode == 0 {
		return fmt.Errorf("failure waiting for index server to stop, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	return nil
}

func (s *Snapshot) diskSnapshotFailureHandler(ctx context.Context, run queryFunc, snapshotID string) {
	usagemetrics.Error(usagemetrics.DiskSnapshotCreateFailure)
	if err := s.abandonHANASnapshot(run, snapshotID); err != nil {
		log.CtxLogger(ctx).Errorw("Error discarding HANA snapshot")
		usagemetrics.Error(usagemetrics.DiskSnapshotFailedDBNotComplete)
	}
}

func (s *Snapshot) abandonPreparedSnapshot(run queryFunc) error {
	// Read the already prepared snapshot.
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	snapshotID, err := run(s.db, snapshotIDQuery)
	if err != nil {
		return err
	}
	if snapshotID == "" {
		log.Logger.Info("No prepared snapshot found")
		return nil
	}

	log.Logger.Infow("Found prepared snapshot", "snapshotid", snapshotID)
	if !s.AbandonPrepared {
		return fmt.Errorf("a HANA data snapshot is already prepared or is in progress, rerun with <-abandon-prepared=true> to abandon this snapshot")
	}
	if err = s.abandonHANASnapshot(run, snapshotID); err != nil {
		return err
	}
	log.Logger.Info("Snapshot abandoned", "snapshotID", snapshotID)
	return nil
}

func (s *Snapshot) abandonHANASnapshot(run queryFunc, snapshotID string) error {
	_, err := run(s.db, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`)
	return err
}

func (s *Snapshot) createNewHANASnapshot(run queryFunc) (snapshotID string, err error) {
	// Create a new HANA snapshot with the given name and return its ID.
	log.Logger.Infow("Creating new HANA snapshot", "comment", s.SnapshotName)
	_, err = run(s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT '%s'", s.SnapshotName))
	if err != nil {
		return "", err
	}
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	if snapshotID, err = run(s.db, snapshotIDQuery); err != nil {
		return "", err
	}
	if snapshotID == "" {
		return "", fmt.Errorf("could not read ID of the newly created snapshot")
	}
	log.Logger.Infow("Snapshot created", "snapshotid", snapshotID, "comment", s.SnapshotName)
	return snapshotID, nil
}

func (s *Snapshot) createDiskSnapshot(ctx context.Context) (*compute.Operation, error) {
	log.CtxLogger(ctx).Infow("Creating disk snapshot", "sourcedisk", s.Disk, "sourcediskzone", s.DiskZone, "snapshotname", s.SnapshotName)

	var op *compute.Operation
	var err error

	snapshot := &compute.Snapshot{
		Description:      s.Description,
		Name:             s.SnapshotName,
		SnapshotType:     s.SnapshotType,
		StorageLocations: []string{s.StorageLocation},
		Labels:           s.parseLabels(),
	}

	// In case customer is taking a snapshot from an encrypted disk, the snapshot created from it also
	// needs to be encrypted. For simplicity we support the use case in which disk encryption and
	// snapshot encryption key are the same.
	if s.DiskKeyFile != "" {
		usagemetrics.Action(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := readKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			usagemetrics.Error(usagemetrics.EncryptedDiskSnapshotFailure)
			return nil, err
		}
		snapshot.SourceDiskEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
		snapshot.SnapshotEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}
	if s.computeService == nil {
		return nil, fmt.Errorf("computeService needed to proceed")
	}
	dbFreezeStartTime = time.Now()
	if err := s.freezeXFS(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return nil, err
	}
	if op, err = s.computeService.Disks.CreateSnapshot(s.Project, s.DiskZone, s.Disk, snapshot).Do(); err != nil {
		return nil, err
	}
	if err := s.waitForCreationCompletionWithRetry(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}

func (s *Snapshot) waitForCreationCompletion(op *compute.Operation) error {
	ss, err := s.computeService.Snapshots.Get(s.Project, s.SnapshotName).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Snapshot creation status", "snapshot", s.SnapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)
	if ss.Status == "CREATING" {
		return fmt.Errorf("snapshot creation is in progress, snapshot name: %s, status:  CREATING", s.SnapshotName)
	}
	log.Logger.Infow("Snapshot creation progress", "snapshot", s.SnapshotName, "status", ss.Status)
	return nil
}

// Each waitForCreationCompletion() returns immediately, we sleep for 1s between
// retries a total 300 times => max_wait_duration = 5 minutes
// TODO: change timeout depending on disk snapshot limits
func (s *Snapshot) waitForCreationCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(1 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 300), ctx)
	return backoff.Retry(func() error { return s.waitForCreationCompletion(op) }, bo)
}

func (s *Snapshot) waitForUploadCompletion(op *compute.Operation) error {
	zos := compute.NewZoneOperationsService(s.computeService)
	tracker, err := zos.Wait(s.Project, s.DiskZone, op.Name).Do()
	if err != nil {
		log.Logger.Errorw("Error in operation", "operation", op.Name)
		return err
	}
	log.Logger.Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("Compute operation is not DONE yet")
	}

	ss, err := s.computeService.Snapshots.Get(s.Project, s.SnapshotName).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Snapshot upload status", "snapshot", s.SnapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)

	if ss.Status == "READY" {
		return nil
	}
	return fmt.Errorf("snapshot %s not READY yet, snapshotStatus: %s, operationStatus: %s", s.SnapshotName, ss.Status, op.Status)
}

// Each waitForUploadCompletionWithRetry() returns immediately, we sleep for 30s between
// retries a total 480 times => max_wait_duration = 30*480 = 4 Hours
// TODO: change timeout depending on disk snapshot limits
func (s *Snapshot) waitForUploadCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(30 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 480), ctx)
	return backoff.Retry(func() error { return s.waitForUploadCompletion(op) }, bo)
}

// sendStatusToMonitoring sends the status of one time execution to cloud monitoring as a GAUGE metric.
func (s *Snapshot) sendStatusToMonitoring(ctx context.Context, bo *cloudmonitoring.BackOffIntervals, cp *ipb.CloudProperties) bool {
	if !s.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Optional: sending HANA disk snapshot status to cloud monitoring", "status", s.status)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildBool(timeseries.Params{
			CloudProp:  cp,
			MetricType: metricPrefix + s.Name() + "/status",
			Timestamp:  tspb.Now(),
			BoolValue:  s.status,
			MetricLabels: map[string]string{
				"sid":           s.Sid,
				"disk":          s.Disk,
				"snapshot_name": s.SnapshotName,
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, s.timeSeriesCreator, bo, s.Project); err != nil {
		log.CtxLogger(ctx).Debugw("Error sending status metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}

func (s *Snapshot) sendDurationToCloudMonitoring(ctx context.Context, mtype string, dur time.Duration, bo *cloudmonitoring.BackOffIntervals, cp *ipb.CloudProperties) bool {
	if !s.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Optional: Sending HANA disk snapshot duration to cloud monitoring", "duration", dur)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildFloat64(timeseries.Params{
			CloudProp:    cp,
			MetricType:   mtype,
			Timestamp:    tspb.Now(),
			Float64Value: dur.Seconds(),
			MetricLabels: map[string]string{
				"sid":           s.Sid,
				"disk":          s.Disk,
				"snapshot_name": s.SnapshotName,
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, s.timeSeriesCreator, bo, s.Project); err != nil {
		log.CtxLogger(ctx).Debugw("Error sending duration metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}

func (s *Snapshot) parseBasePath(ctx context.Context, pattern string, exec commandlineexecutor.Execute) (string, error) {
	args := `-c 'grep ` + pattern + ` /usr/sap/*/SYS/global/hdb/custom/config/global.ini | cut -d= -f 2'`
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing base path, stderr: %s, err: %s", result.StdErr, result.Error)
	}

	basePath := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Found HANA Base data directory", "hanaDataPath", basePath)
	return basePath, nil
}

// unmount unmounts the given directory.
func (s *Snapshot) unmount(ctx context.Context, path string, exec commandlineexecutor.Execute) error {
	log.CtxLogger(ctx).Infow("Unmount path", "directory", path)
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: fmt.Sprintf(" -c 'sync;umount -f %s'", path),
	})
	if result.Error != nil {
		r := exec(ctx, commandlineexecutor.Params{
			Executable:  "bash",
			ArgsToSplit: fmt.Sprintf(" -c 'lsof | grep %s'", path), // NOLINT
		})
		msg := `failure unmounting directory: %s, stderr: %s, err: %s.
		Here are the possible open references to the path, stdout: %s stderr: %s.
		Please ensure these references are cleaned up for unmount to proceed and retry the command`
		return fmt.Errorf(msg, path, result.StdErr, result.Error, r.StdOut, r.StdErr)
	}
	return nil
}

func (s *Snapshot) freezeXFS(ctx context.Context, exec commandlineexecutor.Execute) error {
	if !s.freezeFileSystem {
		// NO-OP when freeze is not requested.
		return nil
	}
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-f " + s.hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("Filesystem frozen successfully", "hanaDataPath", s.hanaDataPath)
	return nil
}

func (s *Snapshot) unFreezeXFS(ctx context.Context, exec commandlineexecutor.Execute) error {
	if !s.freezeFileSystem {
		// NO-OP when freeze is not requested.
		return nil
	}
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-u " + s.hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure un freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("Filesystem unfrozen successfully", "hanaDataPath", s.hanaDataPath)
	return nil
}

func (s *Snapshot) checkPreConditions(ctx context.Context) error {
	return s.checkDataDir(ctx)
}

// parseLogicalPath parses the logical path from the base path.
func (s *Snapshot) parseLogicalPath(ctx context.Context, basePath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf("-c 'df --output=source %s | tail -n 1'", basePath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing logical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	logicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Directory to logical device mapping", "DirectoryPath", basePath, "LogicalDevice", logicalDevice)
	return logicalDevice, nil
}

// parsePhysicalPath parses the physical path from the logical path.
func (s *Snapshot) parsePhysicalPath(ctx context.Context, logicalPath string, exec commandlineexecutor.Execute) (string, error) {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf("-c '/sbin/lvdisplay -m %s | grep \"Physical volume\" | awk \"{print \\$3}\"'", logicalPath),
	})
	if result.Error != nil {
		return "", fmt.Errorf("failure parsing physical path, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	phyisicalDevice := strings.TrimSuffix(result.StdOut, "\n")
	log.CtxLogger(ctx).Infow("Logical device to physical device mapping", "LogicalDevice", logicalPath, "PhysicalDevice", phyisicalDevice)
	return phyisicalDevice, nil
}

// checkDataDir checks if the data directory is valid and has a valid physical volume.
func (s *Snapshot) checkDataDir(ctx context.Context) error {
	var err error
	log.CtxLogger(ctx).Infow("Data volume base path", "path", s.hanaDataPath)
	if s.logicalDataPath, err = s.parseLogicalPath(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	if !strings.HasPrefix(s.logicalDataPath, "/dev/mapper") {
		return fmt.Errorf("only data disks using LVM are supported, exiting")
	}
	if s.physicalDataPath, err = s.parsePhysicalPath(ctx, s.logicalDataPath, commandlineexecutor.ExecuteCommand); err != nil {
		return err
	}
	return s.checkDataDeviceForStripes(ctx, commandlineexecutor.ExecuteCommand)
}

// checkDataDeviceForStripes checks if the data device is striped.
func (s *Snapshot) checkDataDeviceForStripes(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "/bin/sh",
		ArgsToSplit: fmt.Sprintf(" -c '/sbin/lvdisplay -m %s | grep Stripes'", s.logicalDataPath),
	})
	if result.ExitCode == 0 {
		return fmt.Errorf("backup of striped HANA data disks are not currently supported, exiting")
	}
	return nil
}

// Key defines the contents of each entry in the encryption key file.
// Reference: https://cloud.google.com/compute/docs/disks/customer-supplied-encryption#key_file
type Key struct {
	URI     string `json:"uri"`
	Key     string `json:"key"`
	KeyType string `json:"key-type"`
}

func readKey(file, diskURI string, read configuration.ReadConfigFile) (string, error) {
	var keys []Key
	fileContent, err := read(file)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(fileContent, &keys); err != nil {
		return "", err
	}

	for _, k := range keys {
		if k.URI == diskURI {
			return k.Key, nil
		}
	}
	return "", fmt.Errorf("no matching key for for the disk")
}

func (s *Snapshot) parseLabels() map[string]string {
	labels := make(map[string]string)
	if s.labels != "" {
		for _, label := range strings.Split(s.labels, ",") {
			split := strings.Split(label, "=")
			if len(split) == 2 {
				labels[split[0]] = split[1]
			}
		}
	}
	return labels
}
