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

// Package validate implements the one time execution mode for validate.
package validate

import (
	"context"
	"os"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
)

// Validate implements the subcommand interface.
type Validate struct {
	workloadCollection string
}

// Name returns the name of the command.
func (*Validate) Name() string { return "validate" }

// Synopsis returns a short string (less than one line) describing the command.
func (*Validate) Synopsis() string { return "validate an Agent for SAP configuration file" }

// Usage returns a long string explaining the command and giving usage information.
func (*Validate) Usage() string {
	return "validate [-workloadcollection <filename>]\n"
}

// SetFlags adds the flags for this command to the specified set.
func (v *Validate) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&v.workloadCollection, "workloadcollection", "", "workload collection filename")
	fs.StringVar(&v.workloadCollection, "wc", "", "workload collection filename")
}

// Execute executes the command and returns an ExitStatus.
func (v *Validate) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	log.SetupOneTimeLogging(lp, v.Name())
	return v.validateHandler()
}

func (v *Validate) validateHandler() subcommands.ExitStatus {
	if v.workloadCollection != "" {
		return v.validateWorkloadCollectionHandler(os.ReadFile, v.workloadCollection)
	}
	return subcommands.ExitSuccess
}

func (v *Validate) validateWorkloadCollectionHandler(read collectiondefinition.ReadFile, path string) subcommands.ExitStatus {
	log.Print("Beginning workload collection validation for file: " + path)
	log.Logger.Infow("Beginning workload collection validation.", "path", path)
	cd, err := collectiondefinition.FromJSONFile(read, path)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Failed to load workload collection file.", err)
		return subcommands.ExitFailure
	}

	validator := collectiondefinition.NewValidator(configuration.AgentVersion, cd)
	validator.Validate()
	if !validator.Valid() {
		err := collectiondefinition.ValidationError{FailureCount: validator.FailureCount()}
		onetime.LogErrorToFileAndConsole("Validation Result: FAILURE", err)
	} else {
		log.Print("Validation Result: SUCCESS")
		log.Logger.Info("Validation Result: SUCCESS")
	}
	return subcommands.ExitSuccess
}
