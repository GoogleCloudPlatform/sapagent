/*
Copyright 2022 Google LLC

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

// Package main serves as the Main entry point for the GC SAP Agent.
package main

import (
	"context"
	"os"
	"runtime"

	"flag"

	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/startdaemon"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func registerSubCommands() {
	for _, command := range [...]subcommands.Command{
		&startdaemon.Daemon{},
		&onetime.LogUsage{},
		&onetime.MaintenanceMode{},
		&onetime.RemoteValidation{},
		&onetime.Snapshot{},
		&onetime.MigrateHANAMonitoring{},
		subcommands.HelpCommand(),  // Implement "help"
		subcommands.FlagsCommand(), // Implement "flags"
	} {
		subcommands.Register(command, "")
	}
	flag.Parse()
}

func main() {
	registerSubCommands()
	ctx := context.Background()
	lp := log.Parameters{
		OSType: runtime.GOOS,
		Level:  cpb.Configuration_INFO,
	}

	cloudProps := metadataserver.FetchCloudProperties()
	lp.CloudLoggingClient = log.CloudLoggingClient(ctx, cloudProps.ProjectId)

	rc := int(subcommands.Execute(ctx, nil, lp))
	// making sure we flush the cloud logs.
	if lp.CloudLoggingClient != nil {
		log.FlushCloudLog()
		lp.CloudLoggingClient.Close()
	}
	os.Exit(rc)
}
