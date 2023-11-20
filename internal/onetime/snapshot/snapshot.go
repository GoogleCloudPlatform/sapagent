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

// Package snapshot implements one time execution mode for snapshot.
package snapshot

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
	}
)

// Snapshot has args for snapshot subcommands.
type Snapshot struct {
	project, host, port, sid       string
	user, password, passwordSecret string
	disk, diskZone                 string

	diskKeyFile, storageLocation, csekKeyFile string
	snapshotName, description                 string
	abandonPrepared, sendToMonitoring         bool

	db                *sql.DB
	gceService        gceInterface
	computeService    *compute.Service
	status            bool
	timeSeriesCreator cloudmonitoring.TimeSeriesCreator
	cloudProps        *ipb.CloudProperties
	help, version     bool
	logLevel          string
	hanaDataPath      string
}

// Name implements the subcommand interface for snapshot.
func (*Snapshot) Name() string { return "snapshot" }

// Synopsis implements the subcommand interface for snapshot.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for snapshot.
func (*Snapshot) Usage() string {
	return `Usage: snapshot -project=<project-name> -host=<hostname> -port=<port-number> -sid=<HANA-SID> -user=<user-name>
	-source-disk=<PD-name> -source-disk-zone=<PD-zone> [-password=<passwd> | -password-secret=<secret-name>] [-abandon-prepared=<true|false>]
	[-h] [-v] [loglevel]=<debug|info|warn|error>` + "\n"
}

// SetFlags implements the subcommand interface for snapshot.
func (s *Snapshot) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.project, "project", "", "GCP project. (required)")
	fs.StringVar(&s.host, "host", "", "HANA host. (required)")
	fs.StringVar(&s.port, "port", "", "HANA port. (required)")
	fs.StringVar(&s.sid, "sid", "", "HANA SID. (required)")
	fs.StringVar(&s.user, "user", "", "HANA username. (required)")
	fs.StringVar(&s.password, "password", "", "HANA password. (discouraged - use password-secret instead)")
	fs.StringVar(&s.passwordSecret, "password-secret", "", "Secret Manager secret name that holds HANA Password.")
	fs.BoolVar(&s.abandonPrepared, "abandon-prepared", false, "Abandon any prepared HANA snapshot that is in progress, (optional - defaults to false)")
	fs.StringVar(&s.snapshotName, "snapshot-name", "", "Snapshot name override.(Optional - deafaults to 'hana-sid-snapshot-yyyymmdd-hhmmss')")
	fs.StringVar(&s.disk, "source-disk", "", "name of the persistent disk from which you want to create a snapshot (required)")
	fs.StringVar(&s.diskZone, "source-disk-zone", "", "zone of the persistent disk from which you want to create a snapshot. (required)")
	fs.StringVar(&s.diskKeyFile, "source-disk-key-file", "", `Path to the customer-supplied encryption key of the source disk. (optional)\n (required if the source disk is protected by a customer-supplied encryption key.)`)
	fs.StringVar(&s.storageLocation, "storage-location", "", "Cloud Storage multi-region or the region where you want to store your snapshot. (optional) (default: nearby regional or multi-regional location automatically chosen.)")
	fs.StringVar(&s.csekKeyFile, "csek-key-file", "", `Path to a Customer-Supplied Encryption Key (CSEK) key file. (optional)`)
	fs.StringVar(&s.description, "snapshot-description", "", "Description of the new snapshot(optional)")
	fs.BoolVar(&s.sendToMonitoring, "send-status-to-monitoring", true, "Send the execution status to cloud monitoring as a metric")
	fs.BoolVar(&s.help, "h", false, "Displays help")
	fs.BoolVar(&s.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&s.logLevel, "loglevel", "info", "Sets the logging level")
}

// Execute implements the subcommand interface for snapshot.
func (s *Snapshot) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if s.help {
		return onetime.HelpCommand(f)
	}
	if s.version {
		onetime.PrintAgentVersion()
		return subcommands.ExitSuccess
	}
	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	s.cloudProps, ok = args[2].(*ipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, s.Name(), log.StringLevelToZapcore(s.logLevel))

	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		return subcommands.ExitFailure
	}
	s.timeSeriesCreator = mc

	p, err := s.parseBasePath(ctx, "basepath_datavolumes", commandlineexecutor.ExecuteCommand)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to parse HANA data path", "error", err)
		return subcommands.ExitFailure
	}
	s.hanaDataPath = p
	return s.snapshotHandler(ctx, gce.NewGCEClient, onetime.NewComputeService)
}

