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
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/workloadmanager"
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
		fakeExists commandlineexecutor.CommandExistsRunner
		fakeRunner commandlineexecutor.CommandRunner
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
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "hasForwardingRulePcs",
		fakeExists: func(f string) bool { return f == "pcs" },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		fakeExists: func(string) bool { return false },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
	}, {
		name:       "discoverClusterCommandError",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "", "", errors.New("Some command error") },
	}, {
		name:       "noVipInClusterConfig",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) {
			return "line1\nline2", "", nil
		},
	}, {
		name:       "noAddressInClusterConfig",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) {
			return "rsc_vip_int-primary IPaddr2\nnot-an-adress", "", nil
		},
	}, {
		name: "errorListingAddresses",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.Address{nil},
			GetAddressByIPErr:  []error{errors.New("Some API error")},
		},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
	}, {
		name:       "addressNotInUse",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "addressUserNotForwardingRule",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "errorGettingForwardingRule",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
				gceService:    test.gceService,
				exists:        test.fakeExists,
				commandRunner: test.fakeRunner,
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
		fakeExists commandlineexecutor.CommandExistsRunner
		fakeRunner commandlineexecutor.CommandRunner
		want       []*spb.SapDiscovery_Resource
	}{{
		name:       "hasLoadBalancer",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "fwrNoBackendService",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{},
		fwr: &compute.ForwardingRule{
			SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
		fr: &spb.SapDiscovery_Resource{},
	}, {
		name:       "bakendServiceNoRegion",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{},
		fwr: &compute.ForwardingRule{
			BackendService: "projects/test-project/zegion/test-region/backendServices/test-bes",
		},
		fr: &spb.SapDiscovery_Resource{
			ResourceUri: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
		},
	}, {
		name:       "errorGettingBackendService",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "backendGroupNotInstanceGroup",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "backendGroupNoZone",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "errorGettingInstanceGroup",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "listInstanceGroupInstancesError",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "noInstanceNameInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "noProjectInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
		name:       "noZoneInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
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
				gceService:    test.gceService,
				commandRunner: test.fakeRunner,
				exists:        test.fakeExists,
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
		fakeExists     commandlineexecutor.CommandExistsRunner
		fakeRunner     commandlineexecutor.CommandRunner
		testIR         *spb.SapDiscovery_Resource
		want           []*spb.SapDiscovery_Resource
		wantRelatedRes []string
	}{{
		name:       "singleFilestore",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "127.0.0.1:/vol", "", nil },
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
		name:       "multipleFilestore",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "127.0.0.1:/vol\n127.0.0.2:/vol", "", nil },
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
		fakeExists: func(string) bool { return false },
	}, {
		name:       "dfErr",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) {
			return "", "Permission denied", errors.New("Permission denied")
		},
	}, {
		name:       "noFilestoreInMounts",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "tmpfs", "", nil },
	}, {
		name:       "justIPInMounts",
		gceService: &fake.TestGCE{},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "127.0.0.1", "", nil },
	}, {
		name: "errGettingFilestore",
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{nil},
			GetFilestoreByIPErr:  []error{errors.New("some error")},
		},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "127.0.0.1:/vol", "", nil },
	}, {
		name: "noFilestoreFound",
		gceService: &fake.TestGCE{
			GetFilestoreByIPResp: []*file.ListInstancesResponse{{Instances: []*file.Instance{}}},
			GetFilestoreByIPErr:  []error{nil},
		},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return "127.0.0.1:/vol", "", nil },
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService:    test.gceService,
				commandRunner: test.fakeRunner,
				exists:        test.fakeExists,
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
		fakeRunner   func(user, executable string, args ...string) (string, string, error)
		fakeResolver func(string) ([]string, error)
		gceService   *fake.TestGCE
		want         []*spb.SapDiscovery_Resource
	}{{
		name:         "appToDBWithIPAddrToInstance",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:       "appToDBWithIPDirectToInstance",
		fakeRunner: func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "appToDBWithIPToLoadBalancerZonalFwr",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "appToDBWithIPToLoadBalancerRegionalFwr",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "appToDBWithIPToLoadBalancerGlobalFwr",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:       "errGettingUserStore",
		fakeRunner: func(string, string, ...string) (string, string, error) { return "", "", errors.New("error") },
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		fakeRunner: func(string, string, ...string) (string, string, error) {
			return "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				"",
				nil
		},
		gceService: &fake.TestGCE{},
	}, {
		name:       "errGettingUserStore",
		fakeRunner: func(string, string, ...string) (string, string, error) { return "", "", errors.New("error") },
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		fakeRunner: func(string, string, ...string) (string, string, error) {
			return "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				"",
				nil
		},
		gceService: &fake.TestGCE{},
	}, {
		name:         "appToDBWithIPToFwrUnknownLocation",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:       "errGettingUserStore",
		fakeRunner: func(string, string, ...string) (string, string, error) { return "", "", errors.New("error") },
		gceService: &fake.TestGCE{},
	}, {
		name: "noHostnameInUserstoreOutput",
		fakeRunner: func(string, string, ...string) (string, string, error) {
			return "KEY default\nENV : \n			USER: SAPABAP1\n			DATABASE: DEH\n		Operation succeed.",
				"",
				nil
		},
		gceService: &fake.TestGCE{},
	}, {
		name:         "unableToResolveHost",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
		fakeResolver: func(string) ([]string, error) { return nil, errors.New("error") },
		gceService:   &fake.TestGCE{},
	}, {
		name:         "noAddressesForHost",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
		fakeResolver: func(string) ([]string, error) { return []string{}, nil },
		gceService:   &fake.TestGCE{},
	}, {
		name:         "hostNotComputeAddressNotInstance",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
		fakeResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"nil"},
			GetURIForIPErr:  []error{errors.New("No resource found")},
		},
	}, {
		name:         "hostAddressNoUser",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "hostAddressUnrecognizedUser",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "hostAddressFwrUserUnknownLocation",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "instanceMissingName",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "instanceMissingProject",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
		name:         "instanceMissingZone",
		fakeRunner:   func(string, string, ...string) (string, string, error) { return defaultUserstoreOutput, "", nil },
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
				gceService:        test.gceService,
				hostResolver:      test.fakeResolver,
				userCommandRunner: test.fakeRunner,
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
	var userRunnerCalls int
	var runnerCalls int
	tests := []struct {
		name                string
		testSID             string
		fakeUserRunner      func(user, executable string, args ...string) (string, string, error)
		fakeRunner          commandlineexecutor.CommandRunner
		want                string
		wantErr             error
		wantUserRunnerCalls int
		wantRunnerCalls     int
	}{{
		name:    "hdbUserStoreErr",
		testSID: defaultSID,
		fakeUserRunner: func(user string, executable string, args ...string) (string, string, error) {
			userRunnerCalls++
			return "", "", errors.New("Some err")
		},
		wantErr:             cmpopts.AnyError,
		wantUserRunnerCalls: 1,
		wantRunnerCalls:     0,
	}, {
		name:    "profileGrepErr",
		testSID: defaultSID,
		fakeUserRunner: func(user string, executable string, args ...string) (string, string, error) {
			userRunnerCalls++
			return "", "", nil
		},
		fakeRunner: func(string, string) (string, string, error) {
			runnerCalls++
			return "", "", errors.New("Some err")
		},
		wantErr:             cmpopts.AnyError,
		wantUserRunnerCalls: 1,
		wantRunnerCalls:     1,
	}, {
		name:    "noSIDInGrep",
		testSID: defaultSID,
		fakeUserRunner: func(user string, executable string, args ...string) (string, string, error) {
			userRunnerCalls++
			return "", "", nil
		},
		fakeRunner: func(string, string) (string, string, error) {
			runnerCalls++
			return "", "", nil
		},
		wantErr:             cmpopts.AnyError,
		wantUserRunnerCalls: 1,
		wantRunnerCalls:     1,
	}, {
		name:    "sidInUserStore",
		testSID: defaultSID,
		fakeUserRunner: func(user string, executable string, args ...string) (string, string, error) {
			userRunnerCalls++
			return `KEY default
			ENV : dnwh75ldbci:30013
			USER: SAPABAP1
			DATABASE: DEH
		Operation succeed.`, "", nil
		},
		fakeRunner: func(string, string) (string, string, error) {
			runnerCalls++
			return "", "", errors.New("Some err")
		},
		want:                "DEH",
		wantErr:             nil,
		wantUserRunnerCalls: 1,
		wantRunnerCalls:     0,
	}, {
		name:    "sidInProfiles",
		testSID: defaultSID,
		fakeUserRunner: func(user string, executable string, args ...string) (string, string, error) {
			userRunnerCalls++
			return "", "", nil
		},
		fakeRunner: func(string, string) (string, string, error) {
			runnerCalls++
			return `
			grep: /usr/sap/S15/SYS/profile/DEFAULT.PFL: Permission denied
			/usr/sap/S15/SYS/profile/s:rsdb/dbid = HN1`, "", nil
		},
		want:                "HN1",
		wantErr:             nil,
		wantUserRunnerCalls: 1,
		wantRunnerCalls:     1,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userRunnerCalls = 0
			runnerCalls = 0
			d := Discovery{
				userCommandRunner: test.fakeUserRunner,
				commandRunner:     test.fakeRunner,
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
			if userRunnerCalls != test.wantUserRunnerCalls {
				t.Errorf("discoverDatabaseSID() userRunnerCalls = %d, want %d", userRunnerCalls, test.wantUserRunnerCalls)
			}
			if runnerCalls != test.wantRunnerCalls {
				t.Errorf("discoverDatabaseSID() runnerCalls = %d, want %d", runnerCalls, test.wantRunnerCalls)
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
			ResourceKind:     "test",
			ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
			RelatedResources: []string{"other/resource"},
			UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
		},
		want: &workloadmanager.SapDiscoveryResource{
			ResourceURI:      "test/uri",
			ResourceKind:     "test",
			ResourceType:     "COMPUTE",
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
		name: "discoveryComponentToInsightComponent",
		comp: &spb.SapDiscovery_Component{
			HostProject: "test/project",
			Sid:         "SID",
			Resources: []*spb.SapDiscovery_Resource{{
				ResourceUri:      "test/uri",
				ResourceKind:     "test",
				ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
				RelatedResources: []string{"other/resource"},
				UpdateTime:       timestamppb.New(time.Unix(1682955911, 0)),
			}},
		},
		want: &workloadmanager.SapDiscoveryComponent{
			HostProject: "test/project",
			Sid:         "SID",
			Resources: []*workloadmanager.SapDiscoveryResource{{
				ResourceURI:      "test/uri",
				ResourceKind:     "test",
				ResourceType:     "COMPUTE",
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
					ResourceKind:     "test",
					ResourceType:     spb.SapDiscovery_Resource_COMPUTE,
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
						ResourceKind:     "test",
						ResourceType:     "COMPUTE",
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
