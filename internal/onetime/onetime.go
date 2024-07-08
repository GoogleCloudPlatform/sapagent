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
	"go.uber.org/zap/zapcore"
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

	// InternallyInvokedOTE is a struct which contains necessary context for an OTE when it is not
	// invoked via command line.
	// Follow the practice used in third_party/sapagent/internal/onetime/hanachangedisktype/hanachangedisktype.go
	// to invoke an OTE internally.
	InternallyInvokedOTE struct {
		InvokedBy string
		Lp        log.Parameters
		Cp        *iipb.CloudProperties
	}

	// InitOptions is a struct which contains necessary context for Init function to initialise the OTEs.
	InitOptions struct {
		Help                    bool
		Name, LogLevel, LogPath string
		Fs                      *flag.FlagSet
		IIOTE                   *InternallyInvokedOTE
	}

	// RunOptions is a struct which contains necessary context for the Run function.
	RunOptions struct {
		CloudProperties *iipb.CloudProperties
	}
)

// Init performs all initialization steps for OTE subcommands.
// Args are verified, usage metrics configured, and logging is set up.
// If name is empty, file logging and usage metrics will not occur.
// Returns the parsed args, and a bool indicating if initialization completed.
func Init(ctx context.Context, opt InitOptions, args ...any) (log.Parameters, *iipb.CloudProperties, subcommands.ExitStatus, bool) {
	// If the user provided a log path, check that it
	// is not a path to directory to prevent
	// unintentional directory access issues for the user.
	if _, err := os.ReadDir(opt.LogPath); opt.LogPath != "" && err == nil {
		log.CtxLogger(ctx).Errorf("Invalid log-path provided: %s is a directory. Please provide a path to a file", opt.LogPath)
		return log.Parameters{}, nil, subcommands.ExitUsageError, false
	}
	if opt.IIOTE != nil {
		// OTE is invoked internally, no need to run the below assertions, setting up one time logging
		// and usage metrics is enough.
		SetupOneTimeLogging(opt.IIOTE.Lp, opt.IIOTE.InvokedBy, log.StringLevelToZapcore(opt.LogLevel))
		ConfigureUsageMetricsForOTE(opt.IIOTE.Cp, "", "")
		return opt.IIOTE.Lp, opt.IIOTE.Cp, subcommands.ExitSuccess, true
	}
	if opt.Help {
		return log.Parameters{}, nil, HelpCommand(opt.Fs), false
	}

	if len(args) < 3 {
		log.CtxLogger(ctx).Errorf("Not enough args for Init(). Want: 3, Got: %d", len(args))
		return log.Parameters{}, nil, subcommands.ExitUsageError, false
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return log.Parameters{}, nil, subcommands.ExitUsageError, false
	}
	cloudProps, ok := args[2].(*iipb.CloudProperties)
	if !ok {
		log.CtxLogger(ctx).Errorf("Unable to assert args[2] of type %T to *iipb.CloudProperties.", args[2])
		return log.Parameters{}, nil, subcommands.ExitUsageError, false
	}

	if opt.Name == "" {
		return lp, cloudProps, subcommands.ExitSuccess, true
	}
	if opt.LogLevel == "" {
		opt.LogLevel = "info"
	}
	if opt.LogPath != "" {
		lp.LogFileName = opt.LogPath
	}
	SetupOneTimeLogging(lp, opt.Name, log.StringLevelToZapcore(opt.LogLevel))
	ConfigureUsageMetricsForOTE(cloudProps, "", "")
	return lp, cloudProps, subcommands.ExitSuccess, true
}

// ConfigureUsageMetricsForOTE configures usage metrics for Agent in one time execution mode.
func ConfigureUsageMetricsForOTE(cp *iipb.CloudProperties, name, version string) {
	if cp == nil {
		log.Logger.Error("CloudProperties is nil, not configuring usage metrics for OTE.")
		return
	}
	if name == "" {
		name = configuration.AgentName
	}
	if version == "" {
		version = configuration.AgentVersion
	}

	usagemetrics.SetAgentProperties(&cpb.AgentProperties{
		Name:            name,
		Version:         version,
		LogUsageMetrics: true,
	})
	usagemetrics.SetCloudProperties(cp)
}

// LogErrorToFileAndConsole prints out the error message to console and also to the log file.
func LogErrorToFileAndConsole(ctx context.Context, msg string, err error) {
	log.Print(msg + " " + err.Error() + "\n" + "Refer to log file at:" + log.GetLogFile())
	log.CtxLogger(ctx).Errorw(msg, "error", err.Error())
}

// LogMessageToFileAndConsole prints out the console message and also to the log file.
func LogMessageToFileAndConsole(ctx context.Context, msg string) {
	fmt.Println(msg)
	log.CtxLogger(ctx).Info(msg)
}

// SetupOneTimeLogging creates logging config for the agent's one time execution.
func SetupOneTimeLogging(params log.Parameters, subcommandName string, level zapcore.Level) log.Parameters {
	// If the user did not override the log file name, use the default log file name.
	if params.LogFileName == "" {
		oteDir := `/var/log/google-cloud-sap-agent/`
		if params.OSType == "windows" {
			oteDir = `C:\Program Files\Google\google-cloud-sap-agent\logs\`
		}
		params.LogFileName = oteDir + subcommandName + ".log"
	}
	params.Level = level
	params.CloudLogName = "google-cloud-sap-agent-" + subcommandName
	log.SetupLogging(params)
	// Make all of the OTE log files read + write for user and group.
	// The directory is owned by sapsys group and in general, sidadm users
	// are members of sapsys group.
	os.Chmod(params.LogFileName, 0660)
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

// GetAgentVersion returns the current version of the agent as a string.
func GetAgentVersion() string {
	return fmt.Sprintf("Google Cloud Agent for SAP version %s.%s", configuration.AgentVersion, configuration.AgentBuildChange)
}

// PrintAgentVersion prints the current version of the agent to stdout.
func PrintAgentVersion() subcommands.ExitStatus {
	fmt.Println(GetAgentVersion() + "\n")
	return subcommands.ExitSuccess
}

// LogFilePath returns the log file path for the OTE invoked depending if it is invoked internally or via command line.
func LogFilePath(name string, iiote *InternallyInvokedOTE) string {
	if iiote == nil {
		return fmt.Sprintf("/var/log/google-cloud-sap-agent/%s.log", name)
	}
	return fmt.Sprintf("/var/log/google-cloud-sap-agent/%s.log", iiote.InvokedBy)
}