func (s *Snapshot) snapshotHandler(ctx context.Context, gceServiceCreator gceServiceFunc, computeServiceCreator computeServiceFunc) subcommands.ExitStatus {
	var err error
	s.status = false
	if err = s.validateParameters(runtime.GOOS); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}

	defer s.sendStatusToMonitoring(ctx, cloudmonitoring.NewDefaultBackOffIntervals())

	s.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create GCE service", err)
		return subcommands.ExitFailure
	}

	log.CtxLogger(ctx).Infow("Starting disk snapshot for HANA", "sid", s.sid)
	onetime.ConfigureUsageMetricsForOTE(s.cloudProps, "", "")
	usagemetrics.Action(usagemetrics.HANADiskSnapshot)
	dbp := databaseconnector.Params{
		Username:       s.user,
		Password:       s.password,
		PasswordSecret: s.passwordSecret,
		Host:           s.host,
		Port:           s.port,
		GCEService:     s.gceService,
		Project:        s.project,
	}
	if s.db, err = databaseconnector.Connect(ctx, dbp); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to connect to database", err)
		return subcommands.ExitFailure
	}

	s.computeService, err = computeServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create compute service", err)
		return subcommands.ExitFailure
	}

	if err = s.runWorkflow(ctx, runQuery); err != nil {
		onetime.LogErrorToFileAndConsole("Error: Failed to run HANA disk snapshot workflow", err)
		return subcommands.ExitFailure
	}
	log.Print("SUCCESS: HANA backup and persistent disk snapshot creation successful.")
	s.status = true
	return subcommands.ExitSuccess
}

func (s *Snapshot) validateParameters(os string) error {
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot is only supported on Linux systems")
	case s.host == "" || s.port == "" || s.sid == "" || s.user == "" || s.disk == "" || s.diskZone == "":
		return fmt.Errorf("required arguments not passed. Usage:" + s.Usage())
	case s.password == "" && s.passwordSecret == "":
		return fmt.Errorf("either -password or -password-secret is required. Usage:" + s.Usage())
	}
	if s.snapshotName == "" {
		t := time.Now()
		s.snapshotName = fmt.Sprintf("hana-%s-snapshot-%d%02d%02d-%02d%02d%02d",
			strings.ToLower(s.sid), t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
	if s.description == "" {
		s.description = fmt.Sprintf("Snapshot created by Agent for SAP for HANA SID: %q", s.sid)
	}
	log.Logger.Debug("Parameter validation successful.")
	return nil
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

func (s *Snapshot) runWorkflow(ctx context.Context, run queryFunc) (err error) {
	log.CtxLogger(ctx).Info("Start run snapshot workflow")
	if err = s.abandonPreparedSnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}
	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	op, err := s.createPDSnapshot(ctx)
	s.unFreezeXFS(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error creating persistent disk snapshot", "error", err)
		s.pdSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.Logger.Info("Waiting for PD snapshot to complete uploading.")
	if err := s.waitForUploadCompletionWithRetry(ctx, op); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading persistent disk snapshot", "error", err)
		s.pdSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.Logger.Info("PD Snapshot created, marking HANA snapshot as successful.")
	if _, err = run(s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, s.snapshotName)); err != nil {
		log.CtxLogger(ctx).Errorw("Error marking HANA snapshot as SUCCESSFUL")
		usagemetrics.Error(usagemetrics.DiskSnapshotDoneDBNotComplete)
		return err
	}
	return nil
}

func (s *Snapshot) pdSnapshotFailureHandler(ctx context.Context, run queryFunc, snapshotID string) {
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
	if !s.abandonPrepared {
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
	log.Logger.Infow("Creating new HANA snapshot", "comment", s.snapshotName)
	_, err = run(s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT '%s'", s.snapshotName))
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
	log.Logger.Infow("Snapshot created", "snapshotid", snapshotID, "comment", s.snapshotName)
	return snapshotID, nil
}

func (s *Snapshot) createPDSnapshot(ctx context.Context) (*compute.Operation, error) {
	log.CtxLogger(ctx).Infow("Creating persistent disk snapshot", "sourcedisk", s.disk, "sourcediskzone", s.diskZone, "snapshotname", s.snapshotName)

	var op *compute.Operation
	var err error

	snapshot := &compute.Snapshot{
		Description:      s.description,
		Name:             s.snapshotName,
		SnapshotType:     "STANDARD",
		StorageLocations: []string{s.storageLocation},
	}

	if s.diskKeyFile != "" {
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.project, s.diskZone, s.disk)
		srcDiskKey, err := readKey(s.diskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			return nil, err
		}
		snapshot.SourceDiskEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}
	if s.computeService == nil {
		return nil, fmt.Errorf("computeService needed to proceed")
	}
	if err := s.freezeXFS(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		return nil, err
	}
	if op, err = s.computeService.Disks.CreateSnapshot(s.project, s.diskZone, s.disk, snapshot).Do(); err != nil {
		return nil, err
	}
	if err := s.waitForCreationCompletionWithRetry(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}

func (s *Snapshot) waitForCreationCompletion(op *compute.Operation) error {
	ss, err := s.computeService.Snapshots.Get(s.project, s.snapshotName).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Snapshot creation status", "snapshot", s.snapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)
	if ss.Status == "CREATING" {
		return fmt.Errorf("snapshot creation is in progress, snapshot name: %s, status:  CREATING", s.snapshotName)
	}
	log.Logger.Infow("Snapshot creation progress", "snapshot", s.snapshotName, "status", ss.Status)
	return nil
}

// Each waitForCreationCompletion() returns immediately, we sleep for 120s between
// retries a total 10 times => max_wait_duration = 120*10 = 20 minutes
// TODO: change timeout depending on PD limits
func (s *Snapshot) waitForCreationCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 10), ctx)
	return backoff.Retry(func() error { return s.waitForCreationCompletion(op) }, bo)
}

