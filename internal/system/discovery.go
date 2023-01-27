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

// Package system contains types and functions needed to perform SAP System discovery operations.
package system

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"

	"golang.org/x/exp/slices"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	spb "github.com/GoogleCloudPlatform/sapagent/protos/system"
)

var (
	ipRegex      = regexp.MustCompile("[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+")
	fsMountRegex = regexp.MustCompile("([0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+):(/[a-zA-Z0-9]+)")
)

type gceInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	GetDisk(project, zone, name string) (*compute.Disk, error)
	GetAddressByIP(project, region, ip string) (*compute.AddressList, error)
	GetForwardingRule(project, zone, name string) (*compute.ForwardingRule, error)
	GetRegionalBackendService(project, region, name string) (*compute.BackendService, error)
	GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error)
	ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error)
	GetFilestoreByIP(project, location, ip string) (*file.ListInstancesResponse, error)
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
// Returns true if the discovery goroutine is started, and false otherwise.
func StartSAPSystemDiscovery(ctx context.Context, config *cpb.Configuration, gceService gceInterface) bool {
	// Start SAP system discovery only if sap_system_discovery is enabled.
	if !config.GetCollectionConfiguration().GetSapSystemDiscovery() {
		log.Logger.Info("Not starting SAP system discovery.")
		return false
	}

	d := Discovery{
		gceService:    gceService,
		exists:        commandlineexecutor.CommandExists,
		commandRunner: commandlineexecutor.ExpandAndExecuteCommand,
	}

	go runDiscovery(config, d)
	return true
}

func runDiscovery(config *cpb.Configuration, d Discovery) {
	cp := config.GetCloudProperties()
	if cp == nil {
		log.Logger.Debug("No Metadata Cloud Properties found, cannot collect resource information from the Compute API")
		return
	}

	for {
		// Discover instance and immediately adjacent resources (disks, addresses, networks)
		res, ci, ir := d.discoverInstance(cp.GetProjectId(), cp.GetZone(), cp.GetInstanceName())

		netRes := d.discoverNetworks(cp.GetProjectId(), ci, ir)
		res = append(res, netRes...)

		disks := d.discoverDisks(cp.GetProjectId(), cp.GetZone(), ci, ir)
		res = append(res, disks...)

		fsRes := d.discoverFilestores(cp.GetProjectId(), ir)
		res = append(res, fsRes...)

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
		log.Logger.Debugw("Discovered resources", "resources", s)

		// Perform discovery at most every 4 hours.
		time.Sleep(4 * 60 * 60 * time.Second)
	}

}

func (d *Discovery) discoverInstance(projectID, zone, instanceName string) ([]*spb.Resource, *compute.Instance, *spb.Resource) {
	var res []*spb.Resource
	log.Logger.Debugw("Discovering instance", log.String("instance", instanceName))
	ci, err := d.gceService.GetInstance(projectID, zone, instanceName)
	if err != nil {
		log.Logger.Errorw("Could not get instance info from compute API",
			log.String("project", projectID),
			log.String("zone", zone),
			log.String("instance", instanceName),
			log.Error(err))
		return res, nil, nil
	}

	now := time.Now().Unix()

	ir := &spb.Resource{
		ResourceType: spb.Resource_COMPUTE,
		ResourceKind: "ComputeInstance",
		ResourceUri:  ci.SelfLink,
		LastUpdated:  now,
	}
	res = append(res, ir)

	return res, ci, ir
}

func (d *Discovery) discoverDisks(projectID, zone string, ci *compute.Instance, ir *spb.Resource) []*spb.Resource {
	var disks []*spb.Resource
	if ci == nil || ci.Disks == nil || len(ci.Disks) == 0 {
		return disks
	}
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
			log.Logger.Warnw("Could not get disk info from compute API",
				log.String("project", projectID),
				log.String("zone", zone),
				log.String("instance", diskName),
				log.Error(err))
			continue
		}

		dr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeDisk",
			ResourceUri:  cd.SelfLink,
			LastUpdated:  now,
		}
		disks = append(disks, dr)
		ir.RelatedResources = append(ir.RelatedResources, dr.ResourceUri)
	}
	return disks
}

