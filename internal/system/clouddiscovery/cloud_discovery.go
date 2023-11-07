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

	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"golang.org/x/exp/slices"
	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
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
)

type gceInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	GetInstanceByIP(project, ip string) (*compute.Instance, error)
	GetDisk(project, zone, name string) (*compute.Disk, error)
	GetAddress(project, location, name string) (*compute.Address, error)
	GetAddressByIP(project, region, ip string) (*compute.Address, error)
	GetForwardingRule(project, location, name string) (*compute.ForwardingRule, error)
	GetRegionalBackendService(project, region, name string) (*compute.BackendService, error)
	GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error)
	ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error)
	GetFilestore(project, location, name string) (*file.Instance, error)
	GetFilestoreByIP(project, location, ip string) (*file.ListInstancesResponse, error)
	GetURIForIP(project, ip string) (string, error)
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

func getResourceKind(uri string) string {
	parts := strings.Split(uri, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// CloudDiscovery provides methods to discover a set of resources, and ones related to those.
type CloudDiscovery struct {
	currentInstance    *compute.Instance
	currentInstanceRes *spb.SapDiscovery_Resource
	gceService         gceInterface
	hostResolver       func(string) ([]string, error)
	discoveryFunctions map[string]func(context.Context, string) (*spb.SapDiscovery_Resource, []toDiscover, error)
}

type toDiscover struct {
	name   string
	parent *spb.SapDiscovery_Resource
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
func (d *CloudDiscovery) DiscoverComputeResources(ctx context.Context, parent string, hostList []string, cp *ipb.CloudProperties) ([]*spb.SapDiscovery_Resource, error) {
	var res []*spb.SapDiscovery_Resource
	var uris []string
	var discoverQueue []toDiscover
	pRes, discoverQueue, err := d.discoverResource(ctx, toDiscover{parent, nil}, cp.GetProjectId())
	if err != nil {
		return nil, err
	}
	res = append(res, pRes)
	for _, h := range hostList {
		discoverQueue = append(discoverQueue, toDiscover{h, pRes})
	}
	for len(discoverQueue) > 0 {
		var h toDiscover
		h, discoverQueue = discoverQueue[0], discoverQueue[1:]
		if slices.Contains(uris, h.name) {
			// Already discovered, ignore
			continue
		}
		r, dis, err := d.discoverResource(ctx, h, cp.GetProjectId())
		if err != nil {
			log.CtxLogger(ctx).Warnw("Error discovering resource", "resourceURI", h.name, "error", err)
			continue
		}
		discoverQueue = append(discoverQueue, dis...)
		res = append(res, r)
		uris = append(uris, h.name)
		if h.name != r.ResourceUri {
			uris = append(uris, r.ResourceUri)
		}
	}

	return res, nil
}

func (d *CloudDiscovery) discoverResource(ctx context.Context, h toDiscover, project string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	// h may be a resource URI, a hostname, or an IP address
	uri := h.name
	addrs, err := d.hostResolver(h.name)
	if err != nil {
		return nil, nil, err
	}
	if len(addrs) > 0 {
		addr := addrs[0]
		if h.parent != nil {
			project = extractFromURI(h.parent.ResourceUri, projectsURIPart)
		}
		// h is a hostname or IP address
		uri, err = d.gceService.GetURIForIP(project, addr)
		if err != nil {
			return nil, nil, err
		}
	}

	res, toAdd, err := d.discoverResourceForURI(ctx, uri)
	if h.parent != nil && err == nil {
		h.parent.RelatedResources = append(h.parent.RelatedResources, res.ResourceUri)
	}
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
	ca, err := d.gceService.GetAddress(project, region, extractFromURI(addressURI, addressesURIPart))
	if err != nil {
		return nil, nil, err
	}
	ar := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_ADDRESS,
		ResourceUri:  ca.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}

	toAdd := []toDiscover{{ca.Subnetwork, ar}, {ca.Network, ar}}
	for _, u := range ca.Users {
		toAdd = append(toAdd, toDiscover{u, ar})
	}

	return ar, toAdd, nil
}

func (d *CloudDiscovery) discoverInstance(ctx context.Context, instanceURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(instanceURI, projectsURIPart)
	zone := extractFromURI(instanceURI, zonesURIPart)
	instanceName := extractFromURI(instanceURI, instancesURIPart)
	log.CtxLogger(ctx).Debugw("Discovering instance", "instance", instanceName)
	ci, err := d.gceService.GetInstance(project, zone, instanceName)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not get instance info from compute API",
			"project", project,
			"zone", zone,
			"instance", instanceName,
			"error", err)
		return nil, nil, err
	}

	ir := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_INSTANCE,
		ResourceUri:  ci.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}

	toAdd := []toDiscover{}
	for _, disk := range ci.Disks {
		toAdd = append(toAdd, toDiscover{disk.Source, ir})
	}

	for _, net := range ci.NetworkInterfaces {
		toAdd = append(toAdd,
			toDiscover{net.Network, ir},
			toDiscover{net.Subnetwork, ir},
			toDiscover{net.NetworkIP, ir})
	}

	return ir, toAdd, nil
}

