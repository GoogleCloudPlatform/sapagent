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

package backint

import (
	"context"
	"errors"
	"os"
	"testing"

	"flag"
	s "cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/storage"
	bpb "github.com/GoogleCloudPlatform/sapagent/protos/backint"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	fakeServer = fakestorage.NewServer([]fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "object.txt",
			},
			Content: []byte("test content"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				// The backup object name is in the format <userID>/<fileName>/<externalBackupID>.bak
				Name: "test@TST/object.txt/12345.bak",
			},
			Content: []byte("test content"),
		},
	})
	defaultBucketHandle  = fakeServer.Client().Bucket("test-bucket")
	defaultStorageClient = func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
		return fakeServer.Client(), nil
	}
)

func defaultParametersFile(t *testing.T) *os.File {
	filePath := t.TempDir() + "/parameters.json"
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("os.Create(%v) failed: %v", filePath, err)
	}
	f.WriteString(`{
		"bucket": "test-bucket",
		"retries": 5,
		"parallel_streams": 2,
		"buffer_size_mb": 100,
		"encryption_key": "",
		"compress": false,
		"kms_key": "",
		"service_account": "",
		"rate_limit_mb": 0,
		"file_read_timeout_ms": 1000,
		"dump_data": false,
		"log_level": "INFO",
		"log_delay_sec": 3
	}`)
	return f
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name    string
		backint *Backint
		want    subcommands.ExitStatus
		args    []any
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
			name:    "FailParseAndValidateConfig",
			backint: &Backint{},
			want:    subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			backint: &Backint{
				version: true,
			},
			args: []any{
				"test",
				log.Parameters{},
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			backint: &Backint{
				help: true,
			},
			args: []any{
				"test",
				log.Parameters{},
			},
			want: subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.backint.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.backint, test.args, got, test.want)
			}
		})
	}
}

func TestBackintHandler(t *testing.T) {
	tests := []struct {
		name    string
		backint *Backint
		client  storage.Client
		input   string
		want    subcommands.ExitStatus
	}{
		{
			name:    "FailParseAndValidateConfig",
			backint: &Backint{},
			want:    subcommands.ExitUsageError,
		},
		{
			name: "FailConnectToBucket",
			backint: &Backint{
				user:      "test@TST",
				function:  "backup",
				paramFile: defaultParametersFile(t).Name(),
			},
			client: func(ctx context.Context, opts ...option.ClientOption) (*s.Client, error) {
				return nil, errors.New("client create error")
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "SuccessfulBackup",
			backint: &Backint{
				user:      "test@TST",
				function:  "backup",
				paramFile: defaultParametersFile(t).Name(),
				inFile:    t.TempDir() + "/input.txt",
				outFile:   t.TempDir() + "/output.txt",
			},
			client: defaultStorageClient,
			want:   subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.backint.inFile != "" {
				f, err := os.Create(test.backint.inFile)
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.backint.inFile, err)
				}
				defer f.Close()
			}

			got := test.backint.backintHandler(context.Background(), log.Parameters{}, test.client)
			if got != test.want {
				t.Errorf("(%#v).backintHandler()=%v, want %v", test.backint, got, test.want)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	b := &Backint{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	b.SetFlags(fs)

	flags := []string{"user", "u", "function", "f", "input", "i", "output", "o", "paramfile", "p", "backupid", "s", "count", "c", "level", "l", "v", "h", "loglevel"}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		config *bpb.BackintConfiguration
		bucket *s.BucketHandle
		input  string
		want   bool
	}{
		{
			name: "ErrorOpeningInputFile",
			want: false,
		},
		{
			name: "ErrorOpeningOuputFile",
			config: &bpb.BackintConfiguration{
				InputFile: t.TempDir() + "/input.txt",
			},
			want: false,
		},
		{
			name: "UnspecifiedFunction",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_FUNCTION_UNSPECIFIED,
			},
			want: false,
		},
		{
			name: "BackupFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_BACKUP,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "BackupSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_BACKUP,
			},
			input: `#SOFTWAREID "backint 1.50"`,
			want:  true,
		},
		{
			name: "InquireFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_INQUIRE,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "InquireSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_INQUIRE,
			},
			input: `#SOFTWAREID "backint 1.50"`,
			want:  true,
		},
		{
			name: "DeleteFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_DELETE,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "DeleteSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_DELETE,
			},
			bucket: defaultBucketHandle,
			input:  `#SOFTWAREID "backint 1.50"`,
			want:   true,
		},
		{
			name: "RestoreFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_RESTORE,
			},
			input: "#SOFTWAREID",
			want:  false,
		},
		{
			name: "RestoreSuccess",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_RESTORE,
			},
			bucket: defaultBucketHandle,
			input:  `#SOFTWAREID "backint 1.50"`,
			want:   true,
		},
		{
			name: "DiagnoseFailed",
			config: &bpb.BackintConfiguration{
				InputFile:  t.TempDir() + "/input.txt",
				OutputFile: t.TempDir() + "/output.txt",
				Function:   bpb.Function_DIAGNOSE,
			},
			bucket: nil,
			want:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.config.GetInputFile() != "" {
				f, err := os.Create(test.config.GetInputFile())
				if err != nil {
					t.Fatalf("os.Create(%v) failed: %v", test.config.GetInputFile(), err)
				}
				f.WriteString(test.input)
				defer f.Close()
			}

			got := run(context.Background(), test.config, test.bucket)
			if got != test.want {
				t.Errorf("run(%#v) = %v, want %v", test.config, got, test.want)
			}
		})
	}
}
