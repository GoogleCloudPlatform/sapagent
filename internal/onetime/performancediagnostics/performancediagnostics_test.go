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

package performancediagnostics

import (
	"context"
	"fmt"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/backint/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId:    "default-project",
	InstanceName: "default-instance",
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		args []any
		want subcommands.ExitStatus
	}{
		{
			name: "FailLengthArgs",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailParseAndValidateConfig",
			d:    &Diagnose{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForAgentVersion",
			d: &Diagnose{
				version: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			d: &Diagnose{
				help: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, tc.args...)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Execute() returned an unexpected diff (-want +got): %v", diff)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	c := &Diagnose{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	c.SetFlags(fs)

	flags := []string{
		"scope", "test-bucket", "param-file", "result-bucket", "bundle-name",
		"override-hyper-threading", "path", "loglevel", "help", "h", "version", "v",
	}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		want bool
	}{
		{
			name: "InvalidScope",
			d: &Diagnose{
				scope: "nw",
			},
			want: false,
		},
		{
			name: "NoParamFileAndTestBucket1",
			d: &Diagnose{
				scope: "all",
			},
			want: false,
		},
		{
			name: "NoParamFileAndTestBucket2",
			d: &Diagnose{
				scope: "backup",
			},
			want: false,
		},
		{
			name: "ParamFilePresent",
			d: &Diagnose{
				scope:     "all",
				paramFile: "/tmp/param_file.txt",
			},
			want: true,
		},
		{
			name: "TestBucketPresent",
			d: &Diagnose{
				scope:      "all",
				testBucket: "test_bucket",
			},
			want: true,
		},
		{
			name: "Valid1",
			d: &Diagnose{
				scope:      "io",
				path:       "/tmp/path.txt",
				bundleName: "test_bundle",
			},
			want: true,
		},
		{
			name: "Valid2",
			d: &Diagnose{
				scope:      "backup, io",
				testBucket: "test_bucket",
				path:       "/tmp/path.txt",
				bundleName: "test_bundle",
			},
			want: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.validateParams(ctx, &flag.FlagSet{Usage: func() { return }})
			if got != tc.want {
				t.Errorf("validateParams(ctx) = %v, want: %v", got, tc.want)
			}
		})
	}
}

func TestListOperations(t *testing.T) {
	tests := []struct {
		name       string
		operations []string
		want       map[string]struct{}
	}{
		{
			name:       "InvalidOpPresent",
			operations: []string{"backup", " nw"},
			want: map[string]struct{}{
				"backup": {},
			},
		},
		{
			name:       "AllPresent",
			operations: []string{"all", "backup", "io"},
			want: map[string]struct{}{
				"all": {},
			},
		},
		{
			name:       "AllAbsent",
			operations: []string{"backup", "io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
		{
			name:       "Spaces",
			operations: []string{" backup  ", "  io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
		{
			name:       "DuplicateOps",
			operations: []string{"backup", "backup", "io", "io", " io"},
			want: map[string]struct{}{
				"backup": {},
				"io":     {},
			},
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := listOperations(ctx, tc.operations)
			if diff := cmp.Diff(tc.want, got, cmpopts.SortMaps(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("listOperations(%v) returned an unexpected diff (-want +got): %v", tc.operations, diff)
			}
		})
	}
}

func TestSetBucket(t *testing.T) {
	tests := []struct {
		name       string
		d          *Diagnose
		readFunc   ReadConfigFile
		fs         filesystem.FileSystem
		wantBucket string
		wantErr    error
	}{
		{
			name: "ReadError",
			d:    &Diagnose{},
			readFunc: func(string) ([]byte, error) {
				return nil, fmt.Errorf("error")
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "UnmarshalError",
			d:    &Diagnose{},
			readFunc: func(string) ([]byte, error) {
				fileContent := `{"test_bucket": "test_bucket", "enc": "true"}`
				return []byte(fileContent), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "NoBucket",
			d:    &Diagnose{},
			readFunc: func(string) ([]byte, error) {
				return []byte{}, nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TestBucketEmpty",
			d:    &Diagnose{},
			readFunc: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket"}`
				return []byte(fileContent), nil
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TestBucketPresent",
			d: &Diagnose{
				testBucket: "test_bucket1",
				path:       "/tmp/",
				paramFile:  "/param_file.json",
			},
			readFunc: func(string) ([]byte, error) {
				fileContent := `{"bucket": "test_bucket2"}`
				return []byte(fileContent), nil
			},
			fs:         filesystem.Helper{},
			wantBucket: "test_bucket1",
			wantErr:    nil,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.d.setBucket(ctx, tc.readFunc, tc.fs)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("setBucket(%v, %v) returned error: %v, want error: %v", tc.readFunc, tc.fs, gotErr, tc.wantErr)
			}

			if tc.wantBucket != "" {
				content, err := os.ReadFile(tc.d.paramFile)
				if err != nil {
					t.Fatalf("os.ReadFile(%s) failed: %v", tc.d.paramFile, err)
				}
				config, err := configuration.Unmarshal(tc.d.paramFile, content)
				if err != nil {
					t.Fatalf("configuration.Unmarshal(%s) failed: %v", tc.d.paramFile, err)
				}
				if config.GetBucket() != tc.wantBucket {
					t.Errorf("setBucket(%v, %v) = %v, want %v", tc.readFunc, tc.fs, config.GetBucket(), tc.wantBucket)
				}
			}
		})
	}
}

func TestAddToBundle(t *testing.T) {
	tests := []struct {
		name    string
		paths   []moveFiles
		fs      filesystem.FileSystem
		wantErr error
	}{
		{
			name:    "Empty",
			wantErr: nil,
		},
		{
			name: "InvalidPaths",
			paths: []moveFiles{
				{
					oldPath: "/tmp/old_path",
				},
				{
					newPath: "/tmp/new_path",
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ValidPaths",
			paths: []moveFiles{
				{
					oldPath: "/tmp/old/path.txt",
					newPath: "/tmp/new/path.txt",
				},
			},
			fs:      filesystem.Helper{},
			wantErr: nil,
		},
	}

	ctx := context.Background()
	if err := os.MkdirAll("/tmp/old", 0777); err != nil {
		fmt.Printf("os.MkdirAll(%s, 0777) failed: %v", "/tmp/old", err)
	}
	if err := os.MkdirAll("/tmp/new", 0777); err != nil {
		fmt.Printf("os.MkdirAll(%s, 0777) failed: %v", "/tmp/new", err)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range tc.paths {
				if path.oldPath == "" || path.newPath == "" {
					continue
				}
				if _, err := tc.fs.Create(path.oldPath); err != nil {
					fmt.Printf("tc.fs.Create(%s) failed, failed to create temp file at oldPath: %v", path.oldPath, err)
				}
			}
			gotErr := addToBundle(ctx, tc.paths, tc.fs)
			if diff := cmp.Diff(gotErr, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("addToBundle(%v, %v) returned error: %v, want error: %v", tc.paths, tc.fs, gotErr, tc.wantErr)
			}

			for _, path := range tc.paths {
				if path.oldPath == "" || path.newPath == "" {
					continue
				}
				if _, err := tc.fs.Stat(path.newPath); os.IsNotExist(err) {
					t.Errorf("addToBundle(%v, %v) did not create new file: %s", tc.paths, tc.fs, path.newPath)
				}
			}
		})
	}
}

func TestGetParamFileName(t *testing.T) {
	tests := []struct {
		name string
		d    *Diagnose
		want string
	}{
		{
			name: "Empty",
			d:    &Diagnose{},
		},
		{
			name: "SampleParamFile1",
			d: &Diagnose{
				paramFile: "/tmp/sample_param_file.txt",
			},
			want: "sample_param_file.txt",
		},
		{
			name: "SampleParamFile2",
			d: &Diagnose{
				paramFile: "sample_param_file.txt",
			},
			want: "sample_param_file.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.d.getParamFileName()
			if got != test.want {
				t.Errorf("getParamFileName(%#v) = %s, want %s", test.d, got, test.want)
			}
		})
	}
}
