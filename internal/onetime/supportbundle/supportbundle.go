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

// Package supportbundle implements one time execution mode for
// supportbundle.
package supportbundle

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"flag"
	"cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/rest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"

	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	st "cloud.google.com/go/storage"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

type (
	// SupportBundle has args for support bundle collection one time mode.
	SupportBundle struct {
		Sid                    string                        `json:"sid"`
		InstanceNums           string                        `json:"instance-numbers"`
		instanceNumsAfterSplit []string                      `json:"-"`
		Hostname               string                        `json:"hostname"`
		PacemakerDiagnosis     bool                          `json:"pacemaker-diagnosis,string"`
		AgentLogsOnly          bool                          `json:"agent-logs-only,string"`
		ProcessMetrics         bool                          `json:"process-metrics"`
		Timestamp              string                        `json:"timestamp"`
		BeforeDuration         int                           `json:"before-duration"`
		AfterDuration          int                           `json:"after-duration"`
		Help                   bool                          `json:"help,string"`
		LogLevel               string                        `json:"loglevel"`
		ResultBucket           string                        `json:"result-bucket"`
		IIOTEParams            *onetime.InternallyInvokedOTE `json:"-"`
		LogPath                string                        `json:"log-path"`
		rest                   RestService
		createQueryClient      createQueryClient
		createMetricClient     createMetricClient
		oteLogger              *onetime.OTELogger
	}
	// zipperHelper is a testable struct for zipper.
	zipperHelper struct{}

	// uploader interface provides abstraction for ease of testing.
	uploader interface {
		Upload(ctx context.Context) (int64, error)
	}

	// getReaderWriter is a function to get the reader writer for uploading the file.
	getReaderWriter func(rw storage.ReadWriter) uploader

	// RestService is the interface for rest.Rest.
	RestService interface {
		NewRest()
		GetResponse(ctx context.Context, method string, baseURL string, data []byte) ([]byte, error)
	}
	// httpClient is the interface for http.Client.
	httpClient interface {
		Do(req *http.Request) (*http.Response, error)
	}

	// EntriesResponse is the response for listEntries which mirrors the proto.
	EntriesResponse struct {
		Entries       []Entry `json:"entries"`
		NextPageToken string  `json:"nextPageToken"`
	}

	// Entry is the entry for listEntries which mirrors the proto.
	Entry struct {
		InsertID         string      `json:"insertId"`
		JSONPayload      JSONPayload `json:"jsonPayload"`
		Resource         any         `json:"resource"`
		Timestamp        string      `json:"timestamp"`
		Severity         string      `json:"severity"`
		LogName          string      `json:"logName"`
		ReceiveTimestamp string      `json:"receiveTimestamp"`
	}

	// JSONPayload is the payload for the entry which mirrors the proto.
	JSONPayload struct {
		Type      string `json:"type"`
		Discovery string `json:"discovery"`
	}

	// TimeSeries is the time series data which mirrors the proto.
	TimeSeries struct {
		Metric    string            `json:"metric"`
		Labels    map[string]string `json:"labels"`
		Values    []string          `json:"value"`
		Timestamp string            `json:"timestamp"`
	}

	// createQueryClient is an abstracted function to create a query client.
	createQueryClient func(context.Context) (cloudmonitoring.TimeSeriesQuerier, error)

	// createMetricClient is an abstracted function to create a metric client.
	createMetricClient func(context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error)
)

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

const (
	destFilePathPrefix    = `/tmp/google-cloud-sap-agent/`
	linuxConfigFilePath   = `/etc/google-cloud-sap-agent/configuration.json`
	linuxLogFilesPath     = `/var/log/`
	agentOnetimeFilesPath = `/var/log/google-cloud-sap-agent/`
	systemDBErrorsFile    = `_SYSTEM_DB_BACKUP_ERROR.txt`
	journalCTLLogs        = `_JOURNAL_CTL_LOGS.txt`
	varLogMessagesFile    = `_VAR_LOG_MESSAGES.txt`
	hanaVersionFile       = `_HANA_VERSION.txt`
	tenantDBErrorsFile    = `_TENANT_DB_BACKUP_ERROR.txt`
	backintErrorsFile     = `_BACKINT_ERROR.txt`
	globalINIFile         = `/custom/config/global.ini`
	backintGCSPath        = `/opt/backint/backint-gcs`
	sapDiscoveryFile      = `sapdiscovery.json`
	metricPrefix          = "workload.googleapis.com"
)

var processMetricsList = []string{
	"sap/control/cpu/utilization",
	"sap/control/memory/utilization",
	"sap/hana/availability",
	"sap/hana/cpu/utilization",
	"sap/hana/ha/availability",
	"sap/hana/ha/replication",
	"sap/hana/iops/reads",
	"sap/hana/iops/writes",
	"sap/hana/memory/utilization",
	"sap/hana/query/state",
	"sap/hana/service",
}

// Name implements the subcommand interface for collecting support bundle report collection for support team.
func (*SupportBundle) Name() string {
	return "supportbundle"
}

// Synopsis implements the subcommand interface for support bundle report collection for support team.
func (*SupportBundle) Synopsis() string {
	return "collect support bundle of Agent for SAP for the support team"
}

// Usage implements the subcommand interface for support bundle report collection for support team.
func (*SupportBundle) Usage() string {
	return `Usage: supportbundle [-sid=<SAP System Identifier>] [-instance-numbers=<Instance numbers>]
	[-hostname=<Hostname>] [agent-logs-only=true|false] [-h] [-loglevel=<debug|info|warn|error>]
	[-result-bucket=<name of the result bucket where bundle zip is uploaded>] [-log-path=<log-path>]
	Example: supportbundle -sid="DEH" -instance-numbers="00 01 11" -hostname="sample_host"` + "\n"
}

