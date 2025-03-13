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

// Package fake provides a fake implementation of the filesystem interface for unit tests
package fake

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
)

// FileSystem provides a fake implementation of the filesystem.Filesystem interface.
type FileSystem struct {
	MkDirErr       []error
	mkDirCallCount int

	ReadFileResp      [][]byte
	ReadFileErr       []error
	readFileCallCount int

	ReadDirResp      [][]fs.FileInfo
	ReadDirErr       []error
	readDirCallCount int

	OpenResp      []*os.File
	OpenErr       []error
	openCallCount int

	OpenFileResp      []*os.File
	OpenFileErr       []error
	openFileCallCount int

	RemoveAllErr       []error
	removeAllCallCount int

	CreateResp      []*os.File
	CreateErr       []error
	createCallCount int

	WriteStringToFileResp      []int
	WriteStringToFileErr       []error
	writeStringToFileCallCount int

	RenameErr       []error
	renameCallCount int

	CopyResp      []int64
	CopyErr       []error
	copyCallCount int

	ChmodErr       []error
	chmodCallCount int

	StatResp      []os.FileInfo
	StatErr       []error
	statCallCount int

	WalkAndZipErr       []error
	walkAndZipCallCount int
}

// MockedFileInfo is a fake implementation of fs.FileInfo for unit testing.
type MockedFileInfo struct {
	FileName    string
	FileSize    int64
	FileMode    fs.FileMode
	FileModTime time.Time
	FileIsDir   bool
	FileSys     any
}

// Name is a fake implementation for unit testing.
func (mfi MockedFileInfo) Name() string {
	return mfi.FileName
}

// Size is a fake implementation for unit testing.
func (mfi MockedFileInfo) Size() int64 {
	return mfi.FileSize
}

// Mode is a fake implementation for unit testing.
func (mfi MockedFileInfo) Mode() fs.FileMode {
	return mfi.FileMode
}

// ModTime is a fake implementation for unit testing.
func (mfi MockedFileInfo) ModTime() time.Time {
	return mfi.FileModTime
}

// IsDir is a fake implementation for unit testing.
func (mfi MockedFileInfo) IsDir() bool {
	return mfi.FileIsDir
}

// Sys is a fake implementation for unit testing.
func (mfi MockedFileInfo) Sys() any {
	return mfi.FileSys
}

// MkdirAll is a fake implementation for unit testing.
func (f *FileSystem) MkdirAll(string, os.FileMode) error {
	defer func() { f.mkDirCallCount++ }()
	return f.MkDirErr[f.mkDirCallCount]
}

// ReadFile is a fake implementation for unit testing.
func (f *FileSystem) ReadFile(string) ([]byte, error) {
	defer func() { f.readFileCallCount++ }()
	return f.ReadFileResp[f.readFileCallCount], f.ReadFileErr[f.readFileCallCount]
}

// ReadDir is a fake implementation for unit testing.
func (f *FileSystem) ReadDir(string) ([]fs.FileInfo, error) {
	defer func() { f.readDirCallCount++ }()
	return f.ReadDirResp[f.readDirCallCount], f.ReadDirErr[f.readDirCallCount]
}

// Open is a fake implementation for unit testing.
func (f *FileSystem) Open(string) (*os.File, error) {
	defer func() { f.openCallCount++ }()
	return f.OpenResp[f.openCallCount], f.OpenErr[f.openCallCount]
}

// OpenFile is a fake implementation for unit testing.
func (f *FileSystem) OpenFile(string, int, os.FileMode) (*os.File, error) {
	defer func() { f.openFileCallCount++ }()
	return f.OpenFileResp[f.openFileCallCount], f.OpenFileErr[f.openFileCallCount]
}

// RemoveAll is a fake implementation for unit testing.
func (f *FileSystem) RemoveAll(string) error {
	defer func() { f.removeAllCallCount++ }()
	return f.RemoveAllErr[f.removeAllCallCount]
}

// Create is a fake implementation for unit testing.
func (f *FileSystem) Create(string) (*os.File, error) {
	defer func() { f.createCallCount++ }()
	return f.CreateResp[f.createCallCount], f.CreateErr[f.createCallCount]
}

// WriteStringToFile is a fake implementation for unit testing.
func (f *FileSystem) WriteStringToFile(*os.File, string) (int, error) {
	defer func() { f.writeStringToFileCallCount++ }()
	return f.WriteStringToFileResp[f.writeStringToFileCallCount], f.WriteStringToFileErr[f.writeStringToFileCallCount]
}

// Rename is a fake implementation for unit testing.
func (f *FileSystem) Rename(string, string) error {
	defer func() { f.renameCallCount++ }()
	return f.RenameErr[f.renameCallCount]
}

// Copy is a fake implementation for unit testing.
func (f *FileSystem) Copy(io.Writer, io.Reader) (int64, error) {
	defer func() { f.copyCallCount++ }()
	return f.CopyResp[f.copyCallCount], f.CopyErr[f.copyCallCount]
}

// Chmod is a fake implementation for unit testing.
func (f *FileSystem) Chmod(string, os.FileMode) error {
	defer func() { f.chmodCallCount++ }()
	return f.ChmodErr[f.chmodCallCount]
}

// Stat is a fake implementation for unit testing.
func (f *FileSystem) Stat(string) (os.FileInfo, error) {
	defer func() { f.statCallCount++ }()
	return f.StatResp[f.statCallCount], f.StatErr[f.statCallCount]
}

// WalkAndZip is a fake implementation for unit testing.
func (f *FileSystem) WalkAndZip(string, zipper.Zipper, *zip.Writer) error {
	defer func() { f.walkAndZipCallCount++ }()
	return f.WalkAndZipErr[f.walkAndZipCallCount]
}

// FileInfo is a fake implementation of fs.FileInfo for unit testing.
type FileInfo struct {
	FakeName    string
	FakeSize    int64
	FakeMode    fs.FileMode
	FakeModTime time.Time
	FakeIsDir   bool
	FakeSYS     any
}

// Name is a fake implementation for unit testing.
func (fi FileInfo) Name() string {
	return fi.FakeName
}

// Size is a fake implementation for unit testing.
func (fi FileInfo) Size() int64 {
	return fi.FakeSize
}

// Mode is a fake implementation for unit testing.
func (fi FileInfo) Mode() fs.FileMode {
	return fi.FakeMode
}

// ModTime is a fake implementation for unit testing.
func (fi FileInfo) ModTime() time.Time {
	return fi.FakeModTime
}

// IsDir is a fake implementation for unit testing.
func (fi FileInfo) IsDir() bool {
	return fi.FakeIsDir
}

// Sys is a fake implementation for unit testing.
func (fi FileInfo) Sys() any {
	return fi.FakeSYS
}
