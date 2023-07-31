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

package migratehanamonitoring

import (
	"context"
	_ "embed"
	"os"
	"strings"
	"testing"

	"flag"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	hmmpb "github.com/GoogleCloudPlatform/sapagent/protos/hanamonitoringmigration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	//go:embed testdata/configurationHM.yaml
	oldAgentConfig string
	//go:embed testdata/configurationHMSSL.yaml
	oldAgentConfigWithSSL string
	//go:embed testdata/configurationHMMalformed.yaml
	oldAgentConfigMalformed string
)

func migrationConfig(ssl bool) *hmmpb.HANAMonitoringConfiguration {
	conf := &hmmpb.HANAMonitoringConfiguration{
		Agent: &hmmpb.AgentConfig{
			SampleInterval:   300,
			QueryTimeout:     300,
			ExecutionThreads: 10,
			HanaInstances: []*hmmpb.HANAInstance{
				&hmmpb.HANAInstance{
					Name:                  "sample_instance1",
					Host:                  "127.0.0.1",
					Port:                  30015,
					Connections:           10,
					User:                  "SYSTEM",
					Password:              "PASSWORD",
					EnableSsl:             ssl,
					ValidateCertificate:   true,
					HostNameInCertificate: "HOST_NAME_IN_CERTIFICATE",
				},
			},
		},
		Queries: []*hmmpb.Query{
			&hmmpb.Query{
				Name:    "default_host_queries",
				Enabled: true,
			},
			&hmmpb.Query{
				Name:        "custom_memory_utilization",
				Enabled:     false,
				Description: "Custom Total memory utilization by services\n",
				Sql:         "SELECT\n       SUM(TOTAL_MEMORY_USED_SIZE) AS \"mem_used\",\n       SUM(PHYSICAL_MEMORY_SIZE) AS \"resident_mem_used\"\nFROM M_SERVICE_MEMORY;\n",
				Columns: []*hmmpb.Column{
					&hmmpb.Column{
						Name:        "mem_used",
						MetricType:  hmmpb.MetricType_GAUGE,
						ValueType:   hmmpb.ValueType_INT64,
						Description: "Amount of memory from the memory pool.\n",
						Units:       "By",
					},
					&hmmpb.Column{
						Name:        "resident_mem_used",
						MetricType:  hmmpb.MetricType_GAUGE,
						ValueType:   hmmpb.ValueType_INT64,
						Description: "Amount of memory used in total by all the services.\n",
						Units:       "By",
					},
				},
			},
		},
	}
	return conf
}

func agentForSAPConf() *cpb.Configuration {
	conf := &cpb.Configuration{
		ProvideSapHostAgentMetrics: true,
		CloudProperties: &iipb.CloudProperties{
			ProjectId:  "config-project-id",
			InstanceId: "config-instance-id",
			Zone:       "config-zone",
		},
	}
	return conf
}

func TestExecuteMigrateHANAMonitoring(t *testing.T) {
	tests := []struct {
		name    string
		migrate MigrateHANAMonitoring
		want    subcommands.ExitStatus
		args    []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
		{
			name:    "SuccessfullyParseArgs",
			migrate: MigrateHANAMonitoring{},
			want:    subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			migrate: MigrateHANAMonitoring{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForHelp",
			migrate: MigrateHANAMonitoring{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.migrate.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.migrate, test.args, got, test.want)
			}
		})
	}
}

func TestMigrationHandler(t *testing.T) {
	tests := []struct {
		name           string
		migrate        MigrateHANAMonitoring
		fs             *flag.FlagSet
		readFile       configuration.ReadConfigFile
		writeFile      configuration.WriteConfigFile
		wantExitStatus subcommands.ExitStatus
	}{
		{
			name:    "OldHMConfigReadFailure",
			migrate: MigrateHANAMonitoring{},
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfig), os.ErrNotExist
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			writeFile:      func(path string, data []byte, mode os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name:    "OldHMConfigParseFailure",
			migrate: MigrateHANAMonitoring{},
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(""), nil
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			writeFile:      func(path string, data []byte, mode os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name:    "AgentForSAPConfigReadFails",
			migrate: MigrateHANAMonitoring{},
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfig), nil
				}
				return nil, os.ErrNotExist
			},
			writeFile:      func(path string, data []byte, mode os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name:    "AgentForSAPConfigParseFails",
			migrate: MigrateHANAMonitoring{},
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfig), nil
				}
				return []byte("["), nil
			},
			writeFile:      func(path string, data []byte, mode os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name: "SSLModeOnInHANAMonitoringAgent",
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfigWithSSL), nil
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			writeFile:      func(p string, data []byte, perm os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "InvalidQueries",
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfigMalformed), nil
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			wantExitStatus: subcommands.ExitUsageError,
		},
		{
			name: "ConfigUpdateFails",
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfig), nil
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			writeFile:      func(p string, data []byte, perm os.FileMode) error { return os.ErrPermission },
			wantExitStatus: subcommands.ExitFailure,
		},
		{
			name:    "MigrationSuccessful",
			migrate: MigrateHANAMonitoring{},
			readFile: func(p string) ([]byte, error) {
				if strings.Contains(p, "yaml") {
					return []byte(oldAgentConfig), nil
				}
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			writeFile:      func(path string, data []byte, mode os.FileMode) error { return nil },
			wantExitStatus: subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.migrate.migrationHandler(test.fs, test.readFile, test.writeFile)
			if got != test.wantExitStatus {
				t.Errorf("migrationHandler(%v) = %v, want = %v", test.fs, got, test.wantExitStatus)
			}
		})
	}
}

