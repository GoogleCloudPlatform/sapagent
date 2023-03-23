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

package onetime

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"time"

	"flag"
	backoff "github.com/cenkalti/backoff/v4"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"golang.org/x/oauth2/google"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/timeseries"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
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
}

// Name implements the subcommand interface for snapshot.
func (*Snapshot) Name() string { return "snapshot" }

// Synopsis implements the subcommand interface for snapshot.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for snapshot.
func (*Snapshot) Usage() string {
	return `snapshot -project=<project-name> -host=<hostname> -port=<port-number> -sid=<HANA-SID> -user=<user-name>
	-source-disk=<PD-name> -source-disk-zone=<PD-zone> [-password=<passwd> | -password-secret=<secret-name>]
	`
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
}

// Execute implements the subcommand interface for snapshot.
func (s *Snapshot) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		log.Logger.Errorw("Failed to create Cloud Monitoring metric client", "error", err)
		return subcommands.ExitFailure
	}
	s.timeSeriesCreator = mc
	s.cloudProps = metadataserver.FetchCloudProperties()
	return s.snapshotHandler(ctx, gce.New, newComputeService)
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
		logErrorToFileAndConsole("ERROR: Failed to create GCE service", err)
		return subcommands.ExitFailure
	}

	log.Logger.Infow("Starting disk snapshot for HANA", "sid", s.sid)
	configureUsageMetricsForOTE(metadataserver.FetchCloudProperties(), "")
	usagemetrics.Action(usagemetrics.HANADiskSnapshot)
	s.db, err = s.connectToDB(ctx)
	if err != nil {
		logErrorToFileAndConsole("ERROR: Failed to connect to database", err)
		return subcommands.ExitFailure
	}

	s.computeService, err = computeServiceCreator(ctx)
	if err != nil {
		logErrorToFileAndConsole("ERROR: Failed to create compute service", err)
		return subcommands.ExitFailure
	}

	if err = s.runWorkflow(ctx, runQuery); err != nil {
		logErrorToFileAndConsole("Error: Failed to run HANA disk snapshot workflow", err)
		return subcommands.ExitFailure
	}
	log.Print("SUCCESS: HANA backup and persistent disk snapshot creation successful.")
	s.status = true
	return subcommands.ExitSuccess
}

func (s *Snapshot) validateParameters(os string) error {
	log.SetupOneTimeLogging(os, s.Name(), cpb.Configuration_INFO)
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

func (s *Snapshot) connectToDB(ctx context.Context) (handle *sql.DB, err error) {
	if s.password == "" && s.passwordSecret != "" {
		if s.password, err = s.gceService.GetSecret(ctx, s.project, s.passwordSecret); err != nil {
			return nil, err
		}
		log.Logger.Debug("Read from secret manager successful")
	}
	if s.db, err = databaseconnector.Connect(s.user, s.password, s.host, s.port); err != nil {
		return nil, err
	}
	log.Logger.Debug("Database connection successful")
	return s.db, nil
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
	if err = s.abandonPreparedSnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}
	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(run); err != nil {
		usagemetrics.Error(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	if err := s.createPDSnapshot(ctx); err != nil {
		log.Logger.Errorw("Error creating persistent disk snapshot", "error", err)
		usagemetrics.Error(usagemetrics.DiskSnapshotCreateFailure)
		if _, err := run(s.db, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`); err != nil {
			usagemetrics.Error(usagemetrics.DiskSnapshotFailedDBNotComplete)
			return err
		}
		return err
	}

	if _, err = run(s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, s.snapshotName)); err != nil {
		usagemetrics.Error(usagemetrics.DiskSnapshotDoneDBNotComplete)
		return err
	}
	return nil
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
	if _, err = run(s.db, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`); err != nil {
		return err
	}
	log.Logger.Info("Snapshot abandoned", "snapshotID", snapshotID)
	return nil
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

func (s *Snapshot) createPDSnapshot(ctx context.Context) (err error) {
	log.Logger.Infow("Creating persistent disk snapshot", "sourcedisk", s.disk, "sourcediskzone", s.diskZone, "snapshotname", s.snapshotName)

	var op *compute.Operation
	snapshot := &compute.Snapshot{
		Description:      s.description,
		Name:             s.snapshotName,
		StorageLocations: []string{s.storageLocation},
	}
	if s.computeService == nil {
		return fmt.Errorf("computeService needed to proceed")
	}
	if op, err = s.computeService.Disks.CreateSnapshot(s.project, s.diskZone, s.disk, snapshot).Do(); err != nil {
		return err
	}
	return s.waitForCompletionWithRetry(ctx, op)
}

func (s *Snapshot) waitForCompletion(op *compute.Operation) error {
	zos := compute.NewZoneOperationsService(s.computeService)
	tracker, err := zos.Wait(s.project, s.diskZone, op.Name).Do()
	if err != nil {
		return err
	}
	log.Logger.Infow("Snapshot creation progress", "perecentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("Snapshot creation is not DONE yet")
	}
	return nil
}

// Each waitForCompletion() calls blocks for max 120s, we sleep for 120s between
// retries a total 120 times => max_wait_duration = (120+120)*120 = 8 Hours
func (s *Snapshot) waitForCompletionWithRetry(ctx context.Context, op *compute.Operation) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 120), ctx)
	return backoff.Retry(func() error { return s.waitForCompletion(op) }, bo)
}

func newComputeService(ctx context.Context) (cs *compute.Service, err error) {
	client, err := google.DefaultClient(ctx, compute.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("Failure creating compute HTTP client" + err.Error())
	}
	if cs, err = compute.NewService(ctx, option.WithHTTPClient(client)); err != nil {
		return nil, fmt.Errorf("Failure creating compute service" + err.Error())
	}
	return cs, nil
}

func logErrorToFileAndConsole(msg string, err error) {
	log.Print(msg + " " + err.Error() + "\n" + "Refer log file at:" + log.GetLogFile())
	log.Logger.Errorw(msg, "error", err.Error())
}

// sendStatusToMonitoring sends the status of one time execution to cloud monitoring as a GAUGE metric.
func (s *Snapshot) sendStatusToMonitoring(ctx context.Context, bo *cloudmonitoring.BackOffIntervals) bool {
	if !s.sendToMonitoring {
		return false
	}
	log.Logger.Infow("Sending HANA disk snapshot status to cloud monitoring", "status", s.status)
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
		log.Logger.Errorw("Error sending status metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}
