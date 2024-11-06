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

// Package clouddiscovery contains functions related to discovering SAP System cloud resources.
package clouddiscovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"golang.org/x/exp/slices"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/file/v1"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const (
	zonesURIPart           = "zones"
	projectsURIPart        = "projects"
	regionsURIPart         = "regions"
	instancesURIPart       = "instances"
	addressesURIPart       = "addresses"
	disksURIPart           = "disks"
	forwardingRulesURIPart = "forwardingRules"
	backendServicesURIPart = "backendServices"
	subnetworksURIPart     = "subnetworks"
	networksURIPart        = "networks"
	instanceGroupsURIPart  = "instanceGroups"
	filestoresURIPart      = "filestores"
	healthChecksURIPart    = "healthChecks"
	locationsURIPart       = "locations"
)

type gceInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	GetInstanceByIP(project, ip string) (*compute.Instance, error)
	GetDisk(project, zone, name string) (*compute.Disk, error)
	GetAddress(project, location, name string) (*compute.Address, error)
	GetAddressByIP(project, region, subnetwork, ip string) (*compute.Address, error)
	GetForwardingRule(project, location, name string) (*compute.ForwardingRule, error)
	GetRegionalBackendService(project, region, name string) (*compute.BackendService, error)
	GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error)
	ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error)
	GetFilestore(project, location, name string) (*file.Instance, error)
	GetFilestoreByIP(project, location, ip string) (*file.ListInstancesResponse, error)
	GetURIForIP(project, ip, region, subnetwok string) (string, error)
	GetHealthCheck(projectID, name string) (*compute.HealthCheck, error)
}

func extractFromURI(uri, field string) string {
	parts := strings.Split(uri, "/")
	for i, s := range parts {
		if s == field && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}

func regionFromZone(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) == 3 {
		return parts[0] + "-" + parts[1]
	}
	return ""
}

