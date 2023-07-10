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
	"testing"
	"time"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	logging "cloud.google.com/go/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/workloadmanager"
	logfake "github.com/GoogleCloudPlatform/sapagent/internal/log/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

const (
	defaultInstanceName  = "test-instance-id"
	defaultProjectID     = "test-project-id"
	defaultZone          = "test-zone"
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
		InstanceName: defaultInstanceName,
		ProjectId:    defaultProjectID,
		Zone:         defaultZone,
	}
	resourceListDiffOpts = []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "update_time"),
		protocmp.SortRepeatedFields(&spb.SapDiscovery_Resource{}, "related_resources"),
		cmpopts.SortSlices(resourceLess),
	}
)

func resourceLess(a, b *spb.SapDiscovery_Resource) bool {
	return a.String() < b.String()
}

func TestStartSAPSystemDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		config  *cpb.Configuration
		testLog *logfake.TestCloudLogging
		want    bool
	}{{
		name: "succeeds",
		config: &cpb.Configuration{
			CollectionConfiguration: &cpb.CollectionConfiguration{
				SapSystemDiscovery: true,
			},
			CloudProperties: defaultCloudProperties,
		},
		testLog: &logfake.TestCloudLogging{
			FlushErr: []error{nil},
		},
		want: true,
	}, {
		name: "failsDueToConfig",
		config: &cpb.Configuration{
			CollectionConfiguration: &cpb.CollectionConfiguration{
				SapSystemDiscovery: false,
			},
		},
		testLog: &logfake.TestCloudLogging{
			FlushErr: []error{nil},
		},
		want: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gceService := &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{nil},
				GetInstanceErr:  []error{errors.New("Instance not found")},
			}

			got := StartSAPSystemDiscovery(context.Background(), test.config, gceService, nil, test.testLog)
			if got != test.want {
				t.Errorf("StartSAPSystemDiscovery(%#v) = %t, want: %t", test.config, got, test.want)
			}
		})
	}
}