// SetFlags implements the subcommand interface for support bundle report collection.
func (s *SupportBundle) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.Sid, "sid", "", "SAP System Identifier - required for collecting HANA traces")
	fs.StringVar(&s.InstanceNums, "instance-numbers", "", "Instance numbers - required for collecting HANA traces")
	fs.StringVar(&s.Hostname, "hostname", "", "Hostname - required for collecting HANA traces")
	fs.BoolVar(&s.PacemakerDiagnosis, "pacemaker-diagnosis", false, "Indicate if pacemaker support files are to be collected")
	fs.BoolVar(&s.AgentLogsOnly, "agent-logs-only", false, "Indicate if only agent logs are to be collected")
	fs.BoolVar(&s.ProcessMetrics, "process-metrics", false, "Indicate if process metrics are to be collected (experimental)")
	fs.StringVar(&s.Timestamp, "timestamp", "", "Timestamp to be used for collecting process metrics, format: YYYY-MM-DD HH:MM:SS(eg: 2024-11-11 12:34:56). If not provided, current timestamp will be used.")
	fs.IntVar(&s.BeforeDuration, "before-duration", 3600, "Before duration(in seconds) to be used for collecting process metrics, default value is 3600 seconds (1 hour)")
	fs.IntVar(&s.AfterDuration, "after-duration", 1800, "After duration(in seconds) to be used for collecting process metrics, default value is 1800 seconds (30 minutes)")
	fs.BoolVar(&s.Help, "h", false, "Displays help")
	fs.StringVar(&s.LogLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&s.ResultBucket, "result-bucket", "", "Name of the result bucket where bundle zip is uploaded")
	fs.StringVar(&s.LogPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/supportbundle.log")
}

func getReadWriter(rw storage.ReadWriter) uploader {
	return &rw
}

// Execute implements the subcommand interface for support bundle report collection.
func (s *SupportBundle) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     s.Name(),
		Help:     s.Help,
		LogLevel: s.LogLevel,
		LogPath:  s.LogPath,
		Fs:       f,
		IIOTE:    s.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, exitStatus = s.Run(ctx, onetime.CreateRunOptions(cp, false), commandlineexecutor.ExecuteCommand)
	return exitStatus
}

// Run executes the command and returns the message and exit status.
func (s *SupportBundle) Run(ctx context.Context, opts *onetime.RunOptions, exec commandlineexecutor.Execute) (string, subcommands.ExitStatus) {
	s.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	return s.supportBundleHandler(ctx, destFilePathPrefix, exec, filesystem.Helper{}, zipperHelper{}, opts.CloudProperties)
}

// CollectAgentSupport collects the agent support bundle on the local machine.
func CollectAgentSupport(ctx context.Context, f *flag.FlagSet, lp log.Parameters, cp *ipb.CloudProperties, ote string) subcommands.ExitStatus {
	s := &SupportBundle{
		IIOTEParams: &onetime.InternallyInvokedOTE{
			InvokedBy: ote,
			Lp:        lp,
			Cp:        cp,
		},
		AgentLogsOnly: true,
	}
	return s.Execute(ctx, f, lp, cp)
}

