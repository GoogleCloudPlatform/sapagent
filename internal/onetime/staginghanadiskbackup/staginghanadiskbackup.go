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

// Package staginghanadiskbackup implements one time execution mode for HANA Disk based backup workflow.
package staginghanadiskbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanabackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/instantsnapshotgroup"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// queryFunc provides testable replacement to the SQL API.
	queryFunc func(context.Context, *databaseconnector.DBHandle, string) (string, error)

	// gceServiceFunc provides testable replacement for gce.New API.
	gceServiceFunc func(context.Context) (*gce.GCE, error)

	// computeServiceFunc provides testable replacement for compute.Service API
	computeServiceFunc func(context.Context) (*compute.Service, error)

	// diskSnapshotFunc provides testable replacement for compute.service.Disks.CreateSnapshot
	diskSnapshotFunc func(*compute.Snapshot) fakeDiskCreateSnapshotCall

	// fakeDiskCreateSnapshotCall is the testable equivalent for compute.DisksCreateSnapshotCall.
	fakeDiskCreateSnapshotCall interface {
		Context(context.Context) *compute.DisksCreateSnapshotCall
		Do(...googleapi.CallOption) (*compute.Operation, error)
		Fields(...googleapi.Field) *compute.DisksCreateSnapshotCall
		GuestFlush(bool) *compute.DisksCreateSnapshotCall
		Header() http.Header
		RequestId(string) *compute.DisksCreateSnapshotCall
	}

	// gceInterface is the testable equivalent for gce.GCE for secret manager access.
	gceInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
		GetInstance(project, zone, instance string) (*compute.Instance, error)
		ListZoneOperations(project, zone, filter string, maxResults int64) (*compute.OperationList, error)
		GetDisk(project, zone, name string) (*compute.Disk, error)
		ListDisks(project, zone, filter string) (*compute.DiskList, error)
		DiskAttachedToInstance(projectID, zone, instanceName, diskName string) (string, bool, error)
		WaitForSnapshotCreationCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error
		WaitForSnapshotUploadCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error
	}

	// ISGInterface is the testable equivalent for ISGService for ISG operations.
	ISGInterface interface {
		NewService() error
		GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error)
		CreateISG(ctx context.Context, project, zone string, data []byte) error
		DescribeInstantSnapshots(ctx context.Context, project, zone, isgName string) ([]instantsnapshotgroup.ISItem, error)
		DescribeStandardSnapshots(ctx context.Context, project, zone, isgName string) ([]*compute.Snapshot, error)
		DeleteISG(ctx context.Context, project, zone, isgName string) error
		WaitForStandardSnapshotCreationWithRetry(ctx context.Context, baseURL string) error
		WaitForStandardSnapshotCompletionWithRetry(ctx context.Context, baseURL string) error
		WaitForISGUploadCompletionWithRetry(ctx context.Context, baseURL string) error
	}
)

const (
	metricPrefix = "workload.googleapis.com/sap/agent/"
)

var (
	dbFreezeStartTime, workflowStartTime time.Time
)

// Snapshot has args for snapshot subcommands.
type Snapshot struct {
	Project                                string `json:"project"`
	Host                                   string `json:"host"`
	Port                                   string `json:"port"`
	Sid                                    string `json:"sid"`
	HanaSidAdm                             string `json:"-"`
	InstanceID                             string `json:"instance-id"`
	HanaDBUser                             string `json:"hana-db-user"`
	Password                               string `json:"password"`
	PasswordSecret                         string `json:"password-secret"`
	HDBUserstoreKey                        string `json:"hdbuserstore-key"`
	Disk                                   string `json:"source-disk"`
	DiskZone                               string `json:"source-disk-zone"`
	Region                                 string
	DiskKeyFile                            string `json:"source-disk-key-file"`
	StorageLocation                        string `json:"storage-location"`
	SnapshotName                           string `json:"snapshot-name"`
	SnapshotType                           string `json:"snapshot-type"`
	Description                            string `json:"snapshot-description"`
	AbandonPrepared                        bool   `json:"abandon-prepared,string"`
	SendToMonitoring                       bool   `json:"send-metrics-to-monitoring,string"`
	FreezeFileSystem                       bool   `json:"freeze-file-system,string"`
	ConfirmDataSnapshotAfterCreate         bool   `json:"confirm-data-snapshot-after-create,string"`
	groupSnapshotName                      string
	disks                                  []string
	db                                     *databaseconnector.DBHandle
	gceService                             gceInterface
	computeService                         *compute.Service
	isgService                             ISGInterface
	status                                 bool
	timeSeriesCreator                      cloudmonitoring.TimeSeriesCreator
	help                                   bool
	SkipDBSnapshotForChangeDiskType        bool   `json:"skip-db-snapshot-for-change-disk-type,string"`
	HANAChangeDiskTypeOTEName              string `json:"-"`
	ForceStopHANA                          bool   `json:"-"`
	LogLevel                               string `json:"loglevel"`
	LogPath                                string `json:"log-path"`
	hanaDataPath                           string
	logicalDataPath, physicalDataPath      string
	Labels                                 string                        `json:"labels"`
	IIOTEParams                            *onetime.InternallyInvokedOTE `json:"-"`
	instanceProperties                     *ipb.InstanceProperties
	cgName                                 string
	groupSnapshot                          bool
	provisionedIops, provisionedThroughput int64
	oteLogger                              *onetime.OTELogger
	isGroupSnapshot                        bool
	isgDisks                               []*compute.AttachedDisk
}

// Name implements the subcommand interface for staginghanadiskbackup.
func (*Snapshot) Name() string { return "staginghanadiskbackup" }

