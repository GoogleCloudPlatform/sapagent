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

package features

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		f    *Feature
		want subcommands.ExitStatus
		args []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
			},
		},
		{
			name: "FailParseAndValidateConfig",
			f:    &Feature{},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			f: &Feature{
				version: true,
			},
			args: []any{
				"test",
				log.Parameters{},
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "SuccessForHelp",
			f: &Feature{
				help: true,
			},
			args: []any{
				"test",
				log.Parameters{},
			},
			want: subcommands.ExitSuccess,
		},
		{
			name: "FailureForModifyConfig",
			f:    &Feature{feature: "host_metrics"},
			want: subcommands.ExitFailure,
			args: []any{
				"test",
				log.Parameters{},
			},
		},
		{
			name: "SuccessForModifyConfig",
			f: &Feature{
				feature: "host_metrics",
				enable:  true,
				path:    path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus {
					return subcommands.ExitSuccess
				},
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
			ctx := context.Background()
			if test.f != nil && test.f.path != "" {
				writeFile(ctx, &cpb.Configuration{
					ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
					LogLevel:                   1,
					CollectionConfiguration: &cpb.CollectionConfiguration{
						CollectAgentMetrics:   true,
						CollectProcessMetrics: true,
					},
					LogToCloud: &wpb.BoolValue{Value: true},
				}, test.f.path)
			}
			got := test.f.Execute(ctx, &flag.FlagSet{Usage: func() { return }}, test.args...)
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
			name: "CollectionConfigAbsent",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics:  &wpb.BoolValue{Value: true},
				LogLevel:                    2,
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			want: map[string]bool{
				"hana_monitoring":     false,
				"workload_validation": false,
				"process_metrics":     false,
				"host_metrics":        true,
				"agent_metrics":       false,
				"sap_discovery":       false,
			},
		},
		{
			name: "HMCAbsent",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			want: map[string]bool{
				"sap_discovery":       true,
				"agent_metrics":       true,
				"host_metrics":        true,
				"process_metrics":     false,
				"workload_validation": true,
				"hana_monitoring":     false,
			},
		},
		{
			name: "HostMetricsAbsent",
			config: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"host_metrics":        false,
				"sap_discovery":       true,
				"agent_metrics":       true,
				"process_metrics":     false,
				"workload_validation": true,
				"hana_monitoring":     true,
			},
		},
		{
			name: "SAPDiscoveryAbsent",
			config: &cpb.Configuration{
				LogLevel: 2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			want: map[string]bool{
				"agent_metrics":       true,
				"host_metrics":        false,
				"process_metrics":     false,
				"workload_validation": true,
				"hana_monitoring":     true,
				"sap_discovery":       false,
			},
		},
		{
			name: "SuccessAllFieldsPresent",
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			want: map[string]bool{
				"host_metrics":        true,
				"sap_discovery":       true,
				"agent_metrics":       true,
				"hana_monitoring":     false,
				"process_metrics":     false,
				"workload_validation": true,
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
				"host_metrics":        false,
				"process_metrics":     false,
				"workload_validation": false,
				"hana_monitoring":     false,
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
		f         *Feature
		oldConfig *cpb.Configuration
		newConfig *cpb.Configuration
		readFunc  configuration.ReadConfigFile
		want      subcommands.ExitStatus
	}{
		{
			name: "EmptyConfigFile",
			f: &Feature{
				path: path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "AbsenceOfEnable|Disable",
			f: &Feature{
				feature: "host_metrics",
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{},
			newConfig: &cpb.Configuration{},
			readFunc:  os.ReadFile,
			want:      subcommands.ExitUsageError,
		},
		{
			name: "ErrorOpeningInputFile1",
			f: &Feature{
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
			name: "Unknown Metric",
			f: &Feature{
				feature: "TCP_Metrics",
				path:    path.Join(t.TempDir(), "/configuration.json"),
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitUsageError,
		},
		{
			name: "EnableHostMetrics",
			f: &Feature{
				enable:       true,
				feature:      "host_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: false},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled: false,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
				LogToCloud:                  &wpb.BoolValue{Value: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableHostMetrics",
			f: &Feature{
				disable:      true,
				feature:      "host_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: false},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud: &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					Enabled: false,
				},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableProcessMetrics",
			f: &Feature{
				enable:       true,
				feature:      "process_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:    &wpb.BoolValue{Value: true},
					CollectProcessMetrics: true,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableProcessMetrics",
			f: &Feature{
				disable:      true,
				feature:      "process_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            true,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableWorkloadValidation",
			f: &Feature{
				enable:       true,
				feature:      "workload_validation",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectWorkloadValidationMetrics: true,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableWorkloadValidation",
			f: &Feature{
				disable:      true,
				feature:      "workload_validation",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: true,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableSapDiscovery",
			f: &Feature{
				enable:       true,
				feature:      "sap_discovery",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableSapDiscovery",
			f: &Feature{
				disable:      true,
				feature:      "sap_discovery",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectProcessMetrics:            false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: false},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableAgentMetrics",
			f: &Feature{
				enable:       true,
				feature:      "agent_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:  &wpb.BoolValue{Value: true},
					CollectAgentMetrics: true,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableAgentMetrics",
			f: &Feature{
				disable:      true,
				feature:      "agent_metrics",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: false},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "EnableHANAMonitoring",
			f: &Feature{
				enable:       true,
				feature:      "hana_monitoring",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
		{
			name: "DisableHANAMonitoring",
			f: &Feature{
				disable:      true,
				feature:      "hana_monitoring",
				path:         path.Join(t.TempDir(), "/configuration.json"),
				restartAgent: func(ctx context.Context) subcommands.ExitStatus { return subcommands.ExitSuccess },
			},
			oldConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              false,
					CollectWorkloadValidationMetrics: false,
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{Enabled: true},
			},
			newConfig: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: &wpb.BoolValue{Value: true},
				},
				LogToCloud:                  &wpb.BoolValue{Value: true},
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{},
			},
			readFunc: os.ReadFile,
			want:     subcommands.ExitSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			writeFile(ctx, test.oldConfig, test.f.path)
			got := test.f.modifyConfig(ctx, test.readFunc)
			if got != test.want {
				t.Errorf("modifyConfig(%v) returned unexpected ExitStatus.\ngot: %v\nwant %v", test.f.path, got, test.want)
				return
			}

			gotConfig := configuration.Read(test.f.path, os.ReadFile)
			if diff := cmp.Diff(test.newConfig, gotConfig, protocmp.Transform()); diff != "" {
				t.Errorf("modifyConfig(%v) returned an unexpected diff (-want +got): %v", test.f.path, diff)
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
			name:     "ErrorWritingOutput",
			wantErr:  cmpopts.AnyError,
			wantFile: "{}",
		},
		{
			name: "validFile",
			path: path.Join(t.TempDir(), "/configuration.json"),
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:               &wpb.BoolValue{Value: true},
					CollectAgentMetrics:              true,
					CollectWorkloadValidationMetrics: true,
				},
				LogToCloud: &wpb.BoolValue{Value: true},
			},
			wantFile: `{
 "provideSapHostAgentMetrics": true,
 "logLevel": "INFO",
 "collectionConfiguration": {
  "collectWorkloadValidationMetrics": true,
  "sapSystemDiscovery": true,
  "collectAgentMetrics": true
 },
 "logToCloud": true
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

			file, err := protojson.Marshal(configuration.Read(tc.path, os.ReadFile))
			if err != nil {
				t.Errorf("protojson.Marhsal in writeFile(%v, %v) returned unexpected error: %v", tc.config, tc.path, err)
				return
			}

			var gotFile bytes.Buffer
			json.Indent(&gotFile, file, "", " ")
			if diff := cmp.Diff(tc.wantFile, gotFile.String()); diff != "" {
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
					CollectWorkloadValidationMetrics: true,
				},
			},
			want: &cpb.CollectionConfiguration{
				CollectWorkloadValidationMetrics: true,
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
	f := &Feature{}
	fs := flag.NewFlagSet("flags", flag.ExitOnError)
	f.SetFlags(fs)

	flags := []string{"feature", "f", "version", "v", "help", "h", "loglevel", "enable", "disable", "showall"}
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
		f      *Feature
		want   subcommands.ExitStatus
	}{
		{
			name: "EmptyPath",
			f:    &Feature{},
			want: subcommands.ExitFailure,
		},
		{
			name: "SampleShow",
			f: &Feature{
				path: path.Join(t.TempDir(), "/configuration.json"),
			},
			config: &cpb.Configuration{
				ProvideSapHostAgentMetrics: &wpb.BoolValue{Value: true},
				LogLevel:                   2,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: true,
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
			if tc.f != nil && len(tc.f.path) > 0 {
				writeFile(context.Background(), tc.config, tc.f.path)
			}
			got := tc.f.showFeatures(ctx)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("showFeatures(%v) returned an unexpected diff (-want +got): %v", tc.f.path, diff)
			}
		})
	}
}

func TestShowStatus(t *testing.T) {
	tests := []struct {
		name    string
		feature string
		enabled bool
		want    string
	}{
		{
			name:    "SampleEnable",
			feature: "host_metrics",
			enabled: true,
			want:    "host_metrics [0;32m[ENABLED][0m\n",
		},
		{
			name:    "SampleDisable",
			feature: "agent_metrics",
			enabled: false,
			want:    "agent_metrics [0;31m[DISABLED][0m\n",
		},
	}

	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := showStatus(ctx, tc.feature, tc.enabled)
			if err != nil {
				t.Fatalf("showStatus(%v, %v) returned an unexpected error: %v", tc.feature, tc.enabled, err)
			}

			if got != tc.want {
				t.Errorf("showStatus(%v, %v) = %v, want: %v", tc.feature, tc.enabled, got, tc.want)
			}
		})
	}
}
