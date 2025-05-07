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

package supportbundle

import (
	"archive/zip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"flag"
	st "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/hostmetrics/cloudmetricreader"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/rest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/storage"

	lpb "google.golang.org/genproto/googleapis/api/label"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tpb "google.golang.org/protobuf/types/known/timestamppb"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	fakeCM "github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring/fake"
)

var (
	defaultRunOptions      = onetime.CreateRunOptions(nil, false)
	defaultOTELogger       = onetime.CreateOTELogger(false)
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:  "sample-project",
		InstanceId: "123456789",
	}

	defaultNewClient = func(timeout time.Duration, trans *http.Transport) httpClient {
		return &http.Client{Timeout: timeout, Transport: trans}
	}
	defaultTransport = func() *http.Transport {
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxConnsPerHost:       100,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       10 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	//go:embed testdata/sample_sap_discovery_response.txt
	sampleDiscoveryResponse string
	//go:embed testdata/wanted_discovery.txt
	discoveryResponse string

	successfulTimezoneExec = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			ExitCode: 0,
			StdOut:   "location=America/Los_Angeles\n",
			StdErr:   "",
		}
	}
	wrongTimezoneExec = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			ExitCode: 0,
			StdOut:   "location=India/Los_Angeles\n",
			StdErr:   "",
		}
	}
	failedTimezoneExec = func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.Result{
			ExitCode: 2,
			StdOut:   "",
			StdErr:   "failure",
		}
	}
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

type (
	mockedfilesystem struct {
		fileContent    string
		readDirContent []fs.FileInfo
		copyErr        error
		reqErr         error
	}

	mockedZipper struct {
		fileInfoErr     error
		createHeaderErr error
	}

	mockedFileInfo struct {
		name    string
		size    int64
		mode    fs.FileMode
		modTime time.Time
		isDir   bool
		sys     any
	}

	fakeReadWriter struct {
		err error
	}

	mockedWriter struct {
		err error
	}

	mockToken struct {
		token *oauth2.Token
		err   error
	}

	mockRest struct {
		err error
	}
)

func (w mockedWriter) Write([]byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	return 10, nil
}

func (mfi mockedFileInfo) Name() string {
	return mfi.name
}

func (mfi mockedFileInfo) Size() int64 {
	return mfi.size
}

func (mfi mockedFileInfo) Mode() fs.FileMode {
	return mfi.mode
}

func (mfi mockedFileInfo) ModTime() time.Time {
	return mfi.modTime
}

func (mfi mockedFileInfo) IsDir() bool {
	return mfi.isDir
}

func (mfi mockedFileInfo) Sys() any {
	return mfi.sys
}

func (mfu mockedfilesystem) MkdirAll(path string, perm os.FileMode) error {
	if strings.Contains(path, "failure") {
		return cmpopts.AnyError
	}
	return nil
}

func (mfu mockedfilesystem) ReadFile(path string) ([]byte, error) {
	if strings.Contains(path, "failure") {
		return nil, cmpopts.AnyError
	}
	return []byte(mfu.fileContent), nil
}

func (mfu mockedfilesystem) ReadDir(path string) ([]fs.FileInfo, error) {
	if strings.Contains(path, "failure") {
		return nil, cmpopts.AnyError
	}
	return mfu.readDirContent, nil
}

func (mfu mockedfilesystem) Open(path string) (*os.File, error) {
	if mfu.reqErr != nil {
		return nil, mfu.reqErr
	}
	if strings.Contains(path, "failure") {
		return nil, cmpopts.AnyError
	}
	return &os.File{}, nil
}

func (mfu mockedfilesystem) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	if mfu.reqErr != nil {
		return nil, mfu.reqErr
	}
	if strings.Contains(path, "failure") {
		return nil, cmpopts.AnyError
	}
	if strings.Contains(path, "nil-file") {
		return nil, nil
	}
	if strings.Contains(path, "does-not-exist") {
		return nil, os.ErrNotExist
	}
	return &os.File{}, nil
}

func (mfu mockedfilesystem) RemoveAll(path string) error {
	if strings.Contains(path, "failure") {
		return cmpopts.AnyError
	}
	return nil
}

