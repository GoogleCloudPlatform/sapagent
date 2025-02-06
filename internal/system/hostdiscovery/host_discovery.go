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

// Package hostdiscovery contains functions for performing SAP System discovery operations available only on the current host.
package hostdiscovery

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

var (
	fsMountRegex = regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+):(/[a-zA-Z0-9]+)`)
	crmIPRegex   = regexp.MustCompile(`params ip=([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	pcsIPRegex   = regexp.MustCompile(`ip=([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
)

// HostDiscovery is for discovering details that can only be performed on the host running the agent.
type HostDiscovery struct {
	Exists  commandlineexecutor.Exists
	Execute commandlineexecutor.Execute
}

// DiscoverCurrentHost invokes the necessary commands to discover the resources visible only
// on the current host.
func (d *HostDiscovery) DiscoverCurrentHost(ctx context.Context) []string {
	fs := d.discoverFilestores(ctx)

	addrs, err := d.discoverClusterAddresses(ctx)
	if err != nil {
		log.CtxLogger(ctx).Infow("Error discovering cluster", "error", err)
		return fs
	}

	return append(fs, addrs...)
}

func (d *HostDiscovery) discoverClusterAddresses(ctx context.Context) ([]string, error) {
	if d.Exists("crm") {
		return d.discoverClustersCRM(ctx)
	}
	if d.Exists("pcs") {
		return d.discoverClustersPCS(ctx)
	}
	return nil, errors.New("no cluster command found")
}

func (d *HostDiscovery) discoverClustersCRM(ctx context.Context) ([]string, error) {
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable:  "crm",
		ArgsToSplit: "config show",
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Infow("running crm", "error", result.Error, "stdOut", result.StdOut, "stdErr", result.StdErr)
		return nil, result.Error
	}

	var addrs []string
	matches := crmIPRegex.FindAllStringSubmatch(result.StdOut, -1)
	for _, match := range matches {
		if match == nil {
			continue
		}
		addrs = append(addrs, match[1])
	}
	return addrs, nil
}

func (d *HostDiscovery) discoverClustersPCS(ctx context.Context) ([]string, error) {
	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable:  "pcs",
		ArgsToSplit: "config show",
	})
	if result.Error != nil {
		return nil, result.Error
	}

	var addrs []string
	matches := pcsIPRegex.FindAllStringSubmatch(result.StdOut, -1)
	for _, match := range matches {
		if match == nil {
			continue
		}
		addrs = append(addrs, match[1])
	}
	return addrs, nil
}

func (d *HostDiscovery) discoverFilestores(ctx context.Context) []string {
	if !d.Exists("df") {
		return nil
	}

	result := d.Execute(ctx, commandlineexecutor.Params{
		Executable: "df",
		Args:       []string{"-h"},
	})
	if result.Error != nil {
		return nil
	}
	fs := []string{}
	for _, l := range strings.Split(result.StdOut, "\n") {
		matches := fsMountRegex.FindStringSubmatch(l)
		if len(matches) < 2 {
			continue
		}
		// The first match is the fully matched string, we only need the first submatch, the IP address.
		address := matches[1]
		fs = append(fs, address)
	}

	return fs
}
