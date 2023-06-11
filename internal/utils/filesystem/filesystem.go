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

// Package filesystem is used for providing OS level file utility methods.
package filesystem

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"

	"github.com/GoogleCloudPlatform/sapagent/internal/utils/zipper"
)

type (
	// FileSystem interface is an interface created to help make OS file system methods testable.
	// Any caller should be able to provide their own mocked implementation in the unit tests.
	FileSystem interface {
		MkdirAll(string, os.FileMode) error
		ReadFile(string) ([]byte, error)
		ReadDir(string) ([]fs.FileInfo, error)
		Open(string) (*os.File, error)
		OpenFile(string, int, os.FileMode) (*os.File, error)
		RemoveAll(string) error
		Create(string) (*os.File, error)
		WriteStringToFile(*os.File, string) (int, error)
		Copy(io.Writer, io.Reader) (int64, error)
		Chmod(string, os.FileMode) error
		Stat(string) (os.FileInfo, error)
		WalkAndZip(string, zipper.Zipper, *zip.Writer) error
	}
)
