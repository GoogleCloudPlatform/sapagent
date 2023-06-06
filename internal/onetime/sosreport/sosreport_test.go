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

package sosreport

import (
	"context"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

func TestSetFlagsForSOSReport(t *testing.T) {
	sosrc := SOSReport{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"sid", "instance-numbers", "hostname"}
	sosrc.SetFlags(fs)
	for _, flagName := range flags {
		got := fs.Lookup(flagName)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flagName)
		}
	}
}

func TestExecuteForSOSReport(t *testing.T) {
	tests := []struct {
		name string
		sosr *SOSReport
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
			sosr: &SOSReport{sid: "DEH", instanceNums: "00 01", hostname: "sample_host"},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sosr.Execute(context.Background(), &flag.FlagSet{}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.sosr, test.args, got, test.want)
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name  string
		sosrc SOSReport
		want  []string
	}{
		{
			name:  "NoValueForSID",
			sosrc: SOSReport{instanceNums: "00 01", hostname: "sample_host"},
			want:  []string{"no value provided for sid"},
		},
		{
			name:  "NoValueForInstance",
			sosrc: SOSReport{sid: "DEH", instanceNums: "", hostname: "sample_host"},
			want:  []string{"no value provided for instance-numbers"},
		},
		{
			name:  "InvalidValueForinstanceNums",
			sosrc: SOSReport{sid: "DEH", instanceNums: "00 011", hostname: "sample_host"},
			want:  []string{"invalid instance number 011"},
		},
		{
			name:  "NoValueForHostName",
			sosrc: SOSReport{sid: "DEH", instanceNums: "00 01", hostname: ""},
			want:  []string{"no value provided for hostname"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.sosrc.validateParams()
			if len(got) != len(test.want) || !cmp.Equal(got, test.want) {
				t.Errorf("validateParams() = %v, want %v", got, test.want)
			}
		})
	}
}
