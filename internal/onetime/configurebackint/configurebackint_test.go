/*
Copyright 2024 Google LLC

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

package configurebackint

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
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

const (
	fileContents = `{
	"bucket": "test-bucket",
	"recovery_bucket": "backup-bucket",
	"log_to_cloud": true,
	"log_level": "INFO",
	"compress": true,
	"encryption_key": "/my/path",
	"kms_key": "/another/path",
	"retries": 5,
	"parallel_streams": 10,
	"rate_limit_mb": 100,
	"service_account_key": "another/path/here",
	"threads": 100,
	"file_read_timeout_ms": 10000000,
	"buffer_size_mb": 200,
	"retry_backoff_initial": 15,
	"retry_backoff_max": 250,
	"retry_backoff_multiplier": 2.5,
	"log_delay_sec": 5,
	"client_endpoint": "my.endpoint",
	"folder_prefix": "my/prefix",
	"recovery_folder_prefix": "recover/prefix"
}`
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
func defaultWriteFile(numNil int) func(string, []byte, os.FileMode) error {
	return func(string, []byte, os.FileMode) error {
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

func defaultReadFile(numNil int) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if numNil > 0 {
			numNil--
			return []byte(fileContents), nil
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

func TestExecuteConfigureBackint(t *testing.T) {
	tests := []struct {
		name string
		b    ConfigureBackint
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
			name: "SuccessForHelp",
			b: ConfigureBackint{
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
			name: "NoFileSpecified",
			want: subcommands.ExitUsageError,
			b:    ConfigureBackint{},
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "NoFileFound",
			want: subcommands.ExitFailure,
			b: ConfigureBackint{
				fileName: "file-does-not-exist",
			},
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

func TestSynopsisForConfigureBackint(t *testing.T) {
	want := "edit Backint JSON configuration files and migrate from legacy agent's TXT configuration files"
	b := ConfigureBackint{}
	got := b.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForConfigureBackint(t *testing.T) {
	b := ConfigureBackint{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"f", "h", "bucket", "recovery_bucket", "log_to_cloud", "log_level", "compress", "encryption_key", "kms_key", "retries", "parallel_streams", "rate_limit_mb", "service_account_key", "threads", "file_read_timeout_ms", "buffer_size_mb", "retry_backoff_initial", "retry_backoff_max", "retry_backoff_multiplier", "log_delay_sec", "client_endpoint", "folder_prefix", "recovery_folder_prefix", "log-path"}
	b.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestConfigureBackintHandler(t *testing.T) {
	tests := []struct {
		name  string
		b     ConfigureBackint
		flags map[string]string
		want  error
	}{
		{
			name: "FailStat",
			b: ConfigureBackint{
				stat: defaultStat(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FileNotFound",
			b: ConfigureBackint{
				stat: defaultStatNotExist(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailToReadFile",
			b: ConfigureBackint{
				stat:     defaultStat(1),
				readFile: defaultReadFile(0),
			},
			want: cmpopts.AnyError,
		},
		{
			name: "EmptyFile",
			b: ConfigureBackint{
				stat: defaultStat(1),
				readFile: func(string) ([]byte, error) {
					return nil, nil
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "FailToUnmarshal",
			b: ConfigureBackint{
				stat: defaultStat(1),
				readFile: func(string) ([]byte, error) {
					return []byte("{"), nil
				},
			},
			flags: map[string]string{"f": "test.json"},
			want:  cmpopts.AnyError,
		},
		{
			name: "FailToWrite",
			b: ConfigureBackint{
				stat:      defaultStat(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(0),
			},
			flags: map[string]string{"f": "test.json"},
			want:  cmpopts.AnyError,
		},
		{
			name: "FailToChmod",
			b: ConfigureBackint{
				stat:      defaultStat(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(0),
			},
			flags: map[string]string{"f": "test.json"},
			want:  cmpopts.AnyError,
		},
		{
			name: "FailToChown",
			b: ConfigureBackint{
				stat:      defaultStat(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(1),
				chown:     defaultChown(0),
			},
			flags: map[string]string{"f": "test.json"},
			want:  cmpopts.AnyError,
		},
		{
			name: "SuccessNoUpdates",
			b: ConfigureBackint{
				stat:      defaultStat(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(1),
				chown:     defaultChown(1),
			},
			flags: map[string]string{"f": "test.json"},
			want:  nil,
		},
		{
			name: "SuccessUpdateEverything",
			b: ConfigureBackint{
				stat:      defaultStat(1),
				readFile:  defaultReadFile(1),
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(1),
				chown:     defaultChown(1),
			},
			flags: map[string]string{
				"f":                        "test.json",
				"bucket":                   "test-bucket",
				"recovery_bucket":          "backup-bucket",
				"log_to_cloud":             "true",
				"log_level":                "INFO",
				"compress":                 "true",
				"encryption_key":           "/my/path",
				"kms_key":                  "/another/path",
				"retries":                  "5",
				"parallel_streams":         "10",
				"rate_limit_mb":            "100",
				"service_account_key":      "another/path/here",
				"threads":                  "1",
				"file_read_timeout_ms":     "1000",
				"buffer_size_mb":           "200",
				"retry_backoff_initial":    "15",
				"retry_backoff_max":        "250",
				"retry_backoff_multiplier": "2.5",
				"log_delay_sec":            "5",
				"client_endpoint":          "my.endpoint",
				"folder_prefix":            "my/prefix",
				"recovery_folder_prefix":   "recover/prefix",
			},
			want: nil,
		},
		{
			name: "SuccessMigrateFromLegacyTxt",
			b: ConfigureBackint{
				stat: defaultStat(1),
				readFile: func(string) ([]byte, error) {
					return []byte("#BUCKET test-bucket"), nil
				},
				writeFile: defaultWriteFile(1),
				chmod:     defaultChmod(1),
				chown:     defaultChown(1),
			},
			flags: map[string]string{
				"f":      "test.txt",
				"bucket": "test-bucket",
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fs := flag.NewFlagSet("flags", flag.ExitOnError)
			test.b.SetFlags(fs)
			for k, v := range test.flags {
				if err := fs.Set(k, v); err != nil {
					t.Errorf("SetFlags(%#v) failed: %v", fs, err)
				}
			}
			got := test.b.configureBackintHandler(context.Background(), fs)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("configureBackintHandler()=%v want %v", got, test.want)
			}
		})
	}
}
