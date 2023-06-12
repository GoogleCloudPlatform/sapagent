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

// Package sosreport implements one time execution mode for
// sosreport.
package sosreport

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
)

type (
	// SOSReport has args for SOS report collection one time mode.
	SOSReport struct {
		sid                    string
		instanceNums           string
		instanceNumsAfterSplit []string
		hostname               string
	}

	fileSystemHelper struct{}

	zipperHelper struct{}
)

// MkdirAll provides testable implementation of os.MkdirAll method
func (h fileSystemHelper) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// ReadFile provides testable implementation of os.ReadFile method.
func (h fileSystemHelper) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadDir provides testable implementation of os.ReadDir method.
func (h fileSystemHelper) ReadDir(path string) ([]fs.FileInfo, error) {
	return ioutil.ReadDir(path)
}

// Open provides testable implementation of os.Open method.
func (h fileSystemHelper) Open(path string) (*os.File, error) {
	return os.Open(path)
}

// OpenFile provides testable implementation of os.OpenFile method.
func (h fileSystemHelper) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

// RemoveAll provides testable implementation of os.RemoveAll method.
func (h fileSystemHelper) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Create provides testable implementation of os.Create method.
func (h fileSystemHelper) Create(path string) (*os.File, error) {
	return os.Create(path)
}

// WriteStringToFile provides testable implementation of os.WriteStringToFile method.
func (h fileSystemHelper) WriteStringToFile(file *os.File, content string) (int, error) {
	return file.WriteString(content)
}

// Copy provides testable implementation of io.Copy method.
func (h fileSystemHelper) Copy(w io.Writer, r io.Reader) (int64, error) {
	return io.Copy(w, r)
}

// Chmod provides testable implementation of os.Chmod method.
func (h fileSystemHelper) Chmod(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

// Stat provides testable implementation of os.Stat method.
func (h fileSystemHelper) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// WalkAndZip provides testable implementation of filepath.Walk which zips the content of the directory.
func (h fileSystemHelper) WalkAndZip(source string, z zipper.Zipper, w *zip.Writer) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := z.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Method = zip.Deflate

		header.Name, err = filepath.Rel(filepath.Dir(source), path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			header.Name += "/"
		}

		headerWriter, err := z.CreateHeader(w, header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		f, err := h.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = h.Copy(headerWriter, f)
		return err
	})
}

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
	destFilePathPrefix  = `/tmp/google-cloud-sap-agent/sos-report-`
	linuxConfigFilePath = `/etc/google-cloud-sap-agent/configuration.json`
	linuxLogFilesPath   = `/var/log/`
	systemDBErrorsFile  = `_SYSTEM_DB_BACKUP_ERROR.txt`
	tenantDBErrorsFile  = `_TENANT_DB_BACKUP_ERROR.txt`
	backintErrorsFile   = `_BACKINT_ERROR.txt`
	globalINIFile       = `/custom/config/global.ini`
	backintGCSPath      = `/opt/backint/backint-gcs`
)

// Name implements the subcommand interface for collecting SOS report collection for support team.
func (*SOSReport) Name() string {
	return "sosreport"
}

// Synopsis implements the subcommand interface for SOS report collection for support team.
func (*SOSReport) Synopsis() string {
	return "collect sos report of Agent for SAP for the support team"
}

// Usage implements the subcommand interface for SOS report collection for support team.
func (*SOSReport) Usage() string {
	return `sosreport [-sid=<SAP System Identifier> -instance-numbers=<Instance numbers> -hostname=<Hostname>]
	Example: sosreport -sid="DEH" -instance-numbers="00 01 11" -hostname="sample_host"
	`
}

// SetFlags implements the subcommand interface for SOS report collection.
func (s *SOSReport) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.sid, "sid", "", "SAP System Identifier")
	fs.StringVar(&s.instanceNums, "instance-numbers", "", "Instance numbers")
	fs.StringVar(&s.hostname, "hostname", "", "Hostname")
}

// Execute implements the subcommand interface for SOS report collection.
func (s *SOSReport) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	log.SetupOneTimeLogging(lp, s.Name())
	return s.sosReportHandler(ctx, destFilePathPrefix, commandlineexecutor.ExecuteCommand, fileSystemHelper{}, zipperHelper{})
}

