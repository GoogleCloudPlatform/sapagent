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
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

// Snapshot has args for snapshot subcommands.
type Snapshot struct {
	project, host, port, sid       string
	user, password, passwordSecret string
	disk, diskZone, diskKeyFile    string
	storageLocation, csekKeyFile   string
}

// Name implements the subcommand interface for snapshot.
func (*Snapshot) Name() string { return "snapshot" }

// Synopsis implements the subcommand interface for snapshot.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for snapshot.
func (*Snapshot) Usage() string {
	return `snapshot -project=<project-name> -host=<hostname> -port=<port-number> -sid=<HANA-SID> -user=<user-name>
	-disk=<PD-name> -disk-zone=<PD-zone> [-password=<passwd> | -password-secret=<secret-name>]
	[-disk-key-file=<key-file-path>] [-storage-location=<storage-region>] [-csek-key-file=<csek-file-path>]
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
	fs.StringVar(&s.disk, "disk", "", "name of the persistent disk from which you want to create a snapshot (required)")
	fs.StringVar(&s.diskZone, "disk-zone", "", "zone of the persistent disk from which you want to create a snapshot. (required)")
	fs.StringVar(&s.diskKeyFile, "disk-key-file", "", `Path to the customer-supplied encryption key of the source disk. (optional)\n (required if the source disk is protected by a customer-supplied encryption key.)`)
	fs.StringVar(&s.storageLocation, "storage-location", "", "Cloud Storage multi-region or the region where you want to store your snapshot. (optional) (default: nearby regional or multi-regional location automatically chosen.)")
	fs.StringVar(&s.csekKeyFile, "csek-key-file", "", `Path to a Customer-Supplied Encryption Key (CSEK) key file. (optional)`)
}

// Execute implements the subcommand interface for snapshot.
func (s *Snapshot) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return s.handler(ctx, runtime.GOOS)
}

// queryFunc provides an easily testable replacement to the SQL API.
type queryFunc func(*sql.DB, string) (string, error)

func (s *Snapshot) handler(ctx context.Context, os string) subcommands.ExitStatus {
	log.SetupOneTimeLogging(os, s.Name(), cpb.Configuration_INFO)
	switch {
	case os == "windows":
		log.Print("Disk snapshot is only supported on Linux systems.")
		return subcommands.ExitUsageError
	case s.host == "" || s.port == "" || s.sid == "" || s.user == "" || s.disk == "" || s.diskZone == "":
		log.Print("Required arguments not passed. Usage:" + s.Usage())
		return subcommands.ExitUsageError
	case s.password == "" && s.passwordSecret == "":
		log.Print("Either -password or -password-secret is required. Usage:" + s.Usage())
		return subcommands.ExitUsageError
	}

	log.Logger.Infow("Starting disk snapshot for HANA", "sid", s.sid)
	if s.password == "" && s.passwordSecret != "" {
		gceService, err := gce.New(ctx)
		if err != nil {
			log.Print(err.Error())
			log.Logger.Errorw("Failure creating GCE Serice to query secret manager", "error", err.Error())
			return subcommands.ExitFailure
		}
		if s.password, err = gceService.GetSecret(ctx, s.project, s.passwordSecret); err != nil {
			log.Print(err.Error())
			log.Logger.Errorw("Failure reading password from secret manager", "error", err.Error())
			return subcommands.ExitFailure
		}
	}

	handle, err := databaseconnector.Connect(s.user, s.password, s.host, s.port)
	if err != nil {
		log.Print(err.Error())
		log.Logger.Errorw("Failure connecting to database", "error", err.Error())
		return subcommands.ExitFailure
	}
	if err := s.runWorkflow(handle, runQuery); err != nil {
		log.Print(err.Error())
		log.Logger.Errorw("Failure running disk snapshot", "error", err.Error())
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func (s *Snapshot) runWorkflow(handle *sql.DB, qf queryFunc) error {
	snapshotName := fmt.Sprintf("HANA-%s-snapshot-%s", s.sid, time.Now().Format("YYYYMMDD-HHMMSS"))
	snapshotID, err := createNewHANASnapshot(handle, qf, snapshotName)
	if err != nil {
		return err
	}

	if err := createPDSnapshot(s.disk, s.diskZone); err != nil {
		if _, err := qf(handle, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`); err != nil {
			return err
		}
		return err
	}

	confirmSnapshot := fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, snapshotName)
	if _, err = qf(handle, confirmSnapshot); err != nil {
		return err
	}
	return nil
}

func createNewHANASnapshot(handle *sql.DB, qf queryFunc, snapshotName string) (snapshotID string, err error) {
	// If HANA backup already exists, abandon it.
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	if snapshotID, err = qf(handle, snapshotIDQuery); err != nil {
		return "", err
	}

	if snapshotID != "" {
		_, err := qf(handle, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`)
		if err != nil {
			return "", err
		}
	}

	// Create a new HANA snapshot with the given name and return its ID.
	_, err = qf(handle, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT '%s'", snapshotName))
	if err != nil {
		return "", err
	}

	if snapshotID, err = qf(handle, snapshotIDQuery); err != nil {
		return "", err
	}
	return snapshotID, nil
}

func runQuery(h *sql.DB, q string) (string, error) {
	rows, err := h.Query(q)
	if err != nil {
		return "", err
	}
	val := ""
	rows.Scan(&val)
	log.Logger.Debugw("Query successful", "value", val)
	return val, nil
}

func createPDSnapshot(disk, diskZone string) error {
	// TODO: Create PD disk snapshots using gce APIs
	return nil
}
