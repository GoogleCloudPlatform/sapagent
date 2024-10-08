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
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"flag"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"github.com/google/subcommands"
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
	// checkDataDirFunc provides testable replacement for hanabackup.CheckDataDir
	checkDataDirFunc func(ctx context.Context, exec commandlineexecutor.Execute) (dataPath string, logicalDataPath string, physicalDataPath string, err error)

	// queryFunc provides testable replacement to the SQL API.
	queryFunc func(context.Context, *databaseconnector.DBHandle, string) (string, error)

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
		ListSnapshots(ctx context.Context, project string) (*compute.SnapshotList, error)

		DiskAttachedToInstance(projectID, zone, instanceName, diskName string) (string, bool, error)
		WaitForSnapshotCreationCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error
		WaitForSnapshotUploadCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error
		WaitForInstantSnapshotConversionCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error
		CreateSnapshot(ctx context.Context, project string, snapshotReq *compute.Snapshot) (*compute.Operation, error)
	}

	// ISGInterface is the testable equivalent for ISGService for ISG operations.
	ISGInterface interface {
		NewService() error
		CreateISG(ctx context.Context, project, zone string, data []byte) error
		ListInstantSnapshotGroups(ctx context.Context, project, zone string) ([]instantsnapshotgroup.ISGItem, error)
		DescribeInstantSnapshots(ctx context.Context, project, zone, isgName string) ([]instantsnapshotgroup.ISItem, error)
		DeleteISG(ctx context.Context, project, zone, isgName string) error
		WaitForISGUploadCompletionWithRetry(ctx context.Context, baseURL string) error
	}

	snapshotOp struct {
		op   *compute.Operation
		name string
	}
)

const (
	metricPrefix = "workload.googleapis.com/sap/agent/"
)

var (
	dbFreezeStartTime, workflowStartTime time.Time
)

// ISG is a placeholder struct defining fields potentially required
// for lifecycle management of Instant Snapshot Groups.
// It is currently placed here, but ideally it would reside within the compute package.
type ISG struct {
	Disks         []*compute.AttachedDisk
	EncryptionKey *compute.CustomerEncryptionKey
}

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
}

// Name implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Name() string { return "hanadiskbackup" }

// Synopsis implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Synopsis() string { return "invoke HANA backup using disk snapshots" }

// Usage implements the subcommand interface for hanadiskbackup.
func (*Snapshot) Usage() string {
	return `Usage: hanadiskbackup -port=<port-number> -sid=<HANA-sid> -hana-db-user=<HANA DB User>
	[-source-disk=<disk-name>] [-source-disk-zone=<disk-zone>] [-host=<hostname>]
	[-project=<project-name>] [-password=<passwd> | -password-secret=<secret-name>]
	[-hdbuserstore-key=<userstore-key>] [-abandon-prepared=<true|false>]
	[-send-metrics-to-monitoring]=<true|false>] [-source-disk-key-file=<path-to-key-file>]
	[-storage-location=<storage-location>] [-snapshot-description=<description>]
	[-snapshot-name=<snapshot-name>] [-snapshot-type=<snapshot-type>] [-group-snapshot-name=<group-snapshot-name>]
	[-freeze-file-system=<true|false>] [-labels="label1=value1,label2=value2"]
	[-confirm-data-snapshot-after-create=<true|false>]
	[-instance-id=<instance-id>]
	[-h] [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>]

	Authentication Flag Combinations:
	1. -hana-db-user=<HANA DB User> [-password=<passwd> | -password-secret=<secret-name>] -host=<hostname> -port=<port-number>
	2. -hdbuserstore-key=<userstore-key>

	For single disk backup:
	hanadiskbackup -sid=<HANA SID> [Authentication Flags] -snapshot-name=<snapshot-name> -source-disk=<disk-name> -source-disk-zone=<disk-zone> -source-disk-key-file=<path-to-key-file>

	For multi-disk backup:
	hanadiskbackup -sid=<HANA SID> [Authentication Flags] -group-snapshot-name=<group-snapshot-name> -snapshot-type=<snapshot-type>
	` + "\n"
}

// SetFlags implements the subcommand interface for hanadiskbackup.
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
	fs.StringVar(&s.Host, "host", "localhost", "HANA host. (optional) Default: localhost")
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
	fs.StringVar(&s.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/hanadiskbackup.log")
	fs.BoolVar(&s.help, "h", false, "Displays help")
	fs.StringVar(&s.LogLevel, "loglevel", "info", "Sets the logging level")
	fs.StringVar(&s.Labels, "labels", "", "Labels to be added to the disk snapshot")
	fs.StringVar(&s.groupSnapshotName, "group-snapshot-name", "", "Group Snapshot name override.(optional - defaults to '<consistency-group-name>-yyyymmdd-hhmmss'.)")
}

// Execute implements the subcommand interface for hanadiskbackup.
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

	message, exitStatus := s.snapshotHandler(ctx, gce.NewGCEClient, onetime.NewComputeService, hanabackup.CheckDataDir, opts.CloudProperties)
	if exitStatus != subcommands.ExitSuccess {
		return message, subcommands.ExitFailure
	}
	return message, subcommands.ExitSuccess
}

