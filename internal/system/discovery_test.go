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
	cfgpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
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
)

var (
	defaultCloudProperties = &instancepb.CloudProperties{
		InstanceName: defaultInstanceName,
		ProjectId:    defaultProjectID,
		Zone:         defaultZone,
	}
)

func TestStartSAPSystemDiscovery(t *testing.T) {
	tests := []struct {
		name   string
		config *cfgpb.Configuration
		want   bool
	}{{
		name: "succeeds",
		config: &cfgpb.Configuration{
			CollectionConfiguration: &cfgpb.CollectionConfiguration{
				SapSystemDiscovery: true,
			},
			CloudProperties: defaultCloudProperties,
		},
		want: true,
	}, {
		name: "failsDueToConfig",
		config: &cfgpb.Configuration{
			CollectionConfiguration: &cfgpb.CollectionConfiguration{
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

			got := StartSAPSystemDiscovery(context.Background(), test.config, gceService)
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
		want       []*spb.Resource
	}{{
		name: "justInstance",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "some/resource/uri",
			}},
			GetInstanceErr: []error{nil},
		},
		want: []*spb.Resource{{
			ResourceType: spb.Resource_COMPUTE,
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
			less := func(a, b *spb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(got, test.want, protocmp.Transform(), protocmp.IgnoreFields(&spb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&spb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
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
		testIR     *spb.Resource
		want       []*spb.Resource
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{&spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  "no/source/disk",
		}, {
			ResourceType: spb.Resource_COMPUTE,
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType: spb.Resource_COMPUTE,
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{&spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
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
			less := func(a, b *spb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(got, test.want, protocmp.Transform(), protocmp.IgnoreFields(&spb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&spb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
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
		testIR     *spb.Resource
		want       []*spb.Resource
	}{{
		name: "instanceWithNetworkInterface",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{SelfLink: "some/address/uri"}},
			}},
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeSubnetwork",
			ResourceUri:      "regions/test-region/subnet",
			RelatedResources: []string{"some/address/uri"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "PublicAddress",
			ResourceUri:  "1.2.3.4",
		}, {
			ResourceType: spb.Resource_COMPUTE,
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"noregionsubnet"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
			ResourceUri:  "noregionsubnet",
		}},
	}, {
		name: "errGetAddressByIP",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{nil},
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
			ResourceUri:  "regions/test-region/subnet",
		}},
	}, {
		name: "noAddressFound",
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{}},
			GetAddressByIPErr:  []error{nil},
		},
		testCI: &compute.Instance{
			SelfLink: "some/resource/uri",
			NetworkInterfaces: []*compute.NetworkInterface{{
				Network:    "network",
				Subnetwork: "regions/test-region/subnet",
				NetworkIP:  "10.2.3.4",
			}},
		},
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeNetwork",
			ResourceUri:      "network",
			RelatedResources: []string{"regions/test-region/subnet"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
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
			less := func(a, b *spb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(got, test.want, protocmp.Transform(), protocmp.IgnoreFields(&spb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&spb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
				t.Errorf("discoverNetworks() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverLoadBalancer(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		fakeExists commandlineexecutor.CommandExistsRunner
		fakeRunner commandlineexecutor.CommandRunner
		want       []*spb.Resource
	}{{
		name:       "hasLoadBalancer",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "hasLoadBalancerPcs",
		fakeExists: func(f string) bool { return f == "pcs" },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
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
			GetAddressByIPResp: []*compute.AddressList{nil},
			GetAddressByIPErr:  []error{errors.New("Some API error")},
		},
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
	}, {
		name:       "noAddressFound",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{},
			}},
			GetAddressByIPErr: []error{nil},
		},
	}, {
		name:       "addressNotInUse",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{SelfLink: "some/compute/address"}},
			}},
			GetAddressByIPErr: []error{nil},
		},
		want: []*spb.Resource{{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:       "addressUserNotForwardingRule",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/notFWR/some-name"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
		},
		want: []*spb.Resource{{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:       "errorGettingForwardingRule",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr:     []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{nil},
			GetForwardingRuleErr:  []error{errors.New("Some API error")},
		},
		want: []*spb.Resource{{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  "some/compute/address",
		}},
	}, {
		name:       "fwrNoBackendService",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			}},
			GetForwardingRuleErr: []error{nil},
		},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
			},
		}},
	}, {
		name:       "bakendServiceNoRegion",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/zegion/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
		},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}},
	}, {
		name:       "errorGettingBackendService",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr:          []error{nil},
			GetRegionalBackendServiceResp: []*compute.BackendService{nil},
			GetRegionalBackendServiceErr:  []error{errors.New("Some API error")},
		},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeForwardingRule",
			ResourceUri:      "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{"some/compute/address"},
		}},
	}, {
		name:       "backendGroupNotInstanceGroup",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "backendGroupNoZone",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "errorGettingInstanceGroup",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "listInstanceGroupInstancesError",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "noInstanceNameInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "noProjectInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstance",
			ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
		}},
	}, {
		name:       "noZoneInInstancesItem",
		fakeExists: func(string) bool { return true },
		fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
		gceService: &fake.TestGCE{
			GetAddressByIPResp: []*compute.AddressList{{
				Items: []*compute.Address{{
					SelfLink: "some/compute/address",
					Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}},
			}},
			GetAddressByIPErr: []error{nil},
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
			}},
			GetForwardingRuleErr: []error{nil},
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
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeAddress",
			ResourceUri:      "some/compute/address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeForwardingRule",
			ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
			RelatedResources: []string{
				"some/compute/address",
				"projects/test-project/regions/test-region/backendServices/test-bes",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeBackendService",
			ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
			RelatedResources: []string{
				"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
				"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			},
		}, {
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
		}, {
			ResourceType:     spb.Resource_COMPUTE,
			ResourceKind:     "ComputeInstanceGroup",
			ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
			RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
		}, {
			ResourceType: spb.Resource_COMPUTE,
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
			got := d.discoverLoadBalancer(defaultCloudProperties)
			less := func(a, b *spb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), protocmp.IgnoreFields(&spb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&spb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
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
		testIR         *spb.Resource
		want           []*spb.Resource
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
		testIR: &spb.Resource{ResourceUri: "some/resource/uri"},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_STORAGE,
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
		testIR: &spb.Resource{
			ResourceUri:      "some/resource/uri",
			RelatedResources: []string{},
		},
		want: []*spb.Resource{{
			ResourceType:     spb.Resource_STORAGE,
			ResourceKind:     "ComputeFilestore",
			ResourceUri:      "some/filestore/uri",
			RelatedResources: []string{"some/resource/uri"},
		}, {
			ResourceType:     spb.Resource_STORAGE,
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
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
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