func (s *SupportBundle) supportBundleHandler(ctx context.Context, destFilePathPrefix string, exec commandlineexecutor.Execute, fs filesystem.FileSystem, z zipper.Zipper, cp *ipb.CloudProperties) (string, subcommands.ExitStatus) {
	if errs := s.validateParams(); len(errs) > 0 {
		errMessage := strings.Join(errs, ", ")
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Invalid params for collecting support bundle Report for Agent for SAP", errors.New(errMessage))
		return fmt.Sprintf("Invalid params for collecting support bundle Report for Agent for SAP: %s", errMessage), subcommands.ExitUsageError
	}
	s.Sid = strings.ToUpper(s.Sid)
	bundlename := fmt.Sprintf("supportbundle-%s-%s", s.Hostname, strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1))
	destFilesPath := fmt.Sprintf("%s%s", destFilePathPrefix, bundlename)
	if err := fs.MkdirAll(destFilesPath, 0777); err != nil {
		errMessage := "Error while making directory: " + destFilesPath
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		return errMessage, subcommands.ExitFailure
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting Support Bundle Report for Agent for SAP...")
	reqFilePaths := []string{linuxConfigFilePath}
	globalPath := fmt.Sprintf(`/usr/sap/%s/SYS/global/hdb`, s.Sid)

	hanaPaths := []string{}
	for _, inr := range s.instanceNumsAfterSplit {
		hanaPaths = append(hanaPaths, fmt.Sprintf(`/usr/sap/%s/HDB%s/%s`, s.Sid, inr, s.Hostname))
	}

	var failureMsgs []string
	if !s.AgentLogsOnly {
		if isError := s.extractSystemDBErrors(ctx, destFilesPath, s.Hostname, hanaPaths, exec, fs); isError {
			failureMsgs = append(failureMsgs, "Error while extracting system DB errors")
		}
		if isError := s.extractTenantDBErrors(ctx, destFilesPath, s.Sid, s.Hostname, hanaPaths, exec, fs); isError {
			failureMsgs = append(failureMsgs, "Error while extracting tenant DB errors")
		}
		if isError := s.extractBackintErrors(ctx, destFilesPath, globalPath, s.Hostname, exec, fs); isError {
			failureMsgs = append(failureMsgs, "Error while extracting backint errors")
		}
		if isError := s.extractJournalCTLLogs(ctx, destFilesPath, s.Hostname, exec, fs); isError {
			failureMsgs = append(failureMsgs, "Error while extracting journalctl logs")
		}
		if isError := s.copyVarLogMessagesToBundle(ctx, destFilesPath, s.Hostname, fs); isError {
			failureMsgs = append(failureMsgs, "Error while copying var log messages to bundle")
		}
		if isError := s.extractHANAVersion(ctx, destFilesPath, s.Sid, s.Hostname, exec, fs); isError {
			failureMsgs = append(failureMsgs, "Error while extracting HANA version")
		}
		if isError := s.fetchPackageInfo(ctx, destFilesPath, s.Hostname, exec, fs); isError != nil {
			failureMsgs = append(failureMsgs, "Error while fetching package info")
		}
		if isError := s.fetchOSProcesses(ctx, destFilesPath, s.Hostname, exec, fs); isError != nil {
			failureMsgs = append(failureMsgs, "Error while fetching OS processes")
		}
		if isError := s.fetchSystemDServices(ctx, destFilesPath, s.Hostname, exec, fs); isError != nil {
			failureMsgs = append(failureMsgs, "Error while fetching systemd services")
		}
		reqFilePaths = append(reqFilePaths, s.nameServerTracesAndBackupLogs(ctx, hanaPaths, s.Sid, fs)...)
		reqFilePaths = append(reqFilePaths, s.tenantDBNameServerTracesAndBackupLogs(ctx, hanaPaths, s.Sid, fs)...)
		reqFilePaths = append(reqFilePaths, s.backintParameterFiles(ctx, globalPath, s.Sid, fs)...)
		reqFilePaths = append(reqFilePaths, s.backintLogs(ctx, globalPath, s.Sid, fs)...)
	}
	reqFilePaths = append(reqFilePaths, s.agentLogFiles(ctx, linuxLogFilesPath, fs)...)
	reqFilePaths = append(reqFilePaths, s.agentOTELogFiles(ctx, agentOnetimeFilesPath, fs)...)

	for _, path := range reqFilePaths {
		s.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Copying file %s ...", path))
		if err := copyFile(path, destFilesPath+path, fs); err != nil {
			errMessage := "Error while copying file: " + path
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			failureMsgs = append(failureMsgs, errMessage)
		}
	}

	if !s.AgentLogsOnly {
		baseURL := "https://logging.googleapis.com/v2/entries:list"
		if err := s.collectSapDiscovery(ctx, baseURL, destFilesPath, cp, fs); err != nil {
			errMessage := "Error while collecting GCP Agent for SAP's Discovery data"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			failureMsgs = append(failureMsgs, errMessage)
		}
	}

	if s.ProcessMetrics {
		if errMsgs := s.collectProcessMetrics(ctx, processMetricsList, destFilesPath, cp, fs); len(errMsgs) > 0 {
			failureMsgs = append(failureMsgs, errMsgs...)
		}
	}

	var successMsgs []string
	zipfile := fmt.Sprintf("%s/%s.zip", destFilesPath, bundlename)
	if err := zipSource(destFilesPath, zipfile, fs, z); err != nil {
		errMessage := fmt.Sprintf("Error while zipping destination folder %s", destFilesPath)
		s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
		failureMsgs = append(failureMsgs, errMessage)
	} else {
		msg := fmt.Sprintf("Zipped destination support bundle file HANA/Backint at %s", zipfile)
		s.oteLogger.LogMessageToFileAndConsole(ctx, msg)
		successMsgs = append(successMsgs, msg)
	}

	if s.ResultBucket != "" {
		if err := s.uploadZip(ctx, zipfile, bundlename, storage.ConnectToBucket, getReadWriter, fs, st.NewClient); err != nil {
			errMessage := fmt.Sprintf("Error while uploading zip file %s to bucket %s", destFilePathPrefix+".zip", s.ResultBucket)
			s.oteLogger.LogMessageToConsole(fmt.Sprintf(errMessage, " Error: ", err))
			failureMsgs = append(failureMsgs, errMessage)
		} else {
			msg := fmt.Sprintf("Bundle uploaded to bucket %s", s.ResultBucket)
			successMsgs = append(successMsgs, msg)
			// removing the destination directory after zip file is created.
			if err := s.removeDestinationFolder(ctx, destFilesPath, fs); err != nil {
				errMessage := fmt.Sprintf("Error while removing destination folder %s", destFilesPath)
				s.oteLogger.LogMessageToConsole(fmt.Sprintf(errMessage, " Error: ", err))
				failureMsgs = append(failureMsgs, errMessage)
			}
		}
	}

	// Rotate out old support bundles so we don't fill the file system.
	if err := s.rotateOldBundles(ctx, destFilePathPrefix, fs); err != nil {
		errMessage := fmt.Sprintf("Error while rotating old support bundles: %s", err.Error())
		failureMsgs = append(failureMsgs, errMessage)
	}

	if s.PacemakerDiagnosis {
		// collect pacemaker reports using OS Specific commands
		pacemakerFilesDir := fmt.Sprintf("%spacemaker-%s", destFilePathPrefix, time.Now().UTC().String()[:16])
		pacemakerFilesDir = strings.ReplaceAll(pacemakerFilesDir, " ", "-")
		pacemakerFilesDir = strings.ReplaceAll(pacemakerFilesDir, ":", "-")
		err := s.pacemakerLogs(ctx, pacemakerFilesDir, exec, fs)
		if err != nil {
			errMessage := "Error while collecting pacemaker logs"
			s.oteLogger.LogErrorToFileAndConsole(ctx, errMessage, err)
			failureMsgs = append(failureMsgs, errMessage)
		} else {
			msg := fmt.Sprintf("Pacemaker logs are collected and sent to directory %s", pacemakerFilesDir)
			s.oteLogger.LogMessageToFileAndConsole(ctx, msg)
			successMsgs = append(successMsgs, msg)
		}
	}

	if len(failureMsgs) > 0 {
		return strings.Join(failureMsgs, ", "), subcommands.ExitFailure
	}
	return strings.Join(successMsgs, ", "), subcommands.ExitSuccess
}