func (mfu mockedfilesystem) Create(path string) (*os.File, error) {
	if strings.Contains(path, "failure") {
		return nil, cmpopts.AnyError
	}
	if strings.Contains(path, "sap-discovery-success") {
		f, err := os.CreateTemp(os.TempDir(), "sap-discovery-success")
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	return &os.File{}, nil
}

func (mfu mockedfilesystem) WriteStringToFile(f *os.File, content string) (int, error) {
	if f == nil {
		return 0, cmpopts.AnyError
	}
	return 10, nil
}

func (mfu mockedfilesystem) Rename(old, new string) error {
	if strings.Contains(old, "failure") {
		return cmpopts.AnyError
	}
	return nil
}

func (mfu mockedfilesystem) Copy(w io.Writer, r io.Reader) (int64, error) {
	if mfu.copyErr != nil {
		return 0, mfu.copyErr
	}
	return 10, nil
}

func (mfu mockedfilesystem) Chmod(path string, perm os.FileMode) error {
	if path == "failure" {
		return cmpopts.AnyError
	}
	return nil
}

func (mfu mockedfilesystem) Stat(path string) (os.FileInfo, error) {
	if path == "failure" {
		return nil, cmpopts.AnyError
	}
	return mockedFileInfo{name: mfu.readDirContent[0].Name(), mode: mfu.readDirContent[0].Mode()}, nil
}

func (mfu mockedfilesystem) WalkAndZip(path string, z zipper.Zipper, w *zip.Writer) error {
	if path == "failure" {
		return cmpopts.AnyError
	}
	if strings.Contains(path, "test") {
		fsh := filesystem.Helper{}
		return fsh.WalkAndZip(path, z, w)
	}
	return nil
}

func (mz mockedZipper) NewWriter(w io.Writer) *zip.Writer {
	return &zip.Writer{}
}

func (mz mockedZipper) FileInfoHeader(fi fs.FileInfo) (*zip.FileHeader, error) {
	if mz.fileInfoErr != nil {
		return nil, mz.fileInfoErr
	}
	return &zip.FileHeader{}, nil
}

func (mz mockedZipper) CreateHeader(w *zip.Writer, fh *zip.FileHeader) (io.Writer, error) {
	if mz.createHeaderErr != nil {
		return nil, mz.createHeaderErr
	}
	return mockedWriter{err: nil}, nil
}

func (mz mockedZipper) Close(w *zip.Writer) error {
	if w == nil {
		return cmpopts.AnyError
	}
	return nil
}

func (f *fakeReadWriter) Upload(ctx context.Context) (int64, error) {
	return 0, f.err
}

func (m *mockToken) Token() (*oauth2.Token, error) {
	return m.token, m.err
}

func (m *mockRest) GetResponse(ctx context.Context, url string) ([]byte, error) {
	return nil, m.err
}

func (m *mockRest) NewRest() {}

func fakeExec(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if p.ArgsToSplit == "error" {
		return commandlineexecutor.Result{
			Error:    cmpopts.AnyError,
			StdErr:   "failure",
			ExitCode: 2,
		}
	}
	return commandlineexecutor.Result{
		ExitCode: 0,
		StdOut:   "success",
	}
}

func fakeExecForErrOnly(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecRHEL(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if strings.Contains(p.ArgsToSplit, `rhel`) {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	if strings.Contains(p.Executable, `sosreport`) {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecSLESHBSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if strings.Contains(p.Executable, "hb_report") {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecSLESCRMSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if strings.Contains(p.Executable, "crm_report") {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecSupportConfigSuccess(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if strings.Contains(p.Executable, "supportconfig") {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func fakeExecSLES(ctx context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
	if strings.Contains(p.ArgsToSplit, `SLES`) {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	if strings.Contains(p.Executable, `hb`) || strings.Contains(p.Executable, `supportconfig`) || strings.Contains(p.Executable, `crm_report`) {
		return commandlineexecutor.Result{
			ExitCode: 0,
		}
	}
	return commandlineexecutor.Result{ExitCode: 2, StdErr: "failure", Error: cmpopts.AnyError}
}

func TestSetFlagsForSOSReport(t *testing.T) {
	sosrc := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"sid", "instance-numbers", "hostname"}
	sosrc.SetFlags(fs)
	for _, flagName := range flags {
		got := fs.Lookup(flagName)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flagName)
		}
	}
}

func TestExecuteForSOSReport(t *testing.T) {
	tests := []struct {
		name string
		sosr *SupportBundle
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			sosr: &SupportBundle{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			sosr: &SupportBundle{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
		{
			name: "SuccessForHelp",
			sosr: &SupportBundle{
				Help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.sosr.oteLogger = defaultOTELogger
			got := test.sosr.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.sosr, test.args, got, test.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name           string
		sosr           *SupportBundle
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name:           "FailLengthArgs",
			sosr:           &SupportBundle{},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name:           "FailAssertArgs",
			sosr:           &SupportBundle{},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "InvalidParams",
			sosr: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "",
				Hostname:     "sample_host",
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.sosr.oteLogger = defaultOTELogger
			_, exitStatus := test.sosr.Run(context.Background(), defaultRunOptions, fakeExec)
			if exitStatus != test.wantExitStatus {
				t.Errorf("ExecuteAndGetMessage(%v) = %v; want: %v", test.sosr, exitStatus, test.wantExitStatus)
			}
		})
	}
}

func TestCollectAgentSupport(t *testing.T) {
	tests := []struct {
		name string
		sosr *SupportBundle
		want subcommands.ExitStatus
	}{
		{
			name: "Failure",
			sosr: &SupportBundle{
				Sid: "",
			},
			want: subcommands.ExitFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CollectAgentSupport(context.Background(), &flag.FlagSet{Usage: func() { return }}, log.Parameters{}, nil, "")
			if got != test.want {
				t.Errorf("CollectAgentSupport(%v)=%v, want %v", test.sosr, got, test.want)
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name  string
		sosrc SupportBundle
		want  []string
	}{
		{
			name:  "NoValueForSID",
			sosrc: SupportBundle{InstanceNums: "00 01", Hostname: "sample_host"},
			want:  []string{"no value provided for sid"},
		},
		{
			name:  "NoValueForInstance",
			sosrc: SupportBundle{Sid: "DEH", InstanceNums: "", Hostname: "sample_host"},
			want:  []string{"no value provided for instance-numbers"},
		},
		{
			name:  "InvalidValueForinstanceNums",
			sosrc: SupportBundle{Sid: "DEH", InstanceNums: "00 011", Hostname: "sample_host"},
			want:  []string{"invalid instance number 011"},
		},
		{
			name:  "NoValueForHostName",
			sosrc: SupportBundle{Sid: "DEH", InstanceNums: "00 01", Hostname: ""},
			want:  []string{"no value provided for hostname"},
		},
		{
			name: "NoTimestamp",
			sosrc: SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 01",
				Hostname:     "sample_host",
				Metrics:      true,
			},
			want: []string{},
		},
		{
			name: "AgentLogsOnly",
			sosrc: SupportBundle{
				AgentLogsOnly: true,
			},
			want: []string{},
		},
		{
			name: "AllLogsAndTraces",
			sosrc: SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 11",
				Hostname:     "sample_host",
			},
			want: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.sosrc.oteLogger = defaultOTELogger
			got := test.sosrc.validateParams()
			if len(got) != len(test.want) || !slices.Equal(got, test.want) {
				t.Errorf("validateParams() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSOSReportHandler(t *testing.T) {
	tests := []struct {
		name           string
		sosr           *SupportBundle
		ctx            context.Context
		destFilePrefix string
		exec           commandlineexecutor.Execute
		fs             filesystem.FileSystem
		z              zipper.Zipper
		wantMessage    string
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name: "InvalidParams",
			sosr: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "",
				Hostname:     "sample_host",
			},
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{},
			z:              mockedZipper{},
			wantMessage:    "Invalid params for collecting support bundle Report for Agent for SAP: no value provided for instance-numbers",
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "MkdirError",
			sosr: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 11",
				Hostname:     "sample_host",
			},
			destFilePrefix: "failure",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{},
			z:              mockedZipper{},
			wantMessage:    "Error while making directory",
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "FaultInExtractingErrors",
			sosr: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 11",
				Hostname:     "sample_host",
			},
			destFilePrefix: "samplefile",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{reqErr: os.ErrInvalid},
			z:              mockedZipper{},
			wantMessage:    "Error while extracting system DB errors\nError while extracting tenant DB errors\nError while extracting journalctl logs\nError while copying var log messages to bundle\nError while extracting HANA version\nError while fetching package info\nError while fetching OS processes\nError while fetching systemd services\nError while copying file: /etc/google-cloud-sap-agent/configuration.json\nError while copying file: /usr/sap/DEH/SYS/global/hdb/custom/config/global.ini",
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "Success",
			sosr: &SupportBundle{
				Sid:           "DEH",
				InstanceNums:  "00 11",
				Hostname:      "sample_host",
				AgentLogsOnly: true,
			},
			destFilePrefix: "samplefile",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{
						name: "samplefile",
						mode: 0777,
					},
				},
			},
			z:              mockedZipper{},
			wantMessage:    "Zipped destination support bundle file HANA/Backint",
			wantExitStatus: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForPacemakerDiagnosis",
			sosr: &SupportBundle{
				Sid:                "DEH",
				InstanceNums:       "00 11",
				Hostname:           "sample_host",
				AgentLogsOnly:      true,
				PacemakerDiagnosis: true,
			},
			destFilePrefix: "samplefile",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{
						name: "samplefile",
						mode: 0777,
					},
				},
			},
			z:              mockedZipper{},
			wantMessage:    "Pacemaker logs are collected and sent to directory",
			wantExitStatus: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.sosr.oteLogger = defaultOTELogger
			message, exitStatus := test.sosr.supportBundleHandler(test.ctx, test.destFilePrefix, test.exec, test.fs, test.z, defaultCloudProperties)
			if !strings.Contains(message, test.wantMessage) || exitStatus != test.wantExitStatus {
				t.Errorf("sosReportHandler() = %v, %v; want %v, %v", message, exitStatus, test.wantMessage, test.wantExitStatus)
			}
		})
	}
}

func TestExtractErrorsUsingGrep(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name     string
		ctx      context.Context
		destFile string
		hostname string
		exec     commandlineexecutor.Execute
		p        commandlineexecutor.Params
		opFile   string
		fu       filesystem.FileSystem
		want     error
	}{
		{
			name:     "CommandFailure",
			ctx:      context.Background(),
			destFile: "sampleFile",
			hostname: "sampleHost",
			exec:     fakeExec,
			p:        commandlineexecutor.Params{ArgsToSplit: "error"},
			opFile:   "sampleOpFile",
			fu:       mockedfilesystem{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "OpenFileFailure",
			ctx:      context.Background(),
			destFile: "failure",
			hostname: "sampleHost",
			exec:     fakeExec,
			p:        commandlineexecutor.Params{ArgsToSplit: "success"},
			opFile:   "sampleOpFile",
			fu:       mockedfilesystem{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "StringWritingFailure",
			ctx:      context.Background(),
			destFile: "sampleFile",
			hostname: "sampleHost",
			exec:     fakeExec,
			p:        commandlineexecutor.Params{ArgsToSplit: "success"},
			opFile:   "nil-file",
			fu:       mockedfilesystem{},
			want:     cmpopts.AnyError,
		},
		{
			name:     "StringWritingSuccess",
			ctx:      context.Background(),
			destFile: "sampleFile",
			hostname: "sampleHost",
			exec:     fakeExec,
			p:        commandlineexecutor.Params{ArgsToSplit: "success"},
			opFile:   "sampleOpFile",
			fu:       mockedfilesystem{},
			want:     nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.execAndWriteToFile(test.ctx, test.destFile, test.hostname, test.exec, test.p, test.opFile, test.fu)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("extractErrorsUsingGREP() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractSystemDBErrors(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name      string
		destFile  string
		hostname  string
		hanaPaths []string
		exec      commandlineexecutor.Execute
		fs        filesystem.FileSystem
		want      bool
	}{
		{
			name:      "HasErrors",
			destFile:  "failure",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"file1", "file2"},
			fs:        mockedfilesystem{reqErr: os.ErrInvalid},
			want:      true,
		},
		{
			name:      "FileDoesNotExists",
			destFile:  "sampleFile",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"does-not-exist", "file2"},
			fs:        mockedfilesystem{},
			want:      false,
		},
		{
			name:      "NoErrors",
			destFile:  "sampleFile",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"file1", "file2"},
			fs:        mockedfilesystem{},
			want:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.extractSystemDBErrors(context.Background(), test.destFile, test.hostname, test.hanaPaths, test.exec, test.fs); got != test.want {
				t.Errorf("extractSystemDBErrors() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractTenantDBErrors(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name      string
		destFile  string
		hostname  string
		hanaPaths []string
		exec      commandlineexecutor.Execute
		fs        filesystem.FileSystem
		want      bool
	}{
		{
			name:      "HasErrors",
			destFile:  "failure",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"file1", "failure"},
			fs:        mockedfilesystem{reqErr: os.ErrInvalid},
			want:      true,
		},
		{
			name:      "FileDoesNotExists",
			destFile:  "sampleFile",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"does-not-exist", "file2"},
			fs:        mockedfilesystem{},
			want:      false,
		},
		{
			name:      "NoErrors",
			destFile:  "sampleFile",
			hostname:  "sampleHost",
			exec:      fakeExec,
			hanaPaths: []string{"file1", "file2"},
			fs:        mockedfilesystem{},
			want:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.extractTenantDBErrors(context.Background(), test.destFile, "DEH", test.hostname, test.hanaPaths, test.exec, test.fs); got != test.want {
				t.Errorf("extractTenantDBErrors() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPacemakerLogFiles(t *testing.T) {
	tests := []struct {
		name              string
		s                 *SupportBundle
		linuxLogFilesPath string
		fu                filesystem.FileSystem
		want              []string
	}{
		{
			name: "ReadDirError",
			s: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 11",
				Hostname:     "sample_host",
			},
			linuxLogFilesPath: "failure",
			fu:                mockedfilesystem{},
			want:              nil,
		},
		{
			name: "Success",
			s: &SupportBundle{
				Sid:          "DEH",
				InstanceNums: "00 11",
				Hostname:     "sample_host",
			},
			linuxLogFilesPath: "success",
			fu: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{name: "bundles", isDir: true},
					mockedFileInfo{name: "pacemaker.log", isDir: false},
					mockedFileInfo{name: "pacemaker.log-20200304.gz", isDir: false},
				},
			},
			want: []string{"success/pacemaker/pacemaker.log", "success/pacemaker/pacemaker.log-20200304.gz"},
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got := tc.s.pacemakerLogFiles(ctx, tc.linuxLogFilesPath, tc.fu)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("pacemakerLogFiles(%q, %v) returned an unexpected diff (-want +got): %v", tc.linuxLogFilesPath, tc.fu, diff)
			}
		})
	}
}

func TestExtractJournalCTLLogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name     string
		destFile string
		hostname string
		exec     commandlineexecutor.Execute
		fs       filesystem.FileSystem
		want     bool
	}{
		{
			name:     "HasErrors",
			destFile: "failure",
			hostname: "sampleHost",
			exec:     fakeExec,
			fs:       mockedfilesystem{reqErr: os.ErrInvalid},
			want:     true,
		},
		{
			name:     "NoErrors",
			destFile: "sampleFile",
			hostname: "sampleHost",
			exec:     fakeExec,
			fs:       mockedfilesystem{},
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.extractJournalCTLLogs(context.Background(), test.destFile, test.hostname, test.exec, test.fs); got != test.want {
				t.Errorf("extractJournalCTLLogs() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractVarLogMessages(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name     string
		destFile string
		hostname string
		fs       filesystem.FileSystem
		want     bool
	}{
		{
			name:     "HasErrorsInOpen",
			destFile: "failure",
			hostname: "sampleHost",
			fs: &fake.FileSystem{
				OpenResp: []*os.File{nil},
				OpenErr:  []error{os.ErrInvalid},
			},
			want: true,
		},
		{
			name:     "HasErrorsInOpenFile",
			destFile: "sampleFile",
			hostname: "sampleHost",
			fs: &fake.FileSystem{
				OpenResp:     []*os.File{&os.File{}},
				OpenErr:      []error{nil},
				OpenFileResp: []*os.File{nil},
				OpenFileErr:  []error{os.ErrInvalid},
			},
			want: true,
		},
		{
			name:     "HasErrorInCopy",
			destFile: "sampleFile",
			hostname: "sampleHost",
			fs: &fake.FileSystem{
				OpenResp:     []*os.File{&os.File{}},
				OpenErr:      []error{nil},
				OpenFileResp: []*os.File{&os.File{}},
				OpenFileErr:  []error{nil},
				CopyResp:     []int64{0},
				CopyErr:      []error{os.ErrInvalid},
			},
			want: true,
		},
		{
			name:     "NoErrors",
			destFile: "sampleFile",
			hostname: "sampleHost",
			fs: &fake.FileSystem{
				OpenResp:     []*os.File{&os.File{}},
				OpenErr:      []error{nil},
				OpenFileResp: []*os.File{&os.File{}},
				OpenFileErr:  []error{nil},
				CopyResp:     []int64{100},
				CopyErr:      []error{nil},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.copyVarLogMessagesToBundle(context.Background(), test.destFile, test.hostname, test.fs); got != test.want {
				t.Errorf("copyVarLogMessagesToBundle() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCollectMessagesLogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		destFilesPath string
		fs            filesystem.FileSystem
		want          []string
	}{
		{
			name:          "ReadDirError",
			destFilesPath: "failure",
			fs:            mockedfilesystem{},
			want:          nil,
		},
		{
			name:          "Success",
			destFilesPath: "success",
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{name: "bundles", isDir: true},
					mockedFileInfo{name: "messages", isDir: false},
					mockedFileInfo{name: "messages-20200304", isDir: false},
				},
			},
			want: []string{"success/messages", "success/messages-20200304"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.collectMessagesLogs(context.Background(), test.destFilesPath, test.fs); !cmp.Equal(got, test.want) {
				t.Errorf("collectMessagesLogs() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractBackintErrors(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name       string
		destFile   string
		globalPath string
		hostname   string
		exec       commandlineexecutor.Execute
		fs         filesystem.FileSystem
		want       bool
	}{
		{
			name:       "ReadDirError",
			destFile:   "sampleFile",
			hostname:   "sampleHost",
			exec:       fakeExec,
			globalPath: "failure",
			fs:         mockedfilesystem{},
			want:       true,
		},
		{
			name:       "HasError",
			destFile:   "sampleFile",
			hostname:   "sampleHost",
			exec:       fakeExec,
			globalPath: "globalPath",
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{mockedFileInfo{name: "file1"}, mockedFileInfo{name: "failure"}},
				reqErr:         os.ErrInvalid,
			},
			want: true,
		},
		{
			name:       "Success",
			destFile:   "sampleFile",
			hostname:   "sampleHost",
			exec:       fakeExec,
			globalPath: "globalPath",
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{mockedFileInfo{name: "file1"}, mockedFileInfo{name: "failure"}},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.extractBackintErrors(context.Background(), test.destFile, test.globalPath, test.hostname, test.exec, test.fs); got != test.want {
				t.Errorf("extractBackintErrors() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWalkAndZip(t *testing.T) {
	tests := []struct {
		name string
		fs   filesystem.FileSystem
		z    zipper.Zipper
		zw   *zip.Writer
		want error
	}{
		{
			name: "FileInfoHeaderError",
			fs:   mockedfilesystem{reqErr: os.ErrPermission},
			z:    mockedZipper{fileInfoErr: os.ErrPermission},
			zw:   &zip.Writer{},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateHeaderError",
			fs:   mockedfilesystem{},
			z:    mockedZipper{createHeaderErr: os.ErrPermission},
			zw:   &zip.Writer{},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			fs:   mockedfilesystem{},
			z:    mockedZipper{},
			zw:   &zip.Writer{},
			want: nil,
		},
	}
	tmpDir, _ := ioutil.TempDir("", "testDir")
	tmpfile, _ := ioutil.TempFile(tmpDir, "testfile")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.fs.WalkAndZip(tmpDir, test.z, test.zw)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("WalkAndZip() = %v, want %v", got, test.want)
			}
		})
	}
	os.Remove(tmpfile.Name())
	os.RemoveAll(tmpDir)
}

func TestNameservertraceAndBackupLog(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name     string
		hanaPath []string
		sid      string
		fu       filesystem.FileSystem
		want     []string
	}{
		{
			name:     "ReadFileError",
			hanaPath: []string{"failure"},
			sid:      "DEH",
			fu:       mockedfilesystem{},
			want:     nil,
		},
		{
			name:     "NoMatch",
			hanaPath: []string{"success"},
			sid:      "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "file1", isDir: true},
			},
			},
			want: []string{},
		},
		{
			name:     "Success",
			hanaPath: []string{"success"},
			sid:      "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "backup.log", isDir: false},
			},
			},
			want: []string{path.Join("success/trace", "backup.log")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.nameServerTracesAndBackupLogs(context.Background(), test.hanaPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("nameServerTracesAndBackupLog(%q, %q, %q)=%q, want %q", test.hanaPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestTenantDBNameservertraceAndBackupLog(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name     string
		hanaPath []string
		sid      string
		fu       filesystem.FileSystem
		want     []string
	}{
		{
			name:     "ReadFileError",
			hanaPath: []string{"failure"},
			sid:      "DEH",
			fu:       mockedfilesystem{},
			want:     nil,
		},
		{
			name:     "NoMatch",
			hanaPath: []string{"success"},
			sid:      "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "file1", isDir: true},
			},
			},
			want: []string{},
		},
		{
			name:     "Success",
			hanaPath: []string{"success"},
			sid:      "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "backup.log", isDir: false},
			},
			},
			want: []string{path.Join("success/trace/DB_DEH/", "backup.log")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.tenantDBNameServerTracesAndBackupLogs(context.Background(), test.hanaPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("nameServerTracesAndBackupLog(%q, %q, %q)=%q, want %q", test.hanaPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestBackintParameterFiles(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name       string
		globalPath string
		sid        string
		fu         filesystem.FileSystem
		want       []string
	}{
		{
			name:       "ReadFileError",
			globalPath: "failure",
			sid:        "DEH",
			fu:         mockedfilesystem{},
			want:       nil,
		},
		{
			name:       "UnexpectedContent",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{
				fileContent: `_backup_parameter_file = /usr/sap/file1
				xyz_backup_parameter_file = /usr/sap/file2
				abc_backup_parameter_file
				`,
			},
			want: []string{path.Join("success", globalINIFile), "/usr/sap/file1", "/usr/sap/file2"},
		},
		{
			name:       "Success",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{
				fileContent: `_backup_parameter_file = /usr/sap/file1`,
			},
			want: []string{path.Join("success", globalINIFile), "/usr/sap/file1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.backintParameterFiles(context.Background(), test.globalPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("backintParameterFiles(%q, %q, %q) = %q, want %q", test.globalPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestBackintlogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name       string
		globalPath string
		sid        string
		fu         filesystem.FileSystem
		want       []string
	}{
		{
			name:       "ReadDirError",
			globalPath: "failure",
			sid:        "DEH",
			fu:         mockedfilesystem{},
			want:       nil,
		},
		{
			name:       "NoMatch",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "file1", isDir: true},
			},
			},
			want: []string{},
		},
		{
			name:       "InstallationLog",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "installation.log", isDir: false},
			},
			},
			want: []string{path.Join("success", backintGCSPath, "installation.log")},
		},
		{
			name:       "logsFile",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "logs", isDir: false},
			}},
			want: []string{path.Join("success", backintGCSPath, "logs")},
		},
		{
			name:       "Version.txt",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "VERSION.txt", isDir: false},
			}},
			want: []string{path.Join("success", backintGCSPath, "VERSION.txt")},
		},
		{
			name:       "loggingPropertiesFile",
			globalPath: "success",
			sid:        "DEH",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{name: "logging.properties", isDir: false},
			}},
			want: []string{path.Join("success", backintGCSPath, "logging.properties")},
		},
	}

	for _, test := range tests {
		got := sosr.backintLogs(context.Background(), test.globalPath, test.sid, test.fu)
		if !cmp.Equal(got, test.want) {
			t.Errorf("BackIntLogs(%q, %q, %q) = %q, want %q", test.globalPath, test.sid, test.fu, got, test.want)
		}
	}
}

