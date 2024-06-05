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

// Package instanceinfo provides functionality for interfacing with the compute API.
package instanceinfo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	compute "google.golang.org/api/compute/v1"

	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// The DiskMapper interface is a wrapper which allows for ease of testing.
type DiskMapper interface {
	ForDeviceName(context.Context, string) (string, error)
}

type gceInterface interface {
	GetInstance(project, zone, instance string) (*compute.Instance, error)
	ListZoneOperations(project, zone, filter string, maxResults int64) (*compute.OperationList, error)
	GetDisk(project, zone, name string) (*compute.Disk, error)
	ListDisks(project, zone, filter string) (*compute.DiskList, error)
}

// Reader handles the retrieval of instance properties from a compute client instance.
type Reader struct {
	dm                 DiskMapper
	gceService         gceInterface
	instanceProperties *instancepb.InstanceProperties
}

// New instantiates a Reader with default instance properties.
func New(dm DiskMapper, gceService gceInterface) *Reader {
	return &Reader{
		dm:                 dm,
		gceService:         gceService,
		instanceProperties: &instancepb.InstanceProperties{},
	}
}

// InstanceProperties returns the currently set instance property information.
func (r *Reader) InstanceProperties() *instancepb.InstanceProperties {
	return r.instanceProperties
}

func (r *Reader) createDiskFilter(names []string) string {
	var f strings.Builder
	for c, n := range names {
		if c > 0 {
			f.WriteString(" OR ")
		}
		f.WriteString(fmt.Sprintf("(name=%s)", n))
	}
	return f.String()
}

func (r *Reader) getDiskData(disks *compute.DiskList, diskName string) *compute.Disk {
	if disks == nil {
		return nil
	}
	for _, disk := range disks.Items {
		if disk.Name == diskName {
			return disk
		}
	}
	return nil
}

// Read queries instance information using the compute API and stores the result as instanceProperties.
func (r *Reader) Read(ctx context.Context, config *configpb.Configuration, mapper NetworkInterfaceAddressMapper) {
	instance, builder, err := r.ReadDiskMapping(ctx, config)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not read disk mapping", "error", err)
		return
	}

	for _, networkInterface := range instance.NetworkInterfaces {
		mapping, err := networkMappingForInterface(networkInterface, mapper)
		if err != nil {
			log.CtxLogger(ctx).Warnw("No mapping set for network", "name", networkInterface.Name, "ip", networkInterface.NetworkIP, "error", err)
		}
		builder.NetworkAdapters = append(builder.NetworkAdapters, &instancepb.NetworkAdapter{
			Name:      networkInterface.Name,
			Network:   networkInterface.Network,
			NetworkIp: networkInterface.NetworkIP,
			Mapping:   mapping,
		})
	}

	// Get last migration info if available.
	operationList, err := r.gceService.ListZoneOperations(
		config.GetCloudProperties().GetProjectId(),
		config.GetCloudProperties().GetZone(),
		fmt.Sprintf(`(targetId eq %s) (status eq DONE) (operationType eq compute.instances.migrateOnHostMaintenance)`, config.GetCloudProperties().GetInstanceId()),
		1,
	)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not get zone operation list from compute API", "project",
			config.GetCloudProperties().GetProjectId(), "zone", config.GetCloudProperties().GetZone(),
			"instanceid", config.GetCloudProperties().GetInstanceId(), "error", err)
	} else if len(operationList.Items) > 0 {
		// Sort by EndTime and use the last (most recent) entry.
		items := endTimeSort(operationList.Items)
		sort.Sort(items)
		builder.LastMigrationEndTimestamp = items[len(items)-1].EndTime
	}
	r.instanceProperties = builder
}