func TestDiscoverInstance(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		want       []*spb.SapDiscovery_Resource
	}{{
		name: "justInstance",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/resource/uri",
			}},
			GetInstanceErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "some/resource/uri",
		}},
	}, {
		name: "noInstanceInfo",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{nil},
			GetInstanceErr:  []error{errors.New("Instance not found")},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
			}
			got, _, _ := d.discoverInstance(defaultProjectID, defaultZone, defaultInstanceName)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverInstance() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverDisks(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		testCI     *compute.Instance
		testIR     *spb.SapDiscovery_Resource
		want       []*spb.SapDiscovery_Resource
	}{{
		name: "instanceWithDisks",
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "no/source/disk",
			}, {
				SelfLink: "no/source/disk2",
			}},
			GetDiskErr: []error{nil, nil},
			GetDiskArgs: []*fake.GetDiskArguments{{
				Project:  "test-project-id",
				Zone:     "test-zone",
				DiskName: "noSourceDisk",
			}, {
				Project:  "test-project-id",
				Zone:     "test-zone",
				DiskName: "noSourceDisk2",
			}},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			Disks: []*compute.AttachedDisk{{
				Source:     "",
				DeviceName: "noSourceDisk",
			}, {
				Source:     "",
				DeviceName: "noSourceDisk2",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{&spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "no/source/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "no/source/disk2",
		}},
	}, {
		name: "getDiskFromSource",
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{SelfLink: "uri/for/disk1"}},
			GetDiskErr:  []error{nil},
			GetDiskArgs: []*fake.GetDiskArguments{{
				Project:  "test-project-id",
				Zone:     "test-zone",
				DiskName: "disk1",
			}},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			Disks: []*compute.AttachedDisk{{
				Source:     "source/for/disk1",
				DeviceName: "nonSourceName",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "uri/for/disk1",
		}},
	}, {
		name: "errGettingDisk",
		gceService: &fake.TestGCE{
			GetDiskErr: []error{errors.New("Disk not found"), nil},
			GetDiskResp: []*compute.Disk{
				nil,
				{SelfLink: "uri/for/disk2"},
			},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			Disks: []*compute.AttachedDisk{{
				Source:     "",
				DeviceName: "noSourceDisk",
			}, {
				Source:     "",
				DeviceName: "noSourceDisk2",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{&spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "uri/for/disk2",
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
			}
			got := d.discoverDisks(defaultProjectID, defaultZone, test.testCI, test.testIR)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverDisks() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverNetworks(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		testCI     *compute.Instance
		testIR     *spb.SapDiscovery_Resource
		want       []*spb.SapDiscovery_Resource
	}{{
		name: "instanceWithNetworkInterface",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{SelfLink: "some/address/uri"}},
			GetAddressByIPArgs: []*fake.GetAddressByIPArguments{{
				Project: "test-project-id",
				Region:  "test-region",
				Address: "10.2.3.4",
			}},
			GetAddressByIPErr: []error{nil},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			NetworkInterfaces: []*compute.NetworkInterface{{
				Network:       "network",
				Subnetwork:    "regions/test-region/subnet",
				AccessConfigs: []*compute.AccessConfig{{NatIP: "1.2.3.4"}},
				NetworkIP:     "10.2.3.4",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"some/address/uri"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_PUBLIC_ADDRESS,
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "some/address/uri",
		}},
	}, {
		name:       "noSubnetRegion",
		gceService: &fake.TestGCE{},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			NetworkInterfaces: []*compute.NetworkInterface{{
				Network:    "network",
				Subnetwork: "noregionsubnet",
				NetworkIP:  "10.2.3.4",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:      "network",
			RelatedResources: []string{"noregionsubnet"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:  "noregionsubnet",
		}},
	}, {
		name: "errGetAddressByIP",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{nil},
			GetAddressByIPArgs: []*fake.GetAddressByIPArguments{{
				Project: "test-project-id",
				Region:  "test-region",
				Address: "10.2.3.4",
			}},
			GetAddressByIPErr: []error{errors.New("Error listing addresses")},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			NetworkInterfaces: []*compute.NetworkInterface{{
				Network:    "network",
				Subnetwork: "regions/test-region/subnet",
				NetworkIP:  "10.2.3.4",
			}},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:  "regions/test-region/subnet",
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
			}
			got := d.discoverNetworks(defaultProjectID, test.testCI, test.testIR)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverNetworks() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverClusterForwardingRule(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		exists     commandlineexecutor.Exists
		exec       commandlineexecutor.Execute
		want       []*spb.SapDiscovery_Resource
		wantFWR    *compute.ForwardingRule
		wantFR     *spb.SapDiscovery_Resource
	}{{
		name: "hasForwardingRule",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
			}},
			GetAddressByIPErr:     []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr"}},
			GetForwardingRuleErr:  []error{nil},
		},
		exists: func(string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}},
		wantFWR: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		wantFR: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		},
	}, {
		name:   "hasForwardingRulePcs",
		exists: func(f string) bool { return f == "pcs" },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			}},
			GetForwardingRuleErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}},
		wantFWR: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		wantFR: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		},
	}, {
		name:       "noClusterCommands",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return false },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
	}, {
		name:       "discoverClusterCommandError",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some command error"),
			}
		},
	}, {
		name:       "noVipInClusterConfig",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "line1\nline2",
				StdErr: "",
			}
		},
	}, {
		name:       "noAddressInClusterConfig",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "rsc_vip_int-primary IPaddr2\nnot-an-adress",
				StdErr: "",
			}
		},
	}, {
		name: "errorListingAddresses",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{nil},
			GetAddressByIPErr:  []error{errors.New("Some API error")},
		},
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
	}, {
		name:   "addressNotInUse",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{SelfLink: "some/compute/address"}},
			GetAddressByIPErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:   "addressUserNotForwardingRule",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/zones/test-zone/notFWR/some-name"},
			}},
			GetAddressByIPErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:   "errorGettingForwardingRule",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
			}},
			GetAddressByIPErr:     []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{nil},
			GetForwardingRuleErr:  []error{errors.New("Some API error")},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "some/compute/address",
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
				exists:     test.exists,
				execute:    test.exec,
			}
			got, fwr, fr := d.discoverClusterForwardingRule(context.Background(), defaultProjectID, defaultZone)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverClusterForwardingRule() mismatch (-want, +got):\n%s", diff)
			}
			if diff := cmp.Diff(fwr, test.wantFWR, protocmp.Transform()); diff != "" {
				t.Errorf("discoverClusterForwardingRule() compute.ForwardingRule mismatch (-want, +got):\n%s", diff)
			}
			opts := []cmp.Option{
				protocmp.Transform(),
				protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "update_time"),
			}
			if diff := cmp.Diff(fr, test.wantFR, opts...); diff != "" {
				t.Errorf("discoverClusterForwardingRule() spb.SapDiscovery_Resource mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverLoadBalancer(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		fwr        *compute.ForwardingRule
		fr         *spb.SapDiscovery_Resource
		exists     commandlineexecutor.Exists
		exec       commandlineexecutor.Execute
		want       []*spb.SapDiscovery_Resource
	}{{
		name:   "hasLoadBalancer",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			}, {
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil, nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone/instances/test-instance-id",
				}},
			}, {
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil, nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "fwrNoBackendService",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
		fwr: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		fr: &spb.SapDiscovery_Resource{},
	}, {
		name:   "bakendServiceNoRegion",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/zegion/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
	}, {
		name:   "errorGettingBackendService",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{nil},
			GetRegionalBackendServiceErr:  []error{errors.New("Some API error")},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
	}, {
		name:   "backendGroupNotInstanceGroup",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/notIG/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil, nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "backendGroupNoZone",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/fones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil, nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "errorGettingInstanceGroup",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{
				nil,
				{SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2"},
			},
			GetInstanceGroupErr: []error{errors.New("Some API error"), nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil, nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "listInstanceGroupInstancesError",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			}, {
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil, nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
				nil,
				{
					Items: []*compute.InstanceWithNamedPorts{{
						Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					}},
				},
			},
			ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noInstanceNameInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			}, {
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil, nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone/unstances/test-instance-id1",
				}},
			}, {
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noProjectInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			}, {
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil, nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "zrojects/test-project/zones/test-zone/instances/test-instance-id1",
				}},
			}, {
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noZoneInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
				Backends: []*compute.Backend{{
					Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			}, {
				SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			}},
			GetInstanceGroupErr: []error{nil, nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/ones/test-zone/instances/test-instance-id1",
				}},
			}, {
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
			}},
			GetInstanceErr: []error{nil},
		},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
				execute:    test.exec,
				exists:     test.exists,
			}
			got := d.discoverLoadBalancerFromForwardingRule(test.fwr, test.fr)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverLoadBalancer() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverFilestores(t *testing.T) {
	tests := []struct {
		name           string
		gceService     *fake.TestGCE
		exists         commandlineexecutor.Exists
		exec           commandlineexecutor.Execute
		testIR         *spb.SapDiscovery_Resource
		want           []*spb.SapDiscovery_Resource
		wantRelatedRes []string
	}{{
		name:   "singleFilestore",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "127.0.0.1:/vol",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{
				Instances: []*file.Instance{{Name: "some/filestore/uri"}},
			}},
			GetFilestoreByIPErr: []error{nil},
		},
		testIR: &spb.SapDiscovery_Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "some/filestore/uri",
			RelatedResources: []string{"some/resource/uri"},
		}},
		wantRelatedRes: []string{"some/filestore/uri"},
	}, {
		name:   "multipleFilestore",
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "127.0.0.1:/vol\n127.0.0.2:/vol",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{
				Instances: []*file.Instance{{Name: "some/filestore/uri"}},
			}, {
				Instances: []*file.Instance{{Name: "some/filestore/uri2"}},
			}},
			GetFilestoreByIPErr: []error{nil, nil},
		},
		testIR: &spb.SapDiscovery_Resource{
			ResourceUri:      "some/resource/uri",
			RelatedResources: []string{},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "some/filestore/uri",
			RelatedResources: []string{"some/resource/uri"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "some/filestore/uri2",
			RelatedResources: []string{"some/resource/uri"},
		}},
		wantRelatedRes: []string{"some/filestore/uri", "some/filestore/uri2"},
	}, {
		name:       "noDF",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return false },
	}, {
		name:       "dfErr",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "Permission denied",
				Error:  errors.New("Permission denied"),
			}
		},
	}, {
		name:       "noFilestoreInMounts",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "tmpfs",
				StdErr: "",
			}
		},
	}, {
		name:       "justIPInMounts",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "127.0.0.1",
				StdErr: "",
			}
		},
	}, {
		name: "errGettingFilestore",
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{nil},
			GetFilestoreByIPErr:  []error{errors.New("some error")},
		},
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "127.0.0.1:/vol",
				StdErr: "",
			}
		},
	}, {
		name: "noFilestoreFound",
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		exists: func(f string) bool { return true },
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "127.0.0.1:/vol",
				StdErr: "",
			}
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService: test.gceService,
				execute:    test.exec,
				exists:     test.exists,
			}
			got := d.discoverFilestores(context.Background(), defaultProjectID, test.testIR)
			if diff := cmp.Diff(got, test.want, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverFilestores() mismatch (-want, +got):\n%s", diff)
			}
			if test.wantRelatedRes == nil && test.testIR != nil && len(test.testIR.RelatedResources) != 0 {
				t.Errorf("discoverFilestores() unexpected change to parent Related Resources: %q", test.testIR.RelatedResources)
			}
			if test.wantRelatedRes != nil {
				if diff := cmp.Diff(test.wantRelatedRes, test.testIR.RelatedResources); diff != "" {
					t.Errorf("discoverFilestores() mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
}
func TestDiscoverAppToDBConnection(t *testing.T) {
	tests := []struct {
		name         string
		exec         func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		fakeResolver func(string) ([]string, error)
		gceService   *fake.TestGCE
		want         []*spb.SapDiscovery_Resource
	}{{
		name: "appToDBWithIPAddrToInstance",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/zones/test-zone/instances/test-instance"},
				SelfLink: "some/compute/address",
			}},
			GetAddressErr: []error{nil},
			GetAddressByIPResp: []*compute.Address{{
				Users:    []string{"projects/test-project/zones/test-zone/instances/test-instance"},
				SelfLink: "some/compute/address2",
			}},
			GetAddressByIPErr: []error{nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/compute/instance",
				Disks: []*compute.AttachedDisk{{
					Source:     "",
					DeviceName: "noSourceDisk",
				}},
				NetworkInterfaces: []*compute.NetworkInterface{{
					Network:       "network",
					Subnetwork:    "regions/test-region/subnet",
					AccessConfigs: []*compute.AccessConfig{{NatIP: "1.2.3.4"}},
					NetworkIP:     "10.2.3.4",
				}},
			}},
			GetInstanceErr:       []error{nil},
			GetDiskResp:          []*compute.Disk{{SelfLink: "some/compute/disk"}},
			GetDiskErr:           []error{nil},
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "some/compute/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "some/compute/address2",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_PUBLIC_ADDRESS,
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"some/compute/address2"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:      "some/compute/instance",
			RelatedResources: []string{"regions/test-region/subnet", "network", "1.2.3.4", "some/compute/address2", "some/compute/disk"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}},
	}, {
		name: "appToDBWithIPDirectToInstance",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(host string) ([]string, error) {
			if host == "test-instance" {
				return []string{"1.2.3.4"}, nil
			}
			return nil, errors.New("Unrecognized host")
		},
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
			GetURIForIPErr:  []error{nil},
			GetAddressByIPResp: []*compute.Address{{
				Users:    []string{"projects/test-project/zones/test-zone/instances/test-instance"},
				SelfLink: "projects/test-project/zones/test-zone/addresses/test-address",
			}},
			GetAddressByIPErr: []error{nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/compute/instance",
				Disks: []*compute.AttachedDisk{{
					Source:     "",
					DeviceName: "noSourceDisk",
				}},
				NetworkInterfaces: []*compute.NetworkInterface{{
					Network:       "network",
					Subnetwork:    "regions/test-region/subnet",
					AccessConfigs: []*compute.AccessConfig{{NatIP: "1.2.3.4"}},
					NetworkIP:     "10.2.3.4",
				}},
			}},
			GetInstanceErr:       []error{nil},
			GetDiskResp:          []*compute.Disk{{SelfLink: "some/compute/disk"}},
			GetDiskErr:           []error{nil},
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{}}},
			GetFilestoreByIPErr:  []error{nil},
			GetInstanceByIPResp:  []*compute.Instance{{SelfLink: "projects/test-project/zones/test-zone/instances/test-instance"}},
			GetInstanceByIPErr:   []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "some/compute/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_PUBLIC_ADDRESS,
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:      "some/compute/instance",
			RelatedResources: []string{"regions/test-region/subnet", "network", "1.2.3.4", "projects/test-project/zones/test-zone/addresses/test-address", "some/compute/disk"},
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerZonalFwr",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				SelfLink: "some/compute/address",
			}},
			GetAddressErr:         []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr"}},
			GetForwardingRuleErr:  []error{nil},
			GetForwardingRuleArgs: []*fake.GetForwardingRuleArguments{{
				Project:  "test-project",
				Location: "test-zone",
				Name:     "test-fwr",
			}},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerRegionalFwr",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/regions/test-region/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/regions/test-region/forwardingRules/test-fwr"},
				SelfLink: "some/compute/address",
			}},
			GetAddressErr:         []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{SelfLink: "projects/test-project/regions/test-region/forwardingRules/test-fwr"}},
			GetForwardingRuleErr:  []error{nil},
			GetForwardingRuleArgs: []*fake.GetForwardingRuleArguments{{
				Project:  "test-project",
				Location: "test-region",
				Name:     "test-fwr",
			}},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/regions/test-region/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:  "projects/test-project/regions/test-region/forwardingRules/test-fwr",
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerGlobalFwr",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/global/forwardingRules/test-fwr"},
				SelfLink: "some/compute/address",
			}},
			GetAddressErr:         []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{SelfLink: "projects/test-project/global/forwardingRules/test-fwr"}},
			GetForwardingRuleErr:  []error{nil},
			GetForwardingRuleArgs: []*fake.GetForwardingRuleArguments{{
				Project:  "test-project",
				Location: "",
				Name:     "test-fwr",
			}},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:  "projects/test-project/global/forwardingRules/test-fwr",
		}},
	}, {
		name: "errGettingUserStore",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "errGettingUserStore",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "appToDBWithIPToFwrUnknownLocation",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/unknown-location/forwardingRules/test-fwr"},
				SelfLink: "some/compute/address",
			}},
			GetAddressErr:         []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{SelfLink: "projects/test-project/global/forwardingRules/test-fwr"}},
			GetForwardingRuleErr:  []error{nil},
			GetForwardingRuleArgs: []*fake.GetForwardingRuleArguments{{
				Project:  "test-project",
				Location: "",
				Name:     "test-fwr",
			}},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "errGettingUserStore",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "unableToResolveHost",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return nil, errors.New("error") },
		gceService:   &fake.TestGCE{},
	}, {
		name: "noAddressesForHost",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{}, nil },
		gceService:   &fake.TestGCE{},
	}, {
		name: "hostNotComputeAddressNotInstance",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"nil"},
			GetURIForIPErr:  []error{errors.New("No resource found")},
		},
	}, {
		name: "hostAddressNoUser",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp:  []*compute.Address{{SelfLink: "some/compute/address"}},
			GetAddressErr:   []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "hostAddressUnrecognizedUser",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/zones/test-zone/unknownObject/test-object"},
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "hostAddressFwrUserUnknownLocation",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				SelfLink: "some/compute/address",
				Users:    []string{"projects/test-project/unknownLocation/test-zone/instances/test-object"},
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingName",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/zones/test-zone/not-inst/no-name"},
				SelfLink: "some/compute/address2",
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingProject",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"not-proj/no-project/zones/test-zone/instances/test-instance"},
				SelfLink: "some/compute/address2",
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingZone",
		exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/global/addresses/test-address"},
			GetURIForIPErr:  []error{nil},
			GetAddressResp: []*compute.Address{{
				Users:    []string{"projects/test-project/not-zone/no-zone/instances/test-instance"},
				SelfLink: "some/compute/address2",
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService:   test.gceService,
				hostResolver: test.fakeResolver,
				execute:      test.exec,
			}
			parent := &spb.SapDiscovery_Resource{ResourceUri: "test/parent/uri"}
			got := d.discoverAppToDBConnection(context.Background(), defaultCloudProperties, defaultSID, parent)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverAppToDBConnection() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverDatabaseSID(t *testing.T) {
	var execCalls int
	var userExecCalls int
	tests := []struct {
		name              string
		testSID           string
		exec              commandlineexecutor.Execute
		want              string
		wantErr           error
		wantUserExecCalls int
		wantExecCalls     int
	}{{
		name:    "hdbUserStoreErr",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			if params.User != "" {
				userExecCalls++
			} else {
				execCalls++
			}
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr:           cmpopts.AnyError,
		wantUserExecCalls: 1,
		wantExecCalls:     0,
	}, {
		name:    "profileGrepErr",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			if params.User != "" {
				userExecCalls++
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			}
			execCalls++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		wantErr:           cmpopts.AnyError,
		wantUserExecCalls: 1,
		wantExecCalls:     1,
	}, {
		name:    "noSIDInGrep",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			if params.User != "" {
				userExecCalls++
			} else {
				execCalls++
			}
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
			}
		},
		wantErr:           cmpopts.AnyError,
		wantUserExecCalls: 1,
		wantExecCalls:     1,
	}, {
		name:    "sidInUserStore",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			if params.User != "" {
				userExecCalls++
				return commandlineexecutor.Result{
					StdOut: `KEY default
					ENV : dnwh75ldbci:30013
					USER: SAPABAP1
					DATABASE: DEH
				Operation succeed.`,
					StdErr: "",
				}
			}
			execCalls++
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("Some err"),
			}
		},
		want:              "DEH",
		wantErr:           nil,
		wantUserExecCalls: 1,
		wantExecCalls:     0,
	}, {
		name:    "sidInProfiles",
		testSID: defaultSID,
		exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
			if params.User != "" {
				userExecCalls++
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			}
			execCalls++
			return commandlineexecutor.Result{
				StdOut: `
				grep: /usr/sap/S15/SYS/profile/DEFAULT.PFL: Permission denied
				/usr/sap/S15/SYS/profile/s:rsdb/dbid = HN1`,
				StdErr: "",
			}
		},
		want:              "HN1",
		wantErr:           nil,
		wantUserExecCalls: 1,
		wantExecCalls:     1,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userExecCalls = 0
			execCalls = 0
			d := Discovery{
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
			if userExecCalls != test.wantUserExecCalls {
				t.Errorf("discoverDatabaseSID() userExecCalls = %d, want %d", userExecCalls, test.wantUserExecCalls)
			}
			if execCalls != test.wantExecCalls {
				t.Errorf("discoverDatabaseSID() execCalls = %d, want %d", execCalls, test.wantExecCalls)
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
			ResourceURI:      "test/uri",
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
				AscsURI:         "ascs/uri",
				NfsURI:          "nfs/uri",
			},
			Resources: []*workloadmanager.SapDiscoveryResource{{
				ResourceURI:      "test/uri",
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
				PrimaryInstanceURI: "primary/uri",
				SharedNfsURI:       "shared/uri",
			},
			Resources: []*workloadmanager.SapDiscoveryResource{{
				ResourceURI:      "test/uri",
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
				SystemID:   "test-system",
				UpdateTime: "2023-05-01T15:45:11Z",
				ApplicationLayer: &workloadmanager.SapDiscoveryComponent{
					HostProject: "test/project",
					Sid:         "SID",
					Resources: []*workloadmanager.SapDiscoveryResource{{
						ResourceURI:      "test/uri",
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

func TestDiscoverDBNodes(t *testing.T) {
	tests := []struct {
		name           string
		sid            string
		instanceNumber string
		project        string
		zone           string
		execute        commandlineexecutor.Execute
		gceService     *fake.TestGCE
		want           []*spb.SapDiscovery_Resource
	}{{
		name:           "discoverSingleNode",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputSingleNode,
			}
		},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/compute/instance",
			}},
			GetInstanceErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceUri:  "some/compute/instance",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}},
	}, {
		name:           "discoverMultipleNodes",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputMultipleNodes,
			}
		},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/compute/instance",
			}, {
				SelfLink: "some/compute/instance2",
			}, {
				SelfLink: "some/compute/instance3",
			}, {
				SelfLink: "some/compute/instance4",
			}},
			GetInstanceErr: []error{nil, nil, nil, nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceUri:  "some/compute/instance",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}, {
			ResourceUri:  "some/compute/instance2",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}, {
			ResourceUri:  "some/compute/instance3",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}, {
			ResourceUri:  "some/compute/instance4",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}},
	}, {
		name:           "pythonScriptReturnsNonfatalCode",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   defaultLandscapeOutputSingleNode,
				Error:    cmpopts.AnyError,
				ExitCode: 3,
			}
		},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/compute/instance",
			}},
			GetInstanceErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceUri:  "some/compute/instance",
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		}},
	}, {
		name:           "pythonScriptFails",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   defaultLandscapeOutputSingleNode,
				Error:    cmpopts.AnyError,
				ExitCode: 1,
			}
		},
		want: nil,
	}, {
		name:           "noHostsInOutput",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{}
		},
		gceService: &fake.TestGCE{},
		want:       nil,
	}, {
		name:           "sidNotProvided",
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputSingleNode,
			}
		},
		want: nil,
	}, {
		name:    "instanceNumberNotProvided",
		sid:     defaultSID,
		project: defaultProjectID,
		zone:    defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputSingleNode,
			}
		},
		want: nil,
	}, {
		name:           "projectNotProvided",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		zone:           defaultZone,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputSingleNode,
			}
		},
		want: nil,
	}, {
		name:           "zoneNotProvided",
		sid:            defaultSID,
		instanceNumber: defaultInstanceNumber,
		project:        defaultProjectID,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultLandscapeOutputSingleNode,
			}
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := Discovery{
				execute:    test.execute,
				gceService: test.gceService,
			}
			got := d.discoverDBNodes(context.Background(), test.sid, test.instanceNumber, test.project, test.zone)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverDBNodes() mismatch (-want, +got):\n%s", diff)
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
				cloudLogInterface: test.logInterface,
			}
			d.writeToCloudLogging(test.system)
		})
	}
}

