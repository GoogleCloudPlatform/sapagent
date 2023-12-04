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

// Package gce is a lightweight testable wrapper around GCE compute APIs needed for the gcagent.
package gce

import (
	"context"
	"fmt"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/pkg/errors"
	smpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"
	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
)

// GCE is a wrapper for Google Compute Engine services.
type GCE struct {
	service *compute.Service
	file    *file.Service
	secret  *secretmanager.Client
}

// NewGCEClient creates a new GCE service wrapper.
func NewGCEClient(ctx context.Context) (*GCE, error) {
	s, err := compute.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error creating GCE client")
	}
	f, err := file.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error creating filestore client")
	}
	sm, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error creating secret manager client")
	}

	return &GCE{s, f, sm}, nil
}

// OverrideComputeBasePath overrides the base path of the GCE clients.
func (g *GCE) OverrideComputeBasePath(basePath string) {
	g.service.BasePath = basePath
}

// GetInstance retrieves a GCE Instance defined by the project, zone, and name provided.
func (g *GCE) GetInstance(project, zone, instance string) (*compute.Instance, error) {
	return g.service.Instances.Get(project, zone, instance).Do()
}

// GetInstanceByIP retrieves a GCE Instance defined by the project, and IP provided.
// May return nil if an instance with the corresponding IP cannot be found.
func (g *GCE) GetInstanceByIP(project, ip string) (*compute.Instance, error) {
	list, err := g.service.Instances.AggregatedList(project).Do()
	if err != nil {
		return nil, fmt.Errorf("error retrieving aggregated instance list: %s", err)
	}

	for _, l := range list.Items {
		for _, instance := range l.Instances {
			for _, n := range instance.NetworkInterfaces {
				if n.NetworkIP == ip {
					return instance, nil
				}
			}
		}
	}
	return nil, errors.Errorf("no instance with IP %s found", ip)
}

// GetDisk retrieves a GCE Persistent Disk defined by the project zone and name provided.
func (g *GCE) GetDisk(project, zone, disk string) (*compute.Disk, error) {
	return g.service.Disks.Get(project, zone, disk).Do()
}

// ListDisks retrieves GCE Persistent Disks defined by the project, sone, and filter provided.
func (g *GCE) ListDisks(project, zone, filter string) (*compute.DiskList, error) {
	return g.service.Disks.List(project, zone).Filter(filter).Do()
}

// ListZoneOperations retrieves a list of Operations resources defined by the project, and zone provided.
// Results will be filtered according to the provided filter string, and limit the number jof results to maxResults.
func (g *GCE) ListZoneOperations(project, zone, filter string, maxResults int64) (*compute.OperationList, error) {
	s := g.service.ZoneOperations.List(project, zone)
	if filter != "" {
		s = s.Filter(filter)
	}
	if maxResults > 0 {
		s = s.MaxResults(maxResults)
	}
	return s.Do()
}

// GetAddress retrieves a GCE Address defined by the project, location, and name provided.
func (g *GCE) GetAddress(project, location, name string) (*compute.Address, error) {
	if location == "" {
		return g.service.GlobalAddresses.Get(project, name).Do()
	}
	return g.service.Addresses.Get(project, location, name).Do()
}

// GetAddressByIP attempts to find a ComputeAddress object with a given IP.
// The string is assumed to be an exact match, so a full IPv4 address is expected.
// A region should be supplied, or "" to search for a global address.
func (g *GCE) GetAddressByIP(project, region, ip string) (*compute.Address, error) {
	filter := fmt.Sprintf("(address eq %s)", ip)
	if region == "" {
		list, err := g.service.Addresses.AggregatedList(project).Filter(filter).Do()
		if err != nil {
			return nil, err
		}

		for _, l := range list.Items {
			if len(l.Addresses) > 0 {
				return l.Addresses[0], nil
			}
		}

		return nil, errors.Errorf("No address with ip %s found", ip)
	}

	list, err := g.service.Addresses.List(project, region).Filter(filter).Do()
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, errors.Errorf("No address with IP %s found", ip)
	}

	return list.Items[0], nil
}

