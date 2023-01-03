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

// Package system contains types and functions needed to perform SAP System discovery opeerations.
package system

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"

	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sap-agent/protos/system"
)

type gceInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	GetDisk(project, zone, name string) (*compute.Disk, error)
	GetAddressByIP(project, region, ip string) (*compute.AddressList, error)
	GetForwardingRule(project, zone, name string) (*compute.ForwardingRule, error)
	GetRegionalBackendService(project, region, name string) (*compute.BackendService, error)
	GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error)
	ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error)
}

// Discovery is a type used to perform SAP System discovery operations.
type Discovery struct {
	gceService    gceInterface
	exists        commandlineexecutor.CommandExistsRunner
	commandRunner commandlineexecutor.CommandRunner
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

// StartSAPSystemDiscovery Initializes the discovery object and starts the discovery subroutine.
func StartSAPSystemDiscovery(ctx context.Context, config *cpb.Configuration) {

	// Start SAP system discovery only if sap_system_discovery is enabled.
	if config.GetCollectionConfiguration() != nil && !config.GetCollectionConfiguration().GetSapSystemDiscovery() {
		log.Logger.Info("Not starting SAP system discovery.")
		return
	}

	gceService, err := gce.New(ctx)
	if err != nil {
		log.Logger.Error("Failed to create GCE service", log.Error(err))
		usagemetrics.Error(3) // Unexpected error
		return
	}

	d := Discovery{
		gceService:    gceService,
		exists:        commandlineexecutor.CommandExists,
		commandRunner: commandlineexecutor.ExpandAndExecuteCommand,
	}

	go runDiscovery(config, d)
}

func runDiscovery(config *cpb.Configuration, d Discovery) {
	cp := config.GetCloudProperties()
	if cp == nil {
		log.Logger.Debug("No Metadata Cloud Properties found, cannot collect resource information from the Compute API")
		return
	}

	for {
		// Discover instance and immediately adjacent resources (disks, addresses, networks)
		res := d.discoverResources(cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())

		lbRes := d.discoverLoadBalancer(cp)

		// Only add the unique resources, some may be shared, such as network and subnetwork
		for _, l := range lbRes {
			if idx := slices.IndexFunc(res, func(r *spb.Resource) bool { return r.ResourceUri == l.ResourceUri }); idx == -1 {
				res = append(res, l)
			}
		}

		var s string
		for _, r := range res {
			s += fmt.Sprintf("{\n%s\n},", r.String())
		}
		log.Logger.Debugf("Discovered resources:\n%s", s)

		// Perform discovery at most every 4 hours.
		time.Sleep(4 * 60 * 60 * time.Second)
	}

}

func (d *Discovery) discoverResources(projectID, zone, instanceName string) []*spb.Resource {
	var res []*spb.Resource

	log.Logger.Debugf("Discovering instance: %s", instanceName)
	ci, err := d.gceService.GetInstance(projectID, zone, instanceName)
	if err != nil {
		log.Logger.Error(fmt.Sprintf("Could not get instance info from compute API, project=%s zone=%s instance=%s", projectID, zone, instanceName), log.Error(err))
		return res
	}

	now := time.Now().Unix()

	ir := &spb.Resource{
		ResourceType: spb.Resource_COMPUTE,
		ResourceKind: "ComputeInstance",
		ResourceUri:  ci.SelfLink,
		LastUpdated:  now,
	}
	res = append(res, ir)

	disks := d.discoverDisks(projectID, zone, ci)
	res = append(res, disks...)
	for _, disk := range disks {
		ir.RelatedResources = append(ir.RelatedResources, disk.ResourceUri)
	}

	netRes := d.discoverNetworks(projectID, ci)
	res = append(res, netRes...)
	for _, net := range netRes {
		ir.RelatedResources = append(ir.RelatedResources, net.ResourceUri)
	}

	return res
}

func (d *Discovery) discoverDisks(projectID, zone string, ci *compute.Instance) []*spb.Resource {
	var disks []*spb.Resource
	now := time.Now().Unix()
	// Get the disks
	for _, disk := range ci.Disks {
		source, diskName := disk.Source, disk.DeviceName

		s := strings.Split(source, "/")
		if len(s) >= 2 {
			diskName = s[len(s)-1]
		}

		cd, err := d.gceService.GetDisk(projectID, zone, diskName)
		if err != nil {
			log.Logger.Warn(fmt.Sprintf("Could not get disk info from compute API, project=%s zone=%s instance=%s", projectID, zone, diskName), log.Error(err))
			continue
		}

		dr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  cd.SelfLink,
			LastUpdated:  now,
		}
		disks = append(disks, dr)
	}
	return disks
}

