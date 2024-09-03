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
	"context"
	"testing"

	compute "google.golang.org/api/compute/v1"
	file "google.golang.org/api/file/v1"

	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// GetDiskArguments is a struct to match arguments passed in to the GetDisk function for validation.
type GetDiskArguments struct{ Project, Zone, DiskName string }

// GetAddressByIPArguments is a struct to match arguments passed in to the GetAddressbyIP function for validation.
type GetAddressByIPArguments struct{ Project, Region, Subnetwork, Address string }

// GetForwardingRuleArguments is a struct to match arguments passed in to the GetForwardingRule function for validation.
type GetForwardingRuleArguments struct{ Project, Location, Name string }

// GetURIForIPArguments is a struct to match arguments passed in to the GetURIForIP function.
type GetURIForIPArguments struct{ Project, Region, Subnetwork, IP string }

// TestGCE implements GCE interfaces. A new TestGCE instance should be used per iteration of the test.
type TestGCE struct {
	T                    *testing.T
	GetInstanceResp      []*compute.Instance
	GetInstanceErr       []error
	GetInstanceCallCount int

	GetInstanceByIPResp      []*compute.Instance
	GetInstanceByIPErr       []error
	GetInstanceByIPCallCount int

	GetDiskResp        []*compute.Disk
	GetDiskArgs        []*GetDiskArguments
	GetDiskErr         []error
	GetDiskCallCount   int
	ListDisksResp      []*compute.DiskList
	ListDisksErr       []error
	ListDisksCallCount int

	ListZoneOperationsResp      []*compute.OperationList
	ListZoneOperationsErr       []error
	ListZoneOperationsCallCount int

	GetAddressResp      []*compute.Address
	GetAddressErr       []error
	GetAddressCallCount int

	GetAddressByIPResp      []*compute.Address
	GetAddressByIPArgs      []*GetAddressByIPArguments
	GetAddressByIPErr       []error
	GetAddressByIPCallCount int

	GetRegionalBackendServiceResp      []*compute.BackendService
	GetRegionalBackendServiceErr       []error
	GetRegionalBackendServiceCallCount int

	GetForwardingRuleResp       []*compute.ForwardingRule
	GetForwardingRuleErr        []error
	GetForwardingRuleArgs       []*GetForwardingRuleArguments
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

	GetURIForIPResp      []string
	GetURIForIPErr       []error
	GetURIForIPArgs      []*GetURIForIPArguments
	GetURIForIPCallCount int

	GetSecretResp      []string
	GetSecretErr       []error
	GetSecretCallCount int

	GetFilestoreResp      []*file.Instance
	GetFilestoreErr       []error
	GetFilestoreCallCount int

	GetHealthCheckResp      []*compute.HealthCheck
	GetHealthCheckErr       []error
	GetHealthCheckCallCount int

	DiskAttachedToInstanceDeviceName string
	IsDiskAttached                   bool
	DiskAttachedToInstanceErr        error

	CreationCompletionErr error
	UploadCompletionErr   error
	DiskOpErr             error

	AttachDiskErr error
	DetachDiskErr error

	CreateStandardSnapshotOp  *compute.Operation
	CreateStandardSnapshotErr error

	SnapshotList    *compute.SnapshotList
	SnapshotListErr error

	AddResourcePoliciesOp  *compute.Operation
	AddResourcePoliciesErr error

	RemoveResourcePoliciesOp  *compute.Operation
	RemoveResourcePoliciesErr error

	SetLabelsOp  *compute.Operation
	SetLabelsErr error
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

// ListDisks fakes a call to the compute API to retrieve disks.
func (g *TestGCE) ListDisks(project, zone, filter string) (*compute.DiskList, error) {
	defer func() {
		g.ListDisksCallCount++
		if g.ListDisksCallCount >= len(g.ListDisksResp) || g.ListDisksCallCount >= len(g.ListDisksErr) {
			g.ListDisksCallCount = 0
		}
	}()
	return g.ListDisksResp[g.ListDisksCallCount], g.ListDisksErr[g.ListDisksCallCount]
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

// GetAddress fakes a call to the compute API to retrieve a list of addresses.
func (g *TestGCE) GetAddress(project, location, name string) (*compute.Address, error) {
	defer func() {
		g.GetAddressCallCount++
		if g.GetAddressCallCount >= len(g.GetAddressResp) || g.GetAddressCallCount >= len(g.GetAddressByIPErr) {
			g.GetAddressCallCount = 0
		}
	}()
	return g.GetAddressResp[g.GetAddressCallCount], g.GetAddressErr[g.GetAddressCallCount]
}

// GetAddressByIP fakes a call to the compute API to retrieve a list of addresses.
func (g *TestGCE) GetAddressByIP(project, region, subnetwork, address string) (*compute.Address, error) {
	defer func() {
		g.GetAddressByIPCallCount++
		if g.GetAddressByIPCallCount >= len(g.GetAddressByIPResp) || g.GetAddressByIPCallCount >= len(g.GetAddressByIPErr) {
			g.GetAddressByIPCallCount = 0
		}
	}()
	if g.GetAddressByIPArgs != nil && len(g.GetAddressByIPArgs) > 0 {
		args := g.GetAddressByIPArgs[g.GetAddressByIPCallCount]
		if args != nil && (args.Project != project || args.Region != region || args.Address != address || args.Subnetwork != subnetwork) {
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
func (g *TestGCE) GetForwardingRule(project, location, name string) (*compute.ForwardingRule, error) {
	defer func() {
		g.GetForwardingRulleCallCount++
		if g.GetForwardingRulleCallCount >= len(g.GetForwardingRuleResp) || g.GetForwardingRulleCallCount >= len(g.GetForwardingRuleErr) {
			g.GetForwardingRulleCallCount = 0
		}
	}()
	if len(g.GetForwardingRuleArgs) > 0 {
		args := g.GetForwardingRuleArgs[g.GetForwardingRulleCallCount]
		if args != nil && (args.Project != project || args.Location != location || args.Name != name) {

			g.T.Errorf("Mismatch in expected arguments for GetForwardingRule: \ngot: (%s, %s, %s)\nwant:  (%s, %s, %s)", project, location, name, args.Project, args.Location, args.Name)
		}
	}
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

// GetInstanceByIP fakes a call to the compute API to retrieve a compute instance
// by its IP address.
func (g *TestGCE) GetInstanceByIP(project, ip string) (*compute.Instance, error) {
	defer func() {
		g.GetInstanceByIPCallCount++
		if g.GetInstanceByIPCallCount >= len(g.GetInstanceByIPResp) || g.GetInstanceByIPCallCount >= len(g.GetInstanceByIPErr) {
			g.GetInstanceByIPCallCount = 0
		}
	}()
	return g.GetInstanceByIPResp[g.GetInstanceByIPCallCount], g.GetInstanceByIPErr[g.GetInstanceByIPCallCount]
}

// GetURIForIP fakes calls to compute APIs to locate an object URI related to the IP address provided.
func (g *TestGCE) GetURIForIP(project, ip, region, subnetwork string) (string, error) {
	defer func() {
		g.GetURIForIPCallCount++
		if g.GetURIForIPCallCount >= len(g.GetURIForIPResp) || g.GetURIForIPCallCount >= len(g.GetURIForIPErr) {
			g.GetURIForIPCallCount = 0
		}
	}()
	if g.GetURIForIPArgs != nil && len(g.GetURIForIPArgs) > g.GetURIForIPCallCount {
		args := g.GetURIForIPArgs[g.GetURIForIPCallCount]
		gotArgs := &GetURIForIPArguments{
			Project:    project,
			Region:     region,
			Subnetwork: subnetwork,
			IP:         ip,
		}
		if args != nil && *args != *gotArgs {
			g.T.Errorf("Mismatch in expected arguments for GetURIForIP: \ngot: (%v)\nwant:  (%v)", gotArgs, args)
		}
	}
	return g.GetURIForIPResp[g.GetURIForIPCallCount], g.GetURIForIPErr[g.GetURIForIPCallCount]
}

// GetSecret fakes calls to secretmanager APIs to access a secret version.
func (g *TestGCE) GetSecret(ctx context.Context, projectID, secretName string) (string, error) {
	defer func() {
		g.GetSecretCallCount++
		if g.GetSecretCallCount >= len(g.GetSecretResp) || g.GetSecretCallCount >= len(g.GetSecretErr) {
			g.GetSecretCallCount = 0
		}
	}()
	return g.GetSecretResp[g.GetSecretCallCount], g.GetSecretErr[g.GetSecretCallCount]
}

// GetFilestore fakes calls to the cloud APIs to access a Filestore instance.
func (g *TestGCE) GetFilestore(projectID, zone, name string) (*file.Instance, error) {
	defer func() {
		g.GetFilestoreCallCount++
		if g.GetFilestoreCallCount >= len(g.GetFilestoreResp) || g.GetFilestoreCallCount >= len(g.GetFilestoreErr) {
			g.GetFilestoreCallCount = 0
		}
	}()
	return g.GetFilestoreResp[g.GetFilestoreCallCount], g.GetFilestoreErr[g.GetFilestoreCallCount]
}

// GetHealthCheck fakes calls to the cloud APIs to access a Health Check
func (g *TestGCE) GetHealthCheck(projectID, name string) (*compute.HealthCheck, error) {
	defer func() {
		g.GetHealthCheckCallCount++
		if g.GetHealthCheckCallCount >= len(g.GetHealthCheckResp) || g.GetHealthCheckCallCount >= len(g.GetHealthCheckErr) {
			g.GetHealthCheckCallCount = 0
		}
	}()
	return g.GetHealthCheckResp[g.GetHealthCheckCallCount], g.GetHealthCheckErr[g.GetHealthCheckCallCount]
}

// DiskAttachedToInstance fakes calls to the cloud APIs to access a disk attached to an instance.
func (g *TestGCE) DiskAttachedToInstance(project, zone, instanceName, diskName string) (string, bool, error) {
	return g.DiskAttachedToInstanceDeviceName, g.IsDiskAttached, g.DiskAttachedToInstanceErr
}

// WaitForSnapshotCreationCompletionWithRetry fakes calls to the cloud APIs to wait for a disk creation operation to complete.
func (g *TestGCE) WaitForSnapshotCreationCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	return g.CreationCompletionErr
}

// WaitForSnapshotUploadCompletionWithRetry fakes calls to the cloud APIs to wait for a disk upload operation to complete.
func (g *TestGCE) WaitForSnapshotUploadCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	return g.UploadCompletionErr
}

// AttachDisk fakes calls to the cloud APIs to attach a disk to an instance.
func (g *TestGCE) AttachDisk(ctx context.Context, diskName string, cp *ipb.CloudProperties, project, dataDiskZone string) error {
	return g.AttachDiskErr
}

// DetachDisk fakes calls to the cloud APIs to detach a disk from an instance.
func (g *TestGCE) DetachDisk(ctx context.Context, cp *ipb.CloudProperties, project, dataDiskZone, dataDiskName, dataDiskDeviceName string) error {
	return g.DetachDiskErr
}

// WaitForDiskOpCompletionWithRetry fakes calls to the cloud APIs to wait for a disk operation to complete.
func (g *TestGCE) WaitForDiskOpCompletionWithRetry(ctx context.Context, op *compute.Operation, project, dataDiskZone string) error {
	return g.DiskOpErr
}

// CreateStandardSnapshot fakes calls to the cloud APIs to create a standard snapshot.
func (g *TestGCE) CreateStandardSnapshot(ctx context.Context, project string, snapshotReq *compute.Snapshot) (*compute.Operation, error) {
	return g.CreateStandardSnapshotOp, g.CreateStandardSnapshotErr
}

// ListSnapshots fakes calls to the cloud APIs to list snapshots.
func (g *TestGCE) ListSnapshots(ctx context.Context, project string) (*compute.SnapshotList, error) {
	return g.SnapshotList, g.SnapshotListErr
}

// AddResourcePolicies fakes calls to the cloud APIs to add resource policies to a disk.
func (g *TestGCE) AddResourcePolicies(ctx context.Context, project, zone, diskName string, resourcePolicies []string) (*compute.Operation, error) {
	return g.AddResourcePoliciesOp, g.AddResourcePoliciesErr
}

// RemoveResourcePolicies fakes calls to the cloud APIs to remove resource policies from a disk.
func (g *TestGCE) RemoveResourcePolicies(ctx context.Context, project, zone, diskName string, resourcePolicies []string) (*compute.Operation, error) {
	return g.RemoveResourcePoliciesOp, g.RemoveResourcePoliciesErr
}

// SetLabels fakes calls to the cloud APIs to set labels on a disk.
func (g *TestGCE) SetLabels(ctx context.Context, project, zone, diskName, labelFingerprint string, labels map[string]string) (*compute.Operation, error) {
	return g.SetLabelsOp, g.SetLabelsErr
}
