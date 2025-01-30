/*
Copyright 2023 Google LLC

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

package clouddiscovery

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/file/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/fake"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

const (
	defaultRegion    = "test-region"
	defaultProjectID = "test-project-id"
	defaultZone      = "test-zone"
)

func resourceLess(a, b *spb.SapDiscovery_Resource) bool {
	return a.String() < b.String()
}

func makeZonalURI(project, zone, resourceType, resourceName string) string {
	return fmt.Sprintf("projects/%s/zones/%s/%s/%s", project, zone, resourceType, resourceName)
}

func makeRegionalURI(project, region, resourceType, resourceName string) string {
	return fmt.Sprintf("projects/%s/regions/%s/%s/%s", project, region, resourceType, resourceName)
}

func makeGlobalURI(project, resourceType, resourceName string) string {
	return fmt.Sprintf("projects/%s/global/%s/%s", project, resourceType, resourceName)
}

var (
	resourceDiffOpts = []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "update_time"),
		protocmp.SortRepeatedFields(&spb.SapDiscovery_Resource{}, "related_resources"),
	}
	resourceListDiffOpts = append(resourceDiffOpts,
		cmpopts.SortSlices(resourceLess))
	defaultCloudProperties = &ipb.CloudProperties{
		ProjectId:  defaultProjectID,
		InstanceId: "test-instance",
		Zone:       defaultZone,
		Region:     defaultRegion,
	}
	toDiscoverListOpts = append(resourceDiffOpts,
		cmp.AllowUnexported(toDiscover{}),
		protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "related_resources"),
		cmpopts.EquateEmpty(),
		cmpopts.SortSlices(toDiscoverLess),
	)
)

func TestDiscoverComputeResources(t *testing.T) {
	tests := []struct {
		name       string
		parent     *spb.SapDiscovery_Resource
		hostList   []string
		gceService *fake.TestGCE
		want       []*spb.SapDiscovery_Resource
		wantParent *spb.SapDiscovery_Resource
	}{{
		name:     "discoverEmptyList",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/disks/test-disk"},
		hostList: []string{},
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "test-disk",
			}},
			GetDiskErr: []error{nil},
		},
		want: nil,
	}, {
		name:     "discoverSingleItem",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/disks/test-disk"},
		hostList: []string{"projects/test-project/zones/test-zone/disks/other-disk"},
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "projects/test-project/zones/test-zone/disks/other-disk",
			}},
			GetDiskErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:      "projects/test-project/zones/test-zone/disks/other-disk",
			RelatedResources: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
		}},
	}, {
		name:     "discoverMultipleItems",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/disks/test-disk"},
		hostList: []string{"projects/test-project/zones/test-zone/disks/other-disk", "projects/test-project/regions/test-region/filestores/test-filestore"},
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "projects/test-project/zones/test-zone/disks/other-disk",
			}},
			GetDiskErr: []error{nil},
			GetFilestoreResp: []*file.Instance{{
				Name: "projects/test-project/regions/test-region/filestores/test-filestore",
			}},
			GetFilestoreErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:      "projects/test-project/zones/test-zone/disks/other-disk",
			RelatedResources: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
		}, {
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "projects/test-project/regions/test-region/filestores/test-filestore",
			RelatedResources: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
		}},
	}, {
		name:     "continueAfterError",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/instances/test-instance"},
		hostList: []string{"projects/test-project/zones/test-zone/disks/test-disk", "projects/test-project/regions/test-region/filestores/test-filestore"},
		gceService: &fake.TestGCE{
			GetDiskResp:      []*compute.Disk{nil},
			GetDiskErr:       []error{cmpopts.AnyError},
			GetFilestoreResp: []*file.Instance{{Name: "projects/test-project/regions/test-region/filestores/test-filestore"}},
			GetFilestoreErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "projects/test-project/regions/test-region/filestores/test-filestore",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		}},
	}, {
		name:     "skipEmptyName",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/instances/test-instance"},
		hostList: []string{"", "projects/test-project/regions/test-region/filestores/test-filestore"},
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{{Name: "projects/test-project/regions/test-region/filestores/test-filestore"}},
			GetFilestoreErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:      "projects/test-project/regions/test-region/filestores/test-filestore",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		}},
	}, {
		name:     "parentNameDiffersFromResourceURI",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/instances/test-hostname"},
		hostList: []string{},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "test-instance",
			}},
			GetInstanceErr: []error{nil},
		},
		want: nil,
	}, {
		name:     "skipsAlreadyDiscoveredResource",
		parent:   &spb.SapDiscovery_Resource{ResourceUri: "projects/test-project/zones/test-zone/instances/test-instance"},
		hostList: []string{"projects/test-project/regions/test-region/addresses/test-address", "test-instance"},
		gceService: &fake.TestGCE{
			GetAddressResp: []*compute.Address{{
				SelfLink: "projects/test-project/regions/test-region/addresses/test-address",
				Users:    []string{"test-instance"},
			}},
			GetAddressErr: []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:      "projects/test-project/regions/test-region/addresses/test-address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		}},
	}, {
		name: "parentHasVirtualHostname",
		parent: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:  "projects/test-project/zones/test-zone/instanceGroups/test-instance-group",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "virtual-hostname",
			},
		},
		hostList: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{SelfLink: "projects/test-project/zones/test-zone/instances/test-instance"}},
			GetInstanceErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "virtual-hostname",
			},
			RelatedResources: []string{"projects/test-project/zones/test-zone/instanceGroups/test-instance-group"},
		}},
	}, {
		name:     "applyVirtualHostname",
		hostList: []string{"instances/hostname"},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{SelfLink: "projects/test-project/zones/test-zone/instances/test-instance"}},
			GetInstanceErr:  []error{nil},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "instances/hostname",
			},
		}},
	}, {
		name:     "instanceInheritsHostnameFromParent",
		hostList: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{SelfLink: "projects/test-project/zones/test-zone/instances/test-instance"}},
			GetInstanceErr:  []error{nil},
		},
		parent: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
			RelatedResources: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
		}},
		wantParent: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:      "projects/test-project/zones/test-zone/addresses/test-address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		},
	}, {
		name:     "noninstanceDoesNotInheritHostname",
		hostList: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{SelfLink: "projects/test-project/zones/test-zone/disks/test-disk"}},
			GetDiskErr:  []error{nil},
		},
		parent: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/zones/test-zone/addresses/test-address",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:      "projects/test-project/zones/test-zone/disks/test-disk",
			RelatedResources: []string{"projects/test-project/zones/test-zone/addresses/test-address"},
		}},
		wantParent: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:      "projects/test-project/zones/test-zone/addresses/test-address",
			RelatedResources: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
		},
	}, {
		name:     "instanceDoesNotInheritHostnameFromInstance",
		hostList: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{SelfLink: "projects/test-project/zones/test-zone/instances/test-instance"}},
			GetInstanceErr:  []error{nil},
		},
		parent: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance-parent",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
		},
		want: []*spb.SapDiscovery_Resource{{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			RelatedResources:   []string{"projects/test-project/zones/test-zone/instances/test-instance-parent"},
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		}},
		wantParent: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:      "projects/test-project/zones/test-zone/instances/test-instance-parent",
			RelatedResources: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "some-hostname",
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				HostResolver: func(string) ([]string, error) { return []string{}, nil },
				GceService:   test.gceService,
			}
			got := c.DiscoverComputeResources(context.Background(), test.parent, "", test.hostList, defaultCloudProperties)
			if diff := cmp.Diff(test.want, got, resourceListDiffOpts...); diff != "" {
				t.Errorf("discoverComputeResources() returned unexpected diff (-want +got):\n%s", diff)
			}
			if test.wantParent != nil {
				if diff := cmp.Diff(test.wantParent, test.parent, resourceDiffOpts...); diff != "" {
					t.Errorf("discoverComputeResources() returned unexpected diff on parent (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestDiscoverResourceCache(t *testing.T) {
	tests := []struct {
		name                 string
		firstHost            toDiscover
		secondHost           toDiscover
		gceService           *fake.TestGCE
		cache                map[string]cacheEntry
		firstResolver        func(string) ([]string, error)
		secondResolver       func(string) ([]string, error)
		firstWant            *spb.SapDiscovery_Resource
		firstWantToDiscover  []toDiscover
		firstWantErr         error
		secondWant           *spb.SapDiscovery_Resource
		secondWantToDiscover []toDiscover
		secondWantErr        error
	}{{
		name:           "discoverUsesCacheOnSecondCall",
		firstHost:      toDiscover{name: "projects/test-project/zones/test-zone/instances/test-instance"},
		secondHost:     toDiscover{name: "projects/test-project/zones/test-zone/instances/test-instance"},
		firstResolver:  func(string) ([]string, error) { return []string{}, nil },
		secondResolver: func(string) ([]string, error) { return []string{}, nil },
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "projects/test-project/zones/test-zone/disks/test-disk",
				}},
			}, nil},
			GetInstanceErr: []error{nil, cmpopts.AnyError},
		},
		firstWant: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		},
		firstWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			},
		}},
		secondWant: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
		},
		secondWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}},
	}, {
		name:           "discoverUsesCacheOnSecondCallWithDifferentName",
		firstHost:      toDiscover{name: "test-instance"},
		secondHost:     toDiscover{name: "projects/test-project/zones/test-zone/instances/test-instance"},
		firstResolver:  func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		secondResolver: func(string) ([]string, error) { return []string{}, cmpopts.AnyError },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/instances/test-instance", ""},
			GetURIForIPErr:  []error{nil, cmpopts.AnyError},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "projects/test-project/zones/test-zone/disks/test-disk",
				}},
			}, nil},
			GetInstanceErr: []error{nil, cmpopts.AnyError},
		},
		firstWant: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "test-instance",
			},
		},
		firstWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-instance",
				},
			},
		}},
		secondWant: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "test-instance",
			},
		},
		secondWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-instance",
				},
			},
		}},
	}, {
		name:          "discoverUsesCacheOnSecondCallWithDifferentAddress",
		firstHost:     toDiscover{name: "test-instance"},
		secondHost:    toDiscover{name: "alternate-hostname"},
		firstResolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		secondResolver: func(string) ([]string, error) {
			return []string{"5.6.7.8"}, nil
		},
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/instances/test-instance", "projects/test-project/zones/test-zone/instances/test-instance", ""},
			GetURIForIPErr:  []error{nil, nil, cmpopts.AnyError},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "projects/test-project/zones/test-zone/disks/test-disk",
				}},
			}, nil},
			GetInstanceErr: []error{nil, cmpopts.AnyError},
		},
		firstWant: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "test-instance",
			},
		},
		firstWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-instance",
				},
			},
		}},
		secondWant: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "test-instance",
			},
		},
		secondWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-instance",
				},
			},
		}},
	}, {
		name:           "discoverRefreshesOldCache",
		firstHost:      toDiscover{name: "projects/test-project/zones/test-zone/instances/test-instance"},
		secondHost:     toDiscover{name: "projects/test-project/zones/test-zone/instances/test-instance"},
		firstResolver:  func(string) ([]string, error) { return []string{}, nil },
		secondResolver: func(string) ([]string, error) { return []string{}, nil },
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "projects/test-project/zones/test-zone/disks/test-disk2",
				}},
			}},
			GetInstanceErr: []error{nil},
		},
		cache: map[string]cacheEntry{
			"projects/test-project/zones/test-zone/instances/test-instance": {
				res: &spb.SapDiscovery_Resource{
					ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
					ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
					ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
					UpdateTime:   timestamppb.New(time.Unix(0, 0)),
				},
				related: []toDiscover{
					{
						name: "test-other-resource",
						parent: &spb.SapDiscovery_Resource{
							ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
							ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
							ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
						},
					},
				},
			},
		},
		firstWant: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		},
		firstWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}},
		secondWant: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		},
		secondWantToDiscover: []toDiscover{{
			name: "projects/test-project/zones/test-zone/disks/test-disk2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run(test.name, func(t *testing.T) {
				c := CloudDiscovery{
					GceService:    test.gceService,
					resourceCache: test.cache,
				}

				c.HostResolver = test.firstResolver
				got, gotToDiscover, err := c.discoverResource(context.Background(), test.firstHost, defaultProjectID)
				if diff := cmp.Diff(test.firstWant, got, resourceDiffOpts...); diff != "" {
					t.Errorf("discoverResource() returned unexpected diff on first call (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(test.firstWantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
					t.Errorf("discoverResource() returned unexpected toDiscover diff on first call (-want +got):\n%s", diff)
				}
				if !cmp.Equal(test.firstWantErr, err, cmpopts.EquateErrors()) {
					t.Errorf("discoverResource() returned unexpected error on first call: %v", err)
				}

				c.HostResolver = test.secondResolver
				got, gotToDiscover, err = c.discoverResource(context.Background(), test.secondHost, defaultProjectID)
				if diff := cmp.Diff(test.secondWant, got, resourceDiffOpts...); diff != "" {
					t.Errorf("discoverResource() returned unexpected diff on second call (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(test.secondWantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
					t.Errorf("discoverResource() returned unexpected toDiscover diff on second call (-want +got):\n%s", diff)
				}
				if !cmp.Equal(test.secondWantErr, err, cmpopts.EquateErrors()) {
					t.Errorf("discoverResource() returned unexpected error on second call: %v", err)
				}
			})
		})
	}
}

func TestDiscoverResource(t *testing.T) {
	tests := []struct {
		name           string
		host           toDiscover
		project        string
		gceService     *fake.TestGCE
		resolver       func(string) ([]string, error)
		want           *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name:    "discoverFromHostname",
		host:    toDiscover{name: "test-host"},
		project: defaultProjectID,
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/instances/test-instance"},
			GetURIForIPErr:  []error{nil},
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "test-disk",
				}, {
					Source: "test-disk2",
				}},
				NetworkInterfaces: []*compute.NetworkInterface{{
					Network:    "test-network",
					Subnetwork: "test-subnetwork",
					NetworkIP:  "1.2.3.4",
				}, {
					Network:    "test-network2",
					Subnetwork: "test-subnetwork2",
					NetworkIP:  "5.6.7.8",
				}},
			}},
			GetInstanceErr: []error{nil},
		},
		resolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
				VirtualHostname: "test-host",
			},
		},
		wantToDiscover: []toDiscover{{
			name: "test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name: "test-disk2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "test-network",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "test-network2",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "test-subnetwork",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "test-subnetwork2",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "1.2.3.4",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}, {
			name:    "5.6.7.8",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:  "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
					VirtualHostname: "test-host",
				},
			},
		}},
	}, {
		name:     "resolverError",
		resolver: func(string) ([]string, error) { return nil, fmt.Errorf("error") },
		wantErr:  cmpopts.AnyError,
	}, {
		name: "useProjectFromParent",
		host: toDiscover{
			name: "test-host",
			parent: &spb.SapDiscovery_Resource{
				ResourceUri: "projects/parent-project/zones/test-zone/instances/test-instance",
			}},
		project:  "other-project",
		resolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
			GetURIForIPErr:  []error{nil},
			GetURIForIPArgs: []*fake.GetURIForIPArguments{{
				Project:    "parent-project",
				Subnetwork: "",
				IP:         "1.2.3.4",
			}},
			GetDiskResp: []*compute.Disk{{SelfLink: "test-disk"}},
			GetDiskErr:  []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:      "test-disk",
			RelatedResources: []string{"projects/parent-project/zones/test-zone/instances/test-instance"},
		},
	}, {
		name:     "getURIError",
		host:     toDiscover{name: "test-host"},
		resolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{GetURIForIPResp: []string{""},
			GetURIForIPErr: []error{fmt.Errorf("some error")},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name:     "discoverResourceForURISuccess",
		host:     toDiscover{name: "projects/test-project/zones/test-zone/filestores/test-filestore"},
		resolver: func(string) ([]string, error) { return []string{}, nil },
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{{
				Name: "projects/test-project/zones/test-zone/filestores/test-filestore",
			}},
			GetFilestoreErr: []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:  "projects/test-project/zones/test-zone/filestores/test-filestore",
		},
	}, {
		name:     "discoverResourceForURIFailure",
		host:     toDiscover{name: "projects/test-project/zones/test-zone/filestores/test-filestore"},
		resolver: func(string) ([]string, error) { return []string{}, nil },
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{nil},
			GetFilestoreErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "usesNetwork",
		host: toDiscover{
			name:    "projects/test-project/zones/test-zone/filestores/test-filestore",
			network: "test-network",
		},
		project:  "test-project",
		resolver: func(string) ([]string, error) { return []string{"1.2.3.4"}, nil },
		gceService: &fake.TestGCE{
			GetURIForIPResp: []string{"projects/test-project/zones/test-zone/disks/test-disk"},
			GetURIForIPErr:  []error{nil},
			GetURIForIPArgs: []*fake.GetURIForIPArguments{{
				Project:    "test-project",
				Subnetwork: "test-subnetwork",
				IP:         "1.2.3.4",
			}},
			GetDiskResp: []*compute.Disk{{SelfLink: "test-disk"}},
			GetDiskErr:  []error{nil},
			GetNetworkResp: []*compute.Network{{
				Name:        "test-network",
				SelfLink:    "projects/test-project/global/networks/test-network",
				Subnetworks: []string{"test-subnetwork"},
			}},
			GetNetworkErr: []error{nil},
			GetSubnetworkResp: []*compute.Subnetwork{{
				Name:        "test-subnetwork",
				SelfLink:    "projects/test-project/region/test-region/subnetworks/test-subnetwork",
				IpCidrRange: "1.2.3.0/24",
			}},
			GetSubnetworkErr: []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "test-disk",
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService:   test.gceService,
				HostResolver: test.resolver,
			}
			if test.gceService != nil {
				test.gceService.T = t
			}
			got, gotToDiscover, err := c.discoverResource(context.Background(), test.host, test.project)
			if diff := cmp.Diff(test.want, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverResource() returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverResource() returned unexpected toDiscover diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverResource() returned unexpected error: %v", err)
			}
		})
	}
}

func toDiscoverLess(a, b toDiscover) bool {
	return a.name < b.name
}

func TestDiscoverResourceForURI(t *testing.T) {
	tests := []struct {
		name           string
		uri            string
		gceService     *fake.TestGCE
		wantResource   *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "discoverInstanceSuccess",
		uri:  "projects/test-project/zones/test-zone/instances/test-instance",
		gceService: &fake.TestGCE{GetInstanceResp: []*compute.Instance{{
			SelfLink: "projects/test-project/zones/test-zone/instances/test-instance",
			Disks: []*compute.AttachedDisk{{
				Source: "test-disk",
			}, {
				Source: "test-disk2",
			}},
			NetworkInterfaces: []*compute.NetworkInterface{{
				Network:    "test-network",
				Subnetwork: "test-subnetwork",
				NetworkIP:  "1.2.3.4",
			}, {
				Network:    "test-network2",
				Subnetwork: "test-subnetwork2",
				NetworkIP:  "5.6.7.8",
			}},
		}},
			GetInstanceErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		},
		wantToDiscover: []toDiscover{{
			name: "test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name: "test-disk2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-network",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-network2",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-subnetwork",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-subnetwork2",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "1.2.3.4",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "5.6.7.8",
			network: "test-network2",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "projects/test-project/zones/test-zone/instances/test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}},
	}, {
		name: "discoverInstanceFailure",
		uri:  "projects/test-project/zones/test-zone/instances/test-instance",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{nil},
			GetInstanceErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverDiskSuccess",
		uri:  "projects/test-project/zones/test-zone/disks/test-disk",
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "projects/test-project/zones/test-zone/disks/test-disk",
			}},
			GetDiskErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "projects/test-project/zones/test-zone/disks/test-disk",
		},
	}, {
		name: "discoverDiskFailure",
		uri:  "projects/test-project/zones/test-zone/disks/test-disk",
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{nil},
			GetDiskErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverAddressSuccess",
		uri:  "projects/test-project/zones/test-zone/addresses/test-address",
		gceService: &fake.TestGCE{
			GetAddressResp: []*compute.Address{{
				SelfLink:   "projects/test-project/regions/test-region/addresses/test-address",
				Network:    "test-network",
				Subnetwork: "test-subnetwork",
			}},
			GetAddressErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "projects/test-project/regions/test-region/addresses/test-address",
		},
		wantToDiscover: []toDiscover{{
			name:    "test-network",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceUri:  "projects/test-project/regions/test-region/addresses/test-address",
			},
		}, {
			name:    "test-subnetwork",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceUri:  "projects/test-project/regions/test-region/addresses/test-address",
			},
		}},
	}, {
		name: "discoverAddressFailure",
		uri:  "projects/test-project/zones/test-zone/addresses/test-address",
		gceService: &fake.TestGCE{
			GetAddressResp: []*compute.Address{nil},
			GetAddressErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverForwardingRuleSuccess",
		uri:  "projects/test-project/regions/us-central1/forwardingRules/test-forwarding-rule",
		gceService: &fake.TestGCE{
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "test-forwarding-rule",
				BackendService: "test-backend-service",
				Network:        "test-network",
				Subnetwork:     "test-subnetwork",
			}},
			GetForwardingRuleErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:  "test-forwarding-rule",
		},
		wantToDiscover: []toDiscover{{
			name:    "test-backend-service",
			region:  "us-central1",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}, {
			name:    "test-network",
			region:  "us-central1",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}, {
			name:    "test-subnetwork",
			region:  "us-central1",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}},
	}, {
		name: "discoverForwardingRuleFailure",
		uri:  "projects/test-project/regions/us-central1/forwardingRules/test-forwarding-rule",
		gceService: &fake.TestGCE{
			GetForwardingRuleResp: []*compute.ForwardingRule{nil},
			GetForwardingRuleErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverInstanceGroupSuccess",
		uri:  "projects/test-project/zones/us-central1-a/instanceGroups/test-instance-group",
		gceService: &fake.TestGCE{
			GetInstanceGroupResp: []*compute.InstanceGroup{{SelfLink: "test-instance-group"}},
			GetInstanceGroupErr:  []error{nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "test-instance",
				}, {
					Instance: "test-instance2",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "test-instance-group",
			RelatedResources: []string{"test-instance", "test-instance2"},
		},
		wantToDiscover: []toDiscover{{
			name:   "test-instance",
			region: "us-central1",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
				ResourceUri:  "test-instance-group",
			},
		}, {
			name:   "test-instance2",
			region: "us-central1",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
				ResourceUri:  "test-instance-group",
			},
		}},
	}, {
		name: "discoverInstanceGroupFailure",
		uri:  "projects/test-project/zones/us-central1-a/instanceGroups/test-instance-group",
		gceService: &fake.TestGCE{
			GetInstanceGroupResp: []*compute.InstanceGroup{nil},
			GetInstanceGroupErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverFilestoreSuccess",
		uri:  "projects/test-project/regions/test-region/filestores/test-filestore",
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{{Name: "projects/test-project/regions/test-region/filestores/test-filestore"}},
			GetFilestoreErr:  []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:  "projects/test-project/regions/test-region/filestores/test-filestore",
		},
	}, {
		name: "discoverFilestoreFailure",
		uri:  "projects/test-project/regions/test-region/filestores/test-filestore",
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{nil},
			GetFilestoreErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverHealthCheckSuccess",
		uri:  "projects/test-project/zones/us-central1-a/healthChecks/test-health-check",
		gceService: &fake.TestGCE{
			GetHealthCheckResp: []*compute.HealthCheck{{
				SelfLink: "test-health-check",
			}},
			GetHealthCheckErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_HEALTH_CHECK,
			ResourceUri:  "test-health-check",
		},
	}, {
		name: "discoverHealthCheckFailure",
		uri:  "projects/test-project/zones/us-central1-a/healthChecks/test-health-check",
		gceService: &fake.TestGCE{
			GetHealthCheckResp: []*compute.HealthCheck{nil},
			GetHealthCheckErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}, {
		name: "discoverBackendServiceSuccess",
		uri:  "projects/test-project/regions/test-region/backendServices/test-backend",
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "test-backend",
				Backends: []*compute.Backend{{
					Group: "test-group",
				}, {
					Group: "test-group2",
				}},
			}},
			GetRegionalBackendServiceErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "test-backend",
		},
		wantToDiscover: []toDiscover{{
			name:   "test-group",
			region: "test-region",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
				ResourceUri:  "test-backend",
			},
		}, {
			name:   "test-group2",
			region: "test-region",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
				ResourceUri:  "test-backend",
			},
		}},
	}, {
		name:       "discoverBackendServiceFailure",
		uri:        "projects/test-project/regions/test-region/backendServices/test-backend",
		gceService: &fake.TestGCE{GetRegionalBackendServiceResp: []*compute.BackendService{nil}, GetRegionalBackendServiceErr: []error{cmpopts.AnyError}},
		wantErr:    cmpopts.AnyError,
	}, {
		name: "discoverSubnetwork",
		uri:  "projects/test-project/zones/test-zone/subnetworks/test-subnet",
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
			ResourceUri:  "projects/test-project/zones/test-zone/subnetworks/test-subnet",
		},
	}, {
		name: "discoverNetwork",
		uri:  "projects/test-project/zones/test-zone/networks/test-network",
		gceService: &fake.TestGCE{
			GetNetworkResp: []*compute.Network{{
				SelfLink: "projects/test-project/zones/test-zone/networks/test-network",
				Subnetworks: []string{
					"projects/test-project/regions/test-region/subnetworks/test-subnetwork",
					"projects/test-project/regions/test-region2/subnetworks/test-subnetwork2",
				},
			}},
			GetNetworkErr: []error{nil},
			GetSubnetworkResp: []*compute.Subnetwork{{
				Name:        "test-subnetwork",
				SelfLink:    "projects/test-project/regions/test-region/subnetworks/test-subnetwork",
				IpCidrRange: "10.0.0.0/8",
			}, {
				Name:        "test-subnetwork2",
				SelfLink:    "projects/test-project/regions/test-region2/subnetworks/test-subnetwork2",
				IpCidrRange: "10.1.0.0/8",
			}},
			GetSubnetworkErr: []error{nil, nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
			ResourceUri:  "projects/test-project/zones/test-zone/networks/test-network",
			RelatedResources: []string{
				"projects/test-project/regions/test-region/subnetworks/test-subnetwork",
				"projects/test-project/regions/test-region2/subnetworks/test-subnetwork2",
			},
		},
	}, {
		name:    "unsupportedURIFailure",
		uri:     "some/other/unknown/uri",
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			got, gotToDiscover, err := c.discoverResourceForURI(context.Background(), test.uri)
			if diff := cmp.Diff(test.wantResource, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverResourceForURI() returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverResourceForURI() returned unexpected toDiscover diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverResourceForURI() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverAddress(t *testing.T) {
	tests := []struct {
		name           string
		gceService     *fake.TestGCE
		wantResource   *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			GetAddressResp: []*compute.Address{{
				SelfLink:   "test-address",
				Network:    "test-network",
				Subnetwork: "test-subnet",
			}},
			GetAddressErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
			ResourceUri:  "test-address",
		},
		wantToDiscover: []toDiscover{{
			name:    "test-network",
			region:  defaultRegion,
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceUri:  "test-address",
			},
		}, {
			name:    "test-subnet",
			region:  defaultRegion,
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
				ResourceUri:  "test-address",
			}}},
	}, {
		name:       "failure",
		gceService: &fake.TestGCE{GetAddressResp: []*compute.Address{nil}, GetAddressErr: []error{cmpopts.AnyError}},
		wantErr:    cmpopts.AnyError,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			addressURI := makeRegionalURI(defaultProjectID, defaultRegion, "addresses", "test-address")
			gotResource, gotToDiscover, err := c.discoverAddress(ctx, addressURI)
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverAddress() returned unexpected address diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantResource, gotResource, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverAddress() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverAddress() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverInstance(t *testing.T) {
	tests := []struct {
		name           string
		gceService     *fake.TestGCE
		wantResource   *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{{
				SelfLink: "test-instance",
				Disks: []*compute.AttachedDisk{{
					Source: "test-disk",
				}, {
					Source: "test-other-disk",
				}},
				NetworkInterfaces: []*compute.NetworkInterface{{
					Network:    "test-network",
					Subnetwork: "test-subnet",
					NetworkIP:  "test-network-ip",
				},
				}}},
			GetInstanceErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
			ResourceUri:        "test-instance",
			InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
		},
		wantToDiscover: []toDiscover{{
			name: "test-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name: "test-other-disk",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-network",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-subnet",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		}, {
			name:    "test-network-ip",
			network: "test-network",
			parent: &spb.SapDiscovery_Resource{
				ResourceType:       spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind:       spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
				ResourceUri:        "test-instance",
				InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{},
			},
		},
		},
	}, {
		name: "failure",
		gceService: &fake.TestGCE{
			GetInstanceResp: []*compute.Instance{nil},
			GetInstanceErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			instanceURI := makeZonalURI(defaultProjectID, defaultZone, "instances", "test-instance")
			gotResource, gotToDiscover, err := c.discoverInstance(ctx, instanceURI)
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverInstance() returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantResource, gotResource, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverInstance() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverInstance() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverDisk(t *testing.T) {
	tests := []struct {
		name           string
		diskURI        string
		gceService     *fake.TestGCE
		want           *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name:    "success",
		diskURI: makeZonalURI(defaultProjectID, defaultZone, "disks", "test-disk"),
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{{
				SelfLink: "test-disk",
			}},
			GetDiskErr: []error{nil},
			GetDiskArgs: []*fake.GetDiskArguments{{
				Project:  defaultProjectID,
				Zone:     defaultZone,
				DiskName: "test-disk",
			}},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
			ResourceUri:  "test-disk",
		},
	}, {
		name:    "failure",
		diskURI: makeZonalURI(defaultProjectID, defaultZone, "disks", "test-disk"),
		gceService: &fake.TestGCE{
			GetDiskResp: []*compute.Disk{nil},
			GetDiskErr:  []error{cmpopts.AnyError},
			GetDiskArgs: []*fake.GetDiskArguments{{
				Project:  defaultProjectID,
				Zone:     defaultZone,
				DiskName: "test-disk",
			}},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			got, gotToDiscover, err := c.discoverDisk(ctx, test.diskURI)
			if diff := cmp.Diff(test.want, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverDisk() returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverDisk() returned unexpected diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverDisk() returned unexpected error: %v", err)
			}
		})
	}
}
func TestDiscoverForwardingRule(t *testing.T) {
	tests := []struct {
		name           string
		gceService     *fake.TestGCE
		wantResource   *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			GetForwardingRuleResp: []*compute.ForwardingRule{{
				SelfLink:       "test-forwarding-rule",
				Network:        "test-network",
				Subnetwork:     "test-subnet",
				BackendService: "test-backend-service",
			}},
			GetForwardingRuleErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
			ResourceUri:  "test-forwarding-rule",
		},
		wantToDiscover: []toDiscover{{
			name:    "test-network",
			network: "test-network",
			region:  defaultRegion,
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}, {
			name:    "test-subnet",
			network: "test-network",
			region:  defaultRegion,
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}, {
			name:    "test-backend-service",
			network: "test-network",
			region:  defaultRegion,
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
				ResourceUri:  "test-forwarding-rule",
			},
		}},
	}, {
		name: "failure",
		gceService: &fake.TestGCE{
			GetForwardingRuleResp: []*compute.ForwardingRule{nil},
			GetForwardingRuleErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			fwrURI := makeRegionalURI(defaultProjectID, defaultRegion, "forwardingRules", "test-forwarding-rule")
			gotResource, gotToDiscover, err := c.discoverForwardingRule(ctx, fwrURI)
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverForwardingRule() returned unexpected diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantResource, gotResource, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverForwardingRule() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverInstance() returned unexpected error: %v", err)
			}
		})
	}

}

func TestDiscoverInstanceGroup(t *testing.T) {
	tests := []struct {
		name           string
		gceService     *fake.TestGCE
		wantResource   *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			GetInstanceGroupResp: []*compute.InstanceGroup{{
				SelfLink: "test-group-name",
			}},
			GetInstanceGroupErr: []error{nil},
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "test/instance",
				}, {
					Instance: "test/other/instance",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil},
		},
		wantResource: &spb.SapDiscovery_Resource{
			ResourceType:     spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind:     spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
			ResourceUri:      "test-group-name",
			RelatedResources: []string{"test/instance", "test/other/instance"},
		},
		wantToDiscover: []toDiscover{{
			name: "test/instance",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
				ResourceUri:  "test-group-name",
			},
		}, {
			name: "test/other/instance",
			parent: &spb.SapDiscovery_Resource{
				ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
				ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
				ResourceUri:  "test-group-name",
			},
		}},
	}, {
		name: "failure",
		gceService: &fake.TestGCE{
			GetInstanceGroupResp: []*compute.InstanceGroup{nil},
			GetInstanceGroupErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			groupURI := makeZonalURI(defaultProjectID, defaultZone, "instanceGroups", "test-group-name")
			gotResource, gotToDiscover, err := c.discoverInstanceGroup(ctx, groupURI)
			if diff := cmp.Diff(test.wantResource, gotResource, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverInstanceGroup() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverInstanceGroup() returned unexpected instances diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverInstance() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverInstanceGroupInstances(t *testing.T) {
	tests := []struct {
		name       string
		gceService *fake.TestGCE
		want       []string
		wantErr    error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{{
				Items: []*compute.InstanceWithNamedPorts{{
					Instance: "some/instance",
				}, {
					Instance: "some/other/instance",
				}},
			}},
			ListInstanceGroupInstancesErr: []error{nil},
		},
		want: []string{"some/instance", "some/other/instance"},
	}, {
		name: "failure",
		gceService: &fake.TestGCE{
			ListInstanceGroupInstancesResp: []*compute.InstanceGroupsListInstances{nil},
			ListInstanceGroupInstancesErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			groupURI := makeZonalURI(defaultProjectID, defaultZone, "instanceGroups", "test-group-name")
			got, err := c.discoverInstanceGroupInstances(ctx, groupURI)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("discoverInstance() returned unexpected diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverInstance() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverFilestore(t *testing.T) {
	tests := []struct {
		name           string
		filestoreName  string
		gceService     *fake.TestGCE
		want           *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name:          "success",
		filestoreName: "some/filestore",
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{{
				Name: "some/filestore",
			}},
			GetFilestoreErr: []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
			ResourceUri:  "some/filestore",
		},
	}, {
		name:          "fail",
		filestoreName: "some/filestore",
		gceService: &fake.TestGCE{
			GetFilestoreResp: []*file.Instance{nil},
			GetFilestoreErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			fsURI := makeZonalURI(defaultProjectID, defaultZone, "fileStores", test.filestoreName)
			ctx := context.Background()
			got, gotToDiscover, err := c.discoverFilestore(ctx, fsURI)
			if diff := cmp.Diff(test.want, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverFilestore() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverFilestore() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverFilestore() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverHealthCheck(t *testing.T) {
	tests := []struct {
		name           string
		hcName         string
		gceService     *fake.TestGCE
		want           *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name:   "success",
		hcName: "some/healthcheck",
		gceService: &fake.TestGCE{
			GetHealthCheckResp: []*compute.HealthCheck{{
				SelfLink: "some/healthcheck",
			}},
			GetHealthCheckErr: []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_HEALTH_CHECK,
			ResourceUri:  "some/healthcheck",
		},
	}, {
		name:   "fail",
		hcName: "some/healthcheck",
		gceService: &fake.TestGCE{
			GetHealthCheckResp: []*compute.HealthCheck{nil},
			GetHealthCheckErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			hcURI := makeGlobalURI(defaultProjectID, "healthChecks", test.hcName)
			got, gotToDiscover, err := c.discoverHealthCheck(ctx, hcURI)
			if diff := cmp.Diff(test.want, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverHealthCheck() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverHealthCheck() returned unexpected toDiscover diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverInstance() returned unexpected error: %v", err)
			}
		})
	}
}

func TestDiscoverBackendService(t *testing.T) {
	tests := []struct {
		name           string
		beName         string
		gceService     *fake.TestGCE
		want           *spb.SapDiscovery_Resource
		wantToDiscover []toDiscover
		wantErr        error
	}{{
		name: "success",
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{{
				SelfLink: "some/compute/backend",
			}},
			GetRegionalBackendServiceErr: []error{nil},
		},
		want: &spb.SapDiscovery_Resource{
			ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
			ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
			ResourceUri:  "some/compute/backend",
		},
	}, {
		name: "fail",
		gceService: &fake.TestGCE{
			GetRegionalBackendServiceResp: []*compute.BackendService{nil},
			GetRegionalBackendServiceErr:  []error{cmpopts.AnyError},
		},
		wantErr: cmpopts.AnyError,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := CloudDiscovery{
				GceService: test.gceService,
			}
			ctx := context.Background()
			beURI := makeRegionalURI(defaultProjectID, defaultRegion, "backendServices", test.beName)
			got, gotToDiscover, err := c.discoverBackendService(ctx, beURI)
			if diff := cmp.Diff(test.want, got, resourceDiffOpts...); diff != "" {
				t.Errorf("discoverBackendService() returned unexpected resource diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantToDiscover, gotToDiscover, toDiscoverListOpts...); diff != "" {
				t.Errorf("discoverBackendService() returned unexpected toDiscover diff (-want +got):\n%s", diff)
			}
			if !cmp.Equal(test.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("discoverBackendService() returned unexpected error: %v", err)
			}
		})
	}
}
