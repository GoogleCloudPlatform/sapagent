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
	"embed"
	"io"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	impb "github.com/GoogleCloudPlatform/sapagent/protos/instancemetadata"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

var (
	//go:embed test_data/os-release.txt
	testFS embed.FS
)

func defaultReadCloser(path string) (io.ReadCloser, error) {
	file, err := testFS.Open(path)
	var f io.ReadCloser = file
	return f, err
}

func TestSetFlags(t *testing.T) {
	m := &InstanceMetadata{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	m.SetFlags(fs)

	flags := []string{
		"loglevel", "help", "h", "log-path", "os-release-path",
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
			m: &InstanceMetadata{
				OSReleasePath: "test_data/os-release.txt",
				RC:            defaultReadCloser,
			},
			fs: &flag.FlagSet{Usage: func() { return }},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "Failure",
			m: &InstanceMetadata{
				OSReleasePath: "test_data/os-release.txt",
				RC: func(string) (io.ReadCloser, error) {
					return nil, cmpopts.AnyError
				},
			},
			fs: &flag.FlagSet{Usage: func() { return }},
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
			want: subcommands.ExitFailure,
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

// checks the failure case only to cover lines
func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		m          *InstanceMetadata
		opts       *onetime.RunOptions
		want       *impb.Metadata
		wantMsg    string
		wantStatus subcommands.ExitStatus
	}{
		{
			name: "Fail",
			m: &InstanceMetadata{
				OSReleasePath: "",
				RC: func(path string) (io.ReadCloser, error) {
					return nil, cmpopts.AnyError
				},
			},
			opts: onetime.CreateRunOptions(
				&ipb.CloudProperties{
					InstanceName: "test-instance",
					Zone:         "us-central1-a",
				},
				false,
			),
			want:       nil,
			wantMsg:    "could not read OS release info, error: any error",
			wantStatus: subcommands.ExitFailure,
		},
		{
			name: "Success",
			m: &InstanceMetadata{
				OSReleasePath: "test_data/os-release.txt",
				RC:            defaultReadCloser,
			},
			opts: onetime.CreateRunOptions(
				&ipb.CloudProperties{
					InstanceName: "test-instance",
					Zone:         "us-central1-a",
				},
				false,
			),
			want: &impb.Metadata{
				OsName:           "linux",
				OsVendor:         "debian",
				OsVersion:        "11",
				AgentVersion:     configuration.AgentVersion,
				AgentBuildChange: configuration.AgentBuildChange,
			},
			wantStatus: subcommands.ExitSuccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotMsg, gotStatus := tc.m.Run(context.Background(), tc.opts)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Run(%v, %v) returned an unexpected diff (-want +got): %v", tc.m, tc.opts, diff)
			}
			if diff := cmp.Diff(tc.wantMsg, gotMsg); diff != "" {
				t.Errorf("Run(%v, %v) returned an unexpected diff (-want +got): %v", tc.m, tc.opts, diff)
			}
			if diff := cmp.Diff(tc.wantStatus, gotStatus); diff != "" {
				t.Errorf("Run(%v, %v) returned an unexpected diff (-want +got): %v", tc.m, tc.opts, diff)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	im := &InstanceMetadata{}
	got := im.Usage()
	want := "Usage: instancemetadata [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>] [-h]\n"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Usage() returned an unexpected diff (-want +got): %v", diff)
	}
}

func TestSynopsis(t *testing.T) {
	im := &InstanceMetadata{}
	got := im.Synopsis()
	want := "fetch the metadata of the instance"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Synopsis() returned an unexpected diff (-want +got): %v", diff)
	}
}

func TestSetDefaults(t *testing.T) {
	im := &InstanceMetadata{}
	im.setDefaults()
	if im.OSReleasePath == "" {
		t.Errorf("setDefaults() OSReleasePath not set")
	}
	if im.RC == nil {
		t.Errorf("setDefaults() RC not set")
	}
}


