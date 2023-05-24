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

package onetime

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"

	"flag"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/collectiondefinition"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/workloadmanager"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// RemoteValidation has args for remote subcommands.
type RemoteValidation struct {
	project, instanceid, instancename, zone string
}

// Name implements the subcommand interface for remote.
func (*RemoteValidation) Name() string { return "remote" }

// Synopsis implements the subcommand interface for remote.
func (*RemoteValidation) Synopsis() string {
	return "run in remote mode to collect workload manager metrics"
}

// Usage implements the subcommand interface for remote.
func (*RemoteValidation) Usage() string {
	return `remote -project=<project-id> -instance=<instance-id> -name=<instance-name> -zone=<instance-zone>\n`
}

// SetFlags implements the subcommand interface for remote.
func (r *RemoteValidation) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&r.project, "p", "", "project id of this instance")
	fs.StringVar(&r.project, "project", "", "project id of this instance")
	fs.StringVar(&r.instanceid, "i", "", "instance id of this instance")
	fs.StringVar(&r.instanceid, "instance", "", "instance id of this instance")
	fs.StringVar(&r.instancename, "n", "", "instance name of this instance")
	fs.StringVar(&r.instancename, "name", "", "instance name of this instance")
	fs.StringVar(&r.zone, "z", "", "zone of this instance")
	fs.StringVar(&r.zone, "zone", "", "zone of this instance")
}

// Execute implements the subcommand interface for remote.
func (r *RemoteValidation) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	gceService, err := gce.NewGCEClient(ctx)
	if err != nil {
		log.Print(fmt.Sprintf("ERROR: Failed to create GCE service: %v", err))
		return subcommands.ExitFailure
	}
	instanceInfoReader := instanceinfo.New(&instanceinfo.PhysicalPathReader{runtime.GOOS}, gceService)
	log.SetupLoggingToDiscard()

	loadOptions := collectiondefinition.LoadOptions{
		ReadFile: os.ReadFile,
		OSType:   runtime.GOOS,
		Version:  configuration.AgentVersion,
	}

	return r.remoteValidationHandler(ctx, instanceInfoReader, loadOptions)
}

var (
	osStatReader = workloadmanager.OSStatReader(func(f string) (os.FileInfo, error) {
		return os.Stat(f)
	})
	configFileReader = workloadmanager.ConfigFileReader(func(path string) (io.ReadCloser, error) {
		file, err := os.Open(path)
		var f io.ReadCloser = file
		return f, err
	})
	execute = commandlineexecutor.Execute(func(ctx context.Context, params commandlineexecutor.Params) commandlineexecutor.Result {
		return commandlineexecutor.ExecuteCommand(ctx, params)
	})
	exists = commandlineexecutor.Exists(func(exe string) bool {
		return commandlineexecutor.CommandExists(exe)
	})
	defaultTokenGetter = workloadmanager.DefaultTokenGetter(func(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
		return google.DefaultTokenSource(ctx, scopes...)
	})
	jsonCredentialsGetter = workloadmanager.JSONCredentialsGetter(func(ctx context.Context, json []byte, scopes ...string) (*google.Credentials, error) {
		return google.CredentialsFromJSON(ctx, json, scopes...)
	})
)

func (r *RemoteValidation) remoteValidationHandler(ctx context.Context, iir *instanceinfo.Reader, opts collectiondefinition.LoadOptions) subcommands.ExitStatus {
	if r.project == "" || r.instanceid == "" || r.zone == "" {
		log.Print("ERROR When running in remote mode the project, instanceid, and zone are required")
		return subcommands.ExitUsageError
	}

	cd, err := collectiondefinition.Load(opts)
	if err != nil {
		log.Print(fmt.Sprintf("ERROR: %v", err))
		return subcommands.ExitFailure
	}

	wlmparams := workloadmanager.Parameters{
		Config:                r.createConfiguration(),
		WorkloadConfig:        cd.GetWorkloadValidation(),
		Remote:                true,
		ConfigFileReader:      configFileReader,
		Execute:               execute,
		Exists:                exists,
		InstanceInfoReader:    *iir,
		OSStatReader:          osStatReader,
		DefaultTokenGetter:    defaultTokenGetter,
		JSONCredentialsGetter: jsonCredentialsGetter,
		OSType:                runtime.GOOS,
		OSReleaseFilePath:     workloadmanager.OSReleaseFilePath,
		InterfaceAddrsGetter:  net.InterfaceAddrs,
	}
	wlmparams.SetOSReleaseInfo()
	fmt.Println(workloadmanager.CollectMetricsToJSON(ctx, wlmparams))
	return subcommands.ExitSuccess
}

func (r *RemoteValidation) createConfiguration() *cpb.Configuration {
	return &cpb.Configuration{
		CloudProperties: &iipb.CloudProperties{
			ProjectId:    r.project,
			InstanceId:   r.instanceid,
			InstanceName: r.instancename,
			Zone:         r.zone,
		},
		AgentProperties: &cpb.AgentProperties{
			Name:    configuration.AgentName,
			Version: configuration.AgentVersion,
		},
	}
}
