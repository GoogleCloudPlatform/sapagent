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

package configure

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var defaultCloudProperties = &ipb.CloudProperties{
	ProjectId:    "default-project",
	InstanceName: "default-instance",
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		c    *Configure
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			c:    &Configure{},
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			c:    &Configure{},
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name: "FailParseAndValidateConfig",
			c:    &Configure{},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForAgentVersion",
			c: &Configure{
				version: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			c: &Configure{
				help: true,
			},
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "FailureForModifyConfig",
			c:    &Configure{feature: "host_metrics"},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
		{
			name: "SuccessForModifyConfig",
			c: &Configure{
				feature: "host_metrics",
				enable:  true,
				path:    path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus {
					return subcommands.ExitSuccess
				},
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				defaultCloudProperties,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.c != nil && test.c.path != "" {
				writeFile(ctx, &cpb.Configuration{
					ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
					LogLevel:                   1,
					CollectionConfiguration: &cpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						CollectProcessMetrics: true,
					},
					LogToCloud: &wpb.BoolValue{Value: true},
				}, test.c.path)
			}
			got := test.c.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Execute() returned an unexpected diff (-want +got): %v", diff)
			}
		})
	}
}

func TestSetStatus(t *testing.T) {
	tests := []struct {
		name   string
		config *cpb.Configuration
		want   map[string]bool
	}{
		{
			name: "CollectionConfigAbsentHMEnabled",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				LogToCloud:                 &wpb.BoolValue{Value: true},
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"hana_monitoring":     true,
				"workload_evaluation": true,
				"process_metrics":     false,
				"host_metrics":        true,
				"agent_metrics":       false,
				"sap_discovery":       true,
				"reliability_metrics": false,
			},
		},
		{
			name: "HMCAbsent",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            true,
					CollectReliabilityMetrics:        &wpb.BoolValue{Value: true},
				},
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			want: map[string]bool{
				"sap_discovery":       true,
				"agent_metrics":       true,
				"host_metrics":        true,
				"process_metrics":     true,
				"workload_evaluation": true,
				"reliability_metrics": true,
				"hana_monitoring":     false,
			},
		},
		{
			name: "HostMetricsAbsent",
			config: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            true,
					CollectReliabilityMetrics:        &wpb.BoolValue{Value: true},
				},
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"host_metrics":        true,
				"sap_discovery":       true,
				"agent_metrics":       true,
				"process_metrics":     true,
				"workload_evaluation": true,
				"hana_monitoring":     true,
				"reliability_metrics": true,
			},
		},
		{
			name: "SAPDiscoveryAbsent",
			config: &cpb.Configuration{
				LogLevel:                   2,
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            true,
					CollectReliabilityMetrics:        &wpb.BoolValue{Value: false},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"agent_metrics":       true,
				"host_metrics":        true,
				"process_metrics":     true,
				"workload_evaluation": true,
				"hana_monitoring":     true,
				"sap_discovery":       false,
				"reliability_metrics": false,
			},
		},
		{
			name: "SuccessAllFieldsPresent",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            true,
					CollectReliabilityMetrics:        &wpb.BoolValue{Value: true},
				},
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"host_metrics":        true,
				"sap_discovery":       true,
				"agent_metrics":       true,
				"hana_monitoring":     true,
				"process_metrics":     true,
				"workload_evaluation": true,
				"reliability_metrics": true,
			},
		},
		{
			name: "SuccessNotAllFieldsPresent",
			config: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			want: map[string]bool{
				"sap_discovery":       false,
				"agent_metrics":       false,
				"host_metrics":        true,
				"process_metrics":     false,
				"workload_evaluation": true,
				"hana_monitoring":     false,
				"reliability_metrics": false,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := setStatus(context.Background(), test.config)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("setStatus() returned an unexpected diff (-want +got): %v", diff)
			}
		})
	}
}