func TestAgentLogsFiles(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name string
		path string
		fu   filesystem.FileSystem
		want []string
	}{
		{
			name: "ReadDirError",
			path: "failure",
			fu:   mockedfilesystem{},
			want: []string{},
		},
		{
			name: "ReadDirSuccess",
			path: "success",
			fu: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{
						name:  "google-cloud-sap-agent.log",
						isDir: false,
					},
					mockedFileInfo{
						name:  "google-cloud-dir",
						isDir: true,
					},
				},
			},
			want: []string{"success/google-cloud-sap-agent.log"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.agentLogFiles(context.Background(), test.path, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("agentLogFiles(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	tests := []struct {
		name string
		src  string
		dest string
		fu   filesystem.FileSystem
		want error
	}{
		{
			name: "MkdirError",
			src:  "sampleFile",
			dest: "failure",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "SourceFileOpenError",
			src:  "failure",
			dest: "dest",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "CreateError",
			src:  "sampleFile",
			dest: "failure",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "CopyError",
			src:  "sampleFile",
			dest: "failure",
			fu:   mockedfilesystem{copyErr: cmpopts.AnyError},
			want: cmpopts.AnyError,
		},
		{
			name: "OsStatError",
			src:  "failure",
			dest: "destFile",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "ChmodError",
			src:  "sampleFile",
			dest: "failure",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "CopySuccess",
			src:  "sampleFile",
			dest: "destFile",
			fu: mockedfilesystem{readDirContent: []fs.FileInfo{
				mockedFileInfo{
					name:  "destFile",
					mode:  0777,
					isDir: false,
				},
			},
			},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := copyFile(test.src, test.dest, test.fu)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("copyFile(%q, %q) = %v, want %v", test.src, test.dest, got, test.want)
			}
		})
	}
}

func TestZipSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		target string
		fu     filesystem.FileSystem
		z      zipper.Zipper
		want   error
	}{
		{
			name:   "CreateError",
			source: "failure",
			target: "failure",
			fu:     mockedfilesystem{reqErr: cmpopts.AnyError},
			z:      mockedZipper{},
			want:   cmpopts.AnyError,
		},
		{
			name:   "WalkAndZipError",
			source: "failure",
			target: "destFile",
			fu:     mockedfilesystem{},
			z:      mockedZipper{},
			want:   cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := zipSource(test.source, test.target, test.fu, test.z)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("zipSource(%q, %q) = %v, want %v", test.source, test.target, got, test.want)
			}
		})
	}
}

func TestRemoveDestinationFolder(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name string
		path string
		fu   filesystem.FileSystem
		want error
	}{
		{
			name: "ErrorWhileRemoving",
			path: "failure",
			fu:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			path: "success",
			fu:   mockedfilesystem{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.removeDestinationFolder(context.Background(), test.path, test.fu)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("removeDestinationFolder(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestRotateOldBundles(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name string
		dir  string
		fs   filesystem.FileSystem
		want error
	}{
		{
			name: "ErrorWhileReading",
			dir:  "failure",
			fs:   mockedfilesystem{},
			want: cmpopts.AnyError,
		},
		{
			name: "SuccessNoFiles",
			dir:  "success",
			fs:   mockedfilesystem{},
		},
		{
			name: "SuccessMultipleFiles",
			dir:  "success",
			fs: mockedfilesystem{
				readDirContent: []fs.FileInfo{
					mockedFileInfo{name: "supportbundle1"},
					mockedFileInfo{name: "supportbundle2"},
					mockedFileInfo{name: "supportbundle3"},
					mockedFileInfo{name: "supportbundle4"},
					mockedFileInfo{name: "supportbundle5"},
					mockedFileInfo{name: "supportbundle6"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.rotateOldBundles(context.Background(), test.dir, test.fs)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("rotateOldBundles(%q) = %v, want %v", test.dir, got, test.want)
			}
		})
	}
}

func TestCollectPacemakerLogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name         string
		ctx          context.Context
		destFilePath string
		exec         commandlineexecutor.Execute
		fs           filesystem.FileSystem
		want         error
	}{
		{
			name:         "InvalidOS",
			ctx:          context.Background(),
			destFilePath: "sample",
			exec:         fakeExecForErrOnly,
			fs:           mockedfilesystem{},
			want:         cmpopts.AnyError,
		},
		{
			name:         "RHELError",
			ctx:          context.Background(),
			destFilePath: "failure",
			exec:         fakeExecRHEL,
			fs:           mockedfilesystem{},
			want:         cmpopts.AnyError,
		},
		{
			name:         "SLESError",
			ctx:          context.Background(),
			destFilePath: "failure",
			exec:         fakeExecSLES,
			fs:           mockedfilesystem{},
			want:         cmpopts.AnyError,
		},
		{
			name:         "SuccessRHEL",
			ctx:          context.Background(),
			destFilePath: "sample",
			exec:         fakeExecRHEL,
			fs:           mockedfilesystem{},
			want:         nil,
		},
		{
			name:         "SuccessSLES",
			ctx:          context.Background(),
			destFilePath: "sample",
			exec:         fakeExecSLES,
			fs:           mockedfilesystem{},
			want:         nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sosr.pacemakerLogs(test.ctx, test.destFilePath, test.exec, test.fs)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectPacemakerLogs(%q) = %v, want %v", test.destFilePath, got, test.want)
			}
		})
	}
}

func TestCollectRHELPacemakerLogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		ctx           context.Context
		exec          commandlineexecutor.Execute
		p             commandlineexecutor.Params
		destFilesPath string
		fs            filesystem.FileSystem
		want          error
	}{
		{
			name:          "MkdirError",
			ctx:           context.Background(),
			exec:          fakeExec,
			p:             commandlineexecutor.Params{},
			destFilesPath: "failure",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "CommandFailure",
			ctx:           context.Background(),
			exec:          fakeExec,
			p:             commandlineexecutor.Params{ArgsToSplit: "error"},
			destFilesPath: "failure",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "AllCommandsFailure",
			ctx:           context.Background(),
			exec:          fakeExecForErrOnly,
			p:             commandlineexecutor.Params{ArgsToSplit: "failure"},
			destFilesPath: "success",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "Success",
			ctx:           context.Background(),
			exec:          fakeExec,
			p:             commandlineexecutor.Params{ArgsToSplit: "success"},
			destFilesPath: "success",
			fs:            mockedfilesystem{},
			want:          nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.rhelPacemakerLogs(test.ctx, test.exec, test.destFilesPath, test.fs); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectRHELPacemakerLogs(%q) = %v, want %v", test.destFilesPath, got, test.want)
			}
		})
	}
}

func TestExtractHANAVersion(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		destFilesPath string
		sid           string
		hostname      string
		exec          commandlineexecutor.Execute
		fu            filesystem.FileSystem
		want          bool
	}{
		{
			name:          "NoErrors",
			destFilesPath: "tmppath",
			sid:           "deh",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			want:          false,
		},
		{
			name:          "HasErrors",
			destFilesPath: "tmppath",
			sid:           "deh",
			hostname:      "testhost",
			exec:          fakeExecForErrOnly,
			fu:            mockedfilesystem{},
			want:          true,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		got := sosr.extractHANAVersion(ctx, tc.destFilesPath, tc.sid, tc.hostname, tc.exec, tc.fu)
		if got != tc.want {
			t.Errorf("extractHANAVersion(%v, %v, %v, %v, %v) = %v, want: %v", tc.destFilesPath, tc.sid, tc.hostname, tc.exec, tc.fu, got, tc.want)
		}
	}
}

func TestCollectSLESPacemakerLogs(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		ctx           context.Context
		exec          commandlineexecutor.Execute
		destFilesPath string
		fs            filesystem.FileSystem
		want          error
	}{
		{
			name:          "MkdirError",
			ctx:           context.Background(),
			exec:          fakeExec,
			destFilesPath: "failure",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "AllCommandsFailure",
			ctx:           context.Background(),
			exec:          fakeExecForErrOnly,
			destFilesPath: "failure",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "HBReportSuccess",
			ctx:           context.Background(),
			exec:          fakeExecSLESHBSuccess,
			destFilesPath: "success",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "CRMReportSuccess",
			ctx:           context.Background(),
			exec:          fakeExecSLESCRMSuccess,
			destFilesPath: "success",
			fs:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "SupportConfigSuccess",
			ctx:           context.Background(),
			exec:          fakeExecSLES,
			destFilesPath: "success",
			fs:            mockedfilesystem{},
			want:          nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.slesPacemakerLogs(test.ctx, test.exec, test.destFilesPath, test.fs); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectSLESPacemakerLogs(%q) = %v, want %v", test.destFilesPath, got, test.want)
			}
		})
	}
}

func TestUploadZip(t *testing.T) {
	tests := []struct {
		name          string
		sb            *SupportBundle
		destFilesPath string
		ctb           storage.BucketConnector
		grw           getReaderWriter
		fs            filesystem.FileSystem
		wantErr       error
	}{
		{
			name: "OpenFail",
			sb: &SupportBundle{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "failure",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
				return &st.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			fs: &fake.FileSystem{
				OpenErr:  []error{fmt.Errorf("error")},
				OpenResp: []*os.File{nil},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "StatFail",
			sb: &SupportBundle{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
				return &st.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			fs: &fake.FileSystem{
				OpenErr:  []error{nil},
				OpenResp: []*os.File{&os.File{}},
				StatErr:  []error{fmt.Errorf("error")},
				StatResp: []os.FileInfo{nil},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ConnectToBucketFail",
			sb: &SupportBundle{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
				return nil, false
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			fs: &fake.FileSystem{
				OpenErr:  []error{nil},
				OpenResp: []*os.File{&os.File{}},
				StatErr:  []error{nil},
				StatResp: []os.FileInfo{
					mockedFileInfo{name: "samplefile", mode: 0777},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UploadFail",
			sb: &SupportBundle{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
				return &st.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{
					err: fmt.Errorf("error"),
				}
			},
			fs: &fake.FileSystem{
				OpenErr:  []error{nil},
				OpenResp: []*os.File{&os.File{}},
				StatErr:  []error{nil},
				StatResp: []os.FileInfo{
					mockedFileInfo{name: "samplefile", mode: 0777},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UploadSuccess",
			sb: &SupportBundle{
				ResultBucket: "test_bucket",
			},
			destFilesPath: "sampleFile",
			ctb: func(ctx context.Context, p *storage.ConnectParameters) (*st.BucketHandle, bool) {
				return &st.BucketHandle{}, true
			},
			grw: func(rw storage.ReadWriter) uploader {
				return &fakeReadWriter{}
			},
			fs: &fake.FileSystem{
				OpenErr:  []error{nil},
				OpenResp: []*os.File{&os.File{}},
				StatErr:  []error{nil},
				StatResp: []os.FileInfo{
					mockedFileInfo{name: "samplefile", mode: 0777},
				},
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.sb.oteLogger = defaultOTELogger
			gotErr := tc.sb.uploadZip(ctx, tc.destFilesPath, "bundle", tc.ctb, tc.grw, tc.fs, st.NewClient)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("uploadZip(%q, %v, %v) returned an unexpected error: %v", tc.destFilesPath, tc.ctb, tc.grw, diff)
			}
		})
	}
}

func TestFetchPackageInfo(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		destFilesPath string
		hostname      string
		exec          commandlineexecutor.Execute
		fu            filesystem.FileSystem
		want          error
	}{
		{
			name:          "AllCommandsFailure",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExecForErrOnly,
			fu:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "FileCreationError",
			destFilesPath: "failure",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			want:          cmpopts.AnyError,
		},
		{
			name:          "Success",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			want:          nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sosr.fetchPackageInfo(context.Background(), test.destFilesPath, test.hostname, test.exec, test.fu); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("fetchPackageInfo(%q, %q, %v, %v) = %v, want %v", test.destFilesPath, test.hostname, test.exec, test.fu, got, test.want)
			}
		})
	}
}

func TestFetchOSProcessesErrors(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		destFilesPath string
		hostname      string
		exec          commandlineexecutor.Execute
		fu            filesystem.FileSystem
		wantErr       error
	}{
		{
			name:          "CommandFailure",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExecForErrOnly,
			fu:            mockedfilesystem{},
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "FileCreationError",
			destFilesPath: "failure",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "Success",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			wantErr:       nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if gotErr := sosr.fetchOSProcesses(ctx, tc.destFilesPath, tc.hostname, tc.exec, tc.fu); !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("fetchOSProcesses(%q, %q, %v, %v) returned an unexpected error: %v", tc.destFilesPath, tc.hostname, tc.exec, tc.fu, gotErr)
			}
		})
	}
}

func TestFetchSystemDServicesErrors(t *testing.T) {
	sosr := SupportBundle{
		oteLogger: defaultOTELogger,
	}
	tests := []struct {
		name          string
		destFilesPath string
		hostname      string
		exec          commandlineexecutor.Execute
		fu            filesystem.FileSystem
		wantErr       error
	}{
		{
			name:          "CommandFailure",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExecForErrOnly,
			fu:            mockedfilesystem{},
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "FileCreationError",
			destFilesPath: "failure",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			wantErr:       cmpopts.AnyError,
		},
		{
			name:          "Success",
			destFilesPath: "samplePath",
			hostname:      "testhost",
			exec:          fakeExec,
			fu:            mockedfilesystem{},
			wantErr:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if gotErr := sosr.fetchSystemDServices(context.Background(), tc.destFilesPath, tc.hostname, tc.exec, tc.fu); !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("fetchSystemDServices(%q, %q, %v, %v) returned an unexpected error: %v", tc.destFilesPath, tc.hostname, tc.exec, tc.fu, gotErr)
			}
		})
	}
}

func TestCollectSapDiscoveryErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/success":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, sampleDiscoveryResponse)
		case "/test/error":
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		case "/test/empty_entries_1":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"nextPageToken": "eo8BCooBAfQucPiUM0WFCK4rapeQ61o35Yt3tvyb0ijSOsy2KD5jfiyCEK2A3ekvvpNJhKPP5HMnc0fQlBLPXxtwSRbu2g2rIMv7cBGVxRaNAtPgW4YSo6E1UTRzS-wNX7-wLaRw2EHeyRzVeHhMAahmcoFBDaprD453wWtjVsMbYCOelakVjDN7wZhqyQ4gEAA"}`)
		case "/test/empty_entries_2":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
		case "/test/empty_entries_3":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"entries": []"}`)
		case "/test/query_cloud_logging_error":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"error": {"code": 404, "message": "Project does not exist: core-connect-dev-a", "status": "NOT_FOUND"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name               string
		s                  *SupportBundle
		baseURL            string
		destFilePathPrefix string
		cp                 *ipb.CloudProperties
		fs                 filesystem.FileSystem
		wantErr            error
	}{
		{
			name: "QueryCloudLoggingError",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/error",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "EmptyEntriesError1",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/empty_entries_1",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "EmptyEntriesError2",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/empty_entries_2",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "EmptyEntriesError3",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/empty_entries_3",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "CreateFileError",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/success",
			destFilePathPrefix: "failure",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/success",
			destFilePathPrefix: "sap-discovery-success",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			wantErr:            nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			gotErr := tc.s.collectSapDiscovery(ctx, tc.baseURL, tc.destFilePathPrefix, tc.cp, tc.fs)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("collectSapDiscovery(%q, %v, %v) returned an unexpected error: %v", tc.destFilePathPrefix, tc.cp, tc.fs, diff)
			}
		})
	}
}

func TestQueryCloudLogging(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/success":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, sampleDiscoveryResponse)
		case "/test/error":
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		case "/test/error_response":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"error": {"code": 404, "message": "Project does not exist: core-connect-dev-a", "status": "NOT_FOUND"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name      string
		s         *SupportBundle
		baseURL   string
		filter    string
		project   string
		wantBytes []byte
		wantError error
	}{
		{
			name: "GetResponseError",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:   ts.URL + "/test/error",
			filter:    "test",
			project:   "test-project",
			wantBytes: nil,
			wantError: cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:   ts.URL + "/test/success",
			filter:    "test",
			project:   "test-project",
			wantBytes: []byte(sampleDiscoveryResponse),
			wantError: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			request := CloudLoggingRequest{
				ResourceNames: []string{fmt.Sprintf("projects/%s", tc.project)},
				Filter:        tc.filter,
			}
			got, err := tc.s.queryCloudLogging(ctx, request, tc.baseURL)
			if diff := cmp.Diff(got, tc.wantBytes); diff != "" {
				t.Errorf("queryCloudLogging() returned diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(err, tc.wantError, cmpopts.EquateErrors()) {
				t.Errorf("queryCloudLogging(%q) returned an unexpected error: %v", tc.filter, err)
			}
		})
	}
}

func TestCollectSapEvents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/success":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, sampleDiscoveryResponse)
		case "/test/error":
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			conn.Close()
		case "/test/unmarshalling_error":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "{value: invalid_json}")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	tests := []struct {
		name               string
		s                  *SupportBundle
		baseURL            string
		destFilePathPrefix string
		cp                 *ipb.CloudProperties
		fs                 filesystem.FileSystem
		exec               commandlineexecutor.Execute
		wantErr            error
	}{
		{
			name:               "ParseTimeInLocationError",
			s:                  &SupportBundle{Timestamp: "2025-01-01 00:00:00"},
			baseURL:            ts.URL + "/test/error",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			exec:               wrongTimezoneExec,
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "QueryCloudLoggingError",
			s: &SupportBundle{
				Timestamp:      "2025-01-01 00:00:00",
				AfterDuration:  1800,
				BeforeDuration: 3600,
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/error",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			exec:               successfulTimezoneExec,
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "JSONUnmarshallingError",
			s: &SupportBundle{
				Timestamp:      "2025-01-01 00:00:00",
				AfterDuration:  1800,
				BeforeDuration: 3600,
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/unmarshalling_error",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			exec:               successfulTimezoneExec,
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "MkDirError",
			s: &SupportBundle{
				Timestamp:      "2025-01-01 00:00:00",
				AfterDuration:  1800,
				BeforeDuration: 3600,
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/success",
			destFilePathPrefix: "failure",
			cp:                 defaultCloudProperties,
			fs:                 mockedfilesystem{},
			exec:               successfulTimezoneExec,
			wantErr:            cmpopts.AnyError,
		},
		{
			name: "FileCreateError",
			s: &SupportBundle{
				Timestamp:      "2025-01-01 00:00:00",
				AfterDuration:  1800,
				BeforeDuration: 3600,
				rest: &rest.Rest{
					HTTPClient: defaultNewClient(10*time.Minute, defaultTransport()),
					TokenGetter: func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
						return &mockToken{
							token: &oauth2.Token{
								AccessToken: "access-token",
							},
							err: nil,
						}, nil
					},
				},
			},
			baseURL:            ts.URL + "/test/success",
			destFilePathPrefix: "test",
			cp:                 defaultCloudProperties,
			fs: &fake.FileSystem{
				MkDirErr:   []error{nil},
				CreateResp: []*os.File{nil},
				CreateErr:  []error{cmpopts.AnyError},
			},
			exec:    successfulTimezoneExec,
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			gotErr := tc.s.collectSapEvents(ctx, tc.baseURL, tc.destFilePathPrefix, tc.cp, tc.fs, tc.exec)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("collectSapEvents() returned an unexpected error: %v", diff)
			}
		})
	}
}

func TestCollectProcessMetrics(t *testing.T) {
	tests := []struct {
		name              string
		s                 *SupportBundle
		exec              commandlineexecutor.Execute
		metrics           []string
		destFilesPath     string
		metricsType       string
		cp                *ipb.CloudProperties
		fs                filesystem.FileSystem
		wantNonZeroLength bool
	}{
		{
			name: "GetProcessMetricsClientsError",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return nil, cmpopts.AnyError
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{}, nil
				},
			},
			exec:              successfulTimezoneExec,
			metrics:           []string{"workload.googleapis.com/test/cpu/utilization"},
			metricsType:       "process_metrics",
			destFilesPath:     "/tmp",
			cp:                defaultCloudProperties,
			fs:                mockedfilesystem{},
			wantNonZeroLength: true,
		},
		{
			name: "MkdirError",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{}, nil
				},
			},
			exec:              successfulTimezoneExec,
			metrics:           []string{"workload.googleapis.com/test/cpu/utilization"},
			destFilesPath:     "failure",
			metricsType:       "hana_monitoring_metrics",
			cp:                defaultCloudProperties,
			fs:                mockedfilesystem{},
			wantNonZeroLength: true,
		},
		{
			name: "GetTimezoneError",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{
						TS:  nil,
						Err: cmpopts.AnyError,
					}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{
						ResourceDescriptor:    nil,
						ResourceDescriptorErr: cmpopts.AnyError,
						MetricDescriptor:      nil,
						MetricDescriptorErr:   cmpopts.AnyError,
					}, nil
				},
				Timestamp: "2025-01-01 00:00:00",
			},
			exec:              failedTimezoneExec,
			metrics:           []string{"workload.googleapis.com/test/cpu/utilization"},
			destFilesPath:     "/tmp",
			metricsType:       "process_metrics",
			cp:                defaultCloudProperties,
			fs:                mockedfilesystem{},
			wantNonZeroLength: true,
		},
		{
			name: "FetchTimeSeriesDataError",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{
						TS:  nil,
						Err: cmpopts.AnyError,
					}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{
						ResourceDescriptor:    nil,
						ResourceDescriptorErr: cmpopts.AnyError,
						MetricDescriptor:      nil,
						MetricDescriptorErr:   cmpopts.AnyError,
					}, nil
				},
				Timestamp: "2025-01-01 00:00:00",
			},
			exec:              successfulTimezoneExec,
			metrics:           []string{"workload.googleapis.com/test/cpu/utilization"},
			destFilesPath:     "/tmp",
			metricsType:       "process_metrics",
			cp:                defaultCloudProperties,
			fs:                mockedfilesystem{},
			wantNonZeroLength: true,
		},
		{
			name: "CreateFileError",
			s: &SupportBundle{
				Timestamp: "2025-01-01 00:00:00",
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{
						TS: []*mrpb.TimeSeriesData{
							&mrpb.TimeSeriesData{
								LabelValues: []*mrpb.LabelValue{
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-project",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-instance-id",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-zone",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-process",
										},
									},
								},
								PointData: []*mrpb.TimeSeriesData_PointData{
									&mrpb.TimeSeriesData_PointData{
										Values: []*cpb.TypedValue{
											&cpb.TypedValue{
												Value: &cpb.TypedValue_DoubleValue{
													DoubleValue: 123.456,
												},
											},
										},
										TimeInterval: &cpb.TimeInterval{
											StartTime: &tpb.Timestamp{
												Seconds: 1744790182,
											},
											EndTime: &tpb.Timestamp{
												Seconds: 1744790182,
											},
										},
									},
								},
							},
						},
						Err: nil,
					}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{
						MetricDescriptor: &metricpb.MetricDescriptor{
							Name: "projects/test-project/metricDescriptors/workload.googleapis.com/test/cpu/utilization",
							Type: "workload.googleapis.com/test/cpu/utilization",
							Labels: []*lpb.LabelDescriptor{
								{
									Key:         "process",
									ValueType:   lpb.LabelDescriptor_STRING,
									Description: "The name of the process.",
								},
							},
						},
						MetricDescriptorErr: nil,
						ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
							Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
							Type: "gce_instance",
							Labels: []*lpb.LabelDescriptor{
								{
									Key:         "project_id",
									ValueType:   lpb.LabelDescriptor_STRING,
									Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
								},
								{
									Key:         "instance_id",
									ValueType:   lpb.LabelDescriptor_STRING,
									Description: "The numeric VM instance identifier assigned by Compute Engine.",
								},
								{
									Key:         "zone",
									ValueType:   lpb.LabelDescriptor_STRING,
									Description: "The Compute Engine zone in which the VM is running.",
								},
							},
						},
						ResourceDescriptorErr: nil,
					}, nil
				},
			},
			exec:              successfulTimezoneExec,
			metrics:           []string{"workload.googleapis.com/failure/cpu/utilization"},
			destFilesPath:     "/tmp",
			metricsType:       "process_metrics",
			cp:                defaultCloudProperties,
			fs:                mockedfilesystem{},
			wantNonZeroLength: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			gotErr := tc.s.collectMetrics(ctx, tc.metrics, tc.destFilesPath, tc.metricsType, tc.cp, tc.fs, tc.exec)
			if tc.wantNonZeroLength && len(gotErr) == 0 {
				t.Errorf("collectProcessMetrics() returned an empty error")
			}
		})
	}
}

