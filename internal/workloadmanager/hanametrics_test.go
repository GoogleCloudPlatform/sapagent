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
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/gce/fake"

	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredresourcepb "google.golang.org/genproto/googleapis/api/monitoredres"
	cpb "google.golang.org/genproto/googleapis/monitoring/v3"
	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	configpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	wpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	spb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/system"
)

// fakeDiskMapper provides a testable fake implementation of the DiskMapper interface
type fakeDiskMapper struct {
	err error
	out string
}

type fakeDiscoveryInterface struct {
	systems   []*spb.SapDiscovery
	instances *sapb.SAPInstances
}

func (d *fakeDiscoveryInterface) GetSAPSystems() []*spb.SapDiscovery  { return d.systems }
func (d *fakeDiscoveryInterface) GetSAPInstances() *sapb.SAPInstances { return d.instances }

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
      },
			{
				"name": "/dev/sdc",
				"type": "disk",
				"mountpoint": "/hana/log",
				"size": "4096"
			},
			{
				"name": "/dev/sdf",
				"type": "disk",
				"mountpoint": "/export/backup",
				"size": "4096"
			}
   ]
}
`
	defaultHanaINI = `
[fileio]
num_completion_queues = 12
num_submit_queues = 12

[ha_dr_provider_SAPHanaSR]

[persistance]
basepath_datavolumes = /hana/data/ISC
basepath_logvolumes = /hana/log/ISC
basepath_persistent_memory_volumes = /hana/memory/ISC
basepath_databackup = /export/backup/ISC
`
	defaultIndexserverINI = `
[global]
load_table_numa_aware = true

[parallel]
tables_preloaded_in_parallel = 32
`
	dfTargetData = `
Mounted on
/hana/data
`
	dfTargetLog = `
Mounted on
/hana/log
`
	dfTargetBackup = `