func TestDiscoverASCS(t *testing.T) {
	tests := []struct {
		name         string
		comp         *spb.SapDiscovery_Component
		cp           *instancepb.CloudProperties
		execute      commandlineexecutor.Execute
		resolver     func(string) ([]string, error)
		gceInterface *fake.TestGCE
		wantErr      error
		wantComp     *spb.SapDiscovery_Component
	}{{
		name: "discoverASCS",
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some extra line\nrdisp/mshost = some-test-ascs ",
			}
		},
		resolver: func(string) ([]string, error) {
			return []string{"1.2.3.4"}, nil
		},
		gceInterface: &fake.TestGCE{
			GetURIForIPResp: []string{"some/resource/uri"},
			GetURIForIPErr:  []error{nil},
		},
		wantErr: nil,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{
				AscsUri: "some/resource/uri",
			}},
		},
	}, {
		name: "errorWhenPassedDatabaseProperties",
		comp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
	}, {
		name: "errorExecutingCommand",
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{Error: errors.New("Error running command"), ExitCode: 1}
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}, {
		name: "noHostInProfile",
		comp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some extra line\nrno host in output",
			}
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
	}, {
		name: "emptyHostInProfile",
		comp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some extra line\nrdisp/mshost = ",
			}
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
	}, {
		name: "hostResolutionError",
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some extra line\nrdisp/mshost = some-test-ascs ",
			}
		},
		resolver: func(string) ([]string, error) {
			return nil, errors.New("host resolution error")
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}, {
		name: "hostResolutionError",
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "some extra line\nrdisp/mshost = some-test-ascs ",
			}
		},
		resolver: func(string) ([]string, error) {
			return nil, errors.New("host resolution error")
		},
		gceInterface: &fake.TestGCE{
			GetURIForIPResp: []string{""},
			GetURIForIPErr:  []error{errors.New("error finding resource for IP")},
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := Discovery{
				execute:      test.execute,
				hostResolver: test.resolver,
				gceService:   test.gceInterface,
			}
			gotErr := d.discoverASCS(context.Background(), defaultSID, test.comp, test.cp)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Unexpected error from discoverASCS (got, want), (%s, %s)", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantComp, test.comp, protocmp.Transform()); diff != "" {
				t.Errorf("discoverASCS() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverAppNFS(t *testing.T) {
	tests := []struct {
		name         string
		app          *sappb.SAPInstance
		comp         *spb.SapDiscovery_Component
		cp           *instancepb.CloudProperties
		execute      commandlineexecutor.Execute
		gceInterface *fake.TestGCE
		wantErr      error
		wantComp     *spb.SapDiscovery_Component
	}{{
		name: "discoverAppNFS",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{
				ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
			}},
		},
		cp: defaultCloudProperties,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		gceInterface: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{{Name: "some/resource/uri"}}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		wantErr: nil,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
				&spb.SapDiscovery_Component_ApplicationProperties{
					ApplicationType: spb.SapDiscovery_Component_ApplicationProperties_NETWEAVER,
					NfsUri:          "some/resource/uri",
				}}},
	}, {
		name: "discoverAppNFSWithEmptyProperties",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{},
		cp:   defaultCloudProperties,
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		gceInterface: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{{Name: "some/resource/uri"}}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		wantErr: nil,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{
				&spb.SapDiscovery_Component_ApplicationProperties{
					NfsUri: "some/resource/uri",
				}}},
	}, {
		name: "errorWhenPassedDatabaseProperties",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_DatabaseProperties_{},
		},
	}, {
		name: "errorExecutingCommand",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{Error: errors.New("Error running command"), ExitCode: 1}
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}, {
		name: "noNFSInMounts",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				// StdOut: "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdOut:   "some extra line\n/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}, {
		name: "errorFindingNFSByIP",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		gceInterface: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{nil},
			GetFilestoreByIPErr:  []error{errors.New("some error")},
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}, {
		name: "noNFSFoundByIP",
		app: &sappb.SAPInstance{
			Sapsid: defaultSID,
		},
		comp: &spb.SapDiscovery_Component{},
		execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut:   "some extra line\n1.2.3.4:/some/volume 1007G   42G  914G   5% /sapmnt/ABC",
				StdErr:   "",
				ExitCode: 0,
			}
		},
		gceInterface: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		wantErr: cmpopts.AnyError,
		wantComp: &spb.SapDiscovery_Component{
			Properties: &spb.SapDiscovery_Component_ApplicationProperties_{&spb.SapDiscovery_Component_ApplicationProperties{}},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			d := Discovery{
				execute:    test.execute,
				gceService: test.gceInterface,
			}
			gotErr := d.discoverAppNFS(context.Background(), test.app, test.comp, test.cp)
			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Unexpected error from discoverAppNFS (got, want), (%s, %s)", gotErr, test.wantErr)
			}
			if diff := cmp.Diff(test.wantComp, test.comp, protocmp.Transform()); diff != "" {
				t.Errorf("discoverAppNFS() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
