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
	"time"

	"github.com/cenkalti/backoff/v4"
	"cloud.google.com/go/secretmanager/apiv1"
	"github.com/pkg/errors"
	smpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/file/v1"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
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
func (g *GCE) GetAddressByIP(project, region, subnetwork, ip string) (*compute.Address, error) {
	filter := fmt.Sprintf("(address eq %s)", ip)
	if subnetwork != "" {
		filter += fmt.Sprintf(` (subnetwork eq ".*%s.*")`, subnetwork)
	}
	log.Logger.Debugw("GetAddressByIP", "project", project, "region", region, "subnetwork", subnetwork, "ip", ip, "filter", filter)
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

// GetForwardingRuleByIP retrieves a GCE Forwarding rule defined by the project, and IP address provided.
func (g *GCE) GetForwardingRuleByIP(project, ip string) (*compute.ForwardingRule, error) {
	filter := fmt.Sprintf("(IPAddress eq %s)", ip)
	list, err := g.service.ForwardingRules.AggregatedList(project).Filter(filter).Do()
	if err != nil {
		return nil, fmt.Errorf("error retrieving aggregated forwarding rules list: %s", err)
	}

	for _, l := range list.Items {
		for _, fwr := range l.ForwardingRules {
			if fwr.IPAddress == ip {
				return fwr, nil
			}
		}
	}
	return nil, errors.Errorf("no forwarding rule with IP %s found", ip)
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
func (g *GCE) GetURIForIP(project, ip, region, subnetwork string) (string, error) {
	log.Logger.Debugw("GetURIForIP", "project", project, "ip", ip, "region", region, "subnetwork", subnetwork)
	addr, _ := g.GetAddressByIP(project, "", subnetwork, ip)
	if addr != nil {
		return addr.SelfLink, nil
	}
	inst, _ := g.GetInstanceByIP(project, ip)
	if inst != nil {
		return inst.SelfLink, nil
	}

	fwr, _ := g.GetForwardingRuleByIP(project, ip)
	if fwr != nil {
		return fwr.SelfLink, nil
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
		log.Logger.Debugw("Disk attached to instance", "disk", disk.Source, "diskName", diskName)
		if strings.Contains(disk.Source, diskName) {
			return disk.DeviceName, true, nil
		}
	}
	return "", false, nil
}

// waitForSnapshotCreationCompletion waits for the given snapshot creation operation to complete.
func (g *GCE) waitForSnapshotCreationCompletion(ctx context.Context, op *compute.Operation, project, snapshotName string) error {
	ss, err := g.service.Snapshots.Get(project, snapshotName).Do()
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Snapshot creation status", "snapshot", snapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)
	if ss.Status == "CREATING" {
		return fmt.Errorf("snapshot creation is in progress, snapshot name: %s, status:  CREATING", snapshotName)
	}
	log.CtxLogger(ctx).Infow("Snapshot creation progress", "snapshot", snapshotName, "status", ss.Status)
	return nil
}

// WaitForSnapshotCreationCompletionWithRetry waits for the given compute operation to complete.
// We sleep for 1s between retries a total 300 times => max_wait_duration = 5 minutes
func (g *GCE) WaitForSnapshotCreationCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	constantBackoff := backoff.NewConstantBackOff(1 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 300), ctx)
	return backoff.Retry(func() error { return g.waitForSnapshotCreationCompletion(ctx, op, project, snapshotName) }, bo)
}

// waitForUploadCompletion waits for the given snapshot upload operation to complete.
func (g *GCE) waitForUploadCompletion(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	zos := compute.NewZoneOperationsService(g.service)
	tracker, err := zos.Wait(project, diskZone, op.Name).Do()
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name)
		return err
	}
	log.CtxLogger(ctx).Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("compute operation is not DONE yet")
	}
	if tracker.Error != nil {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "error", tracker.Error)
		return fmt.Errorf("compute operation failed with error: %v", tracker.Error)
	}
	if tracker.HttpErrorMessage != "" {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "errorStatusCode", tracker.HttpErrorStatusCode, "error", tracker.HttpErrorMessage)
		return fmt.Errorf("compute operation failed with error: %v", tracker.HttpErrorMessage)
	}

	ss, err := g.service.Snapshots.Get(project, snapshotName).Do()
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Snapshot upload status", "snapshot", snapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)

	if ss.Status == "READY" {
		return nil
	}
	return fmt.Errorf("snapshot %s not READY yet, snapshotStatus: %s, operationStatus: %s", snapshotName, ss.Status, op.Status)
}

