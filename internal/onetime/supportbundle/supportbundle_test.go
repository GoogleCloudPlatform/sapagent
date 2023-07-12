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
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
)

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

	mockedWriter struct {
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
	return &os.File{}, nil
}

func (mfu mockedfilesystem) WriteStringToFile(f *os.File, content string) (int, error) {
	if f == nil {
		return 0, cmpopts.AnyError
	}
	return 10, nil
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
		fsh := fileSystemHelper{}
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
	sosrc := SupportBundle{}
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
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sosr.Execute(context.Background(), &flag.FlagSet{}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.sosr, test.args, got, test.want)
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
			sosrc: SupportBundle{instanceNums: "00 01", hostname: "sample_host"},
			want:  []string{"no value provided for sid"},
		},
		{
			name:  "NoValueForInstance",
			sosrc: SupportBundle{sid: "DEH", instanceNums: "", hostname: "sample_host"},
			want:  []string{"no value provided for instance-numbers"},
		},
		{
			name:  "InvalidValueForinstanceNums",
			sosrc: SupportBundle{sid: "DEH", instanceNums: "00 011", hostname: "sample_host"},
			want:  []string{"invalid instance number 011"},
		},
		{
			name:  "NoValueForHostName",
			sosrc: SupportBundle{sid: "DEH", instanceNums: "00 01", hostname: ""},
			want:  []string{"no value provided for hostname"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sosrc.validateParams()
			if len(got) != len(test.want) || !cmp.Equal(got, test.want) {
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
		want           subcommands.ExitStatus
	}{
		{
			name: "InvalidParams",
			sosr: &SupportBundle{sid: "DEH",
				instanceNums: "",
				hostname:     "sample_host",
			},
			ctx:  context.Background(),
			exec: fakeExec,
			fs:   mockedfilesystem{},
			z:    mockedZipper{},
			want: subcommands.ExitUsageError,
		},
		{
			name: "MkdirError",
			sosr: &SupportBundle{
				sid:          "DEH",
				instanceNums: "00 11",
				hostname:     "sample_host",
			},
			destFilePrefix: "failure",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{},
			z:              mockedZipper{},
			want:           subcommands.ExitFailure,
		},
		{
			name: "FaultInExtractingErrors",
			sosr: &SupportBundle{
				sid:          "DEH",
				instanceNums: "00 11",
				hostname:     "sample_host",
			},
			destFilePrefix: "samplefile",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{reqErr: os.ErrInvalid},
			z:              mockedZipper{},
			want:           subcommands.ExitFailure,
		},
		{
			name: "Success",
			sosr: &SupportBundle{
				sid:                "DEH",
				instanceNums:       "00 11",
				hostname:           "sample_host",
				pacemakerDiagnosis: true,
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
			z:    mockedZipper{},
			want: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sosr.sosReportHandler(test.ctx, test.destFilePrefix, test.exec, test.fs, test.z)
			if got != test.want {
				t.Errorf("sosReportHandler() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractErrorsUsingGrep(t *testing.T) {
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
			got := execAndWriteToFile(test.ctx, test.destFile, test.hostname, test.exec, test.p, test.opFile, test.fu)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("extractErrorsUsingGREP() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractSystemDBErrors(t *testing.T) {
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
			if got := extractSystemDBErrors(context.Background(), test.destFile, test.hostname, test.hanaPaths, test.exec, test.fs); got != test.want {
				t.Errorf("extractSystemDBErrors() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractTenantDBErrors(t *testing.T) {
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
			if got := extractTenantDBErrors(context.Background(), test.destFile, "DEH", test.hostname, test.hanaPaths, test.exec, test.fs); got != test.want {
				t.Errorf("extractTenantDBErrors() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractBackintErrors(t *testing.T) {
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
			if got := extractBackintErrors(context.Background(), test.destFile, test.globalPath, test.hostname, test.exec, test.fs); got != test.want {
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
			got := nameServerTracesAndBackupLogs(context.Background(), test.hanaPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("nameServerTracesAndBackupLog(%q, %q, %q)=%q, want %q", test.hanaPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestTenantDBNameservertraceAndBackupLog(t *testing.T) {
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
			got := tenantDBNameServerTracesAndBackupLogs(context.Background(), test.hanaPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("nameServerTracesAndBackupLog(%q, %q, %q)=%q, want %q", test.hanaPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestBackintParameterFiles(t *testing.T) {
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
			got := backintParameterFiles(context.Background(), test.globalPath, test.sid, test.fu)
			if !cmp.Equal(got, test.want) {
				t.Errorf("backintParameterFiles(%q, %q, %q) = %q, want %q", test.globalPath, test.sid, test.fu, got, test.want)
			}
		})
	}
}

func TestBackintlogs(t *testing.T) {
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
		got := backintLogs(context.Background(), test.globalPath, test.sid, test.fu)
		if !cmp.Equal(got, test.want) {
			t.Errorf("BackIntLogs(%q, %q, %q) = %q, want %q", test.globalPath, test.sid, test.fu, got, test.want)
		}
	}
}

func TestAgentLogsFiles(t *testing.T) {
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
			got := agentLogFiles(test.path, test.fu)
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
			source: "sampleFile",
			target: "failure",
			fu:     mockedfilesystem{},
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
		{
			name:   "CreateError",
			source: "sampleFile",
			target: "dest",
			fu:     mockedfilesystem{},
			z:      mockedZipper{},
			want:   nil,
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
			got := removeDestinationFolder(test.path, test.fu)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("removeDestinationFolder(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestCollectPacemakerLogs(t *testing.T) {
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
			got := pacemakerLogs(test.ctx, test.destFilePath, test.exec, test.fs)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectPacemakerLogs(%q) = %v, want %v", test.destFilePath, got, test.want)
			}
		})
	}
}

func TestCollectRHELPacemakerLogs(t *testing.T) {
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
			if got := rhelPacemakerLogs(test.ctx, test.exec, test.destFilesPath, test.fs); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectRHELPacemakerLogs(%q) = %v, want %v", test.destFilesPath, got, test.want)
			}
		})
	}
}

func TestCollectSLESPacemakerLogs(t *testing.T) {
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
			if got := slesPacemakerLogs(test.ctx, test.exec, test.destFilesPath, test.fs); !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("collectSLESPacemakerLogs(%q) = %v, want %v", test.destFilesPath, got, test.want)
			}
		})
	}
}