func (d *Discovery) discoverNetworks(projectID string, ci *compute.Instance, ir *spb.Resource) []*spb.Resource {
	var netRes []*spb.Resource
	if ci == nil || ci.NetworkInterfaces == nil || len(ci.NetworkInterfaces) == 0 {
		return netRes
	}
	now := time.Now().Unix()
	// Get Network related resources
	for _, net := range ci.NetworkInterfaces {
		sr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeSubnetwork",
			ResourceUri:  net.Subnetwork,
			LastUpdated:  now,
		}
		netRes = append(netRes, sr)
		ir.RelatedResources = append(ir.RelatedResources, sr.ResourceUri)

		nr := &spb.Resource{
			ResourceType: spb.Resource_COMPUTE,
			ResourceKind: "ComputeNetwork",
			ResourceUri:  net.Network,
			LastUpdated:  now,
		}
		nr.RelatedResources = append(nr.RelatedResources, sr.ResourceUri)
		netRes = append(netRes, nr)
		ir.RelatedResources = append(ir.RelatedResources, nr.ResourceUri)

		// Examine assigned IP addresses
		for _, ac := range net.AccessConfigs {
			ar := &spb.Resource{
				ResourceType: spb.Resource_COMPUTE,
				ResourceKind: "PublicAddress",
				LastUpdated:  now,
				ResourceUri:  ac.NatIP,
			}
			netRes = append(netRes, ar)
			ir.RelatedResources = append(ir.RelatedResources, ar.ResourceUri)
		}

		netRegion := extractFromURI(net.Subnetwork, "regions")
		if netRegion == "" {
			log.Logger.Warnw("Unable to extract region from subnetwork",
				log.String("subnetwork", net.Subnetwork))
			continue
		}

		// Check Network Interface address to see if it exists as a resource
		ip := net.NetworkIP
		addrs, err := d.gceService.GetAddressByIP(projectID, netRegion, ip)
		if err != nil {
			log.Logger.Warnw("Error locating Address by IP",
				log.String("project", projectID),
				log.String("region", netRegion),
				log.String("ip", ip),
				log.Error(err))
			continue
		} else if len(addrs.Items) == 0 {
			log.Logger.Infow("No ComputeAddress with IP",
				log.String("IP", ip),
				log.String("project", projectID),
				log.String("region", netRegion))
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
		ir.RelatedResources = append(ir.RelatedResources, sr.ResourceUri)
	}
	return netRes
}

