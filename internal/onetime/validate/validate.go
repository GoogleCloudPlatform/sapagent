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
	"fmt"
	"os"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// Validate implements the subcommand interface.
type Validate struct {
	workloadCollection string
	help, version      bool
	logLevel           string
}

// Name returns the name of the command.
func (*Validate) Name() string { return "validate" }

// Synopsis returns a short string (less than one line) describing the command.
func (*Validate) Synopsis() string {
	return "validate the Agent for SAP - workload manager collection definition file"
}

// Usage returns a long string explaining the command and giving usage information.
func (*Validate) Usage() string {
	return "Usage: validate [-workloadcollection <filename>] [-h] [-v] [-loglevel]=<debug|info|warn|error>\n"
}

// SetFlags adds the flags for this command to the specified set.
func (v *Validate) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&v.workloadCollection, "workloadcollection", "", "workload collection filename")
	fs.StringVar(&v.workloadCollection, "wc", "", "workload collection filename")
	fs.BoolVar(&v.help, "h", false, "Displays help")
	fs.BoolVar(&v.version, "v", false, "Displays the current version of the agent")
	fs.StringVar(&v.logLevel, "loglevel", "info", "Sets the logging level for a log file")
}

// Execute executes the command and returns an ExitStatus.
func (v *Validate) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, _, exitStatus, completed := onetime.Init(ctx, onetime.Options{
		Name:     v.Name(),
		Help:     v.help,
		Version:  v.version,
		LogLevel: v.logLevel,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	return v.validateHandler(ctx)
}

func (v *Validate) validateHandler(ctx context.Context) subcommands.ExitStatus {
	if v.workloadCollection != "" {
		return v.validateWorkloadCollectionHandler(ctx, os.ReadFile, v.workloadCollection)
	}
	return subcommands.ExitSuccess
}

func (v *Validate) validateWorkloadCollectionHandler(ctx context.Context, read collectiondefinition.ReadFile, path string) subcommands.ExitStatus {
	fmt.Println("Beginning workload collection definition validation for file: " + path)
	log.Logger.Infow("Beginning workload collection validation.", "path", path)
	cd, err := collectiondefinition.FromJSONFile(ctx, read, path)
	if err != nil {
		onetime.LogErrorToFileAndConsole("Failed to load workload collection definition file.", err)
		return subcommands.ExitFailure
	}

	validator := collectiondefinition.NewValidator(configuration.AgentVersion, cd)
	validator.Validate()
	if !validator.Valid() {
		err := collectiondefinition.ValidationError{FailureCount: validator.FailureCount()}
		onetime.LogErrorToFileAndConsole("Workload collection definition validation Result: FAILURE", err)
	} else {
		fmt.Println("Workload collection definition validation Result: SUCCESS")
		log.Logger.Info("Workload collection definition validation Result: SUCCESS")
	}
	return subcommands.ExitSuccess
}