// Synopsis implements the subcommand interface for staginghanadiskbackup.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for staginghanadiskbackup.
func (*Snapshot) Usage() string {
	return `Usage: staginghanadiskbackup -port=<port-number> -sid=<HANA-sid> -hana-db-user=<HANA DB User>
  [-source-disk=<disk-name>] [-source-disk-zone=<disk-zone>] [-host=<hostname>]
  [-project=<project-name>] [-password=<passwd> | -password-secret=<secret-name>]
  [-hdbuserstore-key=<userstore-key>] [-abandon-prepared=<true|false>]
  [-send-status-to-monitoring]=<true|false>] [-source-disk-key-file=<path-to-key-file>]
  [-storage-location=<storage-location>] [-snapshot-description=<description>]
  [-snapshot-name=<snapshot-name>] [-snapshot-type=<snapshot-type>]
  [-freeze-file-system=<true|false>] [-labels="label1=value1,label2=value2"]
  [-confirm-data-snapshot-after-create=<true|false>]
  [-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]
  ` + "\n"
}

// SetFlags implements the subcommand interface for staginghanadiskbackup.
func (s *Snapshot) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.Port, "port", "", "HANA port. (optional - Either port or instance-id must be provided)")
	fs.StringVar(&s.Sid, "sid", "", "HANA sid. (required)")
	fs.StringVar(&s.InstanceID, "instance-id", "", "HANA instance ID. (optional - Either port or instance-id must be provided)")
	fs.StringVar(&s.HanaDBUser, "hana-db-user", "", "HANA DB Username. (optional) when hdbuserstore-key is passed, required for other modes of authentication")
	fs.StringVar(&s.Password, "password", "", "HANA password. (discouraged - use password-secret or hdbuserstore-key instead)")
	fs.StringVar(&s.PasswordSecret, "password-secret", "", "Secret Manager secret name that holds HANA password. (optional - either password-secret or hdbuserstore-key must be provided)")
	fs.StringVar(&s.HDBUserstoreKey, "hdbuserstore-key", "", "HANA userstore key specific to HANA instance.")
	fs.StringVar(&s.Disk, "source-disk", "", "name of the disk from which you want to create a snapshot (optional). Default: disk used to store /hana/data/")
	fs.StringVar(&s.DiskZone, "source-disk-zone", "", "zone of the disk from which you want to create a snapshot. (optional) Default: Same zone as current instance")
	fs.BoolVar(&s.FreezeFileSystem, "freeze-file-system", false, "Freeze file system. (optional) Default: false")
	fs.StringVar(&s.Host, "host", "localhost", "HANA host. (optional)")
	fs.StringVar(&s.Project, "project", "", "GCP project. (optional) Default: project corresponding to this instance")
	fs.BoolVar(&s.AbandonPrepared, "abandon-prepared", false, "Abandon any prepared HANA snapshot that is in progress, (optional) Default: false)")
	fs.BoolVar(&s.SkipDBSnapshotForChangeDiskType, "skip-db-snapshot-for-change-disk-type", false, "Skip DB snapshot for change disk type, (optional) Default: false")
	fs.BoolVar(&s.ConfirmDataSnapshotAfterCreate, "confirm-data-snapshot-after-create", true, "Confirm HANA data snapshot after disk snapshot create and then wait for upload. (optional) Default: true")
	fs.StringVar(&s.SnapshotName, "snapshot-name", "", "Snapshot name override.(Optional - defaults to 'snapshot-diskname-yyyymmdd-hhmmss'.)")
	fs.StringVar(&s.SnapshotType, "snapshot-type", "STANDARD", "Snapshot type override.(Optional - defaults to 'STANDARD', use 'ARCHIVE' for archive snapshots.)")
	fs.StringVar(&s.DiskKeyFile, "source-disk-key-file", "", `Path to the customer-supplied encryption key of the source disk. (optional)\n (required if the source disk is protected by a customer-supplied encryption key.)`)
	fs.StringVar(&s.StorageLocation, "storage-location", "", "Cloud Storage multi-region or the region where you want to store your snapshot. (optional) Default: nearby regional or multi-regional location automatically chosen.")
	fs.StringVar(&s.Description, "snapshot-description", "", "Description of the new snapshot(optional)")
	fs.BoolVar(&s.SendToMonitoring, "send-metrics-to-monitoring", true, "Send backup related metrics to cloud monitoring. (optional) Default: true")
	fs.StringVar(&s.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/staginghanadiskbackup.log")
	fs.BoolVar(&s.help, "h", false, "Displays help")
	fs.StringVar(&s.LogLevel, "loglevel", "info", "Sets the logging level")
	fs.StringVar(&s.Labels, "labels", "", "Labels to be added to the disk snapshot")
	// These are temporary flags for ISG staging testing.
	fs.BoolVar(&s.isGroupSnapshot, "is-group-snapshot", false, "Is group snapshot enabled. (optional) Default: false")
	fs.StringVar(&s.groupSnapshotName, "group-snapshot-name", "", "Group Snapshot name override.(optional - defaults to 'group-snapshot-diskname-yyyymmdd-hhmmss'.)")
}

// Execute implements the subcommand interface for staginghanadiskbackup.
func (s *Snapshot) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	// Help will return before the args are parsed.
	lp, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     s.Name(),
		Help:     s.help,
		LogLevel: s.LogLevel,
		LogPath:  s.LogPath,
		Fs:       f,
		IIOTE:    s.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, status := s.Run(ctx, onetime.CreateRunOptions(cp, false))
	if status == subcommands.ExitFailure {
		supportbundle.CollectAgentSupport(ctx, f, lp, cp, s.Name())
	}
	return status
}

