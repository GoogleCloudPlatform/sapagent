/*
Copyright 2022 Google LLC

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

package sapdiscovery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

const (
	defaultInstanceName    = "test-instance-id"
	defaultProjectID       = "test-project-id"
	defaultZone            = "test-zone"
	defaultUserstoreOutput = `
KEY default
	ENV: 
	a:b:c
  ENV : test-instance:30013
  USER: SAPABAP1
  DATABASE: DEH
Operation succeed.
`
	defaultSID                = "ABC"
	defaultInstanceNumber     = "00"
	landscapeOutputSingleNode = `
| Host        | Host   | Host   | Failover | Remove | Storage   | Storage   | Failover | Failover | NameServer | NameServer | IndexServer | IndexServer | Host    | Host    | Worker  | Worker  |
|             | Active | Status | Status   | Status | Config    | Actual    | Config   | Actual   | Config     | Actual     | Config      | Actual      | Config  | Actual  | Config  | Actual  |
|             |        |        |          |        | Partition | Partition | Group    | Group    | Role       | Role       | Role        | Role        | Roles   | Roles   | Groups  | Groups  |
| ----------- | ------ | ------ | -------- | ------ | --------- | --------- | -------- | -------- | ---------- | ---------- | ----------- | ----------- | ------- | ------- | ------- | ------- |
| test-instance   | yes    | info   |          |        |         1 |         0 | default  | default  | master 1   | slave      | worker      | standby     | worker  | standby | default | -       |

overall host status: info
`
	landscapeOutputMultipleNodes = `
| Host        | Host   | Host   | Failover | Remove | Storage   | Storage   | Failover | Failover | NameServer | NameServer | IndexServer | IndexServer | Host    | Host    | Worker  | Worker  |
|             | Active | Status | Status   | Status | Config    | Actual    | Config   | Actual   | Config     | Actual     | Config      | Actual      | Config  | Actual  | Config  | Actual  |
|             |        |        |          |        | Partition | Partition | Group    | Group    | Role       | Role       | Role        | Role        | Roles   | Roles   | Groups  | Groups  |
| ----------- | ------ | ------ | -------- | ------ | --------- | --------- | -------- | -------- | ---------- | ---------- | ----------- | ----------- | ------- | ------- | ------- | ------- |
| test-instance   | yes    | info   |          |        |         1 |         0 | default  | default  | master 1   | slave      | worker      | standby     | worker  | standby | default | -       |
| test-instancew1 | yes    | ok     |          |        |         2 |         2 | default  | default  | master 2   | slave      | worker      | slave       | worker  | worker  | default | default |
| test-instancew2 | yes    | ok     |          |        |         3 |         3 | default  | default  | slave      | slave      | worker      | slave       | worker  | worker  | default | default |
| test-instancew3 | yes    | info   |          |        |         0 |         1 | default  | default  | master 3   | master     | standby     | master      | standby | worker  | default | default |

overall host status: info
`
	defaultAppMountOutput = `
Filesystem                        Size  Used Avail Use% Mounted on
udev                               48G     0   48G   0% /dev
tmpfs                             9.5G  4.2M  9.5G   1% /run
1.2.3.4:/vol                        8G     0    8G   0% /sapmnt/abc
tmpfs                              48G  2.0M   48G   1% /dev/shm
	`
	defaultDBMountOutput = `
	Filesystem                        Size  Used Avail Use% Mounted on
	udev                               48G     0   48G   0% /dev
	tmpfs                             9.5G  4.2M  9.5G   1% /run
	1.2.3.4:/vol                        8G     0    8G   0% /hana/shared
	tmpfs                              48G  2.0M   48G   1% /dev/shm
		`
)

var (
	defaultCloudProperties = &instancepb.CloudProperties{
		InstanceName: defaultInstanceName,
		ProjectId:    defaultProjectID,
		Zone:         defaultZone,
	}
	defaultUserStoreResult = commandlineexecutor.Result{
		StdOut: defaultUserstoreOutput,
	}
	netweaverMountResult = commandlineexecutor.Result{
		StdOut: defaultAppMountOutput,
	}
	defaultProfileResult = commandlineexecutor.Result{
		StdOut: "rdisp/mshost = some-test-ascs",
	}
	hanaMountResult = commandlineexecutor.Result{
		StdOut: defaultDBMountOutput,
	}
	landscapeSingleNodeResult    = commandlineexecutor.Result{StdOut: landscapeOutputSingleNode}
	landscapeMultipleNodesResult = commandlineexecutor.Result{
		StdOut: landscapeOutputMultipleNodes,
	}
)

func sortSapSystemDetails(a, b sapSystemDetails) bool {
	if a.appSID == b.appSID {
		return a.dbSID < b.dbSID
	}
	return a.appSID < b.appSID
}

type fakeCommandExecutor struct {
	t                *testing.T
	params           []commandlineexecutor.Params
	results          []commandlineexecutor.Result
	executeCallCount int
}

func (f *fakeCommandExecutor) Execute(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	defer func() { f.executeCallCount++ }()
	if diff := cmp.Diff(f.params[f.executeCallCount], params, cmpopts.IgnoreFields(commandlineexecutor.Params{}, "Args", "ArgsToSplit")); diff != "" {
		f.t.Errorf("Execute params mismatch (-want, +got):\n%s", diff)
	}
	return f.results[f.executeCallCount]
}

func TestDiscoverAppToDBConnection(t *testing.T) {
	tests := []struct {
		name    string
		exec    func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want    []string
		wantErr error
	}{{
		name: "appToDBWithIPAddr",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		want: []string{"test-instance"},
	}, {
		name: "errGettingUserStore",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noHostsInUserstoreOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := sapDiscovery{
				execute: test.exec,
			}
			got, err := d.discoverAppToDBConnection(context.Background(), defaultSID)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverAppToDBConnection() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverAppToDBConnection() error: got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestDiscoverDatabaseSID(t *testing.T) {
	var execCalls map[string]int
	tests := []struct {
		name          string
		testSID       string
		exec          commandlineexecutor.Execute
		want          string
		wantErr       error
		wantExecCalls map[string]int
	}{{
		name:    "hdbUserStoreErr",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr: cmpopts.AnyError,

		wantExecCalls: map[string]int{"sudo": 1},
	}, {
		name:    "profileGrepErr",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			if params.Executable == "sudo" {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			}
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr:       cmpopts.AnyError,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name:    "noSIDInGrep",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		wantErr:       cmpopts.AnyError,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name:    "sidInUserStore",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			if params.Executable == "sudo" {
				return commandlineexecutor.Result{
					StdOut: `KEY default
					ENV : dnwh75ldbci:30013
					USER: SAPABAP1
					DATABASE: DEH
				Operation succeed.`,
					StdErr: "",
				}
			}
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		want:          "DEH",
		wantErr:       nil,
		wantExecCalls: map[string]int{"sudo": 1},
	}, {
		name:    "sidInProfiles",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			if params.Executable == "sudo" {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			}
			return commandlineexecutor.Result{
				StdOut: `
				grep: /usr/sap/S15/SYS/profile/DEFAULT.PFL: Permission denied
				/usr/sap/S15/SYS/profile/s:rsdb/dbid = HN1`,
				StdErr: "",
			}
		},
		want:          "HN1",
		wantErr:       nil,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execCalls = make(map[string]int)
			d := sapDiscovery{
				execute: test.exec,
			}
			got, gotErr := d.discoverDatabaseSID(context.Background(), test.testSID)
			if test.want != "" {
				if got != test.want {
					t.Errorf("discoverDatabaseSID() = %q, want %q", got, test.want)
				}
			}
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverDatabaseSID() gotErr %q, wantErr %q", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantExecCalls, execCalls); diff != "" {
				t.Errorf("discoverDatabaseSID() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverDBNodes(t *testing.T) {
	tests := []struct {
		name           string
		sid            string
		instanceNumber string
		execute        commandlineexecutor.Execute
		want           []string
		wantErr        error
	}{{
		name:           "discoverSingleNode",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputSingleNode,
			}
		},
		want:    []string{"test-instance"},
		wantErr: nil,
	}, {
		name:           "discoverMultipleNodes",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputMultipleNodes,
			}
		},
		want: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
	}, {
		name:           "pythonScriptReturnsNonfatalCode",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   landscapeOutputSingleNode,
				Error:    cmpopts.AnyError,
				ExitCode: 3,
			}
		},
		want: []string{"test-instance"},
	}, {
		name:           "pythonScriptFails",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   landscapeOutputSingleNode,
				Error:    cmpopts.AnyError,
				ExitCode: 1,
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name:           "noHostsInOutput",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{}
		},
		want: nil,
	}, {
		name:           "sidNotProvided",
		instanceNumber: defaultInstanceNumber,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputSingleNode,
			}
		},
		want:    nil,
		wantErr: cmpopts.AnyError,
	}, {
		name: "instanceNumberNotProvided",
		sid:  defaultSID,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputSingleNode,
			}
		},
		want:    nil,
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := sapDiscovery{
				execute: test.execute,
			}
			got, err := d.discoverDBNodes(context.Background(), test.sid, test.instanceNumber)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverDBNodes() mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("discoverDBNodes() error mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverASCS(t *testing.T) {
	tests := []struct {
		name    string
		sid     string
		execute commandlineexecutor.Execute
		want    string
		wantErr error
	}{
		{
			name: "discoverASCS",
			sid:  defaultSID,
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "some extra line\nrdisp/mshost = some-test-ascs ",
				}
			},
			want: "some-test-ascs",
		}, {
			name: "errorExecutingCommand",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: errors.New("Error running command"), ExitCode: 1}
			},
			wantErr: cmpopts.AnyError,
		}, {
			name: "noHostInProfile",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "some extra line\nrno host in output",
				}
			},
			wantErr: cmpopts.AnyError,
		}, {
			name: "emptyHostInProfile",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "some extra line\nrdisp/mshost = ",
				}
			},
			want: "",
		}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := sapDiscovery{
				execute: test.execute,
			}
			got, err := d.discoverASCS(context.Background(), test.sid)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverASCS() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Unexpected error from discoverASCS (got, want), (%s, %s)", err, test.wantErr)
			}
		})
	}
}

func TestDiscoverAppNFS(t *testing.T) {
	tests := []struct {
		name    string
		sid     string
		execute commandlineexecutor.Execute
		want    string
		wantErr error
	}{
		{
			name: "discoverAppNFS",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
					StdErr:   "",
					ExitCode: 0,
				}
			},
			want: "1.2.3.4",
		}, {
			name: "errorExecutingCommand",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{Error: errors.New("Error running command"), ExitCode: 1}
			},
			wantErr: cmpopts.AnyError,
		}, {
			name: "noNFSInMounts",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					// StdOut: "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
					StdOut:   "some extra line\n/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
					StdErr:   "",
					ExitCode: 0,
				}
			},
			wantErr: cmpopts.AnyError,
		}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			d := sapDiscovery{
				execute: test.execute,
			}
			got, err := d.discoverAppNFS(context.Background(), test.sid)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverAppNFS() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Unexpected error from discoverAppNFS (got, want), (%s, %s)", err, test.wantErr)
			}
		})
	}
}

func TestDiscoverDatabaseNFS(t *testing.T) {
	tests := []struct {
		name    string
		execute func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want    string
		wantErr error
	}{{
		name: "discoverDatabaseNFS",
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /hana/shared",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		want: "1.2.3.4",
	}, {
		name: "errorExecutingCommand",
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{Error: errors.New("Error running command"), ExitCode: 1}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noNFSInMounts",
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\nsome other line\n",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		wantErr: cmpopts.AnyError,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := sapDiscovery{
				execute: test.execute,
			}
			got, err := d.discoverDatabaseNFS(context.Background())
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverDatabaseNFS() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Unexpected error from discoverDatabaseNFS (got, want), (%s, %s)", err, test.wantErr)
			}
		})
	}
}

func TestDiscoverNetweaver(t *testing.T) {
	tests := []struct {
		name    string
		app     *sappb.SAPInstance
		execute commandlineexecutor.Execute
		want    sapSystemDetails
	}{{
		name: "justNetweaverConnectedToDB",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return defaultUserStoreResult
			case "grep":
				return defaultProfileResult
			case "df":
				return netweaverMountResult
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		want: sapSystemDetails{
			appSID:  "abc",
			dbSID:   "DEH",
			dbHosts: []string{"test-instance"},
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
		},
	}, {
		name: "noDBSID",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		want: sapSystemDetails{},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := sapDiscovery{execute: tc.execute}
			got := d.discoverNetweaver(ctx, tc.app)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(sapSystemDetails{}), protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaver(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
		})
	}
}

func TestDiscoverHANA(t *testing.T) {
	tests := []struct {
		name    string
		app     *sappb.SAPInstance
		execute commandlineexecutor.Execute
		want    sapSystemDetails
	}{{
		name: "singleNode",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return landscapeSingleNodeResult
			case "df":
				return hanaMountResult
			}
			return commandlineexecutor.Result{
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		want: sapSystemDetails{
			dbSID:   "abc",
			dbHosts: []string{"test-instance"},
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
		},
	}, {
		name: "errGettingNodes",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdErr:   "Lanscape error",
				Error:    errors.New("Lanscape error"),
				ExitCode: 1,
			}
		},
		want: sapSystemDetails{},
	}, {
		name: "",
		app:  &sappb.SAPInstance{},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputSingleNode,
			}
		},
		want: sapSystemDetails{},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := sapDiscovery{execute: tc.execute}
			got := d.discoverHANA(ctx, tc.app)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(sapSystemDetails{}), protocmp.Transform()); diff != "" {
				t.Errorf("discoverHANA(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
		})
	}
}

func TestDiscoverSAPApps(t *testing.T) {
	tests := []struct {
		name         string
		cp           *instancepb.CloudProperties
		executor     *fakeCommandExecutor
		sapInstances *sappb.SAPInstances
		want         []sapSystemDetails
	}{{
		name:         "noSAPApps",
		sapInstances: &sappb.SAPInstances{},
		executor:     &fakeCommandExecutor{},
		want:         []sapSystemDetails{},
	}, {
		name: "justHANA",
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "abc",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{landscapeSingleNodeResult, hanaMountResult},
		},
		want: []sapSystemDetails{
			{
				dbSID:    "abc",
				dbOnHost: true,
				dbHosts:  []string{"test-instance"},
				dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
					DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					SharedNfsUri: "1.2.3.4",
				},
			},
		},
	}, {
		name: "justNetweaver",
		cp:   defaultCloudProperties,
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
			},
		},
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
			},
			dbSID:   "DEH",
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "twoNetweaver",
		cp:   defaultCloudProperties,
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
			},
		},
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
				&sappb.SAPInstance{
					Sapsid: "def",
					Type:   sappb.InstanceType_NETWEAVER,
				},
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
			},
			dbSID:   "DEH",
			dbHosts: []string{"test-instance"},
		}, {
			appSID:    "def",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				AscsUri:         "some-test-ascs",
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
			},
			dbSID:   "DEH",
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "twoHANA",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "abc",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
				&sappb.SAPInstance{
					Sapsid:         "def",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{landscapeSingleNodeResult, hanaMountResult, landscapeSingleNodeResult, hanaMountResult},
		},
		want: []sapSystemDetails{{
			dbSID:    "abc",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}, {
			dbSID:    "def",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "netweaverThenHANAConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
				&sappb.SAPInstance{
					Sapsid:         "DEH",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult, landscapeSingleNodeResult, hanaMountResult,
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			dbSID:    "DEH",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "hanaThenNetweaverConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "DEH",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult, hanaMountResult,
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			dbSID:    "DEH",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "netweaverThenHANANotConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
				&sappb.SAPInstance{
					Sapsid:         "DB2",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult, landscapeSingleNodeResult, hanaMountResult,
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			dbSID:    "DEH",
			dbOnHost: false,
			dbHosts:  []string{"test-instance"},
		}, {
			dbSID:    "DB2",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name: "hanaThenNetweaverNotConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "DB2",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
				&sappb.SAPInstance{
					Sapsid: "abc",
					Type:   sappb.InstanceType_NETWEAVER,
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
			}, {
				Executable: "df",
			}, {
				Executable: "sudo",
			}, {
				Executable: "sudo",
			}, {
				Executable: "grep",
			}, {
				Executable: "df",
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult, hanaMountResult,
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
			},
		},
		want: []sapSystemDetails{{
			appSID:    "abc",
			appOnHost: true,
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			dbSID:    "DEH",
			dbOnHost: false,
			dbHosts:  []string{"test-instance"},
		}, {
			dbSID:    "DB2",
			dbOnHost: true,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		}},
	}, {
		name:         "",
		cp:           defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{},
		executor:     &fakeCommandExecutor{},
		want:         []sapSystemDetails{},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.executor.t = t
			d := sapDiscovery{
				appsDiscovery: func(context.Context) *sappb.SAPInstances { return tc.sapInstances },
				execute: func(c context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
					return tc.executor.Execute(c, p)
				},
			}
			got := d.discoverSAPApps(ctx, tc.cp)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(sapSystemDetails{}), cmpopts.SortSlices(sortSapSystemDetails), protocmp.Transform(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("discoverSAPApps(%v) returned an unexpected diff (-want +got): %v", tc.cp, diff)
			}
		})
	}
}

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{{
		name:  "singleDuplicate",
		input: []string{"foo", "foo", "bar"},
		want:  []string{"foo", "bar"},
	}, {
		name:  "noDuplicates",
		input: []string{"foo", "bar"},
		want:  []string{"foo", "bar"},
	}, {
		name:  "empty",
		input: nil,
		want:  []string{},
	}, {
		name:  "unsortedSingleDuplicate",
		input: []string{"foo", "bar", "foo"},
		want:  []string{"foo", "bar"},
	}, {
		name:  "repeatedDuplicate",
		input: []string{"foo", "foo", "foo", "bar", "bar", "bar"},
		want:  []string{"foo", "bar"},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := removeDuplicates(test.input)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("removeDuplicates() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestMergeSystemDetails(t *testing.T) {
	tests := []struct {
		name       string
		oldDetails sapSystemDetails
		newDetails sapSystemDetails
		want       sapSystemDetails
	}{{
		name:       "empty",
		oldDetails: sapSystemDetails{},
		newDetails: sapSystemDetails{},
		want:       sapSystemDetails{},
	}, {
		name: "onlyOld",
		oldDetails: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		},
		newDetails: sapSystemDetails{},
		want: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
			dbHosts: []string{"test-instance"},
		},
	}, {
		name:       "onlyNew",
		oldDetails: sapSystemDetails{},
		newDetails: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},
			dbHosts: []string{"test-instance"},
		},
		want: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},
			dbHosts: []string{"test-instance"},
		},
	}, {
		name: "identicalOldAndNew",
		oldDetails: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},

			dbHosts: []string{"test-instance"},
		}, newDetails: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},
			dbHosts: []string{"test-instance"},
		},
		want: sapSystemDetails{
			appSID: "abc",
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
			appHosts: []string{"test-instance"},
			dbSID:    "DEH",
			dbOnHost: false,
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},
			dbHosts: []string{"test-instance"},
		},
	}, {
		name:       "oldAppNotOnHostNewAppOnHost",
		oldDetails: sapSystemDetails{appOnHost: false},
		newDetails: sapSystemDetails{appOnHost: true},
		want:       sapSystemDetails{appOnHost: true},
	}, {
		name:       "oldAppOnHostNewAppNotOnHost",
		oldDetails: sapSystemDetails{appOnHost: true},
		newDetails: sapSystemDetails{appOnHost: false},
		want:       sapSystemDetails{appOnHost: true},
	}, {
		name:       "oldDBNotOnHostNewDBOnHost",
		oldDetails: sapSystemDetails{dbOnHost: false},
		newDetails: sapSystemDetails{dbOnHost: true},
		want:       sapSystemDetails{dbOnHost: true},
	}, {
		name:       "oldDBOnHostNewDBNotOnHost",
		oldDetails: sapSystemDetails{dbOnHost: true},
		newDetails: sapSystemDetails{dbOnHost: false},
		want:       sapSystemDetails{dbOnHost: true},
	}, {
		name:       "dontOverwriteAppSID",
		oldDetails: sapSystemDetails{appSID: "abc"},
		newDetails: sapSystemDetails{appSID: "def"},
		want:       sapSystemDetails{appSID: "abc"},
	}, {
		name:       "dontOverwriteDBSID",
		oldDetails: sapSystemDetails{dbSID: "abc"},
		newDetails: sapSystemDetails{dbSID: "def"},
		want:       sapSystemDetails{dbSID: "abc"},
	}, {
		name:       "useNewAppSidWithoutOldAppSid",
		oldDetails: sapSystemDetails{},
		newDetails: sapSystemDetails{appSID: "abc"},
		want:       sapSystemDetails{appSID: "abc"},
	}, {
		name:       "useNewDBSidWithoutOldDBSid",
		oldDetails: sapSystemDetails{},
		newDetails: sapSystemDetails{dbSID: "abc"},
		want:       sapSystemDetails{dbSID: "abc"},
	}, {
		name: "mergeAppHosts",
		oldDetails: sapSystemDetails{
			appHosts: []string{"test-instance1"},
		},
		newDetails: sapSystemDetails{
			appHosts: []string{"test-instance2"},
		},
		want: sapSystemDetails{
			appHosts: []string{"test-instance1", "test-instance2"},
		},
	}, {
		name: "mergeDBHosts",
		oldDetails: sapSystemDetails{
			dbHosts: []string{"test-instance1"},
		},
		newDetails: sapSystemDetails{
			dbHosts: []string{"test-instance2"},
		},
		want: sapSystemDetails{
			dbHosts: []string{"test-instance1", "test-instance2"},
		},
	}, {
		name: "mergeAppProperties",
		oldDetails: sapSystemDetails{
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
			},
		},
		newDetails: sapSystemDetails{
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				NfsUri:          "1.2.3.4",
			},
		},
		want: sapSystemDetails{
			appProperties: &spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
				AscsUri:         "some-test-ascs",
				NfsUri:          "1.2.3.4",
			},
		},
	}, {
		name: "mergeDBProperties",
		oldDetails: sapSystemDetails{
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
			},
		},
		newDetails: sapSystemDetails{
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
				SharedNfsUri: "1.2.3.4",
			},
		},
		want: sapSystemDetails{
			dbProperties: &spb.SapDiscovery_Component_DatabaseProperties{
				DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
				SharedNfsUri: "1.2.3.4",
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mergeSystemDetails(test.oldDetails, test.newDetails)
			if diff := cmp.Diff(test.want, got, cmp.AllowUnexported(sapSystemDetails{}), protocmp.Transform(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mergeSystemDetails() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