func TestModifyConfig(t *testing.T) {
	tests := []struct {
		name      string
		c         *Configure
		oldConfig *cpb.Configuration
		newConfig *cpb.Configuration
		readFunc  configuration.ReadConfigFile
		want      subcommands.ExitStatus
	}{
		{
			name: "EmptyConfigFile",
			c: &Configure{
				path: path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfEnable|Disable1",
			c: &Configure{
				feature: "host_metrics",
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfEnable|Disable2",
			c: &Configure{
				feature: "hana_monitoring",
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfEnable|Disable3",
			c: &Configure{
				setting:      "log_to_cloud",
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfFeature|Setting",
			c: &Configure{
				enable:       true,
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    3,
				LogToCloud:                  &wpb.BoolValue{Value: false},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfEnable|Disable2",
			c: &Configure{
				setting:      "log_to_cloud",
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfFeature|Setting",
			c: &Configure{
				enable:       true,
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "ErrorOpeningInputFile1",
			c: &Configure{
				feature: "host_metrics",
				enable:  true,
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc: func(s string) ([]byte, error) {
				return nil, cmpopts.AnyError
			},
			want: subcommands.ExitFailure,
		},
		{
			name: "InvalidLogConfig",
			c: &Configure{
				logLevel:     "warning",
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "ValidLogConfig",
			c: &Configure{
				logLevel:     "warn",
				path:         t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   cpb.Configuration_INFO,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   cpb.Configuration_WARNING,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "InvalidSetting",
			c: &Configure{
				setting: "logToCloud",
				enable:  true,
				path:    t.TempDir() + "/configuration.json",
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: false},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "ValidSetting",
			c: &Configure{
				setting: "log_to_cloud",
				disable: true,
				path:    t.TempDir() + "/configuration.json",
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus {
					return subcommands.ExitSuccess
				},
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "Unknown Metric",
			c: &Configure{
				feature: "TCP_Metrics",
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "EnableHostMetrics",
			c: &Configure{
				enable:       true,
				feature:      "host_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: false},
				LogLevel:                   2,
				LogToCloud:                 &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				LogToCloud:                 &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableHostMetrics",
			c: &Configure{
				disable:      true,
				feature:      "host_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				LogToCloud:                 &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: false},
				LogLevel:                   2,
				LogToCloud:                 &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableProcessMetrics",
			c: &Configure{
				enable:       true,
				feature:      "process_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 3,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 3,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableProcessMetrics",
			c: &Configure{
				disable:      true,
				feature:      "process_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "ValidSetFreqProcessMetrics",
			c: &Configure{
				feature:              "process_metrics",
				fastMetricsFrequency: 30,
				slowMetricsFrequency: 50,
				path:                 path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:         func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       true,
					ProcessMetricsFrequency:     30,
					SlowProcessMetricsFrequency: 50,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "InvalidSetFreqProcessMetrics1",
			c: &Configure{
				feature:              "process_metrics",
				fastMetricsFrequency: -30,
				slowMetricsFrequency: 50,
				path:                 path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:         func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:   true,
					ProcessMetricsFrequency: 30,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:   true,
					ProcessMetricsFrequency: 30,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "InvalidSetFreqProcessMetrics3",
			c: &Configure{
				feature:              "process_metrics",
				fastMetricsFrequency: 30,
				slowMetricsFrequency: -50,
				path:                 path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:         func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       true,
					SlowProcessMetricsFrequency: 10,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics:       true,
					SlowProcessMetricsFrequency: 10,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "AddSkipMetrics",
			c: &Configure{
				feature:      "process_metrics",
				skipMetrics:  "/sap/networkstats/rtt, /sap/networkstats/rcv_rtt, /sap/hana/utilization",
				add:          true,
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
					ProcessMetricsToSkip: []string{
						"/sap/networkstats/rtt",
						"/sap/hana/memory",
					},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
					ProcessMetricsToSkip: []string{
						"/sap/networkstats/rtt",
						"/sap/hana/memory",
						"/sap/networkstats/rcv_rtt",
						"/sap/hana/utilization",
					},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "RemoveSkipMetrics",
			c: &Configure{
				feature:      "process_metrics",
				skipMetrics:  "/sap/networkstats/rtt",
				remove:       true,
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
					ProcessMetricsToSkip: []string{
						"/sap/networkstats/rtt",
						"/sap/networkstats/rcv_rtt",
					},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectProcessMetrics: false,
					ProcessMetricsToSkip:  []string{"/sap/networkstats/rcv_rtt"},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableWorkloadValidation",
			c: &Configure{
				enable:       true,
				feature:      "workload_evaluation",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableWorkloadValidation",
			c: &Configure{
				disable:      true,
				feature:      "workload_evaluation",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: false},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "ValidSetFreqWorkloadValidation",
			c: &Configure{
				feature:                    "workload_evaluation",
				validationMetricsFrequency: 30,
				dbFrequency:                50,
				path:                       path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:               func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:     &wpb.BoolValue{Value: true},
					WorkloadValidationMetricsFrequency:   10,
					WorkloadValidationDbMetricsFrequency: 20,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:     &wpb.BoolValue{Value: true},
					WorkloadValidationMetricsFrequency:   30,
					WorkloadValidationDbMetricsFrequency: 50,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "InvalidSetFreqWorkloadValidation1",
			c: &Configure{
				feature:                    "workload_evaluation",
				validationMetricsFrequency: -30,
				dbFrequency:                50,
				path:                       path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:               func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:   &wpb.BoolValue{Value: true},
					WorkloadValidationMetricsFrequency: 10,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:   &wpb.BoolValue{Value: true},
					WorkloadValidationMetricsFrequency: 10,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "InvalidSetFreqWorkloadValidation2",
			c: &Configure{
				feature:                    "workload_evaluation",
				validationMetricsFrequency: 30,
				dbFrequency:                -50,
				path:                       path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:               func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:     &wpb.BoolValue{Value: true},
					WorkloadValidationDbMetricsFrequency: 50,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:     &wpb.BoolValue{Value: true},
					WorkloadValidationDbMetricsFrequency: 50,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "EnableSapDiscovery",
			c: &Configure{
				enable:       true,
				feature:      "sap_discovery",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: false},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableSapDiscovery",
			c: &Configure{
				disable:      true,
				feature:      "sap_discovery",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: false},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableAgentMetrics",
			c: &Configure{
				enable:       true,
				feature:      "agent_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableAgentMetrics",
			c: &Configure{
				disable:      true,
				feature:      "agent_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "ValidSetFreqAgentMetrics",
			c: &Configure{
				feature:               "agent_metrics",
				agentMetricsFrequency: 25,
				agentHealthFrequency:  20,
				heartbeatFrequency:    10,
				path:                  path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:          func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:   true,
					AgentMetricsFrequency: 15,
					AgentHealthFrequency:  4,
					HeartbeatFrequency:    2,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:   true,
					AgentMetricsFrequency: 25,
					AgentHealthFrequency:  20,
					HeartbeatFrequency:    10,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "InvalidSetFreqAgentMetrics1",
			c: &Configure{
				feature:               "agent_metrics",
				agentMetricsFrequency: -25,
				agentHealthFrequency:  20,
				heartbeatFrequency:    10,
				path:                  path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:          func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:   true,
					AgentMetricsFrequency: 15,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:   true,
					AgentMetricsFrequency: 15,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "InvalidSetFreqAgentMetrics2",
			c: &Configure{
				feature:               "agent_metrics",
				agentMetricsFrequency: 25,
				agentHealthFrequency:  -20,
				heartbeatFrequency:    10,
				path:                  path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:          func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:  true,
					AgentHealthFrequency: 4,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:  true,
					AgentHealthFrequency: 4,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "InvalidSetFreqAgentMetrics3",
			c: &Configure{
				feature:               "agent_metrics",
				agentMetricsFrequency: 25,
				agentHealthFrequency:  20,
				heartbeatFrequency:    -10,
				path:                  path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:          func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: true,
					HeartbeatFrequency:  2,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics: true,
					HeartbeatFrequency:  2,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "EnableHANAMonitoring",
			c: &Configure{
				enable:       true,
				feature:      "hana_monitoring",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableHANAMonitoring",
			c: &Configure{
				disable:      true,
				feature:      "hana_monitoring",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "ValidSetHANAMonitoring",
			c: &Configure{
				feature:           "hana_monitoring",
				sampleIntervalSec: 2,
				queryTimeoutSec:   10,
				path:              path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:      func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled:           true,
					SampleIntervalSec: 2,
					QueryTimeoutSec:   10,
				},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "InvalidSetHANAMonitoring1",
			c: &Configure{
				feature:           "hana_monitoring",
				sampleIntervalSec: -3,
				path:              path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:      func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled:           true,
					SampleIntervalSec: 2,
				},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled:           true,
					SampleIntervalSec: 2,
				},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "InvalidSetHANAMonitoring2",
			c: &Configure{
				feature:         "hana_monitoring",
				queryTimeoutSec: -10,
				path:            path.Join(t.TempDir(), "/configuration.json"),
				RestartAgent:    func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled:         true,
					QueryTimeoutSec: 10,
				},
			},
			newConfig: &cpb.Configuration{
				LogLevel:   2,
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled:         true,
					QueryTimeoutSec: 10,
				},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			writeFile(ctx, test.oldConfig, test.c.path)
			got := test.c.modifyConfig(ctx, nil, test.readFunc)
			if got != test.want {
				t.Errorf("modifyConfig(%v) returned unexpected ExitStatus.\ngot: %v\nwant %v", test.c.path, got, test.want)
				return
			}

			gotConfig := configuration.Read(test.c.path, os.ReadFile)
			if diff := cmp.Diff(test.newConfig, gotConfig, protocmp.Transform(),
				cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("modifyConfig(%v) returned an unexpected diff (-want +got): %v", test.c.path, diff)
			}
		})
	}
}

func TestWriteFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		config   *cpb.Configuration
		wantFile string
		wantErr  error
	}{
		{
			name:    "ErrorWritingOutput",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "validFile",
			path: path.Join(t.TempDir(), "/configuration.json"),
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
				},
				DiscoveryConfiguration: &cpb.DiscoveryConfiguration{
					EnableDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			wantFile: `{
  "provide_sap_host_agent_metrics": true,
  "log_level": "INFO",
  "collection_configuration": {
    "collect_workload_validation_metrics": true,
    "collect_agent_metrics": true
  },
  "log_to_cloud": true,
  "discovery_configuration": {
    "enable_discovery": true
  }
}`,
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := writeFile(context.Background(), tc.config, tc.path)
			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("writeFile(%v, %v) returned an unexpected diff (-want +got): %v", tc.config, tc.path, diff)
			}
			if gotErr != nil {
				return
			}

			file, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("os.ReadFile(%v) returned unexpected error: %v", tc.path, err)
			}

			if diff := cmp.Diff(tc.wantFile, string(file)); diff != "" {
				t.Errorf("writeFile(%v, %v) returned an unexpected diff (-want +got): %v", tc.config, tc.path, diff)
			}
		})
	}
}

func TestCheckCollectionConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *cpb.Configuration
		want   *cpb.CollectionConfiguration
	}{
		{
			name:   "emptyCollectionConfig",
			config: &cpb.Configuration{},
			want:   &cpb.CollectionConfiguration{},
		},
		{
			name: "validCollectionConfig",
			config: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
				},
			},
			want: &cpb.CollectionConfiguration{
				CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkCollectionConfig(tc.config)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("checkCollectionConfig(%v) returned an unexpected diff (-want +got): %v", tc.config, diff)
			}
		})
	}
}

func TestSetFlags(t *testing.T) {
	c := &Configure{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	c.SetFlags(fs)

	flags := []string{
		"feature", "f", "version", "v", "help", "h", "loglevel", "setting",
		"enable", "disable", "showall", "add", "remove", "process_metrics_frequency", "workload_evaluation_db_metrics_frequency",
		"sample_interval_sec", "query_timeout_sec", "process_metrics_to_skip", "slow_process_metrics_frequency",
		"heartbeat_frequency", "agent_health_frequency", "agent_metrics_frequency",
		"workload_evaluation_metrics_frequency", "reliability_metrics_frequency",
	}
	for _, flag := range flags {
		got := fs.Lookup(flag)
		if got == nil {
			t.Errorf("SetFlags(%#v) flag not found: %s", fs, flag)
		}
	}
}

func TestShowFeatures(t *testing.T) {
	tests := []struct {
		name   string
		config *cpb.Configuration
		c      *Configure
		want   subcommands.ExitStatus
	}{
		{
			name: "EmptyPath",
			c:    &Configure{},
			want: subcommands.ExitFailure,
		},
		{
			name: "SampleShow",
			c: &Configure{
				path: path.Join(t.TempDir(), "/configuration.json"),
			},
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectProcessMetrics:            false,
				},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: subcommands.ExitSuccess,
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.c != nil && len(tc.c.path) > 0 {
				writeFile(context.Background(), tc.config, tc.c.path)
			}
			got := tc.c.showFeatures(ctx)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("showFeatures(%v) returned an unexpected diff (-want +got): %v", tc.c.path, diff)
			}
		})
	}
}

func TestShowStatus(t *testing.T) {
	tests := []struct {
		name      string
		feature   string
		enabled   bool
		isWindows bool
		want      string
	}{
		{
			name:      "SampleEnable",
			feature:   "host_metrics",
			enabled:   true,
			isWindows: false,
			want:      "host_metrics [0;32m[ENABLED][0m\n",
		},
		{
			name:      "SampleDisable",
			feature:   "agent_metrics",
			enabled:   false,
			isWindows: false,
			want:      "agent_metrics [0;31m[DISABLED][0m\n",
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := showStatus(ctx, tc.feature, tc.enabled, tc.isWindows)
			if err != nil {
				t.Fatalf("showStatus(%v, %v) returned an unexpected error: %v", tc.feature, tc.enabled, err)
			}

			if got != tc.want {
				t.Errorf("showStatus(%v, %v), got: %v, want: %v", tc.feature, tc.enabled, got, tc.want)
			}
		})
	}
}