// Run executes the command and returns the message and exit status.
func (s *Snapshot) Run(ctx context.Context, opts *onetime.RunOptions) (string, subcommands.ExitStatus) {
	s.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	if err := s.validateParameters(runtime.GOOS, opts.CloudProperties); err != nil {
		errMessage := err.Error()
		s.oteLogger.LogMessageToConsole(errMessage)
		return errMessage, subcommands.ExitUsageError
	}

	mc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		errMessage := "ERROR: Failed to create Cloud Monitoring metric client"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}
	s.timeSeriesCreator = mc
	message, exitStatus := s.snapshotHandler(ctx, gce.NewGCEClient, onetime.NewComputeService, opts.CloudProperties)
	if exitStatus != subcommands.ExitSuccess {
		return message, subcommands.ExitFailure
	}
	return message, subcommands.ExitSuccess
}

func (s *Snapshot) snapshotHandler(ctx context.Context, gceServiceCreator gceServiceFunc, computeServiceCreator computeServiceFunc, cp *ipb.CloudProperties) (string, subcommands.ExitStatus) {
	var err error
	s.status = false

	defer s.sendStatusToMonitoring(ctx, cloudmonitoring.NewDefaultBackOffIntervals(), cp)

	s.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		errMessage := "ERROR: Failed to create GCE service"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	if s.hanaDataPath, s.logicalDataPath, s.physicalDataPath, err = hanabackup.CheckDataDir(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		errMessage := "ERROR: Failed to check preconditions"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	if s.Disk == "" {
		log.CtxLogger(ctx).Info("Reading disk mapping for /hana/data/")
		ctx = context.WithValue(ctx, instantsnapshotgroup.EnvKey("env"), "staging")
		if err := s.readDiskMapping(ctx, cp); err != nil {
			errMessage := "ERROR: Failed to read disk mapping"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			return errMessage, subcommands.ExitFailure
		}

		if len(s.disks) > 1 {
			s.isgService = &instantsnapshotgroup.ISGService{}
			if err := s.isgService.NewService(); err != nil {
				errMessage := "ERROR: Failed to create Instant Snapshot Group service"
				s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
				return errMessage, subcommands.ExitFailure
			}
			if err := s.validateDisksBelongToCG(ctx); err != nil {
				errMessage := "ERROR: Failed to validate whether disks belong to consistency group"
				s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
				return errMessage, subcommands.ExitFailure
			}
			s.groupSnapshot = true
		}
		log.CtxLogger(ctx).Infow("Successfully read disk mapping for /hana/data/", "disks", s.disks, "cgName", s.cgName, "groupSnapshot", s.groupSnapshot)
	}

	log.CtxLogger(ctx).Infow("Starting disk snapshot for HANA", "sid", s.Sid)
	s.oteLogger.LogUsageAction(usagemetrics.HANADiskSnapshot)
	if s.HDBUserstoreKey != "" {
		s.oteLogger.LogUsageAction(usagemetrics.HANADiskSnapshotUserstoreKey)
	}
	dbp := databaseconnector.Params{
		Username:       s.HanaDBUser,
		Password:       s.Password,
		PasswordSecret: s.PasswordSecret,
		Host:           s.Host,
		Port:           s.Port,
		HDBUserKey:     s.HDBUserstoreKey,
		GCEService:     s.gceService,
		Project:        s.Project,
		SID:            s.Sid,
	}
	if s.SkipDBSnapshotForChangeDiskType {
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Skipping connecting to HANA Database in case of changedisktype workflow.")
	} else if s.db, err = databaseconnector.CreateDBHandle(ctx, dbp); err != nil {
		errMessage := "ERROR: Failed to connect to database"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	s.computeService, err = computeServiceCreator(ctx)
	if err != nil {
		errMessage := "ERROR: Failed to create compute service"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	workflowStartTime := time.Now()
	if s.SkipDBSnapshotForChangeDiskType {
		err := s.runWorkflowForChangeDiskType(ctx, s.createSnapshot, cp)
		if err != nil {
			errMessage := "ERROR: Failed to run HANA disk snapshot workflow"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			return errMessage, subcommands.ExitFailure
		}
	} else if s.groupSnapshot {
		if err := s.runWorkflowForInstantSnapshotGroups(ctx, runQuery, s.createSnapshot, cp); err != nil {
			errMessage := "ERROR: Failed to run HANA disk snapshot workflow"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			return errMessage, subcommands.ExitFailure
		}
	} else if err = s.runWorkflowForDiskSnapshot(ctx, runQuery, s.createSnapshot, cp); err != nil {
		errMessage := "ERROR: Failed to run HANA disk snapshot workflow"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}
	workflowDur := time.Since(workflowStartTime)

	snapshotName := s.SnapshotName
	if s.groupSnapshot {
		snapshotName = s.groupSnapshotName
		s.oteLogger.LogMessageToConsole(fmt.Sprintf("SUCCESS: HANA backup and group disk snapshot creation successful. Group Backup Name: %s", snapshotName))
	}
	successMessage := fmt.Sprintf("SUCCESS: HANA backup and disk snapshot creation successful. Snapshot Name: %s", snapshotName)
	s.oteLogger.LogMessageToConsole(successMessage)

	s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/totaltime", snapshotName, workflowDur, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	s.status = true
	return successMessage, subcommands.ExitSuccess
}

func (s *Snapshot) validateDisksBelongToCG(ctx context.Context) error {
	disksTraversed := []string{}
	for _, d := range s.disks {
		var cg string
		var err error

		cg, err = s.stagingReadConsistencyGroup(ctx, d)
		// TODO: Update this when ISG APIs are in prod.
		// cg, err = s.readConsistencyGroup(ctx, d)
		if err != nil {
			return err
		}

		if s.cgName != "" && cg != s.cgName {
			return fmt.Errorf("all disks should belong to the same consistency group, however disk %s belongs to %s, while other disks %s belong to %s", d, cg, disksTraversed, s.cgName)
		}
		disksTraversed = append(disksTraversed, d)
		s.cgName = cg
	}

	return nil
}

func (s *Snapshot) readDiskMapping(ctx context.Context, cp *ipb.CloudProperties) error {
	var instance *compute.Instance
	var err error

	instanceInfoReader := instanceinfo.New(&instanceinfo.PhysicalPathReader{OS: runtime.GOOS}, s.gceService)
	if instance, s.instanceProperties, err = instanceInfoReader.ReadDiskMapping(ctx, &cpb.Configuration{CloudProperties: cp}); err != nil {
		return err
	}

	log.CtxLogger(ctx).Debugw("Reading disk mapping", "ip", s.instanceProperties)
	for _, d := range s.instanceProperties.GetDisks() {
		if strings.Contains(s.physicalDataPath, d.GetMapping()) {
			log.CtxLogger(ctx).Debugw("Found disk mapping", "physicalPath", s.physicalDataPath, "diskName", d.GetDiskName())
			s.Disk = d.GetDiskName()
			s.DiskZone = cp.GetZone()
			s.disks = append(s.disks, d.GetDiskName())
			s.isgDisks = instance.Disks
			s.provisionedIops = d.GetProvisionedIops()
			s.provisionedThroughput = d.GetProvisionedThroughput()
		}
	}
	return nil
}

func (s *Snapshot) validateParameters(os string, cp *ipb.CloudProperties) error {
	if s.SkipDBSnapshotForChangeDiskType {
		log.Logger.Debug("Skipping parameter validation for change disk type workflow.")
		return nil
	}
	switch {
	case os == "windows":
		return fmt.Errorf("disk snapshot is only supported on Linux systems")
	case s.Sid == "":
		return fmt.Errorf("required argument -sid not passed. Usage:" + s.Usage())
	case s.HDBUserstoreKey == "":
		switch {
		case s.HanaDBUser == "":
			return fmt.Errorf("either -hana-db-user or -hdbuserstore-key is required. Usage:" + s.Usage())
		case s.Port == "" && s.InstanceID == "":
			return fmt.Errorf("either -port and -instance-id, or -hdbuserstore-key is required. Usage:" + s.Usage())
		case s.Password == "" && s.PasswordSecret == "":
			return fmt.Errorf("either -password, -password-secret or -hdbuserstore-key is required. Usage:" + s.Usage())
		}
	}
	if s.Project == "" {
		s.Project = cp.GetProjectId()
	}
	if s.DiskZone == "" {
		s.DiskZone = cp.GetZone()
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

func runQuery(ctx context.Context, h *databaseconnector.DBHandle, q string) (string, error) {
	rows, err := h.Query(ctx, q, commandlineexecutor.ExecuteCommand)
	if err != nil {
		return "", err
	}
	val := ""
	for rows.Next() {
		if err := rows.ReadRow(&val); err != nil {
			return "", err
		}
	}
	return val, nil
}

func (s *Snapshot) createSnapshot(snapshot *compute.Snapshot) fakeDiskCreateSnapshotCall {
	return s.computeService.Disks.CreateSnapshot(s.Project, s.DiskZone, s.Disk, snapshot)
}

func (s *Snapshot) runWorkflowForInstantSnapshotGroups(ctx context.Context, run queryFunc, createSnapshot diskSnapshotFunc, cp *ipb.CloudProperties) (err error) {
	// TODO: Commenting this out for now until ISG APIs are in prod.
	// for _, d := range s.disks {
	//  if err = s.isDiskAttachedToInstance(ctx, d, cp); err != nil {
	//    return err
	//  }
	// }

	log.CtxLogger(ctx).Info("Start run HANA Striped Disk based backup workflow")
	if err = s.abandonPreparedSnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	err = s.createInstantSnapshotGroup(ctx)
	if s.FreezeFileSystem {
		if err := hanabackup.UnFreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "error unfreezing XFS", err)
			return err
		}
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", s.groupSnapshotName, freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error creating instant snapshot group, HANA snapshot %s is not successful", snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	var instantSnapshots []instantsnapshotgroup.ISItem
	if instantSnapshots, err = s.convertISGtoSS(ctx, cp); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error converting instant snapshots to %s, HANA snapshot %s is not successful", strings.ToLower(s.SnapshotType), snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	if s.ConfirmDataSnapshotAfterCreate {
		log.CtxLogger(ctx).Info("Marking HANA snapshot as successful after disk standard snapshots are created but not yet uploaded.")
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}

	s.oteLogger.LogMessageToFileAndConsole(ctx, "Waiting for disk snapshots to complete uploading.")
	if err := s.waitForStandardSnapshotsUpload(ctx, instantSnapshots); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading disk snapshots", "error", err)
		if s.ConfirmDataSnapshotAfterCreate {
			s.oteLogger.LogErrorToFileAndConsole(
				ctx, fmt.Sprintf("Error uploading disk snapshots, HANA snapshot %s is not successful", snapshotID), err[0],
			)
		}
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err[0]
	}
	if err = s.isgService.DeleteISG(ctx, s.Project, s.DiskZone, s.groupSnapshotName); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("error deleting instant snapshot group, HANA snapshot %s is not successful", snapshotID), err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.CtxLogger(ctx).Info(fmt.Sprintf("Instant snapshot group and %s equivalents created, marking HANA snapshot as successful.", strings.ToLower(s.SnapshotType)))
	if !s.ConfirmDataSnapshotAfterCreate {
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Snapshot) runWorkflowForDiskSnapshot(ctx context.Context, run queryFunc, createSnapshot diskSnapshotFunc, cp *ipb.CloudProperties) (err error) {
	if err := s.isDiskAttachedToInstance(ctx, s.Disk, cp); err != nil {
		return err
	}

	log.CtxLogger(ctx).Info("Start run HANA Disk based backup workflow")
	if err = s.abandonPreparedSnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}
	var snapshotID string
	if snapshotID, err = s.createNewHANASnapshot(ctx, run); err != nil {
		s.oteLogger.LogUsageError(usagemetrics.SnapshotDBNotReadyFailure)
		return err
	}

	op, err := s.createDiskSnapshot(ctx, createSnapshot)
	if s.FreezeFileSystem {
		if err := hanabackup.UnFreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "Error unfreezing XFS", err)
			return err
		}
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", s.SnapshotName, freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}

	if err != nil {
		log.CtxLogger(ctx).Errorw("Error creating disk snapshot", "error", err)
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	if s.ConfirmDataSnapshotAfterCreate {
		log.CtxLogger(ctx).Info("Marking HANA snapshot as successful after disk snapshot is created but not yet uploaded.")
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Waiting for disk snapshot to complete uploading.")
	if err := s.gceService.WaitForSnapshotUploadCompletionWithRetry(ctx, op, s.Project, s.DiskZone, s.SnapshotName); err != nil {
		log.CtxLogger(ctx).Errorw("Error uploading disk snapshot", "error", err)
		if s.ConfirmDataSnapshotAfterCreate {
			s.oteLogger.LogErrorToFileAndConsole(
				ctx, fmt.Sprintf("Error uploading disk snapshot, HANA snapshot %s is not successful", snapshotID), err,
			)
		}
		s.diskSnapshotFailureHandler(ctx, run, snapshotID)
		return err
	}

	log.CtxLogger(ctx).Info("Disk snapshot created, marking HANA snapshot as successful.")
	if !s.ConfirmDataSnapshotAfterCreate {
		if err := s.markSnapshotAsSuccessful(ctx, run, snapshotID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Snapshot) isDiskAttachedToInstance(ctx context.Context, disk string, cp *ipb.CloudProperties) error {
	_, ok, err := s.gceService.DiskAttachedToInstance(s.Project, s.DiskZone, cp.GetInstanceName(), disk)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("ERROR: Failed to check if the source-disk=%v is attached to the instance", disk), err)
		return fmt.Errorf("failed to check if the source-disk=%v is attached to the instance", disk)
	}
	if !ok {
		return fmt.Errorf("source-disk=%v is not attached to the instance", disk)
	}
	return nil
}

func (s *Snapshot) markSnapshotAsSuccessful(ctx context.Context, run queryFunc, snapshotID string) error {
	if _, err := run(ctx, s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID %s SUCCESSFUL '%s'", snapshotID, s.SnapshotName)); err != nil {
		log.CtxLogger(ctx).Errorw("Error marking HANA snapshot as SUCCESSFUL")
		s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotDoneDBNotComplete)
		return err
	}
	return nil
}

func (s *Snapshot) runWorkflowForChangeDiskType(ctx context.Context, createSnapshot diskSnapshotFunc, cp *ipb.CloudProperties) (err error) {
	err = s.prepareForChangeDiskTypeWorkflow(ctx, commandlineexecutor.ExecuteCommand)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error preparing for change disk type workflow", err)
		return err
	}
	_, ok, err := s.gceService.DiskAttachedToInstance(s.Project, s.DiskZone, cp.GetInstanceName(), s.Disk)
	if err != nil {
		return fmt.Errorf("failed to check if the source-disk=%v is attached to the instance", s.Disk)
	}
	if !ok {
		return fmt.Errorf("source-disk=%v is not attached to the instance", s.Disk)
	}
	op, err := s.createDiskSnapshot(ctx, createSnapshot)
	if s.FreezeFileSystem {
		if err := hanabackup.UnFreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "Error unfreezing XFS", err)
			return err
		}
		freezeTime := time.Since(dbFreezeStartTime)
		defer s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/dbfreezetime", s.SnapshotName, freezeTime, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	}
	if err != nil {
		return err
	}

	log.CtxLogger(ctx).Info("Waiting for disk snapshot to complete uploading.")
	if err := s.gceService.WaitForSnapshotUploadCompletionWithRetry(ctx, op, s.Project, s.DiskZone, s.SnapshotName); err != nil {
		return err
	}

	log.CtxLogger(ctx).Info("Disk snapshot created.")
	return nil
}

