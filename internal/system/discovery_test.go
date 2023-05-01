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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
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
	defaultSID = "ABC"
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
		name   string
		config *cpb.Configuration
		want   bool
	}{{
		name: "succeeds",
		config: &cpb.Configuration{
			CollectionConfiguration: &cpb.CollectionConfiguration{
				SapSystemDiscovery: true,
			},
			CloudProperties: defaultCloudProperties,
		},
		want: true,
	}, {
		name: "failsDueToConfig",
		config: &cpb.Configuration{
			CollectionConfiguration: &cpb.CollectionConfiguration{
				SapSystemDiscovery: false,
			},
		},
		want: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gceService := &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{nil},
				GetInstanceErr:  []error{errors.New("Instance not found")},
			}

			got := StartSAPSystemDiscovery(context.Background(), test.config, gceService, nil)
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  "no/source/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
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
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeSubnetwork",
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"some/address/uri"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "PublicAddress",
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
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
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"noregionsubnet"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
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
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}},
		wantFWR: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		wantFR: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		},
	}, {
		name:   "hasForwardingRulePcs",
		exists: func(f string) bool { return f == "pcs" },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}},
		wantFWR: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		wantFR: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		},
	}, {
		name:       "noClusterCommands",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return false },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
	}, {
		name:       "discoverClusterCommandError",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "line1\nline2",
				StdErr: "",
			}
		},
	}, {
		name:       "noAddressInClusterConfig",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultClusterOutput,
				StdErr: "",
			}
		},
	}, {
		name:   "addressNotInUse",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:   "addressUserNotForwardingRule",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:   "errorGettingForwardingRule",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
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
			got, fwr, fr := d.discoverClusterForwardingRule(defaultProjectID, defaultZone)
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "fwrNoBackendService",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "backendGroupNoZone",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "errorGettingInstanceGroup",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "listInstanceGroupInstancesError",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noInstanceNameInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noProjectInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:   "noZoneInInstancesItem",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType:     spb.SapDiscovery_Resource_STORAGE,
			ResourceKind:     "ComputeFilestore",
			ResourceUri:      "some/filestore/uri",
			RelatedResources: []string{"some/resource/uri"},
		}},
		wantRelatedRes: []string{"some/filestore/uri"},
	}, {
		name:   "multipleFilestore",
		exists: func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType:     spb.SapDiscovery_Resource_STORAGE,
			ResourceKind:     "ComputeFilestore",
			ResourceUri:      "some/filestore/uri",
			RelatedResources: []string{"some/resource/uri"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_STORAGE,
			ResourceKind:     "ComputeFilestore",
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "tmpfs",
				StdErr: "",
			}
		},
	}, {
		name:       "justIPInMounts",
		gceService: &fake.TestGCE{},
		exists:     func(f string) bool { return true },
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			got := d.discoverFilestores(defaultProjectID, test.testIR)
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
		exec         func(commandlineexecutor.Params) commandlineexecutor.Result
		fakeResolver func(string) ([]string, error)
		gceService   *fake.TestGCE
		want         []*spb.SapDiscovery_Resource
	}{{
		name: "appToDBWithIPAddrToInstance",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  "some/compute/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address2",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "PublicAddress",
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeSubnetwork",
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"some/compute/address2"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstance",
			ResourceUri:      "some/compute/instance",
			RelatedResources: []string{"regions/test-region/subnet", "network", "1.2.3.4", "some/compute/address2", "some/compute/disk"},
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}},
	}, {
		name: "appToDBWithIPDirectToInstance",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  "some/compute/disk",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "PublicAddress",
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeSubnetwork",
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind:     "ComputeInstance",
			ResourceUri:      "some/compute/instance",
			RelatedResources: []string{"regions/test-region/subnet", "network", "1.2.3.4", "projects/test-project/zones/test-zone/addresses/test-address", "some/compute/disk"},
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerZonalFwr",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerRegionalFwr",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/regions/test-region/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/regions/test-region/forwardingRules/test-fwr",
		}},
	}, {
		name: "appToDBWithIPToLoadBalancerGlobalFwr",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}, {
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/global/forwardingRules/test-fwr",
		}},
	}, {
		name: "errGettingUserStore",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "errGettingUserStore",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "appToDBWithIPToFwrUnknownLocation",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "errGettingUserStore",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "",
				StdErr: "",
				Error:  errors.New("error"),
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				StdErr: "",
			}
		},
		gceService: &fake.TestGCE{},
	}, {
		name: "unableToResolveHost",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return nil, errors.New("error") },
		gceService:   &fake.TestGCE{},
	}, {
		name: "noAddressesForHost",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
			return commandlineexecutor.Result{
				StdOut: defaultUserstoreOutput,
				StdErr: "",
			}
		},
		fakeResolver: func(string) ([]string, error) { return []string{}, nil },
		gceService:   &fake.TestGCE{},
	}, {
		name: "hostNotComputeAddressNotInstance",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "hostAddressUnrecognizedUser",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "hostAddressFwrUserUnknownLocation",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingName",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingProject",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "projects/test-project/global/addresses/test-address",
		}},
	}, {
		name: "instanceMissingZone",
		exec: func(commandlineexecutor.Params) commandlineexecutor.Result {
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
			ResourceType: spb.SapDiscovery_Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
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
			got := d.discoverAppToDBConnection(defaultCloudProperties, defaultSID, parent)
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
		exec: func(params commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(params commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(params commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(params commandlineexecutor.Params) commandlineexecutor.Result {
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
		exec: func(params commandlineexecutor.Params) commandlineexecutor.Result {
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
			got, gotErr := d.discoverDatabaseSID(test.testSID)
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