func getResourceKind(uri string) string {
	parts := strings.Split(uri, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// CloudDiscovery provides methods to discover a set of resources, and ones related to those.
type CloudDiscovery struct {
	GceService         gceInterface
	HostResolver       func(string) ([]string, error)
	discoveryFunctions map[string]func(context.Context, string) (*spb.SapDiscovery_Resource, []toDiscover, error)
	resourceCache      map[string]cacheEntry
}

type toDiscover struct {
	name       string
	region     string
	subnetwork string
	parent     *spb.SapDiscovery_Resource
}

type cacheEntry struct {
	res     *spb.SapDiscovery_Resource
	related []toDiscover
}

func (d *CloudDiscovery) configureDiscoveryFunctions() {
	d.discoveryFunctions = make(map[string]func(context.Context, string) (*spb.SapDiscovery_Resource, []toDiscover, error))
	d.discoveryFunctions[instancesURIPart] = d.discoverInstance
	d.discoveryFunctions[addressesURIPart] = d.discoverAddress
	d.discoveryFunctions[disksURIPart] = d.discoverDisk
	d.discoveryFunctions[forwardingRulesURIPart] = d.discoverForwardingRule
	d.discoveryFunctions[backendServicesURIPart] = d.discoverBackendService
	d.discoveryFunctions[instanceGroupsURIPart] = d.discoverInstanceGroup
	d.discoveryFunctions[filestoresURIPart] = d.discoverFilestore
	d.discoveryFunctions[healthChecksURIPart] = d.discoverHealthCheck
	d.discoveryFunctions[subnetworksURIPart] = d.discoverSubnetwork
	d.discoveryFunctions[networksURIPart] = d.discoverNetwork
}

// DiscoverComputeResources attempts to gather information about the provided hosts and any additional
// resources that are identified as related from the cloud descriptions.
func (d *CloudDiscovery) DiscoverComputeResources(ctx context.Context, parentResource *spb.SapDiscovery_Resource, parentSubnetwork string, hostList []string, cp *ipb.CloudProperties) []*spb.SapDiscovery_Resource {
	log.CtxLogger(ctx).Debugw("DiscoverComputeResources called", "parent", parentResource, "hostList", hostList)
	var res []*spb.SapDiscovery_Resource
	var uris []string
	var discoverQueue []toDiscover
	var region string
	if cp.GetZone() != "" {
		region = regionFromZone(cp.GetZone())
	}
	for _, h := range hostList {
		discoverQueue = append(discoverQueue, toDiscover{
			name:       h,
			region:     region,
			subnetwork: parentSubnetwork,
			parent:     parentResource,
		})
	}
	for len(discoverQueue) > 0 {
		var h toDiscover
		h, discoverQueue = discoverQueue[0], discoverQueue[1:]
		if h.name == "" {
			continue
		}
		if slices.Contains(uris, h.name) {
			log.CtxLogger(ctx).Debugw("Already discovered", "h", h.name)
			// Already discovered, ignore
			continue
		}
		r, dis, err := d.discoverResource(ctx, h, cp.GetProjectId())
		if err != nil {
			continue
		}
		log.CtxLogger(ctx).Debugw("Adding to queue", "dis", dis, "h", h.name)
		discoverQueue = append(discoverQueue, dis...)
		res = append(res, r)
		uris = append(uris, h.name)
		if h.name != r.ResourceUri {
			uris = append(uris, r.ResourceUri)
		}
	}

	return res
}

func (d *CloudDiscovery) discoverResource(ctx context.Context, host toDiscover, project string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverResource", "name", host.name, "parent", host.parent.GetResourceUri())
	if d.resourceCache == nil {
		d.resourceCache = make(map[string]cacheEntry)
	}
	now := time.Now()
	// Check cache for this hostname
	if c, ok := d.resourceCache[host.name]; ok {
		if now.Sub(c.res.UpdateTime.AsTime()) < (10 * time.Minute) {
			log.CtxLogger(ctx).Debugw("discoverResource cache hit", "name", host.name, "now", now, "res", c.res, "related", c.related)
			return c.res, c.related, nil
		}
	}
	// h may be a resource URI, a hostname, or an IP address
	uri := host.name
	var addr string
	addrs, _ := d.HostResolver(host.name)
	log.CtxLogger(ctx).Debugw("discoverResource addresses", "addrs", addrs)
	// An error may just mean that
	if len(addrs) > 0 {
		addr = addrs[0]

		// Check cache for this address
		if c, ok := d.resourceCache[addr]; ok {
			// Cache did not hit for the hostname, add it
			d.resourceCache[host.name] = c
			if now.Sub(c.res.UpdateTime.AsTime()) < (10 * time.Minute) {
				log.CtxLogger(ctx).Debugw("discoverResource cache hit", "name", host.name, "now", now, "res", c.res, "related", c.related)
				return c.res, c.related, nil
			}
		}

		if host.parent != nil {
			project = extractFromURI(host.parent.ResourceUri, projectsURIPart)
		}

		var err error
		uri, err = d.GceService.GetURIForIP(project, addr, host.region, host.subnetwork)
		if err != nil {
			log.CtxLogger(ctx).Infow("discoverResource URI error", "err", err, "addr", addr, "host", host.name)
			return nil, nil, err
		}
		log.CtxLogger(ctx).Debugw("discoverResource uri for ip", "uri", uri)

		// Check cache for this URI
		if c, ok := d.resourceCache[uri]; ok {
			// Cache did not hit for the hostname or address, add it
			d.resourceCache[host.name] = c
			d.resourceCache[addr] = c
			if now.Sub(c.res.UpdateTime.AsTime()) < (10 * time.Minute) {
				log.CtxLogger(ctx).Debugw("discoverResource cache hit", "name", host.name, "now", now, "res", c.res, "related", c.related)
				return c.res, c.related, nil
			}
		}
	}
	log.CtxLogger(ctx).Debugw("discoverResource host did not resolve", "uri", uri)
	res, toAdd, err := d.discoverResourceForURI(ctx, uri)
	if res == nil || err != nil {
		return nil, nil, err
	}
	if uri != host.name && res.ResourceKind == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE {
		res.InstanceProperties.VirtualHostname = host.name
	}
	if host.parent != nil {
		if !slices.Contains(host.parent.RelatedResources, res.ResourceUri) {
			host.parent.RelatedResources = append(host.parent.RelatedResources, res.ResourceUri)
		}
		if !slices.Contains(res.RelatedResources, host.parent.ResourceUri) {
			res.RelatedResources = append(res.RelatedResources, host.parent.ResourceUri)
		}
	}
	c := cacheEntry{res, toAdd}
	d.resourceCache[host.name] = c
	if host.name != uri {
		d.resourceCache[uri] = c
	}
	if addr != "" && addr != host.name {
		d.resourceCache[addr] = c
	}
	log.CtxLogger(ctx).Debugw("discoverResource result", "res", res, "toAdd", toAdd, "err", err)
	return res, toAdd, err
}

func (d *CloudDiscovery) discoverResourceForURI(ctx context.Context, uri string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	if d.discoveryFunctions == nil {
		d.configureDiscoveryFunctions()
	}
	resourceKind := getResourceKind(uri)
	f, ok := d.discoveryFunctions[resourceKind]
	if !ok {
		return nil, nil, fmt.Errorf("Unsupported resource URI: %q", uri)
	}
	return f(ctx, uri)
}

func (d *CloudDiscovery) discoverAddress(ctx context.Context, addressURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(addressURI, projectsURIPart)
	region := extractFromURI(addressURI, regionsURIPart)
	ca, err := d.GceService.GetAddress(project, region, extractFromURI(addressURI, addressesURIPart))
	if err != nil {
		return nil, nil, err
	}
	ar := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
		ResourceUri:  ca.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}

	toAdd := []toDiscover{{
		name:   ca.Subnetwork,
		region: region,
		parent: ar,
	}, {
		name:   ca.Network,
		region: region,
		parent: ar,
	}}
	for _, u := range ca.Users {
		toAdd = append(toAdd, toDiscover{
			name:       u,
			region:     region,
			subnetwork: ca.Subnetwork,
			parent:     ar,
		})
	}

	return ar, toAdd, nil
}

func (d *CloudDiscovery) discoverInstance(ctx context.Context, instanceURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(instanceURI, projectsURIPart)
	zone := extractFromURI(instanceURI, zonesURIPart)
	region := regionFromZone(zone)
	instanceName := extractFromURI(instanceURI, instancesURIPart)
	ci, err := d.GceService.GetInstance(project, zone, instanceName)
	if err != nil {
		return nil, nil, err
	}

	ir := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		ResourceUri:  ci.SelfLink,
		UpdateTime:   timestamppb.Now(),
		InstanceProperties: &spb.SapDiscovery_Resource_InstanceProperties{
			InstanceNumber: ci.Id,
		},
	}

	toAdd := []toDiscover{}
	for _, disk := range ci.Disks {
		toAdd = append(toAdd, toDiscover{
			name:   disk.Source,
			region: region,
			parent: ir,
		})
	}

	for _, net := range ci.NetworkInterfaces {
		toAdd = append(toAdd,
			toDiscover{
				name:   net.Network,
				region: region,
				parent: ir,
			},
			toDiscover{
				name:   net.Subnetwork,
				region: region,
				parent: ir,
			},
			toDiscover{
				name:   net.NetworkIP,
				region: region,
				parent: ir,
			})
	}

	return ir, toAdd, nil
}