func (s *Snapshot) prepareForChangeDiskTypeWorkflow(ctx context.Context, exec commandlineexecutor.Execute) (err error) {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Stopping HANA")
	if err = hanabackup.StopHANA(ctx, false, s.HanaSidAdm, s.Sid, exec); err != nil {
		return err
	}
	if err = hanabackup.WaitForIndexServerToStopWithRetry(ctx, s.HanaSidAdm, exec); err != nil {
		return err
	}
	return nil
}

func (s *Snapshot) diskSnapshotFailureHandler(ctx context.Context, run queryFunc, snapshotID string) {
	s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotCreateFailure)
	if err := s.abandonHANASnapshot(ctx, run, snapshotID); err != nil {
		log.CtxLogger(ctx).Errorw("Error discarding HANA snapshot")
		s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotFailedDBNotComplete)
	}
}

func (s *Snapshot) abandonPreparedSnapshot(ctx context.Context, run queryFunc) error {
	// Read the already prepared snapshot.
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	snapshotID, err := run(ctx, s.db, snapshotIDQuery)
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
	if err = s.abandonHANASnapshot(ctx, run, snapshotID); err != nil {
		return err
	}
	log.Logger.Info("Snapshot abandoned", "snapshotID", snapshotID)
	return nil
}

func (s *Snapshot) abandonHANASnapshot(ctx context.Context, run queryFunc, snapshotID string) error {
	_, err := run(ctx, s.db, `BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID `+snapshotID+` UNSUCCESSFUL`)
	return err
}