// WaitForSnapshotUploadCompletionWithRetry waits for the given compute operation to complete.
// We sleep for 30s between retries a total 480 times => max_wait_duration = 30*480 = 4 Hours
func (g *GCE) WaitForSnapshotUploadCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	constantBackoff := backoff.NewConstantBackOff(30 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 480), ctx)
	return backoff.Retry(func() error { return g.waitForUploadCompletion(ctx, op, project, diskZone, snapshotName) }, bo)
}

// waitForDiskOpCompletion waits for the given disk operation to complete.
func (g *GCE) waitForDiskOpCompletion(ctx context.Context, op *compute.Operation, project, dataDiskZone string) error {
	zos := compute.NewZoneOperationsService(g.service)
	tracker, err := zos.Wait(project, dataDiskZone, op.Name).Do()
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("compute operation is not DONE yet")
	}
	if tracker.Error != nil {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "error", tracker.Error)
		return fmt.Errorf("compute operation failed with error: %v", tracker.Error)
	}
	if tracker.HttpErrorMessage != "" {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "errorStatusCode", tracker.HttpErrorStatusCode, "error", tracker.HttpErrorMessage)
		return fmt.Errorf("compute operation failed with error: %v", tracker.HttpErrorMessage)
	}
	return nil
}

// WaitForDiskOpCompletionWithRetry waits for the given compute operation to complete.
// We sleep for 120s between retries a total 10 times => max_wait_duration = 10*120 = 20 Minutes
func (g *GCE) WaitForDiskOpCompletionWithRetry(ctx context.Context, op *compute.Operation, project, dataDiskZone string) error {
	constantBackoff := backoff.NewConstantBackOff(120 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 10), ctx)
	return backoff.Retry(func() error { return g.waitForDiskOpCompletion(ctx, op, project, dataDiskZone) }, bo)
}

// AttachDisk attaches the disk with the given name to the instance.
func (g *GCE) AttachDisk(ctx context.Context, diskName string, cp *ipb.CloudProperties, project, dataDiskZone string) error {
	log.CtxLogger(ctx).Infow("Attaching disk", "diskName", diskName)
	attachDiskToVM := &compute.AttachedDisk{
		DeviceName: diskName, // Keep the device name and disk name same.
		Source:     fmt.Sprintf("projects/%s/zones/%s/disks/%s", project, dataDiskZone, diskName),
	}
	op, err := g.service.Instances.AttachDisk(project, dataDiskZone, cp.GetInstanceName(), attachDiskToVM).Do()
	if err != nil {
		return fmt.Errorf("failed to attach disk: %v", err)
	}
	if err := g.WaitForDiskOpCompletionWithRetry(ctx, op, project, dataDiskZone); err != nil {
		return fmt.Errorf("attach disk operation failed: %v", err)
	}
	return nil
}

// DetachDisk detaches given disk from the instance.
func (g *GCE) DetachDisk(ctx context.Context, cp *ipb.CloudProperties, project, dataDiskZone, dataDiskName, dataDiskDeviceName string) error {
	log.CtxLogger(ctx).Infow("Detatching disk", "diskName", dataDiskName, "deviceName", dataDiskDeviceName)
	op, err := g.service.Instances.DetachDisk(project, dataDiskZone, cp.GetInstanceName(), dataDiskDeviceName).Do()
	if err != nil {
		return fmt.Errorf("failed to detach old data disk: %v", err)
	}
	if err := g.WaitForDiskOpCompletionWithRetry(ctx, op, project, dataDiskZone); err != nil {
		return fmt.Errorf("detach data disk operation failed: %v", err)
	}

	_, ok, err := g.DiskAttachedToInstance(project, dataDiskZone, cp.GetInstanceName(), dataDiskName)
	if err != nil {
		return fmt.Errorf("failed to check if disk %v is still attached to the instance", dataDiskName)
	}
	if ok {
		return fmt.Errorf("Disk %v is still attached to the instance", dataDiskName)
	}
	return nil
}

// CreateSnapshot creates a new standard snapshot.
func (g *GCE) CreateSnapshot(ctx context.Context, project string, snapshotReq *compute.Snapshot) (*compute.Operation, error) {
	snapshotsService := compute.NewSnapshotsService(g.service)
	op, err := snapshotsService.Insert(project, snapshotReq).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot: %v", err)
	}
	return op, nil
}

