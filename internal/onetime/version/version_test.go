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

package version

import (
	"context"
	"os"
	"testing"

	"flag"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId:    "default-project",
	InstanceName: "default-instance",
}

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
	v := Version{}
	want := "print sapagent version information"

	got := v.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want %v", got, want)
	}
}

func TestName(t *testing.T) {
	v := Version{}
	want := "version"

	got := v.Name()
	if got != want {
		t.Errorf("Name()=%v, want %v", got, want)
	}
}

func TestSetFlags(t *testing.T) {
	v := Version{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	v.SetFlags(fs)

	flags := []string{"v", "h", "loglevel"}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestExecuteVersion(t *testing.T) {
	tests := []struct {
		name string
		v    Version
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
				"test3",
			},
		},
		{
			name: "SuccessForAgentVersion",
			v:    Version{version: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForHelp",
			v:    Version{help: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessGeneral",
			v:    Version{},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.v.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.v, test.args, got, test.want)
			}
		})
	}
}
