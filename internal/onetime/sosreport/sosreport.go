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

// Package sosreport implements one time execution mode for
// sosreport.
package sosreport

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
)

// SOSReport has args for SOS report collection one time mode.
type SOSReport struct {
	sid                    string
	instanceNums           string
	instanceNumsAfterSplit []string
	hostname               string
}

const (
	destFilePathPrefix  = `/tmp/google-cloud-sap-agent/sos-report-`
	linuxConfigFilePath = `/etc/google-cloud-sap-agent/configuration.json`
	linuxLogFilesPath   = `/var/log/`
	systemDBErrorsFile  = `_SYSTEM_DB_BACKUP_ERROR.txt`
	tenantDBErrorsFile  = `_TENANT_DB_BACKUP_ERROR.txt`
	backintErrorsFile   = `_BACKINT_ERROR.txt`
	globalINIFile       = `/custom/config/global.ini`
	backintGCSPath      = `/opt/backint/backint-gcs`
)

// Name implements the subcommand interface for collecting SOS report collection for support team.
func (*SOSReport) Name() string {
	return "sosreport"
}

// Synopsis implements the subcommand interface for SOS report collection for support team.
func (*SOSReport) Synopsis() string {
	return "collect sos report of Agent for SAP for the support team"
}

// Usage implements the subcommand interface for SOS report collection for support team.
func (*SOSReport) Usage() string {
	return `sosreport [-sid=<SAP System Identifier> -instance-numbers=<Instance numbers> -hostname=<Hostname>]
	Example: sosreport -sid="DEH" -instance-numbers="00 01 11" -hostname="sample_host"
	`
}

// SetFlags implements the subcommand interface for SOS report collection.
func (s *SOSReport) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.sid, "sid", "", "SAP System Identifier")
	fs.StringVar(&s.instanceNums, "instance-numbers", "", "Instance numbers")
	fs.StringVar(&s.hostname, "hostname", "", "Hostname")
}

// Execute implements the subcommand interface for SOS report collection.
func (s *SOSReport) Execute(ctx context.Context, fs *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	log.SetupOneTimeLogging(lp, s.Name())
	return s.sosReportCollectionHandler(ctx)
}

func (s *SOSReport) sosReportCollectionHandler(ctx context.Context) subcommands.ExitStatus {
	if errs := s.validateParams(); len(errs) > 0 {
		errMessage := strings.Join(errs, ", ")
		log.Logger.Errorf("Invalid params for collecting SOS Report for Agent for SAP", "error", errMessage)
		usagemetrics.Error(usagemetrics.SOSReportCollectionUsageError)
		return subcommands.ExitUsageError
	}
	destFilesPath := destFilePathPrefix + s.hostname + "-" + strings.Replace(time.Now().Format(time.RFC3339), ":", "-", -1)
	if err := os.MkdirAll(destFilesPath, 0777); err != nil {
		log.Logger.Error(err)
		usagemetrics.Error(usagemetrics.SOSReportCollectionExitFailure)
		return subcommands.ExitFailure
	}

	hanaPaths := []string{}
	for _, inr := range s.instanceNumsAfterSplit {
		hanaPaths = append(hanaPaths, fmt.Sprintf(`/usr/sap/%s/HDB%s/%s`, s.sid, inr, s.hostname))
	}
	log.Logger.Infof("Required HANA Paths", hanaPaths)
	return subcommands.ExitSuccess
}

func (s *SOSReport) validateParams() []string {
	var errs []string
	if s.sid == "" {
		errs = append(errs, "no value provided for sid")
	}
	if s.instanceNums == "" {
		errs = append(errs, "no value provided for instance-numbers")
	} else {
		s.instanceNumsAfterSplit = strings.Split(s.instanceNums, " ")
		for _, nos := range s.instanceNumsAfterSplit {
			if len(nos) != 2 {
				errs = append(errs, fmt.Sprintf("invalid instance number %s", nos))
			}
		}
	}
	if s.hostname == "" {
		errs = append(errs, "no value provided for hostname")
	}
	return errs
}
