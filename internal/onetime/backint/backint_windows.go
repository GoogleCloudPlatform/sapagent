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

package backint

import (
	"context"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
)

// Backint has args for backint subcommands.
type Backint struct {
	IIOTEParams        *onetime.InternallyInvokedOTE
	User, Function     string
	OutFile, ParamFile string
}

// Name implements the subcommand interface for backint.
func (*Backint) Name() string { return "" }

// Synopsis implements the subcommand interface for backint.
func (*Backint) Synopsis() string { return "" }

// Usage implements the subcommand interface for backint.
func (*Backint) Usage() string { return "" }

// SetFlags implements the subcommand interface for backint.
func (b *Backint) SetFlags(fs *flag.FlagSet) {}

// Execute implements the subcommand interface for backint.
func (b *Backint) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("Backint is not supported for windows platforms.")
	return subcommands.ExitUsageError
}

// ExecuteAndGetMessage executes the backint command and returns the message and exit status.
func (b *Backint) Run(ctx context.Context, opts *onetime.RunOptions) (string, subcommands.ExitStatus) {
	return "Backint is not supported for windows platforms.", subcommands.ExitUsageError
}
