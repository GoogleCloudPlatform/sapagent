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
	"testing"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.backint.Execute(context.Background(), &flag.FlagSet{}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.backint, test.args, got, test.want)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	b := &Backint{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	b.SetFlags(fs)

	flags := []string{"user", "u", "function", "f", "input", "i", "output", "o", "paramfile", "p", "backupid", "s", "count", "c", "level", "l"}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}