// uploadZip uploads the zip file to the bucket provided.
func (s *SupportBundle) uploadZip(ctx context.Context, destFilesPath, bundleName string, ctb storage.BucketConnector, grw getReaderWriter, fs filesystem.FileSystem, client storage.Client) error {
	s.oteLogger.LogMessageToConsole(fmt.Sprintf("Uploading bundle %s to bucket %s", destFilesPath, s.ResultBucket))
	f, err := fs.Open(destFilesPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fileInfo, err := fs.Stat(destFilesPath)
	if err != nil {
		return err
	}

	connectParams := &storage.ConnectParameters{
		StorageClient:    client,
		BucketName:       s.ResultBucket,
		UserAgentSuffix:  "Support Bundle",
		VerifyConnection: true,
		UserAgent:        configuration.StorageAgentName(),
	}
	bucketHandle, ok := ctb(ctx, connectParams)
	if !ok {
		err := errors.New("error establishing connection to bucket, please check the logs")
		return err
	}

	objectName := fmt.Sprintf("%s/%s.zip", s.Name(), bundleName)
	fileSize := fileInfo.Size()
	readWriter := storage.ReadWriter{
		Reader:       f,
		Copier:       io.Copy,
		BucketHandle: bucketHandle,
		BucketName:   s.ResultBucket,
		ObjectName:   objectName,
		TotalBytes:   fileSize,
		VerifyUpload: true,
	}

	rw := grw(readWriter)
	var bytesWritten int64
	if bytesWritten, err = rw.Upload(ctx); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("File uploaded", "bucket", s.ResultBucket, "bytesWritten", bytesWritten, "fileSize", fileSize)
	s.oteLogger.LogMessageToConsole(fmt.Sprintf("Bundle uploaded to bucket %s", s.ResultBucket))
	return nil
}

func copyFile(src, dst string, fs filesystem.FileSystem) error {
	var err error
	var srcFD, dstFD *os.File
	var srcinfo os.FileInfo

	destFolder := path.Dir(dst)

	if err := fs.MkdirAll(destFolder, 0777); err != nil {
		return err
	}
	if srcFD, err = fs.Open(src); err != nil {
		return err
	}
	defer srcFD.Close()

	if dstFD, err = fs.Create(dst); err != nil {
		return err
	}
	defer dstFD.Close()

	if _, err = fs.Copy(dstFD, srcFD); err != nil {
		return err
	}
	if srcinfo, err = fs.Stat(src); err != nil {
		return err
	}
	return fs.Chmod(dst, srcinfo.Mode())
}

func zipSource(source, target string, fs filesystem.FileSystem, z zipper.Zipper) error {
	// Creating zip at a temporary location to prevent a blank zip file from
	// being created in the final bundle.
	tempZip := "/tmp/tmp-support-bundle.zip"
	f, err := fs.Create(tempZip)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := z.NewWriter(f)
	defer z.Close(writer)

	if err := fs.WalkAndZip(source, z, writer); err != nil {
		return err
	}
	return fs.Rename(tempZip, target)
}

func (s *SupportBundle) backintParameterFiles(ctx context.Context, globalPath string, sid string, fs filesystem.FileSystem) []string {
	backupFiles := regexp.MustCompile(`_backup_parameter_file.*`)
	res := []string{globalPath + globalINIFile}

	content, err := fs.ReadFile(globalPath + globalINIFile)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading file: "+globalPath+globalINIFile, err)
		return nil
	}
	contentData := string(content)
	op := backupFiles.FindAllString(contentData, -1)
	for _, path := range op {
		pathSplit := strings.Split(path, "=")
		if len(pathSplit) != 2 {
			s.oteLogger.LogMessageToFileAndConsole(ctx, "Unexpected output from global.ini content")
			continue
		}
		rfp := strings.TrimSpace(strings.Split(path, "=")[1])
		s.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", rfp))
		res = append(res, rfp)
	}
	return res
}

func (s *SupportBundle) nameServerTracesAndBackupLogs(ctx context.Context, hanaPaths []string, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	for _, hanaPath := range hanaPaths {
		fds, err := fs.ReadDir(fmt.Sprintf("%s/trace", hanaPath))
		if err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+hanaPath+"/trace", err)
			return nil
		}
		for _, fd := range fds {
			if fd.IsDir() {
				continue
			}
			if matchNameServerTraceAndBackup(fd.Name()) {
				s.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", path.Join(hanaPath+"/trace", fd.Name())))
				res = append(res, path.Join(hanaPath+"/trace/", fd.Name()))
			}
		}
	}
	return res
}

func (s *SupportBundle) tenantDBNameServerTracesAndBackupLogs(ctx context.Context, hanaPaths []string, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	for _, hanaPath := range hanaPaths {
		fds, err := fs.ReadDir(fmt.Sprintf("%s/trace/DB_%s", hanaPath, sid))
		if err != nil {
			s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+hanaPath+"/trace", err)
			return nil
		}
		for _, fd := range fds {
			if fd.IsDir() {
				continue
			}
			if matchNameServerTraceAndBackup(fd.Name()) {
				s.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", path.Join(fmt.Sprintf("%s/trace/DB_%s", hanaPath, sid), fd.Name())))
				res = append(res, path.Join(fmt.Sprintf("%s/trace/DB_%s", hanaPath, sid), fd.Name()))
			}
		}
	}
	return res
}

func matchNameServerTraceAndBackup(name string) bool {
	nameserverTrace := regexp.MustCompile(`nameserver.*[0-9]\.[0-9][0-9][0-9]\.trc`)
	nameserverTopologyJSON := regexp.MustCompile(`nameserver.*topology.*json`)
	indexServer := regexp.MustCompile(`indexserver.*[0-9]\.[0-9][0-9][0-9]\.trc`)
	backuplog := regexp.MustCompile(`backup(.*?).log`)
	backintlog := regexp.MustCompile(`backint(.*?).log`)

	if nameserverTrace.MatchString(name) || indexServer.MatchString(name) ||
		backuplog.MatchString(name) || backintlog.MatchString(name) ||
		nameserverTopologyJSON.MatchString(name) {
		return true
	}
	return false
}