func (d *CloudDiscovery) discoverDisk(ctx context.Context, diskURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	diskName := extractFromURI(diskURI, disksURIPart)
	diskZone := extractFromURI(diskURI, zonesURIPart)
	projectID := extractFromURI(diskURI, projectsURIPart)
	cd, err := d.GceService.GetDisk(projectID, diskZone, diskName)
	if err != nil {
		return nil, nil, err
	}

	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_DISK,
		ResourceUri:  cd.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}

func (d *CloudDiscovery) discoverForwardingRule(ctx context.Context, fwrURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(fwrURI, projectsURIPart)
	region := extractFromURI(fwrURI, regionsURIPart)
	fwrName := extractFromURI(fwrURI, forwardingRulesURIPart)
	fwr, err := d.GceService.GetForwardingRule(project, region, fwrName)
	if err != nil {
		return nil, nil, err
	}

	fr := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
		ResourceUri:  fwr.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}
	toAdd := []toDiscover{{
		name:       fwr.BackendService,
		region:     region,
		subnetwork: fwr.Subnetwork,
		parent:     fr,
	}, {
		name:       fwr.Network,
		region:     region,
		subnetwork: fwr.Subnetwork,
		parent:     fr,
	}, {
		name:       fwr.Subnetwork,
		region:     region,
		subnetwork: fwr.Subnetwork,
		parent:     fr,
	}}

	return fr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroup(ctx context.Context, groupURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debug("Discovering instance group", "groupURI", groupURI)
	project := extractFromURI(groupURI, projectsURIPart)
	zone := extractFromURI(groupURI, zonesURIPart)
	name := extractFromURI(groupURI, instanceGroupsURIPart)
	ig, err := d.GceService.GetInstanceGroup(project, zone, name)
	if err != nil {
		return nil, nil, err
	}

	igr := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE_GROUP,
		ResourceUri:  ig.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}

	instances, err := d.discoverInstanceGroupInstances(ctx, groupURI)
	if err != nil {
		return nil, nil, err
	}

	region := regionFromZone(zone)
	toAdd := []toDiscover{}
	for _, inst := range instances {
		toAdd = append(toAdd, toDiscover{
			name:       inst,
			region:     region,
			subnetwork: ig.Subnetwork,
			parent:     igr,
		})
	}
	return igr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroupInstances(ctx context.Context, groupURI string) ([]string, error) {
	project := extractFromURI(groupURI, projectsURIPart)
	zone := extractFromURI(groupURI, zonesURIPart)
	name := extractFromURI(groupURI, instanceGroupsURIPart)
	list, err := d.GceService.ListInstanceGroupInstances(project, zone, name)
	if err != nil {
		return nil, err
	}

	var instances []string
	for _, i := range list.Items {
		instances = append(instances, i.Instance)
	}

	return instances, nil
}

