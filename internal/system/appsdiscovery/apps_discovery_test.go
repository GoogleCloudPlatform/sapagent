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
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
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
	defaultSIDAdm             = "abcadm"
	defaultInstanceNumber     = "00"
	landscapeOutputSingleNode = `24.07.2024 13:57:24
	GetSystemInstanceList
	OK
	hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
	test-instance, 0, 50013, 50014, 0.3, HDB|HDB_WORKER, GREEN 
`
	landscapeOutputMultipleNodes = `24.07.2024 13:57:24
	GetSystemInstanceList
	OK
	hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
test-instance , 0, 50013, 50014, 0.3, HDB|HDB_WORKER, GREEN 
test-instancew1, 0, 50013, 50014, 0.3, HDB|HDB_WORKER, GREEN 
test-instancew2, 0, 50013, 50014, 0.3, HDB|HDB_WORKER, GREEN 
test-instancew3, 0, 50013, 50014, 0.3, HDB|HDB_WORKER, GREEN 

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
  version:             2.12.056.34.1624618329
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
	defaultBatchConfigOutput = `
	Listing the SCA versions:
	SERVERCORE : 1000.7.50.25.5.20221121195400
	Listing the versions of SDAs/EARs per SCA:
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
	defaultR3transOutput = `This is R3trans version 6.26 (release 753 - 17.01.22 - 17:53:42 ).
unicode enabled version
R3trans finished (0000).
	`
	defaultPCSOutput = `Cluster Name: ascscluster_ascs
	Corosync Nodes:
	 fs1-nw-node2 fs1-nw-node1
	Pacemaker Nodes:
	 fs1-nw-node2 fs1-nw-node1`
	defaultNetweaverInstanceListOutput = `04.03.2024 11:35:40
	GetSystemInstanceList
	OK
	hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
	ascs, 01, 50113, 50114, 1, MESSAGESERVER, GREEN
	ers, 10, 51013, 51014, 0.5, ENQREP, GREEN
	app1, 11, 51113, 51114, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
	app2, 12, 51113, 51114, 3, ABAP|GATEWAY|ICMAN|IGS, GRAY
	`
	oneAppInstanceListOutput = `04.03.2024 11:35:40
	GetSystemInstanceList
	OK
	hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
	app1, 11, 51113, 51114, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
	`
	twoAppInstanceListOutput = `04.03.2024 11:35:40
	GetSystemInstanceList
	OK
	hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
	app1, 11, 51113, 51114, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
	app2, 12, 51113, 51114, 3, ABAP|GATEWAY|ICMAN|IGS, GRAY
	`
	defaultProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = hdb
dbms/name = DEH
dbid = DEH
SAPDBHOST = test-instance
	`
	db2ProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = db2
dbms/name = DEH
dbid = DEH
SAPDBHOST = db2Hostname
	`
	oracleProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = ora
dbms/name = DEH
dbid = DEH
SAPDBHOST = oracleHostname
	`
	sqlServerProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = mss
dbms/name = DEH
dbid = DEH
SAPDBHOST = mssHostname
	`
	aseProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = syb