func TestGetProcessMetricsClients(t *testing.T) {
	tests := []struct {
		name    string
		s       *SupportBundle
		wantErr error
	}{
		{
			name: "ClientAlreadyInitialized",
			s: &SupportBundle{
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fakeCM.TimeSeriesQuerier{},
					BackOffs:    cloudmonitoring.NewDefaultBackOffIntervals(),
				},
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{},
			},
			wantErr: nil,
		},
		{
			name: "QueryClientFailure",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return nil, cmpopts.AnyError
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{}, nil
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "MetricClientFailure",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return nil, cmpopts.AnyError
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				createQueryClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesQuerier, error) {
					return &fakeCM.TimeSeriesQuerier{}, nil
				},
				createMetricClient: func(ctx context.Context) (cloudmonitoring.TimeSeriesDescriptorQuerier, error) {
					return &fakeCM.TimeSeriesDescriptorQuerier{}, nil
				},
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.getMetricsClients(ctx)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getProcessMetricsClients() returned an unexpected error: %v", err)
			}
		})
	}
}

func TestFetchTimeSeriesData(t *testing.T) {
	tests := []struct {
		name    string
		s       *SupportBundle
		cp      *ipb.CloudProperties
		metric  string
		wantTS  []TimeSeries
		wantErr error
	}{
		{
			name: "FetchLabelDescriptorsFailure",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fakeCM.TimeSeriesQuerier{
						TS:  nil,
						Err: cmpopts.AnyError,
					},
					BackOffs: cloudmonitoring.NewDefaultBackOffIntervals(),
				},
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					ResourceDescriptor:    nil,
					ResourceDescriptorErr: cmpopts.AnyError,
					MetricDescriptor:      nil,
					MetricDescriptorErr:   cmpopts.AnyError,
				},
			},
			cp:      defaultCloudProperties,
			metric:  "workload.googleapis.com/test/cpu/utilization",
			wantTS:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TimeParseError",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				Timestamp: "2222-01-01 44:00:00",
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fakeCM.TimeSeriesQuerier{
						TS:  nil,
						Err: cmpopts.AnyError,
					},
					BackOffs: cloudmonitoring.NewDefaultBackOffIntervals(),
				},
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					MetricDescriptor: &metricpb.MetricDescriptor{
						Name: "projects/test-project/metricDescriptors/workload.googleapis.com/test/cpu/utilization",
						Type: "workload.googleapis.com/test/cpu/utilization",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "process",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The name of the process.",
							},
						},
					},
					MetricDescriptorErr: nil,
					ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
						Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
						Type: "gce_instance",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "project_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
							},
							{
								Key:         "instance_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The numeric VM instance identifier assigned by Compute Engine.",
							},
							{
								Key:         "zone",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The Compute Engine zone in which the VM is running.",
							},
						},
					},
					ResourceDescriptorErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			metric:  "workload.googleapiscom/test/cpu/utilization",
			wantTS:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "QueryTimeSeriesFailure",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				Timestamp: "2025-01-01 00:00:00",
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fakeCM.TimeSeriesQuerier{
						TS:  nil,
						Err: cmpopts.AnyError,
					},
					BackOffs: cloudmonitoring.NewDefaultBackOffIntervals(),
				},
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					MetricDescriptor: &metricpb.MetricDescriptor{
						Name: "projects/test-project/metricDescriptors/workload.googleapis.com/test/cpu/utilization",
						Type: "workload.googleapis.com/test/cpu/utilization",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "process",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The name of the process.",
							},
						},
					},
					MetricDescriptorErr: nil,
					ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
						Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
						Type: "gce_instance",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "project_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
							},
							{
								Key:         "instance_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The numeric VM instance identifier assigned by Compute Engine.",
							},
							{
								Key:         "zone",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The Compute Engine zone in which the VM is running.",
							},
						},
					},
					ResourceDescriptorErr: nil,
				},
			},
			cp:      defaultCloudProperties,
			metric:  "workload.googleapiscom/test/cpu/utilization",
			wantTS:  nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				Timestamp: "2025-01-01 00:00:00",
				cmr: &cloudmetricreader.CloudMetricReader{
					QueryClient: &fakeCM.TimeSeriesQuerier{
						TS: []*mrpb.TimeSeriesData{
							&mrpb.TimeSeriesData{
								LabelValues: []*mrpb.LabelValue{
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-project",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-instance-id",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-zone",
										},
									},
									&mrpb.LabelValue{
										Value: &mrpb.LabelValue_StringValue{
											StringValue: "sample-process",
										},
									},
								},
								PointData: []*mrpb.TimeSeriesData_PointData{
									&mrpb.TimeSeriesData_PointData{
										Values: []*cpb.TypedValue{
											&cpb.TypedValue{
												Value: &cpb.TypedValue_DoubleValue{
													DoubleValue: 123.456,
												},
											},
										},
										TimeInterval: &cpb.TimeInterval{
											StartTime: &tpb.Timestamp{
												Seconds: 1744790182,
											},
											EndTime: &tpb.Timestamp{
												Seconds: 1744790182,
											},
										},
									},
								},
							},
						},
						Err: nil,
					},
					BackOffs: cloudmonitoring.NewDefaultBackOffIntervals(),
				},
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					MetricDescriptor: &metricpb.MetricDescriptor{
						Name: "projects/test-project/metricDescriptors/workload.googleapis.com/test/cpu/utilization",
						Type: "workload.googleapis.com/test/cpu/utilization",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "process",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The name of the process.",
							},
						},
					},
					MetricDescriptorErr: nil,
					ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
						Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
						Type: "gce_instance",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "project_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
							},
							{
								Key:         "instance_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The numeric VM instance identifier assigned by Compute Engine.",
							},
							{
								Key:         "zone",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The Compute Engine zone in which the VM is running.",
							},
						},
					},
					ResourceDescriptorErr: nil,
				},
			},
			cp:     defaultCloudProperties,
			metric: "workload.googleapiscom/test/cpu/utilization",
			wantTS: []TimeSeries{
				TimeSeries{
					Metric: "workload.googleapiscom/test/cpu/utilization",
					Labels: map[string]string{"project_id": "sample-project", "instance_id": "sample-instance-id", "zone": "sample-zone", "process": "sample-process"},
					Values: []string{"123.456000"},
				},
			},
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.s.fetchTimeSeriesData(ctx, tc.cp, tc.metric)
			cmpOpts := []cmp.Option{
				cmpopts.IgnoreFields(TimeSeries{}, "Timestamp"),
			}
			if diff := cmp.Diff(got, tc.wantTS, cmpOpts...); diff != "" {
				t.Errorf("fetchTimeSeriesData() returned an unexpected diff (-want +got): %v", diff)
			}
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("fetchTimeSeriesData() returned an unexpected error: %v", diff)
			}
		})
	}
}

