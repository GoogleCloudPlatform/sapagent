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

package instanceinfo

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"

	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// fakeDiskMapper provides a testable fake implementation of the DiskMapper interface
type fakeDiskMapper struct {
	err error
	out string
}

func (f *fakeDiskMapper) ForDeviceName(ctx context.Context, deviceName string) (string, error) {
	return f.out, f.err
}

var (
	defaultConfig = &configpb.Configuration{
		BareMetal: false,
		CloudProperties: &instancepb.CloudProperties{
			InstanceId: "test-instance-id",
			ProjectId:  "test-project-id",
			Zone:       "test-zone",
		},
	}
	defaultDiskMapper = &fakeDiskMapper{err: nil, out: "disk-mapping"}
	defaultMapperFunc = func() (map[InterfaceName][]NetworkAddress, error) {
		return map[InterfaceName][]NetworkAddress{"lo": []NetworkAddress{NetworkAddress(defaultNetworkIP)}}, nil
	}
	defaultNetworkIP = "127.0.0.1"
)

func TestInstanceProperties(t *testing.T) {
	want := &instancepb.InstanceProperties{
		MachineType:       "test-machine-type",
		CpuPlatform:       "test-cpu-platform",
		Disks:             []*instancepb.Disk{},
		NetworkAdapters:   []*instancepb.NetworkAdapter{},
		CreationTimestamp: "test-creation-timestamp",
	}
	reader := &Reader{instanceProperties: want}
	got := reader.InstanceProperties()
	if d := cmp.Diff(want, got, protocmp.Transform()); d != "" {
		t.Errorf("InstanceProperties() mismatch (-want, +got):\n%s", d)
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name       string
		config     *configpb.Configuration
		dm         *fakeDiskMapper
		gceService *fake.TestGCE
		mapper     NetworkInterfaceAddressMapper
		want       *instancepb.InstanceProperties
	}{
		{
			name:   "success",
			config: defaultConfig,
			dm:     defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp: []*compute.Disk{{Type: "/some/path/device-type"}},
				GetDiskErr:  []error{nil},
				GetInstanceResp: []*compute.Instance{
					{
						MachineType:       "test-machine-type",
						CpuPlatform:       "test-cpu-platform",
						CreationTimestamp: "test-creation-timestamp",
						Disks: []*compute.AttachedDisk{
							{
								Source:     "/some/path/disk-name",
								DeviceName: "disk-device-name",
								Type:       "PERSISTENT",
							},
							{
								Source:     "",
								DeviceName: "disk-device-name",
								Type:       "SCRATCH",
							},
						},
						NetworkInterfaces: []*compute.NetworkInterface{
							{
								Name:      "network-name",
								Network:   "test-network",
								NetworkIP: defaultNetworkIP,
							},
						},
					},
				},
				GetInstanceErr: []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{
					{
						Items: []*compute.Operation{
							{
								EndTime: "2022-08-23T12:00:01.000-04:00",
							},
							{
								EndTime: "2022-08-23T12:00:00.000-04:00",
							},
						},
					},
				},
				ListZoneOperationsErr: []error{nil},
			},
			mapper: defaultMapperFunc,
			want: &instancepb.InstanceProperties{
				MachineType:       "test-machine-type",
				CpuPlatform:       "test-cpu-platform",
				CreationTimestamp: "test-creation-timestamp",
				Disks: []*instancepb.Disk{
					&instancepb.Disk{
						Type:       "PERSISTENT",
						DeviceType: "device-type",
						DeviceName: "disk-device-name",
						IsLocalSsd: false,
						DiskName:   "disk-name",
						Mapping:    "disk-mapping",
					},
					&instancepb.Disk{
						Type:       "SCRATCH",
						DeviceType: "local-ssd",
						DeviceName: "disk-device-name",
						IsLocalSsd: true,
						DiskName:   "disk-device-name",
						Mapping:    "disk-mapping",
					},
				},
				NetworkAdapters: []*instancepb.NetworkAdapter{
					&instancepb.NetworkAdapter{
						Name:      "network-name",
						Network:   "test-network",
						NetworkIp: defaultNetworkIP,
						Mapping:   "lo",
					},
				},
				LastMigrationEndTimestamp: "2022-08-23T12:00:01.000-04:00",
			},
		},
		{
			name: "bareMetal",
			config: &configpb.Configuration{
				BareMetal: true,
				CloudProperties: &instancepb.CloudProperties{
					InstanceId: "test-instance-id",
					ProjectId:  "test-project-id",
					Zone:       "test-zone",
				},
			},
			dm: defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp:            []*compute.Disk{nil},
				GetDiskErr:             []error{nil},
				GetInstanceResp:        []*compute.Instance{nil},
				GetInstanceErr:         []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{nil},
				ListZoneOperationsErr:  []error{nil},
			},
			mapper: defaultMapperFunc,
			want:   &instancepb.InstanceProperties{},
		},
		{
			name:   "nilCloudProperties",
			config: &configpb.Configuration{},
			dm:     defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp:            []*compute.Disk{nil},
				GetDiskErr:             []error{nil},
				GetInstanceResp:        []*compute.Instance{nil},
				GetInstanceErr:         []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{nil},
			},
			mapper: defaultMapperFunc,
			want:   &instancepb.InstanceProperties{},
		},
		{
			name:   "errInstancesGet",
			config: defaultConfig,
			dm:     defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp:            []*compute.Disk{nil},
				GetDiskErr:             []error{nil},
				GetInstanceResp:        []*compute.Instance{nil},
				GetInstanceErr:         []error{errors.New("Instances.Get error")},
				ListZoneOperationsResp: []*compute.OperationList{nil},
				ListZoneOperationsErr:  []error{nil},
			},
			mapper: defaultMapperFunc,
			want:   &instancepb.InstanceProperties{},
		},
		{
			name:   "errDisksGet",
			config: defaultConfig,
			dm:     &fakeDiskMapper{err: errors.New("disk mapping error"), out: ""},
			gceService: &fake.TestGCE{
				GetDiskResp: []*compute.Disk{nil},
				GetDiskErr:  []error{errors.New("Disks.Get error")},
				GetInstanceResp: []*compute.Instance{{
					MachineType:       "test-machine-type",
					CpuPlatform:       "test-cpu-platform",
					CreationTimestamp: "test-creation-timestamp",
					Disks: []*compute.AttachedDisk{
						{
							Source:     "/some/path/disk-name",
							DeviceName: "disk-device-name",
							Type:       "PERSISTENT",
						},
					},
					NetworkInterfaces: []*compute.NetworkInterface{},
				}},
				GetInstanceErr: []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{{
					Items: []*compute.Operation{},
				}},
				ListZoneOperationsErr: []error{nil},
			},
			mapper: defaultMapperFunc,
			want: &instancepb.InstanceProperties{
				MachineType:       "test-machine-type",
				CpuPlatform:       "test-cpu-platform",
				CreationTimestamp: "test-creation-timestamp",
				Disks: []*instancepb.Disk{
					&instancepb.Disk{
						Type:       "PERSISTENT",
						DeviceType: "unknown",
						DeviceName: "disk-device-name",
						IsLocalSsd: false,
						DiskName:   "disk-name",
						Mapping:    "unknown",
					},
				},
			},
		},
		{
			name:   "errNetworkMapping",
			config: defaultConfig,
			dm:     defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp: []*compute.Disk{nil},
				GetDiskErr:  []error{nil},
				GetInstanceResp: []*compute.Instance{{
					MachineType:       "test-machine-type",
					CpuPlatform:       "test-cpu-platform",
					CreationTimestamp: "test-creation-timestamp",
					NetworkInterfaces: []*compute.NetworkInterface{
						{
							Name:      "network-name",
							Network:   "test-network",
							NetworkIP: defaultNetworkIP,
						},
					},
				}},
				GetInstanceErr: []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{{
					Items: []*compute.Operation{},
				}},
				ListZoneOperationsErr: []error{nil},
			},
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return nil, errors.New("NetworkMapping error")
			},
			want: &instancepb.InstanceProperties{
				MachineType:       "test-machine-type",
				CpuPlatform:       "test-cpu-platform",
				CreationTimestamp: "test-creation-timestamp",
				NetworkAdapters: []*instancepb.NetworkAdapter{
					&instancepb.NetworkAdapter{
						Name:      "network-name",
						Network:   "test-network",
						NetworkIp: defaultNetworkIP,
						Mapping:   "",
					},
				},
			},
		},
		{
			name:   "errZoneOperationsList",
			config: defaultConfig,
			dm:     defaultDiskMapper,
			gceService: &fake.TestGCE{
				GetDiskResp: []*compute.Disk{nil},
				GetDiskErr:  []error{nil},
				GetInstanceResp: []*compute.Instance{{
					MachineType:       "test-machine-type",
					CpuPlatform:       "test-cpu-platform",
					CreationTimestamp: "test-creation-timestamp",
					Disks:             []*compute.AttachedDisk{},
					NetworkInterfaces: []*compute.NetworkInterface{},
				}},
				GetInstanceErr:         []error{nil},
				ListZoneOperationsResp: []*compute.OperationList{nil},
				ListZoneOperationsErr:  []error{errors.New("ZoneOperations.List error")},
			},
			mapper: defaultMapperFunc,
			want: &instancepb.InstanceProperties{
				MachineType:       "test-machine-type",
				CpuPlatform:       "test-cpu-platform",
				CreationTimestamp: "test-creation-timestamp",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := New(test.dm, test.gceService)
			r.Read(context.Background(), test.config, test.mapper)
			got := r.InstanceProperties()

			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("Read() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}
