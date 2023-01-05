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
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	systempb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

var (
	defaultCloudProperties = &instancepb.CloudProperties{
		InstanceName: "test-instance-id",
		ProjectId:    "test-project-id",
		Zone:         "test-zone",
	}
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

func TestDiscoverResources(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		fakeExists commandlineexecutor.CommandExistsRunner
		fakeRunner commandlineexecutor.CommandRunner
		want       []*systempb.Resource
	}{
		{
			name: "justInstance",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
				}},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{&systempb.Resource{
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeInstance",
				ResourceUri:  "some/resource/uri",
			},
			},
		}, {
			name: "instanceWithDisks",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					Disks: []*compute.AttachedDisk{{
						Source:     "",
						DeviceName: "noSourceDisk",
					}, {
						Source:     "",
						DeviceName: "noSourceDisk2",
					},
					},
				}},
				GetInstanceErr: []error{nil},
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
			want: []*systempb.Resource{&systempb.Resource{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"no/source/disk", "no/source/disk2"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeDisk",
				ResourceUri:  "no/source/disk",
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeDisk",
				ResourceUri:  "no/source/disk2",
			},
			},
		}, {
			name: "getDiskFromSource",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					Disks: []*compute.AttachedDisk{{
						Source:     "source/for/disk1",
						DeviceName: "nonSourceName",
					}},
				}},
				GetInstanceErr: []error{nil},
				GetDiskResp: []*compute.Disk{{
					SelfLink: "uri/for/disk1",
				}},
				GetDiskErr: []error{nil},
				GetDiskArgs: []*fake.GetDiskArguments{{
					Project:  "test-project-id",
					Zone:     "test-zone",
					DiskName: "disk1",
				}},
			},
			want: []*systempb.Resource{{
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeDisk",
				ResourceUri:  "uri/for/disk1",
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"uri/for/disk1"},
			}},
		}, {
			name: "instanceWithNetworkInterface",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					NetworkInterfaces: []*compute.NetworkInterface{{
						Network:    "network",
						Subnetwork: "regions/test-region/subnet",
						AccessConfigs: []*compute.AccessConfig{{
							NatIP: "1.2.3.4",
						}},
						NetworkIP: "10.2.3.4",
					}},
				},
				},
				GetInstanceErr: []error{nil},
				GetAddressByIPResp: []*compute.AddressList{{
					Items: []*compute.Address{{
						SelfLink: "some/address/uri",
					}},
				}},
				GetAddressByIPArgs: []*fake.GetAddressByIPArguments{{
					Project: "test-project-id",
					Region:  "test-region",
					Address: "10.2.3.4",
				}},
				GetAddressByIPErr: []error{nil},
			},
			want: []*systempb.Resource{{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"network", "regions/test-region/subnet", "1.2.3.4", "some/address/uri"},
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeNetwork",
				ResourceUri:      "network",
				RelatedResources: []string{"regions/test-region/subnet"},
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeSubnetwork",
				ResourceUri:      "regions/test-region/subnet",
				RelatedResources: []string{"some/address/uri"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "PublicAddress",
				ResourceUri:  "1.2.3.4",
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeAddress",
				ResourceUri:  "some/address/uri",
			},
			},
		}, {
			name: "noInstanceInfo",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{nil},
				GetInstanceErr:  []error{errors.New("Instance not found")},
			},
			want: nil,
		},
		{
			name: "errGettingDisk",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					Disks: []*compute.AttachedDisk{{
						Source:     "",
						DeviceName: "noSourceDisk",
					}, {
						Source:     "",
						DeviceName: "noSourceDisk2",
					},
					},
				}},
				GetInstanceErr: []error{nil},
				GetDiskErr:     []error{errors.New("Disk not found"), nil},
				GetDiskResp: []*compute.Disk{nil, {
					SelfLink: "uri/for/disk2",
				}},
			},
			want: []*systempb.Resource{&systempb.Resource{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"uri/for/disk2"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeDisk",
				ResourceUri:  "uri/for/disk2",
			},
			},
		}, {
			name: "noSubnetRegion",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					NetworkInterfaces: []*compute.NetworkInterface{{
						Network:    "network",
						Subnetwork: "noregionsubnet",
						NetworkIP:  "10.2.3.4",
					}},
				}},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"network", "noregionsubnet"},
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeNetwork",
				ResourceUri:      "network",
				RelatedResources: []string{"noregionsubnet"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeSubnetwork",
				ResourceUri:  "noregionsubnet",
			},
			},
		}, {
			name: "errGetAddressByIP",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					NetworkInterfaces: []*compute.NetworkInterface{{
						Network:    "network",
						Subnetwork: "regions/test-region/subnet",
						NetworkIP:  "10.2.3.4",
					}},
				}},
				GetInstanceErr:     []error{nil},
				GetAddressByIPResp: []*compute.AddressList{nil},
				GetAddressByIPArgs: []*fake.GetAddressByIPArguments{{
					Project: "test-project-id",
					Region:  "test-region",
					Address: "10.2.3.4",
				}},
				GetAddressByIPErr: []error{errors.New("Error listing addresses")},
			},
			want: []*systempb.Resource{{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"network", "regions/test-region/subnet"},
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeNetwork",
				ResourceUri:      "network",
				RelatedResources: []string{"regions/test-region/subnet"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeSubnetwork",
				ResourceUri:  "regions/test-region/subnet",
			},
			},
		}, {
			name: "noAddressFound",
			gceService: &fake.TestGCE{
				GetInstanceResp: []*compute.Instance{{
					SelfLink: "some/resource/uri",
					NetworkInterfaces: []*compute.NetworkInterface{{
						Network:    "network",
						Subnetwork: "regions/test-region/subnet",
						NetworkIP:  "10.2.3.4",
					}},
				}},
				GetInstanceErr:     []error{nil},
				GetAddressByIPResp: []*compute.AddressList{{}},
				GetAddressByIPErr:  []error{nil},
			},
			want: []*systempb.Resource{{
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeInstance",
				ResourceUri:      "some/resource/uri",
				RelatedResources: []string{"network", "regions/test-region/subnet"},
			}, {
				ResourceType:     systempb.Resource_COMPUTE,
				ResourceKind:     "ComputeNetwork",
				ResourceUri:      "network",
				RelatedResources: []string{"regions/test-region/subnet"},
			}, {
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeSubnetwork",
				ResourceUri:  "regions/test-region/subnet",
			},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService:    test.gceService,
				commandRunner: test.fakeRunner,
				exists:        test.fakeExists,
			}
			got := d.discoverResources("test-project-id", "test-zone", "instanceName")
			less := func(a, b *systempb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(got, test.want, protocmp.Transform(), protocmp.IgnoreFields(&systempb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&systempb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
				t.Errorf("discoverResources() mismatch (-want, +got):\n%s", diff)
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
		want       []*systempb.Resource
	}{
		{
			name:       "hasLoadBalancer",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone/instances/test-instance-id",
							},
						},
					}, {
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{nil, nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "hasLoadBalancerPcs",
			fakeExists: func(f string) bool { return f == "pcs" },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone/instances/test-instance-id",
							},
						},
					}, {
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{nil, nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance-id"},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "noClusterCommands",
			gceService: &fake.TestGCE{},
			fakeExists: func(string) bool { return false },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			want:       nil,
		}, {
			name:       "discoverClusterCommandError",
			gceService: &fake.TestGCE{},
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return "", "", errors.New("Some command error") },
			want:       nil,
		}, {
			name:       "noVipInClusterConfig",
			gceService: &fake.TestGCE{},
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) {
				return "line1\nline2", "", nil
			},
			want: nil,
		}, {
			name:       "noAddressInClusterConfig",
			gceService: &fake.TestGCE{},
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) {
				return "rsc_vip_int-primary IPaddr2\nnot-an-adress", "", nil
			},
			want: nil,
		}, {
			name: "errorListingAddresses",
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{nil},
				GetAddressByIPErr:  []error{errors.New("Some API error")},
			},
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			want:       nil,
		}, {
			name:       "noAddressFound",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{},
					},
				},
				GetAddressByIPErr: []error{nil},
			},
			want: nil,
		}, {
			name:       "addressNotInUse",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
			},
			want: []*systempb.Resource{{
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeAddress",
				ResourceUri:  "some/compute/address",
			}},
		}, {
			name:       "addressUserNotForwardingRule",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/notFWR/some-name"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
			},
			want: []*systempb.Resource{{
				ResourceType: systempb.Resource_COMPUTE,
				ResourceKind: "ComputeAddress",
				ResourceUri:  "some/compute/address",
			}},
		}, {
			name: "errorGettingForwardingRule",

			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr:     []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{nil},
				GetForwardingRuleErr:  []error{errors.New("Some API error")},
			},
			want: []*systempb.Resource{
				{
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeAddress",
					ResourceUri:  "some/compute/address",
				},
			},
		}, {
			name:       "fwrNoBackendService",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink: "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					},
				},
				GetForwardingRuleErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
					},
				},
			},
		}, {
			name:       "bakendServiceNoRegion",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/zegion/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
					},
				},
			},
		}, {
			name:       "errorGettingBackendService",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr:          []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{nil},
				GetRegionalBackendServiceErr:  []error{errors.New("Some API error")},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
					},
				},
			},
		}, {
			name:       "backendGroupNotInstanceGroup",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/notIG/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{nil, nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "backendGroupNoZone",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/fones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{nil, nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "errorGettingInstanceGroup",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{nil,
					{
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{errors.New("Some API error"), nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{nil, nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "listInstanceGroupInstancesError",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{nil,
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstanceGroup",
					ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "noInstanceNameInInstancesItem",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone/unstances/test-instance-id1",
							},
						},
					},
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstanceGroup",
					ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "noProjectInInstancesItem",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "zrojects/test-project/zones/test-zone/instances/test-instance-id1",
							},
						},
					},
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstanceGroup",
					ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		}, {
			name:       "noZoneInInstancesItem",
			fakeExists: func(string) bool { return true },
			fakeRunner: func(string, string) (string, string, error) { return defaultClusterOutput, "", nil },
			gceService: &fake.TestGCE{
				GetAddressByIPResp: []*compute.AddressList{
					{
						Items: []*compute.Address{
							{
								SelfLink: "some/compute/address",
								Users:    []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
							},
						},
					},
				},
				GetAddressByIPErr: []error{nil},
				GetForwardingRuleResp: []*compute.ForwardingRule{
					{
						SelfLink:       "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						BackendService: "projects/test-project/regions/test-region/backendServices/test-bes",
					},
				},
				GetForwardingRuleErr: []error{nil},
				GetRegionalBackendServiceResp: []*compute.BackendService{
					{
						SelfLink: "projects/test-project/regions/test-region/backendServices/test-bes",
						Backends: []*compute.Backend{
							{
								Group: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
							}, {
								Group: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
							},
						},
					},
				},
				GetRegionalBackendServiceErr: []error{nil},
				GetInstanceGroupResp: []*compute.InstanceGroup{
					{
						SelfLink: "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
					}, {
						SelfLink: "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				},
				GetInstanceGroupErr: []error{nil, nil},
				ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/ones/test-zone/instances/test-instance-id1",
							},
						},
					},
					{
						Items: []*compute.InstanceWithNamedPorts{
							{
								Instance: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
							},
						},
					},
				},
				ListInstanceGroupInstancesErr: []error{errors.New("Some API Error"), nil},
				GetInstanceResp: []*compute.Instance{
					{
						SelfLink: "projects/test-project/zones/test-zone2/instances/test-instance-id2",
					},
				},
				GetInstanceErr: []error{nil},
			},
			want: []*systempb.Resource{
				{
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeAddress",
					ResourceUri:      "some/compute/address",
					RelatedResources: []string{"projects/test-project/zones/test-zone/forwardingRules/test-fwr"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeForwardingRule",
					ResourceUri:  "projects/test-project/zones/test-zone/forwardingRules/test-fwr",
					RelatedResources: []string{
						"some/compute/address",
						"projects/test-project/regions/test-region/backendServices/test-bes",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeBackendService",
					ResourceUri:  "projects/test-project/regions/test-region/backendServices/test-bes",
					RelatedResources: []string{
						"projects/test-project/zones/test-zone/forwardingRules/test-fwr",
						"projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
						"projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstanceGroup",
					ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/instancegroup1",
				}, {
					ResourceType:     systempb.Resource_COMPUTE,
					ResourceKind:     "ComputeInstanceGroup",
					ResourceUri:      "projects/test-project/zones/test-zone2/instanceGroups/instancegroup2",
					RelatedResources: []string{"projects/test-project/zones/test-zone2/instances/test-instance-id2"},
				}, {
					ResourceType: systempb.Resource_COMPUTE,
					ResourceKind: "ComputeInstance",
					ResourceUri:  "projects/test-project/zones/test-zone2/instances/test-instance-id2",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.gceService.T = t
			d := Discovery{
				gceService:    test.gceService,
				commandRunner: test.fakeRunner,
				exists:        test.fakeExists,
			}
			got := d.discoverLoadBalancer(defaultCloudProperties)
			less := func(a, b *systempb.Resource) bool { return a.String() < b.String() }
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), protocmp.IgnoreFields(&systempb.Resource{}, "last_updated"), protocmp.SortRepeatedFields(&systempb.Resource{}, "related_resources"), cmpopts.SortSlices(less)); diff != "" {
				t.Errorf("discoverResources() mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