func (d *CloudDiscovery) discoverFilestore(ctx context.Context, filestoreURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(filestoreURI, projectsURIPart)
	location := extractFromURI(filestoreURI, locationsURIPart)
	name := extractFromURI(filestoreURI, filestoresURIPart)
	f, err := d.GceService.GetFilestore(project, location, name)
	if err != nil {
		return nil, nil, err
	}

	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_STORAGE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FILESTORE,
		ResourceUri:  f.Name,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}

func (d *CloudDiscovery) discoverHealthCheck(ctx context.Context, healthCheckURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(healthCheckURI, projectsURIPart)
	name := extractFromURI(healthCheckURI, healthChecksURIPart)
	hc, err := d.GceService.GetHealthCheck(project, name)
	if err != nil {
		return nil, nil, err
	}
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_HEALTH_CHECK,
		ResourceUri:  hc.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}

func (d *CloudDiscovery) discoverBackendService(ctx context.Context, backendServiceURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(backendServiceURI, projectsURIPart)
	region := extractFromURI(backendServiceURI, regionsURIPart)
	name := extractFromURI(backendServiceURI, backendServicesURIPart)
	bes, err := d.GceService.GetRegionalBackendService(project, region, name)
	if err != nil {
		return nil, nil, err
	}
	bsr := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_BACKEND_SERVICE,
		ResourceUri:  bes.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}
	toAdd := []toDiscover{}
	for _, gs := range bes.Backends {
		if gs.Group != "" {
			toAdd = append(toAdd, toDiscover{
				name:   gs.Group,
				region: region,
				parent: bsr,
			})
		}
	}
	for _, hc := range bes.HealthChecks {
		toAdd = append(toAdd, toDiscover{
			name:   hc,
			region: region,
			parent: bsr,
		})
	}
	return bsr, toAdd, nil
}

func (d *CloudDiscovery) discoverNetwork(ctx context.Context, networkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
		ResourceUri:  networkURI,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}

func (d *CloudDiscovery) discoverSubnetwork(ctx context.Context, subnetworkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
		ResourceUri:  subnetworkURI,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}