dbms/name = DEH
dbid = DEH
SAPDBHOST = sybHostname
	`
	maxDBProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = ada
dbms/name = DEH
dbid = DEH
SAPDBHOST = adaHostname
	`
	unspecifiedTypeProfileOutput = `
rdisp/mshost = some-test-ascs
dbs/hdb/dbname = DEH
j2ee/dbname = DEH
dbms/type = somethingElse
dbms/name = DEH
dbid = DEH
SAPDBHOST = otherHostname
	`
	defaultLandscapeID = "5b91d2e8-e104-e541-9749-6ae7b2ebcfe9"
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
		StdOut: defaultProfileOutput,
	}
	db2ProfileResult = commandlineexecutor.Result{
		StdOut: db2ProfileOutput,
	}
	oracleProfileResult = commandlineexecutor.Result{
		StdOut: oracleProfileOutput,
	}
	sqlServerProfileResult = commandlineexecutor.Result{
		StdOut: sqlServerProfileOutput,
	}
	aseProfileResult = commandlineexecutor.Result{
		StdOut: aseProfileOutput,
	}
	maxDBProfileResult = commandlineexecutor.Result{
		StdOut: maxDBProfileOutput,
	}
	unspecifiedTypeProfileResult = commandlineexecutor.Result{
		StdOut: unspecifiedTypeProfileOutput,
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
	defaultBatchConfigResult = commandlineexecutor.Result{
		StdOut: defaultBatchConfigOutput,
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
	exampleJavaWorkloadProperties = &spb.SapDiscovery_WorkloadProperties{
		ProductVersions: []*pv{
			&pv{Name: "SAP Netweaver", Version: "7.50"},
		},
		SoftwareComponentVersions: []*scp{
			&scp{
				Name:       "SERVERCORE",
				Version:    "7.50",
				ExtVersion: "25",
				Type:       "5",
			},
		},
	}
	defaultNetweaverInstanceListResult = commandlineexecutor.Result{
		StdOut: defaultNetweaverInstanceListOutput,
	}
	oneAppInstanceListResult = commandlineexecutor.Result{
		StdOut: oneAppInstanceListOutput,
	}
	twoAppInstanceListResult = commandlineexecutor.Result{
		StdOut: twoAppInstanceListOutput,
	}
	defaultErrorResult = commandlineexecutor.Result{
		StdOut: "",
		StdErr: "",
		Error:  errors.New("default error"),
	}
	defaultLandscapeIDResult = commandlineexecutor.Result{
		StdOut: "id = 5b91d2e8-e104-e541-9749-6ae7b2ebcfe9",
	}
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func sortSapSystemDetails(a, b SapSystemDetails) bool {
	if a.AppComponent != nil && b.AppComponent != nil &&
		a.AppComponent.GetSid() == b.AppComponent.GetSid() {
		return a.DBComponent.GetSid() < b.DBComponent.GetSid()
	}
	return a.AppComponent.GetSid() < b.AppComponent.GetSid()
}

func sortInstanceProperties(a, b *spb.SapDiscovery_Resource_InstanceProperties) bool {
	return a.GetVirtualHostname() < b.GetVirtualHostname()
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
		abap    bool
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
		abap: true,
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
		abap:    true,
		wantErr: cmpopts.AnyError,
	}, {
		name: "noHostsInUserstoreOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		abap:    true,
		wantErr: cmpopts.AnyError,
	}, {
		name: "javaGrepEerror",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return defaultErrorResult
		},
		abap:    false,
		wantErr: cmpopts.AnyError,
	}, {
		name: "javaNoHostInProfile",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  nil,
			}
		},
		abap:    false,
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: test.exec,
			}
			got, err := d.discoverAppToDBConnection(context.Background(), defaultSID, test.abap)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverAppToDBConnection() mismatch (-want, +got):\n%s", diff)
			}
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverAppToDBConnection() error: got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestDiscoverDatabaseSIDUserStore(t *testing.T) {
	tests := []struct {
		name    string
		exec    commandlineexecutor.Execute
		want    string
		wantErr error
	}{{
		name: "hdbUserStoreErr",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noSIDInUserStore",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : dnwh75ldbci:30013\nUSER: SAPABAP1\nOperation succeed.",
				StdErr: "",
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "sidInUserStore",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `KEY default
				ENV : dnwh75ldbci:30013
				USER: SAPABAP1
				DATABASE: DEH
			Operation succeed.`,
				StdErr: "",
			}
		},
		want: "DEH",
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: tc.exec,
			}
			got, err := d.discoverDatabaseSIDUserStore(ctx, defaultSID, defaultSIDAdm)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("discoverDatabaseSIDUserStore() error expectation: %s", diff)
			}

			if got != tc.want {
				t.Errorf("discoverDatabaseSIDUserStore() = %q, want: %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverDatabaseSIDProfiles(t *testing.T) {
	tests := []struct {
		name    string
		exec    commandlineexecutor.Execute
		abap    bool
		want    string
		wantErr error
	}{{
		name: "profileGrepErr",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "noSIDInGrep",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "dbidInProfile",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
				dbid = HN1
				SAPSYSTEMNAME = PTJ
				#-----------------------------------------------------------------------
				# SAP Central Service Instance for J2EE
				#-----------------------------------------------------------------------
				j2ee/scs/host = anjh75ldbci
				j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		want: "HN1",
	}, {
		name: "dbmsNameInProfile",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbms/name = HN1
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		want: "HN1",
	}, {
		name: "j2eeDbnameInProfile",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
j2ee/dbname = HN1
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		want: "HN1",
	}, {
		name: "dbsHdbDbnameInProfile",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbs/hdb/dbname = HN1
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		want: "HN1",
	}, {
		name: "abapAlwaysUsesDBID",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbs/hdb/dbname = HN4
j2ee/dbname = HN3
dbms/name = HN2
dbid = HN1
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: true,
		want: "HN1",
	}, {
		name: "abapPrioritizesDbmsName",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbs/hdb/dbname = HN4
j2ee/dbname = HN3
dbms/name = HN2
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: true,
		want: "HN2",
	}, {
		name: "abapWithJ2eeName",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbs/hdb/dbname = HN4
j2ee/dbname = HN3
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: true,
		want: "HN3",
	}, {
		name: "abapWithDbsHdbName",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
dbs/hdb/dbname = HN4
SAPSYSTEMNAME = PTJ
#-----------------------------------------------------------------------
# SAP Central Service Instance for J2EE
#-----------------------------------------------------------------------
j2ee/scs/host = anjh75ldbci
j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: true,
		want: "HN4",
	}, {
		name: "javaAlwaysUsesJ2ee",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
	dbs/hdb/dbname = HN4
	dbms/name = HN2
	dbid = HN1
	j2ee/dbname = HN3
	SAPSYSTEMNAME = PTJ
	#-----------------------------------------------------------------------
	# SAP Central Service Instance for J2EE
	#-----------------------------------------------------------------------
	j2ee/scs/host = anjh75ldbci
	j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: false,
		want: "HN3",
	}, {
		name: "javaPrioritizesDbsHdbName",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
						dbms/name = HN2
						dbid = HN1
						dbs/hdb/dbname = HN4
		SAPSYSTEMNAME = PTJ
		#-----------------------------------------------------------------------
		# SAP Central Service Instance for J2EE
		#-----------------------------------------------------------------------
		j2ee/scs/host = anjh75ldbci
		j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: false,
		want: "HN4",
	}, {
		name: "javaWithDbid",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
							dbms/name = HN2
							dbid = HN1
			SAPSYSTEMNAME = PTJ
			#-----------------------------------------------------------------------
			# SAP Central Service Instance for J2EE
			#-----------------------------------------------------------------------
			j2ee/scs/host = anjh75ldbci
			j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: false,
		want: "HN1",
	}, {
		name: "javaWithOnlyDbmsName",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: `SAPDBHOST = anjh75ldbci
								dbms/name = HN2
				SAPSYSTEMNAME = PTJ
				#-----------------------------------------------------------------------
				# SAP Central Service Instance for J2EE
				#-----------------------------------------------------------------------
				j2ee/scs/host = anjh75ldbci
				j2ee/scs/system = 01`,
				StdErr: "",
			}
		},
		abap: false,
		want: "HN2",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := SapDiscovery{
				Execute: tc.exec,
			}
			got, err := d.discoverDatabaseSIDProfiles(context.Background(), defaultSID, defaultSIDAdm, tc.abap)
			if diff := cmp.Diff(err, tc.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("discoverDatabaseSIDProfiles() error expectation diff: %s", diff)
			}
			if got != tc.want {
				t.Errorf("discoverDatabaseSIDProfiles() = %q, want: %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverDatabaseSID(t *testing.T) {
	var execCalls map[string]int
	tests := []struct {
		name          string
		exec          commandlineexecutor.Execute
		abap          bool
		want          string
		wantErr       error
		wantExecCalls map[string]int
	}{{
		name: "hdbUserStoreAndGrepError",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		abap:          true,
		wantErr:       cmpopts.AnyError,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name: "profileGrepErr",
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
		abap:          true,
		wantErr:       cmpopts.AnyError,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name: "noSIDInGrep",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		abap:          true,
		wantErr:       cmpopts.AnyError,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name: "sidInUserStore",
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
		abap:          true,
		want:          "DEH",
		wantErr:       nil,
		wantExecCalls: map[string]int{"sudo": 1},
	}, {
		name: "sidInProfiles",
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
				/usr/sap/S15/SYS/profile/s:rsdb/dbid = HN1`,
				StdErr: "",
			}
		},
		abap:          true,
		want:          "HN1",
		wantErr:       nil,
		wantExecCalls: map[string]int{"sudo": 1, "sh": 1},
	}, {
		name: "javaOnlyUsesProfile",
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			execCalls[params.Executable]++
			if params.Executable == "sudo" {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
					Error:  errors.New("Unexpected call to sudo"),
				}
			}
			return commandlineexecutor.Result{
				StdOut: `
				/usr/sap/S15/SYS/profile/s:rsdb/dbid = HN1`,
				StdErr: "",
			}
		},
		abap:          false,
		want:          "HN1",
		wantErr:       nil,
		wantExecCalls: map[string]int{"sh": 1},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execCalls = make(map[string]int)
			d := SapDiscovery{
				Execute: test.exec,
			}
			got, gotErr := d.discoverDatabaseSID(context.Background(), defaultSID, test.abap)
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
		name:           "sapcontrolFails",
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
			wantErr: cmpopts.AnyError,
		}, {
			name: "commentInProfile",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "#rdisp/mshost = some-other-ascs\nrdisp/mshost = some-test-ascs",
				}
			},
			want: "some-test-ascs",
		}, {
			name: "nonHostnameInProfile",
			execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "some extra line\nrdisp/mshost = not actually a host name",
				}
			},
			wantErr: cmpopts.AnyError,
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: exampleWorkloadProperties,
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "javaNetweaver",
		app: &sappb.SAPInstance{
			Sapsid: "abc",
			Type:   sappb.InstanceType_NETWEAVER,
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			switch params.Executable {
			case "sudo":
				log.CtxLogger(ctx).Infof("important test!! - sudo: %v", params.Args)
				if slices.Contains(params.Args, "hdbuserstore") {
					return defaultErrorResult
				} else if slices.Contains(params.Args, "HAGetFailoverConfig") {
					return defaultFailoverConfigResult
				} else if slices.Contains(params.Args, "R3trans") {
					return defaultErrorResult
				} else if slices.Contains(params.Args, "get.versions.of.deployed.units") {
					log.CtxLogger(ctx).Infof("found it - important test!! - sudo: %v", params.Args)
					return defaultBatchConfigResult
				}
			case "grep":
				return defaultProfileResult
			case "df":
				return netweaverMountResult
			case "sh":
				return defaultProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_JAVA,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: exampleJavaWorkloadProperties,
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance", "test-instance2"},
			WorkloadProperties: exampleWorkloadProperties,
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "DB2",
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
				return db2ProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DB2,
					},
				},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"db2Hostname"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "Oracle",
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
				return oracleProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_ORACLE,
					},
				},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"oracleHostname"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "SQLServer",
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
				return sqlServerProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_SQLSERVER,
					},
				},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"mssHostname"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "ASE",
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
				return aseProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_ASE,
					},
				},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"sybHostname"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "MaxDB",
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
				return maxDBProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_MAXDB,
					},
				},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts:            []string{"adaHostname"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}, {
		name: "Unspecified",
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
				return unspecifiedTypeProfileResult
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:           []string{"fs1-nw-node2", "fs1-nw-node1"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
						ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER_ABAP,
						AscsUri:         "some-test-ascs",
						NfsUri:          "1.2.3.4",
					}},
				Sid:     "abc",
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{},
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
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
			case "sh":
				return defaultProfileResult
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
					}},
				Sid: "abc",
			},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
			AppInstance:        &sappb.SAPInstance{Sapsid: "abc", Type: sappb.InstanceType_NETWEAVER},
		},
	}}
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
				if strings.Contains(params.Args[3], "HDB") {
					return defaultHANAVersionResult
				}
				return landscapeSingleNodeResult
			case "df":
				return hanaMountResult
			default:
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
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts: []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
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
				if strings.Contains(params.Args[3], "HDB") {
					return defaultHANAVersionResult
				}
				return landscapeMultipleNodesResult
			case "df":
				return hanaMountResult
			default:
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
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
			DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
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
				if strings.Contains(params.Args[3], "HDB") {
					return defaultHANAVersionResult
				}
				return landscapeSingleNodeResult
			case "df":
				return hanaMountResult
			default:
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
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts: []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBHosts: []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}},
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
				if strings.Contains(params.Args[3], "HDB") {
					return defaultHANAVersionResult
				}
				return landscapeMultipleNodesResult
			case "df":
				return hanaMountResult
			default:
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
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
			DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_OUT,
			},
			DBHosts: []string{"test-instance", "test-instancew1", "test-instancew2", "test-instancew3"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}},
	}, {
		name: "errGettingNodes",
		app: &sappb.SAPInstance{
			Sapsid:         "abc",
			Type:           sappb.InstanceType_HANA,
			InstanceNumber: "00",
		},
		execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdErr:   "Landscape error",
				Error:    errors.New("Landscape error"),
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
		fileSystem: &fakefs.FileSystem{
			StatResp: []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:  []error{nil},
		},
		want: []SapSystemDetails{},
	}, {
		name: "noUsrSAPExecutePermission",
		sapInstances: &sappb.SAPInstances{
			Instances: []*sappb.SAPInstance{
				&sappb.SAPInstance{
					Sapsid:         "abc",
					Type:           sappb.InstanceType_HANA,
					InstanceNumber: "00",
				},
			},
		}, executor: &fakeCommandExecutor{},
		fileSystem: &fakefs.FileSystem{
			StatResp: []os.FileInfo{fakefs.FileInfo{FakeMode: 0000}},
			StatErr:  []error{nil},
		},
		want: []SapSystemDetails{},
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
				// HDB Version
				Executable: "sudo",
			}, {
				// HANA landscape id
				Executable: "grep",
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultLandscapeIDResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte("")},
			ReadFileErr:  []error{nil},
			StatResp:     []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:      []error{nil},
		},
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
						LandscapeId:     defaultLandscapeID,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}},
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
				Executable: "sudo", // Netweaver hosts
			}, {
				Executable: "sudo", // ABAP
			}, {
				Executable: "sudo", // Java
			}, {
				Executable: "sh", // profile for SID
			}, {
				Executable: "sh", // profile for nodes
			}, {
				Executable: "grep", // Get profile
			}, {
				Executable: "grep", // Get profile
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,                                             // Get profile
				netweaverMountResult,                                             // Get NFS
				defaultNetweaverKernelResult,                                     // Kernel version
				defaultFailoverConfigResult,                                      // Failover config
				defaultNetweaverInstanceListResult,                               // Netweaver hosts
				commandlineexecutor.Result{Error: errors.New("R3trans err")},     // ABAP
				commandlineexecutor.Result{Error: errors.New("batchconfig err")}, // Java
				defaultProfileResult,                                             // profile for SID
				defaultProfileResult,                                             // profile for nodes
				defaultProfileResult,                                             // Get profile
				defaultProfileResult,                                             // Get profile
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
			StatResp:              []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:               []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:            "some-test-ascs",
						NfsUri:             "1.2.3.4",
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			AppInstance: &sappb.SAPInstance{
				Sapsid: "abc",
				Type:   sappb.InstanceType_NETWEAVER,
			},
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
				Executable: "sudo", // Netweaver hosts
			}, {
				Executable: "sudo", // ABAP
			}, {
				Executable: "sudo", // Java
			}, {
				Executable: "sh", // profile for SID
			}, {
				Executable: "sh", // profile for nodes
			}, {
				Executable: "grep", // Get profile
			}, {
				Executable: "grep", // Get profile
			}, {
				Executable: "df", // Get NFS
			}, {
				Executable: "sudo", // Kernel version
			}, {
				Executable: "sudo", // Failover config
			}, {
				Executable: "sudo", // Netweaver hosts
			}, {
				Executable: "sudo", // ABAP
			}, {
				Executable: "sudo", // Java
			}, {
				Executable: "sh", // profile for SID
			}, {
				Executable: "sh", // profile for nodes
			}, {
				Executable: "grep", // Get profile
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,                                             // Get profile
				netweaverMountResult,                                             // Get NFS
				defaultNetweaverKernelResult,                                     // Kernel version
				defaultFailoverConfigResult,                                      // Failover config
				defaultNetweaverInstanceListResult,                               // Netweaver hosts
				commandlineexecutor.Result{Error: errors.New("R3trans err")},     // ABAP
				commandlineexecutor.Result{Error: errors.New("batchconfig err")}, // Java
				defaultProfileResult,                                             // profile for SID
				defaultProfileResult,                                             // profile for nodes
				defaultProfileResult,                                             // Get profile
				defaultProfileResult,                                             // Get profile
				netweaverMountResult,                                             // Get NFS
				defaultNetweaverKernelResult,                                     // Kernel version
				defaultFailoverConfigResult,                                      // Failover config
				defaultNetweaverInstanceListResult,                               // Netweaver hosts
				commandlineexecutor.Result{Error: errors.New("R3trans err")},     // ABAP
				commandlineexecutor.Result{Error: errors.New("batchconfig err")}, // Java
				defaultProfileResult,                                             // profile for SID
				defaultProfileResult,                                             // profile for nodes
				defaultProfileResult,                                             // Get profile
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
			StatResp:              []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:               []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:            "some-test-ascs",
						NfsUri:             "1.2.3.4",
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			AppInstance: &sappb.SAPInstance{
				Sapsid: "abc",
				Type:   sappb.InstanceType_NETWEAVER,
			},
		}, {
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "def",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri:            "some-test-ascs",
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			AppOnHost: true,
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			AppInstance: &sappb.SAPInstance{
				Sapsid: "def",
				Type:   sappb.InstanceType_NETWEAVER,
			},
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
				// HDB Version
				Executable: "sudo",
			}, {
				// HANA Landscape ID
				Executable: "grep",
			}, {
				Executable: "sudo",
			}, {
				Executable: "df",
			}, {
				// HDB Version
				Executable: "sudo",
			}, {
				// HANA Landscape ID
				Executable: "grep",
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult, hanaMountResult, defaultHANAVersionResult, defaultLandscapeIDResult,
				landscapeSingleNodeResult, hanaMountResult, defaultHANAVersionResult, defaultLandscapeIDResult},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}, []byte{}},
			ReadFileErr:  []error{nil, nil},
			StatResp:     []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:      []error{nil},
		},
		want: []SapSystemDetails{{
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "abc",
						InstanceNumber:  "00",
						LandscapeId:     defaultLandscapeID,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "def",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "def",
						InstanceNumber:  "00",
						LandscapeId:     defaultLandscapeID,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "def",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
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
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "/usr/sap/ABC/J11/j2ee/configtool/batchconfig.csh", "-task", "get.versions.of.deployed.units"},
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "dbid\|dbms/name\|j2ee/dbname\|dbs/hdb/dbname" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "SAPDBHOST" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable: "grep",
				Args:       []string{"dbms/type", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "dehadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "dehadm", "/usr/sap/DEH/HDB00/HDB", "version"},
			}, {
				Executable: "grep",
				Args:       []string{"'id ='", "/usr/sap/DEH/SYS/global/hdb/custom/config/nameserver.ini"},
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				defaultNetweaverInstanceListResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				commandlineexecutor.Result{Error: errors.New("batchconfig err")},
				defaultProfileResult,
				defaultProfileResult,
				defaultProfileResult,
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultLandscapeIDResult,
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
			StatResp:              []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:               []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:            "some-test-ascs",
						NfsUri:             "1.2.3.4",
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
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
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "DEH",
						InstanceNumber:  "00",
						LandscapeId:     defaultLandscapeID,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			AppInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_NETWEAVER,
				InstanceNumber: "11",
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "DEH",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
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
				Args:       []string{"-i", "-u", "dehadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "dehadm", "/usr/sap/DEH/HDB00/HDB", "version"},
			}, {
				Executable: "grep",
				Args:       []string{"'id ='", "/usr/sap/DEH/SYS/global/hdb/custom/config/nameserver.ini"},
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
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "/usr/sap/ABC/J11/j2ee/configtool/batchconfig.csh", "-task", "get.versions.of.deployed.units"},
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "dbid\|dbms/name\|j2ee/dbname\|dbs/hdb/dbname" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "SAPDBHOST" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable: "grep",
				Args:       []string{"dbms/type", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}},
			results: []commandlineexecutor.Result{
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultLandscapeIDResult,
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				defaultNetweaverInstanceListResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				commandlineexecutor.Result{Error: errors.New("batchconfig err")},
				defaultProfileResult,
				defaultProfileResult,
				defaultProfileResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
			StatResp:     []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:      []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:            "some-test-ascs",
						NfsUri:             "1.2.3.4",
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
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
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "DEH",
						InstanceNumber:  "00",
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			AppInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_NETWEAVER,
				InstanceNumber: "11",
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "DEH",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
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
				Args:       []string{"-i", "-u", "abcadm", "sapcontrol", "-nr", "11", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "R3trans", "-d", "-w", "/tmp/r3trans/tmp.log"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "abcadm", "/usr/sap/ABC/J11/j2ee/configtool/batchconfig.csh", "-task", "get.versions.of.deployed.units"},
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "dbid\|dbms/name\|j2ee/dbname\|dbs/hdb/dbname" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable:  "sh",
				ArgsToSplit: `-c 'grep "SAPDBHOST" /usr/sap/ABC/SYS/profile/*'`,
			}, {
				Executable: "grep", // Get profile
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "db2adm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}, {
				Executable: "df",
				Args:       []string{"-h"},
			}, {
				Executable: "sudo",
				Args:       []string{"-i", "-u", "db2adm", "/usr/sap/DB2/HDB00/HDB", "version"},
			}, {
				Executable: "grep",
				Args:       []string{"'id ='", "/usr/sap/DB2/SYS/global/hdb/custom/config/nameserver.ini"},
			}, {
				Executable: "grep",
				Args:       []string{"dbms/type", "/sapmnt/abc/profile/DEFAULT.PFL"},
			}},
			results: []commandlineexecutor.Result{
				defaultProfileResult,
				netweaverMountResult,
				defaultNetweaverKernelResult,
				defaultFailoverConfigResult,
				defaultNetweaverInstanceListResult,
				commandlineexecutor.Result{Error: errors.New("R3trans error")},
				commandlineexecutor.Result{Error: errors.New("batchconfig error")},
				defaultProfileResult,
				defaultProfileResult,
				defaultProfileResult,
				landscapeSingleNodeResult,
				hanaMountResult,
				defaultHANAVersionResult,
				defaultLandscapeIDResult,
				defaultProfileResult,
			},
		},
		fileSystem: &fakefs.FileSystem{
			ReadFileResp: [][]byte{[]byte{}},
			ReadFileErr:  []error{nil},
			MkDirErr:     []error{nil},
			ChmodErr:     []error{nil},
			RemoveAllErr: []error{nil},
			StatResp:     []os.FileInfo{fakefs.FileInfo{FakeMode: os.ModePerm}},
			StatErr:      []error{nil},
		},
		want: []SapSystemDetails{{
			AppComponent: &spb.SapDiscovery_Component{
				Sid: "abc",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						ApplicationType:    spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
						AscsUri:            "some-test-ascs",
						NfsUri:             "1.2.3.4",
						KernelVersion:      "SAP Kernel 785 Patch 100",
						AscsInstanceNumber: "01",
						ErsInstanceNumber:  "10",
					}},
				HaHosts: []string{"fs1-nw-node2", "fs1-nw-node1"},
			},
			AppOnHost: true,
			AppHosts:  []string{"fs1-nw-node2", "fs1-nw-node1"},
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DEH",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_HANA,
					},
				},
			},
			DBOnHost:           false,
			DBHosts:            []string{"test-instance"},
			WorkloadProperties: nil,
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "ascs",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ASCS,
			}, {
				VirtualHostname: "ers",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
			}, {
				VirtualHostname: "app1",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "11",
				}},
			}, {
				VirtualHostname: "app2",
				InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "12",
				}},
			}},
			AppInstance: &sappb.SAPInstance{
				Sapsid:         "abc",
				Type:           sappb.InstanceType_NETWEAVER,
				InstanceNumber: "11",
			},
		}, {
			DBComponent: &spb.SapDiscovery_Component{
				Sid: "DB2",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:    spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:    "1.2.3.4",
						DatabaseVersion: "HANA 2.12 Rev 56",
						DatabaseSid:     "DB2",
						InstanceNumber:  "00",
						LandscapeId:     defaultLandscapeID,
					}},
				TopologyType: spb.SapDiscovery_Component_TOPOLOGY_SCALE_UP,
			},
			DBOnHost: true,
			DBHosts:  []string{"test-instance"},
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{{
					Name:    "SAP HANA",
					Version: "2.12 SPS05 Rev56.34",
				}},
			},
			DBInstance: &sappb.SAPInstance{
				Sapsid:         "DB2",
				Type:           sappb.InstanceType_HANA,
				InstanceNumber: "00",
			},
		}},
	}}

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
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:   "1.2.3.4",
						InstanceNumber: "00",
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
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:   "1.2.3.4",
						InstanceNumber: "00",
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
		name: "mergeWlPropertiesOnlyOld",
		oldDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions:           []*pv{},
				SoftwareComponentVersions: []*scp{},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
	}, {
		name:       "mergeWlPropertiesOnlyNew",
		oldDetails: SapSystemDetails{},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
	}, {
		name: "mergeWlProperties",
		oldDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product2",
					Version: "version2",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component2",
					Version:    "version2",
					ExtVersion: "y",
					Type:       "type2",
				}},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}, {
					Name:    "product2",
					Version: "version2",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}, {
					Name:       "component2",
					Version:    "version2",
					ExtVersion: "y",
					Type:       "type2",
				}},
			},
		},
	}, {
		name: "mergeWlPropertiesNewOverwritesOldByName",
		oldDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version1",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version1",
					ExtVersion: "x",
					Type:       "type",
				}},
			},
		},
		newDetails: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version2",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version2",
					ExtVersion: "y",
					Type:       "type2",
				}},
			},
		},
		want: SapSystemDetails{
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*pv{{
					Name:    "product1",
					Version: "version2",
				}},
				SoftwareComponentVersions: []*scp{{
					Name:       "component1",
					Version:    "version2",
					ExtVersion: "y",
					Type:       "type2",
				}},
			},
		},
	}, {
		name: "mergeDBProperties",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						InstanceNumber: "00",
					}},
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
						SharedNfsUri:   "1.2.3.4",
						DatabaseSid:    "DB1",
						InstanceNumber: "01",
					}},
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:   "1.2.3.4",
						DatabaseSid:    "DB1",
						InstanceNumber: "01",
					}},
			},
		},
	}, {
		name: "mergeDBPropertiesAllNew",
		oldDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType: spb.SapDiscovery_Component_DatabaseProperties_DATABASE_TYPE_UNSPECIFIED,
					}},
			},
		},
		newDetails: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:   "1.2.3.4",
						DatabaseSid:    "DB1",
						InstanceNumber: "01",
					}},
			},
		},
		want: SapSystemDetails{
			DBComponent: &spb.SapDiscovery_Component{
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						DatabaseType:   spb.SapDiscovery_Component_DatabaseProperties_HANA,
						SharedNfsUri:   "1.2.3.4",
						DatabaseSid:    "DB1",
						InstanceNumber: "01",
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
	}, {
		name: "mergesInstancePropertiesWithDifferentVirtualHostnames",
		oldDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
			}},
		},
		newDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "otherhostname",
				InstanceNumber:  3456,
			}},
		},
		want: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
			}, {
				VirtualHostname: "otherhostname",
				InstanceNumber:  3456,
			}},
		},
	}, {
		name: "mergesInstancePropertiesOverwritesOldVirtualHostname",
		oldDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
			}},
		},
		newDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  3456,
			}},
		},
		want: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  3456,
			}},
		},
	}, {
		name: "mergesInstancePropertiesCombinesAppInstances",
		oldDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "01",
				}},
			}},
		},
		newDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app2",
					Number: "02",
				}},
			}},
		},
		want: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "01",
				}, {
					Name:   "app2",
					Number: "02",
				}},
			}},
		},
	}, {
		name: "mergesInstancePropertiesOverwritesAppInstances",
		oldDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "01",
				}},
			}},
		},
		newDetails: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "02",
				}},
			}},
		},
		want: SapSystemDetails{
			InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
				VirtualHostname: "saphostagent",
				InstanceNumber:  1234,
				AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
					Name:   "app1",
					Number: "02",
				}},
			}},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mergeSystemDetails(test.oldDetails, test.newDetails)
			diffOpts := []cmp.Option{
				cmp.AllowUnexported(SapSystemDetails{}),
				protocmp.Transform(),
				cmpopts.EquateEmpty(),
				protocmp.SortRepeatedFields(&spb.SapDiscovery_WorkloadProperties{}, "product_versions", "software_component_versions"),
				protocmp.SortRepeatedFields(&spb.SapDiscovery_Resource_InstanceProperties{}, "app_instances"),
				cmpopts.SortSlices(sortInstanceProperties),
			}
			if diff := cmp.Diff(test.want, got, diffOpts...); diff != "" {
				t.Errorf("mergeSystemDetails() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverHANAVersion(t *testing.T) {
	tests := []struct {
		name        string
		exec        func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want        string
		wantProdVer string
		wantErr     error
	}{{
		name: "success",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return defaultHANAVersionResult
		},
		want:        "HANA 2.12 Rev 56",
		wantProdVer: "2.12 SPS05 Rev56.34",
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
			got, gotProdVer, err := d.discoverHANAVersion(context.Background(), app)
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("discoverHANAVersion() error: got %v, want %v", err, tc.wantErr)
			}

			if got != tc.want {
				t.Errorf("discoverHANAVersion() = %v, want: %v", got, tc.want)
			}
			if gotProdVer != tc.wantProdVer {
				t.Errorf("discoverHANAVersion() productVersion = %v, want: %v", gotProdVer, tc.wantProdVer)
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

func TestParseBatchConfigOutput(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    *spb.SapDiscovery_WorkloadProperties
		wantErr error
	}{{
		name: "realisticCase",
		s: `
		INFO: Loading tool launcher...
		INFO: [OS: Linux] [VM vendor: SAP AG] [VM version: 1.8.0_351] [VM type: SAP Java Server VM]
		INFO: Main class to start: "com.sap.engine.configtool.batch.BatchConfig"
		INFO: Loading 21 JAR files: [./lib/jdbc.jar, ./lib/jvmx.jar, ./lib/sap.com~tc~bl~config~impl.jar, ./lib/sap.com~tc~bl~deploy~controller~offline_phase_asm.jar, ./lib/sap.com~tc~bl~gui~impl.jar, ./lib/sap.com~tc~bl~iqlib~impl.jar, ./lib/sap.com~tc~bl~jarsap~impl.jar, ./lib/sap.com~tc~bl~offline_launcher~impl.jar, ./lib/sap.com~tc~bl~opensql~implStandalone.jar, ./lib/sap.com~tc~bl~sl~utility~impl.jar, ./lib/sap.com~tc~exception~impl.jar, ./lib/sap.com~tc~je~cachegui.jar, ./lib/sap.com~tc~je~configtool.jar, ./lib/sap.com~tc~je~configuration~impl.jar, ./lib/sap.com~tc~je~offlineconfiguration~impl.jar, ./lib/sap.com~tc~je~offlinelicense_tool.jar, ./lib/sap.com~tc~je~opm.jar, ./lib/sap.com~tc~logging~java~impl.jar, ./lib/sap.com~tc~sapxmltoolkit~sapxmltoolkit.jar, ./lib/sap.com~tc~sec~likey.jar, ./lib/sap.com~tc~sec~secstorefs~java~core.jar]
		INFO: Start
 Listing the SCA versions:
 AJAX-RUNTIME : 1000.7.50.25.4.20221122172500
 BASETABLES : 1000.7.50.25.0.20220803150200
 BI-WDALV : 1000.7.50.25.0.20220803152500
 BI_UDI : 1000.7.50.25.0.20220803154900
 CFG_ZA : 1000.7.50.25.0.20220803150200
 CFG_ZA_CE : 1000.7.50.25.0.20220803150200
 COMP_BUILDT : 1000.7.50.25.0.20220803154300
 CORE-TOOLS : 1000.7.50.25.3.20221025194100
 CU-BASE-JAVA : 1000.7.50.25.0.20220803155300
 CU-BASE-WD : 1000.7.50.25.0.20220803153900
 DATA-MAPPING : 1000.7.50.25.0.20220803155300
 DI_CLIENTS : 1000.7.50.25.0.20220803150500
 ECM-CORE : 1000.7.50.25.0.20220803145600
 ENGFACADE : 1000.7.50.25.2.20221027184300
 ENGINEAPI : 1000.7.50.25.0.20220803150200
 EP-BASIS : 1000.7.50.25.5.20221208201400
 EP-BASIS-API : 1000.7.50.25.1.20220921195800
 ESCONF_BUILDT : 1000.7.50.25.0.20220803154300
 ESI-UI : 1000.7.50.25.0.20220803160700
 ESMP_BUILDT : 1000.7.50.25.0.20220803155300
 ESP_FRAMEWORK : 1000.7.50.25.0.20220803154700
 ESREG-BASIC : 1000.7.50.25.0.20220803154700
 ESREG-SERVICES : 1000.7.50.25.0.20220803154700
 FRAMEWORK : 1000.7.50.25.0.20220803150500
 FRAMEWORK-EXT : 1000.7.50.25.1.20221208200600
 J2EE-APPS : 1000.7.50.25.3.20221026224800
 J2EE-FRMW : 1000.7.50.25.2.20221209202300
 JSPM : 1000.7.50.25.0.20220803150200
 KM-KW_JIKS : 1000.7.50.25.0.20220803154900
 LM-CORE : 1000.7.50.25.2.20221006174500
 LM-CTS : 1000.7.50.25.0.20220803152500
 LM-CTS-UI : 1000.7.50.25.0.20220803150500
 LM-MODEL-BASE : 1000.7.50.25.0.20220803152500
 LM-MODEL-NW : 1000.7.50.25.0.20220803152500
 LM-SLD : 1000.7.50.25.0.20220803152500
 LM-TOOLS : 1000.7.50.25.0.20220803153900
 LMCFG : 1000.7.50.25.0.20220803152500
 LMCTC : 1000.7.50.25.0.20220803152500
 LMNWABASICAPPS : 1000.7.50.25.2.20221017181000
 LMNWABASICCOMP : 1000.7.50.25.0.20220803145600
 LMNWABASICMBEAN : 1000.7.50.25.0.20220803145600
 LMNWACDP : 1000.7.50.25.0.20220803152500
 LMNWATOOLS : 1000.7.50.25.0.20220803153900
 LMNWAUIFRMRK : 1000.7.50.25.1.20220914195200
 MESSAGING : 1000.7.50.25.9.20221206194700
 MMR_SERVER : 1000.7.50.25.0.20220803154900
 MOIN_BUILDT : 1000.7.50.25.0.20220803150200
 NWTEC : 1000.7.50.25.0.20220803152500
 ODATA-CXF-EXT : 1000.7.50.25.0.20220803152500
 SAP-XI3RDPARTY : 1000.7.50.22.0.20210810194900
 SAP_BUILDT : 1000.7.50.25.0.20220803150200
 SECURITY-EXT : 1000.7.50.25.0.20220803150200
 SERVERCORE : 1000.7.50.25.5.20221121195400
 SERVICE-COMP : 1000.7.50.25.0.20220803155300
 SOAMONBASIC : 1000.7.50.25.1.20221018201200
 SR-UI : 1000.7.50.25.0.20220803160700
 SUPPORTTOOLS : 1000.7.50.25.0.20220803150200
 SWLIFECYCL : 1000.7.50.25.0.20220803153900
 UDDI : 1000.7.50.25.0.20220803154700
 UISAPUI5_JAVA : 1000.7.50.25.3.20221201172400
 UKMS_JAVA : 1000.7.50.25.0.20220803160700
 UMEADMIN : 1000.7.50.25.1.20221014212600
 WD-ADOBE : 1000.7.50.25.0.20220803145600
 WD-APPS : 1000.7.50.25.0.20220803154200
 WD-RUNTIME : 1000.7.50.25.0.20220803145600
 WD-RUNTIME-EXT : 1000.7.50.25.0.20220803154200
 WSRM : 1000.7.50.25.0.20220803154700
 
 Listing the versions of SDAs/EARs per SCA:
 AJAX-RUNTIME : 1000.7.50.25.4.20221122172500
				 sap.com/com.sap.tc.useragent.interface : 7.5025.20221121070113.0000
				 sap.com/com.sap.tc.useragent.service : 7.5025.20221121070113.0000
				 sap.com/sc~ajax-runtime : 7.5025.20220520160029.0000
				 sap.com/tc~ui~lightspeed : 7.5025.20221121070113.0000
				 sap.com/tc~ui~lightspeed_services : 7.5025.20221121070113.0000
 
 BASETABLES : 1000.7.50.25.0.20220803150200
				 sap.com/com.sap.engine.cacheschema : 7.5025.20220627122548.0000
				 sap.com/com.sap.engine.cpt.dbschema : 7.5025.20220523141013.0000
				 sap.com/com.sap.engine.jddischema : 7.5025.20220607134755.0000
				 sap.com/com.sap.engine.monitor.dbschema : 7.5025.20220627132548.0000
				 sap.com/com.sap.security.dbschema : 7.5025.20220627152548.0000
				 sap.com/component.info.db_schema : 7.5025.20220627112548.0000
				 sap.com/jmsjddschema : 7.5025.20220627142548.0000
				 sap.com/scheduler~jddschema : 7.5025.20220627142548.0000
				 sap.com/sessionmgmt~DB : 7.5025.20220607124755.0000
				 sap.com/synclog : 7.5025.20220607144755.0000
				 sap.com/tc~SL~utiljddschema : 7.5025.20220523141013.0000
				 sap.com/tc~TechSrv~XML_DAS_Schema : 7.5025.20220627142548.0000
				 sap.com/textcontainer~jddschema : 7.5025.20220627112548.0000
				 sap.com/tsdbschema : 7.5025.20220627122548.0000
 
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{&pv{Name: "SAP Netweaver", Version: "7.50"}},
			SoftwareComponentVersions: []*scp{
				&scp{Name: "AJAX-RUNTIME", Version: "7.50", ExtVersion: "25", Type: "4"},
				&scp{Name: "BASETABLES", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "BI-WDALV", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "BI_UDI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "CFG_ZA", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "CFG_ZA_CE", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "COMP_BUILDT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "CORE-TOOLS", Version: "7.50", ExtVersion: "25", Type: "3"},
				&scp{Name: "CU-BASE-JAVA", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "CU-BASE-WD", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "DATA-MAPPING", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "DI_CLIENTS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ECM-CORE", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ENGFACADE", Version: "7.50", ExtVersion: "25", Type: "2"},
				&scp{Name: "ENGINEAPI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "EP-BASIS", Version: "7.50", ExtVersion: "25", Type: "5"},
				&scp{Name: "EP-BASIS-API", Version: "7.50", ExtVersion: "25", Type: "1"},
				&scp{Name: "ESCONF_BUILDT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ESI-UI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ESMP_BUILDT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ESP_FRAMEWORK", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ESREG-BASIC", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ESREG-SERVICES", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "FRAMEWORK", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "FRAMEWORK-EXT", Version: "7.50", ExtVersion: "25", Type: "1"},
				&scp{Name: "J2EE-APPS", Version: "7.50", ExtVersion: "25", Type: "3"},
				&scp{Name: "J2EE-FRMW", Version: "7.50", ExtVersion: "25", Type: "2"},
				&scp{Name: "JSPM", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "KM-KW_JIKS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-CORE", Version: "7.50", ExtVersion: "25", Type: "2"},
				&scp{Name: "LM-CTS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-CTS-UI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-MODEL-BASE", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-MODEL-NW", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-SLD", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LM-TOOLS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMCFG", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMCTC", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMNWABASICAPPS", Version: "7.50", ExtVersion: "25", Type: "2"},
				&scp{Name: "LMNWABASICCOMP", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMNWABASICMBEAN", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMNWACDP", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMNWATOOLS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "LMNWAUIFRMRK", Version: "7.50", ExtVersion: "25", Type: "1"},
				&scp{Name: "MESSAGING", Version: "7.50", ExtVersion: "25", Type: "9"},
				&scp{Name: "MMR_SERVER", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "MOIN_BUILDT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "NWTEC", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "ODATA-CXF-EXT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SAP-XI3RDPARTY", Version: "7.50", ExtVersion: "22", Type: "0"},
				&scp{Name: "SAP_BUILDT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SECURITY-EXT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SERVERCORE", Version: "7.50", ExtVersion: "25", Type: "5"},
				&scp{Name: "SERVICE-COMP", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SOAMONBASIC", Version: "7.50", ExtVersion: "25", Type: "1"},
				&scp{Name: "SR-UI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SUPPORTTOOLS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "SWLIFECYCL", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "UDDI", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "UISAPUI5_JAVA", Version: "7.50", ExtVersion: "25", Type: "3"},
				&scp{Name: "UKMS_JAVA", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "UMEADMIN", Version: "7.50", ExtVersion: "25", Type: "1"},
				&scp{Name: "WD-ADOBE", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "WD-APPS", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "WD-RUNTIME", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "WD-RUNTIME-EXT", Version: "7.50", ExtVersion: "25", Type: "0"},
				&scp{Name: "WSRM", Version: "7.50", ExtVersion: "25", Type: "0"},
			},
		},
	}, {
		name: "servercoreOnly",
		s: `
 Listing the SCA versions:
 SERVERCORE : 1000.7.50.25.5.20221121195400
 Listing the versions of SDAs/EARs per SCA:
		`,
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions: []*pv{&pv{Name: "SAP Netweaver", Version: "7.50"}},
			SoftwareComponentVersions: []*scp{
				&scp{Name: "SERVERCORE", Version: "7.50", ExtVersion: "25", Type: "5"},
			},
		},
	}, {
		name: "emptyCase",
		s:    "",
		want: &spb.SapDiscovery_WorkloadProperties{
			ProductVersions:           []*pv{&pv{}},
			SoftwareComponentVersions: []*scp{},
		},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatchConfigOutput(ctx, tc.s)

			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("parseBatchConfigOutput(%v) returned an unexpected diff (-want +got): %v", tc.name, diff)
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

func TestDiscoverNetweaverHosts(t *testing.T) {
	tests := []struct {
		name     string
		app      *sappb.SAPInstance
		exec     *fakeCommandExecutor
		wantASCS []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance
		wantERS  []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance
		wantApp  []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance
	}{{
		name: "oneApp",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{
				oneAppInstanceListResult,
			},
		},
		wantApp: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "app1",
			Number: "11",
		}},
	}, {
		name: "twoApps",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{
				twoAppInstanceListResult,
			},
		},
		wantApp: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "app1",
			Number: "11",
		}, {
			Name:   "app2",
			Number: "12",
		}},
	}, {
		name: "justASCS",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `04.03.2024 11:35:40
				GetSystemInstanceList
				OK
				hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
				ascs, 01, 50113, 50114, 1, MESSAGESERVER, GREEN`,
			}},
		},
		wantASCS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ascs",
			Number: "01",
		}},
	}, {
		name: "justERS",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `04.03.2024 11:35:40
				GetSystemInstanceList
				OK
				hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
				ers, 10, 51013, 51014, 0.5, ENQREP, GREEN`,
			}},
		},
		wantERS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ers",
			Number: "10",
		}},
	}, {
		name: "allParts",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{
				defaultNetweaverInstanceListResult,
			},
		},
		wantASCS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ascs",
			Number: "01",
		}},
		wantERS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ers",
			Number: "10",
		}},
		wantApp: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "app1",
			Number: "11",
		}, {
			Name:   "app2",
			Number: "12",
		}},
	}, {
		name: "commandError",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdErr: "error",
				Error:  errors.New("error"),
			}},
		},
	}, {
		name: "noOutput",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: "",
			}},
		},
	}, {
		name: "singleDigitInstanceNumber",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `
09.07.2024 09:23:47
GetSystemInstanceList
OK
hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
ascs, 1, 50113, 50114, 1, MESSAGESERVER|ENQUE, GREEN
ers, 2, 50213, 50214, 0.5, ENQREP, GREEN
app11, 0, 50013, 50014, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
app12, 0, 50013, 50014, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
app13, 0, 50013, 50014, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN
app14, 0, 50013, 50014, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN`,
			}},
		},
		wantASCS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ascs",
			Number: "01",
		}},
		wantERS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ers",
			Number: "02",
		}},
		wantApp: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "app11",
			Number: "00",
		}, {
			Name:   "app12",
			Number: "00",
		}, {
			Name:   "app13",
			Number: "00",
		}, {
			Name:   "app14",
			Number: "00",
		}},
	}, {
		name: "invalidInstanceNumber",
		app: &sappb.SAPInstance{
			Sapsid:         "sid",
			InstanceNumber: "00",
		},
		exec: &fakeCommandExecutor{
			params: []commandlineexecutor.Params{{
				Executable: "sudo",
				Args:       []string{"-i", "-u", "sidadm", "sapcontrol", "-nr", "00", "-function", "GetSystemInstanceList"},
			}},
			results: []commandlineexecutor.Result{{
				StdOut: `
09.07.2024 09:23:47
GetSystemInstanceList
OK
hostname, instanceNr, httpPort, httpsPort, startPriority, features, dispstatus
ascs, 1, 50113, 50114, 1, MESSAGESERVER|ENQUE, GREEN
ers, qq, 50213, 50214, 0.5, ENQREP, GREEN
app11, 0, 50013, 50014, 3, ABAP|GATEWAY|ICMAN|IGS, GREEN`,
			}},
		},
		wantASCS: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "ascs",
			Number: "01",
		}},
		wantApp: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
			Name:   "app11",
			Number: "00",
		}},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &SapDiscovery{
				Execute: func(c context.Context, p commandlineexecutor.Params) commandlineexecutor.Result {
					return tc.exec.Execute(c, p)
				},
			}

			got1, got2, got3 := d.discoverNetweaverHosts(ctx, tc.app)

			if diff := cmp.Diff(tc.wantASCS, got1, protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaverHosts(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
			if diff := cmp.Diff(tc.wantERS, got2, protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaverHosts(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
			if diff := cmp.Diff(tc.wantApp, got3, protocmp.Transform()); diff != "" {
				t.Errorf("discoverNetweaverHosts(%v) returned an unexpected diff (-want +got): %v", tc.app, diff)
			}
		})
	}
}
