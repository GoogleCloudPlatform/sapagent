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
	"time"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/sys/unix"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultBackint = InstallBackint{
		SID: "TST",
	}
)

type fakeFileInfo struct{}

func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return os.ModePerm }
func (f fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any {
	return &unix.Stat_t{
		Uid: 123,
		Gid: 123,
	}
}

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

func defaultStat(numNil int) func(string, *unix.Stat_t) error {
	return func(string, *unix.Stat_t) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func defaultRename(numNil int) func(string, string) error {
	return func(string, string) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func defaultGlob(numNil int) func(string) ([]string, error) {
	return func(string) ([]string, error) {
		if numNil > 0 {
			numNil--
			return []string{"/backint/backint-gcs/parameters.txt", "/backint/backint-gcs/VERSION.txt"}, nil
		}
		return nil, cmpopts.AnyError
	}
}

func defaultReadFile(numNil int) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if numNil > 0 {
			numNil--
			return nil, nil
		}
		return nil, cmpopts.AnyError
	}
}

func defaultChmod(numNil int) func(string, os.FileMode) error {
	return func(string, os.FileMode) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

func defaultChown(numNil int) func(string, int, int) error {
	return func(string, int, int) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return cmpopts.AnyError
	}
}

// defaultStatNotExist returns os.ErrNotExist after numNil is exhausted.
func defaultStatNotExist(numNil int) func(string, *unix.Stat_t) error {
	return func(string, *unix.Stat_t) error {
		if numNil > 0 {
			numNil--
			return nil
		}
		return os.ErrNotExist
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
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgsNoSID",
			want: subcommands.ExitUsageError,
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
				&ipb.CloudProperties{},
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
	flags := []string{"sid", "h", "loglevel", "log-path"}
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
		name string
		b    InstallBackint
		want error
	}{
		{
			name: "FailStat",
			b: InstallBackint{
				stat: defaultStat(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailMakeBackintInstallDir",
			b: InstallBackint{
				stat:  defaultStatNotExist(1),
				mkdir: defaultMkdir(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailChownBackintInstallDir",
			b: InstallBackint{
				stat:  defaultStatNotExist(1),
				mkdir: defaultMkdir(2),
				chown: defaultChown(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailMakeHdbconfigInstallDir",
			b: InstallBackint{
				stat:  defaultStatNotExist(1),
				mkdir: defaultMkdir(2),
				chown: defaultChown(2),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteBackintFile",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(3),
				writeFile: defaultWriteFile(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteParameterFile",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(4),
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailChmodParameterFile",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(4),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteBackintSymlink",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(5),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(2),
				symlink:   defaultSymlink(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailChownBackintSymlink",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(5),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(2),
				symlink:   defaultSymlink(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteParametersSymlink",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(6),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(2),
				symlink:   defaultSymlink(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailWriteLogSymlink",
			b: InstallBackint{
				stat:      defaultStatNotExist(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(7),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(2),
				symlink:   defaultSymlink(2),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SuccessNoMigration",
			b: InstallBackint{
				stat:      defaultStatNotExist(2),
				rename:    defaultRename(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(8),
				writeFile: defaultWriteFile(2),
				chmod:     defaultChmod(2),
				symlink:   defaultSymlink(3),
			},
			want: nil,
		},
		{
			name: "FailedToStatInstallationFolder",
			b: InstallBackint{
				stat: defaultStat(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToStatOldAgent",
			b: InstallBackint{
				stat:   defaultStat(2),
				rename: defaultRename(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToRename",
			b: InstallBackint{
				stat:   defaultStat(2),
				rename: defaultRename(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToMkdir",
			b: InstallBackint{
				stat:   defaultStat(3),
				rename: defaultRename(1),
				mkdir:  defaultMkdir(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToChownNewDir",
			b: InstallBackint{
				stat:   defaultStat(3),
				rename: defaultRename(1),
				mkdir:  defaultMkdir(2),
				chown:  defaultChown(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToChownOldDir",
			b: InstallBackint{
				stat:   defaultStat(3),
				rename: defaultRename(1),
				mkdir:  defaultMkdir(3),
				chown:  defaultChown(2),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToGlob",
			b: InstallBackint{
				stat:   defaultStat(3),
				rename: defaultRename(1),
				mkdir:  defaultMkdir(3),
				chown:  defaultChown(3),
				glob:   defaultGlob(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToReadFile",
			b: InstallBackint{
				stat:     defaultStat(3),
				rename:   defaultRename(1),
				mkdir:    defaultMkdir(3),
				chown:    defaultChown(3),
				glob:     defaultGlob(1),
				readFile: defaultReadFile(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToWriteLegacyParameterFile",
			b: InstallBackint{
				stat:      defaultStat(3),
				rename:    defaultRename(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(3),
				glob:      defaultGlob(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToChmodLegacyParameterFile",
			b: InstallBackint{
				stat:      defaultStat(3),
				rename:    defaultRename(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(3),
				glob:      defaultGlob(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(3),
				chmod:     defaultChmod(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailedToChownLegacyParameterFile",
			b: InstallBackint{
				stat:      defaultStat(3),
				rename:    defaultRename(1),
				mkdir:     defaultMkdir(3),
				chown:     defaultChown(3),
				glob:      defaultGlob(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(3),
				chmod:     defaultChmod(1),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "SuccessWithMigration",
			b: InstallBackint{
				stat:      defaultStat(3),
				rename:    defaultRename(1),
				mkdir:     defaultMkdir(6),
				chown:     defaultChown(12),
				glob:      defaultGlob(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(3),
				chmod:     defaultChmod(3),
				symlink:   defaultSymlink(3),
			},
			want: nil,
		},
	}
	for _, test := range tests {
		test.b.oteLogger = onetime.CreateOTELogger(false)
		t.Run(test.name, func(t *testing.T) {
			got := test.b.installBackintHandler(context.Background(), t.TempDir())
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("installBackintHandler()=%v want %v", got, test.want)
			}
		})
	}
}
