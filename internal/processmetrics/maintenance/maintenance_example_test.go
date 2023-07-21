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

package maintenance_test

import (
	"fmt"
	"os"

	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
)

type (
	mockedFileReader struct {
		expectedData []byte
		expectedErr  error
	}
	mockedFileWriter struct {
		expectedErrForMakeDirs error
		expectedErrForWrite    error
	}
)

func (mfr mockedFileReader) Read(name string) ([]byte, error) {
	return mfr.expectedData, mfr.expectedErr
}

func (mfw mockedFileWriter) Write(name string, data []byte, perm os.FileMode) error {
	return mfw.expectedErrForWrite
}

func (mfw mockedFileWriter) MakeDirs(path string, perm os.FileMode) error {
	return mfw.expectedErrForMakeDirs
}

func ExampleReadMaintenanceMode() {
	// Creating a mock file reader.
	// For real implementation, maintenance.ModeReader{} will be used instead.
	// ModeReader is exported from maintenance package for this convenience.
	mfr := mockedFileReader{
		expectedData: []byte(`{"sids":["deh"]}`),
		expectedErr:  nil,
	}

	fmt.Println(maintenance.ReadMaintenanceMode(mfr))
	// Output:
	// [deh] <nil>
}

func ExampleUpdateMaintenanceMode() {
	// Creating a mock file reader and writer.
	// For real implementation, maintenance.ModeReader{} and  maintenance.ModeWriter{} will be used.
	// ModeReader and ModeWriter are exported from maintenance package for this convenience.
	mfr := mockedFileReader{
		expectedData: []byte(`{"sids":["deh"]}`),
		expectedErr:  nil,
	}
	mfw := mockedFileWriter{}

	// Enable Maintenance mode and update SIDs
	fmt.Println(maintenance.UpdateMaintenanceMode(true, "abc", mfr, mfw))
	// Output:
	// [deh abc] <nil>
}
