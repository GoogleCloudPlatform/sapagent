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

package configurebackint

import (
	"context"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

// ConfigureBackint has args for configurebackint subcommands.
type ConfigureBackint struct{}

// Name implements the subcommand interface for configurebackint.
func (*ConfigureBackint) Name() string { return "" }

// Synopsis implements the subcommand interface for configurebackint.
func (*ConfigureBackint) Synopsis() string { return "" }

// Usage implements the subcommand interface for configurebackint.
func (*ConfigureBackint) Usage() string { return "" }

// SetFlags implements the subcommand interface for configurebackint.
func (b *ConfigureBackint) SetFlags(fs *flag.FlagSet) {}

// Execute implements the subcommand interface for configurebackint.
func (b *ConfigureBackint) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	log.CtxLogger(ctx).Info("Configurebackint is not supported for windows platforms.")
	return subcommands.ExitSuccess
}