// ListSnapshots lists the snapshots for a given project.
func (g *GCE) ListSnapshots(ctx context.Context, project string) (*compute.SnapshotList, error) {
	snapshotService := compute.NewSnapshotsService(g.service)
	finalSnapshotList := &compute.SnapshotList{}
	pageToken := ""

	for {
		snapshotListCall := snapshotService.List(project)
		if pageToken != "" {
			snapshotListCall = snapshotListCall.PageToken(pageToken)
		}

		snapshotList, err := snapshotListCall.Do()
		if err != nil {
			return nil, err
		}

		finalSnapshotList.Items = append(finalSnapshotList.Items, snapshotList.Items...)

		pageToken = snapshotList.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return finalSnapshotList, nil
}

// AddResourcePolicies adds the given resource policies of a disk.
func (g *GCE) AddResourcePolicies(ctx context.Context, project, zone, diskName string, resourcePolicies []string) (*compute.Operation, error) {
	disksService := compute.NewDisksService(g.service)
	op, err := disksService.AddResourcePolicies(project, zone, diskName, &compute.DisksAddResourcePoliciesRequest{ResourcePolicies: resourcePolicies}).Do()
	if err != nil {
		return nil, err
	}
	return op, nil
}

// RemoveResourcePolicies removes the given resource policies of a disk.
func (g *GCE) RemoveResourcePolicies(ctx context.Context, project, zone, diskName string, resourcePolicies []string) (*compute.Operation, error) {
	disksService := compute.NewDisksService(g.service)
	op, err := disksService.RemoveResourcePolicies(project, zone, diskName, &compute.DisksRemoveResourcePoliciesRequest{ResourcePolicies: resourcePolicies}).Do()
	if err != nil {
		return nil, err
	}
	return op, nil
}

// SetLabels sets the labels for a given disk.
func (g *GCE) SetLabels(ctx context.Context, project, zone, diskName, labelFingerprint string, labels map[string]string) (*compute.Operation, error) {
	disksService := compute.NewDisksService(g.service)
	op, err := disksService.SetLabels(project, zone, diskName, &compute.ZoneSetLabelsRequest{
		Labels:           labels,
		LabelFingerprint: labelFingerprint,
	}).Do()
	if err != nil {
		return nil, err
	}
	return op, nil
}

// waitForGlobalUploadCompletion waits for the given snapshot upload operation to complete.
func (g *GCE) waitForGlobalUploadCompletion(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	gos := compute.NewGlobalOperationsService(g.service)
	tracker, err := gos.Wait(project, op.Name).Do()
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "error", err)
		return err
	}
	log.CtxLogger(ctx).Infow("Operation in progress", "Name", op.Name, "percentage", tracker.Progress, "status", tracker.Status)
	if tracker.Status != "DONE" {
		return fmt.Errorf("compute operation is not DONE yet")
	}
	if tracker.Error != nil {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "error", tracker.Error)
		return fmt.Errorf("compute operation failed with error: %v", tracker.Error)
	}
	if tracker.HttpErrorMessage != "" {
		log.CtxLogger(ctx).Errorw("Error in operation", "operation", op.Name, "errorStatusCode", tracker.HttpErrorStatusCode, "error", tracker.HttpErrorMessage)
		return fmt.Errorf("compute operation failed with error: %v", tracker.HttpErrorMessage)
	}

	ss, err := g.service.Snapshots.Get(project, snapshotName).Do()
	if err != nil {
		return err
	}
	log.CtxLogger(ctx).Infow("Snapshot upload status", "snapshot", snapshotName, "SnapshotStatus", ss.Status, "OperationStatus", op.Status)

	if ss.Status == "READY" {
		return nil
	}
	return fmt.Errorf("snapshot %s not READY yet, snapshotStatus: %s, operationStatus: %s", snapshotName, ss.Status, op.Status)
}

// WaitForInstantSnapshotConversionCompletionWithRetry waits for the given compute operation to complete.
// We sleep for 30s between retries a total 480 times => max_wait_duration = 30*480 = 4 Hours
func (g *GCE) WaitForInstantSnapshotConversionCompletionWithRetry(ctx context.Context, op *compute.Operation, project, diskZone, snapshotName string) error {
	constantBackoff := backoff.NewConstantBackOff(30 * time.Second)
	bo := backoff.WithContext(backoff.WithMaxRetries(constantBackoff, 480), ctx)
	return backoff.Retry(func() error { return g.waitForGlobalUploadCompletion(ctx, op, project, diskZone, snapshotName) }, bo)
}

// GetNetwork retrieves the network with the given name and project.
func (g *GCE) GetNetwork(name, project string) (*compute.Network, error) {
	return g.service.Networks.Get(project, name).Do()
}

// GetSubnetwork retrieves the subnetwork with the given name, project, and region.
func (g *GCE) GetSubnetwork(name, project, region string) (*compute.Subnetwork, error) {
	return g.service.Subnetworks.Get(project, region, name).Do()
}
