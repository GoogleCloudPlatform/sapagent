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

// Package instancemetadata implements the one time execution mode for fetching the metadata of the
// instance on which the agent is running.
package instancemetadata

import (
	"context"
	"fmt"
	"runtime"

	"flag"
	"google.golang.org/protobuf/encoding/prototext"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"

	impb "github.com/GoogleCloudPlatform/sapagent/protos/instancemetadata"
)

type (
	// InstanceMetadata stores the arguments for the instancemetadata subcommand.
	InstanceMetadata struct {
		logLevel  string
		logPath   string
		help      bool
		oteLogger *onetime.OTELogger
	}
)

// Name implements the subcommand interface for instancemetadata.
func (*InstanceMetadata) Name() string { return "instancemetadata" }

// Synopsis implements the subcommand interface for instancemetadata.
func (*InstanceMetadata) Synopsis() string { return "fetch the metadata of the instance" }

// Usage implements the subcommand interface for instancemetadata.
func (*InstanceMetadata) Usage() string {
	return "Usage: instancemetadata [-loglevel=<debug|info|warn|error>] [-log-path=<log-path>] [-h]\n"
}

// SetFlags implements the subcommand interface for instancemetadata.
func (m *InstanceMetadata) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&m.help, "h", false, "Display help")
	fs.BoolVar(&m.help, "help", false, "Display help")
	fs.StringVar(&m.logLevel, "loglevel", "info", "Sets the logging level for a log file")
	fs.StringVar(&m.logPath, "log-path", "", "The log path to write the log file (optional), default value is /var/log/google-cloud-sap-agent/instancemetadata.log")
}

// Execute implements the subcommand interface for instancemetadata.
func (m *InstanceMetadata) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	_, cp, exitStatus, completed := onetime.Init(ctx, onetime.InitOptions{
		Name:     m.Name(),
		Help:     m.help,
		LogLevel: m.logLevel,
		LogPath:  m.logPath,
		Fs:       f,
	}, args...)
	if !completed {
		return exitStatus
	}

	_, status := m.Run(ctx, onetime.CreateRunOptions(cp, false))
	if status != subcommands.ExitSuccess {
		m.oteLogger.LogMessageToFileAndConsole(ctx, "Failed to fetch instance metadata")
	}
	return status
}

// Run executes the MetadataHandler and returns the response.
func (m *InstanceMetadata) Run(ctx context.Context, opts *onetime.RunOptions) (*impb.Metadata, subcommands.ExitStatus) {
	m.oteLogger = onetime.CreateOTELogger(opts.DaemonMode)
	return m.metadataHandler(ctx)
}

func (m *InstanceMetadata) metadataHandler(ctx context.Context) (*impb.Metadata, subcommands.ExitStatus) {
	response := &impb.Metadata{
		OsName:           runtime.GOOS,
		AgentVersion:     configuration.AgentVersion,
		AgentBuildChange: configuration.AgentBuildChange,
	}
	m.oteLogger.LogMessageToFileAndConsole(ctx, fmt.Sprintf("Instance Metadata: %s", prototext.Format(response)))
	return response, subcommands.ExitSuccess
}
