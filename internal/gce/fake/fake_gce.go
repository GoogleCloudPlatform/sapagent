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

// Package fake provides a fake version of the GCE struct to return canned responses in unit tests.
package fake

import (
	"testing"

	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"
)

// GetDiskArguments is a struct to match arguments passed in to the GetDisk function for validation.
type GetDiskArguments struct{ Project, Zone, DiskName string }

// GetAddressByIPArguments is a struct to match arguments passed in to the GetAddressbyIP function for validation.
type GetAddressByIPArguments struct{ Project, Region, Address string }

// TestGCE implements GCE interfaces. A new TestGCE instance should be used per iteration of the test.
type TestGCE struct {
	T                    *testing.T
	GetInstanceResp      []*compute.Instance
	GetInstanceErr       []error
	GetInstanceCallCount int

	GetDiskResp      []*compute.Disk
	GetDiskArgs      []*GetDiskArguments
	GetDiskErr       []error
	GetDiskCallCount int

	ListZoneOperationsResp      []*compute.OperationList
	ListZoneOperationsErr       []error
	ListZoneOperationsCallCount int

	GetAddressByIPResp      []*compute.AddressList
	GetAddressByIPArgs      []*GetAddressByIPArguments
	GetAddressByIPErr       []error
	GetAddressByIPCallCount int

	GetRegionalBackendServiceResp      []*compute.BackendService
	GetRegionalBackendServiceErr       []error
	GetRegionalBackendServiceCallCount int

	GetForwardingRuleResp       []*compute.ForwardingRule
	GetForwardingRuleErr        []error
	GetForwardingRulleCallCount int

	GetInstanceGroupResp      []*compute.InstanceGroup
	GetInstanceGroupErr       []error
	GetInstanceGroupCallCount int

	ListInstanceGroupInstancesResp      []*compute.InstanceGroupsListInstances
	ListInstanceGroupInstancesErr       []error
	ListInstanceGroupInstancesCallCount int

	GetFilestoreByIPResp      []*file.ListInstancesResponse
	GetFilestoreByIPErr       []error
	GetFilestoreByIPCallCount int
}

// GetInstance fakes a call to the compute API to retrieve a GCE Instance.
func (g *TestGCE) GetInstance(project, zone, instance string) (*compute.Instance, error) {
	defer func() {
		g.GetInstanceCallCount++
		if g.GetInstanceCallCount >= len(g.GetInstanceResp) || g.GetInstanceCallCount >= len(g.GetInstanceErr) {
			g.GetInstanceCallCount = 0
		}
	}()
	return g.GetInstanceResp[g.GetInstanceCallCount], g.GetInstanceErr[g.GetInstanceCallCount]
}

// GetDisk fakes a call to the compute API to retrieve a GCE Persistent Disk.
func (g *TestGCE) GetDisk(project, zone, disk string) (*compute.Disk, error) {
	defer func() {
		g.GetDiskCallCount++
		if g.GetDiskCallCount >= len(g.GetDiskResp) || g.GetDiskCallCount >= len(g.GetDiskErr) {
			g.GetDiskCallCount = 0
		}
	}()
	if g.GetDiskArgs != nil && len(g.GetDiskArgs) > 0 {
		args := g.GetDiskArgs[g.GetDiskCallCount]
		if args != nil && (args.Project != project || args.Zone != zone || args.DiskName != disk) {

			g.T.Errorf("Mismatch in expected arguments for GetDisk: \ngot: (%s, %s, %s)\nwant:  (%s, %s, %s)", project, zone, disk, args.Project, args.Zone, args.DiskName)
		}
	}
	return g.GetDiskResp[g.GetDiskCallCount], g.GetDiskErr[g.GetDiskCallCount]
}

// ListZoneOperations  fakes a call to the compute API to retrieve a list of Operations resources.
func (g *TestGCE) ListZoneOperations(project, zone, filter string, maxResults int64) (*compute.OperationList, error) {
	defer func() {
		g.ListZoneOperationsCallCount++
		if g.ListZoneOperationsCallCount >= len(g.ListZoneOperationsResp) || g.ListZoneOperationsCallCount >= len(g.ListZoneOperationsErr) {
			g.ListZoneOperationsCallCount = 0
		}
	}()
	return g.ListZoneOperationsResp[g.ListZoneOperationsCallCount], g.ListZoneOperationsErr[g.ListZoneOperationsCallCount]
}

