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
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/backup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr/discovery"

	gpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/gcbdractions"
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
		}, {
			// Discovery command does not have any parameters.
			name: "ParseGCBDRDiscoveryCommand",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{},
			},
			obj:                  &discovery.Discovery{},
			want:                 &discovery.Discovery{},
			ignoreUnexportedType: discovery.Discovery{},
		},
		{
			name: "InvalidValuedCommandParameters",
			command: &gpb.AgentCommand{
				Parameters: map[string]string{
					"use-systemdb-key": "invalid-bool-value",
				},
			},
			obj:                  &backup.Backup{},
			want:                 &backup.Backup{},
			ignoreUnexportedType: backup.Backup{},
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
