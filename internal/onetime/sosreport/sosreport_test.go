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

package sosreport

import (
	"archive/zip"
	"context"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
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

func TestSetFlagsForSOSReport(t *testing.T) {
	sosrc := SOSReport{}
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
		sosr *SOSReport
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
		sosrc SOSReport
		want  []string
	}{
		{
			name:  "NoValueForSID",
			sosrc: SOSReport{instanceNums: "00 01", hostname: "sample_host"},
			want:  []string{"no value provided for sid"},
		},
		{
			name:  "NoValueForInstance",
			sosrc: SOSReport{sid: "DEH", instanceNums: "", hostname: "sample_host"},
			want:  []string{"no value provided for instance-numbers"},
		},
		{
			name:  "InvalidValueForinstanceNums",
			sosrc: SOSReport{sid: "DEH", instanceNums: "00 011", hostname: "sample_host"},
			want:  []string{"invalid instance number 011"},
		},
		{
			name:  "NoValueForHostName",
			sosrc: SOSReport{sid: "DEH", instanceNums: "00 01", hostname: ""},
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
		sosr           *SOSReport
		ctx            context.Context
		destFilePrefix string
		exec           commandlineexecutor.Execute
		fs             filesystem.FileSystem
		z              zipper.Zipper
		want           subcommands.ExitStatus
	}{
		{
			name: "InvalidParams",
			sosr: &SOSReport{sid: "DEH",
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
			sosr: &SOSReport{
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
			sosr: &SOSReport{
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
			sosr: &SOSReport{
				sid:          "DEH",
				instanceNums: "00 11",
				hostname:     "sample_host",
			},
			destFilePrefix: "samplefile",
			ctx:            context.Background(),
			exec:           fakeExec,
			fs:             mockedfilesystem{},
			z:              mockedZipper{},
			want:           subcommands.ExitSuccess,
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