func (d *Discovery) discoverNetworks(projectID string, ci *compute.Instance) []*spb.Resource {
	now := time.Now().Unix()
	var netRes []*spb.Resource
	// Get Network related resources
	for _, net := range ci.NetworkInterfaces {

		sr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
			ResourceUri:  net.Subnetwork,
			LastUpdated:  now,
		}
		netRes = append(netRes, sr)

		nr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeNetwork",
			ResourceUri:  net.Network,
			LastUpdated:  now,
		}
		nr.RelatedResources = append(nr.RelatedResources, sr.ResourceUri)
		netRes = append(netRes, nr)

		// Examine assigned IP addresses
		for _, ac := range net.AccessConfigs {
			ar := &spb.Resource{
				ResourceType: spb.Resource_COMPUTE,
				ResourceKind: "PublicAddress",
				LastUpdated:  now,
				ResourceUri:  ac.NatIP,
			}
			netRes = append(netRes, ar)
		}

		netRegion := extractFromURI(net.Subnetwork, "regions")
		if netRegion == "" {
			log.Logger.Warnf("Unable to extract region from subnetwork: %s", net.Subnetwork)
			continue
		}

		// Check Network Interface address to see if it exists as a resource
		ip := net.NetworkIP
		addrs, err := d.gceService.GetAddressByIP(projectID, netRegion, ip)
		if err != nil {
			log.Logger.Warn(fmt.Sprintf("Error locating Address by IP, project=%s region=%s ip=%s", projectID, netRegion, ip), log.Error(err))
			continue
		} else if len(addrs.Items) == 0 {
			log.Logger.Infof("No ComputeAddress with IP=%s in project=%s, region=%s", ip, projectID, netRegion)
			continue
		}
		ar := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeAddress",
			ResourceUri:  addrs.Items[0].SelfLink,
			LastUpdated:  now,
		}
		sr.RelatedResources = append(sr.RelatedResources, ar.ResourceUri)
		netRes = append(netRes, ar)
	}
	return netRes
}

func (d *Discovery) discoverLoadBalancer(cp *ipb.CloudProperties) []*spb.Resource {
	log.Logger.Debugf("Discovering load balancer: %s", cp.GetInstanceName())
	var res []*spb.Resource
	projectID, zone := cp.GetProjectId(), cp.GetZone()
	now := time.Now().Unix()
	lbAddress, err := d.discoverCluster()
	if err != nil || lbAddress == "" {
		log.Logger.Infof("Encountered error discovering cluster address: %v", err)
		return res
	}

	// With address in hand we can find what it is assinged to
	region := strings.Join(strings.Split(zone, "-")[0:2], "-")
	// Check Network Interface address to see if it exists as a resource
	addrs, err := d.gceService.GetAddressByIP(projectID, region, lbAddress)
	if err != nil {
		log.Logger.Warnf("Error locating Address by IP, project=%s region=%s ip=%s. err: %v", projectID, region, lbAddress, err)
		return res
	}
	if len(addrs.Items) == 0 {
		log.Logger.Infof("No ComputeAddress with IP=%s in project=%s, region=%s", lbAddress, projectID, region)
		return res
	}
	addr := addrs.Items[0]

	ar := &spb.Resource{
		ResourceType: spb.Resource_COMPUTE,
		ResourceKind: "ComputeAddress",
		ResourceUri:  addr.SelfLink,
		LastUpdated:  now,
	}
	res = append(res, ar)

	if len(addr.Users) == 0 {
		log.Logger.Warn("Cluster address not in use by anything")
		return res
	}

	// Examine the user of the address, should be a forwarding rule.
	user := addr.Users[0]
	name := extractFromURI(user, "forwardingRules")
	if name == "" {
		log.Logger.Infof("Cluster address not in use by forwarding rule: %s", user)
		return res
	}
	fwr, err := d.gceService.GetForwardingRule(projectID, region, name)
	if err != nil {
		log.Logger.Warnf("Error retrieving forwarding rule: %v", err)
		return res
	}

	fr := &spb.Resource{
		ResourceType:     spb.Resource_COMPUTE,
		ResourceKind:     "ComputeForwardingRule",
		ResourceUri:      fwr.SelfLink,
		RelatedResources: []string{ar.ResourceUri},
		LastUpdated:      now,
	}
	ar.RelatedResources = append(ar.RelatedResources, fr.ResourceUri)
	res = append(res, fr)

	// Examine fwr backend service, this should be the load balancer
	b := fwr.BackendService
	bEName := extractFromURI(b, "backendServices")
	if bEName == "" {
		log.Logger.Infof("Forwarding rule does not have a backend service: %v", b)
		return res
	}

	bERegion := extractFromURI(b, "regions")
	if bERegion == "" {
		log.Logger.Infof("Unable to extract region from backend service: %v", b)
		return res
	}

	bs, err := d.gceService.GetRegionalBackendService(projectID, bERegion, bEName)
	if err != nil {
		log.Logger.Warnf("Error retrieving backend service: %v", err)
		return res
	}

	bsr := &spb.Resource{
		ResourceType:     spb.Resource_COMPUTE,
		ResourceKind:     "ComputeBackendService",
		ResourceUri:      bs.SelfLink,
		LastUpdated:      now,
		RelatedResources: []string{fr.ResourceUri},
	}
	fr.RelatedResources = append(fr.RelatedResources, bsr.ResourceUri)
	res = append(res, bsr)

	igRes := d.discoverInstanceGroups(bs, cp, bsr)
	res = append(res, igRes...)
	return res
}

