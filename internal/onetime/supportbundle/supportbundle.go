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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"flag"
	st "cloud.google.com/go/storage"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (
	// SupportBundle has args for support bundle collection one time mode.
	SupportBundle struct {
		sid                               string
		instanceNums                      string
		instanceNumsAfterSplit            []string
		hostname                          string
		pacemakerDiagnosis, agentLogsOnly bool
		help, version                     bool
		logLevel                          string
		resultBucket                      string
		IIOTEParams                       *onetime.InternallyInvokedOTE
	}
	zipperHelper struct{}

	// uploader interface provides abstraction for ease of testing.
	uploader interface {
		Upload(ctx context.Context) (int64, error)
	}

	// getReaderWriter is a function to get the reader writer for uploading the file.
	getReaderWriter func(rw storage.ReadWriter) uploader
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
	hanaVersionFile       = `_HANA_VERSION.txt`
	tenantDBErrorsFile    = `_TENANT_DB_BACKUP_ERROR.txt`
	backintErrorsFile     = `_BACKINT_ERROR.txt`
	globalINIFile         = `/custom/config/global.ini`
	backintGCSPath        = `/opt/backint/backint-gcs`
)

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
	[-hostname=<Hostname>] [agent-logs-only=true|false] [-h] [-v] [-loglevel=<debug|info|warn|error>]
	[-result-bucket=<name of the result bucket where bundle zip is uploaded>]
	Example: supportbundle -sid="DEH" -instance-numbers="00 01 11" -hostname="sample_host"` + "\n"
}

// SetFlags implements the subcommand interface for support bundle report collection.
func (s *SupportBundle) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.sid, "sid", "", "SAP System Identifier - required for collecting HANA traces")
	fs.StringVar(&s.instanceNums, "instance-numbers", "", "Instance numbers - required for collecting HANA traces")
	fs.StringVar(&s.hostname, "hostname", "", "Hostname - required for collecting HANA traces")
	fs.BoolVar(&s.pacemakerDiagnosis, "pacemaker-diagnosis", false, "Indicate if pacemaker support files are to be collected")
	fs.BoolVar(&s.agentLogsOnly, "agent-logs-only", false, "Indicate if only agent logs are to be collected")
	fs.BoolVar(&s.help, "h", false, "Displays help")
	fs.BoolVar(&s.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&s.logLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&s.resultBucket, "result-bucket", "", "Name of the result bucket where bundle zip is uploaded")
}

func getReadWriter(rw storage.ReadWriter) uploader {
	return &rw
}

// Execute implements the subcommand interface for support bundle report collection.
func (s *SupportBundle) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:     s.Name(),
		Help:     s.help,
		Version:  s.version,
		LogLevel: s.logLevel,
		Fs:       f,
		IIOTE:    s.IIOTEParams,
	}, args...)
	if !completed {
		return exitStatus
	}

	return s.supportBundleHandler(ctx, destFilePathPrefix, commandlineexecutor.ExecuteCommand, filesystem.Helper{}, zipperHelper{})
}

// CollectAgentSupport collects the agent support bundle on the local machine.
func CollectAgentSupport(ctx context.Context, f *flag.FlagSet, lp log.Parameters, cp *ipb.CloudProperties, ote string) subcommands.ExitStatus {
	s := &SupportBundle{
		IIOTEParams: &onetime.InternallyInvokedOTE{
			InvokedBy: ote,
			Lp:        lp,
			Cp:        cp,
		},
		agentLogsOnly: true,
	}
	return s.Execute(ctx, f, lp, cp)
}

func (s *SupportBundle) supportBundleHandler(ctx context.Context, destFilePathPrefix string, exec commandlineexecutor.Execute, fs filesystem.FileSystem, z zipper.Zipper) subcommands.ExitStatus {
	if errs := s.validateParams(); len(errs) > 0 {
		errMessage := strings.Join(errs, ", ")
		onetime.LogErrorToFileAndConsole(ctx, "Invalid params for collecting support bundle Report for Agent for SAP", errors.New(errMessage))
		return subcommands.ExitUsageError
	}
	s.sid = strings.ToUpper(s.sid)
	bundlename := fmt.Sprintf("supportbundle-%s-%s", s.hostname, strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1))
	destFilesPath := fmt.Sprintf("%s%s", destFilePathPrefix, bundlename)
	if err := fs.MkdirAll(destFilesPath, 0777); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while making directory: "+destFilesPath, err)
		return subcommands.ExitFailure
	}
	onetime.LogMessageToFileAndConsole(ctx, "Collecting Support Bundle Report for Agent for SAP...")
	reqFilePaths := []string{linuxConfigFilePath}
	globalPath := fmt.Sprintf(`/usr/sap/%s/SYS/global/hdb`, s.sid)

	hanaPaths := []string{}
	for _, inr := range s.instanceNumsAfterSplit {
		hanaPaths = append(hanaPaths, fmt.Sprintf(`/usr/sap/%s/HDB%s/%s`, s.sid, inr, s.hostname))
	}

	var hasErrors bool
	if !s.agentLogsOnly {
		hasErrors = extractSystemDBErrors(ctx, destFilesPath, s.hostname, hanaPaths, exec, fs)
		hasErrors = extractTenantDBErrors(ctx, destFilesPath, s.sid, s.hostname, hanaPaths, exec, fs) || hasErrors
		hasErrors = extractBackintErrors(ctx, destFilesPath, globalPath, s.hostname, exec, fs) || hasErrors
		hasErrors = extractJournalCTLLogs(ctx, destFilesPath, s.hostname, exec, fs) || hasErrors
		hasErrors = extractHANAVersion(ctx, destFilesPath, s.sid, s.hostname, exec, fs) || hasErrors
		reqFilePaths = append(reqFilePaths, nameServerTracesAndBackupLogs(ctx, hanaPaths, s.sid, fs)...)
		reqFilePaths = append(reqFilePaths, tenantDBNameServerTracesAndBackupLogs(ctx, hanaPaths, s.sid, fs)...)
		reqFilePaths = append(reqFilePaths, backintParameterFiles(ctx, globalPath, s.sid, fs)...)
		reqFilePaths = append(reqFilePaths, backintLogs(ctx, globalPath, s.sid, fs)...)
	}
	reqFilePaths = append(reqFilePaths, agentLogFiles(ctx, linuxLogFilesPath, fs)...)
	reqFilePaths = append(reqFilePaths, agentOTELogFiles(ctx, agentOnetimeFilesPath, fs)...)

	for _, path := range reqFilePaths {
		onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Copying file %s ...", path))
		if err := copyFile(path, destFilesPath+path, fs); err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Error while copying file: "+path, err)
			hasErrors = true
		}
	}

	zipfile := fmt.Sprintf("%s/%s.zip", destFilesPath, bundlename)
	if err := zipSource(destFilesPath, zipfile, fs, z); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while zipping destination folder %s", destFilesPath), err)
		hasErrors = true
	} else {
		onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Zipped destination support bundle file HANA/Backint %s", fmt.Sprintf("%s.zip", destFilesPath)))
	}

	if s.resultBucket != "" {
		if err := s.uploadZip(ctx, zipfile, bundlename, storage.ConnectToBucket, getReadWriter, fs, st.NewClient); err != nil {
			fmt.Println(fmt.Sprintf("Error while uploading zip file %s to bucket %s", destFilePathPrefix+".zip", s.resultBucket), " Error: ", err)
			hasErrors = true
		} else {
			// removing the destination directory after zip file is created.
			if err := removeDestinationFolder(ctx, destFilesPath, fs); err != nil {
				fmt.Println(fmt.Sprintf("Error while removing destination folder %s", destFilesPath), " Error: ", err)
				hasErrors = true
			}
		}
	}

	// Rotate out old support bundles so we don't fill the file system.
	if err := rotateOldBundles(ctx, destFilePathPrefix, fs); err != nil {
		hasErrors = true
	}

	if s.pacemakerDiagnosis {
		// collect pacemaker reports using OS Specific commands
		pacemakerFilesDir := fmt.Sprintf("%spacemaker-%s", destFilePathPrefix, time.Now().UTC().String()[:16])
		pacemakerFilesDir = strings.ReplaceAll(pacemakerFilesDir, " ", "-")
		pacemakerFilesDir = strings.ReplaceAll(pacemakerFilesDir, ":", "-")
		err := pacemakerLogs(ctx, pacemakerFilesDir, exec, fs)
		if err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Error while collecting pacemaker logs: "+err.Error(), err)
			hasErrors = true
		} else {
			onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Pacemaker logs are collected and sent to directory %s", pacemakerFilesDir))
		}
	}

	if hasErrors {
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

// uploadZip uploads the zip file to the bucket provided.
func (s *SupportBundle) uploadZip(ctx context.Context, destFilesPath, bundleName string, ctb storage.BucketConnector, grw getReaderWriter, fs filesystem.FileSystem, client storage.Client) error {
	fmt.Println(fmt.Sprintf("Uploading bundle %s to bucket %s", destFilesPath, s.resultBucket))
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
		BucketName:       s.resultBucket,
		UserAgentSuffix:  "Support Bundle",
		VerifyConnection: true,
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
		BucketName:   s.resultBucket,
		ObjectName:   objectName,
		TotalBytes:   fileSize,
		VerifyUpload: true,
	}

	rw := grw(readWriter)
	var bytesWritten int64
	if bytesWritten, err = rw.Upload(ctx); err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("File uploaded", "bucket", s.resultBucket, "bytesWritten", bytesWritten, "fileSize", fileSize)
	fmt.Println(fmt.Sprintf("Bundle uploaded to bucket %s", s.resultBucket))
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

func backintParameterFiles(ctx context.Context, globalPath string, sid string, fs filesystem.FileSystem) []string {
	backupFiles := regexp.MustCompile(`_backup_parameter_file.*`)
	res := []string{globalPath + globalINIFile}

	content, err := fs.ReadFile(globalPath + globalINIFile)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while reading file: "+globalPath+globalINIFile, err)
		return nil
	}
	contentData := string(content)
	op := backupFiles.FindAllString(contentData, -1)
	for _, path := range op {
		pathSplit := strings.Split(path, "=")
		if len(pathSplit) != 2 {
			onetime.LogMessageToFileAndConsole(ctx, "Unexpected output from global.ini content")
			continue
		}
		rfp := strings.TrimSpace(strings.Split(path, "=")[1])
		onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", rfp))
		res = append(res, rfp)
	}
	return res
}

func nameServerTracesAndBackupLogs(ctx context.Context, hanaPaths []string, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	for _, hanaPath := range hanaPaths {
		fds, err := fs.ReadDir(fmt.Sprintf("%s/trace", hanaPath))
		if err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+hanaPath+"/trace", err)
			return nil
		}
		for _, fd := range fds {
			if fd.IsDir() {
				continue
			}
			if matchNameServerTraceAndBackup(fd.Name()) {
				onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", path.Join(hanaPath+"/trace", fd.Name())))
				res = append(res, path.Join(hanaPath+"/trace/", fd.Name()))
			}
		}
	}
	return res
}

func tenantDBNameServerTracesAndBackupLogs(ctx context.Context, hanaPaths []string, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	for _, hanaPath := range hanaPaths {
		fds, err := fs.ReadDir(fmt.Sprintf("%s/trace/DB_%s", hanaPath, sid))
		if err != nil {
			onetime.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+hanaPath+"/trace", err)
			return nil
		}
		for _, fd := range fds {
			if fd.IsDir() {
				continue
			}
			if matchNameServerTraceAndBackup(fd.Name()) {
				onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Adding file %s to collection.", path.Join(fmt.Sprintf("%s/trace/DB_%s", hanaPath, sid), fd.Name())))
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
func backintLogs(ctx context.Context, globalPath, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fs.ReadDir(globalPath + backintGCSPath)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+globalPath+backintGCSPath, err)
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
func agentOTELogFiles(ctx context.Context, agentOTEFilesPath string, fu filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fu.ReadDir(agentOTEFilesPath)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+agentOTEFilesPath, err)
		return res
	}
	for _, fd := range fds {
		res = append(res, path.Join(agentOTEFilesPath, fd.Name()))
	}
	return res
}

func agentLogFiles(ctx context.Context, linuxLogFilesPath string, fu filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fu.ReadDir(linuxLogFilesPath)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while reading directory: "+linuxLogFilesPath, err)
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
func extractJournalCTLLogs(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole(ctx, "Extracting journal CTL logs...")
	var hasErrors bool
	p := commandlineexecutor.Params{
		Executable:  "bash",
		ArgsToSplit: "-c 'journalctl | grep google-cloud-sap-agent'",
	}
	if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, journalCTLLogs, fu); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while executing command: journalctl | grep google-cloud-sap-agent", err)
		hasErrors = true
	}
	return hasErrors
}

// extractSystemDBErrors extracts the errors from system DB backup logs.
func extractSystemDBErrors(ctx context.Context, destFilesPath, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole(ctx, "Extracting errors from System DB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: fmt.Sprintf("-w ERROR %s/trace/backup.log", hanaPath),
		}
		onetime.LogMessageToFileAndConsole(ctx, "Executing command: grep -w ERROR"+hanaPath+"/trace/backup.log")
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, systemDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractTenantDBErrors extracts the errors from tenant DB backup logs.
func extractTenantDBErrors(ctx context.Context, destFilesPath, sid, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole(ctx, "Extracting errors from TenantDB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		filePath := fmt.Sprintf("%s/trace/DB_%s/backup.log", hanaPath, sid)
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: "-w ERROR " + filePath,
		}
		onetime.LogMessageToFileAndConsole(ctx, "Executing command: grep -w ERROR"+filePath)
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, tenantDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractBackintErrors extracts the errors from backint logs.
func extractBackintErrors(ctx context.Context, destFilesPath, globalPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole(ctx, "Extracting errors from Backint logs...")
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
		onetime.LogMessageToFileAndConsole(ctx, "Executing command: grep -w SEVERE"+logFilePath)
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, backintErrorsFile, fu); err != nil {
			hasErrors = true
		}
	}
	return hasErrors
}

// extractHANAVersion extracts the HANA version from the sap env.
func extractHANAVersion(ctx context.Context, destFilesPath, sid, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	cmd := "-c 'source /usr/sap/" + sid + "/home/.sapenv.sh && /usr/sap/" + sid + "/*/HDB version'"
	params := commandlineexecutor.Params{
		User:        fmt.Sprintf("%sadm", strings.ToLower(sid)),
		Executable:  "bash",
		ArgsToSplit: cmd,
	}
	onetime.LogMessageToFileAndConsole(ctx, "Executing command: bash -c 'HDB version'")
	err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, params, hanaVersionFile, fu)
	if err != nil {
		return true
	}
	return false
}

// execAndWriteToFile executes the command and writes the output to the file.
func execAndWriteToFile(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, params commandlineexecutor.Params, opFile string, fu filesystem.FileSystem) error {
	res := exec(ctx, params)
	if res.ExitCode != 0 && res.StdErr != "" {
		onetime.LogErrorToFileAndConsole(ctx, "Error while executing command", errors.New(res.StdErr))
		return errors.New(res.StdErr)
	}
	f, err := fu.OpenFile(destFilesPath+"/"+hostname+opFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while opening the file", err)
		return err
	}
	defer f.Close()
	if _, err := fu.WriteStringToFile(f, res.StdOut); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, "Error while writing to the file", err)
		return err
	}
	return nil
}

// pacemakerLogs collects the pacemaker logs.
func pacemakerLogs(ctx context.Context, destFilesPath string, exec commandlineexecutor.Execute, fs filesystem.FileSystem) error {
	rhelParams := commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "-qE rhel /etc/os-release",
	}
	if val := checkForLinuxOSType(ctx, exec, rhelParams); val {
		if err := rhelPacemakerLogs(ctx, exec, destFilesPath, fs); err != nil {
			return err
		}
		return nil
	}
	slesParams := commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "-qE SLES /etc/os-release",
	}
	if val := checkForLinuxOSType(ctx, exec, slesParams); val {
		if err := slesPacemakerLogs(ctx, exec, destFilesPath, fs); err != nil {
			return err
		}
		return nil
	}
	return errors.New("incompatible os type for collecting pacemaker logs")
}

// checkForLinuxOSType checks if the OS type is RHEL or SLES.
func checkForLinuxOSType(ctx context.Context, exec commandlineexecutor.Execute, p commandlineexecutor.Params) bool {
	res := exec(ctx, p)
	if res.ExitCode != 0 || res.StdErr != "" {
		onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while executing command %s %s, returned exitCode: %d", p.Executable, p.ArgsToSplit, res.ExitCode), errors.New(res.StdErr))
		return false
	}
	return true
}

// slesPacemakerLogs collects the pacemaker logs for SLES OS type.
func slesPacemakerLogs(ctx context.Context, exec commandlineexecutor.Execute, destFilesPath string, fu filesystem.FileSystem) error {
	// time.Now().UTC() returns current time UTC format with milliseconds precision,
	// we only need it till first 16 characters to satisfy the hb_report and crm_report command
	to := time.Now().UTC().String()[:16]
	from := time.Now().UTC().AddDate(0, 0, -3).String()[:16]
	if err := fu.MkdirAll(destFilesPath, 0777); err != nil {
		return err
	}
	onetime.LogMessageToFileAndConsole(ctx, "Collecting hb_report...")
	res := exec(ctx, commandlineexecutor.Params{
		Executable:  "hb_report",
		ArgsToSplit: fmt.Sprintf("-S -f %s -t %s %s", from[:10], to[:10], destFilesPath+"/report"),
		Timeout:     3600,
	})
	if res.ExitCode != 0 {
		onetime.LogMessageToFileAndConsole(ctx, "Collecting crm_report...")
		res := exec(ctx, commandlineexecutor.Params{
			Executable:  "crm_report",
			ArgsToSplit: fmt.Sprintf("-S -f %s -t %s %s", from[:10], to[:10], destFilesPath+"/report"),
			Timeout:     3600,
		})
		if res.ExitCode != 0 {
			return errors.New(res.StdErr)
		}
	}
	onetime.LogMessageToFileAndConsole(ctx, "Collecting supportconfig...")
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
func rhelPacemakerLogs(ctx context.Context, exec commandlineexecutor.Execute, destFilesPath string, fu filesystem.FileSystem) error {
	onetime.LogMessageToFileAndConsole(ctx, "Collecting sosreport...")
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
		onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while executing command %s", p.Executable), errors.New(res.StdErr))
		// if sosreport is unsuccessful in collecting pacemaker data, we will fallback to crm_report
		from := time.Now().UTC().AddDate(0, 0, -3).String()[:16]
		to := time.Now().UTC().String()[:16]
		onetime.LogMessageToFileAndConsole(ctx, "Collecting crm_report...")
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

func (s *SupportBundle) validateParams() []string {
	var errs []string
	if s.agentLogsOnly {
		return errs
	}
	if s.sid == "" {
		errs = append(errs, "no value provided for sid")
	}
	if s.instanceNums == "" {
		errs = append(errs, "no value provided for instance-numbers")
	} else {
		s.instanceNumsAfterSplit = strings.Split(s.instanceNums, " ")
		for _, nos := range s.instanceNumsAfterSplit {
			if len(nos) != 2 {
				errs = append(errs, fmt.Sprintf("invalid instance number %s", nos))
			}
		}
	}
	if s.hostname == "" {
		errs = append(errs, "no value provided for hostname")
	}
	return errs
}

func removeDestinationFolder(ctx context.Context, path string, fu filesystem.FileSystem) error {
	if err := fu.RemoveAll(path); err != nil {
		onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while removing folder %s", path), err)
		return err
	}
	return nil
}

func rotateOldBundles(ctx context.Context, dir string, fs filesystem.FileSystem) error {
	fds, err := fs.ReadDir(dir)
	if err != nil {
		onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while reading folder %s", dir), err)
		return err
	}
	sort.Slice(fds, func(i, j int) bool { return fds[i].ModTime().After(fds[j].ModTime()) })
	bundleCount := 0
	for _, fd := range fds {
		if strings.Contains(fd.Name(), "supportbundle") {
			bundleCount++
			if bundleCount > 5 {
				onetime.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Removing old bundle %s", dir+fd.Name()))
				if err := fs.RemoveAll(dir + fd.Name()); err != nil {
					onetime.LogErrorToFileAndConsole(ctx, fmt.Sprintf("Error while removing old bundle %s", dir+fd.Name()), err)
					return err
				}
			}
		}
	}
	return nil
}
