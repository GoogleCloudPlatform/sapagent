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

// Package main is the main entry point for the binary that gets sent to instances to collect metrics remotely.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

const usage = `ERROR: Usage of google-cloud-sap-agent-remote:
  -h, --help prints help information
	-p=project_id, --project=project_id [required] project id of this instance
	-i=instance_id, --instance=instance_id [required] instance id of this instance
	-n=instance_name, --name=instance_name [required] instance name of this instance
	-z=zone, --zone=zone [required] zone of this instance
`

var (
	config       *cnfpb.Configuration
	help         bool
	project      string
	instanceid   string
	instancename string
	zone         string
	osStatReader = workloadmanager.OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
	configFileReader = workloadmanager.ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := os.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	commandRunnerNoSpace = commandlineexecutor.CommandRunnerNoSpace(func(exe string, args ...string) (string, string, error) {
		return commandlineexecutor.ExecuteCommand(exe, args...)
	})
	commandRunner = commandlineexecutor.CommandRunner(func(exe string, args string) (string, string, error) {
		return commandlineexecutor.ExpandAndExecuteCommand(exe, args)
	})
	commandExistsRunner = commandlineexecutor.CommandExistsRunner(func(exe string) bool {
		return commandlineexecutor.CommandExists(exe)
	})
	defaultTokenGetter = workloadmanager.DefaultTokenGetter(func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
		return google.DefaultTokenSource(ctx, scopes...)
	})
	jsonCredentialsGetter = workloadmanager.JSONCredentialsGetter(func(ctx context.Context, json []byte, scopes ...string) (*google.Credentials, error) {
		return google.CredentialsFromJSON(ctx, json, scopes...)
	})
)

func setupFlags() {
	flag.BoolVar(&help, "help", false, "display help")
	flag.BoolVar(&help, "h", false, "display help")
	flag.StringVar(&project, "p", "", "project id of this instance")
	flag.StringVar(&project, "project", "", "project id of this instance")
	flag.StringVar(&instanceid, "i", "", "instance id of this instance")
	flag.StringVar(&instanceid, "instance", "", "instance id of this instance")
	flag.StringVar(&instancename, "n", "", "instance name of this instance")
	flag.StringVar(&instancename, "name", "", "instance name of this instance")
	flag.StringVar(&zone, "z", "", "zone of this instance")
	flag.StringVar(&zone, "zone", "", "zone of this instance")
	flag.Usage = func() { fmt.Print(usage) }
}

func parseFlags(exit bool) {
	flag.Parse()

	if help || project == "" || instanceid == "" || instancename == "" || zone == "" {
		log.Print(usage)
		if exit {
			os.Exit(0)
		}
	}
	config = &cnfpb.Configuration{}
	config.CloudProperties = &ipb.CloudProperties{}
	config.CloudProperties.ProjectId = project
	config.CloudProperties.InstanceId = instanceid
	config.CloudProperties.InstanceName = instancename
	config.CloudProperties.Zone = zone
}

func collectMetrics(goos string) {
	ctx := context.Background()
	gceService, err := gce.New(ctx)
	if err != nil {
		log.Print(fmt.Sprintf("ERROR: Failed to create GCE service: %v", err))
		os.Exit(0)
	}
	ppr := &instanceinfo.PhysicalPathReader{goos}
	instanceInfoReader := instanceinfo.New(ppr, gceService)
	wlmparams := workloadmanager.Parameters{
		Config:                config,
		Remote:                false,
		ConfigFileReader:      configFileReader,
		CommandRunner:         commandRunner,
		CommandRunnerNoSpace:  commandRunnerNoSpace,
		CommandExistsRunner:   commandExistsRunner,
		InstanceInfoReader:    *instanceInfoReader,
		OSStatReader:          osStatReader,
		DefaultTokenGetter:    defaultTokenGetter,
		JSONCredentialsGetter: jsonCredentialsGetter,
		OSType:                goos,
	}
	log.Print(workloadmanager.CollectMetricsToJSON(ctx, wlmparams))
	os.Exit(0)
}

func main() {
	log.SetupLoggingToDiscard()
	setupFlags()
	parseFlags(true)
	collectMetrics(runtime.GOOS)
}
