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
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/backint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanainsights"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/logusage"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/maintenance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/migratehanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/remotevalidation"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/restore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/snapshot"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/validate"
	"github.com/GoogleCloudPlatform/sapagent/internal/startdaemon"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func registerSubCommands() {
	for _, command := range [...]subcommands.Command{
		&startdaemon.Daemon{},
		&logusage.LogUsage{},
		&maintenance.Mode{},
		&remotevalidation.RemoteValidation{},
		&snapshot.Snapshot{},
		&migratehanamonitoring.MigrateHANAMonitoring{},
		&validate.Validate{},
		&hanainsights.HANAInsights{},
		&backint.Backint{},
		&supportbundle.SupportBundle{},
		&restore.Restorer{},
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
		Level:  zapcore.InfoLevel,
	}

	cloudProps := metadataserver.FetchCloudProperties()
	if cloudProps != nil {
		lp.CloudLoggingClient = log.CloudLoggingClient(ctx, cloudProps.GetProjectId())
	}

	rc := int(subcommands.Execute(ctx, nil, lp, cloudProps))
	// making sure we flush the cloud logs.
	if lp.CloudLoggingClient != nil {
		log.FlushCloudLog()
		lp.CloudLoggingClient.Close()
	}
	os.Exit(rc)
}
