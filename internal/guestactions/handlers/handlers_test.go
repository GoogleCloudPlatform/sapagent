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

package handlers

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configureinstance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/backup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/performancediagnostics"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
)

func TestParseAgentCommandParameters(t *testing.T) {
	tests := []struct {
		name                 string
		command              *gpb.AgentCommand
		obj                  subcommands.Command
		want                 subcommands.Command
		ignoreUnexportedType any
	}{
		{
			name: "ParseSupportBundleCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"sid":                 "testsid",
					"instance-numbers":    "testinstance",
					"hostname":            "testhostname",
					"loglevel":            "debug",
					"result-bucket":       "testbucket",
					"pacemaker-diagnosis": "true",
					"agent-logs-only":     "true",
					"help":                "true",
					"version":             "true",
				},
			},
			obj: &supportbundle.SupportBundle{},
			want: &supportbundle.SupportBundle{
				Sid:                "testsid",
				InstanceNums:       "testinstance",
				Hostname:           "testhostname",
				LogLevel:           "debug",
				ResultBucket:       "testbucket",
				PacemakerDiagnosis: true,
				AgentLogsOnly:      true,
				Help:               true,
			},
			ignoreUnexportedType: supportbundle.SupportBundle{},
		},
		{
			name: "ParseConfigureCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"feature":                               "test_feature",
					"loglevel":                              "debug",
					"setting":                               "test_setting",
					"path":                                  "test_path",
					"process_metrics_to_skip":               "test_skip_metrics",
					"workload_evaluation_metrics_frequency": "30",
					"workload_evaluation_db_metrics_frequency": "25",
					"process_metrics_frequency":                "5",
					"slow_process_metrics_frequency":           "10",
					"agent_metrics_frequency":                  "15",
					"agent_health_frequency":                   "20",
					"heartbeat_frequency":                      "12",
					"sample_interval_sec":                      "40",
					"query_timeout_sec":                        "45",
					"help":                                     "true",
					"version":                                  "false",
					"enable":                                   "true",
					"disable":                                  "false",
					"showall":                                  "true",
					"add":                                      "false",
					"remove":                                   "true",
				},
			},
			obj: &configure.Configure{},
			want: &configure.Configure{
				Feature:                    "test_feature",
				LogLevel:                   "debug",
				Setting:                    "test_setting",
				Path:                       "test_path",
				SkipMetrics:                "test_skip_metrics",
				ValidationMetricsFrequency: 30,
				DbFrequency:                25,
				FastMetricsFrequency:       5,
				SlowMetricsFrequency:       10,
				AgentMetricsFrequency:      15,
				AgentHealthFrequency:       20,
				HeartbeatFrequency:         12,
				SampleIntervalSec:          40,
				QueryTimeoutSec:            45,
				Help:                       true,
				Enable:                     true,
				Disable:                    false,
				Showall:                    true,
				Add:                        false,
				Remove:                     true,
			},
			ignoreUnexportedType: configure.Configure{},
		},
		{
			name: "ParseHANADiskBackupCommand",
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
					"abandon-prepared":                      "true",
					"skip-db-snapshot-for-change-disk-type": "true",
					"confirm-data-snapshot-after-create":    "true",
					"send-metrics-to-monitoring":            "true",
				},
			},
			obj: &hanadiskbackup.Snapshot{},
			want: &hanadiskbackup.Snapshot{
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
			ignoreUnexportedType: hanadiskbackup.Snapshot{},
		},
		{
			name: "ParsePerformanceDiagnosticsCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"type":                "test-type",
					"test-bucket":         "test-test-bucket",
					"backint-config-file": "test-backint-config-file",
					"output-bucket":       "test-output-bucket",
					"output-file-name":    "test-output-file-name",
					"output-file-path":    "test-output-file-path",
					"hyper-threading":     "test-hyper-threading",
					"override-version":    "test-override-version",
					"loglevel":            "debug",
					"print-diff":          "true",
				},
			},
			obj: &performancediagnostics.Diagnose{},
			want: &performancediagnostics.Diagnose{
				Type:              "test-type",
				TestBucket:        "test-test-bucket",
				BackintConfigFile: "test-backint-config-file",
				OutputBucket:      "test-output-bucket",
				OutputFileName:    "test-output-file-name",
				OutputFilePath:    "test-output-file-path",
				HyperThreading:    "test-hyper-threading",
				OverrideVersion:   "test-override-version",
				LogLevel:          "debug",
				PrintDiff:         true,
			},
			ignoreUnexportedType: performancediagnostics.Diagnose{},
		},
		{
			name: "ParseConfigureInstanceCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"apply":           "true",
					"check":           "true",
					"printDiff":       "true",
					"overrideType":    "test-machine-type",
					"hyperThreading":  "test-hyper-threading",
					"overrideVersion": "test-override-version",
					"help":            "true",
					"version":         "true",
				},
			},
			obj: &configureinstance.ConfigureInstance{},
			want: &configureinstance.ConfigureInstance{
				Apply:           true,
				Check:           true,
				PrintDiff:       true,
				MachineType:     "test-machine-type",
				HyperThreading:  "test-hyper-threading",
				OverrideVersion: "test-override-version",
				Help:            true,
			},
			ignoreUnexportedType: configureinstance.ConfigureInstance{},
		},
		{
			name: "ParseGCBDRBackupCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"operation-type":                 "test-operation-type",
					"sid":                            "test-sid",
					"hdbuserstore-key":               "test-hdbuserstore-key",
					"job-name":                       "test-job-name",
					"snapshot-status":                "test-snapshot-status",
					"snapshot-type":                  "test-snapshot-type",
					"catalog-backup-retention-days":  "2",
					"production-log-retention-hours": "1",
					"log-backup-end-pit":             "test-log-backup-end-pit",
					"last-backed-up-db-names":        "test-last-backed-up-db-names",
					"use-systemdb-key":               "true",
					"loglevel":                       "debug",
					"log-path":                       "test-log-path",
				},
			},
			obj: &backup.Backup{},
			want: &backup.Backup{
				OperationType:               "test-operation-type",
				SID:                         "test-sid",
				HDBUserstoreKey:             "test-hdbuserstore-key",
				JobName:                     "test-job-name",
				SnapshotStatus:              "test-snapshot-status",
				SnapshotType:                "test-snapshot-type",
				CatalogBackupRetentionDays:  2,
				ProductionLogRetentionHours: 1,
				LogBackupEndPIT:             "test-log-backup-end-pit",
				LastBackedUpDBNames:         "test-last-backed-up-db-names",
				UseSystemDBKey:              true,
				LogLevel:                    "debug",
				LogPath:                     "test-log-path",
			},
			ignoreUnexportedType: backup.Backup{},
		},
		{
			name: "InvalidCommandParameters",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"invalid-param": "test-invalid-param",
				},
			},
			obj:                  &supportbundle.SupportBundle{},
			want:                 &supportbundle.SupportBundle{},
			ignoreUnexportedType: supportbundle.SupportBundle{},
		},
		{
			name: "InvalidValuedCommandParameters",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"agent-logs-only": "invalid-bool-value",
				},
			},
			obj:                  &supportbundle.SupportBundle{},
			want:                 &supportbundle.SupportBundle{},
			ignoreUnexportedType: supportbundle.SupportBundle{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ParseAgentCommandParameters(context.Background(), tc.command, tc.obj)
			if diff := cmp.Diff(tc.want, tc.obj, cmpopts.IgnoreUnexported(tc.ignoreUnexportedType)); diff != "" {
				t.Errorf("ParseAgentCommandParameters() returned an unexpected diff (-want +got):\n%v", diff)
			}
		})
	}
}