// GetRegionalBackendService retrieves a GCE Backend Service defined by the project, region, and name provided.
func (g *GCE) GetRegionalBackendService(project, region, service string) (*compute.BackendService, error) {
	return g.service.RegionBackendServices.Get(project, region, service).Do()
}

// GetForwardingRule retrieves a GCE Forwarding rule defined by the project, zone, and name provided.
func (g *GCE) GetForwardingRule(project, location, name string) (*compute.ForwardingRule, error) {
	return g.service.ForwardingRules.Get(project, location, name).Do()
}

// GetInstanceGroup retrieves a GCE Instance Group rule defined by the project, zone, and name provided.
func (g *GCE) GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error) {
	return g.service.InstanceGroups.Get(project, zone, name).Do()
}

// ListInstanceGroupInstances retrieves a list of GCE Instances in the Instance group defined by the project, zone, and name provided.
func (g *GCE) ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error) {
	return g.service.InstanceGroups.ListInstances(project, zone, name, nil).Do()
}

// GetFilestoreInstance retrieves a GCE Filestore Instance defined by the project, location, and name provided.
func (g *GCE) GetFilestoreInstance(project, location, filestore string) (*file.Instance, error) {
	name := fmt.Sprintf("projects/%s/locations/%s/instances/%s", project, location, filestore)
	return g.file.Projects.Locations.Instances.Get(name).Do()
}

// GetFilestoreByIP attempts to locate a GCE Filestore instance defined by the project, location, and IP Address provided.
func (g *GCE) GetFilestoreByIP(project, location, ip string) (*file.ListInstancesResponse, error) {
	name := fmt.Sprintf("projects/%s/locations/%s", project, location)
	return g.file.Projects.Locations.Instances.List(name).Filter(fmt.Sprintf("networks.ipAddresses:%q", ip)).Do()
}

// GetURIForIP attempts to locate the URI for any object that is related to the IP address provided.
func (g *GCE) GetURIForIP(project, ip string) (string, error) {
	addr, _ := g.GetAddressByIP(project, "", ip)
	if addr != nil {
		return addr.SelfLink, nil
	}
	inst, err := g.GetInstanceByIP(project, ip)
	if inst != nil {
		return inst.SelfLink, nil
	}
	fs, err := g.GetFilestoreByIP(project, "-", ip)
	if fs != nil && len(fs.Instances) > 0 {
		fsURI := strings.Replace(fs.Instances[0].Name, "/instances/", "/filestores/", 1)
		return fsURI, nil
	}
	return "", errors.Errorf("error locating object by IP: %v", err)
}

// GetSecret accesses the secret manager for the specified project ID and returns the stored password.
func (g *GCE) GetSecret(ctx context.Context, projectID, secretName string) (string, error) {
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName)
	result, err := g.secret.AccessSecretVersion(ctx, &smpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return "", err
	}
	return string(result.Payload.Data), nil
}

// GetFilestore attempts to retrieve the filestore instance addressed by the provided project, location, and name.
func (g *GCE) GetFilestore(project, zone, name string) (*file.Instance, error) {
	fsName := fmt.Sprintf("projects/%s/locations/%s/instances/%s", project, zone, name)
	return g.file.Projects.Locations.Instances.Get(fsName).Do()
}

// GetHealthCheck attempts to retrieve the compute HealthCheck object addressed by the provided project and name.
func (g *GCE) GetHealthCheck(project, name string) (*compute.HealthCheck, error) {
	return g.service.HealthChecks.Get(project, name).Do()
}

// DiskAttachedToInstance returns the device name of the disk attached to the instance.
func (g *GCE) DiskAttachedToInstance(project, zone, instanceName, diskName string) (string, bool, error) {
	instance, err := g.service.Instances.Get(project, zone, instanceName).Do()
	if err != nil {
		return "", false, fmt.Errorf("failed to get instance: %v", err)
	}
	for _, disk := range instance.Disks {
		if strings.Contains(disk.Source, diskName) {
			return disk.DeviceName, true, nil
		}
	}
	return "", false, nil
}