func (d *Discovery) discoverLoadBalancer(cp *ipb.CloudProperties) []*spb.Resource {
	log.Logger.Debug("Discovering load balancer")
	var res []*spb.Resource
	projectID, zone := cp.GetProjectId(), cp.GetZone()
	now := time.Now().Unix()
	lbAddress, err := d.discoverCluster()
	if err != nil || lbAddress == "" {
		log.Logger.Warnw("Encountered error discovering cluster address", log.Error(err))
		return res
	}

	// With address in hand we can find what it is assigned to
	region := strings.Join(strings.Split(zone, "-")[0:2], "-")
	// Check Network Interface address to see if it exists as a resource
	addrs, err := d.gceService.GetAddressByIP(projectID, region, lbAddress)
	if err != nil {
		log.Logger.Warnw("Error locating Address by IP",
			log.String("project", projectID),
			log.String("region", region),
			log.String("ip", lbAddress),
			log.Error(err))
		return res
	}
	if len(addrs.Items) == 0 {
		log.Logger.Infow("No ComputeAddress with IP",
			log.String("IP", lbAddress),
			log.String("project", projectID),
			log.String("region", region))
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

	// Examine the user of the address, it should be a forwarding rule.
	user := addr.Users[0]
	name := extractFromURI(user, "forwardingRules")
	if name == "" {
		log.Logger.Infow("Cluster address not in use by forwarding rule", log.String("user", user))
		return res
	}
	fwr, err := d.gceService.GetForwardingRule(projectID, region, name)
	if err != nil {
		log.Logger.Warnw("Error retrieving forwarding rule", log.Error(err))
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
		log.Logger.Infow("Forwarding rule does not have a backend service",
			log.String("bakendService", b))
		return res
	}

	bERegion := extractFromURI(b, "regions")
	if bERegion == "" {
		log.Logger.Infow("Unable to extract region from backend service", log.String("backendService", b))
		return res
	}

	bs, err := d.gceService.GetRegionalBackendService(projectID, bERegion, bEName)
	if err != nil {
		log.Logger.Warnw("Error retrieving backend service", log.Error(err))
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
			log.Logger.Info("Backend group is not an instance group")
			continue
		}
		gZone := extractFromURI(g, "zones")
		if gZone == "" {
			log.Logger.Info("Unable to extract zone from group name")
			continue
		}

		ig, err := d.gceService.GetInstanceGroup(projectID, gZone, gName)
		if err != nil {
			log.Logger.Warnw("Error retrieving instance group", log.Error(err))
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
		log.Logger.Warnw("Error retrieving instance group instances", log.Error(err))
		return res
	}

	var instances []string
	for _, i := range list.Items {
		parent.RelatedResources = append(parent.RelatedResources, i.Instance)
		iName := extractFromURI(i.Instance, "instances")
		if iName == "" {
			log.Logger.Warnw("Unable to extract instance name from instance group items",
				log.String("item", i.Instance))
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
			log.Logger.Warnw("Unable to extract instance name from instance group items", log.String("item", i))
			continue
		}
		iProject := extractFromURI(i, "projects")
		if iProject == "" {
			log.Logger.Warnw("Unable to extract project from instance group items", log.String("item", i))
			continue
		}
		iZone := extractFromURI(i, "zones")
		if iZone == "" {
			log.Logger.Warnw("Unable to extract zone from instance group items", log.String("item", i))
			continue
		}
		instanceRes, ci, ir := d.discoverInstance(iProject, iZone, iName)
		res = append(res, instanceRes...)

		netRes := d.discoverNetworks(cp.GetProjectId(), ci, ir)
		res = append(res, netRes...)

		disks := d.discoverDisks(cp.GetProjectId(), cp.GetZone(), ci, ir)
		res = append(res, disks...)

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
				address := ipRegex.FindString(l)
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
				address := ipRegex.FindString(l)
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

func (d *Discovery) discoverFilestores(projectID string, parent *spb.Resource) []*spb.Resource {
	log.Logger.Info("Discovering mounted file stores")
	var res []*spb.Resource
	if !d.exists("df") {
		log.Logger.Warn("Cannot access command df to discover mounted file stores")
		return res
	}

	stdOut, _, err := d.commandRunner("df", "-h")
	if err != nil {
		log.Logger.Warnw("Error retrieving mounts", "error", err)
		return res
	}
	for _, l := range strings.Split(stdOut, "\n") {
		matches := fsMountRegex.FindStringSubmatch(l)
		if len(matches) < 2 {
			continue
		}
		// The first match is the fully matched string, we only need the first submatch, the IP address.
		address := matches[1]
		fs, err := d.gceService.GetFilestoreByIP(projectID, "-", address)
		if err != nil {
			log.Logger.Errorw("Error retrieving filestore by IP", "error", err)
			continue
		} else if len(fs.Instances) == 0 {
			log.Logger.Warnw("No filestore found with IP", "address", address)
			continue
		}
		for _, i := range fs.Instances {
			fsr := &spb.Resource{
				ResourceType:     spb.Resource_STORAGE,
				ResourceKind:     "ComputeFilestore",
				ResourceUri:      i.Name,
				RelatedResources: []string{parent.ResourceUri},
			}
			parent.RelatedResources = append(parent.RelatedResources, fsr.ResourceUri)
			res = append(res, fsr)
		}
	}

	return res
}
