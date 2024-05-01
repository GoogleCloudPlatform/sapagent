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

package gcbdr

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	hdpb "github.com/GoogleCloudPlatform/sapagent/protos/gcbdrhanadiscovery"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

const (
	validXMLString string = `<applications>
	<application name="deh" friendlytype="SAPHANA" instance="00" DBSID="deh" PORT="00" DBPORT="HDB00" version="2.00.063" datavolowner="dehadm:sapsys" hananodes=" dnwh75rdbci  " masternode=" dnwh75rdbci" standbynode="" extendedworker="" keyname="" dbnames="" uuid="mnt00001" hardwarekey=" 27a36044-d413-7a4c-93cd-e5924d69d4f1" sitename="" configtype="scaleup" clustertype="" replication_nodes="" >
					<files>
									<file path="/hana/data" datavol="/hana/data/DEH" >
									</file>
									<file path="/hana/log" logvol="/hana/log/DEH" >
									</file>
					</files>
					<logbackuppath>
									<file path="/hana/backup/log/DEH" />
					</logbackuppath>
					<globalinipath>
									<file path="/usr/sap/DEH/SYS/global/hdb/custom/config/global.ini" />
					</globalinipath>
					<catalogbackuppath>
									<file path="/hana/backup/log/DEH" />
					</catalogbackuppath>
					<logmode
					/>
					<scripts>
									<script phase="all" path="/act/custom_apps/saphana/CustomApp_SAPHANA.sh" />
					</scripts>
					<volumes>
									<volume name="datavol" mountpoint="/hana/data" vgname="vgdata" lvname="data" >
													<pddisks>
																	<pd disk="/dev/sdc" devicename="hana-data" />
													</pddisks>
									</volume>
									<volume name="logvol" mountpoint="/hana/log" vgname="vglog" lvname="log" >
													<pddisks>
																	<pd disk="/dev/sdd" devicename="hana-log" />
													</pddisks>
									</volume>
					</volumes>
	</application>
</applications>
	`
)

var (
	testCommandExecute = func(stdout, stderr string, err error) commandlineexecutor.Execute {
		return func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			exitCode := 0
			var exitErr *exec.ExitError
			if err != nil && errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return commandlineexecutor.Result{
				StdOut:   stdout,
				StdErr:   stderr,
				Error:    err,
				ExitCode: exitCode,
			}
		}
	}

	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:    "default-project",
		InstanceName: "default-instance",
	}
)

func successfulProtoMessage(xmlString string) []*hdpb.Application {
	appStruct := &Applications{}
	xml.Unmarshal([]byte(xmlString), appStruct)
	return constructApplicationsProto(appStruct)
}

func successfulApplications(xmlString string) *Applications {
	appStruct := &Applications{}
	xml.Unmarshal([]byte(xmlString), appStruct)
	return appStruct
}

func TestSetFlags(t *testing.T) {
	d := &Discovery{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	d.SetFlags(fs)

	flags := []string{"v", "h", "loglevel"}
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
		d    Discovery
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
				"test3",
			},
		},
		{
			name: "SuccessfullyParseArgs",
			d:    Discovery{FSH: &fake.FileSystem{}},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForAgentVersion",
			d:    Discovery{version: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForHelp",
			d:    Discovery{help: true},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.d.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.d, test.args, got, test.want)
			}
		})
	}
}

func TestDiscoveryHandler(t *testing.T) {
	tests := []struct {
		name       string
		fs         *flag.FlagSet
		exec       commandlineexecutor.Execute
		fsh        filesystem.FileSystem
		want       *Applications
		wantStatus subcommands.ExitStatus
	}{
		{
			name: "Success",
			fs:   flag.NewFlagSet("flags", flag.ExitOnError),
			exec: testCommandExecute(validXMLString, "", nil),
			fsh: &fake.FileSystem{
				ReadFileResp: [][]byte{[]byte(validXMLString)},
				ReadFileErr:  []error{nil},
			},
			want:       successfulApplications(validXMLString),
			wantStatus: subcommands.ExitSuccess,
		},
		{
			name:       "FailureInExecutingCommand",
			fs:         flag.NewFlagSet("flags", flag.ExitOnError),
			exec:       testCommandExecute("", "", &exec.ExitError{}),
			fsh:        &fake.FileSystem{},
			want:       nil,
			wantStatus: subcommands.ExitFailure,
		},
		{
			name: "FailureInReadingFile",
			fs:   flag.NewFlagSet("flags", flag.ExitOnError),
			exec: testCommandExecute(validXMLString, "", nil),
			fsh: &fake.FileSystem{
				ReadFileResp: [][]byte{[]byte(validXMLString)},
				ReadFileErr:  []error{cmpopts.AnyError},
			},
			want:       nil,
			wantStatus: subcommands.ExitFailure,
		},
		{
			name: "FailureInUmarshallingXML",
			fs:   flag.NewFlagSet("flags", flag.ExitOnError),
			exec: testCommandExecute("", "", nil),
			fsh: &fake.FileSystem{
				ReadFileResp: [][]byte{[]byte("random")},
				ReadFileErr:  []error{nil},
			},
			want:       nil,
			wantStatus: subcommands.ExitFailure,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Discovery
			got, gotStatus := d.discoveryHandler(ctx, tc.fs, tc.exec, tc.fsh)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("discoveryHandler(%v, %v, %v) returned an unexpected diff (-want +got): %v", tc.fs, tc.exec, tc.fsh, diff)
			}
			if tc.wantStatus != gotStatus {
				t.Errorf("discoveryHandler(%v, %v, %v) returned an unexpected status: %v", tc.fs, tc.exec, tc.fsh, gotStatus)
			}
		})
	}
}

func TestGetHANADiscoveryApplications(t *testing.T) {
	tests := []struct {
		name    string
		fs      *flag.FlagSet
		exec    commandlineexecutor.Execute
		fsh     filesystem.FileSystem
		want    []*hdpb.Application
		wantErr error
	}{
		{
			name: "Success",
			fs:   flag.NewFlagSet("flags", flag.ExitOnError),
			exec: testCommandExecute(validXMLString, "", nil),
			fsh: &fake.FileSystem{
				ReadFileResp: [][]byte{[]byte(validXMLString)},
				ReadFileErr:  []error{nil},
			},
			want:    successfulProtoMessage(validXMLString),
			wantErr: nil,
		},
		{
			name:    "FailureInExecutingCommand",
			fs:      flag.NewFlagSet("flags", flag.ExitOnError),
			exec:    testCommandExecute("", "", &exec.ExitError{}),
			fsh:     &fake.FileSystem{},
			wantErr: cmpopts.AnyError,
			want:    nil,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d Discovery
			got, err := d.GetHANADiscoveryApplications(ctx, tc.fs, tc.exec, tc.fsh)

			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("GetHANADiscoveryApplications(%v, %v, %v) returned an unexpected diff (-want +got): %v", tc.fs, tc.exec, tc.fsh, diff)
			}
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("GetHANADiscoveryApplications(%v, %v, %v) returned an unexpected error: %v", tc.fs, tc.exec, tc.fsh, err)
			}
		})
	}
}