func (s *Snapshot) snapshotHandler(ctx context.Context, gceServiceCreator onetime.GCEServiceFunc, computeServiceCreator onetime.ComputeServiceFunc, checkDataDir checkDataDirFunc, cp *ipb.CloudProperties) (string, subcommands.ExitStatus) {
	var err error
	s.status = false

	defer s.sendStatusToMonitoring(ctx, cloudmonitoring.NewDefaultBackOffIntervals(), cp)

	s.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		errMessage := "ERROR: Failed to create GCE service"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	if s.hanaDataPath, s.logicalDataPath, s.physicalDataPath, err = checkDataDir(ctx, commandlineexecutor.ExecuteCommand); err != nil {
		errMessage := "ERROR: Failed to check preconditions"
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}

	if s.Disk == "" {
		log.CtxLogger(ctx).Info("Reading disk mapping for /hana/data/")
		if err := s.readDiskMapping(ctx, cp); err != nil {
			errMessage := "ERROR: Failed to read disk mapping"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			return errMessage, subcommands.ExitFailure
		}

		if len(s.disks) > 1 {
			s.oteLogger.LogUsageAction(usagemetrics.HANADiskGroupBackupStarted)
			if ok, err := hanabackup.CheckDataDeviceForStripes(ctx, s.logicalDataPath, commandlineexecutor.ExecuteCommand); err != nil {
				errMessage := "ERROR: Failed to check if data device is striped"
				s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
				return errMessage, subcommands.ExitFailure
			} else if !ok {
				errMessage := "ERROR: Multiple disks are backing up /hana/data but data device is not striped"
				s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
				return errMessage, subcommands.ExitFailure
			}
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
		log.CtxLogger(ctx).Infow("Successfully read disk mapping for /hana/data/", "disks", s.disks, "cgPath", s.cgName, "groupSnapshot", s.groupSnapshot)
	}

	if s.groupSnapshotName != "" {
		snapshotList, err := s.gceService.ListSnapshots(ctx, s.Project)
		if err != nil {
			errMessage := "ERROR: Failed to check if group snapshot exists"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			return errMessage, subcommands.ExitFailure
		}

		for _, snapshot := range snapshotList.Items {
			if snapshot.Labels["goog-sapagent-isg"] == s.groupSnapshotName {
				errMessage := "ERROR: Group snapshot with given name already exists"
				s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, fmt.Errorf("group snapshot with given name already exists"))
				return errMessage, subcommands.ExitFailure
			}
		}
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
		if err := s.runWorkflowForInstantSnapshotGroups(ctx, runQuery, cp); err != nil {
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
	var successMessage string
	if s.groupSnapshot {
		snapshotName = s.groupSnapshotName
		successMessage = fmt.Sprintf("SUCCESS: HANA backup and group disk snapshot creation successful. Group Backup Name: %s", snapshotName)
		s.oteLogger.LogMessageToConsole(successMessage)
		s.oteLogger.LogUsageAction(usagemetrics.HANADiskGroupBackupSucceeded)
	} else {
		successMessage = fmt.Sprintf("SUCCESS: HANA backup and disk snapshot creation successful. Snapshot Name: %s", snapshotName)
		s.oteLogger.LogMessageToConsole(successMessage)
		s.oteLogger.LogUsageAction(usagemetrics.HANADiskBackupSucceeded)
	}

	s.sendDurationToCloudMonitoring(ctx, metricPrefix+s.Name()+"/totaltime", snapshotName, workflowDur, cloudmonitoring.NewDefaultBackOffIntervals(), cp)
	s.status = true
	return successMessage, subcommands.ExitSuccess
}

func (s *Snapshot) readDiskMapping(ctx context.Context, cp *ipb.CloudProperties) error {
	var err error

	instanceInfoReader := instanceinfo.New(&instanceinfo.PhysicalPathReader{OS: runtime.GOOS}, s.gceService)
	if _, s.instanceProperties, err = instanceInfoReader.ReadDiskMapping(ctx, &cpb.Configuration{CloudProperties: cp}); err != nil {
		return err
	}

	log.CtxLogger(ctx).Debugw("Reading disk mapping", "ip", s.instanceProperties)
	for _, d := range s.instanceProperties.GetDisks() {
		if strings.Contains(s.physicalDataPath, d.GetMapping()) {
			log.CtxLogger(ctx).Debugw("Found disk mapping", "physicalPath", s.physicalDataPath, "diskName", d.GetDiskName())
			s.Disk = d.GetDiskName()
			s.DiskZone = cp.GetZone()
			s.disks = append(s.disks, d.GetDiskName())
			s.provisionedIops = d.GetProvisionedIops()
			s.provisionedThroughput = d.GetProvisionedThroughput()
		}
	}

	if s.SnapshotName == "" {
		t := time.Now()
		log.CtxLogger(ctx).Debug("disk: ", s.Disk)
		s.SnapshotName = fmt.Sprintf("snapshot-%s-%d%02d%02d-%02d%02d%02d",
			s.Disk, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
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

	if s.SnapshotType != "STANDARD" && s.SnapshotType != "ARCHIVE" {
		return fmt.Errorf("invalid snapshot type, only STANDARD and ARCHIVE are supported")
	}
	if s.Project == "" {
		s.Project = cp.GetProjectId()
	}
	if s.DiskZone == "" {
		s.DiskZone = cp.GetZone()
	}
	if s.Description == "" {
		s.Description = fmt.Sprintf("Snapshot created by Agent for SAP for HANA sid: %q", s.Sid)
	}
	s.Port = s.portValue()

	if s.SnapshotName == "" && s.Disk != "" {
		t := time.Now()
		s.SnapshotName = fmt.Sprintf("snapshot-%s-%d%02d%02d-%02d%02d%02d",
			s.Disk, t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	}
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

func (s *Snapshot) diskSnapshotFailureHandler(ctx context.Context, run queryFunc, snapshotID string) {
	s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotCreateFailure)
	if err := s.abandonHANASnapshot(ctx, run, snapshotID); err != nil {
		log.CtxLogger(ctx).Errorw("Error discarding HANA snapshot")
		s.oteLogger.LogUsageError(usagemetrics.DiskSnapshotFailedDBNotComplete)
	}
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
