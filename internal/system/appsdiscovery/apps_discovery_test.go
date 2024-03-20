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

package appsdiscovery

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/testing/protocmp"
	fakefs "github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	defaultInstanceName    = "test-instance-id"
	defaultProjectID       = "test-project-id"
	defaultZone            = "test-zone"
	defaultUserStoreOutput = `
KEY default
	ENV: 
	a:b:c
  ENV : test-instance:30013
  USER: SAPABAP1
  DATABASE: DEH
Operation succeed.
`
	scaleoutUserStoreOutput = `
KEY default
ENV: 
a:b:c
ENV : test-instance:30013; test-instance2:30013
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
	defaultHANAVersionOutput = `HDB version info:
  version:             2.00.056.00.1624618329
  branch:              fa/hana2sp05
  machine config:      linuxx86_64
  git hash:            1862616087c05005d2e3b87b8ca7a8149c3241fb
  git merge time:      2021-06-25 12:52:09
  weekstone:           0000.00.0
  cloud edition:       0000.00.00
  compile date:        2021-06-25 13:00:46
  compile host:        ld4554
  compile type:        rel`
	defaultNetweaverKernelOutput = `
	--------------------
	disp+work information
	--------------------
	
	kernel release                785
	
	kernel make variant           785_REL
	
	compiled on                   Linux GNU SLES-12 x86_64 cc8.2.1 use-pr220209 for linuxx86_64
	
	compiled for                  64 BIT
	
	compilation mode              UNICODE
	
	compile time                  Feb  9 2022 21:13:56
	
	Wed Oct 25 14:07:38 2023
	Loading DB library '/usr/sap/DEV/SYS/exe/run/dbhdbslib.so' ...
	Library '/usr/sap/DEV/SYS/exe/run/dbhdbslib.so' loaded
	Version of '/usr/sap/DEV/SYS/exe/run/dbhdbslib.so' is "785.03", patchlevel (0.100)
	
	update level                  0
	
	patch number                  100
	
	kernel patch level            100
	
	source id                     0.100
	
	RKS compatibility level       0
	
	DW_GUI compatibility level    100
	
	
	---------------------
	supported environment
	---------------------
	
	database (SAP, table SVERS)   755
																756
																785
	
	operating system
	Linux
	`
	defaultR3transOutput = `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
