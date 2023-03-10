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

package onetime

import (
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
)

type (
	mockedFileReader struct {
		data []byte
		err  error
	}

	mockedFileWriter struct {
		errMakeDirs, errWrite error
	}
)

func (mfr mockedFileReader) Read(string) ([]byte, error) {
	return mfr.data, mfr.err
}

func (mfw mockedFileWriter) Write(string, []byte, os.FileMode) error {
	return mfw.errWrite
}

func (mfw mockedFileWriter) MakeDirs(string, os.FileMode) error {
	return mfw.errMakeDirs
}

func TestMaintenanceModeHandler(t *testing.T) {
	tests := []struct {
		name string
		mm   MaintenanceMode
		fs   *flag.FlagSet
		mfr  mockedFileReader
		mfw  mockedFileWriter
		want subcommands.ExitStatus
	}{
		{
			name: "ShowFileReadFailure",
			mm:   MaintenanceMode{show: true},
			mfr:  mockedFileReader{err: os.ErrPermission},
			want: subcommands.ExitFailure,
		},
		{
			name: "ShowWithNoSIDInMaintenance",
			mm:   MaintenanceMode{show: true},
			want: subcommands.ExitSuccess,
		},
		{
			name: "ShowWithSIDInMaintenance",
			mm:   MaintenanceMode{show: true},
			mfr:  mockedFileReader{data: []byte(`{"sids":["deh"]}`)},
			want: subcommands.ExitSuccess,
		},
		{
			name: "EnableFalseEmptySID",
			mm:   MaintenanceMode{enable: false},
			want: subcommands.ExitUsageError,
		},
		{
			name: "EnableTrueEmptySID",
			mm:   MaintenanceMode{enable: true},
			want: subcommands.ExitUsageError,
		},
		{
			name: "UpdateMMFailure",
			mm:   MaintenanceMode{enable: true, sid: "deh"},
			mfw:  mockedFileWriter{errMakeDirs: cmpopts.AnyError},
			want: subcommands.ExitFailure,
		},
		{
			name: "UpdateMMSuccess",
			mm:   MaintenanceMode{enable: true, sid: "deh"},
			want: subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.mm.maintenanceModeHandler(test.fs, test.mfr, test.mfw)
			if got != test.want {
				t.Errorf("maintenanceModeHandler(%v)=%v, want %v", test.mm, got, test.want)
			}
		})
	}
}