func (d *Discovery) discoverInstanceGroups(bs *compute.BackendService, cp *ipb.CloudProperties, parent *spb.Resource) []*spb.Resource {
	now := time.Now().Unix()
	projectID := cp.GetProjectId()
	var res []*spb.Resource
	var groups []string
	for _, be := range bs.Backends {
		if be.Group != "" {
			groups = append(groups, be.Group)
		}
	}

	for _, g := range groups {
		gName := extractFromURI(g, "instanceGroups")
		if gName == "" {
			log.Logger.Infof("Backend group is not an instance group")
			continue
		}
		gZone := extractFromURI(g, "zones")
		if gZone == "" {
			log.Logger.Infof("Unable to extract zone from group name")
			continue
		}

		ig, err := d.gceService.GetInstanceGroup(projectID, gZone, gName)
		if err != nil {
			log.Logger.Warnf("Error retrieving instance group: %v", err)
			continue
		}
		igr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeInstanceGroup",
			ResourceUri:  ig.SelfLink,
			LastUpdated:  now,
		}
		parent.RelatedResources = append(parent.RelatedResources, igr.ResourceUri)
		res = append(res, igr)

		iRes := d.discoverInstanceGroupInstances(projectID, gZone, gName, igr, cp)
		res = append(res, iRes...)
	}

	return res
}

func (d *Discovery) discoverInstanceGroupInstances(projectID, zone, name string, parent *spb.Resource, cp *ipb.CloudProperties) []*spb.Resource {
	var res []*spb.Resource
	list, err := d.gceService.ListInstanceGroupInstances(projectID, zone, name)
	if err != nil {
		log.Logger.Warnf("Error retrieving instance group instances: %v", err)
		return res
	}

	var instances []string
	for _, i := range list.Items {
		parent.RelatedResources = append(parent.RelatedResources, i.Instance)
		iName := extractFromURI(i.Instance, "instances")
		if iName == "" {
			log.Logger.Warnf("Unable to extract instance name from instance group items: %s", i.Instance)
			continue
		}
		if iName == cp.GetInstanceName() {
			// Skip since this instance's discovery is handled by a different control flow
			continue
		}
		instances = append(instances, i.Instance)
	}

	for _, i := range instances {
		iName := extractFromURI(i, "instances")
		if iName == "" {
			log.Logger.Warnf("Unable to extract instance name from instance group items: %s", i)
			continue
		}
		iProject := extractFromURI(i, "projects")
		if iProject == "" {
			log.Logger.Warnf("Unable to extract project from instance group items: %s", i)
			continue
		}
		iZone := extractFromURI(i, "zones")
		if iZone == "" {
			log.Logger.Warnf("Unable to extract zone from instance group items: %s", i)
			continue
		}
		instance := d.discoverResources(iProject, iZone, iName)
		res = append(res, instance...)
	}
	return res
}

func (d *Discovery) discoverCluster() (string, error) {
	log.Logger.Info("Discovering cluster")
	if d.exists("crm") {
		stdOut, _, err := d.commandRunner("crm", "config show")
		if err != nil {
			return "", err
		}

		var addrPrimitiveFound bool
		for _, l := range strings.Split(stdOut, "\n") {
			if strings.Contains(l, "rsc_vip_int-primary IPaddr2") {
				addrPrimitiveFound = true
			}
			if addrPrimitiveFound && strings.Contains(l, "params ip") {
				re, err := regexp.Compile("[0-9]+.[0-9]+.[0-9]+.[0-9]+")
				if err != nil {
					log.Logger.Infof("Unable to parse IP Address regex: %v", err)
					continue
				}
				address := re.FindString(l)
				if address == "" {
					return "", errors.New("Unable to locate IP address in crm output: " + stdOut)
				}
				return address, nil
			}
		}
		return "", errors.New("No address found in pcs cluster config output")
	}
	if d.exists("pcs") {
		stdOut, _, err := d.commandRunner("pcs", "config show")
		if err != nil {
			return "", err
		}

		var addrPrimitiveFound bool
		for _, l := range strings.Split(stdOut, "\n") {
			if addrPrimitiveFound && strings.Contains(l, "ip") {
				re, err := regexp.Compile("[0-9]+.[0-9]+.[0-9]+.[0-9]+")
				if err != nil {
					log.Logger.Infof("Unable to parse IP Address regex: %v", err)
					continue
				}
				address := re.FindString(l)
				if address == "" {
					return "", errors.New("Unable to locate IP address in crm output: " + stdOut)
				}
				return address, nil
			}
			if strings.Contains(l, "rsc_vip_") {
				addrPrimitiveFound = true
			}
		}
		return "", errors.New("No address found in pcs cluster config output")
	}
	return "", errors.New("No cluster command found")
}
