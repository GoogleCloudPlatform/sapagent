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

package system

import (
	"context"
	"testing"
	"time"

	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	workloadmanager "google.golang.org/api/workloadmanager/v1"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	appsdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery/fake"
	clouddiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery/fake"
	hostdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	wlmfake "github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"
	logfake "github.com/GoogleCloudPlatform/sapagent/shared/log/fake"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	defaultInstanceName  = "test-instance-id"
	defaultProjectID     = "test-project-id"
	defaultZone          = "test-zone-a"
	defaultInstanceURI   = "projects/test-project-id/zones/test-zone-a/instances/test-instance-id"
	defaultClusterOutput = `
	line1
	line2
	rsc_vip_int-primary IPaddr2
	anotherline
	params ip 127.0.0.1 other text
	line3
	line4
	`
	defaultUserstoreOutput = `
KEY default
	ENV: 
	a:b:c
  ENV : test-instance:30013
  USER: SAPABAP1
  DATABASE: DEH
Operation succeed.
`
	defaultSID                       = "ABC"
	defaultInstanceNumber            = "00"
	defaultLandscapeOutputSingleNode = `
| Host        | Host   | Host   | Failover | Remove | Storage   | Storage   | Failover | Failover | NameServer | NameServer | IndexServer | IndexServer | Host    | Host    | Worker  | Worker  |
|             | Active | Status | Status   | Status | Config    | Actual    | Config   | Actual   | Config     | Actual     | Config      | Actual      | Config  | Actual  | Config  | Actual  |
|             |        |        |          |        | Partition | Partition | Group    | Group    | Role       | Role       | Role        | Role        | Roles   | Roles   | Groups  | Groups  |
| ----------- | ------ | ------ | -------- | ------ | --------- | --------- | -------- | -------- | ---------- | ---------- | ----------- | ----------- | ------- | ------- | ------- | ------- |
| dru-s4dan   | yes    | info   |          |        |         1 |         0 | default  | default  | master 1   | slave      | worker      | standby     | worker  | standby | default | -       |

overall host status: info
`
	defaultLandscapeOutputMultipleNodes = `
| Host        | Host   | Host   | Failover | Remove | Storage   | Storage   | Failover | Failover | NameServer | NameServer | IndexServer | IndexServer | Host    | Host    | Worker  | Worker  |
|             | Active | Status | Status   | Status | Config    | Actual    | Config   | Actual   | Config     | Actual     | Config      | Actual      | Config  | Actual  | Config  | Actual  |
|             |        |        |          |        | Partition | Partition | Group    | Group    | Role       | Role       | Role        | Role        | Roles   | Roles   | Groups  | Groups  |
| ----------- | ------ | ------ | -------- | ------ | --------- | --------- | -------- | -------- | ---------- | ---------- | ----------- | ----------- | ------- | ------- | ------- | ------- |
| dru-s4dan   | yes    | info   |          |        |         1 |         0 | default  | default  | master 1   | slave      | worker      | standby     | worker  | standby | default | -       |
| dru-s4danw1 | yes    | ok     |          |        |         2 |         2 | default  | default  | master 2   | slave      | worker      | slave       | worker  | worker  | default | default |
| dru-s4danw2 | yes    | ok     |          |        |         3 |         3 | default  | default  | slave      | slave      | worker      | slave       | worker  | worker  | default | default |
| dru-s4danw3 | yes    | info   |          |        |         0 |         1 | default  | default  | master 3   | master     | standby     | master      | standby | worker  | default | default |

overall host status: info
`
)

var (
	defaultCloudProperties = &instancepb.CloudProperties{
		InstanceName:     defaultInstanceName,
		ProjectId:        defaultProjectID,
		Zone:             defaultZone,
		NumericProjectId: "12345",
	}
	resourceListDiffOpts = []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "update_time"),
		protocmp.SortRepeatedFields(&spb.SapDiscovery_Resource{}, "related_resources"),
		protocmp.SortRepeatedFields(&spb.SapDiscovery_Component{}, "resources"),
		cmpopts.SortSlices(resourceLess),
	}
)

func resourceLess(a, b *spb.SapDiscovery_Resource) bool {
	return a.String() < b.String()
}