// backintLogs returns the list of backint logs to be collected.
func (s *SupportBundle) backintLogs(ctx context.Context, globalPath, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fs.ReadDir(globalPath + backintGCSPath)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+globalPath+backintGCSPath, err)
		return nil
	}
	for _, fd := range fds {
		if fd.IsDir() {
			continue
		}
		switch fd.Name() {
		case "installation.log", "logs", "VERSION.txt", "logging.properties":
			res = append(res, path.Join(globalPath, backintGCSPath, fd.Name()))
		}
	}
	return res
}

// agentOTELogFiles returns the list of agent OTE log files to be collected.
func (s *SupportBundle) agentOTELogFiles(ctx context.Context, agentOTEFilesPath string, fu filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fu.ReadDir(agentOTEFilesPath)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+agentOTEFilesPath, err)
		return res
	}
	for _, fd := range fds {
		res = append(res, path.Join(agentOTEFilesPath, fd.Name()))
	}
	return res
}

func (s *SupportBundle) agentLogFiles(ctx context.Context, linuxLogFilesPath string, fu filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fu.ReadDir(linuxLogFilesPath)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+linuxLogFilesPath, err)
		return res
	}
	for _, fd := range fds {
		if fd.IsDir() {
			continue
		}
		if strings.Contains(fd.Name(), "google-cloud-sap-agent") {
			res = append(res, path.Join(linuxLogFilesPath, fd.Name()))
		}
	}
	return res
}

// extractJournalCTLLogs extracts the journalctl logs for google-cloud-sap-agent.
func (s *SupportBundle) extractJournalCTLLogs(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Extracting journal CTL logs...")
	var hasErrors bool
	p := commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: "-c 'journalctl | grep google-cloud-sap-agent'",
	}
	if err := s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, journalCTLLogs, fu); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while executing command: journalctl | grep google-cloud-sap-agent", err)
		hasErrors = true
	}
	return hasErrors
}

// copies /var/log/messages file to the support bundle. It returns true if there is an error.
func (s *SupportBundle) copyVarLogMessagesToBundle(ctx context.Context, destFilesPath, hostname string, fu filesystem.FileSystem) bool {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Copying /var/log/messages file to the support bundle...")
	logFile := fmt.Sprintf("%smessages", linuxLogFilesPath)
	srcFile, err := fu.Open(logFile)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while opening file: %s", logFile), err)
		return true
	}
	defer srcFile.Close()
	destFilePath := fmt.Sprintf("%s/%s%s", destFilesPath, hostname, varLogMessagesFile)
	destFile, err := fu.OpenFile(destFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while opening file: %s", destFilePath), err)
		return true
	}
	defer destFile.Close()
	if _, err := fu.Copy(destFile, srcFile); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while copying file: /var/log/messages", err)
		return true
	}
	return false
}

// extractSystemDBErrors extracts the errors from system DB backup logs.
func (s *SupportBundle) extractSystemDBErrors(ctx context.Context, destFilesPath, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Extracting errors from System DB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: fmt.Sprintf("-w ERROR %s/trace/backup.log", hanaPath),
		}
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Executing command: grep -w ERROR"+hanaPath+"/trace/backup.log")
		if err := s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, systemDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractTenantDBErrors extracts the errors from tenant DB backup logs.
func (s *SupportBundle) extractTenantDBErrors(ctx context.Context, destFilesPath, sid, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Extracting errors from TenantDB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		filePath := fmt.Sprintf("%s/trace/DB_%s/backup.log", hanaPath, sid)
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: "-w ERROR " + filePath,
		}
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Executing command: grep -w ERROR"+filePath)
		if err := s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, tenantDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractBackintErrors extracts the errors from backint logs.
func (s *SupportBundle) extractBackintErrors(ctx context.Context, destFilesPath, globalPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Extracting errors from Backint logs...")
	fds, err := fu.ReadDir(globalPath + backintGCSPath + "/logs")
	if err != nil {
		return true
	}
	var hasErrors bool
	for _, fd := range fds {
		logFilePath := fmt.Sprintf("%s%s/logs/%s", globalPath, backintGCSPath, fd.Name())
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: "-w SEVERE " + logFilePath,
		}
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Executing command: grep -w SEVERE"+logFilePath)
		if err := s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, backintErrorsFile, fu); err != nil {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractHANAVersion extracts the HANA version from the sap env.
func (s *SupportBundle) extractHANAVersion(ctx context.Context, destFilesPath, sid, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	cmd := "-c 'source /usr/sap/" + sid + "/home/.sapenv.sh && /usr/sap/" + sid + "/*/HDB version'"
	params := commandlineexecutor.Params{
		User:        fmt.Sprintf("%sadm", strings.ToLower(sid)),
		Executable:  "bash",
		ArgsToSplit: cmd,
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Executing command: bash -c 'HDB version'")
	err := s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, params, hanaVersionFile, fu)
	if err != nil {
		return true
	}
	return false
}

// execAndWriteToFile executes the command and writes the output to the file.
func (s *SupportBundle) execAndWriteToFile(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, params commandlineexecutor.Params, opFile string, fu filesystem.FileSystem) error {
	res := exec(ctx, params)
	if res.ExitCode != 0 && res.StdErr != "" {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while executing command", errors.New(res.StdErr))
		return errors.New(res.StdErr)
	}
	f, err := fu.OpenFile(destFilesPath+"/"+hostname+opFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while opening the file", err)
		return err
	}
	defer f.Close()
	if _, err := fu.WriteStringToFile(f, res.StdOut); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, "Error while writing to the file", err)
		return err
	}
	return nil
}

// pacemakerLogs collects the pacemaker logs.
func (s *SupportBundle) pacemakerLogs(ctx context.Context, destFilesPath string, exec commandlineexecutor.Execute, fs filesystem.FileSystem) error {
	rhelParams := commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "-qE rhel /etc/os-release",
	}
	if val := s.checkForLinuxOSType(ctx, exec, rhelParams); val {
		if err := s.rhelPacemakerLogs(ctx, exec, destFilesPath, fs); err != nil {
			return err
		}
		return nil
	}
	slesParams := commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "-qE SLES /etc/os-release",
	}
	if val := s.checkForLinuxOSType(ctx, exec, slesParams); val {
		if err := s.slesPacemakerLogs(ctx, exec, destFilesPath, fs); err != nil {
			return err
		}
		return nil
	}
	return errors.New("incompatible os type for collecting pacemaker logs")
}