Mounted on
/export/backup
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
		ListDisksResp: []*compute.DiskList{
			{
				Items: []*compute.Disk{
					{
						Name: "disk-name",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "other-disk-device-name",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "hana-disk-name",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "hana-disk-name-2",
						Type: "/some/path/default-disk-type",
					},
					{
						Name: "hana-disk-name-3",
						Type: "/some/path/default-disk-type",
					},
				},
			},
		},
		ListDisksErr: []error{nil},
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
				{
					Source:     "/some/path/hana-disk-name-2",
					DeviceName: "sdc",
					Type:       "PERSISTENT",
				},
				{
					Source:     "/some/path/hana-disk-name-3",
					DeviceName: "sdf",
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
	defaultIIR         = instanceinfo.New(defaultDiskMapper, defaultGCEService)
	sapservicesGrepOut = `systemctl --no-ask-password start SAPSBX_00 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_HDB00_tst-backup-test
`
	backupLogGrepCommandOut = `2024-05-14T17:55:39+00:00  P0013384      18f783ee5cd INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT 'snapshot-srk-backup-test-data00001-20240514-175538'
2024-05-14T17:56:37+00:00  P0013384      18f783ee5cd INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID 1715709339085 SUCCESSFUL 'snapshot-srk-backup-test-data00001-20240514-175538'
2024-05-14T17:13:23+00:00  P0013384      18f781834c9 INFO    BACKUP   command: backup data for sbx using backint ('FULL_20240514')
2024-05-14T17:36:41+00:00  P0013384      18f782d8922 INFO    BACKUP   command: backup data incremental for sbx using backint ('INCR_20240514' )
2024-05-14T17:37:06+00:00  P0013384      18f782deb71 INFO    BACKUP   command: backup data differential for sbx using backint ('DIFF_20240514')
2024-05-14T17:55:39+00:00  P0013142      18f783ee5cd INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CREATE SNAPSHOT COMMENT 'snapshot-srk-backup-test-data00001-20240514-175538'
2024-05-14T17:56:37+00:00  P0013142      18f783ee5cd INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID 1715709339085 SUCCESSFUL 'snapshot-srk-backup-test-data00001-20240514-175538'
`
	backupLogGrepCommandOutNoFull = `2024-05-14T17:36:41+00:00  P0013384      18f782d8922 INFO    BACKUP   command: backup data incremental for sbx using backint ('INCR_20240514' )
2024-05-14T17:37:06+00:00  P0013384      18f782deb71 INFO    BACKUP   command: backup data differential for sbx using backint ('DIFF_20240514')
`
	backupLogGrepSuccessOut = `2024-05-14T17:55:39+00:00  P0013384      18f783ee5cd INFO    BACKUP   SNAPSHOT finished successfully
2024-05-14T17:56:39+00:00  P0013384      18f783ee5cd INFO    BACKUP   SNAPSHOT finished successfully
2024-05-14T17:13:49+00:00  P0013384      18f781834c9 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:36:46+00:00  P0013384      18f782d8922 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:37:11+00:00  P0013384      18f782deb71 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:55:00+00:00  P0013143      00000000001 INFO    BACKUP   SNAPSHOT finished successfully
2024-05-14T17:55:39+00:00  P0013142      18f783ee5cd INFO    BACKUP   SNAPSHOT finished successfully
2024-05-14T17:56:39+00:00  P0013142      18f783ee5cd INFO    BACKUP   SNAPSHOT finished successfully
`
	backupLogGrepSuccessOutNoFull = `2024-05-14T17:36:46+00:00  P0013384      18f782d8922 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:37:11+00:00  P0013384      18f782deb71 INFO    BACKUP   SAVE DATA finished successfully
`
	oldestBackedUpTenant         = `tst-backup-test`
	oldestLastFullBackupTime     = `2024-05-14T17:13:49Z`
	oldestLastDeltaBackupTime    = `2024-05-14T17:37:11Z`
	oldestLastSnapshotBackupTime = `2024-05-14T17:56:39Z`
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
		globalINILocation string
		params            Parameters
		volume            *wpb.HANADiskVolumeMetric
		exec              commandlineexecutor.Execute
		mapper            instanceinfo.NetworkInterfaceAddressMapper
		want              map[string]string
	}{
		{
			name:              "TestDiskInfoNoGrep",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute:            defaultExec,
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrep2",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: defaultHanaINI,
						StdErr: "",
						Error:  errors.New("Command failed"),
					}
				},
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoGrepError",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: defaultHanaINI,
						StdErr: "",
						Error:  errors.New("Command failed"),
					}
				},
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoBadINIFormat",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
					return commandlineexecutor.Result{
						StdOut: "basepath_datavolume /hana/data/ISC",
						StdErr: "",
					}
				},
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput2",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidLSBLKOutput3",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoErrorDF",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoInvalidDF",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoMatches",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoSomeMatches",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sdb",
			},
			mapper: defaultMapperFunc,
			want: map[string]string{
				"mountpoint":       "/hana/data",
				"instancedisktype": "default-disk-type",
				"size":             "2048",
				"pdsize":           "4096",
				"blockdevice":      "/dev/sdb",
			},
		},
		{
			name:              "TestDiskInfoNoDevices",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want:   map[string]string{},
		},
		{
			name:              "TestDiskInfoNoChildren",
			globalINILocation: "/etc/config/test.ini",
			params: Parameters{
				Execute: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
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
				InstanceInfoReader: *defaultIIR,
				Config:             defaultConfiguration,
			},
			volume: &wpb.HANADiskVolumeMetric{
				MetricSource:   wpb.HANADiskVolumeMetricSource_GLOBAL_INI,
				BasepathVolume: "/dev/sda",
			},
			mapper: defaultMapperFunc,
			want: map[string]string{
				"mountpoint":       "/hana/data",
				"instancedisktype": "default-disk-type",
				"size":             "4096",
				"pdsize":           "4096",
				"blockdevice":      "/dev/sdb",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.params.InstanceInfoReader.Read(context.Background(), test.params.Config, test.mapper)
			got := diskInfo(context.Background(), test.volume, test.globalINILocation, test.params)
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
				"blockdevice":      "disk-device-name",
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
				"blockdevice":      "other-disk-device-name",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.iir.Read(context.Background(), test.config, test.mapper)
			setDiskInfoForDevice(context.Background(), test.diskInfo, test.matchedBlockDevice, test.matchedMountPoint, test.matchedSize, *test.iir)
			got := test.diskInfo

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("%s failed, setDiskInfoForDevice returned unexpected metric labels diff (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestCheckHAZones(t *testing.T) {
	tests := []struct {
		name   string
		params Parameters
		want   string
	}{
		{
			name: "NoHAHosts",
			params: Parameters{
				Config: defaultConfiguration,
				Discovery: &fakeDiscoveryInterface{
					systems: []*spb.SapDiscovery{
						{
							DatabaseLayer: &spb.SapDiscovery_Component{},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "NoHostInstance",
			params: Parameters{
				Config: defaultConfiguration,
				Discovery: &fakeDiscoveryInterface{
					systems: []*spb.SapDiscovery{
						{
							DatabaseLayer: &spb.SapDiscovery_Component{
								HaHosts: []string{
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-1",
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-2",
								},
							},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "NoOverlappingZones",
			params: Parameters{
				Config: defaultConfiguration,
				Discovery: &fakeDiscoveryInterface{
					systems: []*spb.SapDiscovery{
						{
							DatabaseLayer: &spb.SapDiscovery_Component{
								HaHosts: []string{
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-1",
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-2",
								},
							},
						},
						{
							DatabaseLayer: &spb.SapDiscovery_Component{
								HaHosts: []string{
									"/projects/test-project-id/zones/test-region-zone/instances/test-instance-name",
									"/projects/test-project-id/zones/test-other-zone/instances/other-instance-3",
								},
							},
						},
					},
				},
			},
			want: "",
		},
		{
			name: "HasHAInSameZone",
			params: Parameters{
				Config: defaultConfiguration,
				Discovery: &fakeDiscoveryInterface{
					systems: []*spb.SapDiscovery{
						{
							DatabaseLayer: &spb.SapDiscovery_Component{
								HaHosts: []string{
									"/projects/test-project-id/zones/test-region-zone/instances/test-instance-name",
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-1",
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-2",
									"/projects/test-project-id/zones/test-region-zone/instances/other-instance-2",
									"resourceURI/does/not/match/regexp",
								},
							},
						},
					},
				},
			},
			want: "other-instance-1,other-instance-2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkHAZones(context.Background(), test.params)
			if got != test.want {
				t.Errorf("checkHAZones() got %s, want %s", got, test.want)
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
		discovery        *fakeDiscoveryInterface
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
			discovery: &fakeDiscoveryInterface{
				instances: &sapb.SAPInstances{Instances: []*sapb.SAPInstance{}},
			},
			wantHanaExists: float64(0.0),
			wantLabels:     map[string]string{},
		},
		{
			name: "TestNOSAPInstances",
			exec: func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result {
				return commandlineexecutor.Result{
					StdOut: "",
					StdErr: "",
				}
			},
			osStatReader:     func(string) (os.FileInfo, error) { return nil, nil },
			configFileReader: defaultFileReader,
			discovery:        &fakeDiscoveryInterface{},
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
			discovery: &fakeDiscoveryInterface{
				instances: &sapb.SAPInstances{
					Instances: []*sapb.SAPInstance{
						&sapb.SAPInstance{Type: sapb.InstanceType_HANA},
					},
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
			discovery: &fakeDiscoveryInterface{
				instances: &sapb.SAPInstances{
					Instances: []*sapb.SAPInstance{
						&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
					},
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
			discovery: &fakeDiscoveryInterface{
				instances: &sapb.SAPInstances{
					Instances: []*sapb.SAPInstance{
						&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
					},
				},
			},
			wantHanaExists: float64(1.0),
			wantLabels: map[string]string{
				"disk_data_mount":                           "",
				"disk_data_pd_size":                         "",
				"disk_data_size":                            "",
				"disk_data_type":                            "",
				"disk_log_mount":                            "",
				"disk_log_pd_size":                          "",
				"disk_log_size":                             "",
				"disk_log_type":                             "",
				"fast_restart":                              "disabled",
				"ha_sr_hook_configured":                     "no",
				"num_completion_queues":                     "1",
				"num_submit_queues":                         "1",
				"tables_preloaded_in_parallel":              "",
				"load_table_numa_aware":                     "false",
				"numa_balancing":                            "disabled",
				"oldest_backup_tenant_name":                 "",
				"oldest_last_backup_timestamp_utc":          "",
				"oldest_delta_backup_tenant_name":           "",
				"oldest_last_delta_backup_timestamp_utc":    "",
				"oldest_snapshot_backup_tenant_name":        "",
				"oldest_last_snapshot_backup_timestamp_utc": "",
				"transparent_hugepages":                     "disabled",
				"ha_in_same_zone":                           "",
				"hana_data_volume":                          "",
				"hana_log_volume":                           "",
				"hana_backup_volume":                        "",
				"hana_shared_volume":                        "",
				"usr_sap_volume":                            "",
			},
		},
		{
			name: "TestHanaAllLabelsEnabled",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				if params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "grep") {
					if strings.Contains(params.ArgsToSplit, "finished successfully") {
						return commandlineexecutor.Result{
							StdOut: backupLogGrepSuccessOut,
							StdErr: "",
						}
					}
					if strings.Contains(params.ArgsToSplit, "command:") {
						return commandlineexecutor.Result{
							StdOut: backupLogGrepCommandOut,
							StdErr: "",
						}
					}
				}
				if params.Executable == "grep" {
					if strings.Contains(params.ArgsToSplit, "sapservices") {
						return commandlineexecutor.Result{
							StdOut: sapservicesGrepOut,
							StdErr: "",
						}
					} else if strings.Contains(params.ArgsToSplit, "basepath_logvolumes") {
						return commandlineexecutor.Result{
							StdOut: "basepath location /hana/log",
							StdErr: "",
						}
					} else if strings.Contains(params.ArgsToSplit, "basepath_databackup") {
						return commandlineexecutor.Result{
							StdOut: "basepath location /export/backup",
							StdErr: "",
						}
					}
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
					} else if strings.Contains(params.ArgsToSplit, "/export/backup") {
						return commandlineexecutor.Result{
							StdOut: dfTargetBackup,
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
				switch {
				case strings.Contains(data, "global.ini"):
					return io.NopCloser(strings.NewReader(defaultHanaINI)), nil
				case strings.Contains(data, "indexserver.ini"):
					return io.NopCloser(strings.NewReader(defaultIndexserverINI)), nil
				default:
					return nil, fmt.Errorf("Unexpected file name: %s", data)
				}
			}),
			discovery: &fakeDiscoveryInterface{
				instances: &sapb.SAPInstances{
					Instances: []*sapb.SAPInstance{
						&sapb.SAPInstance{Type: sapb.InstanceType_HANA, Sapsid: "QE0"},
					},
				},
				systems: []*spb.SapDiscovery{
					{
						DatabaseLayer: &spb.SapDiscovery_Component{
							HaHosts: []string{
								"/projects/test-project-id/zones/test-region-zone/instances/test-instance-name",
								"/projects/test-project-id/zones/test-region-zone/instances/other-instance-1",
								"resourceURI/does/not/match/regexp",
							},
						},
					},
				},
			},
			wantHanaExists: float64(1.0),
			wantLabels: map[string]string{
				"disk_data_mount":                           "/hana/data",
				"disk_data_pd_size":                         "4096",
				"disk_data_size":                            "2048",
				"disk_data_type":                            "default-disk-type",
				"disk_log_mount":                            "/hana/log",
				"disk_log_pd_size":                          "4096",
				"disk_log_size":                             "4096",
				"disk_log_type":                             "default-disk-type",
				"fast_restart":                              "enabled",
				"ha_sr_hook_configured":                     "yes",
				"num_completion_queues":                     "12",
				"num_submit_queues":                         "12",
				"tables_preloaded_in_parallel":              "32",
				"load_table_numa_aware":                     "true",
				"numa_balancing":                            "enabled",
				"oldest_backup_tenant_name":                 oldestBackedUpTenant,
				"oldest_last_backup_timestamp_utc":          oldestLastFullBackupTime,
				"oldest_delta_backup_tenant_name":           oldestBackedUpTenant,
				"oldest_last_delta_backup_timestamp_utc":    oldestLastDeltaBackupTime,
				"oldest_snapshot_backup_tenant_name":        oldestBackedUpTenant,
				"oldest_last_snapshot_backup_timestamp_utc": oldestLastSnapshotBackupTime,
				"transparent_hugepages":                     "enabled",
				"ha_in_same_zone":                           "other-instance-1",
				"hana_data_volume":                          "/dev/sdb",
				"hana_log_volume":                           "/dev/sdc",
				"hana_backup_volume":                        "/dev/sdf",
				"hana_shared_volume":                        "",
				"usr_sap_volume":                            "",
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
				Discovery:          test.discovery,
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

func TestHANABackupMetrics(t *testing.T) {
	execErrorResponse := commandlineexecutor.Result{
		StdOut: "",
		StdErr: "Invalid Command",
		Error:  cmpopts.AnyError,
	}
	execEmptyResponse := commandlineexecutor.Result{}

	metricsAllEmpty := map[string]string{
		"oldest_backup_tenant_name":                 "",
		"oldest_last_backup_timestamp_utc":          "",
		"oldest_delta_backup_tenant_name":           "",
		"oldest_last_delta_backup_timestamp_utc":    "",
		"oldest_snapshot_backup_tenant_name":        "",
		"oldest_last_snapshot_backup_timestamp_utc": "",
	}
	metricsEmptyTimestamps := map[string]string{
		"oldest_backup_tenant_name":                 oldestBackedUpTenant,
		"oldest_last_backup_timestamp_utc":          "",
		"oldest_delta_backup_tenant_name":           oldestBackedUpTenant,
		"oldest_last_delta_backup_timestamp_utc":    "",
		"oldest_snapshot_backup_tenant_name":        oldestBackedUpTenant,
		"oldest_last_snapshot_backup_timestamp_utc": "",
	}
	metricsZeroTimestamps := map[string]string{
		"oldest_backup_tenant_name":                 oldestBackedUpTenant,
		"oldest_last_backup_timestamp_utc":          "0001-01-01T00:00:00Z",
		"oldest_delta_backup_tenant_name":           oldestBackedUpTenant,
		"oldest_last_delta_backup_timestamp_utc":    "0001-01-01T00:00:00Z",
		"oldest_snapshot_backup_tenant_name":        oldestBackedUpTenant,
		"oldest_last_snapshot_backup_timestamp_utc": "0001-01-01T00:00:00Z",
	}

	tests := []struct {
		name string
		exec func(context.Context, commandlineexecutor.Params) commandlineexecutor.Result
		want map[string]string
	}{
		{
			name: "discoverHANAdbTenantsError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return execErrorResponse
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{StdOut: backupLogGrepSuccessOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{StdOut: backupLogGrepCommandOut}
				}
				return execEmptyResponse
			},
			want: metricsAllEmpty,
		},
		{
			name: "noHANAdbTenants",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{
						StdOut: `
#systemctl --no-ask-password start SAPSBX_02 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_HDB02_tst-commented-test
systemctl --no-ask-password start SAPSBX_01 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_NOT_HDB_tst-backup1-test
systemctl --no-ask-password start SAPSBX_03 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_HDB03
						`,
					}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{StdOut: backupLogGrepSuccessOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{StdOut: backupLogGrepCommandOut}
				}
				return execEmptyResponse
			},
			want: metricsAllEmpty,
		},
		{
			name: "fetchSuccessfulBackupsError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return execErrorResponse
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{StdOut: backupLogGrepCommandOut}
				}
				return execEmptyResponse
			},
			want: metricsEmptyTimestamps,
		},
		{
			name: "noSuccessfulBackups",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{
						StdOut: `
9999-99-99T99:99:99+99:99  P0013384      18f781834c9 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T19:37:06+00:00  P0013384      18f782deb71 INFO    BACKUP   finished successfully
						`,
					}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{StdOut: backupLogGrepCommandOut}
				}
				return execEmptyResponse
			},
			want: metricsZeroTimestamps,
		},
		{
			name: "fetchBackupThreadIDsError",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{StdOut: backupLogGrepSuccessOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return execErrorResponse
				}
				return execEmptyResponse
			},
			want: metricsEmptyTimestamps,
		},
		{
			name: "noBackupThreadIDs",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{StdOut: backupLogGrepSuccessOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{
						StdOut: `
2024-05-14T19:37:06+00:00  P0013384      18f782deb71 INFO    BACKUP   command:
command: backup data for sbx using backint ('FULL_20240514')
						`,
					}
				}
				return execEmptyResponse
			},
			want: metricsZeroTimestamps,
		},
		{
			name: "backupThreadIDMismatch",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{
						StdOut: `
2024-05-14T19:13:49+00:00  P0013384      18f78180001 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T19:36:46+00:00  P0013384      18f78180002 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T19:37:11+00:00  P0013384      18f78180003 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T19:56:39+00:00  P0013384      18f78180004 INFO    BACKUP   SNAPSHOT finished successfully
						`,
					}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{
						StdOut: `
2024-05-14T19:13:23+00:00  P0013384      18f78180005 INFO    BACKUP   command: backup data for sbx using backint ('FULL_20240514')
2024-05-14T19:36:41+00:00  P0013384      18f78180006 INFO    BACKUP   command: backup data incremental for sbx using backint ('INCR_20240514' )
2024-05-14T19:37:06+00:00  P0013384      18f78180007 INFO    BACKUP   command: backup data differential for sbx using backint ('DIFF_20240514')
2024-05-14T19:56:37+00:00  P0013384      18f78180008 INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID 1715709339085 SUCCESSFUL 'snapshot-srk-backup-test-data00001-20240514-175538'
						`,
					}
				}
				return execEmptyResponse
			},
			want: metricsZeroTimestamps,
		},
		{
			name: "successSingleTenantDB",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{StdOut: sapservicesGrepOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					return commandlineexecutor.Result{StdOut: backupLogGrepSuccessOut}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					return commandlineexecutor.Result{StdOut: backupLogGrepCommandOut}
				}
				return execEmptyResponse
			},
			want: map[string]string{
				"oldest_backup_tenant_name":                 oldestBackedUpTenant,
				"oldest_last_backup_timestamp_utc":          oldestLastFullBackupTime,
				"oldest_delta_backup_tenant_name":           oldestBackedUpTenant,
				"oldest_last_delta_backup_timestamp_utc":    oldestLastDeltaBackupTime,
				"oldest_snapshot_backup_tenant_name":        oldestBackedUpTenant,
				"oldest_last_snapshot_backup_timestamp_utc": oldestLastSnapshotBackupTime,
			},
		},
		{
			name: "successMultipleTenantDBs",
			exec: func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
				switch {
				case params.Executable == "grep" && strings.Contains(params.ArgsToSplit, "sapservices"):
					return commandlineexecutor.Result{
						StdOut: `
systemctl --no-ask-password start SAPSBX_01 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_HDB01_tst-backup-test-1
systemctl --no-ask-password start SAPSBX_02 # sapstartsrv pf=/usr/sap/SBX/SYS/profile/SBX_HDB02_tst-backup-test-2
						`,
					}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "finished successfully"):
					if strings.Contains(params.ArgsToSplit, "tst-backup-test-1") {
						return commandlineexecutor.Result{
							StdOut: `
2024-05-14T17:10:00+00:00  P0013384      18f78180001 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:20:00+00:00  P0013384      18f78180002 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:30:00+00:00  P0013384      18f78180003 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T17:40:00+00:00  P0013384      18f78180004 INFO    BACKUP   SNAPSHOT finished successfully
							`,
						}
					}
					return commandlineexecutor.Result{
						StdOut: `
2024-05-14T18:10:00+00:00  P0013384      18f78180005 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T18:20:00+00:00  P0013384      18f78180006 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T18:30:00+00:00  P0013384      18f78180007 INFO    BACKUP   SAVE DATA finished successfully
2024-05-14T18:40:00+00:00  P0013384      18f78180008 INFO    BACKUP   SNAPSHOT finished successfully
						`,
					}
				case params.Executable == "sudo" && strings.Contains(params.ArgsToSplit, "command:"):
					if strings.Contains(params.ArgsToSplit, "tst-backup-test-1") {
						return commandlineexecutor.Result{
							StdOut: `
2024-05-14T17:09:00+00:00  P0013384      18f78180001 INFO    BACKUP   command: backup data for sbx using backint ('FULL_20240514')
2024-05-14T17:19:00+00:00  P0013384      18f78180002 INFO    BACKUP   command: backup data incremental for sbx using backint ('INCR_20240514' )
2024-05-14T17:29:00+00:00  P0013384      18f78180003 INFO    BACKUP   command: backup data differential for sbx using backint ('DIFF_20240514')
2024-05-14T17:39:00+00:00  P0013384      18f78180004 INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID 1715709339085 SUCCESSFUL 'snapshot-srk-backup-test-data00001-20240514-175538'
							`,
						}
					}
					return commandlineexecutor.Result{
						StdOut: `
2024-05-14T18:09:00+00:00  P0013384      18f78180005 INFO    BACKUP   command: backup data for sbx using backint ('FULL_20240514')
2024-05-14T18:19:00+00:00  P0013384      18f78180006 INFO    BACKUP   command: backup data incremental for sbx using backint ('INCR_20240514' )
2024-05-14T18:29:00+00:00  P0013384      18f78180007 INFO    BACKUP   command: backup data differential for sbx using backint ('DIFF_20240514')
2024-05-14T18:39:00+00:00  P0013384      18f78180008 INFO    BACKUP   command: BACKUP DATA FOR FULL SYSTEM CLOSE SNAPSHOT BACKUP_ID 1715709339085 SUCCESSFUL 'snapshot-srk-backup-test-data00001-20240514-175538'
						`,
					}
				}
				return execEmptyResponse
			},
			want: map[string]string{
				"oldest_backup_tenant_name":                 "tst-backup-test-1",
				"oldest_last_backup_timestamp_utc":          "2024-05-14T17:10:00Z",
				"oldest_delta_backup_tenant_name":           "tst-backup-test-1",
				"oldest_last_delta_backup_timestamp_utc":    "2024-05-14T17:30:00Z",
				"oldest_snapshot_backup_tenant_name":        "tst-backup-test-1",
				"oldest_last_snapshot_backup_timestamp_utc": "2024-05-14T17:40:00Z",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hanaBackupMetrics(context.Background(), test.exec)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("hanaBackupMetrics() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