func TestStartSAPSystemDiscovery(t *testing.T) {
	tests := []struct {
		name               string
		config             *cpb.Configuration
		testLog            *logfake.TestCloudLogging
		testSapDiscovery   *appsdiscoveryfake.SapDiscovery
		testCloudDiscovery *clouddiscoveryfake.CloudDiscovery
		testHostDiscovery  *hostdiscoveryfake.HostDiscovery
		want               bool
	}{{
		name: "succeeds",
		config: &cpb.Configuration{
			CloudProperties: defaultCloudProperties,
			DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
				EnableDiscovery:                &wpb.BoolValue{Value: true},
				SapInstancesUpdateFrequency:    &dpb.Duration{Seconds: 10},
				SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 10},
			},
		},
		testLog: &logfake.TestCloudLogging{
			FlushErr: []error{nil},
		},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: true,
	}, {
		name: "failsDueToConfig",
		config: &cpb.Configuration{
			DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
				EnableDiscovery: &wpb.BoolValue{Value: false},
			},
		},
		testLog: &logfake.TestCloudLogging{
			FlushErr: []error{nil},
		},
		want: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &Discovery{
				SapDiscoveryInterface:   test.testSapDiscovery,
				CloudDiscoveryInterface: test.testCloudDiscovery,
				HostDiscoveryInterface:  test.testHostDiscovery,
				AppsDiscovery:           func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
			}

			ctx, cancel := context.WithCancel(context.Background())
			got := StartSAPSystemDiscovery(ctx, test.config, d)
			if got != test.want {
				t.Errorf("StartSAPSystemDiscovery(%#v) = %t, want: %t", test.config, got, test.want)
			}
			cancel()
		})
	}
}

