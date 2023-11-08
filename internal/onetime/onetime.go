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

// Package onetime contains the common methods which will be used by multiple OTE features to
// avoid duplication.
package onetime

import (
	"context"
	"fmt"
	"os"

	"flag"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"golang.org/x/oauth2/google"
	"github.com/google/subcommands"
	"go.uber.org/zapcore/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

type (

	// GCEServiceFunc provides testable replacement for gce.New API.
	GCEServiceFunc func(context.Context) (*gce.GCE, error)

	// GCEInterface is the testable equivalent for gce.GCE for secret manager access.
	GCEInterface interface {
		GetSecret(ctx context.Context, projectID, secretName string) (string, error)
	}
)

// ConfigureUsageMetricsForOTE configures usage metrics for Agent in one time execution mode.
func ConfigureUsageMetricsForOTE(cp *iipb.CloudProperties, name, version string) {
	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            name,
		Version:         version,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}

// LogErrorToFileAndConsole prints out the error message to console and also to the log file.
func LogErrorToFileAndConsole(msg string, err error) {
	log.Print(msg + " " + err.Error() + "\n" + "Refer to log file at:" + log.GetLogFile())
	log.Logger.Errorw(msg, "error", err.Error())
}

// LogMessageToFileAndConsole prints out the console message and also to the log file.
func LogMessageToFileAndConsole(msg string) {
	log.Print(msg)
	log.Logger.Info(msg)
}

// SetupOneTimeLogging creates logging config for the agent's one time execution.
func SetupOneTimeLogging(params log.Parameters, subcommandName string, level zapcore.Level) log.Parameters {
	oteDir := `/var/log/google-cloud-sap-agent/`
	if params.OSType == "windows" {
		oteDir = `C:\Program Files\Google\google-cloud-sap-agent\logs\`
	}
	params.Level = level
	params.LogFileName = oteDir + subcommandName + ".log"
	params.CloudLogName = "google-cloud-sap-agent-" + subcommandName
	log.SetupLogging(params)
	// make all of the OTE log files global read + write
	os.Chmod(params.LogFileName, 0666)
	return params
}

// NewComputeService creates the compute service.
func NewComputeService(ctx context.Context) (cs *compute.Service, err error) {
	client, err := google.DefaultClient(ctx, compute.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("failure creating compute HTTP client" + err.Error())
	}
	if cs, err = compute.NewService(ctx, option.WithHTTPClient(client)); err != nil {
		return nil, fmt.Errorf("failure creating compute service" + err.Error())
	}
	return cs, nil
}

// HelpCommand is used to subcommand requests with help as an arg.
func HelpCommand(f *flag.FlagSet) subcommands.ExitStatus {
	PrintAgentVersion()
	f.Usage()
	return subcommands.ExitSuccess
}

// PrintAgentVersion prints the current version of the agent to stdout.
func PrintAgentVersion() {
	log.Print(fmt.Sprintf("Google Cloud Agent for SAP version %s", configuration.AgentVersion))
}