unicode enabled version
R3trans finished (0000).
	`
	r3transDataExample = `Test R3trans output
	4 ETW000 REP  CVERS                          *
	4 ETW000 ** 102 ** EA-DFPS                       806       0000      N
	4 ETW000 ** 102 ** EA-HR                         608       0095      N
	4 ETW000 ** 102 ** S4CORE                        106       0000000000R
	4 ETW000 ** 102 ** S4COREOP                      106       0000000000I
	4 ETW000 REP  PRDVERS                        *
	4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAP S/4HANA 2021                                                        +20231215105300
	4 ETW000 ** 394 ** 73554900100900000414SAP NETWEAVER                 7.5                           sap.com                       SAP NETWEAVER 7.5                                                       +20220927121631
	4 ETW000 ** 394 ** 73555000100900003452SAP FIORI FRONT-END SERVER    6.0                           sap.com                       SAP FIORI FRONT-END SERVER 6.0                                          +20220928054714
	`
	defaultPCSOutput = `Cluster Name: ascscluster_ascs
	Corosync Nodes:
	 fs1-nw-node2 fs1-nw-node1
	Pacemaker Nodes:
	 fs1-nw-node2 fs1-nw-node1`
)

var (
	defaultCloudProperties = &instancepb.CloudProperties{
		InstanceName: defaultInstanceName,
		ProjectId:    defaultProjectID,
		Zone:         defaultZone,
	}
	defaultUserStoreResult = commandlineexecutor.Result{
		StdOut: defaultUserStoreOutput,
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
	landscapeZeroNodeResult      = commandlineexecutor.Result{StdOut: ""}
	landscapeMultipleNodesResult = commandlineexecutor.Result{
		StdOut: landscapeOutputMultipleNodes,
	}
	defaultFailoverConfigResult = commandlineexecutor.Result{
		StdOut: `17.11.2023 01:46:41
		HAGetFailoverConfig
		OK
		HAActive: TRUE
		HAProductVersion: SUSE Linux Enterprise Server for SAP Applications 15 SP2
		HASAPInterfaceVersion: SUSE Linux Enterprise Server for SAP Applications 15 SP2 (sap_suse_cluster_connector 3.1.2)
		HADocumentation: https://www.suse.com/products/sles-for-sap/resource-library/sap-best-practices/
		HAActiveNode: fs1-nw-node2
		HANodes: fs1-nw-node2, fs1-nw-node1`,
		ExitCode: 0,
		Error:    nil,
	}
	defaultHANAVersionResult = commandlineexecutor.Result{
		StdOut: defaultHANAVersionOutput,
	}
	defaultNetweaverKernelResult = commandlineexecutor.Result{
		StdOut: defaultNetweaverKernelOutput,
	}
	defaultR3transResult = commandlineexecutor.Result{
		StdOut: defaultR3transOutput,
	}
	defaultDiscoveryConfig = &cpb.DiscoveryConfiguration{
		SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 30},
		EnableDiscovery:                &wpb.BoolValue{Value: true},
		SapInstancesUpdateFrequency:    &dpb.Duration{Seconds: 30},
		EnableWorkloadDiscovery:        &wpb.BoolValue{Value: true},
	}
	defaultPCSResult = commandlineexecutor.Result{
		StdOut: defaultPCSOutput,
	}
	exampleWorkloadProperties = &spb.SapDiscovery_WorkloadProperties{
		ProductVersions: []*pv{
			&pv{Name: "SAP S/4HANA", Version: "2021"},
			&pv{Name: "SAP NETWEAVER", Version: "7.5"},
			&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
		},
		SoftwareComponentVersions: []*scp{
			&scp{
				Name:       "EA-DFPS",
				Version:    "806",
				ExtVersion: "0000",
				Type:       "N",
			},
			&scp{
				Name:       "EA-HR",
				Version:    "608",
				ExtVersion: "0095",
				Type:       "N",
			},
			&scp{
				Name:       "S4CORE",
				Version:    "106",
				ExtVersion: "0000000000",
				Type:       "R",
			},
			&scp{
				Name:       "S4COREOP",
				Version:    "106",
				ExtVersion: "0000000000",
				Type:       "I",
			},
		},
	}
)

func sortSapSystemDetails(a, b SapSystemDetails) bool {
	if a.AppComponent != nil && b.AppComponent != nil &&
		a.AppComponent.GetSid() == b.AppComponent.GetSid() {
		return a.DBComponent.GetSid() < b.DBComponent.GetSid()
	}
	return a.AppComponent.GetSid() < b.AppComponent.GetSid()
}

type fakeCommandExecutor struct {
	t                *testing.T
	params           []commandlineexecutor.Params
	results          []commandlineexecutor.Result
	executeCallCount int
}

type pv = spb.SapDiscovery_WorkloadProperties_ProductVersion
type scp = spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties

type PvByName []*pv

func (a PvByName) Len() int           { return len(a) }
func (a PvByName) Less(i, j int) bool { return a[i].Name < a[j].Name }
func (a PvByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type ScpByName []*scp

func (a ScpByName) Len() int           { return len(a) }
func (a ScpByName) Less(i, j int) bool { return a[i].Name < a[j].Name }
func (a ScpByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func (f *fakeCommandExecutor) Execute(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	log.Logger.Infof("fakeCommandExecutor.Execute: %v", params)
	defer func() { f.executeCallCount++ }()
	opts := []cmp.Option{}
	if f.params[f.executeCallCount].Args == nil {
		opts = append(opts, cmpopts.IgnoreFields(commandlineexecutor.Params{}, "Args"))
	}
	if f.params[f.executeCallCount].ArgsToSplit == "" {
		opts = append(opts, cmpopts.IgnoreFields(commandlineexecutor.Params{}, "ArgsToSplit"))
	}
	if diff := cmp.Diff(f.params[f.executeCallCount], params, opts...); diff != "" {
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
				StdOut: defaultUserStoreOutput,
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
			d := SapDiscovery{
				Execute: test.exec,
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
			d := SapDiscovery{
				Execute: test.exec,
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
			d := SapDiscovery{
				Execute: test.execute,
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
			d := SapDiscovery{
				Execute: test.execute,
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

			d := SapDiscovery{
				Execute: test.execute,
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
			d := SapDiscovery{
				Execute: test.execute,
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

func TestDiscoverNetweaverHA(t *testing.T) {
	tests := []struct {
		name      string
		app       *sappb.SAPInstance
		execute   func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		wantHA    bool
		wantNodes []string
	}{{
		name: "isHA",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				}
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		wantHA:    true,
		wantNodes: []string{"fs1-nw-node2", "fs1-nw-node1"},
	}, {
		name: "isHAWithPCS",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return commandlineexecutor.Result{StdOut: "HAActive: TRUE\nHANodes: "}
				}
			case "pcs":
				return defaultPCSResult
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		wantHA:    true,
		wantNodes: []string{"fs1-nw-node2", "fs1-nw-node1"},
	}, {
		name: "isHAWithPCSClusterLastLine",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return commandlineexecutor.Result{StdOut: "HAActive: TRUE\nHANodes: "}
				}
			case "pcs":
				return commandlineexecutor.Result{
					StdOut: "Pacemaker Nodes:",
				}
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		wantHA: true,
	}, {
		name: "isHAWithPCSError",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return commandlineexecutor.Result{StdOut: "HAActive: TRUE\nHANodes: "}
				}
			case "pcs":
				return commandlineexecutor.Result{StdErr: "pcs error", ExitCode: 1, Error: errors.New("pcs error")}
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		wantHA: true,
	}, {
		name: "noHA",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `17.11.2023 01:46:41
				HAGetFailoverConfig
				OK
				HAActive: FALSE
				HAProductVersion: SUSE Linux Enterprise Server for SAP Applications 15 SP2
				HASAPInterfaceVersion: SUSE Linux Enterprise Server for SAP Applications 15 SP2 (sap_suse_cluster_connector 3.1.2)
				HADocumentation: https://www.suse.com/products/sles-for-sap/resource-library/sap-best-practices/
				HAActiveNode: fs1-nw-node2
				HANodes: fs1-nw-node1`,
				ExitCode: 0,
				Error:    nil,
			}
		},
		wantHA: false,
	}, {
		name: "commandError",
		app: &sappb.SAPInstance{
			InstanceNumber: "00",
			Sapsid:         "abc",
		},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		wantHA: false,
	}}
	log.SetupLoggingForTest()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: test.execute,
			}
			ha, got := d.discoverNetweaverHA(context.Background(), test.app)
			if ha != test.wantHA {
				t.Errorf("discoverNetweaverHA() ha bool mismatch. Got: %t, want: %t", ha, test.wantHA)
			}
			if diff := cmp.Diff(test.wantNodes, got); diff != "" {
				t.Errorf("discoverNetweaverHA() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverNetweaver(t *testing.T) {
	tests := []struct {
		name       string
		app        *sappb.SAPInstance
		execute    commandlineexecutor.Execute
		fileSystem *fakefs.FileSystem
		config     *cpb.DiscoveryConfiguration
		want       SapSystemDetails
	}{{
		name: "justNetweaverConnectedToDB",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return defaultUserStoreResult
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte(r3transDataExample)},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: exampleWorkloadProperties,
		},
	}, {
		name: "justNetweaverConnectedToScaleoutDB",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return commandlineexecutor.Result{StdOut: scaleoutUserStoreOutput}
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte(r3transDataExample)},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
			DBHosts:            []string{"test-instance", "test-instance2"},
			WorkloadProperties: exampleWorkloadProperties,
		},
	}, {
		name: "notHA",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return defaultUserStoreResult
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return commandlineexecutor.Result{Error: errors.New("some error")}
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
	}, {
		name: "ascsErr",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return commandlineexecutor.Result{Error: errors.New("some error")}
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
			case "grep":
				return commandlineexecutor.Result{Error: errors.New("some error")}
			case "df":
				return netweaverMountResult
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
	}, {
		name: "nfsErr",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return commandlineexecutor.Result{Error: errors.New("some error")}
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
			case "grep":
				return defaultProfileResult
			case "df":
				return commandlineexecutor.Result{Error: errors.New("some error")}
			}
			return commandlineexecutor.Result{
				StdErr:   "Unexpected command",
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
	}, {
		name: "noDBSID",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return commandlineexecutor.Result{Error: errors.New("some error")}
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
	}, {
		name: "noDBHosts",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					if slices.Contains(params.Args, "DEFAULT") {
						return commandlineexecutor.Result{Error: errors.New("some error")}
					}
					return defaultUserStoreResult
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            true,
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
	}, {
		name: "workloadDiscoveryDisabled",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				if slices.Contains(params.Args, "hdbuserstore") {
					return defaultUserStoreResult
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return commandlineexecutor.Result{Error: errors.New("some error")}
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultR3transResult
				}
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
		},
		config: &cpb.DiscoveryConfiguration{
			EnableWorkloadDiscovery: wpb.Bool(false),
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						Abap:            false,
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
		},
	}}
	log.SetupLoggingForTest()
	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute:    tc.execute,
				FileSystem: tc.fileSystem,
			}
			if tc.config == nil {
				tc.config = defaultDiscoveryConfig
			}
			got := d.discoverNetweaver(ctx, tc.app, tc.config)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(SapSystemDetails{}), protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaver(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
		})
	}
}

func TestDiscoverHANA(t *testing.T) {
	tests := []struct {
		name     string
		app      *sappb.SAPInstance
		execute  commandlineexecutor.Execute
		topology string
		want     []SapSystemDetails
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
			default:
				if strings.Contains(params.Executable, "HDB") {
					return defaultHANAVersionResult
				}
			}
			return commandlineexecutor.Result{
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts: []string{"test-instance"},
		}},
	}, {
		name: "scaleout",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return landscapeMultipleNodesResult
			case "df":
				return hanaMountResult
			default:
				if strings.Contains(params.Executable, "HDB") {
					return defaultHANAVersionResult
				}
			}
			return commandlineexecutor.Result{
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
			DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
		}},
	}, {
		name: "multiTenant",
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
			default:
				if strings.Contains(params.Executable, "HDB") {
					return defaultHANAVersionResult
				}
			}
			return commandlineexecutor.Result{
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "SYSTEMDB"
					},
					"3": {
						"name": "ABC"
					},
					"4": {
						"name": "DEF"
					}
				}
			}
		}`,
		want: []SapSystemDetails{
			SapSystemDetails{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
							SharedNfsUri:    "1.2.3.4",
							DatabaseVersion: "HANA 2.0 Rev 56",
						}},
					TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
				},
				DBHosts: []string{"test-instance"},
			},
			SapSystemDetails{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
							SharedNfsUri:    "1.2.3.4",
							DatabaseVersion: "HANA 2.0 Rev 56",
						}},
					TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
				},
				DBHosts: []string{"test-instance"},
			},
		},
	}, {
		name: "multiTenantScaleout",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return landscapeMultipleNodesResult
			case "df":
				return hanaMountResult
			default:
				if strings.Contains(params.Executable, "HDB") {
					return defaultHANAVersionResult
				}
			}
			return commandlineexecutor.Result{
				Error:    errors.New("Unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "SYSTEMDB"
					},
					"3": {
						"name": "ABC"
					},
					"4": {
						"name": "DEF"
					}
				}
			}
		}`,
		want: []SapSystemDetails{
			SapSystemDetails{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
							SharedNfsUri:    "1.2.3.4",
							DatabaseVersion: "HANA 2.0 Rev 56",
						}},
					TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
				},
				DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
			},
			SapSystemDetails{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
							SharedNfsUri:    "1.2.3.4",
							DatabaseVersion: "HANA 2.0 Rev 56",
						}},
					TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
				},
				DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
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
		want: nil,
	}, {
		name: "EmptyInstance",
		app:  &sappb.SAPInstance{},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: landscapeOutputSingleNode,
			}
		},
		want: nil,
	}, {
		name: "missingDBNodes",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return landscapeZeroNodeResult
			case "df":
				return hanaMountResult
			case "HDB":
				return defaultHANAVersionResult
			}
			return commandlineexecutor.Result{
				Error:    errors.New("unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: nil,
	}, {
		name: "errorFromDBNodes",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(_ context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				return commandlineexecutor.Result{
					Error:    errors.New("expected error for test"),
					ExitCode: 1,
				}
			case "df":
				return hanaMountResult
			case "HDB":
				return defaultHANAVersionResult
			}
			return commandlineexecutor.Result{
				Error:    errors.New("unexpected command"),
				ExitCode: 1,
			}
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: nil,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: tc.execute,
				FileSystem: &fakefs.FileSystem{
					ReadFileResp: [][]byte{[]byte(tc.topology)},
					ReadFileErr:  []error{nil},
				},
			}
			got := d.discoverHANA(ctx, tc.app)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(SapSystemDetails{}), protocmp.Transform(), cmpopts.SortSlices(func(a, b SapSystemDetails) bool { return a.DBComponent.Sid < b.DBComponent.Sid })); diff != "" {
				t.Errorf("discoverHANA(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
		})
	}
}

func TestDiscoverHANATenantDBs(t *testing.T) {
	tests := []struct {
		name     string
		app      *sappb.SAPInstance
		execute  commandlineexecutor.Execute
		topology string
		want     []string
	}{{
		name: "singleTenant",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: []string{"ABC"},
	}, {
		name: "IgnoreSYSTEMDB",
		app:  &sappb.SAPInstance{},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "SYSTEMDB"
					},
					"2": {
						"name": "ABC"
					}
				}
			}
		}`,
		want: []string{"ABC"},
	}, {
		name: "MultipleTenants",
		app:  &sappb.SAPInstance{},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": "SYSTEMDB"
					},
					"2": {
						"name": "ABC"
					},
					"3": {
						"name": "DEF"
					}
				}
			}
		}`,
		want: []string{"ABC", "DEF"},
	}, {
		name: "ZeroTenants",
		app:  &sappb.SAPInstance{},
		topology: `{
			"topology": {
				"databases": {}
			}
		}`,
		want: nil,
	}, {
		name:     "InvalidJson",
		app:      &sappb.SAPInstance{},
		topology: `{{not valid json}`,
		want:     nil,
	}, {
		name:     "ValidJsonMissingExpectedFields",
		app:      &sappb.SAPInstance{},
		topology: `{"unexpected": "content"}`,
		want:     nil,
	}, {
		name:     "ValidJsonMissingUnexpectedTopologyTypes",
		app:      &sappb.SAPInstance{},
		topology: `{"topology": 123}`,
		want:     nil,
	}, {
		name: "ValidJsonMissingUnexpectedDatabaseTypes",
		app:  &sappb.SAPInstance{},
		topology: `{
			"topology": {
				"databases": true
				}
			}
		}`,
		want: nil,
	}, {
		name: "ValidJsonMissingUnexpectedNameTypes",
		app:  &sappb.SAPInstance{},
		topology: `{
			"topology": {
				"databases": {
					"1": {
						"name": 123
				}
			}
		}`,
		want: nil,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: tc.execute,
				FileSystem: &fakefs.FileSystem{
					ReadFileResp: [][]byte{[]byte(tc.topology)},
					ReadFileErr:  []error{nil},
				},
			}
			got, _ := d.discoverHANATenantDBs(ctx, tc.app, "")
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(SapSystemDetails{}), protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("discoverHANATenantDBs(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
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
		fileSystem   *fakefs.FileSystem
		config       *cpb.DiscoveryConfiguration
		want         []SapSystemDetails
	}{{
		name:         "noSAPApps",
		sapInstances: &sappb.SAPInstances{},
		executor:     &fakeCommandExecutor{},
		want:         []SapSystemDetails{},
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
			}, {
				Executable: "/usr/sap/ABC/HDB00/HDB",
				User:       "abcadm",
			}},
			results: []commandlineexecutor.Result{landscapeSingleNodeResult, hanaMountResult, defaultHANAVersionResult},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte("")},
			ReadFileErr:  []error{nil},
		},
		want: []SapSystemDetails{
			{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "abc",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
							SharedNfsUri:    "1.2.3.4",
							DatabaseVersion: "HANA 2.0 Rev 56",
						}},
					TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
				},
				DBOnHost: true,
				DBHosts:  []string{"test-instance"},
			},
		},
	}, {
		name: "justNetweaver",
		cp:   defaultCloudProperties,
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "grep", // Get profile
			}, {
				Executable: "df", // Get NFS
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo", // Failover config
			}, {
				Executable: "sudo", // hdbuserstore
			}, {
				Executable: "sudo", // hdbuserstore
			}, {
				Executable: "sudo", // ABAP
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans err")},
				defaultUserStoreResult,
				defaultUserStoreResult,
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			ReadFileResp:          [][]byte{[]byte("")},
			ReadFileErr:           []error{nil},
			RemoveAllErr:          []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
		}},
	}, {
		name: "twoNetweaver",
		cp:   defaultCloudProperties,
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "grep", // Get profile
			}, {
				Executable: "df", // Get NFS
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo", // Failover config
			}, {
				Executable: "sudo", // ABAP
			}, {
				Executable: "sudo", // hdbuserstore
			}, {
				Executable: "sudo", // hdbuserstore
			}, {
				Executable: "grep", // Get profile
			}, {
				Executable: "df", // Get NFS
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo", // Failover config
			}, {
				Executable: "sudo", // ABAP
			}, {
				Executable: "sudo", // hdbuserstore
			}, {
				Executable: "sudo", // hdbuserstore
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans err")},
				defaultUserStoreResult,
				defaultUserStoreResult,
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans err")},
				defaultUserStoreResult,
				defaultUserStoreResult,
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
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil, nil},
			ChmodErr:              []error{nil, nil},
			WriteStringToFileResp: []int{0, 0},
			WriteStringToFileErr:  []error{nil, nil},
			RemoveAllErr:          []error{nil, nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
		}, {
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "def",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:         "some-test-ascs",
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			AppOnHost: true,
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
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
				Executable: "/usr/sap/ABC/HDB00/HDB",
				User:       "abcadm",
			}, {
				Executable: "sudo",
			}, {
				Executable: "df",
			}, {
				Executable: "/usr/sap/DEF/HDB00/HDB",
				User:       "defadm",
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult, hanaMountResult, defaultHANAVersionResult,
				landscapeSingleNodeResult, hanaMountResult, defaultHANAVersionResult},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}, []byte{}},
			ReadFileErr:  []error{nil, nil},
		},
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "def",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
		}},
	}, {
		name: "netweaverThenHANAConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "abc",
					Type:           sappb.InstanceType_NETWEAVER,
					InstanceNumber: "11",
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
				Executable: "grep",
				Args:       []string{"rdisp/mshost", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "HAGetFailoverConfig"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list", "DEFAULT"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "dehadm", "python", "/usr/sap/DEH/HDB00/exe/python_support/landscapeHostConfiguration.py"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "/usr/sap/DEH/HDB00/HDB",
				Args:       []string{"version"},
				User:       "dehadm",
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				defaultUserStoreResult,
				defaultUserStoreResult,
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
			RemoveAllErr:          []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost:           true,
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
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
					Sapsid:         "abc",
					Type:           sappb.InstanceType_NETWEAVER,
					InstanceNumber: "11",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "dehadm", "python", "/usr/sap/DEH/HDB00/exe/python_support/landscapeHostConfiguration.py"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "/usr/sap/DEH/HDB00/HDB",
				Args:       []string{"version"},
				User:       "dehadm",
			}, {
				Executable: "grep",
				Args:       []string{"rdisp/mshost", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "HAGetFailoverConfig"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list", "DEFAULT"},
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				defaultUserStoreResult,
				defaultUserStoreResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
		}},
	}, {
		name: "netweaverThenHANANotConnected",
		cp:   defaultCloudProperties,
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "abc",
					Type:           sappb.InstanceType_NETWEAVER,
					InstanceNumber: "11",
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
				Executable: "grep",
				Args:       []string{"rdisp/mshost", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "HAGetFailoverConfig"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list", "DEFAULT"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "db2adm", "python", "/usr/sap/DB2/HDB00/exe/python_support/landscapeHostConfiguration.py"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "/usr/sap/DB2/HDB00/HDB",
				Args:       []string{"version"},
				User:       "db2adm",
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				defaultUserStoreResult,
				defaultUserStoreResult,
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost:           false,
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DB2",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
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
					Sapsid:         "abc",
					Type:           sappb.InstanceType_NETWEAVER,
					InstanceNumber: "11",
				},
			},
		},
		executor: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "db2adm", "python", "/usr/sap/DB2/HDB00/exe/python_support/landscapeHostConfiguration.py"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "/usr/sap/DB2/HDB00/HDB",
				Args:       []string{"version"},
				User:       "db2adm",
			}, {
				Executable: "grep",
				Args:       []string{"rdisp/mshost", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "HAGetFailoverConfig"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "hdbuserstore", "list", "DEFAULT"},
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				defaultUserStoreResult,
				defaultUserStoreResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
			RemoveAllErr: []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
						KernelVersion:   "SAP Kernel 785 Patch 100",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid:          "DEH",
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost:           false,
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DB2",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.0 Rev 56",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
		}},
	}}

	log.SetupLoggingForTest()
	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.executor.t = t
			d := SapDiscovery{
				Execute: func(c context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
					return tc.executor.Execute(c, p)
				},
				FileSystem: tc.fileSystem,
			}
			if tc.config == nil {
				tc.config = defaultDiscoveryConfig
			}
			got := d.DiscoverSAPApps(ctx, tc.sapInstances, tc.config)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(SapSystemDetails{}), cmpopts.SortSlices(sortSapSystemDetails), protocmp.Transform(), cmpopts.EquateEmpty()); diff != "" {
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
		want:  nil,
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
		oldDetails SapSystemDetails
		newDetails SapSystemDetails
		want       SapSystemDetails
	}{{
		name:       "empty",
		oldDetails: SapSystemDetails{},
		newDetails: SapSystemDetails{},
		want:       SapSystemDetails{},
	}, {
		name: "onlyOld",
		oldDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri: "1.2.3.4",
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
		newDetails: SapSystemDetails{},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri: "1.2.3.4",
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
	}, {
		name:       "onlyNew",
		oldDetails: SapSystemDetails{},
		newDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
	}, {
		name: "identicalOldAndNew",
		oldDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
		newDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
			AppHosts: []string{"test-instance"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
			DBOnHost: false,
			DBHosts:  []string{"test-instance"},
		},
	}, {
		name:       "oldAppNotOnHostNewAppOnHost",
		oldDetails: SapSystemDetails{AppOnHost: false},
		newDetails: SapSystemDetails{AppOnHost: true},
		want:       SapSystemDetails{AppOnHost: true},
	}, {
		name:       "oldAppOnHostNewAppNotOnHost",
		oldDetails: SapSystemDetails{AppOnHost: true},
		newDetails: SapSystemDetails{AppOnHost: false},
		want:       SapSystemDetails{AppOnHost: true},
	}, {
		name:       "oldDBNotOnHostNewDBOnHost",
		oldDetails: SapSystemDetails{DBOnHost: false},
		newDetails: SapSystemDetails{DBOnHost: true},
		want:       SapSystemDetails{DBOnHost: true},
	}, {
		name:       "oldDBOnHostNewDBNotOnHost",
		oldDetails: SapSystemDetails{DBOnHost: true},
		newDetails: SapSystemDetails{DBOnHost: false},
		want:       SapSystemDetails{DBOnHost: true},
	}, {
		name: "dontOverwriteAppSID",
		oldDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
		newDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "def",
			},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
	}, {
		name: "dontOverwriteDBSID",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "def",
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
	}, {
		name:       "useNewAppSidWithoutOldAppSid",
		oldDetails: SapSystemDetails{},
		newDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
	}, {
		name:       "useNewDBSidWithoutOldDBSid",
		oldDetails: SapSystemDetails{},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
			},
		},
	}, {
		name: "mergeAppHosts",
		oldDetails: SapSystemDetails{
			AppHosts: []string{"test-instance1"},
		},
		newDetails: SapSystemDetails{
			AppHosts: []string{"test-instance2"},
		},
		want: SapSystemDetails{
			AppHosts: []string{"test-instance1", "test-instance2"},
		},
	}, {
		name: "mergeDBHosts",
		oldDetails: SapSystemDetails{
			DBHosts: []string{"test-instance1"},
		},
		newDetails: SapSystemDetails{
			DBHosts: []string{"test-instance2"},
		},
		want: SapSystemDetails{
			DBHosts: []string{"test-instance1", "test-instance2"},
		},
	}, {
		name: "mergeAppProperties",
		oldDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
					}},
			},
		},
		newDetails: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						NfsUri:          "1.2.3.4",
					}},
			},
		},
		want: SapSystemDetails{
			AppComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
			},
		},
	}, {
		name: "mergeWlProperties",
		oldDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{
					&pv{Name: "SAP S/4HANA", Version: "2021"},
					&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
				},
				SoftwareComponentVersions: []*scp{
					&scp{
						Name:       "EA-DFPS",
						Version:    "806",
						ExtVersion: "0000",
						Type:       "N",
					},
					&scp{
						Name:       "EA-HR",
						Version:    "608",
						ExtVersion: "0095",
						Type:       "N",
					},
					&scp{
						Name:       "S4COREOP",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "I",
					},
				},
			},
		},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{
					&pv{Name: "SAP NETWEAVER", Version: "7.5"},
					&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
				},
				SoftwareComponentVersions: []*scp{
					&scp{
						Name:       "EA-DFPS",
						Version:    "806",
						ExtVersion: "0000",
						Type:       "N",
					},
					&scp{
						Name:       "EA-HR",
						Version:    "608",
						ExtVersion: "0095",
						Type:       "N",
					},
					&scp{
						Name:       "S4CORE",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "R",
					},
				},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{
					&pv{Name: "SAP NETWEAVER", Version: "7.5"},
					&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
				},
				SoftwareComponentVersions: []*scp{
					&scp{
						Name:       "EA-DFPS",
						Version:    "806",
						ExtVersion: "0000",
						Type:       "N",
					},
					&scp{
						Name:       "EA-HR",
						Version:    "608",
						ExtVersion: "0095",
						Type:       "N",
					},
					&scp{
						Name:       "S4CORE",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "R",
					},
				},
			},
		},
	}, {
		name: "mergeNewWlProperties",
		oldDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
		},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{
					&pv{Name: "SAP S/4HANA", Version: "2021"},
					&pv{Name: "SAP NETWEAVER", Version: "7.5"},
					&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
				},
				SoftwareComponentVersions: []*scp{
					&scp{
						Name:       "EA-DFPS",
						Version:    "806",
						ExtVersion: "0000",
						Type:       "N",
					},
					&scp{
						Name:       "EA-HR",
						Version:    "608",
						ExtVersion: "0095",
						Type:       "N",
					},
					&scp{
						Name:       "S4CORE",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "R",
					},
					&scp{
						Name:       "S4COREOP",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "I",
					},
				},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{
					&pv{Name: "SAP S/4HANA", Version: "2021"},
					&pv{Name: "SAP NETWEAVER", Version: "7.5"},
					&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
				},
				SoftwareComponentVersions: []*scp{
					&scp{
						Name:       "EA-DFPS",
						Version:    "806",
						ExtVersion: "0000",
						Type:       "N",
					},
					&scp{
						Name:       "EA-HR",
						Version:    "608",
						ExtVersion: "0095",
						Type:       "N",
					},
					&scp{
						Name:       "S4CORE",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "R",
					},
					&scp{
						Name:       "S4COREOP",
						Version:    "106",
						ExtVersion: "0000000000",
						Type:       "I",
					},
				},
			},
		},
	}, {
		name: "mergeDBProperties",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
						SharedNfsUri: "1.2.3.4",
					}},
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri: "1.2.3.4",
					}},
			},
		},
	}, {
		name: "topologyTypeFavorsScaleOut",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
						SharedNfsUri: "1.2.3.4",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri: "1.2.3.4",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
		},
	}, {
		name: "topologyTypeFavorsScaleOutFromNew",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
						SharedNfsUri: "1.2.3.4",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri: "1.2.3.4",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mergeSystemDetails(test.oldDetails, test.newDetails)
			if diff := cmp.Diff(test.want, got, cmp.AllowUnexported(SapSystemDetails{}), protocmp.Transform(), cmpopts.EquateEmpty(), protocmp.SortRepeatedFields(&spb.SapDiscovery_WorkloadProperties{}, "product_versions", "software_component_versions")); diff != "" {
				t.Errorf("mergeSystemDetails() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverHANAVersion(t *testing.T) {
	tests := []struct {
		name    string
		exec    func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want    string
		wantErr error
	}{{
		name: "success",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return defaultHANAVersionResult
		},
		want: "HANA 2.0 Rev 56",
	}, {
		name: "commandError",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				Error: errors.New("test error"),
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "unexpectedOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some output",
			}
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &SapDiscovery{
				Execute: tc.exec,
			}
			app := &sappb.SAPInstance{
				Sapsid:         defaultSID,
				InstanceNumber: defaultInstanceNumber,
			}
			got, err := d.discoverHANAVersion(context.Background(), app)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverHANAVersion() error: got %v, want %v", err, tc.wantErr)
			}

			if got != tc.want {
				t.Errorf("discoverHANAVersion() = %v, want: %v", got, tc.want)
			}
		})
	}
}

func TestDiscoverNetweaverKernelVersion(t *testing.T) {
	tests := []struct {
		name    string
		exec    func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want    string
		wantErr error
	}{{
		name: "success",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return defaultNetweaverKernelResult
		},
		want:    "SAP Kernel 785 Patch 100",
		wantErr: nil,
	}, {
		name: "commandError",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				Error: errors.New("test error"),
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noKernel",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some output",
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noPatch",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some output",
			}
		},
		wantErr: cmpopts.AnyError,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &SapDiscovery{
				Execute: tc.exec,
			}
			got, err := d.discoverNetweaverKernelVersion(ctx, defaultSID)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverNetweaverKernelVersion() returned an unexpected error: %v", err)
			}

			if got != tc.want {
				t.Errorf("discoverNetweaverKernelVersion() = %v, want: %v", got, tc.want)
			}
		})
	}
}

func TestDiscoverNetweaverABAP(t *testing.T) {
	tests := []struct {
		name           string
		app            *sappb.SAPInstance
		executor       fakeCommandExecutor
		testFilesystem *fakefs.FileSystem
		wantBool       bool
		wantData       *spb.SapDiscovery_WorkloadProperties
		wantErr        error
	}{{
		name: "success",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-w", "/tmp/r3trans/output.txt", "/tmp/r3trans/export_products.ctl"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-w", "/tmp/r3trans/output.txt", "-v", "-l", "/tmp/r3trans/export_products.dat"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}, {
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}, {
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte(r3transDataExample)},
			ReadFileErr:           []error{nil},
		},
		wantBool: true,
		wantData: exampleWorkloadProperties,
	}, {
		name: "mkDirErr",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr: []error{errors.New("test error")},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "chmodErr",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:     []error{nil},
			ChmodErr:     []error{errors.New("test error")},
			RemoveAllErr: []error{nil},
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "firstR3transErr",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}},
			results: []commandlineexecutor.Result{{
				Error: errors.New("test error"),
			}},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "firstR3transFailResponse",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans failed (9999).`,
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "createFileErr",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			CreateResp:   []*os.File{nil},
			CreateErr:    []error{errors.New("test error")},
			RemoveAllErr: []error{nil},
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "writeStringToFileErr",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{errors.New("test error")},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "secondR3transError",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-w", "/tmp/r3trans/output.txt", "/tmp/r3trans/export_products.ctl"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}, {
				Error: errors.New("test error"),
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "thirdR3transError",
		app: &sappb.SAPInstance{
			Sapsid:         defaultSID,
			InstanceNumber: defaultInstanceNumber,
		},
		executor: fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-w", "/tmp/r3trans/output.txt", "/tmp/r3trans/export_products.ctl"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-w", "/tmp/r3trans/output.txt", "-v", "-l", "/tmp/r3trans/export_products.dat"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}, {
				StdOut: `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
				unicode enabled version
				R3trans finished (0000).`,
			}, {
				Error: errors.New("test error"),
			}},
		},
		testFilesystem: &fakefs.FileSystem{
			MkDirErr:              []error{nil},
			ChmodErr:              []error{nil},
			CreateResp:            []*os.File{{}},
			CreateErr:             []error{nil},
			WriteStringToFileResp: []int{0},
			WriteStringToFileErr:  []error{nil},
			RemoveAllErr:          []error{nil},
			ReadFileResp:          [][]byte{[]byte{}},
			ReadFileErr:           []error{nil},
		},
		wantErr: cmpopts.AnyError,
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.executor.t = t
			d := &SapDiscovery{
				Execute: func(c context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
					return tc.executor.Execute(c, p)
				},
				FileSystem: tc.testFilesystem,
			}
			gotBool, gotData, err := d.discoverNetweaverABAP(ctx, tc.app)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("discoverNetweaverABAP(%v) returned an unexpected error: %v", tc.name, err)
			}

			if gotBool != tc.wantBool {
				t.Errorf("discoverNetweaverABAP(%v) = %v, want: %v", tc.app, gotBool, tc.wantBool)
			} else if diff := cmp.Diff(gotData, tc.wantData, protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaverABAP(%v) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}

