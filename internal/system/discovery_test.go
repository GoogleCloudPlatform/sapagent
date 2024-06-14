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
	"errors"
	"io"
	"os"
	"testing"
	"time"

	dpb "google.golang.org/protobuf/types/known/durationpb"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"

	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	appsdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery/fake"
	clouddiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/clouddiscovery/fake"
	hostdiscoveryfake "github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	dwpb "github.com/GoogleCloudPlatform/sapagent/protos/datawarehouse"
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
		protocmp.SortRepeatedFields(&spb.SapDiscovery_Resource_InstanceProperties{}, "app_instances"),
		cmpopts.SortSlices(appInstanceLess),
	}
	defaultInstanceResource = &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		ResourceUri:  defaultInstanceURI,
	}
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func resourceLess(a, b *spb.SapDiscovery_Resource) bool {
	return a.String() < b.String()
}

func appInstanceLess(a, b *spb.SapDiscovery_Resource_InstanceProperties_AppInstance) bool {
	return a.Name < b.Name
}

type MockFileInfo struct {
}

func (mfi MockFileInfo) Name() string       { return "name" }
func (mfi MockFileInfo) Size() int64        { return int64(8) }
func (mfi MockFileInfo) Mode() os.FileMode  { return os.ModePerm }
func (mfi MockFileInfo) ModTime() time.Time { return time.Now() }
func (mfi MockFileInfo) IsDir() bool        { return false }
func (mfi MockFileInfo) Sys() any           { return nil }

func TestStartSAPSystemDiscovery(t *testing.T) {
	config := &cpb.Configuration{
		CloudProperties: defaultCloudProperties,
		DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
			EnableDiscovery:                &wpb.BoolValue{Value: true},
			SapInstancesUpdateFrequency:    &dpb.Duration{Seconds: 10},
			SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 10},
		},
	}

	d := &Discovery{
		SapDiscoveryInterface: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{}},
		},
		CloudDiscoveryInterface: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{}},
		},
		HostDiscoveryInterface: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		AppsDiscovery:     func(context.Context) *sappb.SAPInstances { return &sappb.SAPInstances{} },
		CloudLogInterface: &logfake.TestCloudLogging{FlushErr: []error{nil}},
		OSStatReader:      func(string) (os.FileInfo, error) { return nil, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	got := StartSAPSystemDiscovery(ctx, config, d)
	if got != true {
		t.Errorf("StartSAPSystemDiscovery(%#v) = %t, want: %t", config, got, true)
	}
	cancel()
}