// GetAddressByIP fakes a call to the compute API to retrieve a list of addresses.
func (g *TestGCE) GetAddressByIP(project, region, address string) (*compute.AddressList, error) {
	defer func() {
		g.GetAddressByIPCallCount++
		if g.GetAddressByIPCallCount >= len(g.GetAddressByIPResp) || g.GetAddressByIPCallCount >= len(g.GetAddressByIPErr) {
			g.GetAddressByIPCallCount = 0
		}
	}()
	if g.GetAddressByIPArgs != nil && len(g.GetAddressByIPArgs) > 0 {
		args := g.GetAddressByIPArgs[g.GetAddressByIPCallCount]
		if args != nil && (args.Project != project || args.Region != region || args.Address != address) {
			g.T.Errorf("Mismatch in expected arguments for GetAddressByIP: \ngot: (%s, %s, %s)\nwant:  (%s, %s, %s)", project, region, address, args.Project, args.Region, args.Address)
		}
	}
	return g.GetAddressByIPResp[g.GetAddressByIPCallCount], g.GetAddressByIPErr[g.GetAddressByIPCallCount]
}

// GetRegionalBackendService fakes a call to the compute API to retrieve a regional backend service.
func (g *TestGCE) GetRegionalBackendService(project, region, name string) (*compute.BackendService, error) {
	defer func() {
		g.GetRegionalBackendServiceCallCount++
		if g.GetRegionalBackendServiceCallCount >= len(g.GetRegionalBackendServiceResp) || g.GetRegionalBackendServiceCallCount >= len(g.GetRegionalBackendServiceErr) {
			g.GetRegionalBackendServiceCallCount = 0
		}
	}()
	return g.GetRegionalBackendServiceResp[g.GetRegionalBackendServiceCallCount], g.GetRegionalBackendServiceErr[g.GetRegionalBackendServiceCallCount]
}

// GetForwardingRule fakes a call to the compute API to retrieve a forwarding rule.
func (g *TestGCE) GetForwardingRule(project, region, name string) (*compute.ForwardingRule, error) {
	defer func() {
		g.GetForwardingRulleCallCount++
		if g.GetForwardingRulleCallCount >= len(g.GetForwardingRuleResp) || g.GetForwardingRulleCallCount >= len(g.GetForwardingRuleErr) {
			g.GetForwardingRulleCallCount = 0
		}
	}()
	return g.GetForwardingRuleResp[g.GetForwardingRulleCallCount], g.GetForwardingRuleErr[g.GetForwardingRulleCallCount]
}

// GetInstanceGroup fakes a call to the compute API to retrieve an Instance Group.
func (g *TestGCE) GetInstanceGroup(project, zone, name string) (*compute.InstanceGroup, error) {
	defer func() {
		g.GetInstanceGroupCallCount++
		if g.GetInstanceGroupCallCount >= len(g.GetInstanceGroupResp) || g.GetInstanceGroupCallCount >= len(g.GetInstanceGroupErr) {
			g.GetInstanceGroupCallCount = 0
		}
	}()
	return g.GetInstanceGroupResp[g.GetInstanceGroupCallCount], g.GetInstanceGroupErr[g.GetInstanceGroupCallCount]
}

// ListInstanceGroupInstances fakes a call to the compute API to retrieve a list of instances
// in an instance group.
func (g *TestGCE) ListInstanceGroupInstances(project, zone, name string) (*compute.InstanceGroupsListInstances, error) {
	defer func() {
		g.ListInstanceGroupInstancesCallCount++
		if g.ListInstanceGroupInstancesCallCount >= len(g.ListInstanceGroupInstancesResp) || g.ListInstanceGroupInstancesCallCount >= len(g.ListInstanceGroupInstancesErr) {
			g.ListInstanceGroupInstancesCallCount = 0
		}
	}()
	return g.ListInstanceGroupInstancesResp[g.ListInstanceGroupInstancesCallCount], g.ListInstanceGroupInstancesErr[g.ListInstanceGroupInstancesCallCount]
}

// GetFilestoreByIP fakes a call to the compute API to retrieve a filestore instance
// by its IP address.
func (g *TestGCE) GetFilestoreByIP(project, location, ip string) (*file.ListInstancesResponse, error) {
	defer func() {
		g.GetFilestoreByIPCallCount++
		if g.GetFilestoreByIPCallCount >= len(g.GetFilestoreByIPResp) || g.GetFilestoreByIPCallCount >= len(g.GetFilestoreByIPErr) {
			g.GetFilestoreByIPCallCount = 0
		}
	}()
	return g.GetFilestoreByIPResp[g.GetFilestoreByIPCallCount], g.GetFilestoreByIPErr[g.GetFilestoreByIPCallCount]
}