func (s *Snapshot) waitForUploadCompletion(op *compute.Operation) error {
	zos := compute.NewZoneOperationsService(s.computeService)
	tracker, err := zos.Wait(s.project, s.diskZone, op.Name).Do()
	if err != nil {
		log.Logger.Errorw("Error in operation", "operation", op.Name)
		return err
	}
	log.Logger.Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("Compute operation is not DONE yet")
	}

	ss, err := s.computeService.Snapshots.Get(s.project, s.snapshotName).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Snapshot upload status", "snapshot", s.snapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)

	if ss.Status == "READY" {
		return nil
	}
	return fmt.Errorf("snapshot %s not READY yet, snapshotStatus: %s, operationStatus: %s", s.snapshotName, ss.Status, op.Status)
}

// Each waitForUploadCompletionWithRetry() returns immediately, we sleep for 120s between
// retries a total 120 times => max_wait_duration = 120*120 = 4 Hours
// TODO: change timeout depending on PD limits
func (s *Snapshot) waitForUploadCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 120), ctx)
	return backoff.Retry(func() error { return s.waitForUploadCompletion(op) }, bo)
}

// sendStatusToMonitoring sends the status of one time execution to cloud monitoring as a GAUGE metric.
func (s *Snapshot) sendStatusToMonitoring(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) bool {
	if !s.sendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Sending HANA disk snapshot status to cloud monitoring", "status", s.status)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildBool(timeseries.Params{
			CloudProp:  s.cloudProps,
			MetricType: "workload.googleapis.com/sap/agent/" + s.Name(),
			Timestamp:  tspb.Now(),
			BoolValue:  s.status,
			MetricLabels: map[string]string{
				"sid":           s.sid,
				"disk":          s.disk,
				"snapshot_name": s.snapshotName,
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, s.timeSeriesCreator, bo, s.project); err != nil {
		log.CtxLogger(ctx).Errorw("Error sending status metric to cloud monitoring", "error", err.Error())
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

func (s *Snapshot) freezeXFS(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-f " + s.hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("Filesystem frozen successfully", "hanaDataPath", s.hanaDataPath)
	return nil
}

func (s *Snapshot) unFreezeXFS(ctx context.Context, exec commandlineexecutor.Execute) error {
	result := exec(ctx, commandlineexecutor.Params{Executable: "/usr/sbin/xfs_freeze", ArgsToSplit: "-u " + s.hanaDataPath})
	if result.Error != nil {
		return fmt.Errorf("failure un freezing XFS, stderr: %s, err: %s", result.StdErr, result.Error)
	}
	log.CtxLogger(ctx).Infow("Filesystem unfrozen successfully", "hanaDataPath", s.hanaDataPath)
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
