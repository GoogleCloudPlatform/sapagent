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

// Package systemdiscovery implements the system discovery
// as an OTE to discover SAP systems running on the host.
package systemdiscovery

import (
	"context"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/system"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// SystemDiscovery will have the arguments needed for the systemdiscovery commands.
type SystemDiscovery struct {
	// TODO: b/346952991 - Add arguments for SystemDiscovery struct.
}

// Name implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Name() string {
	return "systemdiscovery"
}

// Synopsis implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Synopsis() string {
	return "discover SAP systems that are running on the host."
}

// Usage implements the subcommand interface for systemdiscovery.
func (*SystemDiscovery) Usage() string {
	return "Usage: systemdiscovery\n"
}

// SetFlags implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) SetFlags(fs *flag.FlagSet) {
	// TODO: b/346953604 - Create flags for systemdiscovery.
}

// Execute implements the subcommand interface for systemdiscovery.
func (sd *SystemDiscovery) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("The systemdiscovery command for OTE mode was invoked.")
	return subcommands.ExitSuccess
}

// SystemDiscoveryHandler implements the execution logic of the systemdiscovery command.
//
// It is exported and made available to be used internally.
func (sd *SystemDiscovery) SystemDiscoveryHandler() (*system.Discovery, error) {
	// TODO: b/346953366 - Implement the SystemDiscoveryHandler.
	return nil, nil
}
