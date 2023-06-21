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

package maintenance

import (
	"context"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
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

func TestSynopsis(t *testing.T) {
	mm := Mode{}
	want := "configure maintenance mode"

	got := mm.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want %v", got, want)
	}
}

func TestSetFlags(t *testing.T) {
	tests := []struct {
		name    string
		mm      Mode
		pattern string
		fs      *flag.FlagSet
	}{
		{
			name:    "HasSidFlag",
			mm:      Mode{},
			pattern: "sid",
			fs:      flag.NewFlagSet("sid", flag.ExitOnError),
		},
		{
			name:    "HasEnableFlag",
			mm:      Mode{},
			pattern: "enable",
			fs:      flag.NewFlagSet("enable", flag.ExitOnError),
		},
		{
			name:    "HasShowFlag",
			mm:      Mode{},
			pattern: "show",
			fs:      flag.NewFlagSet("show", flag.ExitOnError),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.mm.SetFlags(test.fs)
			got := test.fs.Lookup(test.pattern)
			if got == nil {
				t.Errorf("SetupFlags(%v) flag not found %s", test.fs, test.pattern)
			}
		})
	}
}

func TestExecuteMaintenance(t *testing.T) {
	tests := []struct {
		name string
		mm   Mode
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
		{
			name: "SuccessfullyParseArgs",
			mm:   Mode{show: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.mm.Execute(context.Background(), &flag.FlagSet{}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.mm, test.args, got, test.want)
			}
		})
	}
}

func TestModeHandler(t *testing.T) {
	tests := []struct {
		name string
		mm   Mode
		fs   *flag.FlagSet
		mfr  mockedFileReader
		mfw  mockedFileWriter
		want subcommands.ExitStatus
	}{
		{
			name: "ShowFileReadFailure",
			mm:   Mode{show: true},
			mfr:  mockedFileReader{err: os.ErrPermission},
			want: subcommands.ExitFailure,
		},
		{
			name: "ShowWithNoSIDInMaintenance",
			mm:   Mode{show: true},
			want: subcommands.ExitSuccess,
		},
		{
			name: "ShowWithSIDInMaintenance",
			mm:   Mode{show: true},
			mfr:  mockedFileReader{data: []byte(`{"sids":["deh"]}`)},
			want: subcommands.ExitSuccess,
		},
		{
			name: "EnableFalseEmptySID",
			mm:   Mode{enable: false},
			want: subcommands.ExitUsageError,
		},
		{
			name: "EnableTrueEmptySID",
			mm:   Mode{enable: true},
			want: subcommands.ExitUsageError,
		},
		{
			name: "UpdateMMFailure",
			mm:   Mode{enable: true, sid: "deh"},
			mfw:  mockedFileWriter{errMakeDirs: cmpopts.AnyError},
			want: subcommands.ExitFailure,
		},
		{
			name: "UpdateMMSuccess",
			mm:   Mode{enable: true, sid: "deh"},
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
