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

package hanachangedisktype

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

var defaultChangeDiskType = &HanaChangeDiskType{
	project:                         "default-project",
	sid:                             "default-sid",
	host:                            "default-host",
	hanaSidAdm:                      "default-hana-sid-adm",
	disk:                            "default-disk",
	diskZone:                        "default-disk-zone",
	newdiskName:                     "default-newdiskname",
	diskKeyFile:                     "default-disk-key-file",
	storageLocation:                 "default-storage-location",
	abandonPrepared:                 true,
	cloudProps:                      defaultCloudProperties,
	forceStopHANA:                   true,
}

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId:    "default-project",
	InstanceName: "default-instance",
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		cdt  HanaChangeDiskType
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
			name: "SuccessfullyParseArgs",
			cdt:  *defaultChangeDiskType,
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			cdt: HanaChangeDiskType{
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
			cdt: HanaChangeDiskType{
				help: true,
			},
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
			got := test.cdt.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.cdt, test.args, got, test.want)
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name    string
		c       *HanaChangeDiskType
		os      string
		wantErr error
	}{
		{
			name: "Success",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "testdiskzone",
				newdiskName: "testnewdiskname",
			},
			os:      "test",
			wantErr: nil,
		},
		{
			name: "Windows",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				newdiskName: "testnewdiskname",
			},
			os:      "windows",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "TooLongDiskName",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "testdiskzone",
				newdiskName: "testnewdiskname**************************************************************",
			},
			os:      "test",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptySid",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "testdiskzone",
				newdiskName: "testnewdiskname",
			},
			os:      "test",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskZone",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "",
				newdiskName: "testnewdiskname",
			},
			os:      "test",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyNewdiskName",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "testdiskzone",
				newdiskName: "",
			},
			os:      "test",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "EmptyDiskZone",
			c: &HanaChangeDiskType{
				project:     "test",
				sid:         "testsid",
				host:        "testhost",
				hanaSidAdm:  "testhanasidadm",
				disk:        "testdisk",
				diskZone:    "",
				newdiskName: "testnewdiskname",
			},
			os:      "test",
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := tc.c.validateParams(tc.os)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("validateParams(%v) returned error: %v, want error: %v", tc.os, gotErr, tc.wantErr)
			}
		})
	}
}

func TestSetFlagsForChangeDiskTypeWorkflow(t *testing.T) {
	c := &HanaChangeDiskType{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	flags := []string{"provisioned-iops", "provisioned-throughput", "h", "v", "loglevel", "sid", "hana-sidadm",
		"source-disk", "source-disk-zone", "new-disk-name", "skip-db-snapshot-for-change-disk-type",
		"source-disk-key-file", "storage-location", "force-stop-hana"}
	c.SetFlags(fs)
	for _, flag := range flags {
		if fs.Lookup(flag) == nil {
			t.Errorf("SetFlags(%v) returned nil, want non-nil", flag)
		}
	}
}

func TestChangeDiskTypeHandler(t *testing.T) {
	defaultChangeDiskType.changeDiskTypeHandler(context.Background(), &flag.FlagSet{}, log.Parameters{})
}
