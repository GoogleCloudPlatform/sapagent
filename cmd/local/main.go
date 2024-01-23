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
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"flag"
	"github.com/google/subcommands"
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/backint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/configure"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/gcbdr"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskbackup"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanadiskrestore"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/hanainsights"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/installbackint"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/logusage"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/maintenance"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/migratehanamonitoring"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/migratehmadashboards"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/readmetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/reliability"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/remotevalidation"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/supportbundle"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/validate"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime/version"
	"github.com/GoogleCloudPlatform/sapagent/internal/startdaemon"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/filesystem"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce/metadataserver"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

const cn = "google_cloud_sap_agent"

// Registering "help" as a flag makes "-help" and "--help" return help messages.
var (
	_ = flag.Bool("h", false, "Should we display a help message")
	_ = flag.Bool("help", false, "Should we display a help message")
)

func registerSubCommands() {
	scs := [...]subcommands.Command{
		&startdaemon.Daemon{},
		&logusage.LogUsage{},
		&maintenance.Mode{},
		&remotevalidation.RemoteValidation{},
		&hanadiskbackup.Snapshot{},
		&migratehanamonitoring.MigrateHANAMonitoring{},
		&validate.Validate{},
		&hanainsights.HANAInsights{},
		&backint.Backint{},
		&supportbundle.SupportBundle{},
		&hanadiskrestore.Restorer{},
		&readmetrics.ReadMetrics{},
		&installbackint.InstallBackint{},
		&configure.Configure{},
		&version.Version{},
		&migratehmadashboards.MigrateHMADashboards{},
		&reliability.Reliability{},
		&gcbdr.Discovery{FSH: filesystem.Helper{}},
		subcommands.HelpCommand(), // Implement "help"
	}
	for _, command := range scs {
		subcommands.Register(command, "")
	}
	flag.Parse()
	subcommands.DefaultCommander.Explain = func(w io.Writer) {
		onetime.PrintAgentVersion()
		fmt.Fprintf(w, "Usage: %s <subcommand> <subcommand args>\n\n", cn)
		fmt.Fprintf(w, "Subcommands:\n")
		for _, cmd := range scs {
			fmt.Fprintf(w, "\t%-15s  %s\n", cmd.Name(), cmd.Synopsis())
		}
		fmt.Fprintf(w, "\n")
		return
	}
}

func main() {
	registerSubCommands()
	ctx := context.Background()
	lp := log.Parameters{
		OSType:     runtime.GOOS,
		Level:      zapcore.InfoLevel,
		LogToCloud: true,
	}

	cloudProps := metadataserver.FetchCloudProperties()
	if cloudProps != nil {
		lp.CloudLoggingClient = log.CloudLoggingClient(ctx, cloudProps.GetProjectId())
	}
	rc := int(subcommands.Execute(ctx, nil, lp, cloudProps))
	// making sure we flush the cloud logs.
	if lp.CloudLoggingClient != nil {
		flushTimer := time.AfterFunc(30*time.Second, func() {
			log.Logger.Warn("Cloud logging client failed to flush before deadline, exiting.")
			os.Exit(rc)
		})
		log.FlushCloudLog()
		lp.CloudLoggingClient.Close()
		flushTimer.Stop()
	}
	os.Exit(rc)
}
