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

package onetime

import (
	"context"
	"fmt"

	"flag"
	"github.com/kardianos/service"
	"github.com/google/subcommands"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

var winlogger service.Logger

// Winservice has args for winservice subcommands.
type Winservice struct {
	Service service.Service
	Daemon  subcommands.Command
	// NOTE: Context is needed because kardianos/service for windows does not pass the context
	// to the service.Start(), service.Stop(), and service .Run() methods.
	ctx        context.Context
	lp         log.Parameters
	cloudProps *iipb.CloudProperties
}

// Name implements the subcommand interface for winservice.
func (*Winservice) Name() string { return "winservice" }

// Synopsis implements the subcommand interface for version.
func (*Winservice) Synopsis() string { return "Start the Google Cloud SAP Agent Windows service" }

// Usage implements the subcommand interface for verswinserviceion.
func (*Winservice) Usage() string {
	return "Usage: winservice\n"
}

// SetFlags implements the subcommand interface for winservice.
func (w *Winservice) SetFlags(fs *flag.FlagSet) {}

// Execute implements the subcommand interface for winservice.
func (w *Winservice) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	msg, exitStatus := w.Run(ctx, RunOptions{})
	fmt.Println(msg)
	return exitStatus
}

// Start implements the subcommand interface for winservice.
func (w *Winservice) Start(s service.Service) error {
	winlogger.Info("Winservice Start - starting the Google Cloud SAP Agent service")
	go w.run()
	return nil
}

// Stop implements the subcommand interface for winservice.
func (w *Winservice) Stop(s service.Service) error {
	winlogger.Info("Winservice Run - stopping the Google Cloud SAP Agent service")
	return nil
}

func (w *Winservice) run() {
	winlogger.Info("Winservice Run - executing the daemon for the Google Cloud SAP Agent service")
	rc := int(w.Daemon.Execute(w.ctx, nil, nil, w.lp, w.cloudProps))
	winlogger.Info(fmt.Sprintf("Winservice Run - daemon execution is complete for the Google Cloud SAP Agent service, exit code: %d", rc))
}

// NewWinServiceCommand returns a new winservice command.
func NewWinServiceCommand(ctx context.Context, daemon subcommands.Command, lp log.Parameters, cloudProps *iipb.CloudProperties) subcommands.Command {
	w := &Winservice{
		Daemon:     daemon,
		ctx:        ctx,
		lp:         lp,
		cloudProps: cloudProps,
	}
	return w
}

// Run the winservice command.
func (w *Winservice) Run(ctx context.Context, opts RunOptions) (string, subcommands.ExitStatus) {
	fmt.Println("Winservice Execute - starting the Google Cloud SAP Agent service")
	config := &service.Config{
		Name:        "google-cloud-sap-agent",
		DisplayName: "Google Cloud SAP Agent",
		Description: "Google Cloud SAP Agent",
	}
	s, err := service.New(w, config)
	if err != nil {
		return fmt.Errorf("Winservice Execute - error creating Windows service manager interface for the Google Cloud SAP Agent service: %s", err).Error(), subcommands.ExitFailure
	}
	w.Service = s

	winlogger, err = s.Logger(nil)
	if err != nil {
		return fmt.Errorf("Winservice Execute - error creating Windows Event Logger for the Google Cloud SAP Agent service: %s", err).Error(), subcommands.ExitFailure
	}
	fmt.Println("Winservice Execute - Starting the Google Cloud SAP Agent service")
	winlogger.Info("Winservice Execute - Starting the Google Cloud SAP Agent service")

	err = s.Run()
	if err != nil {
		winlogger.Error(err)
	}
	winlogger.Info("Winservice Execute - The Google Cloud SAP Agent service is shutting down")
	return "", subcommands.ExitSuccess
}