func TestGetEndingTimestampForMetrics(t *testing.T) {
	tests := []struct {
		name      string
		s         *SupportBundle
		exec      commandlineexecutor.Execute
		wantEndTs string
		wantErr   error
	}{
		{
			name: "TimestampAlreadySet",
			s: &SupportBundle{
				Timestamp:       "2025-01-01 00:00:00",
				endingTimestamp: "2025/01/01 00:00",
			},
			exec:      successfulTimezoneExec,
			wantEndTs: "2025/01/01 00:00",
			wantErr:   nil,
		},
		{
			name: "GetTimezoneFailure",
			s: &SupportBundle{
				Timestamp: "2025-01-01 00:00:00",
			},
			exec:      failedTimezoneExec,
			wantEndTs: "2025/01/01 00:00",
			wantErr:   nil,
		},
		{
			name: "LocationLoadErr",
			s: &SupportBundle{
				Timestamp: "2025-01-01 00:00:00",
			},
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					ExitCode: 0,
					StdOut:   "location=wrongLocation",
				}
			},
			wantEndTs: "",
			wantErr:   cmpopts.AnyError,
		},
		{
			name: "TimeParseErr",
			s: &SupportBundle{
				Timestamp: "2025-01-01 25:00:00",
			},
			exec:      failedTimezoneExec,
			wantEndTs: "",
			wantErr:   cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				Timestamp: "2025-01-01 00:00:00",
			},
			exec:      successfulTimezoneExec,
			wantEndTs: "2025/01/01 08:00",
			wantErr:   nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.s.oteLogger = defaultOTELogger
			got, err := tc.s.getEndingTimestampForMetrics(ctx, tc.exec)
			fmt.Println("Got: ", got)
			if diff := cmp.Diff(got, tc.wantEndTs); diff != "" {
				t.Errorf("getEndingTimestampForMetrics(%v) returned an unexpected diff (-want +got): %v", tc.exec, diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getEndingTimestampForMetrics(%v) returned an unexpected error: %v", tc.exec, err)
			}
		})
	}
}

func TestGetTimezone(t *testing.T) {
	tests := []struct {
		name    string
		exec    commandlineexecutor.Execute
		wantTZ  string
		wantErr error
	}{
		{
			name:    "Failure",
			exec:    failedTimezoneExec,
			wantTZ:  "",
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "Success",
			exec:    successfulTimezoneExec,
			wantTZ:  "America/Los_Angeles",
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getTimezone(ctx, tc.exec)
			if diff := cmp.Diff(got, tc.wantTZ); diff != "" {
				t.Errorf("getTimezone(%v) returned an unexpected diff (-want +got): %v", tc.exec, diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getTimezone(%v) returned an unexpected error: %v", tc.exec, err)
			}
		})
	}
}

func TestGetLabelValue(t *testing.T) {
	tests := []struct {
		name    string
		lv      *mrpb.LabelValue
		wantVal string
		wantErr error
	}{
		{
			name: "Int64Value",
			lv: &mrpb.LabelValue{
				Value: &mrpb.LabelValue_Int64Value{
					Int64Value: 123,
				},
			},
			wantVal: "123",
			wantErr: nil,
		},
		{
			name: "StringValue",
			lv: &mrpb.LabelValue{
				Value: &mrpb.LabelValue_StringValue{
					StringValue: "test-string",
				},
			},
			wantVal: "test-string",
			wantErr: nil,
		},
		{
			name: "BoolValue",
			lv: &mrpb.LabelValue{
				Value: &mrpb.LabelValue_BoolValue{
					BoolValue: true,
				},
			},
			wantVal: "true",
			wantErr: nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getLabelValue(ctx, tc.lv)
			if diff := cmp.Diff(got, tc.wantVal); diff != "" {
				t.Errorf("getLabelValue(%v) returned diff (-want +got):\n%s", tc.lv, diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getLabelValue(%v) returned an unexpected error: %v", tc.lv, err)
			}
		})
	}
}

func TestGetPointValue(t *testing.T) {
	tests := []struct {
		name    string
		v       *cpb.TypedValue
		wantVal string
		wantErr error
	}{
		{
			name: "Int64Value",
			v: &cpb.TypedValue{
				Value: &cpb.TypedValue_Int64Value{
					Int64Value: 123,
				},
			},
			wantVal: "123",
			wantErr: nil,
		},
		{
			name: "StringValue",
			v: &cpb.TypedValue{
				Value: &cpb.TypedValue_StringValue{
					StringValue: "test-string",
				},
			},
			wantVal: "test-string",
			wantErr: nil,
		},
		{
			name: "BoolValue",
			v: &cpb.TypedValue{
				Value: &cpb.TypedValue_BoolValue{
					BoolValue: true,
				},
			},
			wantVal: "true",
			wantErr: nil,
		},
		{
			name: "DoubleValue",
			v: &cpb.TypedValue{
				Value: &cpb.TypedValue_DoubleValue{
					DoubleValue: 123.456,
				},
			},
			wantVal: "123.456000",
			wantErr: nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getPointValue(ctx, tc.v)
			if diff := cmp.Diff(got, tc.wantVal); diff != "" {
				t.Errorf("getPointValue(%v) returned diff (-want +got):\n%s", tc.v, diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("getPointValue(%v) returned an unexpected error: %v", tc.v, err)
			}
		})
	}
}

func TestFetchLabelDescriptors(t *testing.T) {
	tests := []struct {
		name         string
		s            *SupportBundle
		cp           *ipb.CloudProperties
		metricType   string
		resourceType string
		wantLabels   []string
		wantErr      error
	}{
		{
			name: "ResourceDescriptorFailure",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					MetricDescriptor:    nil,
					MetricDescriptorErr: cmpopts.AnyError,
				},
			},
			cp:           defaultCloudProperties,
			metricType:   "workload.googleapis.com/test/cpu/utilization",
			resourceType: "gce_instance",
			wantLabels:   nil,
			wantErr:      cmpopts.AnyError,
		},
		{
			name: "MetricDescriptorFailure",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
						Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
						Type: "gce_instance",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "project_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
							},
							{
								Key:         "instance_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The numeric VM instance identifier assigned by Compute Engine.",
							},
							{
								Key:         "zone",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The Compute Engine zone in which the VM is running.",
							},
						},
					},
					ResourceDescriptorErr: nil,
					MetricDescriptor:      nil,
					MetricDescriptorErr:   cmpopts.AnyError,
				},
			},
			cp:         defaultCloudProperties,
			wantLabels: nil,
			wantErr:    cmpopts.AnyError,
		},
		{
			name: "Success",
			s: &SupportBundle{
				oteLogger: defaultOTELogger,
				mmc: &fakeCM.TimeSeriesDescriptorQuerier{
					ResourceDescriptor: &monitoredrespb.MonitoredResourceDescriptor{
						Name: "projects/test-project/monitoredResourceDescriptors/gce_instance",
						Type: "gce_instance",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "project_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The identifier of the GCP project associated with this resource, such as \"my-project\".",
							},
							{
								Key:         "instance_id",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The numeric VM instance identifier assigned by Compute Engine.",
							},
							{
								Key:         "zone",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The Compute Engine zone in which the VM is running.",
							},
						},
					},
					MetricDescriptor: &metricpb.MetricDescriptor{
						Name: "projects/test-project/metricDescriptors/workload.googleapis.com/test/cpu/utilization",
						Type: "workload.googleapis.com/test/cpu/utilization",
						Labels: []*lpb.LabelDescriptor{
							{
								Key:         "process",
								ValueType:   lpb.LabelDescriptor_STRING,
								Description: "The name of the process.",
							},
						},
					},
					ResourceDescriptorErr: nil,
					MetricDescriptorErr:   nil,
				},
			},
			cp:         defaultCloudProperties,
			wantLabels: []string{"project_id", "instance_id", "zone", "process"},
			wantErr:    nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.s.fetchLabelDescriptors(ctx, tc.cp, tc.metricType, tc.resourceType)
			if diff := cmp.Diff(got, tc.wantLabels); diff != "" {
				t.Errorf("fetchLabelDescriptors() returned an unexpected diff (-want +got): %v", diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("fetchLabelDescriptors() returned an unexpected error: %v", err)
			}
		})
	}
}

func TestNewQueryClient(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{
			name:    "Failure",
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newQueryClient(ctx)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("NewQueryClient() returned an unexpected error: %v", diff)
			}
		})
	}
}

func TestNewMetricClient(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{
			name:    "Failure",
			wantErr: cmpopts.AnyError,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newMetricClient(ctx)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("NewMetricClient() returned an unexpected error: %v", diff)
			}
		})
	}
}
