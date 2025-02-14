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
	"net"
	"regexp"
	"strings"
	"time"

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"golang.org/x/exp/slices"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/file/v1"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
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

var (
	addressRegex = regexp.MustCompile("^((25[0-5]|(2[0-4]|1\\d|[1-9]|)\\d)\\.?\\b){4}$")
	uriRegex     = regexp.MustCompile("https?://(www|beta)\\.googleapis\\.com(\\/[a-zA-Z0-9\\-]+)+")
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
	GetNetwork(name, project string) (*compute.Network, error)
	GetSubnetwork(name, project, region string) (*compute.Subnetwork, error)
}

// ExtractFromURI attempts to extract the value of a field from a URI. This expects the format of the string to be "/<field>/<value>".
func ExtractFromURI(uri, field string) string {
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
	networks           map[string]*CloudNetwork
}

// CloudSubnetwork represents a Cloud Subnetwork.
type CloudSubnetwork struct {
	name, region string
	ipRange      *net.IPNet
	network      *CloudNetwork
}

// CloudNetwork represents a Cloud Network.
type CloudNetwork struct {
	name, project string
	subnets       []*CloudSubnetwork
}

type toDiscover struct {
	name    string
	region  string
	network string
	parent  *spb.SapDiscovery_Resource
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
func (d *CloudDiscovery) DiscoverComputeResources(ctx context.Context, parentResource *spb.SapDiscovery_Resource, parentNetwork string, hostList []string, cp *ipb.CloudProperties) []*spb.SapDiscovery_Resource {
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
			name:    h,
			region:  region,
			network: parentNetwork,
			parent:  parentResource,
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
			log.CtxLogger(ctx).Infow("discoverResource error", "err", err, "h", h.name)
			continue
		}
		// If the parent is not an instance, and this resource is, then move the instance properties
		// virtual hostname from the parent to this resource.
		if h.parent != nil && r.ResourceKind == spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE &&
			h.parent.ResourceKind != spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE &&
			h.parent.GetInstanceProperties().GetVirtualHostname() != "" {
			if r.InstanceProperties == nil {
				r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
			}
			r.InstanceProperties.VirtualHostname = h.parent.GetInstanceProperties().GetVirtualHostname()
			h.parent.InstanceProperties = nil
		}

		if h.name != r.ResourceUri {
			// Only apply a virtual hostn	ame if the name being discovered is not a URI or IP address.
			log.CtxLogger(ctx).Debugw("Checking virtual hostname", "h", h.name)
			if !addressRegex.MatchString(h.name) && !uriRegex.MatchString(h.name) {
				if r.InstanceProperties == nil {
					r.InstanceProperties = &spb.SapDiscovery_Resource_InstanceProperties{}
				}
				log.CtxLogger(ctx).Debugw("Setting virtual hostname on resource", "h", h.name, "r", r)
				r.InstanceProperties.VirtualHostname = h.name
			}
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

	if len(addrs) > 0 {
		uri = ""
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
			project = ExtractFromURI(host.parent.ResourceUri, projectsURIPart)
		}

		var err error
		if host.network != "" {
			log.CtxLogger(ctx).Debugw("discoverResource host network", "network", host.network)
			ip := net.ParseIP(addr)
			// Find the right subnetwork that the address might belong to
			cloudNet := d.networks[host.network]
			if cloudNet == nil {
				log.CtxLogger(ctx).Debugw("discoverResource host network not found, discovering", "network", host.network)
				d.discoverNetwork(ctx, host.network)
				cloudNet = d.networks[host.network]
			}
			log.CtxLogger(ctx).Debugw("discoverResource host network subnets", "subnets", cloudNet.subnets)
			for _, s := range cloudNet.subnets {
				log.CtxLogger(ctx).Debugw("discoverResource host network subnets ipRange", "ipRange", s.ipRange)
				if s.ipRange.Contains(ip) {
					log.CtxLogger(ctx).Debugw("discoverResource host network subnets ipRange contains", "ipRange", s.ipRange, "ip", addr, "subnet", s.name)
					uri, err = d.GceService.GetURIForIP(project, addr, host.region, s.name)
					if err != nil {
						log.CtxLogger(ctx).Infow("discoverResource URI in network error", "err", err, "addr", addr, "host", host.name)
					}
					break
				}
			}
		}

		if uri == "" {
			uri, err = d.GceService.GetURIForIP(project, addr, host.region, "")
		}
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
	d.resourceCache[res.ResourceUri] = c
	if host.name != uri {
		d.resourceCache[uri] = c
	}
	if addr != "" && addr != host.name {
		d.resourceCache[addr] = c
	}
	return res, toAdd, err
}

func (d *CloudDiscovery) discoverResourceForURI(ctx context.Context, uri string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	if d.discoveryFunctions == nil {
		d.configureDiscoveryFunctions()
	}
	resourceKind := getResourceKind(uri)
	if resourceKind == "" {
		return nil, nil, fmt.Errorf("Undetected resource kind for URI %q", uri)
	}
	log.CtxLogger(ctx).Debugw("discoverResourceForURI", "uri", uri, "resourceKind", resourceKind)
	f, ok := d.discoveryFunctions[resourceKind]
	if !ok {
		return nil, nil, fmt.Errorf("Unsupported resource URI: %q", uri)
	}
	return f(ctx, uri)
}

func (d *CloudDiscovery) discoverAddress(ctx context.Context, addressURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverAddress", "addressURI", addressURI)
	project := ExtractFromURI(addressURI, projectsURIPart)
	region := ExtractFromURI(addressURI, regionsURIPart)
	ca, err := d.GceService.GetAddress(project, region, ExtractFromURI(addressURI, addressesURIPart))
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
		name:    ca.Subnetwork,
		region:  region,
		network: ca.Network,
		parent:  ar,
	}, {
		name:    ca.Network,
		region:  region,
		network: ca.Network,
		parent:  ar,
	}}
	for _, u := range ca.Users {
		toAdd = append(toAdd, toDiscover{
			name:    u,
			region:  region,
			network: ca.Network,
			parent:  ar,
		})
	}

	return ar, toAdd, nil
}

