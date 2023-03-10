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
	"os"
	"runtime"

	"flag"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
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
	gceService, err := gce.New(ctx)
	if err != nil {
		log.Print(fmt.Sprintf("ERROR: Failed to create GCE service: %v", err))
		return subcommands.ExitFailure
	}
	instanceInfoReader := instanceinfo.New(&instanceinfo.PhysicalPathReader{runtime.GOOS}, gceService)
	return r.remoteValidationHandler(ctx, instanceInfoReader)
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

func (r *RemoteValidation) remoteValidationHandler(ctx context.Context, instanceInfoReader *instanceinfo.Reader) subcommands.ExitStatus {
	log.SetupOneTimeLogging(runtime.GOOS, r.Name(), cpb.Configuration_INFO)
	if r.project == "" || r.instanceid == "" || r.zone == "" {
		log.Print("ERROR When running in remote mode the project, instanceid, and zone are required")
		return subcommands.ExitUsageError
	}

	config := &cpb.Configuration{}
	config.CloudProperties = &iipb.CloudProperties{}
	config.CloudProperties.ProjectId = r.project
	config.CloudProperties.InstanceId = r.instanceid
	config.CloudProperties.InstanceName = r.instancename
	config.CloudProperties.Zone = r.zone

	wlmparams := workloadmanager.Parameters{
		Config:                config,
		Remote:                true,
		ConfigFileReader:      configFileReader,
		CommandRunner:         commandRunner,
		CommandRunnerNoSpace:  commandRunnerNoSpace,
		CommandExistsRunner:   commandExistsRunner,
		InstanceInfoReader:    *instanceInfoReader,
		OSStatReader:          osStatReader,
		DefaultTokenGetter:    defaultTokenGetter,
		JSONCredentialsGetter: jsonCredentialsGetter,
		OSType:                runtime.GOOS,
	}
	fmt.Println(workloadmanager.CollectMetricsToJSON(ctx, wlmparams))
	return subcommands.ExitSuccess
}
