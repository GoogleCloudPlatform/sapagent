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

// Package hanadiskbackuphandler contains the handler for the hanadiskbackup command.
package hanadiskbackuphandler

import (
	"context"
	"runtime"
	"strconv"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	gpb "github.com/GoogleCloudPlatform/sapagent/protos/guestactions"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RestartAgent indicates that the agent should be restarted after the hanadiskbackup guest action has been handled.
const RestartAgent = false

func buildHANADiskBackupCommand(command *gpb.AgentCommand) hanadiskbackup.Snapshot {
	params := command.GetParameters()

	s := hanadiskbackup.Snapshot{}
	if port, ok := params["port"]; ok {
		s.Port = port
	}
	if Sid, ok := params["sid"]; ok {
		s.Sid = Sid
	}
	if instanceID, ok := params["instance-id"]; ok {
		s.InstanceID = instanceID
	}
	if hanaDBUser, ok := params["hana-db-user"]; ok {
		s.HanaDBUser = hanaDBUser
	}
	if password, ok := params["password"]; ok {
		s.Password = password
	}
	if passwordSecret, ok := params["password-secret"]; ok {
		s.PasswordSecret = passwordSecret
	}
	if hDBUserstoreKey, ok := params["hdbuserstore-key"]; ok {
		s.HDBUserstoreKey = hDBUserstoreKey
	}
	if disk, ok := params["source-disk"]; ok {
		s.Disk = disk
	}
	if diskZone, ok := params["source-disk-zone"]; ok {
		s.DiskZone = diskZone
	}
	if host, ok := params["host"]; ok {
		s.Host = host
	}
	if project, ok := params["project"]; ok {
		s.Project = project
	}
	if snapshotName, ok := params["snapshot-name"]; ok {
		s.SnapshotName = snapshotName
	}
	if snapshotType, ok := params["snapshot-type"]; ok {
		s.SnapshotType = snapshotType
	}
	if diskKeyFile, ok := params["source-disk-key-file"]; ok {
		s.DiskKeyFile = diskKeyFile
	}
	if storageLocation, ok := params["storage-location"]; ok {
		s.StorageLocation = storageLocation
	}
	if description, ok := params["snapshot-description"]; ok {
		s.Description = description
	}
	if logLevel, ok := params["loglevel"]; ok {
		s.LogLevel = logLevel
	}
	if labels, ok := params["labels"]; ok {
		s.Labels = labels
	}
	if freezeFileSystem, ok := params["freeze-file-system"]; ok {
		s.FreezeFileSystem, _ = strconv.ParseBool(freezeFileSystem)
	}
	if abandonPrepared, ok := params["abandon-prepared"]; ok {
		s.AbandonPrepared, _ = strconv.ParseBool(abandonPrepared)
	}
	if skipDBSnapshotForChangeDiskType, ok := params["skip-db-snapshot-for-change-disk-type"]; ok {
		s.SkipDBSnapshotForChangeDiskType, _ = strconv.ParseBool(skipDBSnapshotForChangeDiskType)
	}
	if confirmDataSnapshotAfterCreate, ok := params["confirm-data-snapshot-after-create"]; ok {
		s.ConfirmDataSnapshotAfterCreate, _ = strconv.ParseBool(confirmDataSnapshotAfterCreate)
	}
	if sendToMonitoring, ok := params["send-metrics-to-monitoring"]; ok {
		s.SendToMonitoring, _ = strconv.ParseBool(sendToMonitoring)
	}
	return s
}

// HANADiskBackupHandler is the handler for the hanadiskbackup command.
func HANADiskBackupHandler(ctx context.Context, command *gpb.AgentCommand, cp *ipb.CloudProperties) (string, subcommands.ExitStatus, bool) {
	s := buildHANADiskBackupCommand(command)
	lp := log.Parameters{OSType: runtime.GOOS}
	message, exitStatus := s.ExecuteAndGetMessage(ctx, nil, lp, cp)
	return message, exitStatus, RestartAgent
}
