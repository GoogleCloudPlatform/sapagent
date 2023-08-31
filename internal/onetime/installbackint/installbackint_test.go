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

package installbackint

import (
	"context"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	defaultBackint = InstallBackint{
		sid: "TST",
	}
)

// The following default functions return nil error up to numNil,
// after which they will return an error.
func defaultMkdir(numNil int) func(string, os.FileMode) error {
	return func(string, os.FileMode) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func defaultWriteFile(numNil int) func(string, []byte, os.FileMode) error {
	return func(string, []byte, os.FileMode) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func defaultSymlink(numNil int) func(string, string) error {
	return func(string, string) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func TestExecuteInstallBackint(t *testing.T) {
	tests := []struct {
		name string
		b    InstallBackint
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
		{
			name: "SuccessfullyParseArgsNoSID",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			b: InstallBackint{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			b: InstallBackint{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "ProvidedSID",
			want: subcommands.ExitFailure,
			b:    defaultBackint,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.b, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForInstallBackint(t *testing.T) {
	want := "install Backint and migrate from Backint agent for SAP HANA"
	b := InstallBackint{}
	got := b.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForInstallBackint(t *testing.T) {
	b := InstallBackint{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"sid", "v", "h", "loglevel"}
	b.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestInstallBackintHandler(t *testing.T) {
	tests := []struct {
		name           string
		baseInstallDir string
		b              InstallBackint
		want           error
	}{
		{
			name:           "FailStat",
			baseInstallDir: "/does/not/exist",
			want:           cmpopts.AnyError,
		},
		{
			name: "FailMakeBackintInstallDir",
			b: InstallBackint{
				mkdir: defaultMkdir(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailMakeHdbconfigInstallDir",
			b: InstallBackint{
				mkdir: defaultMkdir(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteBackintFile",
			b: InstallBackint{
				mkdir:     defaultMkdir(2),
				writeFile: defaultWriteFile(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteParameterFile",
			b: InstallBackint{
				mkdir:     defaultMkdir(2),
				writeFile: defaultWriteFile(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteBackintSymlink",
			b: InstallBackint{
				mkdir:     defaultMkdir(2),
				writeFile: defaultWriteFile(2),
				symlink:   defaultSymlink(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteParametersSymlink",
			b: InstallBackint{
				mkdir:     defaultMkdir(2),
				writeFile: defaultWriteFile(2),
				symlink:   defaultSymlink(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "Success",
			b: InstallBackint{
				mkdir:     defaultMkdir(2),
				writeFile: defaultWriteFile(2),
				symlink:   defaultSymlink(2),
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.b.installBackintHandler(context.Background(), t.TempDir()+"/"+test.baseInstallDir)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("installBackintHandler()=%v want %v", got, test.want)
			}
		})
	}
}
