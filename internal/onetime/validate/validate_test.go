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

package validate

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

//go:embed testdata/collectiondefinition_invalid.json
var invalidCollectionDefinition []byte

func TestExecuteValidate(t *testing.T) {
	tests := []struct {
		name string
		v    Validate
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			args: []any{"test"},
			want: subcommands.ExitUsageError,
		},
		{
			name: "FailAssertFirstArgs",
			args: []any{"test1", "test2"},
			want: subcommands.ExitUsageError,
		},
		{
			name: "Success",
			args: []any{"test", log.Parameters{}},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForAgentVersion",
			v: Validate{
				version: true,
			},
			args: []any{"test", log.Parameters{}},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			v: Validate{
				help: true,
			},
			args: []any{"test", log.Parameters{}},
			want: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validate := Validate{}
			got := validate.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v)=%v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestSetFlagsForValidate(t *testing.T) {
	validate := Validate{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	validate.SetFlags(fs)
	flags := []string{"workloadcollection", "wc"}

	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestValidateWorkloadCollectionHandler(t *testing.T) {
	tests := []struct {
		name string
		read collectiondefinition.ReadFile
		want subcommands.ExitStatus
	}{
		{
			name: "ReadFileError",
			read: func(string) ([]byte, error) { return nil, errors.New("Read File Error") },
			want: subcommands.ExitFailure,
		},
		{
			name: "ValidationError",
			read: func(string) ([]byte, error) { return invalidCollectionDefinition, nil },
			want: subcommands.ExitSuccess,
		},
		{
			name: "Success",
			read: func(string) ([]byte, error) { return configuration.DefaultCollectionDefinition, nil },
			want: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validate := Validate{}
			got := validate.validateWorkloadCollectionHandler(context.Background(), test.read, "filepath")
			if got != test.want {
				t.Errorf("validateWorkloadCollectionHandler() got %v, want %v", got, test.want)
			}
		})
	}
}