// checkForLinuxOSType checks if the OS type is RHEL or SLES.
func (s *SupportBundle) checkForLinuxOSType(ctx context.Context, exec commandlineexecutor.Execute, p commandlineexecutor.Params) bool {
	res := exec(ctx, p)
	if res.ExitCode != 0 || res.StdErr != "" {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while executing command %s %s, returned exitCode: %d", p.Executable, p.ArgsToSplit, res.ExitCode), errors.New(res.StdErr))
		return false
	}
	return true
}

// slesPacemakerLogs collects the pacemaker logs for SLES OS type.
func (s *SupportBundle) slesPacemakerLogs(ctx context.Context, exec commandlineexecutor.Execute, destFilesPath string, fu filesystem.FileSystem) error {
	// time.Now().UTC() returns current time UTC format with milliseconds precision,
	// we only need it till first 16 characters to satisfy the hb_report and crm_report command
	to := time.Now().UTC().String()[:16]
	from := time.Now().UTC().AddDate(0, 0, -3).String()[:16]
	if err := fu.MkdirAll(destFilesPath, 0777); err != nil {
		return err
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting hb_report...")
	res := exec(ctx, commandlineexecutor.Params{
		Executable:  "hb_report",
		ArgsToSplit: fmt.Sprintf("-S -f %s -t %s %s", from[:10], to[:10], destFilesPath+"/report"),
		Timeout:     3600,
	})
	if res.ExitCode != 0 {
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting crm_report...")
		res := exec(ctx, commandlineexecutor.Params{
			Executable:  "crm_report",
			ArgsToSplit: fmt.Sprintf("-S -f %s -t %s %s", from[:10], to[:10], destFilesPath+"/report"),
			Timeout:     3600,
		})
		if res.ExitCode != 0 {
			return errors.New(res.StdErr)
		}
	}
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting supportconfig...")
	res = exec(ctx, commandlineexecutor.Params{
		Executable:  "supportconfig",
		ArgsToSplit: fmt.Sprintf("-bl -R %s", destFilesPath),
		Timeout:     3600,
	})
	if res.ExitCode != 0 {
		return errors.New(res.StdErr)
	}
	return nil
}

// rhelPacemakerLogs collects the pacemaker logs for RHEL OS type.
func (s *SupportBundle) rhelPacemakerLogs(ctx context.Context, exec commandlineexecutor.Execute, destFilesPath string, fu filesystem.FileSystem) error {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting sosreport...")
	p := commandlineexecutor.Params{
		Executable:  "sosreport",
		ArgsToSplit: fmt.Sprintf("--batch --tmp-dir %s", destFilesPath),
		Timeout:     3600,
	}
	if err := fu.MkdirAll(destFilesPath, 0777); err != nil {
		return err
	}
	res := exec(ctx, p)
	if res.ExitCode != 0 && res.StdErr != "" {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while executing command %s", p.Executable), errors.New(res.StdErr))
		// if sosreport is unsuccessful in collecting pacemaker data, we will fallback to crm_report
		from := time.Now().UTC().AddDate(0, 0, -3).String()[:16]
		to := time.Now().UTC().String()[:16]
		s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting crm_report...")
		crmRes := exec(ctx, commandlineexecutor.Params{
			Executable:  "crm_report",
			ArgsToSplit: fmt.Sprintf("-S -f '%s' -t '%s' --dest %s", from, to, destFilesPath+"/report"),
			Timeout:     3600,
		})
		if crmRes.ExitCode != 0 {
			return errors.New(crmRes.StdErr)
		}
	}
	return nil
}

// fetchPackageInfo collects the information about various packages installed on the VM.
func (s *SupportBundle) fetchPackageInfo(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) error {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting package info...")
	p := commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: "rpm -qa",
	}
	return s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, "packages.txt", fu)
}

func (s *SupportBundle) fetchOSProcesses(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) error {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting OS processes...")
	p := commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: "ps aux",
	}
	return s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, "processes.txt", fu)
}

func (s *SupportBundle) fetchSystemDServices(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) error {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting systemd services...")
	p := commandlineexecutor.Params{
		Executable:  "sudo",
		ArgsToSplit: "systemctl list-units --type=service",
	}
	return s.execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, "systemd_services.txt", fu)
}

// collectSapDiscovery collects the SAP Discovery logs from cloud logging.
func (s *SupportBundle) collectSapDiscovery(ctx context.Context, baseURL, destFilePathPrefix string, cp *ipb.CloudProperties, fs filesystem.FileSystem) (err error) {
	// Read from cloud logging
	logName := fmt.Sprintf("logName=\"projects/%s/logs/google-cloud-sap-agent\"", cp.GetProjectId())
	resourceFilter := fmt.Sprintf("resource.type=\"gce_instance\" AND resource.labels.instance_id=\"%s\"", cp.GetInstanceId())
	sapDiscoverFilter := "jsonPayload.type=\"SapDiscovery\""
	timestampFilter := fmt.Sprintf("timestamp>=\"%s\"", time.Now().Add(-24*time.Hour).Format(time.RFC3339))

	filter := fmt.Sprintf("%s AND %s AND %s AND %s", logName, resourceFilter, sapDiscoverFilter, timestampFilter)
	log.CtxLogger(ctx).Infof("Filter: %s", filter)

	discovery, err := s.queryDiscovery(ctx, baseURL, filter, cp.GetProjectId())
	if err != nil {
		return err
	}

	f, err := fs.Create(fmt.Sprintf("%s/%s", destFilePathPrefix, sapDiscoveryFile))
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while creating file", "err", err)
		return err
	}
	defer f.Close()

	_, err = f.Write([]byte(discovery))
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while writing to file", "err", err)
		return err
	}
	return nil
}

