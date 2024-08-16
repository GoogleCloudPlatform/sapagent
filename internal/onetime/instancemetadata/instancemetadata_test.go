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

package instancemetadata

import (
	"context"
	"runtime"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	impb "github.com/GoogleCloudPlatform/sapagent/protos/instancemetadata"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestSetFlags(t *testing.T) {
	m := &InstanceMetadata{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	m.SetFlags(fs)

	flags := []string{
		"loglevel", "help", "h", "log-path",
	}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		m    *InstanceMetadata
		fs   *flag.FlagSet
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			m:    &InstanceMetadata{},
			fs:   &flag.FlagSet{Usage: func() { return }},
			args: []any{},
			want: subcommands.ExitUsageError,
		},
		{
			name: "FailAssertArgs",
			m:    &InstanceMetadata{},
			fs:   &flag.FlagSet{Usage: func() { return }},
			args: []any{
				"test1",
				"test2",
				"test3",
			},
			want: subcommands.ExitUsageError,
		},
		{
			name: "Success",
			m:    &InstanceMetadata{},
			fs:   &flag.FlagSet{Usage: func() { return }},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			want: subcommands.ExitSuccess,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.Execute(context.Background(), tc.fs, tc.args...)
			if got != tc.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", tc.m, tc.args, got, tc.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	runOpts := onetime.CreateRunOptions(
		&ipb.CloudProperties{
			InstanceName: "test-instance",
			Zone:         "us-central1-a",
		},
		false,
	)
	m := &InstanceMetadata{}
	want := &impb.Metadata{}
	want.OsName = runtime.GOOS
	want.AgentVersion = configuration.AgentVersion
	want.AgentBuildChange = configuration.AgentBuildChange
	got, status := m.Run(context.Background(), runOpts)
	if status != subcommands.ExitSuccess {
		t.Errorf("Run(%v)=%v, want %v", runOpts, status, subcommands.ExitSuccess)
	}
	if diff := cmp.Diff(got, want, protocmp.Transform()); diff != "" {
		t.Errorf("Run(%v)=%v, want %v, diff %v", runOpts, got, want, diff)
	}
}
