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

package workloadmanager

import (
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	"github.com/zieckey/goini"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/fake"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	monitoringresourcepb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	instancepb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// fakeDiskMapper provides a testable fake implementation of the DiskMapper interface
type fakeDiskMapper struct {
	err error
	out string
}

func wantDefaultHanaMetrics(ts *timestamppb.Timestamp, hanaExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/hana",
				Labels: map[string]string{},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: hanaExists,
					},
				},
			}},
		}},
	}
}

func wantHanaMetricsAllNegative(ts *timestamppb.Timestamp, hanaExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/hana",
				Labels: map[string]string{
					"fast_restart":          "disabled",
					"ha_sr_hook_configured": "no",
					"numa_balancing":        "disabled",
					"transparent_hugepages": "disabled",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: hanaExists,
					},
				},
			}},
		}},
	}
}

func wantHanaMetricsAllNegativeNoNumaOrHugePage(ts *timestamppb.Timestamp, hanaExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/hana",
				Labels: map[string]string{
					"fast_restart":          "disabled",
					"ha_sr_hook_configured": "no",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: hanaExists,
					},
				},
			}},
		}},
	}
}

func wantHanaMetricsAllPositive(ts *timestamppb.Timestamp, hanaExists float64, os string) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type: "workload.googleapis.com/sap/validation/hana",
				Labels: map[string]string{
					"fast_restart":          "enabled",
					"ha_sr_hook_configured": "yes",
					"numa_balancing":        "enabled",
					"transparent_hugepages": "enabled",
				},
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				Interval: &cpb.TimeInterval{
					StartTime: ts,
					EndTime:   ts,
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: hanaExists,
					},
				},
			}},
		}},
	}
}

func createHANAWorkloadMetrics(labels map[string]string, value float64) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*monitoringresourcepb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/hana",
				Labels: labels,
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*monitoringresourcepb.Point{{
				// We are choosing to ignore these timestamp values 
				// when performing a comparison via cmp.Diff().
				Interval: &cpb.TimeInterval{
					StartTime: &timestamppb.Timestamp{Seconds: time.Now().Unix()},
					EndTime:   &timestamppb.Timestamp{Seconds: time.Now().Unix()},
				},
				Value: &cpb.TypedValue{
					Value: &cpb.TypedValue_DoubleValue{
						DoubleValue: value,
					},
				},
			}},
		}},
	}
}

func (f *fakeDiskMapper) ForDeviceName(deviceName string) (string, error) {
	return deviceName, f.err
}