func (s *Snapshot) createNewHANASnapshot(ctx context.Context, run queryFunc) (snapshotID string, err error) {
	// Create a new HANA snapshot with the given name and return its ID.
	log.Logger.Infow("Creating new HANA snapshot", "comment", s.SnapshotName)
	_, err = run(ctx, s.db, fmt.Sprintf("BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT '%s'", s.SnapshotName))
	if err != nil {
		return "", err
	}
	snapshotIDQuery := `SELECT BACKUP_ID FROM M_BACKUP_CATALOG WHERE ENTRY_TYPE_NAME = 'data snapshot' AND STATE_NAME = 'prepared'`
	if snapshotID, err = run(ctx, s.db, snapshotIDQuery); err != nil {
		return "", err
	}
	if snapshotID == "" {
		return "", fmt.Errorf("could not read ID of the newly created snapshot")
	}
	log.Logger.Infow("Snapshot created", "snapshotid", snapshotID, "comment", s.SnapshotName)
	return snapshotID, nil
}

func (s *Snapshot) createInstantSnapshotGroup(ctx context.Context) error {
	timestamp := time.Now().UTC().UnixMilli()
	if s.groupSnapshotName == "" {
		s.groupSnapshotName = s.cgName + fmt.Sprintf("-%d", timestamp)
	}
	log.CtxLogger(ctx).Infow("Creating Instant snapshot group", "disks", s.disks, "disks zone", s.DiskZone, "groupSnapshotName", s.groupSnapshotName)

	parts := strings.Split(s.DiskZone, "-")
	if len(parts) < 3 {
		return fmt.Errorf("invalid zone, cannot fetch region from it: %s", s.DiskZone)
	}
	region := strings.Join(parts[:len(parts)-1], "-")

	groupSnapshot := map[string]any{
		"name":                   s.groupSnapshotName,
		"sourceConsistencyGroup": fmt.Sprintf("projects/%s/regions/%s/resourcePolicies/%s", s.Project, region, s.cgName),
		"description":            s.Description,
	}

	if s.DiskKeyFile != "" {
		s.oteLogger.LogUsageAction(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			s.oteLogger.LogUsageError(usagemetrics.EncryptedDiskSnapshotFailure)
			return err
		}
		groupSnapshot["sourceDiskEncryptionKey"] = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}

	dbFreezeStartTime = time.Now()
	if s.FreezeFileSystem {
		if err := hanabackup.FreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			return err
		}
	}

	data, err := json.Marshal(groupSnapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal json, err: %w", err)
	}
	if err := s.isgService.CreateISG(ctx, s.Project, s.DiskZone, data); err != nil {
		return err
	}
	baseURL := fmt.Sprintf("https://www.googleapis.com/compute/staging_alpha/projects/%s/zones/%s/instantSnapshotGroups/%s", s.Project, s.DiskZone, s.groupSnapshotName)
	if err := s.isgService.WaitForISGUploadCompletionWithRetry(ctx, baseURL); err != nil {
		return err
	}
	return nil
}