func (d *CloudDiscovery) discoverInstance(ctx context.Context, instanceURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverInstance", "instanceURI", instanceURI)
	project := ExtractFromURI(instanceURI, projectsURIPart)
	zone := ExtractFromURI(instanceURI, zonesURIPart)
	region := regionFromZone(zone)
	instanceName := ExtractFromURI(instanceURI, instancesURIPart)
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
		ir.InstanceProperties.DiskDeviceNames = append(ir.InstanceProperties.DiskDeviceNames,
			&spb.SapDiscovery_Resource_InstanceProperties_DiskDeviceName{
				Source:     disk.Source,
				DeviceName: disk.DeviceName,
			})
		toAdd = append(toAdd, toDiscover{
			name:   disk.Source,
			region: region,
			parent: ir,
		})
	}

	for _, net := range ci.NetworkInterfaces {
		toAdd = append(toAdd,
			toDiscover{
				name:    net.Network,
				region:  region,
				network: net.Network,
				parent:  ir,
			},
			toDiscover{
				name:    net.Subnetwork,
				region:  region,
				network: net.Network,
				parent:  ir,
			},
			toDiscover{
				name:    net.NetworkIP,
				region:  region,
				network: net.Network,
				parent:  ir,
			})
	}

	return ir, toAdd, nil
}

