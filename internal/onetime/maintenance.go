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

// Package onetime processes subcommand flags for onetime execution in agent for SAP.
package onetime

import (
	"context"
	"fmt"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/maintenance"
)

// MaintenanceMode  has args for maintenance subcommands.
type MaintenanceMode struct {
	sid          string
	enable, show bool
}

// Name implements the subcommand interface for maintenance.
func (*MaintenanceMode) Name() string { return "maintenance" }

// Synopsis implements the subcommand interface for maintenance.
func (*MaintenanceMode) Synopsis() string { return "configure maintenance mode" }

// Usage implements the subcommand interface for maintenance.
func (*MaintenanceMode) Usage() string {
	return "maintenance [-enable=true|false -sid=<SAP System Identifier>] [show]\\n"
}

// SetFlags implements the subcommand interface for maintenance.
func (m *MaintenanceMode) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&m.sid, "sid", "", "SAP System Identifier")
	fs.BoolVar(&m.enable, "enable", false, "Enable maintenance mode for SID")
	fs.BoolVar(&m.show, "show", false, "Show maintenance mode status")
}

// Execute implements the subcommand interface for maintenance.
func (m *MaintenanceMode) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	lp := args[0].(log.Parameters)
	log.SetupOneTimeLogging(lp, m.Name())
	return m.maintenanceModeHandler(f, maintenance.ModeReader{}, maintenance.ModeWriter{})
}

func (m *MaintenanceMode) maintenanceModeHandler(fs *flag.FlagSet, fr maintenance.FileReader, fw maintenance.FileWriter) subcommands.ExitStatus {
	if m.show {
		res, err := maintenance.ReadMaintenanceMode(fr)
		if err != nil {
			log.Print(fmt.Sprintf("Error getting maintenance mode status: %v.", err))
			return subcommands.ExitFailure
		}
		if len(res) == 0 {
			log.Print("No SID is under maintenance.")
			return subcommands.ExitSuccess
		}
		log.Print("Maintenance mode flag for process metrics is set to true for the following SIDs:\n")
		for _, v := range res {
			log.Print(v + "\n")
		}
		return subcommands.ExitSuccess
	}

	if m.sid == "" {
		log.Print("Invalid SID provided.\n" + m.Usage())
		return subcommands.ExitUsageError
	}

	_, err := maintenance.UpdateMaintenanceMode(m.enable, m.sid, fr, fw)
	if err != nil {
		log.Print(fmt.Sprintf("Error updating the maintenance mode: %v.", err))
		return subcommands.ExitFailure
	}
	log.Print(fmt.Sprintf("Updated maintenance mode for the SID: %s", m.sid))
	return subcommands.ExitSuccess
}