func (s *Snapshot) convertISGtoSS(ctx context.Context, cp *ipb.CloudProperties) (instantSnapshots []instantsnapshotgroup.ISItem, err error) {
	log.CtxLogger(ctx).Info("Converting Instant Snapshot Group to Standard Snapshots")
	instantSnapshots, err = s.isgService.DescribeInstantSnapshots(ctx, s.Project, s.DiskZone, s.groupSnapshotName)
	if err != nil {
		return instantSnapshots, err
	}

	errors := make(chan error, len(instantSnapshots))
	jobs := make(chan *instantsnapshotgroup.ISItem, runtime.NumCPU())

	var wg sync.WaitGroup
	for range instantSnapshots {
		wg.Add(1)
		go s.stagingCreateBackup(ctx, &wg, jobs, errors)
	}
	for _, is := range instantSnapshots {
		jobs <- &is
	}

	close(jobs)
	wg.Wait()
	close(errors)

	var ok bool
	if err, ok = <-errors; !ok {
		return instantSnapshots, nil
	}

	log.CtxLogger(ctx).Error(err)
	for err = range errors {
		log.CtxLogger(ctx).Error(err)
	}
	return instantSnapshots, fmt.Errorf("Error converting Instant Snapshot Group to Standard snapshots, latest error: %w", err)
}

func (s *Snapshot) createDiskSnapshot(ctx context.Context, createSnapshot diskSnapshotFunc) (*compute.Operation, error) {
	log.CtxLogger(ctx).Infow("Creating disk snapshot", "sourcedisk", s.Disk, "sourcediskzone", s.DiskZone, "snapshotname", s.SnapshotName)

	snapshot := &compute.Snapshot{
		Description:      s.Description,
		Name:             s.SnapshotName,
		SnapshotType:     s.SnapshotType,
		StorageLocations: []string{s.StorageLocation},
		Labels:           s.parseLabels(),
	}

	return s.createBackup(ctx, snapshot, createSnapshot)
}

