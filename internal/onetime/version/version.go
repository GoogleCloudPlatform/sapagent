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

// Package version implements the one time execution mode for displaying version info.
package version

import (
	"context"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
)

// Version has args for version subcommands.
type Version struct{}

// Name implements the subcommand interface for version.
func (*Version) Name() string { return "version" }

// Synopsis implements the subcommand interface for version.
func (*Version) Synopsis() string { return "print Agent for SAP version information" }

// Usage implements the subcommand interface for version.
func (*Version) Usage() string {
	return "Usage: version\n"
}

// SetFlags implements the subcommand interface for version.
func (v *Version) SetFlags(fs *flag.FlagSet) {}

// Execute implements the subcommand interface for version.
func (v *Version) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	msg, exitStatus := v.Run(ctx, onetime.RunOptions{})
	fmt.Println(msg)
	return exitStatus
}

func (v *Version) Run(ctx context.Context, opts onetime.RunOptions) (string, subcommands.ExitStatus) {
	return onetime.GetAgentVersion(), subcommands.ExitSuccess
}
