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

// Package protostruct provides utilities for converting between proto and struct.
package protostruct

import (
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/gce/metadataserver"
)

// ConvertCloudPropertiesToStruct converts Cloud Properties proto to CloudProperties struct.
func ConvertCloudPropertiesToStruct(cp *ipb.CloudProperties) *metadataserver.CloudProperties {
	return &metadataserver.CloudProperties{
		ProjectID:        cp.GetProjectId(),
		InstanceID:       cp.GetInstanceId(),
		Zone:             cp.GetZone(),
		InstanceName:     cp.GetInstanceName(),
		Image:            cp.GetImage(),
		NumericProjectID: cp.GetNumericProjectId(),
		Region:           cp.GetRegion(),
		MachineType:      cp.GetMachineType(),
		Scopes:           cp.GetScopes(),
	}
}

// ConvertCloudPropertiesToProto converts Cloud Properties struct to CloudProperties proto.
func ConvertCloudPropertiesToProto(cp *metadataserver.CloudProperties) *ipb.CloudProperties {
	if cp == nil {
		return nil
	}
	return &ipb.CloudProperties{
		ProjectId:        cp.ProjectID,
		InstanceId:       cp.InstanceID,
		Zone:             cp.Zone,
		InstanceName:     cp.InstanceName,
		Image:            cp.Image,
		NumericProjectId: cp.NumericProjectID,
		Region:           cp.Region,
		MachineType:      cp.MachineType,
		Scopes:           cp.Scopes,
	}
}
