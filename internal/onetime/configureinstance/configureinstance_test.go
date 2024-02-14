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

package configureinstance

import (
	"context"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestExecuteConfigureInstance(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
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
			name: "FailAssertSecondArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				"test3",
			},
		},
		{
			name: "SuccessForAgentVersion",
			c: ConfigureInstance{
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
			c: ConfigureInstance{
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
			name: "NoSubcommandSupplied",
			want: subcommands.ExitUsageError,
			c:    ConfigureInstance{},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "BothSubcommandsSupplied",
			want: subcommands.ExitUsageError,
			c: ConfigureInstance{
				check: true,
				apply: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "UnsupportedMachineType",
			want: subcommands.ExitFailure,
			c: ConfigureInstance{
				apply:       true,
				machineType: "",
			},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "X4MachineType",
			want: subcommands.ExitSuccess,
			c: ConfigureInstance{
				apply:       true,
				machineType: "x4-megamem-1440-metal",
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
			got := test.c.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.c, test.args, got, test.want)
			}
		})
	}
}

func TestSynopsisForConfigureInstance(t *testing.T) {
	want := "check and apply OS settings to support SAP HANA workloads"
	c := ConfigureInstance{}
	got := c.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForConfigureInstance(t *testing.T) {
	c := ConfigureInstance{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"check", "apply", "overrideType", "overrideOLAP", "h", "v"}
	c.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestConfigureInstanceHandler(t *testing.T) {
	tests := []struct {
		name string
		c    ConfigureInstance
		want error
	}{
		{
			name: "UnsupportedMachineType",
			c: ConfigureInstance{
				machineType: "",
			},
			want: cmpopts.AnyError,
		},
		{
			name: "X4MachineType",
			c: ConfigureInstance{
				machineType: "x4-megamem-1440-metal",
			},
			want: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.c.configureInstanceHandler(context.Background())
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("configureInstanceHandler()=%v want %v", got, test.want)
			}
		})
	}
}
