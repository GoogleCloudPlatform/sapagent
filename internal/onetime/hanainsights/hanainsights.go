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

// Package hanainsights implements the one time execution mode for HANA
// insights.
package hanainsights

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"

	"flag"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/databaseconnector"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/preprocessor"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
)

// HANAInsights has args for hanainsights subcommands.
type HANAInsights struct {
	project, host, port, sid       string
	user, password, passwordSecret string
	gceService                     onetime.GCEInterface
	status                         bool
	db                             *sql.DB
}

// Name implements the subcommand interface for hanainsights.
func (*HANAInsights) Name() string { return "hanainsights" }

// Synopsis implements the subcommand interface for hanainsights.
func (*HANAInsights) Synopsis() string { return "invoke HANA local insights workflow" }

// Usage implements the subcommand interface for hanainsights.
func (*HANAInsights) Usage() string {
	return `hanainsights -project=<project-name> -host=<hostname> -port=<port-number> -sid=<HANA-SID> -user=<user-name>
	[-password=<passwd> | -password-secret=<secret-name>]`
}

// SetFlags implements the subcommand interface for hanainsights.
func (h *HANAInsights) SetFlags(fs *flag.FlagSet) {
	fs.StringVar(&h.project, "project", "", "GCP project. (required)")
	fs.StringVar(&h.host, "host", "", "HANA host. (required)")
	fs.StringVar(&h.port, "port", "", "HANA port. (required)")
	fs.StringVar(&h.sid, "sid", "", "HANA SID. (required)")
	fs.StringVar(&h.user, "user", "", "HANA username. (required)")
	fs.StringVar(&h.password, "password", "", "HANA password. (discouraged - use password-secret instead)")
	fs.StringVar(&h.passwordSecret, "password-secret", "", "Secret Manager secret name that holds HANA Password.")
}

// Execute implements the subcommand interface for hanainsights.
func (h *HANAInsights) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 3, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	onetime.SetupOneTimeLogging(lp, h.Name())

	return h.hanaInsightsHandler(ctx, gce.NewGCEClient)
}

func (h *HANAInsights) validateParameters(os string) error {
	switch {
	case os == "windows":
		return fmt.Errorf("hanainsights is only supported on Linux systems")
	case h.host == "" || h.port == "" || h.sid == "" || h.user == "":
		return fmt.Errorf("required arguments not passed. Usage:" + h.Usage())
	case h.password == "" && h.passwordSecret == "":
		return fmt.Errorf("either -password or -password-secret is required. Usage:" + h.Usage())
	}

	log.Logger.Info("Parameter validation successful.")
	return nil
}

func (h *HANAInsights) hanaInsightsHandler(ctx context.Context, gceServiceCreator onetime.GCEServiceFunc) subcommands.ExitStatus {
	var err error
	if err = h.validateParameters(runtime.GOOS); err != nil {
		log.Print(err.Error())
		return subcommands.ExitFailure
	}

	h.gceService, err = gceServiceCreator(ctx)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to create GCE service", err)
		return subcommands.ExitFailure
	}

	dbp := databaseconnector.Params{
		Username:       h.user,
		Password:       h.password,
		PasswordSecret: h.passwordSecret,
		Host:           h.host,
		Port:           h.port,
		GCEService:     h.gceService,
		Project:        h.project,
	}
	if h.db, err = databaseconnector.Connect(ctx, dbp); err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failed to connect to database", err)
		return subcommands.ExitFailure
	}

	rules, err := preprocessor.ReadRules(preprocessor.RuleFilenames)
	if err != nil {
		log.Logger.Errorw("Failure to read HANA rules", "error", err)
		return subcommands.ExitFailure
	}

	insights, err := ruleengine.Run(ctx, h.db, rules)
	if err != nil {
		onetime.LogErrorToFileAndConsole("ERROR: Failure in rule engine", err)
		return subcommands.ExitFailure
	}
	log.Logger.Infow("Generating HANA insights", insights)
	// TODO: Generate local HANA insights in markdown and json format.
	return subcommands.ExitSuccess
}