func TestDiscoverSAPSystems(t *testing.T) {
	tests := []struct {
		name               string
		config             *cpb.Configuration
		testSapDiscovery   *appsdiscoveryfake.SapDiscovery
		testCloudDiscovery *clouddiscoveryfake.CloudDiscovery
		testHostDiscovery  *hostdiscoveryfake.HostDiscovery
		testOSStatReader   workloadmanager.OSStatReader
		testFileReader     workloadmanager.ConfigFileReader
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
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							SharedNfsUri:   "some-shared-nfs-uri",
							InstanceNumber: "00",
						},
					},
				},
				DBHosts: []string{"some-db-host"},
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					VirtualHostname: "some-db-host",
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-shared-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-shared-nfs-uri"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
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
						SharedNfsUri:   "some-shared-nfs-uri",
						InstanceNumber: "00",
					},
				},
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  defaultInstanceURI,
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
						VirtualHostname: "some-db-host",
					},
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
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
						ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
							NfsUri:  "some-nfs-host",
							AscsUri: "some-ascs-host",
						},
					},
				},
				AppHosts: []string{"some-app-host"},
				WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
					ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
						&spb.SapDiscovery_WorkloadProperties_ProductVersion{
							Name:    "some-product-name",
							Version: "some-product-version",
						},
					},
					SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
							Name:       "some-software-component-name",
							Version:    "some-software-component-version",
							ExtVersion: "some-software-component-ext-version",
							Type:       "some-software-component-type",
						},
					},
				},
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "some-app-host",
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-app-instance-name",
						Number: "99",
					}},
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some-ascs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-nfs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-ascs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						VirtualHostname: "some-app-host",
						AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
							Name:   "some-app-instance-name",
							Number: "99",
						}},
					},
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
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
					&spb.SapDiscovery_WorkloadProperties_ProductVersion{
						Name:    "some-product-name",
						Version: "some-product-version",
					},
				},
				SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
					&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						Name:       "some-software-component-name",
						Version:    "some-software-component-version",
						ExtVersion: "some-software-component-ext-version",
						Type:       "some-software-component-type",
					},
				},
			},
		}},
	}, {
		name:   "noASCSResource",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
						ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
							NfsUri:  "some-nfs-uri",
							AscsUri: "some-ascs-uri",
						},
					},
				},
				AppHosts: []string{"some-app-host"},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-nfs-uri",
			}}, {}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-nfs-uri"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-ascs-uri"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-nfs-uri",
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "noNFSResource",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
						ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
							NfsUri:  "some-nfs-uri",
							AscsUri: "some-ascs-uri",
						},
					},
				},
				AppHosts: []string{"some-app-host"},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{
				{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  defaultInstanceURI,
				}}, {{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  defaultInstanceURI,
				}},
				{},
				{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-ascs-uri",
				}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-nfs-uri"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-ascs-uri"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
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
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
				},
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
				},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{defaultInstanceResource}, {}, {}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appOnHost",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
				},
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
				Parent:   nil,
				HostList: []string{defaultInstanceURI, "some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
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
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
				},
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
				Parent:   nil,
				HostList: []string{defaultInstanceURI, "some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid: "DEF",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
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
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
				},
				AppOnHost: true,
				AppHosts:  []string{"some-app-resource"},
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
				},
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
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI, "some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
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
				Sid: "DEF",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
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
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
				},
				AppOnHost: true,
				AppHosts:  []string{"some-app-resource"},
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
				},
				DBOnHost: false,
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
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI, "some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
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
				Sid: "DEF",
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
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
				},
				AppOnHost: false,
				AppHosts:  []string{"some-app-resource"},
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "DEF",
				},
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
				ResourceUri:  "some-app-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}, {{
				ResourceUri:  "some-db-resource",
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI, "some-host-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-resource"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-resource"},
				CP:       defaultCloudProperties,
			}},
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid: "ABC",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:  "some-app-resource",
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				}},
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid: "DEF",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceUri:      defaultInstanceURI,
					ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					RelatedResources: []string{"some-host-resource"},
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
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
		name:   "databaseIPropNotAlreadyDiscovered",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							SharedNfsUri: "some-shared-nfs-uri",
						},
					},
				},
				DBHosts: []string{"some-db-host"},
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					VirtualHostname: "some-other-db-host",
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-shared-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some/other/instance",
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-shared-nfs-uri"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-other-db-host"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					},
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some/other/instance",
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
						VirtualHostname: "some-other-db-host",
					},
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
		name:   "databaseIPropMergesWtihDiscoveredIProp",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				DBComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_DatabaseProperties_{
						DatabaseProperties: &spb.SapDiscovery_Component_DatabaseProperties{
							SharedNfsUri: "some-shared-nfs-uri",
						},
					},
				},
				DBHosts:  []string{"some-db-host"},
				DBOnHost: true,
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
					VirtualHostname: "some-db-host",
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "old-db-host",
				},
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-shared-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}, {
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
				ResourceUri:  "some/disk/uri",
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-shared-nfs-uri"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-db-host"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER | spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_DATABASE,
						VirtualHostname: "some-db-host",
					},
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-shared-nfs-uri",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
					ResourceUri:  "some/disk/uri",
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:   "appIPropNotAlreadyDiscovered",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
						ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
							NfsUri:  "some-nfs-host",
							AscsUri: "some-ascs-host",
						},
					},
				},
				AppHosts: []string{"some-app-host"},
				WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
					ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
						&spb.SapDiscovery_WorkloadProperties_ProductVersion{
							Name:    "some-product-name",
							Version: "some-product-version",
						},
					},
					SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
							Name:       "some-software-component-name",
							Version:    "some-software-component-version",
							ExtVersion: "some-software-component-ext-version",
							Type:       "some-software-component-type",
						},
					},
				},
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "some-other-app-host",
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-app-instance-name",
						Number: "99",
					}},
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some-ascs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some/other/instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "some-other-app-instance-name",
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-app-instance-name",
						Number: "99",
					}},
				},
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-nfs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-ascs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-other-app-host"},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
					ResourceUri:  "some-nfs-uri",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-ascs-uri",
				}, {
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some/other/instance",
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
						VirtualHostname: "some-other-app-host",
						AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
							Name:   "some-app-instance-name",
							Number: "99",
						}, {
							Name:   "some-app-instance-name",
							Number: "99",
						}},
					},
				}},
				HostProject: "12345",
			},
			ProjectNumber: "12345",
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
					&spb.SapDiscovery_WorkloadProperties_ProductVersion{
						Name:    "some-product-name",
						Version: "some-product-version",
					},
				},
				SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
					&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						Name:       "some-software-component-name",
						Version:    "some-software-component-version",
						ExtVersion: "some-software-component-ext-version",
						Type:       "some-software-component-type",
					},
				},
			},
		}},
	}, {
		name:   "appIPropMergesWithAlreadyDiscoveredIProp",
		config: &cpb.Configuration{CloudProperties: defaultCloudProperties},
		testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
			DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
				AppComponent: &spb.SapDiscovery_Component{
					Sid: "ABC",
					Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
						ApplicationProperties: &spb.SapDiscovery_Component_ApplicationProperties{
							NfsUri:  "some-nfs-host",
							AscsUri: "some-ascs-host",
						},
					},
				},
				AppHosts: []string{"some-app-host"},
				WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
					ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
						&spb.SapDiscovery_WorkloadProperties_ProductVersion{
							Name:    "some-product-name",
							Version: "some-product-version",
						},
					},
					SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
							Name:       "some-software-component-name",
							Version:    "some-software-component-version",
							ExtVersion: "some-software-component-ext-version",
							Type:       "some-software-component-type",
						},
					},
				},
				InstanceProperties: []*spb.SapDiscovery_Resource_InstanceProperties{{
					InstanceRole: spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-ers-instance-name",
						Number: "88",
					}, {
						Name:   "some-app-instance-name",
						Number: "11",
					}, {
						Name:   "some-other-instance",
						Number: "12",
					}, {
						Name:   "some-other-instance",
						Number: "12",
					}},
				}},
			}}},
		},
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "some-app-host",
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-app-instance-name",
						Number: "11",
					}},
				},
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
				ResourceUri:  "some-nfs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "some-ascs-uri",
			}}, {{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER,
					VirtualHostname: "",
					AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
						Name:   "some-ers-instance-name",
						Number: "99",
					}},
				},
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-app-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-nfs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{"some-ascs-host"},
				CP:       defaultCloudProperties,
			}, {
				Parent:   defaultInstanceResource,
				HostList: []string{""},
				CP:       defaultCloudProperties,
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
					ResourceUri:  defaultInstanceURI,
					InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
						InstanceRole:    spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_APP_SERVER | spb.SapDiscovery_Resource_InstanceProperties_INSTANCE_ROLE_ERS,
						VirtualHostname: "some-app-host",
						AppInstances: []*spb.SapDiscovery_Resource_InstanceProperties_AppInstance{{
							Name:   "some-ers-instance-name",
							Number: "99",
						}, {
							Name:   "some-app-instance-name",
							Number: "11",
						}, {
							Name:   "some-other-instance",
							Number: "12",
						}},
					},
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
			WorkloadProperties: &spb.SapDiscovery_WorkloadProperties{
				ProductVersions: []*spb.SapDiscovery_WorkloadProperties_ProductVersion{
					&spb.SapDiscovery_WorkloadProperties_ProductVersion{
						Name:    "some-product-name",
						Version: "some-product-version",
					},
				},
				SoftwareComponentVersions: []*spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
					&spb.SapDiscovery_WorkloadProperties_SoftwareComponentProperties{
						Name:       "some-software-component-name",
						Version:    "some-software-component-version",
						ExtVersion: "some-software-component-ext-version",
						Type:       "some-software-component-type",
					},
				},
			},
		}},
	}, {
		name: "usesOverrideFile",
		testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
			DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  defaultInstanceURI,
			}}},
			DiscoverComputeResourcesArgs: []clouddiscoveryfake.DiscoverComputeResourcesArgs{{
				Parent:   nil,
				HostList: []string{defaultInstanceURI},
				CP:       defaultCloudProperties,
			}},
		},
		testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
			DiscoverCurrentHostResp: [][]string{{}},
		},
		testOSStatReader: func(string) (os.FileInfo, error) {
			return MockFileInfo{}, nil
		},
		testFileReader: func(string) (io.ReadCloser, error) {
			return fakeReadCloser{
				fileContents: `{
					"databaseLayer": {
						"hostProject": "12345",
						"sid": "DEF"
					},
					"applicationLayer": {
						"hostProject": "12345",
						"sid": "ABC"
					},
					"projectNumber": "12345"
				}`,
			}, nil
		},
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
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
				OSStatReader: func(string) (os.FileInfo, error) {
					return nil, errors.New("No file")
				},
			}
			if test.testOSStatReader != nil {
				d.OSStatReader = test.testOSStatReader
			}
			if test.testFileReader != nil {
				d.FileReader = test.testFileReader
			}
			got := d.discoverSAPSystems(context.Background(), defaultCloudProperties, test.config)
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
				OSStatReader: func(string) (os.FileInfo, error) {
					return nil, errors.New("No file")
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			go updateSAPInstances(ctx, updateSapInstancesArgs{d: d, config: test.config})
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
	}{
		{
			name: "disableWrite",
			config: &cpb.Configuration{
				CloudProperties: defaultCloudProperties,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery:                &wpb.BoolValue{Value: false},
					SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 5},
				},
			},
			testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
				DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
					AppComponent: &spb.SapDiscovery_Component{Sid: "ABC"},
					DBComponent:  &spb.SapDiscovery_Component{Sid: "DEF"},
				}}},
			},
			testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
				DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{defaultInstanceResource}, {}, {}},
			},
			testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
				DiscoverCurrentHostResp: [][]string{{}},
			},
			testLog: &logfake.TestCloudLogging{
				ExpectedLogEntries: []logging.Entry{},
			},
			testWLM: &wlmfake.TestWLM{
				WriteInsightArgs: []wlmfake.WriteInsightArgs{},
				WriteInsightErrs: []error{nil},
			},
			wantSystems: [][]*spb.SapDiscovery{{{
				ApplicationLayer: &spb.SapDiscovery_Component{
					Sid:         "ABC",
					HostProject: "12345",
				},
				DatabaseLayer: &spb.SapDiscovery_Component{
					Sid:         "DEF",
					HostProject: "12345",
				},
				ProjectNumber: "12345",
			}}},
		},
		{
			name: "singleUpdate",
			config: &cpb.Configuration{
				CloudProperties: defaultCloudProperties,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery:                &wpb.BoolValue{Value: true},
					SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 5},
				},
			},
			testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
				DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
					AppComponent: &spb.SapDiscovery_Component{Sid: "ABC"},
					DBComponent:  &spb.SapDiscovery_Component{Sid: "DEF"},
				}}},
			},
			testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
				DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{defaultInstanceResource}, {}, {}},
			},
			testHostDiscovery: &hostdiscoveryfake.HostDiscovery{
				DiscoverCurrentHostResp: [][]string{{}},
			},
			testLog: &logfake.TestCloudLogging{
				ExpectedLogEntries: []logging.Entry{{
					Severity: logging.Info,
					Payload:  map[string]string{"type": "SapDiscovery", "discovery": ""},
				}},
			},
			testWLM: &wlmfake.TestWLM{
				WriteInsightArgs: []wlmfake.WriteInsightArgs{{
					Project:  "test-project-id",
					Location: "test-zone",
					Req: &dwpb.WriteInsightRequest{
						Insight: &dwpb.Insight{
							SapDiscovery: &spb.SapDiscovery{
								ApplicationLayer: &spb.SapDiscovery_Component{
									Sid:         "ABC",
									HostProject: "12345",
								},
								DatabaseLayer: &spb.SapDiscovery_Component{
									Sid:         "DEF",
									HostProject: "12345",
								},
								ProjectNumber: "12345",
							},
						},
						AgentVersion: configuration.AgentVersion,
					},
				}},
				WriteInsightErrs: []error{nil},
			},
			wantSystems: [][]*spb.SapDiscovery{{{
				ApplicationLayer: &spb.SapDiscovery_Component{
					Sid:         "ABC",
					HostProject: "12345",
				},
				DatabaseLayer: &spb.SapDiscovery_Component{
					Sid:         "DEF",
					HostProject: "12345",
				},
				ProjectNumber: "12345",
			}}},
		},
		{
			name: "multipleUpdates",
			config: &cpb.Configuration{
				CloudProperties: defaultCloudProperties,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery:                &wpb.BoolValue{Value: true},
					SystemDiscoveryUpdateFrequency: &dpb.Duration{Seconds: 5},
				},
			},
			testSapDiscovery: &appsdiscoveryfake.SapDiscovery{
				DiscoverSapAppsResp: [][]appsdiscovery.SapSystemDetails{{{
					AppComponent: &spb.SapDiscovery_Component{Sid: "ABC"},
					DBComponent:  &spb.SapDiscovery_Component{Sid: "DEF"},
				}}, {{
					AppComponent: &spb.SapDiscovery_Component{Sid: "GHI"},
					DBComponent:  &spb.SapDiscovery_Component{Sid: "JKL"},
				}}},
			},
			testCloudDiscovery: &clouddiscoveryfake.CloudDiscovery{
				DiscoverComputeResourcesResp: [][]*spb.SapDiscovery_Resource{{defaultInstanceResource}, {}, {}, {defaultInstanceResource}, {}, {}},
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
				}},
			},
			testWLM: &wlmfake.TestWLM{
				WriteInsightArgs: []wlmfake.WriteInsightArgs{{
					Project:  "test-project-id",
					Location: "test-zone",
					Req: &dwpb.WriteInsightRequest{
						Insight: &dwpb.Insight{
							SapDiscovery: &spb.SapDiscovery{
								ApplicationLayer: &spb.SapDiscovery_Component{
									Sid:         "ABC",
									HostProject: "12345",
								},
								DatabaseLayer: &spb.SapDiscovery_Component{
									Sid:         "DEF",
									HostProject: "12345",
								},
								ProjectNumber: "12345",
							},
						},
						AgentVersion: configuration.AgentVersion,
					},
				}, {
					Project:  "test-project-id",
					Location: "test-zone",
					Req: &dwpb.WriteInsightRequest{
						Insight: &dwpb.Insight{
							SapDiscovery: &spb.SapDiscovery{
								ApplicationLayer: &spb.SapDiscovery_Component{
									Sid:         "GHI",
									HostProject: "12345",
								},
								DatabaseLayer: &spb.SapDiscovery_Component{
									Sid:         "JKL",
									HostProject: "12345",
								},
								ProjectNumber: "12345",
							},
						},
						AgentVersion: configuration.AgentVersion,
					},
				}},
				WriteInsightErrs: []error{nil, nil},
			},
			wantSystems: [][]*spb.SapDiscovery{{{
				ApplicationLayer: &spb.SapDiscovery_Component{
					Sid:         "ABC",
					HostProject: "12345",
				},
				DatabaseLayer: &spb.SapDiscovery_Component{
					Sid:         "DEF",
					HostProject: "12345",
				},
				ProjectNumber: "12345",
			}}, {{
				ApplicationLayer: &spb.SapDiscovery_Component{
					Sid:         "GHI",
					HostProject: "12345",
				},
				DatabaseLayer: &spb.SapDiscovery_Component{
					Sid:         "JKL",
					HostProject: "12345",
				},
				ProjectNumber: "12345",
			}}},
		},
	}
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
				OSStatReader: func(string) (os.FileInfo, error) {
					return nil, errors.New("No file")
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			go runDiscovery(ctx, runDiscoveryArgs{config: test.config, d: d})

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

type fakeReadCloser struct {
	fileContents string
	readError    error
	bytesRead    int
}

func (f fakeReadCloser) Read(p []byte) (n int, err error) {
	if f.readError != nil {
		return 0, f.readError
	}
	log.Logger.Infof("Reading from string %s", f.fileContents)
	bytesLeft := len(f.fileContents) - f.bytesRead
	log.Logger.Infof("bytesLeft: %d", bytesLeft)
	bytesToRead := min(len(p), bytesLeft)
	log.Logger.Infof("bytesToRead: %d", bytesToRead)
	copy(p, []byte(f.fileContents[f.bytesRead:f.bytesRead+bytesToRead]))
	log.Logger.Infof("p: %s", string(p))
	f.bytesRead += bytesToRead
	log.Logger.Infof("f.bytesRead: %d", f.bytesRead)
	if f.bytesRead == len(f.fileContents) {
		return bytesToRead, io.EOF
	}
	return bytesToRead, nil
}
func (f fakeReadCloser) Close() error {
	return nil
}

func TestDiscoverOverrideSystem(t *testing.T) {
	tests := []struct {
		name          string
		fileContents  string
		fileOpenErr   error
		fileReadError error
		instance      *spb.SapDiscovery_Resource
		want          []*spb.SapDiscovery
	}{{
		name: "success",
		fileContents: `{
			"databaseLayer": {
				"hostProject": "12345",
				"sid": "DEF"
			},
			"applicationLayer": {
				"hostProject": "12345",
				"sid": "ABC"
			},
			"projectNumber": "12345"
		}`,
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "ABC",
				HostProject: "12345",
			},
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
			},
			ProjectNumber: "12345",
		}},
	}, {
		name:        "readerError",
		fileOpenErr: errors.New("some error"),
		want:        nil,
	}, {
		name: "readAllError",
		fileContents: `{
			"databaseLayer": {
				"hostProject": "12345",
				"sid": "DEF"
			},
			"applicationLayer": {
				"hostProject": "12345",
				"sid": "ABC"
			},
			"projectNumber": "12345"
		}`,
		fileReadError: errors.New("some error"),
		want:          nil,
	}, {
		name:         "jsonError",
		fileContents: "not json",
		want:         nil,
	}, {
		name: "overwritesDatabaseInstance",
		fileContents: `{
			"databaseLayer": {
				"resources": [{
					"resourceType": "RESOURCE_TYPE_COMPUTE",
					"resourceKind": "RESOURCE_KIND_INSTANCE"
				}],
				"hostProject": "12345",
				"sid": "DEF"
			},
			"projectNumber": "12345"
		}`,
		instance: defaultInstanceResource,
		want: []*spb.SapDiscovery{{
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
				Resources:   []*spb.SapDiscovery_Resource{defaultInstanceResource},
			},
			ProjectNumber: "12345",
		}},
	}, {
		name: "overwritesApplicationInstance",
		fileContents: `{
			"applicationLayer": {
				"resources": [{
					"resourceType": "RESOURCE_TYPE_COMPUTE",
					"resourceKind": "RESOURCE_KIND_INSTANCE"
				}],
				"hostProject": "12345",
				"sid": "DEF"
			},
			"projectNumber": "12345"
		}`,
		instance: defaultInstanceResource,
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
				Resources:   []*spb.SapDiscovery_Resource{defaultInstanceResource},
			},
			ProjectNumber: "12345",
		}},
	}, {
		name: "doesntOverwiteDatabaseInstanceWithURI",
		fileContents: `{
			"databaseLayer": {
				"resources": [{
					"resourceType": "RESOURCE_TYPE_COMPUTE",
					"resourceKind": "RESOURCE_KIND_INSTANCE",
					"resourceUri": "some-uri"
				}],
				"hostProject": "12345",
				"sid": "DEF"
			},
			"projectNumber": "12345"
		}`,
		instance: defaultInstanceResource,
		want: []*spb.SapDiscovery{{
			DatabaseLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-uri",
				}},
			},
			ProjectNumber: "12345",
		}},
	}, {
		name: "doesntOverwiteApplicationInstanceWithURI",
		fileContents: `{
			"applicationLayer": {
				"resources": [{
					"resourceType": "RESOURCE_TYPE_COMPUTE",
					"resourceKind": "RESOURCE_KIND_INSTANCE",
					"resourceUri": "some-uri"
				}],
				"hostProject": "12345",
				"sid": "DEF"
			},
			"projectNumber": "12345"
		}`,
		instance: defaultInstanceResource,
		want: []*spb.SapDiscovery{{
			ApplicationLayer: &spb.SapDiscovery_Component{
				Sid:         "DEF",
				HostProject: "12345",
				Resources: []*spb.SapDiscovery_Resource{{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "some-uri",
				}},
			},
			ProjectNumber: "12345",
		}},
	}}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &Discovery{
				FileReader: func(string) (io.ReadCloser, error) {
					return fakeReadCloser{fileContents: tc.fileContents, readError: tc.fileReadError}, tc.fileOpenErr
				},
			}
			got := d.discoverOverrideSystem(ctx, "overrideFile", tc.instance)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("discoverOverrideSystem() returned an unexpected diff (-want +got): %v", diff)
			}
		})
	}
}