func (d *CloudDiscovery) discoverDisk(ctx context.Context, diskURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverDisk", "diskURI", diskURI)
	diskName := ExtractFromURI(diskURI, disksURIPart)
	diskZone := ExtractFromURI(diskURI, zonesURIPart)
	projectID := ExtractFromURI(diskURI, projectsURIPart)
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
	log.CtxLogger(ctx).Debugw("discoverForwardingRule", "fwrURI", fwrURI)
	project := ExtractFromURI(fwrURI, projectsURIPart)
	region := ExtractFromURI(fwrURI, regionsURIPart)
	fwrName := ExtractFromURI(fwrURI, forwardingRulesURIPart)
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
		name:    fwr.BackendService,
		region:  region,
		network: fwr.Network,
		parent:  fr,
	}, {
		name:    fwr.Network,
		region:  region,
		network: fwr.Network,
		parent:  fr,
	}, {
		name:    fwr.Subnetwork,
		region:  region,
		network: fwr.Network,
		parent:  fr,
	}}

	return fr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroup(ctx context.Context, groupURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("Discovering instance group", "groupURI", groupURI)
	project := ExtractFromURI(groupURI, projectsURIPart)
	zone := ExtractFromURI(groupURI, zonesURIPart)
	name := ExtractFromURI(groupURI, instanceGroupsURIPart)
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
			name:    inst,
			region:  region,
			network: ig.Network,
			parent:  igr,
		})
		igr.RelatedResources = append(igr.RelatedResources, inst)
	}
	return igr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroupInstances(ctx context.Context, groupURI string) ([]string, error) {
	log.CtxLogger(ctx).Debugw("discoverInstanceGroupInstances", "groupURI", groupURI)
	project := ExtractFromURI(groupURI, projectsURIPart)
	zone := ExtractFromURI(groupURI, zonesURIPart)
	name := ExtractFromURI(groupURI, instanceGroupsURIPart)
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
	log.CtxLogger(ctx).Debugw("discoverFilestore", "filestoreURI", filestoreURI)
	project := ExtractFromURI(filestoreURI, projectsURIPart)
	location := ExtractFromURI(filestoreURI, locationsURIPart)
	name := ExtractFromURI(filestoreURI, filestoresURIPart)
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
	log.CtxLogger(ctx).Debugw("discoverHealthCheck", "healthCheckURI", healthCheckURI)
	project := ExtractFromURI(healthCheckURI, projectsURIPart)
	name := ExtractFromURI(healthCheckURI, healthChecksURIPart)
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
	log.CtxLogger(ctx).Debugw("discoverBackendService", "backendServiceURI", backendServiceURI)
	project := ExtractFromURI(backendServiceURI, projectsURIPart)
	region := ExtractFromURI(backendServiceURI, regionsURIPart)
	name := ExtractFromURI(backendServiceURI, backendServicesURIPart)
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
				name:    gs.Group,
				region:  region,
				parent:  bsr,
				network: bes.Network,
			})
		}
	}
	for _, hc := range bes.HealthChecks {
		toAdd = append(toAdd, toDiscover{
			name:    hc,
			region:  region,
			parent:  bsr,
			network: bes.Network,
		})
	}
	return bsr, toAdd, nil
}

func (d *CloudDiscovery) discoverNetwork(ctx context.Context, networkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverNetwork", "networkURI", networkURI)
	if d.networks == nil {
		d.networks = make(map[string]*CloudNetwork)
	}
	cloudNet, ok := d.networks[networkURI]
	if !ok {
		cloudNet = &CloudNetwork{
			name:    ExtractFromURI(networkURI, networksURIPart),
			project: ExtractFromURI(networkURI, projectsURIPart),
		}
	}

	cn, err := d.GceService.GetNetwork(ExtractFromURI(networkURI, networksURIPart), ExtractFromURI(networkURI, projectsURIPart))
	if err != nil {
		return nil, nil, err
	}

	nr := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
		ResourceUri:  cn.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}

	cloudNet.subnets = []*CloudSubnetwork{}
	for _, s := range cn.Subnetworks {
		log.CtxLogger(ctx).Debugw("discoverNetwork subnetwork", "subnetwork", s)
		cs, err := d.GceService.GetSubnetwork(ExtractFromURI(s, subnetworksURIPart),
			ExtractFromURI(s, projectsURIPart),
			ExtractFromURI(s, regionsURIPart))
		if err != nil {
			log.CtxLogger(ctx).Infow("discoverNetwork subnetwork error", "err", err, "subnetwork", s)
			continue
		}
		log.CtxLogger(ctx).Debugw("discoverNetwork subnetwork", "subnetwork", cs)
		_, ipRange, err := net.ParseCIDR(cs.IpCidrRange)
		if err != nil {
			log.CtxLogger(ctx).Infow("discoverNetwork ipRange error", "err", err, "ipRange", cs.IpCidrRange)
			continue
		}
		cloudNet.subnets = append(cloudNet.subnets, &CloudSubnetwork{
			name:    cs.Name,
			region:  cs.Region,
			ipRange: ipRange,
			network: cloudNet,
		})
	}
	d.networks[networkURI] = cloudNet
	log.CtxLogger(ctx).Debugw("discoverNetwork result", "nr", nr, "cloudNet", cloudNet)

	return nr, nil, nil
}

func (d *CloudDiscovery) discoverSubnetwork(ctx context.Context, subnetworkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	log.CtxLogger(ctx).Debugw("discoverSubnetwork", "subnetworkURI", subnetworkURI)
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
		ResourceUri:  subnetworkURI,
		UpdateTime:   timestamppb.Now(),
	}, nil, nil
}
