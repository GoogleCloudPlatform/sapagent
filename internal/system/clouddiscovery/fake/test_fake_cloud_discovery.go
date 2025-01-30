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

// Package fake contains structures and functions to fake the CloudDiscovery interface.
package fake

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

// DiscoverComputeResourcesArgs encapsulates arguments sent to the DiscoverComputeResources function.
type DiscoverComputeResourcesArgs struct {
	Parent     *spb.SapDiscovery_Resource
	Subnetwork string
	HostList   []string
	CP         *ipb.CloudProperties
}

// CloudDiscovery implements a fake version of the public interface for CloudDiscovery.
type CloudDiscovery struct {
	DiscoverComputeResourcesResp      [][]*spb.SapDiscovery_Resource
	DiscoverComputeResourcesArgs      []DiscoverComputeResourcesArgs
	DiscoverComputeResourcesArgsDiffs []string
	discoverComputeResourcesCallCount int
}

// DiscoverComputeResources is a fake implementation for the CloudDiscovery method.
func (c *CloudDiscovery) DiscoverComputeResources(ctx context.Context, parent *spb.SapDiscovery_Resource, subnetwork string, hostList []string, cp *ipb.CloudProperties) []*spb.SapDiscovery_Resource {
	defer func() {
		c.discoverComputeResourcesCallCount++
	}()

	if len(c.DiscoverComputeResourcesArgs) > c.discoverComputeResourcesCallCount {
		curArgs := DiscoverComputeResourcesArgs{Parent: parent, HostList: hostList, CP: cp}
		if diff := cmp.Diff(c.DiscoverComputeResourcesArgs[c.discoverComputeResourcesCallCount], curArgs, protocmp.Transform(), protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "related_resources"), protocmp.IgnoreFields(&spb.SapDiscovery_Resource{}, "instance_properties")); diff != "" {
			c.DiscoverComputeResourcesArgsDiffs = append(c.DiscoverComputeResourcesArgsDiffs, diff)
		}
	}

	return c.DiscoverComputeResourcesResp[c.discoverComputeResourcesCallCount]
}