func TestParseOldConf(t *testing.T) {
	tests := []struct {
		name string
		read configuration.ReadConfigFile
		want *hmmpb.HANAMonitoringConfiguration
	}{
		{
			name: "ReadFails",
			read: func(p string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
		},
		{
			name: "ReadSucceeds",
			read: func(p string) ([]byte, error) {
				return []byte(oldAgentConfig), nil
			},
			want: migrationConfig(false),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseOldConf(test.read)
			if cmp.Diff(got, test.want, protocmp.Transform()) != "" {
				t.Errorf("parseOldConf(%v) = %v, want = %v", test.read, got, test.want)
			}
		})
	}
}

func TestParseAgentConf(t *testing.T) {
	tests := []struct {
		name string
		read configuration.ReadConfigFile
		want *cpb.Configuration
	}{
		{
			name: "ReadFails",
			read: func(p string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
		},
		{
			name: "ReadSucceeds",
			read: func(p string) ([]byte, error) {
				return []byte(`{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone"}}`), nil
			},
			want: agentForSAPConf(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseAgentConf(test.read); cmp.Diff(got, test.want, protocmp.Transform()) != "" {
				t.Errorf("parseAgentConf(%v) = %v, want = %v", test.read, got, test.want)
			}
		})
	}
}

func TestPrepareConfig(t *testing.T) {
	hmmConf := migrationConfig(false)

	want := &cpb.HANAMonitoringConfiguration{
		SampleIntervalSec: 300,
		QueryTimeoutSec:   300,
		ExecutionThreads:  10,
		Enabled:           true,
		HanaInstances: []*cpb.HANAInstance{
			&cpb.HANAInstance{
				Name:      "sample_instance1",
				Host:      "127.0.0.1",
				Port:      "30015",
				User:      "SYSTEM",
				Password:  "PASSWORD",
				EnableSsl: false,
			},
		},
		Queries: []*cpb.Query{
			&cpb.Query{
				Name:    "default_host_queries",
				Enabled: true,
			},
			&cpb.Query{
				Name:    "custom_memory_utilization",
				Enabled: false,
				Sql:     "SELECT SUM(TOTAL_MEMORY_USED_SIZE) AS mem_used, SUM(PHYSICAL_MEMORY_SIZE) AS resident_mem_used FROM M_SERVICE_MEMORY;",
				Columns: []*cpb.Column{
					&cpb.Column{
						Name:       "mem_used",
						MetricType: cpb.MetricType_METRIC_GAUGE,
						ValueType:  cpb.ValueType_VALUE_INT64,
					},
					&cpb.Column{
						Name:       "resident_mem_used",
						MetricType: cpb.MetricType_METRIC_GAUGE,
						ValueType:  cpb.ValueType_VALUE_INT64,
					},
				},
			},
		},
	}

	got := prepareConfig(hmmConf, false)

	if diff := cmp.Diff(got, want, protocmp.Transform()); diff != "" {
		t.Errorf("prepareConfig(%v) = %v, want = %v", hmmConf, got, want)
	}
}

func TestCreateColumns(t *testing.T) {
	oldColumns := []*hmmpb.Column{
		&hmmpb.Column{
			ValueType:  hmmpb.ValueType_BOOL,
			MetricType: hmmpb.MetricType_GAUGE,
		},
		&hmmpb.Column{
			ValueType:  hmmpb.ValueType_INT64,
			MetricType: hmmpb.MetricType_GAUGE,
		},
		&hmmpb.Column{
			ValueType:  hmmpb.ValueType_STRING,
			MetricType: hmmpb.MetricType_LABEL,
		},
		&hmmpb.Column{
			ValueType:  hmmpb.ValueType_DOUBLE,
			MetricType: hmmpb.MetricType_CUMULATIVE,
		},
		&hmmpb.Column{
			ValueType:  hmmpb.ValueType_VALUE_UNSPECIFIED,
			MetricType: hmmpb.MetricType_METRIC_UNSPECIFIED,
		},
	}
	want := []*cpb.Column{
		&cpb.Column{
			MetricType: cpb.MetricType_METRIC_GAUGE,
			ValueType:  cpb.ValueType_VALUE_BOOL,
		},
		&cpb.Column{
			MetricType: cpb.MetricType_METRIC_GAUGE,
			ValueType:  cpb.ValueType_VALUE_INT64,
		},
		&cpb.Column{
			MetricType: cpb.MetricType_METRIC_LABEL,
			ValueType:  cpb.ValueType_VALUE_STRING,
		},
		&cpb.Column{
			MetricType: cpb.MetricType_METRIC_CUMULATIVE,
			ValueType:  cpb.ValueType_VALUE_DOUBLE,
		},
		&cpb.Column{
			MetricType: cpb.MetricType_METRIC_UNSPECIFIED,
			ValueType:  cpb.ValueType_VALUE_UNSPECIFIED,
		},
	}

	got := createColumns(oldColumns)

	if diff := cmp.Diff(got, want, protocmp.Transform()); diff != "" {
		t.Errorf("createColumns(%v) = %v, want = %v", oldColumns, got, want)
	}
}
