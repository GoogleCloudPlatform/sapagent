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
	"io/ioutil"
	"os"
	"path/filepath"

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

// Helper provides a real implementation of the FileSystem interface
type Helper struct{}

// MkdirAll provides testable implementation of os.MkdirAll method
func (h Helper) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// ReadFile provides testable implementation of os.ReadFile method.
func (h Helper) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadDir provides testable implementation of os.ReadDir method.
func (h Helper) ReadDir(path string) ([]fs.FileInfo, error) {
	return ioutil.ReadDir(path)
}

// Open provides testable implementation of os.Open method.
func (h Helper) Open(path string) (*os.File, error) {
	return os.Open(path)
}

// OpenFile provides testable implementation of os.OpenFile method.
func (h Helper) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

// RemoveAll provides testable implementation of os.RemoveAll method.
func (h Helper) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Create provides testable implementation of os.Create method.
func (h Helper) Create(path string) (*os.File, error) {
	return os.Create(path)
}

// WriteStringToFile provides testable implementation of os.WriteStringToFile method.
func (h Helper) WriteStringToFile(file *os.File, content string) (int, error) {
	return file.WriteString(content)
}

// Copy provides testable implementation of io.Copy method.
func (h Helper) Copy(w io.Writer, r io.Reader) (int64, error) {
	return io.Copy(w, r)
}

// Chmod provides testable implementation of os.Chmod method.
func (h Helper) Chmod(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

// Stat provides testable implementation of os.Stat method.
func (h Helper) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// WalkAndZip provides testable implementation of filepath.Walk which zips the content of the directory.
func (h Helper) WalkAndZip(source string, z zipper.Zipper, w *zip.Writer) error {
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