// ReadDiskMapping queries instance information using the compute API and stores the result as instanceProperties.
func (r *Reader) ReadDiskMapping(ctx context.Context, config *configpb.Configuration) (*compute.Instance, *instancepb.InstanceProperties, error) {
	if config.GetBareMetal() {
		return nil, nil, fmt.Errorf("bare Metal configured, cannot get instance information from the Compute API")
	}

	cp := config.GetCloudProperties()
	if cp == nil {
		return nil, nil, fmt.Errorf("no Metadata Cloud Properties found, cannot collect instance information from the Compute API")

	}

	// Nil check before dereferencing to avoid panics.
	if r.dm == nil || r.gceService == nil {
		log.CtxLogger(ctx).Debug("")
		return nil, nil, fmt.Errorf("disk mapper and GCE service must be non-nil to read instance info")
	}

	projectID, zone, instanceID := cp.GetProjectId(), cp.GetZone(), cp.GetInstanceId()
	instance, err := r.gceService.GetInstance(projectID, zone, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("could not get instance info from the Compute API, error: %v", err)
	}

	builder := instancepb.InstanceProperties{
		MachineType:       instance.MachineType,
		CpuPlatform:       instance.CpuPlatform,
		CreationTimestamp: instance.CreationTimestamp,
	}

	diskNames := []string{}
	for _, disk := range instance.Disks {
		source, diskName := disk.Source, disk.DeviceName
		if source != "" {
			s := strings.Split(source, "/")
			diskName = s[len(s)-1]
		}
		diskNames = append(diskNames, diskName)
	}
	f := r.createDiskFilter(diskNames)
	disks, err := r.gceService.ListDisks(projectID, zone, f)
	if err != nil {
		log.Logger.Errorw("Could not get disk info from the Compute API", "project", projectID, "zone", zone, "filter", f, "error", err)
	}

	for _, disk := range instance.Disks {
		source, diskName := disk.Source, disk.DeviceName
		if source != "" {
			s := strings.Split(source, "/")
			diskName = s[len(s)-1]
		}

		mapping, err := r.dm.ForDeviceName(ctx, disk.DeviceName)
		if err != nil {
			log.CtxLogger(ctx).Warnw("No mapping for instance disk", "disk", disk, "error", err)
			mapping = "unknown"
		}
		log.CtxLogger(ctx).Debugw("Instance disk is mapped to device name", "devicename", disk.DeviceName, "mapping", mapping)
		diskData := r.getDiskData(disks, diskName)
		var pIops int64 = 0
		var pThroughput int64 = 0
		if diskData != nil {
			pIops = diskData.ProvisionedIops
			pThroughput = diskData.ProvisionedThroughput
		}
		builder.Disks = append(builder.Disks, &instancepb.Disk{
			Type:                  disk.Type,
			DeviceType:            r.getDeviceType(disk.Type, diskData),
			DeviceName:            disk.DeviceName,
			IsLocalSsd:            disk.Type == "SCRATCH",
			DiskName:              diskName,
			Mapping:               mapping,
			ProvisionedIops:       pIops,
			ProvisionedThroughput: pThroughput,
		})
	}

	log.CtxLogger(ctx).Debugw("Instance properties:", "instanceProperties", instance)
	return instance, &builder, nil
}

// getDeviceType returns a formatted device type for a given disk type and name.
//
// The Disk.Type value returned by the compute API is of the form:
// https://www.googleapis.com/compute/v1/projects/sap-netweaver/zones/us-central1-a/diskTypes/pd-standard
//
// The returned device type will be formatted as: "pd-standard".
func (r *Reader) getDeviceType(diskType string, diskData *compute.Disk) string {
	if diskType == "SCRATCH" {
		return "local-ssd"
	}
	if diskData == nil {
		return "unknown"
	}
	s := strings.Split(diskData.Type, "/")
	return s[len(s)-1]
}

// endTimeSort implements sort.Interface, sorting by EndTime asc.
type endTimeSort []*compute.Operation

func (s endTimeSort) Len() int {
	return len(s)
}

func (s endTimeSort) Less(i, j int) bool {
	return s[i].EndTime < s[j].EndTime
}

func (s endTimeSort) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
