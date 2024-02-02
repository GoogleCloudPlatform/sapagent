/*
Copyright 2023 Google LLC

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

// Package fake contains a set of functionality to discover SAP application details running on the current host, and their related components.
package fake

import (
	"context"

	"github.com/GoogleCloudPlatform/sapagent/internal/system/appsdiscovery"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sappb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

// SapDiscovery provides a fake implementation of the appsdiscovery.SapDiscovery struct.
type SapDiscovery struct {
	DiscoverSapAppsResp      [][]appsdiscovery.SapSystemDetails
	DiscoverSapAppsCallCount int
}

// DiscoverSAPApps fakes calls to the appsdiscovery.DiscoverSAPApps method.
func (f *SapDiscovery) DiscoverSAPApps(ctx context.Context, apps *sappb.SAPInstances, conf *cpb.DiscoveryConfiguration) []appsdiscovery.SapSystemDetails {
	defer func() { f.DiscoverSapAppsCallCount++ }()
	return f.DiscoverSapAppsResp[f.DiscoverSapAppsCallCount]
}
