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
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/fake"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// fakeDiskMapper provides a testable fake implementation of the DiskMapper interface
type fakeDiskMapper struct {
	err error
	out string
}

func createHANAWorkloadMetrics(labels map[string]string, value float64) WorkloadMetrics {
	return WorkloadMetrics{
		Metrics: []*mrpb.TimeSeries{{
			Metric: &metricpb.Metric{
				Type:   "workload.googleapis.com/sap/validation/hana",
				Labels: labels,
			},
			MetricKind: metricpb.MetricDescriptor_GAUGE,
			Resource: &monitoredresourcepb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance-id",
					"zone":        "test-region-zone",
					"project_id":  "test-project-id",
				},
			},
			Points: []*mrpb.Point{{
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

func (f *fakeDiskMapper) ForDeviceName(ctx context.Context, deviceName string) (string, error) {
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
		"mountpoint": "/hana/data",
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
	dfTargetData = `
Mounted on
/hana/data
`
	dfTargetLog = `
Mounted on
/hana/log
`
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

func GlobalINITest1Exec(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
	return commandlineexecutor.Result{
		StdOut: "",
		StdErr: "",
	}
}

func GlobalINITest2Exec(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	if params.Executable == "/bin/sh" {
		return commandlineexecutor.Result{
			StdOut: "Global INI contents",
			StdErr: "",
		}
	}
	return commandlineexecutor.Result{
		StdOut: "",
		StdErr: "",
	}
}

func GlobalINITest3Exec(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	if params.Executable == "pidof" {
		return commandlineexecutor.Result{
			StdOut: "12345",
			StdErr: "",
		}
	} else if params.Executable == "ps" {
		return commandlineexecutor.Result{
			StdOut: "Invalid string",
			StdErr: "",
		}
	}
	return commandlineexecutor.Result{
		StdOut: "",
		StdErr: "",
	}
}

func GlobalINITest4Exec(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
	if params.Executable == "pidof" {
		return commandlineexecutor.Result{
			StdOut: "12345",
			StdErr: "",
		}
	} else if params.Executable == "ps" {
		return commandlineexecutor.Result{
			StdOut: "HDB_INFO",
			StdErr: "",
		}
	}
	return commandlineexecutor.Result{
		StdOut: "",
		StdErr: "",
	}
}

func TestDiskInfo(t *testing.T) {
	tests := []struct {
		name              string
		basePathVolume    string
		globalINILocation string
		exec              commandlineexecutor.Execute
		iir               *instanceinfo.Reader
		config            *configpb.Configuration
		mapper            instanceinfo.NetworkInterfaceAddressMapper
		want              map[string]string
	}{
		{
			name:              "TestDiskInfoNoGrep",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec:              defaultExec,
			iir:               defaultIIR,
			config:            defaultConfiguration,
			mapper:            defaultMapperFunc,
			want:              map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrep2",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
					Error:  errors.New("Command failed"),
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrepError",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
					Error:  errors.New("Command failed"),
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoBadINIFormat",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolume /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: "",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput2",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: "This is invalid lsblk output",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput3",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: DefaultJSONDiskList,
						StdErr: "",
						Error:  errors.New("This is invalid lsblk output"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoErrorDF",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "df" {
					return commandlineexecutor.Result{
						Error: errors.New("Command failed"),
					}
				}
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolumes = /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidDF",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "df" {
					return commandlineexecutor.Result{
						StdOut: "\nMounted on\n/hana/data\ninvalid line\n",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolumes = /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoMatches",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: DefaultJSONDiskList,
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: defaultHanaINI,
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoSomeMatches",
			basePathVolume:    "/dev/sdb",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: DefaultJSONDiskList,
						StdErr: "",
					}
				}
				if params.Executable == "df" {
					return commandlineexecutor.Result{
						StdOut: dfTargetData,
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolumes = /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
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
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: EmptyJSONDiskList,
						StdErr: "",
					}
				}
				if params.Executable == "df" {
					return commandlineexecutor.Result{
						StdOut: dfTargetData,
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolumes = /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoChildren",
			basePathVolume:    "/dev/sda",
			globalINILocation: "/etc/config/test.ini",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: NoChildrenJSONDiskList,
						StdErr: "",
					}
				}
				if params.Executable == "df" {
					return commandlineexecutor.Result{
						StdOut: dfTargetData,
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "basepath_datavolumes = /hana/data/ISC",
					StdErr: "",
				}
			},
			iir:    defaultIIR,
			config: defaultConfiguration,
			mapper: defaultMapperFunc,
			want: map[string]string{
				"mountpoint":       "/hana/data",
				"instancedisktype": "default-disk-type",
				"size":             "4096",
				"pdsize":           "4096",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(context.Background(), test.config, test.mapper)
			got := diskInfo(context.Background(), test.basePathVolume, test.globalINILocation, test.exec, *test.iir)
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
			config:             defaultConfiguration,
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
			config:            defaultConfiguration,
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
			config:            defaultConfiguration,
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
			test.iir.Read(context.Background(), test.config, test.mapper)
			setDiskInfoForDevice(test.diskInfo, test.matchedBlockDevice, test.matchedMountPoint, test.matchedSize, *test.iir)
			got := test.diskInfo

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, setDiskInfoForDevice returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCollectHANAMetricsFromConfig(t *testing.T) {
	tests := []struct {
		name             string
		exec             commandlineexecutor.Execute
		osStatReader     OSStatReader
		configFileReader ConfigFileReader
		sapApplications  *sapb.SAPInstances
		wantHanaExists   float64
		wantLabels       map[string]string
	}{
		{
			name: "TestHanaDoesNotExist",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: defaultFileReader,
			sapApplications:  &sapb.SAPInstances{Instances: []*sapb.SAPInstance{}},
			wantHanaExists:   float64(0.0),
			wantLabels:       map[string]string{},
		},
		{
			name: "TestHanaNoSAPsid",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader:     os.Stat,
			configFileReader: defaultFileReader,
			sapApplications: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{Type: sapb.InstanceType_HANA},
				},
			},
			wantHanaExists: float64(0.0),
			wantLabels:     map[string]string{},
		},
		{
			name: "StatReaderError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "/bin/sh" {
					return commandlineexecutor.Result{
						StdOut: "/etc/config/this_ini_does_not_exist.ini",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, errors.New("error") },
			configFileReader: defaultFileReader,
			sapApplications: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
				},
			},
			wantHanaExists: float64(0.0),
			wantLabels:     map[string]string{},
		},
		{
			name: "TestHanaAllLabelsDisabled",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: defaultFileReader,
			sapApplications: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
				},
			},
			wantHanaExists: float64(1.0),
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
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "grep" {
					return commandlineexecutor.Result{
						StdOut: "basepath location /hana/data",
						StdErr: "",
					}
				}
				if params.Executable == "df" {
					if strings.Contains(params.ArgsToSplit, "/hana/data") {
						return commandlineexecutor.Result{
							StdOut: dfTargetData,
							StdErr: "",
						}
					}
					return commandlineexecutor.Result{
						StdOut: dfTargetLog,
						StdErr: "",
					}
				}
				if params.Executable == "lsblk" {
					return commandlineexecutor.Result{
						StdOut: DefaultJSONDiskList,
						StdErr: "",
					}
				}
				if params.Executable == "cat" {
					if params.Args[0] == "/proc/sys/kernel/numa_balancing" {
						return commandlineexecutor.Result{
							StdOut: "1",
							StdErr: "",
						}
					}
					return commandlineexecutor.Result{
						StdOut: "[always]",
						StdErr: "",
					}
				}
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader: func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: ConfigFileReader(func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(defaultHanaINI)), nil
			}),
			sapApplications: &sapb.SAPInstances{
				Instances: []*sapb.SAPInstance{
					&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
				},
			},
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
	err := protojson.Unmarshal(configuration.DefaultCollectionDefinition, collectionDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			iir := defaultIIR
			iir.Read(context.Background(), defaultConfiguration, defaultMapperFunc)
			p := Parameters{
				Config:             defaultConfiguration,
				OSStatReader:       test.osStatReader,
				InstanceInfoReader: *iir,
				Execute:            test.exec,
				OSType:             "linux",
				osVendorID:         "rhel",
				sapApplications:    test.sapApplications,
				WorkloadConfig:     collectionDefinition.GetWorkloadValidation(),
				ConfigFileReader:   test.configFileReader,
			}

			want := createHANAWorkloadMetrics(test.wantLabels, test.wantHanaExists)
			got := CollectHANAMetricsFromConfig(context.Background(), p)
			if diff := cmp.Diff(want, got, protocmp.Transform(), protocmp.IgnoreFields(&cpb.TimeInterval{}, "start_time", "end_time")); diff != "" {
				t.Errorf("CollectHANAMetricsFromConfig() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
