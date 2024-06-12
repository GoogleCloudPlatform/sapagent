/*
Copyright 2024 Google LLC

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

package hanadiskbackuphandler

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestBuildHANADiskBackupCommand(t *testing.T) {
	tests := []struct {
		name    string
		command *gpb.AgentCommand
		want    hanadiskbackup.Snapshot
	}{
		{
			name: "TrueValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"port":                                  "test-port",
					"sid":                                   "test-sid",
					"instance-id":                           "test-instance-id",
					"hana-db-user":                          "test-hana-db-user",
					"password":                              "test-password",
					"password-secret":                       "test-password-secret",
					"hdbuserstore-key":                      "test-hdbuserstore-key",
					"source-disk":                           "test-source-disk",
					"source-disk-zone":                      "test-source-disk-zone",
					"host":                                  "test-host",
					"project":                               "test-project",
					"snapshot-name":                         "test-snapshot-name",
					"snapshot-type":                         "test-snapshot-type",
					"source-disk-key-file":                  "test-source-disk-key-file",
					"storage-location":                      "test-storage-location",
					"snapshot-description":                  "test-snapshot-description",
					"loglevel":                              "debug",
					"labels":                                "test-labels",
					"freeze-file-system":                    "true",
					"abandon-prepared":                      "True",
					"skip-db-snapshot-for-change-disk-type": "1",
					"confirm-data-snapshot-after-create":    "T",
					"send-metrics-to-monitoring":            "TRUE",
				},
			},
			want: hanadiskbackup.Snapshot{
				Port:                            "test-port",
				Sid:                             "test-sid",
				InstanceID:                      "test-instance-id",
				HanaDBUser:                      "test-hana-db-user",
				Password:                        "test-password",
				PasswordSecret:                  "test-password-secret",
				HDBUserstoreKey:                 "test-hdbuserstore-key",
				Disk:                            "test-source-disk",
				DiskZone:                        "test-source-disk-zone",
				Host:                            "test-host",
				Project:                         "test-project",
				SnapshotName:                    "test-snapshot-name",
				SnapshotType:                    "test-snapshot-type",
				DiskKeyFile:                     "test-source-disk-key-file",
				StorageLocation:                 "test-storage-location",
				Description:                     "test-snapshot-description",
				LogLevel:                        "debug",
				Labels:                          "test-labels",
				FreezeFileSystem:                true,
				AbandonPrepared:                 true,
				SkipDBSnapshotForChangeDiskType: true,
				ConfirmDataSnapshotAfterCreate:  true,
				SendToMonitoring:                true,
			},
		},
		{
			name: "FalseValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"port":                                  "test-port",
					"sid":                                   "test-sid",
					"instance-id":                           "test-instance-id",
					"hana-db-user":                          "test-hana-db-user",
					"password":                              "test-password",
					"password-secret":                       "test-password-secret",
					"hdbuserstore-key":                      "test-hdbuserstore-key",
					"source-disk":                           "test-source-disk",
					"source-disk-zone":                      "test-source-disk-zone",
					"host":                                  "test-host",
					"project":                               "test-project",
					"snapshot-name":                         "test-snapshot-name",
					"snapshot-type":                         "test-snapshot-type",
					"source-disk-key-file":                  "test-source-disk-key-file",
					"storage-location":                      "test-storage-location",
					"snapshot-description":                  "test-snapshot-description",
					"loglevel":                              "debug",
					"labels":                                "test-labels",
					"freeze-file-system":                    "false",
					"abandon-prepared":                      "False",
					"skip-db-snapshot-for-change-disk-type": "0",
					"confirm-data-snapshot-after-create":    "F",
					"send-metrics-to-monitoring":            "FALSE",
				},
			},
			want: hanadiskbackup.Snapshot{
				Port:                            "test-port",
				Sid:                             "test-sid",
				InstanceID:                      "test-instance-id",
				HanaDBUser:                      "test-hana-db-user",
				Password:                        "test-password",
				PasswordSecret:                  "test-password-secret",
				HDBUserstoreKey:                 "test-hdbuserstore-key",
				Disk:                            "test-source-disk",
				DiskZone:                        "test-source-disk-zone",
				Host:                            "test-host",
				Project:                         "test-project",
				SnapshotName:                    "test-snapshot-name",
				SnapshotType:                    "test-snapshot-type",
				DiskKeyFile:                     "test-source-disk-key-file",
				StorageLocation:                 "test-storage-location",
				Description:                     "test-snapshot-description",
				LogLevel:                        "debug",
				Labels:                          "test-labels",
				FreezeFileSystem:                false,
				AbandonPrepared:                 false,
				SkipDBSnapshotForChangeDiskType: false,
				ConfirmDataSnapshotAfterCreate:  false,
				SendToMonitoring:                false,
			},
		},
		{
			name: "InvalidValues",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"port":                                  "",
					"sid":                                   "",
					"instance-id":                           "",
					"hana-db-user":                          "",
					"password":                              "",
					"password-secret":                       "",
					"hdbuserstore-key":                      "",
					"source-disk":                           "",
					"source-disk-zone":                      "",
					"host":                                  "",
					"project":                               "",
					"snapshot-name":                         "",
					"snapshot-type":                         "",
					"source-disk-key-file":                  "",
					"storage-location":                      "",
					"snapshot-description":                  "",
					"loglevel":                              "",
					"labels":                                "",
					"freeze-file-system":                    "invalid",
					"abandon-prepared":                      "invalid-true",
					"skip-db-snapshot-for-change-disk-type": "",
					"confirm-data-snapshot-after-create":    "1233",
					"send-metrics-to-monitoring":            "invalid_false",
				},
			},
			want: hanadiskbackup.Snapshot{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildHANADiskBackupCommand(tc.command)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(hanadiskbackup.Snapshot{})); diff != "" {
				t.Errorf("buildHANADiskBackupCommand(%v) returned an unexpected diff (-want +got):\n%v", tc.command, diff)
			}
		})
	}
}