func TestDiscoverSAPSystems(t *testing.T) {
	tests := []struct {
		name               string
		config             *cpb.Configuration
		testSapDiscovery   *appsdiscoveryfake.SapDiscovery
		testCloudDiscovery *clouddiscoveryfake.CloudDiscovery
		testHostDiscovery  *hostdiscoveryfake.HostDiscovery
		want               []*spb.SapDiscovery
	}{{
		name:   "noDiscovery",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: []*spb.SapDiscovery{},
	}, {
		name:   "justHANA",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				DBSID:   "ABC",
				DBHosts: []string{"some-db-host"},
				DBProperties: &spb.SapDiscovery_Component_DatabaseProperties{
					SharedNfsUri: "some-shared-nfs-uri",
				},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some-db-host",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-shared-nfs-uri",
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent: "some-shared-nfs-uri",
				CP:     defaultCloudProperties,
			}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: []*spb.SapDiscovery{{
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
					DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
						SharedNfsUri: "some-shared-nfs-uri",
					},
				},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-db-host",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-shared-nfs-uri",
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "justApp",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID:   "ABC",
				AppHosts: []string{"some-app-host"},
				AppProperties: &spb.SapDiscovery_Component_ApplicationProperties{
					NfsUri:  "some-nfs-uri",
					AscsUri: "some-ascs-uri",
				},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{},
				{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-app-host",
				}}, {{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-nfs-uri",
				}}, {{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-ascs-uri",
				}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent: "some-nfs-uri",
				CP:     defaultCloudProperties,
			}, {
				Parent: "some-ascs-uri",
				CP:     defaultCloudProperties,
			}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
					ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
						AscsUri: "some-ascs-uri",
						NfsUri:  "some-nfs-uri",
					},
				},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-app-host",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-nfs-uri",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-ascs-uri",
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appAndDB",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID: "ABC",
				DBSID:  "DEF",
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}, {}, {}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				Properties:  &spb.SapDiscovery_Component_ApplicationProperties_{},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				Properties:  &spb.SapDiscovery_Component_DatabaseProperties_{},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appOnHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID:    "ABC",
				AppOnHost: true,
				AppHosts:  []string{"some-app-resource"},
			}}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{"some-host-resource"}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceUri:      defaultInstanceURI,
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"some-host-resource"},
			}, {
				ResourceUri:      "some-host-resource",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{defaultInstanceURI},
			}}, {{
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{"some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:        "ABC",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-app-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "DBOnHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				DBSID:    "DEF",
				DBOnHost: true,
				DBHosts:  []string{"some-db-resource"},
			}}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{"some-host-resource"}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceUri:      defaultInstanceURI,
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"some-host-resource"},
			}, {
				ResourceUri:      "some-host-resource",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{defaultInstanceURI},
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{"some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:        "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-db-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appAndDBOnHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID:    "ABC",
				AppOnHost: true,
				AppHosts:  []string{"some-app-resource"},
				DBSID:     "DEF",
				DBOnHost:  true,
				DBHosts:   []string{"some-db-resource"},
			}}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{"some-host-resource"}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceUri:      defaultInstanceURI,
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"some-host-resource"},
			}, {
				ResourceUri:      "some-host-resource",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{defaultInstanceURI},
			}}, {{
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{"some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:        "ABC",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-app-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:        "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-db-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appOnHostDBOffHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID:    "ABC",
				AppOnHost: true,
				AppHosts:  []string{"some-app-resource"},
				DBSID:     "DEF",
				DBOnHost:  false,
				DBHosts:   []string{"some-db-resource"},
			}}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{"some-host-resource"}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceUri:      defaultInstanceURI,
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"some-host-resource"},
			}, {
				ResourceUri:      "some-host-resource",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{defaultInstanceURI},
			}}, {{
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{"some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:        "ABC",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-app-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:        "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:  "some-db-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "DBOnHostAppOffHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID:    "ABC",
				AppOnHost: false,
				AppHosts:  []string{"some-app-resource"},
				DBSID:     "DEF",
				DBOnHost:  true,
				DBHosts:   []string{"some-db-resource"},
			}}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{"some-host-resource"}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceUri:      defaultInstanceURI,
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"some-host-resource"},
			}, {
				ResourceUri:      "some-host-resource",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{defaultInstanceURI},
			}}, {{
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   defaultInstanceURI,
				HostList: []string{"some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceURI,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:        "ABC",
				Properties: &spb.SapDiscovery_Component_ApplicationProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:  "some-app-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:        "DEF",
				Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
				}, {
					ResourceUri:  "some-db-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}, {
					ResourceUri:      "some-host-resource",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{defaultInstanceURI},
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := &Discovery{
				SapDiscoveryInterface:   test.testSapDiscovery,
				CloudDiscoveryInterface: test.testCloudDiscovery,
				HostDiscoveryInterface:  test.testHostDiscovery,
			}
			got := d.discoverSAPSystems(context.Background(), defaultCloudProperties)
			t.Logf("Got systems: %+v ", got)
			t.Logf("Want systems: %+v ", test.want)
			if diff := cmp.Diff(test.want, got, append(resourceListDiffOpts, protocmp.IgnoreFields(&spb.SapDiscovery{}, "update_time"))...); diff != "" {
				t.Errorf("discoverSAPSystems() mismatch (-want, +got):\n%s", diff)
			}
			if len(test.testCloudDiscovery.DiscoverComputeResourcesArgsDiffs) != 0 {
				for _, diff := range test.testCloudDiscovery.DiscoverComputeResourcesArgsDiffs {
					t.Errorf("discoverSAPSystems() discoverCloudResourcesArgs mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
}

func TestResourceToInsight(t *testing.T) {
	tests := []struct {
		name string
		res  *spb.SapDiscovery_Resource
		want *workloadmanager.SapDiscoveryResource
	}{{
		name: "discoveryResourceToInsightResource",
		res: &spb.SapDiscovery_Resource{
			ResourceUri:      "test/uri",
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			RelatedResources: []string{"other/resource"},
			UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
		},
		want: &workloadmanager.SapDiscoveryResource{
			ResourceUri:      "test/uri",
			ResourceKind:     "RESOURCE_KIND_INSTANCE",
			ResourceType:     "RESOURCE_TYPE_COMPUTE",
			RelatedResources: []string{"other/resource"},
			UpdateTime:       "2023-05-01T15:45:11Z",
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := insightResourceFromSystemResource(test.res)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("insightResourceFromSystemResource() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestComponentToInsight(t *testing.T) {
	tests := []struct {
		name string
		comp *spb.SapDiscovery_Component
		want *workloadmanager.SapDiscoveryComponent
	}{{
		name: "discoveryApplicationComponentToInsightComponent",
		comp: &spb.SapDiscovery_Component{
			HostProject: "test/project",
			Sid:         "SID",
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
				ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
					ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
					AscsUri:         "ascs/uri",
					NfsUri:          "nfs/uri",
				},
			},
			Resources: []*spb.SapDiscovery_Resource{{
				ResourceUri:      "test/uri",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"other/resource"},
				UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
			}},
		},
		want: &workloadmanager.SapDiscoveryComponent{
			HostProject: "test/project",
			Sid:         "SID",
			ApplicationProperties: &workloadmanager.SapDiscoveryComponentApplicationProperties{
				ApplicationType: "NETWEAVER",
				AscsUri:         "ascs/uri",
				NfsUri:          "nfs/uri",
			},
			Resources: []*workloadmanager.SapDiscoveryResource{{
				ResourceUri:      "test/uri",
				ResourceKind:     "RESOURCE_KIND_INSTANCE",
				ResourceType:     "RESOURCE_TYPE_COMPUTE",
				RelatedResources: []string{"other/resource"},
				UpdateTime:       "2023-05-01T15:45:11Z",
			}}},
	}, {
		name: "discoveryDatabaseComponentToInsightComponent",
		comp: &spb.SapDiscovery_Component{
			HostProject: "test/project",
			Sid:         "SID",
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
				DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
					DatabaseType:       spb.SapDiscovery_Component_DatabaseProperties_HANA,
					PrimaryInstanceUri: "primary/uri",
					SharedNfsUri:       "shared/uri",
				},
			},
			Resources: []*spb.SapDiscovery_Resource{{
				ResourceUri:      "test/uri",
				ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				RelatedResources: []string{"other/resource"},
				UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
			}},
		},
		want: &workloadmanager.SapDiscoveryComponent{
			HostProject: "test/project",
			Sid:         "SID",
			DatabaseProperties: &workloadmanager.SapDiscoveryComponentDatabaseProperties{
				DatabaseType:       "HANA",
				PrimaryInstanceUri: "primary/uri",
				SharedNfsUri:       "shared/uri",
			},
			Resources: []*workloadmanager.SapDiscoveryResource{{
				ResourceUri:      "test/uri",
				ResourceKind:     "RESOURCE_KIND_INSTANCE",
				ResourceType:     "RESOURCE_TYPE_COMPUTE",
				RelatedResources: []string{"other/resource"},
				UpdateTime:       "2023-05-01T15:45:11Z",
			}}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := insightComponentFromSystemComponent(test.comp)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("insightComponentFromSystemComponent() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverySystemToInsight(t *testing.T) {
	tests := []struct {
		name string
		sys  *spb.SapDiscovery
		want *workloadmanager.Insight
	}{{
		name: "discoverySystemToInsightSystem",
		sys: &spb.SapDiscovery{
			SystemId:   "test-system",
			UpdateTime: timestamppb.New(time.Unix(1682955911, 0)),
			ApplicationLayer: &spb.SapDiscovery_Component{
				HostProject: "test/project",
				Sid:         "SID",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      "test/uri",
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"other/resource"},
					UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
				}}},
			DatabaseLayer: &spb.SapDiscovery_Component{},
		},
		want: &workloadmanager.Insight{
			SapDiscovery: &workloadmanager.SapDiscovery{
				SystemId:   "test-system",
				UpdateTime: "2023-05-01T15:45:11Z",
				ApplicationLayer: &workloadmanager.SapDiscoveryComponent{
					HostProject: "test/project",
					Sid:         "SID",
					Resources: []*workloadmanager.SapDiscoveryResource{{
						ResourceUri:      "test/uri",
						ResourceKind:     "RESOURCE_KIND_INSTANCE",
						ResourceType:     "RESOURCE_TYPE_COMPUTE",
						RelatedResources: []string{"other/resource"},
						UpdateTime:       "2023-05-01T15:45:11Z",
					}}},
				DatabaseLayer: &workloadmanager.SapDiscoveryComponent{}}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := insightFromSAPSystem(test.sys)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("insightFromSAPSystem() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestWriteToCloudLogging(t *testing.T) {
	tests := []struct {
		name         string
		system       *spb.SapDiscovery
		logInterface *logfake.TestCloudLogging
	}{{
		name:   "writeEmptySystem",
		system: &spb.SapDiscovery{},
		logInterface: &logfake.TestCloudLogging{
			ExpectedLogEntries: []logging.Entry{{
				Severity: logging.Info,
				Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
			}},
		},
	}, {
		name: "writeFullSystem",
		system: &spb.SapDiscovery{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "APP",
				HostProject: "test/project",
				Resources: []*spb.SapDiscovery_Resource{
					{ResourceUri: "some/compute/instance", ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE, ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE},
				},
			},
			DatabaseLayer: &spb.SapDiscovery_Component{Sid: "DAT", HostProject: "test/project", Resources: []*spb.SapDiscovery_Resource{
				{ResourceUri: "some/compute/instance", ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE, ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE},
			},
			},
		},
		logInterface: &logfake.TestCloudLogging{
			ExpectedLogEntries: []logging.Entry{{
				Severity: logging.Info,
				Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.logInterface.T = t
			d := Discovery{
				CloudLogInterface: test.logInterface,
			}
			d.writeToCloudLogging(test.system)
		})
	}
}

func TestUpdateSAPInstances(t *testing.T) {
	tests := []struct {
		name              string
		config            *cpb.Configuration
		discoverResponses []*sappb.SAPInstances
		wantInstances     []*sappb.SAPInstances // An array to test asynchronous update functionality
	}{{
		name: "singleUpdate",
		config: &cpb.Configuration{DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
			SapInstancesUpdateFrequency: &dpb.Duration{Seconds: 5},
		}},
		discoverResponses: []*sappb.SAPInstances{{Instances: []*sappb.SAPInstance{{
			Sapsid: "abc",
		}}}},
		wantInstances: []*sappb.SAPInstances{{Instances: []*sappb.SAPInstance{{
			Sapsid: "abc",
		}}}},
	}, {
		name: "multipleUpdates",
		config: &cpb.Configuration{DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
			SapInstancesUpdateFrequency: &dpb.Duration{Seconds: 5},
		}},
		discoverResponses: []*sappb.SAPInstances{{Instances: []*sappb.SAPInstance{{
			Sapsid: "abc",
		}}}, {Instances: []*sappb.SAPInstance{{
			Sapsid: "def",
		}}}},
		wantInstances: []*sappb.SAPInstances{{Instances: []*sappb.SAPInstance{{
			Sapsid: "abc",
		}}}, {Instances: []*sappb.SAPInstance{{
			Sapsid: "def",
		}}}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			discoverCalls := 0
			d := &Discovery{
				AppsDiscovery: func(context.Context) *sappb.SAPInstances {
					defer func() {
						discoverCalls++
					}()
					return test.discoverResponses[discoverCalls]
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			go updateSAPInstances(ctx, test.config, d)
			var oldInstances *sappb.SAPInstances
			for _, want := range test.wantInstances {
				// Wait the update time
				log.CtxLogger(ctx).Info("Checking updated instances")
				var got *sappb.SAPInstances
				for {
					got = d.GetSAPInstances()
					if got != nil && (oldInstances == nil || got != oldInstances) {
						oldInstances = got
						break
					}
					time.Sleep(test.config.GetDiscoveryConfiguration().GetSapInstancesUpdateFrequency().AsDuration() / 2)
				}
				if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
					t.Errorf("updateSAPInstances() mismatch (-want, +got):\n%s", diff)
				}
			}
			cancel()
		})
	}
}

func TestRunDiscovery(t *testing.T) {
	tests := []struct {
		name               string
		config             *cpb.Configuration
		testLog            *logfake.TestCloudLogging
		testSapDiscovery   *appsdiscoveryfake.SapDiscovery
		testCloudDiscovery *clouddiscoveryfake.CloudDiscovery
		testHostDiscovery  *hostdiscoveryfake.HostDiscovery
		testWLM            *wlmfake.TestWLM
		wantSystems        [][]*spb.SapDiscovery
	}{{
		name: "singleUpdate",
		config: &cpb.Configuration{
			CloudProperties: defaultCloudProperties,
			DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
				SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 5},
			},
		},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID: "ABC",
				DBSID:  "DEF",
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}, {}, {}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		testLog: &logfake.TestCloudLogging{
			ExpectedLogEntries: []logging.Entry{{
				Severity: logging.Info,
				Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
			}}},
		testWLM: &wlmfake.TestWLM{
			WriteInsightArgs: []wlmfake.WriteInsightArgs{{
				Project:  "test-project-id",
				Location: "test-zone",
				Req: &workloadmanager.WriteInsightRequest{
					Insight: &workloadmanager.Insight{
						SapDiscovery: &workloadmanager.SapDiscovery{
							ApplicationLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "ABC",
								ApplicationProperties: &workloadmanager.SapDiscoveryComponentApplicationProperties{
									ApplicationType: "APPLICATION_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
							DatabaseLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "DEF",
								DatabaseProperties: &workloadmanager.SapDiscoveryComponentDatabaseProperties{
									DatabaseType: "DATABASE_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
						},
					},
				},
			}},
			WriteInsightErrs: []error{nil},
		},
		wantSystems: [][]*spb.SapDiscovery{{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				Properties:  &spb.SapDiscovery_Component_ApplicationProperties_{},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				Properties:  &spb.SapDiscovery_Component_DatabaseProperties_{},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}}},
	}, {
		name: "multipleUpdates",
		config: &cpb.Configuration{
			CloudProperties: defaultCloudProperties,
			DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
				SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 5},
			},
		},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppSID: "ABC",
				DBSID:  "DEF",
			}}, {{
				AppSID: "GHI",
				DBSID:  "JKL",
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}, {}, {}, {}, {}, {}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}, {}},
		},
		testLog: &logfake.TestCloudLogging{
			ExpectedLogEntries: []logging.Entry{{
				Severity: logging.Info,
				Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
			}, {
				Severity: logging.Info,
				Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
			}}},
		testWLM: &wlmfake.TestWLM{
			WriteInsightArgs: []wlmfake.WriteInsightArgs{{
				Project:  "test-project-id",
				Location: "test-zone",
				Req: &workloadmanager.WriteInsightRequest{
					Insight: &workloadmanager.Insight{
						SapDiscovery: &workloadmanager.SapDiscovery{
							ApplicationLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "ABC",
								ApplicationProperties: &workloadmanager.SapDiscoveryComponentApplicationProperties{
									ApplicationType: "APPLICATION_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
							DatabaseLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "DEF",
								DatabaseProperties: &workloadmanager.SapDiscoveryComponentDatabaseProperties{
									DatabaseType: "DATABASE_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
						},
					},
				},
			}, {
				Project:  "test-project-id",
				Location: "test-zone",
				Req: &workloadmanager.WriteInsightRequest{
					Insight: &workloadmanager.Insight{
						SapDiscovery: &workloadmanager.SapDiscovery{
							ApplicationLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "GHI",
								ApplicationProperties: &workloadmanager.SapDiscoveryComponentApplicationProperties{
									ApplicationType: "APPLICATION_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
							DatabaseLayer: &workloadmanager.SapDiscoveryComponent{
								Sid: "JKL",
								DatabaseProperties: &workloadmanager.SapDiscoveryComponentDatabaseProperties{
									DatabaseType: "DATABASE_TYPE_UNSPECIFIED",
								},
								HostProject: "12345",
							},
						},
					},
				},
			}},
			WriteInsightErrs: []error{nil, nil},
		},
		wantSystems: [][]*spb.SapDiscovery{{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				Properties:  &spb.SapDiscovery_Component_ApplicationProperties_{},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				Properties:  &spb.SapDiscovery_Component_DatabaseProperties_{},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}}, {{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "GHI",
				Properties:  &spb.SapDiscovery_Component_ApplicationProperties_{},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "JKL",
				Properties:  &spb.SapDiscovery_Component_DatabaseProperties_{},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.testLog.T = t
			test.testWLM.T = t
			d := &Discovery{
				WlmService:              test.testWLM,
				CloudLogInterface:       test.testLog,
				SapDiscoveryInterface:   test.testSapDiscovery,
				CloudDiscoveryInterface: test.testCloudDiscovery,
				HostDiscoveryInterface:  test.testHostDiscovery,
			}
			ctx, cancel := context.WithCancel(context.Background())
			go runDiscovery(ctx, test.config, d)

			var oldSystems []*spb.SapDiscovery
			for _, want := range test.wantSystems {
				log.CtxLogger(ctx).Info("Checking updated instances")
				var got []*spb.SapDiscovery
				for {
					got = d.GetSAPSystems()
					if got != nil && (oldSystems == nil || got[0].GetUpdateTime() != oldSystems[0].GetUpdateTime()) {
						// Got something different, compare with wanted
						oldSystems = got
						break
					}
					// Wait half the refresh interval and check for an update again
					time.Sleep(test.config.GetDiscoveryConfiguration().GetSystemDiscoveryUpdateFrequency().AsDuration() / 2)
				}
				if diff := cmp.Diff(want, got, append(resourceListDiffOpts, protocmp.IgnoreFields(&spb.SapDiscovery{}, "update_time"))...); diff != "" {
					t.Errorf("runDiscovery() mismatch (-want, +got):\n%s", diff)
				}
			}
			cancel()
		})
	}
}
