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

// Package fake contains types and methods to fake the HostDiscovery interface.
package fake

import (
	"context"

	"github.com/GoogleCloudPlatform/sapagent/internal/system/hostdiscovery"
)

// HostDiscovery provides a fake implementation of the public HostDiscovery interface.
type HostDiscovery struct {
	DiscoverCurrentHostResp      []hostdiscovery.HostData
	discoverCurrentHostCallCount int
}

// DiscoverCurrentHost is a fake implementation of the HostDiscovery DiscoverCurrentHost method.
func (d *HostDiscovery) DiscoverCurrentHost(ctx context.Context) hostdiscovery.HostData {
	defer func() {
		d.discoverCurrentHostCallCount++
	}()

	return d.DiscoverCurrentHostResp[d.discoverCurrentHostCallCount]
}