func (s *SOSReport) sosReportHandler(ctx context.Context, destFilePathPrefix string, exec commandlineexecutor.Execute, fs filesystem.FileSystem, z zipper.Zipper) subcommands.ExitStatus {
	if errs := s.validateParams(); len(errs) > 0 {
		errMessage := strings.Join(errs, ", ")
		onetime.LogErrorToFileAndConsole("Invalid params for collecting SOS Report for Agent for SAP"+errMessage, errors.New(errMessage))
		return subcommands.ExitUsageError
	}
	destFilesPath := destFilePathPrefix + s.hostname + "-" + strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1)
	if err := fs.MkdirAll(destFilesPath, 0777); err != nil {
		onetime.LogErrorToFileAndConsole("Error while making directory: "+destFilesPath, err)
		return subcommands.ExitFailure
	}
	onetime.LogMessageToFileAndConsole("Collecting SOS Report for Agent for SAP...")
	reqFilePaths := []string{linuxConfigFilePath}
	onetime.LogMessageToFileAndConsole(fmt.Sprintf("Required file path %v", reqFilePaths))
	globalPath := fmt.Sprintf(`/usr/sap/%s/SYS/global/hdb`, s.sid)

	hanaPaths := []string{}
	for _, inr := range s.instanceNumsAfterSplit {
		hanaPaths = append(hanaPaths, fmt.Sprintf(`/usr/sap/%s/HDB%s/%s`, s.sid, inr, s.hostname))
	}

	var hasErrors bool
	hasErrors = extractSystemDBErrors(ctx, destFilesPath, s.hostname, hanaPaths, exec, fs)
	hasErrors = extractTenantDBErrors(ctx, destFilesPath, s.sid, s.hostname, hanaPaths, exec, fs) || hasErrors
	hasErrors = extractBackintErrors(ctx, destFilesPath, globalPath, s.hostname, exec, fs) || hasErrors
	reqFilePaths = append(reqFilePaths, nameServerTracesAndBackupLogs(ctx, hanaPaths, s.sid, fs)...)
	reqFilePaths = append(reqFilePaths, backintParameterFiles(ctx, globalPath, s.sid, fs)...)
	reqFilePaths = append(reqFilePaths, backintLogs(ctx, globalPath, s.sid, fs)...)
	reqFilePaths = append(reqFilePaths, agentLogFiles(linuxLogFilesPath, fs)...)

	for _, path := range reqFilePaths {
		onetime.LogMessageToFileAndConsole(fmt.Sprintf("Copying file %s ...", path))
		if err := copyFile(path, destFilesPath+path, fs); err != nil {
			onetime.LogErrorToFileAndConsole("Error while copying file: "+path, err)
			hasErrors = true
		}
	}
	if err := zipSource(destFilesPath, destFilesPath+".zip", fs, z); err != nil {
		onetime.LogErrorToFileAndConsole(fmt.Sprintf("Error while zipping destination folder %s", destFilesPath), err)
		hasErrors = true
	}
	if err := removeDestinationFolder(destFilesPath, fs); err != nil {
		hasErrors = true
	}
	if hasErrors {
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
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
	f, err := fs.Create(target)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := z.NewWriter(f)
	defer z.Close(writer)
	return fs.WalkAndZip(source, z, writer)
}

func backintParameterFiles(ctx context.Context, globalPath string, sid string, fs filesystem.FileSystem) []string {
	backupFiles := regexp.MustCompile(`_backup_parameter_file.*`)
	res := []string{globalPath + globalINIFile}

	content, err := fs.ReadFile(globalPath + globalINIFile)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Error while reading file: "+globalPath+globalINIFile, err)
		return nil
	}
	contentData := string(content)
	op := backupFiles.FindAllString(contentData, -1)
	for _, path := range op {
		pathSplit := strings.Split(path, "=")
		if len(pathSplit) != 2 {
			onetime.LogMessageToFileAndConsole("Unexpected output from global.ini content")
			continue
		}
		rfp := strings.TrimSpace(strings.Split(path, "=")[1])
		res = append(res, rfp)
	}
	return res
}

func nameServerTracesAndBackupLogs(ctx context.Context, hanaPaths []string, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	for _, hanaPath := range hanaPaths {
		fds, err := fs.ReadDir(hanaPath + `/trace`)
		if err != nil {
			onetime.LogErrorToFileAndConsole("Error while reading directory: "+hanaPath+"/trace", err)
			return nil
		}
		for _, fd := range fds {
			if fd.IsDir() {
				continue
			}
			if matchNameServerTraceAndBackup(fd.Name()) {
				res = append(res, path.Join(hanaPath+"/trace/", fd.Name()))
			}
		}
	}
	return res
}