func (s *Snapshot) createBackup(ctx context.Context, snapshot *compute.Snapshot, createSnapshot diskSnapshotFunc) (*compute.Operation, error) {
	var op *compute.Operation
	var err error

	// In case customer is taking a snapshot from an encrypted disk, the snapshot created from it also
	// needs to be encrypted. For simplicity we support the use case in which disk encryption and
	// snapshot encryption key are the same.
	if s.DiskKeyFile != "" {
		s.oteLogger.LogUsageAction(usagemetrics.EncryptedDiskSnapshot)
		srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
		srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
		if err != nil {
			s.oteLogger.LogUsageError(usagemetrics.EncryptedDiskSnapshotFailure)
			return nil, err
		}
		snapshot.SourceDiskEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
		snapshot.SnapshotEncryptionKey = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
	}
	if s.computeService == nil {
		return nil, fmt.Errorf("computeService needed to proceed")
	}
	dbFreezeStartTime = time.Now()
	if s.FreezeFileSystem {
		if err := hanabackup.FreezeXFS(ctx, s.hanaDataPath, commandlineexecutor.ExecuteCommand); err != nil {
			return nil, err
		}
	}
	if op, err = createSnapshot(snapshot).Do(); err != nil {
		return nil, err
	}
	if err := s.gceService.WaitForSnapshotCreationCompletionWithRetry(ctx, op, s.Project, s.DiskZone, s.SnapshotName); err != nil {
		return nil, err
	}
	return op, nil
}

func (s *Snapshot) parseLabels() map[string]string {
	labels := s.createGroupBackupLabels()
	if s.Labels != "" {
		for _, label := range strings.Split(s.Labels, ",") {
			split := strings.Split(label, "=")
			if len(split) == 2 {
				labels[split[0]] = split[1]
			}
		}
	}
	return labels
}