var (
	EmptyJSONDiskList = `
{
	 "blockdevices": null
}
`
	NoChildrenJSONDiskList = `
{
	"blockdevices": [{
		"name": "/dev/sdb",
		"type": "disk",
		"mountpoint": null,
		"size": 4096,
		"children": null
	}]
}
`
	DefaultJSONDiskList = `
{
   "blockdevices": [
			{
				 "name": "/dev/sdb",
				 "type": "disk",
				 "mountpoint": null,
				 "size": 4096,
				 "children": [
						{
							"name": "/dev/sdb1",
							"type": "part",
							"mountpoint": "/hana/data",
							"size": "2048"
						}
				 ]
      },{
         "name": "/dev/sda",
         "type": "disk",
         "mountpoint": null,
         "size": 80530636800,
         "children": [
            {
               "name": "/dev/sda1",
               "type": "part",
               "mountpoint": "/boot/efi",
               "size": "1999634432"
            },{
               "name": "/dev/sda2",
               "type": "part",
               "mountpoint": "/boot",
               "size": "1999634432"
            },
						{
               "name": "/dev/sda3",
               "type": "part",
               "mountpoint": null,
               "size": 76529270784,
               "children": [
                  {
                     "name": "/dev/mapper/glinux_20220510-root",
                     "type": "lvm",
                     "mountpoint": "/",
                     "size": 73765224448
                  },{
                     "name": "/dev/mapper/glinux_20220510-swap",
                     "type": "lvm",
                     "mountpoint": "[SWAP]",
                     "size": 2000683008
                  }
               ]
            }
         ]
      }
   ]
}
`
	defaultHanaINI = `
[ha_dr_provider_SAPHanaSR]

[persistance]
basepath_datavolumes = /hana/data/ISC
basepath_logvolumes = /hana/log/ISC
basepath_persistent_memory_volumes = /hana/memory/ISC
`
	defaultConfig = &configpb.Configuration{
		BareMetal: false,
		CloudProperties: &instancepb.CloudProperties{
			InstanceId: "test-instance-id",
			ProjectId:  "test-project-id",
			Zone:       "test-zone",
		},
	}
	defaultDiskMapper = &fakeDiskMapper{err: nil, out: "disk-mapping"}
	defaultMapperFunc = func() (map[instanceinfo.InterfaceName][]instanceinfo.NetworkAddress, error) {
		return map[instanceinfo.InterfaceName][]instanceinfo.NetworkAddress{"lo": []instanceinfo.NetworkAddress{instanceinfo.NetworkAddress(defaultNetworkIP)}}, nil
	}
	defaultNetworkIP = "127.0.0.1"

	defaultGCEService = &fake.TestGCE{
		GetDiskResp: []*compute.Disk{{
			Type: "/some/path/default-disk-type",
		}, {
			Type: "/some/path/default-disk-type",
		}, {
			Type: "/some/path/default-disk-type",
		}},
		GetDiskErr: []error{nil, nil, nil},
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
				{
					Source:     "",
					DeviceName: "other-disk-device-name",
					Type:       "SCRATCH",
				},
				{
					Source:     "/some/path/hana-disk-name",
					DeviceName: "sdb",
					Type:       "PERSISTENT",
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
		ListZoneOperationsResp: []*compute.OperationList{{
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
	}
	defaultIIR = instanceinfo.New(defaultDiskMapper, defaultGCEService)
)

func GlobalINITest1Runner(cmd string, args string) (string, string, error) {
	return "", "", nil
}

func GlobalINITest2Runner(cmd string, args string) (string, string, error) {
	if cmd == "/bin/sh" {
		return "Global INI contents", "", nil
	}
	return "", "", nil
}

func GlobalINITest3Runner(cmd string, args string) (string, string, error) {
	if cmd == "pidof" {
		return "12345", "", nil
	} else if cmd == "ps" {
		return "Invalid string", "", nil
	}
	return "", "", nil
}

func GlobalINITest4Runner(cmd string, args string) (string, string, error) {
	if cmd == "pidof" {
		return "12345", "", nil
	} else if cmd == "ps" {
		return "HDB_INFO", "", nil
	}
	return "", "", nil
}

func TestHanaProcessOrGlobalINI(t *testing.T) {
	tests := []struct {
		name    string
		fakeRun commandlineexecutor.CommandRunner
		want    string
	}{
		{
			name:    "GlobalINITest1",
			fakeRun: GlobalINITest1Runner,
			want:    "",
		},
		{
			name:    "GlobalINITest2",
			fakeRun: GlobalINITest2Runner,
			want:    "Global INI contents",
		},
		{
			name:    "GlobalINITest3",
			fakeRun: GlobalINITest3Runner,
			want:    "",
		},
		{
			name:    "GlobalINITest4",
			fakeRun: GlobalINITest4Runner,
			want:    "HDB_INFO",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hanaProcessOrGlobalINI(test.fakeRun)

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, hanaProcessOrGlobalINI returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestSetVolumeLabels(t *testing.T) {
	tests := []struct {
		name      string
		diskInfo  map[string]string
		dataOrLog string
		want      map[string]string
	}{
		{
			name: "VolumeLabelTest1",
			diskInfo: map[string]string{
				"instancedisktype": "ssd",
				"mountpoint":       "/dev/sda",
				"size":             "1024",
				"pdsize":           "8",
			},
			dataOrLog: "data",
			want: map[string]string{
				"disk_data_type":    "ssd",
				"disk_data_mount":   "/dev/sda",
				"disk_data_size":    "1024",
				"disk_data_pd_size": "8",
			},
		}, {
			name:      "VolumeLabelTest2",
			diskInfo:  map[string]string{},
			dataOrLog: "data",
			want:      map[string]string{},
		}, {
			name: "VolumeLabelTest3",
			diskInfo: map[string]string{
				"instancedisktype": "ssd",
				"mountpoint":       "/dev/sda",
				"size":             "4096",
				"pdsize":           "16",
			},
			dataOrLog: "log",
			want: map[string]string{
				"disk_log_type":    "ssd",
				"disk_log_mount":   "/dev/sda",
				"disk_log_size":    "4096",
				"disk_log_pd_size": "16",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l := map[string]string{}
			setVolumeLabels(l, test.diskInfo, test.dataOrLog)
			got := l
			want := test.want

			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestGlobalINILocation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "GlobalINILocation1",
			input: "/etc/config/test.ini",
			want:  "/etc/config/test.ini",
		},
		{
			name:  "GlobalINILocation2",
			input: "SYS/etc/config/test.ini",
			want:  "SYS/etc/config/test.ini",
		},
		{
			name:  "GlobalINILocation3",
			input: "/etc/config/test.ini hana_test",
			want:  "/etc/config/test.ini/SYS/global/hdb/custom/config/global.ini",
		},
		{
			name:  "GlobalINILocation4",
			input: "hana_test /etc/config/test.ini",
			want:  "hana_test/SYS/global/hdb/custom/config/global.ini",
		},
		{
			name:  "GlobalINILocation5",
			input: "/etc/config/HDB/dir/test.ini hana_test",
			want:  "/etc/config/SYS/global/hdb/custom/config/global.ini",
		},
		{
			name:  "NoGlobalINILocation",
			input: "",
			want:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := globalINILocation(test.input)
			want := test.want

			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestDiskInfo(t *testing.T) {
	tests := []struct {
		name              string
		basePathVolume    string
		globalINILocation string
		runner            commandlineexecutor.CommandRunner
		iir               *instanceinfo.Reader
		config            *configpb.Configuration
		mapper            instanceinfo.NetworkInterfaceAddressMapper
		want              map[string]string
	}{
		{
			name:              "TestDiskInfoNoGrep",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner:            func(string, string) (string, string, error) { return "", "", nil },
			iir:               defaultIIR,
			config:            defaultConfig,
			mapper:            defaultMapperFunc,
			want:              map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrep2",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner:            func(string, string) (string, string, error) { return defaultHanaINI, "", errors.New("Command failed") },
			iir:               defaultIIR,
			config:            defaultConfig,
			mapper:            defaultMapperFunc,
			want:              map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrepError",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(string, string) (string, string, error) {
				return defaultHanaINI, "", errors.New("Command failed")
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoBadINIFormat",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(string, string) (string, string, error) {
				return "basepath_datavolume /hana/data/ISC", "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return "", "", nil
				}
				return defaultHanaINI, "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput2",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return "This is invalid lsblk output", "", nil
				}
				return defaultHanaINI, "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput3",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return DefaultJSONDiskList, "", errors.New("This is invalid lsblk output")
				}
				return defaultHanaINI, "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoMatches",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return DefaultJSONDiskList, "", nil
				}
				return defaultHanaINI, "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoSomeMatches",
			basePathVolume:    "/dev/sdb",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return DefaultJSONDiskList, "", nil
				}
				return "basepath_datavolumes = /hana/data/ISC", "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want: map[string]string{
				"mountpoint":       "/hana/data",
				"instancedisktype": "default-disk-type",
				"size":             "2048",
				"pdsize":           "4096",
			},
		},
		{
			name:              "TestDiskInfoNoDevices",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return EmptyJSONDiskList, "", nil
				}
				return "basepath_datavolumes = /hana/data/ISC", "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoChildren",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "lsblk" {
					return NoChildrenJSONDiskList, "", nil
				}
				return "basepath_datavolumes = /hana/data/ISC", "", nil
			},
			iir:    defaultIIR,
			config: defaultConfig,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(test.config, test.mapper)
			got := diskInfo(test.basePathVolume, test.globalINILocation, test.runner, *test.iir)

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, diskInfo returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestSetDiskInfoForDevice(t *testing.T) {
	tests := []struct {
		name               string
		diskInfo           map[string]string
		matchedBlockDevice *lsblkdevice
		matchedMountPoint  string
		matchedSize        string
		iir                *instanceinfo.Reader
		config             *configpb.Configuration
		mapper             instanceinfo.NetworkInterfaceAddressMapper
		want               map[string]string
	}{
		{
			name:               "SetDiskInfoForDeviceNoMatches",
			diskInfo:           map[string]string{},
			matchedBlockDevice: &lsblkdevice{},
			matchedMountPoint:  "/dev/sda",
			matchedSize:        "1024",
			iir:                defaultIIR,
			config:             defaultConfig,
			mapper:             defaultMapperFunc,
			want:               map[string]string{},
		},
		{
			name:     "SetDiskInfoForDeviceMatch",
			diskInfo: map[string]string{},
			matchedBlockDevice: &lsblkdevice{
				Name: "disk-device-name",
				Size: []byte("2048"),
			},
			matchedMountPoint: "test-mount-point",
			matchedSize:       "1024",
			iir:               defaultIIR,
			config:            defaultConfig,
			mapper:            defaultMapperFunc,
			want: map[string]string{
				"mountpoint":       "test-mount-point",
				"instancedisktype": "default-disk-type",
				"size":             "1024",
				"pdsize":           "2048",
			},
		},
		{
			name: "SetDiskInfoForDeviceMatchExistingInfo",
			diskInfo: map[string]string{
				"testdiskinfo": "N/A",
			},
			matchedBlockDevice: &lsblkdevice{
				Name: "other-disk-device-name",
				Size: []byte("2048"),
			},
			matchedMountPoint: "test-mount-point",
			matchedSize:       "1024",
			iir:               defaultIIR,
			config:            defaultConfig,
			mapper:            defaultMapperFunc,
			want: map[string]string{
				"testdiskinfo":     "N/A",
				"mountpoint":       "test-mount-point",
				"instancedisktype": "default-disk-type",
				"size":             "1024",
				"pdsize":           "2048",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(test.config, test.mapper)
			setDiskInfoForDevice(test.diskInfo, test.matchedBlockDevice, test.matchedMountPoint, test.matchedSize, *test.iir)
			got := test.diskInfo

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, setDiskInfoForDevice returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestGrepKeyInGlobalINI(t *testing.T) {
	tests := []struct {
		name              string
		runner            commandlineexecutor.CommandRunner
		key               string
		globalINILocation string
		want              bool
	}{
		{
			name:              "TestGrepKeyInGlobalINIGrepError",
			runner:            func(string, string) (string, string, error) { return "", "", errors.New("Command failed") },
			key:               "disk_location",
			globalINILocation: "/etc/config/test.ini",
			want:              false,
		},
		{
			name:              "TestGrepKeyInGlobalINIGrepNoReturn",
			runner:            func(string, string) (string, string, error) { return "", "", nil },
			key:               "disk_location",
			globalINILocation: "/etc/config/test.ini",
			want:              false,
		},
		{
			name:              "TestGrepKeyInGlobalINIGrepSuccess",
			runner:            func(string, string) (string, string, error) { return "disk_location = /dev/sda", "", nil },
			key:               "disk_location",
			globalINILocation: "/etc/config/test.ini",
			want:              true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := grepKeyInGlobalINI(test.key, test.globalINILocation, test.runner)

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, grepKeyInGlobalINI returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCollectHanaMetrics(t *testing.T) {
	tests := []struct {
		name            string
		runtimeOS       string
		runner          commandlineexecutor.CommandRunner
		iir             *instanceinfo.Reader
		config          *configpb.Configuration
		mapper          instanceinfo.NetworkInterfaceAddressMapper
		osStatReader    OSStatReader
		wantOsVersion   string
		wantHanaExists  float64
		wantHanaMetrics func(*timestamppb.Timestamp, float64, string) WorkloadMetrics
	}{
		{
			name:            "TestHanaDoesNotExist",
			runtimeOS:       "linux",
			runner:          func(string, string) (string, string, error) { return "", "", nil },
			iir:             defaultIIR,
			config:          defaultConfig,
			mapper:          defaultMapperFunc,
			osStatReader:    os.Stat,
			wantOsVersion:   "test-os-version",
			wantHanaExists:  float64(0.0),
			wantHanaMetrics: wantDefaultHanaMetrics,
		},
		{
			name:      "TestHanaNoINI",
			runtimeOS: "linux",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				return "", "", nil
			},
			iir:             defaultIIR,
			config:          defaultConfig,
			mapper:          defaultMapperFunc,
			osStatReader:    os.Stat,
			wantOsVersion:   "test-os-version",
			wantHanaExists:  float64(0.0),
			wantHanaMetrics: wantDefaultHanaMetrics,
		},
		{
			name:      "TestHanaAllLabelsDisabled",
			runtimeOS: "linux",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				return "", "", nil
			},
			iir:             defaultIIR,
			config:          defaultConfig,
			mapper:          defaultMapperFunc,
			osStatReader:    func(string) (os.FileInfo, error) { return nil, nil },
			wantOsVersion:   "test-os-version",
			wantHanaExists:  float64(1.0),
			wantHanaMetrics: wantHanaMetricsAllNegative,
		},
		{
			name:      "TestHanaAllLabelsEnabled",
			runtimeOS: "linux",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				if cmd == "grep" {
					return "dummy key insert", "", nil
				}
				if cmd == "cat" {
					if args == "/proc/sys/kernel/numa_balancing" {
						return "1", "", nil
					}
					return "[always]", "", nil
				}
				return "", "", nil
			},
			iir:             defaultIIR,
			config:          defaultConfig,
			mapper:          defaultMapperFunc,
			osStatReader:    func(string) (os.FileInfo, error) { return nil, nil },
			wantOsVersion:   "test-os-version",
			wantHanaExists:  float64(1.0),
			wantHanaMetrics: wantHanaMetricsAllPositive,
		},
		{
			name:      "TestHanaLabelsDisabledFromErrors",
			runtimeOS: "linux",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				if cmd == "cat" {
					return "", "", errors.New("Command failed")
				}
				return "", "", nil
			},
			iir:             defaultIIR,
			config:          defaultConfig,
			mapper:          defaultMapperFunc,
			osStatReader:    func(string) (os.FileInfo, error) { return nil, nil },
			wantOsVersion:   "test-os-version",
			wantHanaExists:  float64(1.0),
			wantHanaMetrics: wantHanaMetricsAllNegativeNoNumaOrHugePage,
		},
	}

	iniParse = func(f string) *goini.INI {
		ini := goini.New()
		ini.Set("ID", "test-os")
		ini.Set("VERSION", "version")
		return ini
	}
	ip1, _ := net.ResolveIPAddr("ip", "192.168.0.1")
	ip2, _ := net.ResolveIPAddr("ip", "192.168.0.2")
	netInterfaceAdddrs = func() ([]net.Addr, error) {
		return []net.Addr{ip1, ip2}, nil
	}
	now = func() int64 {
		return int64(1660930735)
	}
	nts := &timestamppb.Timestamp{
		Seconds: now(),
	}
	osCaptionExecute = func() (string, string, error) {
		return "\n\nCaption=Microsoft Windows Server 2019 Datacenter \n   \n    \n", "", nil
	}
	osVersionExecute = func() (string, string, error) {
		return "\n Version=10.0.17763  \n\n", "", nil
	}
	cmdExists = func(c string) bool {
		return true
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(test.config, test.mapper)
			want := test.wantHanaMetrics(nts, test.wantHanaExists, test.wantOsVersion)

			hch := make(chan WorkloadMetrics)
			p := Parameters{
				Config:             cnf,
				OSStatReader:       test.osStatReader,
				InstanceInfoReader: *test.iir,
				CommandRunner:      test.runner,
				OSType:             test.runtimeOS,
			}
			go CollectHanaMetrics(p, hch)
			got := <-hch
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCollectHANAMetricsFromConfig(t *testing.T) {
	tests := []struct {
		name             string
		runner           commandlineexecutor.CommandRunner
		runnerNoSpace    commandlineexecutor.CommandRunnerNoSpace
		osStatReader     OSStatReader
		configFileReader ConfigFileReader
		wantHanaExists   float64
		wantLabels       map[string]string
	}{
		{
			name:             "TestHanaDoesNotExist",
			runner:           func(string, string) (string, string, error) { return "", "", nil },
			runnerNoSpace:    func(cmd string, args ...string) (string, string, error) { return "", "", nil },
			osStatReader:     func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: defaultFileReader,
			wantHanaExists:   float64(0.0),
			wantLabels:       map[string]string{},
		},
		{
			name: "TestHanaNoINI",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				return "", "", nil
			},
			osStatReader:     os.Stat,
			configFileReader: defaultFileReader,
			wantHanaExists:   float64(0.0),
			wantLabels:       map[string]string{},
		},
		{
			name: "StatReaderError",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				return "", "", nil
			},
			runnerNoSpace: func(cmd string, args ...string) (string, string, error) {
				return "", "", nil
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, errors.New("error") },
			configFileReader: defaultFileReader,
			wantHanaExists:   float64(0.0),
			wantLabels:       map[string]string{},
		},
		{
			name: "TestHanaAllLabelsDisabled",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				return "", "", nil
			},
			runnerNoSpace: func(cmd string, args ...string) (string, string, error) {
				return "", "", nil
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: defaultFileReader,
			wantHanaExists:   float64(1.0),
			wantLabels: map[string]string{
				"disk_data_mount":       "",
				"disk_data_pd_size":     "",
				"disk_data_size":        "",
				"disk_data_type":        "",
				"disk_log_mount":        "",
				"disk_log_pd_size":      "",
				"disk_log_size":         "",
				"disk_log_type":         "",
				"fast_restart":          "disabled",
				"ha_sr_hook_configured": "no",
				"numa_balancing":        "disabled",
				"transparent_hugepages": "disabled",
			},
		},
		{
			name: "TestHanaAllLabelsEnabled",
			runner: func(cmd string, args string) (string, string, error) {
				if cmd == "/bin/sh" {
					return "/etc/config/this_ini_does_not_exist.ini", "", nil
				}
				if cmd == "grep" {
					return "basepath location /hana/data", "", nil
				}
				if cmd == "lsblk" {
					return DefaultJSONDiskList, "", nil
				}
				return "", "", nil
			},
			runnerNoSpace: func(cmd string, args ...string) (string, string, error) {
				if cmd == "cat" {
					if args[0] == "/proc/sys/kernel/numa_balancing" {
						return "1", "", nil
					}
					return "[always]", "", nil
				}
				return "", "", nil
			},
			osStatReader: func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: ConfigFileReader(func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(defaultHanaINI)), nil
			}),
			wantHanaExists: float64(1.0),
			wantLabels: map[string]string{
				"disk_data_mount":       "/hana/data",
				"disk_data_pd_size":     "4096",
				"disk_data_size":        "2048",
				"disk_data_type":        "default-disk-type",
				"disk_log_mount":        "/hana/data",
				"disk_log_pd_size":      "4096",
				"disk_log_size":         "2048",
				"disk_log_type":         "default-disk-type",
				"fast_restart":          "enabled",
				"ha_sr_hook_configured": "yes",
				"numa_balancing":        "enabled",
				"transparent_hugepages": "enabled",
			},
		},
	}

	collectionDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(defaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			iir := defaultIIR
			iir.Read(defaultConfig, defaultMapperFunc)
			p := Parameters{
				Config:               cnf,
				OSStatReader:         test.osStatReader,
				InstanceInfoReader:   *iir,
				CommandRunner:        test.runner,
				CommandRunnerNoSpace: test.runnerNoSpace,
				OSType:               "linux",
				osVendorID:           "rhel",
				WorkloadConfig:       collectionDefinition.GetWorkloadValidation(),
				ConfigFileReader:     test.configFileReader,
			}

			want := createHANAWorkloadMetrics(test.wantLabels, test.wantHanaExists)
			got := CollectHANAMetricsFromConfig(p)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectHANAMetricsFromConfig() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
