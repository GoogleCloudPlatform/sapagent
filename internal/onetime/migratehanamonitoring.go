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
	"os"
	"regexp"
	"strconv"
	"strings"

	"flag"

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/yamlpb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	hmmpb "github.com/GoogleCloudPlatform/sapagent/protos/hanamonitoringmigration"
)

const (
	oldConfigPath string = "/usr/sap/google-saphanamonitoring-agent/conf/configuration.yaml"
)

// MigrateHANAMonitoring is a struct which implements subcommands interface.
type MigrateHANAMonitoring struct{}

// Name implements the subcommand interface for migrating HANA Monitoring Agent.
func (*MigrateHANAMonitoring) Name() string { return "migratehma" }

// Synopsis implements the subcommand interface for migrating HANA Monitoring Agent.
func (*MigrateHANAMonitoring) Synopsis() string {
	return "Migrates HANA Monitoring Agent 2.0 to Agent for SAP."
}

// Usage implements the subcommand interface for migrating hana monitoring agent.
func (*MigrateHANAMonitoring) Usage() string {
	return "migratehma"
}

// SetFlags implements the subcommand interface for migrating hana monitoring agent.
func (*MigrateHANAMonitoring) SetFlags(f *flag.FlagSet) {}

// Execute implements the subcommand interface for Migrating HANA Monitoring Agent.
func (m *MigrateHANAMonitoring) Execute(ctx context.Context, f *flag.FlagSet, args ...any) subcommands.ExitStatus {
	if len(args) < 2 {
		log.Logger.Errorf("Not enough args for Execute(). Want: 2, Got: %d", len(args))
		return subcommands.ExitUsageError
	}
	lp, ok := args[1].(log.Parameters)
	if !ok {
		log.Logger.Errorf("Unable to assert args[1] of type %T to log.Parameters.", args[1])
		return subcommands.ExitUsageError
	}
	log.SetupOneTimeLogging(lp, m.Name())
	return m.migrationHandler(f, os.ReadFile, os.WriteFile)
}

// migrateHandler is responsible for reading the old configuration.yaml file, converting the old proto to
// new HANAMonitoringConfiguration proto and updating the Agent for SAP Configuration file with new config.
// It returns the following return code based on outcomes
// Successful Migration - subcommands.ExitSuccess
// Parsing / Reading / Writing Errors - subcommands.ExitFailure
// SSL Mode on / Malformed Queries - subcommands.ExitUsageError
func (m *MigrateHANAMonitoring) migrationHandler(f *flag.FlagSet, read configuration.ReadConfigFile, write configuration.WriteConfigFile) subcommands.ExitStatus {
	hmMigrationConf := parseOldConf(read)
	if hmMigrationConf == nil {
		return subcommands.ExitFailure
	}
	config := parseAgentConf(read)
	if config == nil {
		return subcommands.ExitFailure
	}
	sslEnabled := sslMode(hmMigrationConf)
	hmConfig := prepareConfig(hmMigrationConf, sslEnabled)
	config.HanaMonitoringConfiguration = hmConfig
	if !configuration.ValidateQueries(config.GetHanaMonitoringConfiguration().GetQueries()) {
		logMessageToFileAndConsole("Queries formed using Old HANA Monitoring Agent Config are not valid. File" + oldConfigPath)
		return subcommands.ExitUsageError
	}
	content, err := protojson.MarshalOptions{Multiline: true}.Marshal(config)
	if err != nil {
		return subcommands.ExitFailure
	}
	err = write(configuration.LinuxConfigPath, content, 0777)
	if err != nil {
		logErrorToFileAndConsole("Could not write Agent for SAP Configuration file"+configuration.LinuxConfigPath, err)
		return subcommands.ExitFailure
	}
	if sslEnabled {
		usagemetrics.Action(usagemetrics.SSLModeOnHANAMonitoring)
		msg := `HANA Monitoring Agent had ssl mode on, automatic upgrade could not be completed.
		Refer to https://cloud.google.com/solutions/sap/docs/agent-for-sap/latest/operations#upgrading_ssl-enabled_instances 
		for more details.
		Solution: Add "host_name_in_certificate" and "tls_root_ca_file" for each HANA instance in
		the config file ` + configuration.LinuxConfigPath + `and restart the agent
		to start HANA monitoring functionality in the Agent for SAP.
		`
		logMessageToFileAndConsole(msg)
		return subcommands.ExitUsageError
	}
	logMessageToFileAndConsole("Migrated HANA Monitoring Agent Config successfully")
	return subcommands.ExitSuccess
}

func prepareConfig(hmMigrationConf *hmmpb.HANAMonitoringConfiguration, ssl bool) *cpb.HANAMonitoringConfiguration {
	hmConfig := &cpb.HANAMonitoringConfiguration{}
	hmConfig.ExecutionThreads = hmMigrationConf.GetAgent().GetExecutionThreads()
	// if HANA Monitoring Agent had ssl enabled, we cannot enable HANA monitoring in Agent for SAP till
	// customers update their certificates and then the config.
	hmConfig.Enabled = !ssl
	hmConfig.QueryTimeoutSec = hmMigrationConf.GetAgent().GetQueryTimeout()
	hmConfig.SampleIntervalSec = hmMigrationConf.GetAgent().GetSampleInterval()
	hmConfig.HanaInstances = createInstances(hmMigrationConf)
	hmConfig.Queries = createQueries(hmMigrationConf)
	return hmConfig
}