// sendStatusToMonitoring sends the status of one time execution to cloud monitoring as a GAUGE metric.
func (s *Snapshot) sendStatusToMonitoring(ctx context.Context, bo *cloudmonitoring.BackOffIntervals, cp *ipb.CloudProperties) bool {
	if !s.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Optional: sending HANA disk snapshot status to cloud monitoring", "status", s.status)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildBool(timeseries.Params{
			CloudProp:  timeseries.ConvertCloudProperties(cp),
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

func (s *Snapshot) sendDurationToCloudMonitoring(ctx context.Context, mtype string, snapshotName string, dur time.Duration, bo *cloudmonitoring.BackOffIntervals, cp *ipb.CloudProperties) bool {
	if !s.SendToMonitoring {
		return false
	}
	log.CtxLogger(ctx).Infow("Optional: Sending HANA disk snapshot duration to cloud monitoring", "duration", dur)
	ts := []*mrpb.TimeSeries{
		timeseries.BuildFloat64(timeseries.Params{
			CloudProp:    timeseries.ConvertCloudProperties(cp),
			MetricType:   mtype,
			Timestamp:    tspb.Now(),
			Float64Value: dur.Seconds(),
			MetricLabels: map[string]string{
				"sid":         s.Sid,
				"disk":        s.Disk,
				"backup_name": snapshotName,
			},
		}),
	}
	if _, _, err := cloudmonitoring.SendTimeSeries(ctx, ts, s.timeSeriesCreator, bo, s.Project); err != nil {
		log.CtxLogger(ctx).Debugw("Error sending duration metric to cloud monitoring", "error", err.Error())
		return false
	}
	return true
}

// readConsistencyGroup reads the consistency group (CG) from the resource policies of the disk.
func (s *Snapshot) readConsistencyGroup(ctx context.Context, disk string) (string, error) {
	d, err := s.gceService.GetDisk(s.Project, s.DiskZone, disk)
	if err != nil {
		return "", err
	}
	if cgPath := cgPath(d.ResourcePolicies); cgPath != "" {
		log.CtxLogger(ctx).Infow("Found disk to conistency group mapping", "disk", s.Disk, "cg", cgPath)
		return cgPath, nil
	}
	return "", fmt.Errorf("failed to find consistency group for disk %v", disk)
}

// cgPath returns the name of the compute group (CG) from the resource policies.
func cgPath(policies []string) string {
	// Example policy: https://www.googleapis.com/compute/v1/projects/my-project/regions/my-region/resourcePolicies/my-cg
	for _, policyLink := range policies {
		parts := strings.Split(policyLink, "/")
		if len(parts) >= 10 && parts[9] == "resourcePolicies" {
			return parts[10]
		}
	}
	return ""
}

// createGroupBackupLabels returns the labels to be added for the group snapshot.
func (s *Snapshot) createGroupBackupLabels() map[string]string {
	labels := map[string]string{}
	if !s.groupSnapshot {
		if s.provisionedIops != 0 {
			labels["goog-sapagent-provisioned-iops"] = strconv.FormatInt(s.provisionedIops, 10)
		}
		if s.provisionedThroughput != 0 {
			labels["goog-sapagent-provisioned-throughput"] = strconv.FormatInt(s.provisionedThroughput, 10)
		}
		return labels
	}
	parts := strings.Split(s.DiskZone, "-")
	region := strings.Join(parts[:len(parts)-1], "-")

	labels["goog-sapagent-version"] = strings.ReplaceAll(configuration.AgentVersion, ".", "_")
	labels["goog-sapagent-isg"] = s.groupSnapshotName
	labels["goog-sapagent-cgpath"] = region + "-" + s.cgName
	labels["goog-sapagent-disk-name"] = s.Disk
	labels["goog-sapagent-timestamp"] = strconv.FormatInt(time.Now().UTC().Unix(), 10)
	labels["goog-sapagent-sha224"] = generateSHA(labels)
	return labels
}

// generateSHA generates a SHA-224 hash of labels starting with "goog-sapagent".
func generateSHA(labels map[string]string) string {
	keys := []string{}
	for k := range labels {
		if strings.HasPrefix(k, "goog-sapagent") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var orderedString string
	for _, k := range keys {
		orderedString += k + labels[k]
	}

	hash := sha256.Sum224([]byte(orderedString))
	return hex.EncodeToString(hash[:])
}

// Following functions are for testing purposes only.
func createStandardSnapshotName(instantSnapshotName string) string {
	maxLength := 63
	suffix := "-standard"

	standardSnapshotName := instantSnapshotName
	snapshotNameMaxLength := maxLength - len(suffix)
	if len(instantSnapshotName) > snapshotNameMaxLength {
		standardSnapshotName = instantSnapshotName[:snapshotNameMaxLength]
	}

	standardSnapshotName += suffix
	return standardSnapshotName
}

func (s *Snapshot) stagingCreateBackup(ctx context.Context, wg *sync.WaitGroup, jobs chan *instantsnapshotgroup.ISItem, errors chan error) {
	defer wg.Done()
	for instantSnapshot := range jobs {
		isName := instantSnapshot.Name
		standardSnapshotName := createStandardSnapshotName(isName)
		standardSnapshot := map[string]any{
			"name":                  standardSnapshotName,
			"sourceInstantSnapshot": fmt.Sprintf("projects/%s/zones/%s/instantSnapshots/%s", s.Project, s.DiskZone, isName),
			"labels":                s.parseLabels(),
			"description":           s.Description,
		}

		// In case customer is taking a snapshot from an encrypted disk, the snapshot created from it also
		// needs to be encrypted. For simplicity we support the use case in which disk encryption and
		// snapshot encryption key are the same.
		if s.DiskKeyFile != "" {
			usagemetrics.Action(usagemetrics.EncryptedDiskSnapshot)
			srcDiskURI := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, s.Disk)
			srcDiskKey, err := hanabackup.ReadKey(s.DiskKeyFile, srcDiskURI, os.ReadFile)
			if err != nil {
				errors <- fmt.Errorf("failed to create standard snapshot for instant snapshot %s: %w", isName, err)
				usagemetrics.Error(usagemetrics.EncryptedDiskSnapshotFailure)
				return
			}
			standardSnapshot["sourceDiskEncryptionKey"] = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
			standardSnapshot["snapshotEncryptionKey"] = &compute.CustomerEncryptionKey{RsaEncryptedKey: srcDiskKey}
		}

		baseURL := fmt.Sprintf("https://compute.googleapis.com/compute/staging_alpha/projects/%s/global/snapshots", s.Project)
		data, err := json.Marshal(standardSnapshot)
		if err != nil {
			errors <- fmt.Errorf("failed to create standard snapshot for instant snapshot %s: %w", isName, err)
			return
		}
		bodyBytes, err := s.isgService.GetResponse(ctx, "POST", baseURL, data)
		log.CtxLogger(ctx).Debugw("bodyBytes", "bodyBytes", string(bodyBytes))
		if err != nil {
			errors <- fmt.Errorf("failed to create standard snapshot for instant snapshot %s: %w", isName, err)
			return
		}

		baseURL = fmt.Sprintf("https://www.googleapis.com/compute/staging_alpha/projects/%s/global/snapshots/%s", s.Project, standardSnapshotName)
		if err := s.isgService.WaitForStandardSnapshotCreationWithRetry(ctx, baseURL); err != nil {
			errors <- fmt.Errorf("failed to create standard snapshot for instant snapshot %s: %w", isName, err)
			return
		}
	}
}

func (s *Snapshot) waitForStandardSnapshotsUpload(ctx context.Context, instantSnapshots []instantsnapshotgroup.ISItem) []error {
	var errors []error
	for _, is := range instantSnapshots {
		standardSnapshotName := createStandardSnapshotName(is.Name)
		baseURL := fmt.Sprintf("https://www.googleapis.com/compute/staging_alpha/projects/%s/global/snapshots/%s", s.Project, standardSnapshotName)
		if err := s.isgService.WaitForStandardSnapshotCompletionWithRetry(ctx, baseURL); err != nil {
			errors = append(errors, fmt.Errorf("failed to create standard snapshot for instant snapshot %s: %w", is.Name, err))
		}
	}
	return errors
}

// readConsistencyGroup reads the consistency group (CG) from the resource policies of the disk.
func (s *Snapshot) stagingReadConsistencyGroup(ctx context.Context, diskName string) (string, error) {
	baseURL := fmt.Sprintf("https://compute.googleapis.com/compute/staging_alpha/projects/%s/zones/%s/disks/%s", s.Project, s.DiskZone, diskName)
	bodyBytes, err := s.isgService.GetResponse(ctx, "GET", baseURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read consistency group of disk, err: %w", err)
	}

	var disk compute.Disk
	if err := json.Unmarshal([]byte(bodyBytes), &disk); err != nil {
		return "", fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	log.CtxLogger(ctx).Infow("Disk validating cg", "disk", disk)

	if cgPath := cgPath(disk.ResourcePolicies); cgPath != "" {
		log.CtxLogger(ctx).Infow("Found disk to conistency group mapping", "disk", s.Disk, "cg", cgPath)
		return cgPath, nil
	}
	return "", fmt.Errorf("failed to find consistency group for disk %v", disk)
}