func matchNameServerTraceAndBackup(name string) bool {
	nameserverTrace := regexp.MustCompile(`nameserver.*[0-9]\.[0-9][0-9][0-9]\.trc`)
	nameserverTopologyJSON := regexp.MustCompile(`nameserver.*topology.*json`)
	indexServer := regexp.MustCompile(`indexserver.*[0-9]\.[0-9][0-9][0-9]\.trc`)
	backuplog := regexp.MustCompile(`backup.log`)
	backinitlog := regexp.MustCompile(`backinit.log`)

	if nameserverTrace.MatchString(name) || indexServer.MatchString(name) ||
		backuplog.MatchString(name) || backinitlog.MatchString(name) ||
		nameserverTopologyJSON.MatchString(name) {
		return true
	}
	return false
}

func backintLogs(ctx context.Context, globalPath, sid string, fs filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fs.ReadDir(globalPath + backintGCSPath)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Error while reading directory: "+globalPath+backintGCSPath, err)
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

func agentLogFiles(linuxLogFilesPath string, fu filesystem.FileSystem) []string {
	res := []string{}
	fds, err := fu.ReadDir(linuxLogFilesPath)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Error while reading directory: "+linuxLogFilesPath, err)
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

func extractSystemDBErrors(ctx context.Context, destFilesPath, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole("Extracting errors from System DB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: fmt.Sprintf("-w ERROR %s/trace/backup.log", hanaPath),
		}
		onetime.LogMessageToFileAndConsole("Executing command: grep -w ERROR" + hanaPath + "/trace/backup.log")
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, systemDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

func extractTenantDBErrors(ctx context.Context, destFilesPath, sid, hostname string, hanaPaths []string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole("Extracting errors from TenantDB files...")
	var hasErrors bool
	for _, hanaPath := range hanaPaths {
		filePath := hanaPath + "/trace/DB_" + sid + "/backup.log"
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: "-w ERROR " + filePath,
		}
		onetime.LogMessageToFileAndConsole("Executing command: grep -w ERROR" + filePath)
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, tenantDBErrorsFile, fu); err != nil && !errors.Is(err, os.ErrNotExist) {
			hasErrors = true
		}
	}
	return hasErrors
}

func extractBackintErrors(ctx context.Context, destFilesPath, globalPath, hostname string, exec commandlineexecutor.Execute, fu filesystem.FileSystem) bool {
	onetime.LogMessageToFileAndConsole("Extracting errors from Backint logs...")
	fds, err := fu.ReadDir(globalPath + backintGCSPath + "/logs")
	if err != nil {
		return true
	}
	var hasErrors bool
	for _, fd := range fds {
		logFilePath := globalPath + backintGCSPath + "/logs/" + fd.Name()
		p := commandlineexecutor.Params{
			Executable:  "grep",
			ArgsToSplit: "-w SEVERE " + logFilePath,
		}
		onetime.LogMessageToFileAndConsole("Executing command: grep -w SEVERE" + logFilePath)
		if err := execAndWriteToFile(ctx, destFilesPath, hostname, exec, p, backintErrorsFile, fu); err != nil {
			hasErrors = true
		}
	}
	return hasErrors
}

func execAndWriteToFile(ctx context.Context, destFilesPath, hostname string, exec commandlineexecutor.Execute, params commandlineexecutor.Params, opFile string, fu filesystem.FileSystem) error {
	res := exec(ctx, params)
	if res.ExitCode != 0 && res.StdErr != "" {
		onetime.LogErrorToFileAndConsole("Error while executing command", errors.New(res.StdErr))
		return res.Error
	}
	f, err := fu.OpenFile(destFilesPath+"/"+hostname+opFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Error while opening the file", err)
		return err
	}
	defer f.Close()
	if _, err := fu.WriteStringToFile(f, res.StdOut); err != nil {
		onetime.LogErrorToFileAndConsole("Error while writing to the file", err)
		return err
	}
	return nil
}

func (s *SOSReport) validateParams() []string {
	var errs []string
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

func removeDestinationFolder(path string, fu filesystem.FileSystem) error {
	if err := fu.RemoveAll(path); err != nil {
		onetime.LogErrorToFileAndConsole(fmt.Sprintf("Error while removing folder %s", path), err)
		return err
	}
	return nil
}