func parseOldConf(read configuration.ReadConfigFile) *hmmpb.HANAMonitoringConfiguration {
	content, err := read(oldConfigPath)
	if err != nil {
		logErrorToFileAndConsole("Could not read old config file"+oldConfigPath, err)
		return nil
	}
	hmConfigOld := &hmmpb.HANAMonitoringConfiguration{}
	err = yamlpb.UnmarshalString(string(content), hmConfigOld)
	if err != nil {
		logErrorToFileAndConsole("Could not parse to HANA Monitoring migration proto from old config, file"+oldConfigPath, err)
		return nil
	}
	return hmConfigOld
}

func parseAgentConf(read configuration.ReadConfigFile) *cpb.Configuration {
	configContent, err := read(configuration.LinuxConfigPath)
	if err != nil {
		logErrorToFileAndConsole("Could not read Agent for SAP Configuration file"+configuration.LinuxConfigPath, err)
		return nil
	}
	config := &cpb.Configuration{}
	err = protojson.Unmarshal(configContent, config)
	if err != nil {
		logErrorToFileAndConsole("Could not parse Agent for SAP Configuration, file"+configuration.LinuxConfigPath, err)
		return nil
	}
	return config
}

func sslMode(c *hmmpb.HANAMonitoringConfiguration) bool {
	for _, instance := range c.GetAgent().GetHanaInstances() {
		if instance.GetEnableSsl() {
			return true
		}
	}
	return false
}

func createInstances(hmm *hmmpb.HANAMonitoringConfiguration) []*cpb.HANAInstance {
	res := []*cpb.HANAInstance{}
	for _, instance := range hmm.GetAgent().GetHanaInstances() {
		inst := &cpb.HANAInstance{}
		inst.Name = instance.GetName()
		inst.Host = instance.GetHost()
		inst.Port = strconv.FormatInt(instance.GetPort(), 10)
		inst.User = instance.GetUser()
		inst.Password = instance.GetPassword()
		inst.SecretName = instance.GetSecretName()
		inst.EnableSsl = instance.GetEnableSsl()
		res = append(res, inst)
	}
	return res
}

func createQueries(hmm *hmmpb.HANAMonitoringConfiguration) []*cpb.Query {
	res := []*cpb.Query{}
	for _, query := range hmm.GetQueries() {
		qry := &cpb.Query{}
		qry.Name = query.GetName()
		qry.Sql = format(query.GetSql())
		qry.Enabled = query.GetEnabled()
		qry.SampleIntervalSec = int64(query.GetSampleInterval())
		qry.Columns = createColumns(query.GetColumns())
		res = append(res, qry)
	}
	return res
}

func createColumns(oldQueryCoulumns []*hmmpb.Column) []*cpb.Column {
	res := []*cpb.Column{}
	for _, col := range oldQueryCoulumns {
		c := &cpb.Column{}
		c.Name = col.GetName()
		c.NameOverride = col.GetNameOverride()
		switch col.GetValueType() {
		case hmmpb.ValueType_BOOL:
			c.ValueType = cpb.ValueType_VALUE_BOOL
		case hmmpb.ValueType_INT64:
			c.ValueType = cpb.ValueType_VALUE_INT64
		case hmmpb.ValueType_DOUBLE:
			c.ValueType = cpb.ValueType_VALUE_DOUBLE
		case hmmpb.ValueType_STRING:
			c.ValueType = cpb.ValueType_VALUE_STRING
		default:
			c.ValueType = cpb.ValueType_VALUE_UNSPECIFIED
		}

		switch col.GetMetricType() {
		case hmmpb.MetricType_LABEL:
			c.MetricType = cpb.MetricType_METRIC_LABEL
			c.ValueType = cpb.ValueType_VALUE_STRING
		case hmmpb.MetricType_GAUGE:
			c.MetricType = cpb.MetricType_METRIC_GAUGE
		case hmmpb.MetricType_CUMULATIVE:
			c.MetricType = cpb.MetricType_METRIC_CUMULATIVE
		default:
			c.MetricType = cpb.MetricType_METRIC_UNSPECIFIED
		}
		res = append(res, c)
	}
	return res
}

// format removes all multiple spaces, newlines from yaml sql string and removes escaped double quotes in
// sql query
func format(s string) string {
	allSpaces := regexp.MustCompile(`\s+`)
	doubleQuotes := regexp.MustCompile(`\"`)
	res := allSpaces.ReplaceAllString(s, " ")
	res = strings.TrimSpace(res)

	res = doubleQuotes.ReplaceAllString(res, "")
	return res
}

func logMessageToFileAndConsole(msg string) {
	log.Print(msg)
	log.Logger.Info(msg)
}