// queryDiscovery queries the discovery logs from cloud logging.
func (s *SupportBundle) queryDiscovery(ctx context.Context, baseURL, filter, project string) (string, error) {
	request := struct {
		ResourceNames []string `json:"resourceNames"`
		Filter        string   `json:"filter"`
		OrderBy       string   `json:"orderBy"`
		PageSize      int      `json:"pageSize"`
	}{
		ResourceNames: []string{fmt.Sprintf("projects/%s", project)},
		Filter:        filter,
		OrderBy:       "timestamp desc",
		PageSize:      1,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while marshaling JSON", "err", err)
		return "", err
	}
	data := []byte(string(jsonData))

	bodyBytes, err := s.rest.GetResponse(ctx, "POST", baseURL, data)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while getting response", "err", err)
		return "", err
	}

	var entriesResponse EntriesResponse
	err = json.Unmarshal(bodyBytes, &entriesResponse)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error while unmarshaling JSON", "err", err)
		return "", err
	}

	var entry Entry
	if len(entriesResponse.Entries) == 0 {
		s.oteLogger.LogMessageToFileAndConsole(ctx, "No entries found, could not discover SAP Landscape configuration")
		return "", fmt.Errorf("no entries found, could not discover SAP Landscape configuration")
	}
	entry = entriesResponse.Entries[0]
	discovery := entry.JSONPayload.Discovery

	return discovery, nil
}

// collectProcessMetrics collects and writes the process metrics from cloud monitoring for a given time interval.
func (s *SupportBundle) collectProcessMetrics(ctx context.Context, metrics []string, destFilesPath string, cp *ipb.CloudProperties, fs filesystem.FileSystem) []string {
	s.oteLogger.LogMessageToFileAndConsole(ctx, "Collecting process metrics...")
	var errMsgs []string
	cmr, mmc, err := s.getProcessMetricsClients(ctx)
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("Failed to create Cloud Monitoring clients: %v", err))
		return errMsgs
	}

	pmFolderPath := path.Join(destFilesPath, "process_metrics")
	if err := fs.MkdirAll(pmFolderPath, 0777); err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("Failed to create process metrics folder: %v", err))
		return errMsgs
	}

	for _, metric := range metrics {
		var timeSeries []TimeSeries
		metricFileName := strings.ReplaceAll(metric, "/", "_")
		metric = path.Join(metricPrefix, metric)
		ts, err := s.fetchTimeSeriesData(ctx, cp, cmr, mmc, metric)
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("Failed to fetch time series data for metric %s: %v", metric, err))
			continue
		}
		timeSeries = append(timeSeries, ts...)

		f, err := fs.Create(fmt.Sprintf("%s/%s.json", pmFolderPath, metricFileName))
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("Error while creating file: %v", err))
			continue
		}
		defer f.Close()

		jsonData, err := json.MarshalIndent(timeSeries, "", "  ")
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("Error while marshaling JSON: %v", err))
			continue
		}
		_, err = f.Write(jsonData)
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("Error while writing to file: %v", err))
			continue
		}
	}

	return errMsgs
}

// getProcessMetricsClients returns the clients for reading process metrics.
func (s *SupportBundle) getProcessMetricsClients(ctx context.Context) (*cloudmetricreader.CloudMetricReader, cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
	qc, err := s.createQueryClient(ctx)
	if err != nil {
		usagemetrics.Error(usagemetrics.QueryClientCreateFailure)
		return nil, nil, err
	}
	cmr := &cloudmetricreader.CloudMetricReader{
		QueryClient: qc,
		BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
	}

	mmc, err := s.createMetricClient(ctx)
	if err != nil {
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return nil, nil, err
	}

	return cmr, mmc, nil
}

// fetchTimeSeriesData fetches the time series data for a given metric.
func (s *SupportBundle) fetchTimeSeriesData(ctx context.Context, cp *ipb.CloudProperties, cmr *cloudmetricreader.CloudMetricReader, mmc cloudmonitoring.TimeSeriesDescriptorQuerier, metric string) ([]TimeSeries, error) {
	labelNames, err := s.fetchLabelDescriptors(ctx, cp, mmc, metric, "gce_instance")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch label descriptors for metric %s: %v", metric, err)
	}

	t, err := time.Parse("2006-01-02 15:04:05", s.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp %s: %v", s.Timestamp, err)
	}
	endingTime := t.Add(time.Duration(s.AfterDuration) * time.Second)
	totalDurationInMinutes := (s.AfterDuration + s.BeforeDuration) / 60

	endingTimeStamp := strings.ReplaceAll(endingTime.Format("2006-01-02 15:04"), "-", "/")
	query := fmt.Sprintf("fetch gce_instance | metric '%s' | filter (resource.instance_id = '%s') | within %dm, d'%s'", metric, cp.GetInstanceId(), totalDurationInMinutes, endingTimeStamp)
	// TODO: - Fix deprecated mpb.QueryTimeSeriesRequest.
	req := &mpb.QueryTimeSeriesRequest{
		Name:  fmt.Sprintf("projects/%s", cp.GetProjectId()),
		Query: query,
	}
	data, err := cloudmonitoring.QueryTimeSeriesWithRetry(ctx, cmr.QueryClient, req, cmr.BackOffs)
	if err != nil {
		return nil, err
	}

	var timeSeries []TimeSeries
	for _, d := range data {
		labels := make(map[string]string)
		itr := 0
		for _, lv := range d.GetLabelValues() {
			if labelValue, err := getLabelValue(ctx, lv); err == nil {
				labels[labelNames[itr]] = labelValue
			}
			itr++
		}

		var values []string
		var timestamp string
		for _, p := range d.GetPointData() {
			for _, v := range p.GetValues() {
				if pv, err := getPointValue(ctx, v); err == nil {
					values = append(values, pv)
				}
			}
			unixTimestamp := int64(p.GetTimeInterval().GetEndTime().GetSeconds())
			t := time.Unix(unixTimestamp, 0)
			timestamp = t.Format("2006-01-02 15:04:05")
			timeSeries = append(timeSeries, TimeSeries{
				Metric:    metric,
				Labels:    labels,
				Values:    values,
				Timestamp: timestamp,
			})
		}
	}

	return timeSeries, nil
}

