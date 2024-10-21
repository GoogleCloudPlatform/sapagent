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

// Package status implements the status subcommand to provide information
// on the agent, configuration, IAM and functional statuses.

package status

import (
	"context"
	"fmt"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/status"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestSynopsisForStatus(t *testing.T) {
	want := "get the status of the agent and its services"
	s := Status{}
	got := s.Synopsis()
	if got != want {
		t.Errorf("Synopsis()=%v, want=%v", got, want)
	}
}

func TestSetFlagsForStatus(t *testing.T) {
	s := Status{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"config", "c", "v", "h"}
	s.SetFlags(fs)
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestExecuteStatus(t *testing.T) {
	tests := []struct {
		name string
		s    Status
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
			s: Status{
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
			name: "Success",
			s:    Status{},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.s.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.s, test.args, got, test.want)
			}
		})
	}
}

func TestStatusHandler(t *testing.T) {
	tests := []struct {
		name    string
		s       Status
		want    *spb.AgentStatus
		wantErr error
	}{
		{
			name: "Success",
			s: Status{
				readFile: func(string) ([]byte, error) {
					return nil, nil
				},
			},
			want: &spb.AgentStatus{
				AgentName:             agentPackageName,
				InstalledVersion:      fmt.Sprintf("%s-%s", configuration.AgentVersion, configuration.AgentBuildChange),
				ConfigurationFilePath: configuration.LinuxConfigPath,
				ConfigurationValid:    spb.State_SUCCESS_STATE,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := test.s.statusHandler(context.Background())
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("statusHandler() returned unexpected diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("statusHandler()=%v want %v", gotErr, test.wantErr)
			}
		})
	}
}