func TestParseR3transOutput(t *testing.T) {
	tests := []struct {
		name         string
		fileContents string
		want         *spb.SapDiscovery_WorkloadProperties
		wantErr      error
	}{{
		name: "ignoreNoise",
		fileContents: `Test R3trans output
		noise 123456789
		4 ETW000 REP  CVERS                          *
		4 ETW000 ** 102 ** EA-DFPS                       806       0000      N
		noise line to ignore
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAP S/4HANA 2021                                                        +20231215105300
		more noise to ignore
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{&pv{Name: "SAP S/4HANA", Version: "2021"}},
			SoftwareComponentVersions: []*scp{&scp{
				Name:       "EA-DFPS",
				Version:    "806",
				ExtVersion: "0000",
				Type:       "N",
			}},
		},
	}, {
		name: "touchingFields",
		fileContents: `Test R3trans output
		4 ETW000 REP  CVERS                          *
		4 ETW000 ** 102 ** EA-DFPS                       806       0000000000N
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAP S/4HANA 2021                                                        +20231215105300
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{&pv{Name: "SAP S/4HANA", Version: "2021"}},
			SoftwareComponentVersions: []*scp{&scp{
				Name:       "EA-DFPS",
				Version:    "806",
				ExtVersion: "0000000000",
				Type:       "N",
			}},
		},
	}, {
		name: "cversLineButNoEntry",
		fileContents: `Test R3trans output
		4 ETW000 REP  CVERS                          *
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAP S/4HANA 2021                                                        +20231215105300
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{&pv{Name: "SAP S/4HANA", Version: "2021"}},
		},
	}, {
		name: "prdversLineButNoEntry",
		fileContents: `Test R3trans output
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 REP  CVERS                          *
		4 ETW000 ** 102 ** EA-DFPS                       806       0000000000N
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			SoftwareComponentVersions: []*scp{&scp{
				Name:       "EA-DFPS",
				Version:    "806",
				ExtVersion: "0000000000",
				Type:       "N",
			}},
		},
	}, {
		name: "multipleEntries",
		fileContents: `Test R3trans output
		4 ETW000 REP  CVERS                          *
		4 ETW000 ** 102 ** EA-DFPS                       806       0000      N
		4 ETW000 ** 102 ** EA-HR                         608       0095      N
		4 ETW000 ** 102 ** S4CORE                        106       0000000000R
		4 ETW000 ** 102 ** S4COREOP                      106       0000000000I
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAP S/4HANA 2021                                                        +20231215105300
		4 ETW000 ** 394 ** 73554900100900000414SAP NETWEAVER                 7.5                           sap.com                       SAP NETWEAVER 7.5                                                       +20220927121631
		4 ETW000 ** 394 ** 73555000100900003452SAP FIORI FRONT-END SERVER    6.0                           sap.com                       SAP FIORI FRONT-END SERVER 6.0                                          +20220928054714
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
	}, {
		name: "partialComponents",
		fileContents: `Test R3trans output
		4 ETW000 REP  CVERS                          *
		**
		4 ETW000 ** 102 **
		4 ETW000 ** 102 ** EA-DFPS
		4 ETW000 ** 102 ** EA-HR                         608
		4 ETW000 ** 102 ** S4COREOP                      106       0000000000I
		4 ETW000 ** 102 ** EA-HR                         608       0095      N
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			SoftwareComponentVersions: []*scp{
				&scp{},
				&scp{},
				&scp{
					Name: "EA-DFPS",
				},
				&scp{
					Name:    "EA-HR",
					Version: "608",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
			},
		},
	}, {
		name: "noProductVersion",
		fileContents: `Test R3trans output
		4 ETW000 REP  PRDVERS                        *
		4 ETW000 ** 394 ** 73554900100900005134S4HANA ON PREMISE             2021                          sap.com                       SAPS/4HANA                                                        +20231215105300
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAPS/4HANA"},
			},
		},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseR3transOutput(ctx, tc.fileContents)

			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("parseR3transOutput(%v) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}

func TestMergeWorkloadProperties(t *testing.T) {
	tests := []struct {
		name    string
		new     *spb.SapDiscovery_WorkloadProperties
		old     *spb.SapDiscovery_WorkloadProperties
		want    *spb.SapDiscovery_WorkloadProperties
		wantErr error
	}{{
		name: "multipleEntriesWithOverlap",
		old: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
		new: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
			},
		},
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
			},
		},
	}, {
		name: "nilOld",
		old:  nil,
		new: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
	}, {
		name: "nilNew",
		old: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
		new: nil,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{
				&pv{Name: "SAP S/4HANA", Version: "2021"},
				&pv{Name: "SAP NETWEAVER", Version: "7.5"},
				&pv{Name: "SAP FIORI FRONT-END SERVER", Version: "6.0"},
			},
			SoftwareComponentVersions: []*scp{
				&scp{
					Name:       "EA-DFPS",
					Version:    "806",
					ExtVersion: "0000",
					Type:       "N",
				},
				&scp{
					Name:       "EA-HR",
					Version:    "608",
					ExtVersion: "0095",
					Type:       "N",
				},
				&scp{
					Name:       "S4CORE",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "R",
				},
				&scp{
					Name:       "S4COREOP",
					Version:    "106",
					ExtVersion: "0000000000",
					Type:       "I",
				},
			},
		},
	}, {
		name: "allNil",
		old:  nil,
		new:  nil,
		want: nil,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeWorkloadProperties(tc.old, tc.new)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform(), protocmp.SortRepeatedFields(&spb.SapDiscovery_WorkloadProperties{}, "product_versions", "software_component_versions")); diff != "" {
				t.Errorf("parseR3transOutput(%v) returned an unexpected diff (-want +got): %v", tc.name, diff)
			}
		})
	}
}