// getLabelValue returns the string representation of the label value.
func getLabelValue(ctx context.Context, lv *mrpb.LabelValue) (string, error) {
	switch lv.Value.(type) {
	case *mrpb.LabelValue_StringValue:
		return lv.GetStringValue(), nil
	case *mrpb.LabelValue_Int64Value:
		return fmt.Sprintf("%d", lv.GetInt64Value()), nil
	case *mrpb.LabelValue_BoolValue:
		return fmt.Sprintf("%t", lv.GetBoolValue()), nil
	default:
		return "", fmt.Errorf("unsupported label value type: %T", lv.Value)
	}
}

// getPointValue returns the string representation of the point value.
func getPointValue(ctx context.Context, v *cpb.TypedValue) (string, error) {
	switch tv := v.Value.(type) {
	case *cpb.TypedValue_Int64Value:
		return fmt.Sprintf("%d", tv.Int64Value), nil
	case *cpb.TypedValue_StringValue:
		return tv.StringValue, nil
	case *cpb.TypedValue_BoolValue:
		return fmt.Sprintf("%t", tv.BoolValue), nil
	case *cpb.TypedValue_DoubleValue:
		return fmt.Sprintf("%f", tv.DoubleValue), nil
	default:
		return "", fmt.Errorf("unsupported value type: %T", tv)
	}
}

// fetchLabelDescriptors fetches the label descriptors for the given metric and resource type.
func (s *SupportBundle) fetchLabelDescriptors(ctx context.Context, cp *ipb.CloudProperties, mmc cloudmonitoring.TimeSeriesDescriptorQuerier, metricType, resourceType string) ([]string, error) {
	var labels []string
	rdReq := &mpb.GetMonitoredResourceDescriptorRequest{
		Name: fmt.Sprintf("projects/%s/monitoredResourceDescriptors/%s", cp.GetProjectId(), resourceType),
	}
	rd, err := mmc.GetMonitoredResourceDescriptor(ctx, rdReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitored resource descriptor for %s: %v", resourceType, err)
	}
	for _, l := range rd.GetLabels() {
		labels = append(labels, l.GetKey())
	}

	mdReq := &mpb.GetMetricDescriptorRequest{
		Name: fmt.Sprintf("projects/%s/metricDescriptors/%s", cp.GetProjectId(), metricType),
	}
	md, err := mmc.GetMetricDescriptor(ctx, mdReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric descriptor for %s: %v", metricType, err)
	}
	for _, l := range md.GetLabels() {
		labels = append(labels, l.GetKey())
	}

	log.CtxLogger(ctx).Infof("Labels for metric %s: %v", labels, metricType)
	return labels, nil
}

// newQueryClient abstracts the creation of a new Cloud Monitoring query client for testing purposes.
func newQueryClient(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
	mqc, err := monitoring.NewQueryClient(ctx)
	if err != nil {
		usagemetrics.Error(usagemetrics.QueryClientCreateFailure)
		return nil, fmt.Errorf("failed to create Cloud Monitoring query client: %v", err)
	}

	return &cloudmetricreader.QueryClient{Client: mqc}, nil
}

// newMetricClient abstracts the creation of a new Cloud Monitoring metric client for testing purposes.
func newMetricClient(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
	mmc, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		usagemetrics.Error(usagemetrics.MetricClientCreateFailure)
		return nil, fmt.Errorf("failed to create Cloud Monitoring metric client: %v", err)
	}

	return mmc, nil
}

func (s *SupportBundle) validateParams() []string {
	var errs []string
	if s.AgentLogsOnly {
		return errs
	}
	if s.Sid == "" {
		errs = append(errs, "no value provided for sid")
	}
	if s.InstanceNums == "" {
		errs = append(errs, "no value provided for instance-numbers")
	} else {
		s.instanceNumsAfterSplit = strings.Split(s.InstanceNums, " ")
		for _, nos := range s.instanceNumsAfterSplit {
			if len(nos) != 2 {
				errs = append(errs, fmt.Sprintf("invalid instance number %s", nos))
			}
		}
	}
	if s.Hostname == "" {
		errs = append(errs, "no value provided for hostname")
	}
	if s.ProcessMetrics && s.Timestamp == "" {
		s.Timestamp = time.Now().Format("2006-01-02 15:04:05")
	}

	s.rest = &rest.Rest{}
	s.rest.NewRest()

	s.createQueryClient = newQueryClient
	s.createMetricClient = newMetricClient

	return errs
}

func (s *SupportBundle) removeDestinationFolder(ctx context.Context, path string, fu filesystem.FileSystem) error {
	if err := fu.RemoveAll(path); err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while removing folder %s", path), err)
		return err
	}
	return nil
}

func (s *SupportBundle) rotateOldBundles(ctx context.Context, dir string, fs filesystem.FileSystem) error {
	fds, err := fs.ReadDir(dir)
	if err != nil {
		s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while reading folder %s", dir), err)
		return err
	}
	sort.Slice(fds, func(i, j int) bool { return fds[i].ModTime().After(fds[j].ModTime()) })
	bundleCount := 0
	for _, fd := range fds {
		if strings.Contains(fd.Name(), "supportbundle") {
			bundleCount++
			if bundleCount > 5 {
				s.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Removing old bundle %s", dir+fd.Name()))
				if err := fs.RemoveAll(dir + fd.Name()); err != nil {
					s.oteLogger.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while removing old bundle %s", dir+fd.Name()), err)
					return err
				}
			}
		}
	}
	return nil
}