func (d *CloudDiscovery) discoverDisk(ctx context.Context, diskURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	diskName := extractFromURI(diskURI, disksURIPart)
	diskZone := extractFromURI(diskURI, zonesURIPart)
	projectID := extractFromURI(diskURI, projectsURIPart)

	cd, err := d.gceService.GetDisk(projectID, diskZone, diskName)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Could not get disk info from compute API",
			"project", projectID,
			"zone", diskZone,
			"instance", diskName,
			log.Error(err))
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
	fwr, err := d.gceService.GetForwardingRule(project, region, fwrName)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error retrieving forwarding rule", log.Error(err))
		return nil, nil, err
	}

	fr := &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_COMPUTE,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_FORWARDING_RULE,
		ResourceUri:  fwr.SelfLink,
		UpdateTime:   timestamppb.Now(),
	}
	toAdd := []toDiscover{{fwr.BackendService, fr},
		{fwr.Network, fr},
		{fwr.Subnetwork, fr}}

	return fr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroup(ctx context.Context, groupURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	project := extractFromURI(groupURI, projectsURIPart)
	zone := extractFromURI(groupURI, zonesURIPart)
	name := extractFromURI(groupURI, instanceGroupsURIPart)
	ig, err := d.gceService.GetInstanceGroup(project, zone, name)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error retrieving instance group", log.Error(err))
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
	toAdd := []toDiscover{}
	for _, inst := range instances {
		toAdd = append(toAdd, toDiscover{inst, igr})
	}
	return igr, toAdd, nil
}

func (d *CloudDiscovery) discoverInstanceGroupInstances(ctx context.Context, groupURI string) ([]string, error) {
	project := extractFromURI(groupURI, projectsURIPart)
	zone := extractFromURI(groupURI, zonesURIPart)
	name := extractFromURI(groupURI, instanceGroupsURIPart)
	list, err := d.gceService.ListInstanceGroupInstances(project, zone, name)
	if err != nil {
		log.CtxLogger(ctx).Warnw("Error retrieving instance group instances", log.Error(err))
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
	zone := extractFromURI(filestoreURI, zonesURIPart)
	name := extractFromURI(filestoreURI, filestoresURIPart)
	f, err := d.gceService.GetFilestore(project, zone, name)
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
	hc, err := d.gceService.GetHealthCheck(project, name)
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
	bes, err := d.gceService.GetRegionalBackendService(project, region, name)
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
			toAdd = append(toAdd, toDiscover{gs.Group, bsr})
		}
	}
	for _, hc := range bes.HealthChecks {
		toAdd = append(toAdd, toDiscover{hc, bsr})
	}
	return bsr, toAdd, nil
}

func (d *CloudDiscovery) discoverNetwork(ctx context.Context, networkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_NETWORK,
		ResourceUri:  networkURI,
	}, nil, nil
}

func (d *CloudDiscovery) discoverSubnetwork(ctx context.Context, subnetworkURI string) (*spb.SapDiscovery_Resource, []toDiscover, error) {
	return &spb.SapDiscovery_Resource{
		ResourceType: spb.SapDiscovery_Resource_RESOURCE_TYPE_NETWORK,
		ResourceKind: spb.SapDiscovery_Resource_RESOURCE_KIND_SUBNETWORK,
		ResourceUri:  subnetworkURI,
	}, nil, nil
}
