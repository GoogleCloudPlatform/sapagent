/*
Copyright 2022 Google LLC

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

package configuration

import (
	_ "embed"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

var (
	testAgentProps = &cpb.AgentProperties{Name: AgentName, Version: AgentVersion}
	testCloudProps = &iipb.CloudProperties{
		ProjectId:        "test-project",
		NumericProjectId: "123456789",
		InstanceId:       "test-instance",
		Zone:             "test-zone",
		InstanceName:     "test-instance-name",
		Image:            "test-image",
	}
	testHANAInstance = &cpb.HANAInstance{Name: "sample_instance1",
		Host:                "127.0.0.1",
		Port:                "30015",
		User:                "SYSTEM",
		Password:            "PASSWORD",
		EnableSsl:           false,
		ValidateCertificate: true,
	}

	//go:embed testdata/defaultHANAMonitoringQueries.json
	sampleHANAMonitoringConfigQueriesJSON []byte
	//go:embed testdata/customHANAMonitoringConfig.json
	testCustomHANAMonitoringConfigQueriesJSON []byte
)

func TestReadFromFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		readFunc ReadConfigFile
		want     *cpb.Configuration
	}{
		{
			name: "FileReadError",
			readFunc: func(p string) ([]byte, error) {
				return nil, cmpopts.AnyError
			},
		},
		{
			name: "EmptyConfigFile",
			readFunc: func(p string) ([]byte, error) {
				return nil, nil
			},
		},
		{
			name: "ConfigFileWithContents",
			readFunc: func(p string) ([]byte, error) {
				fileContent := `{"provide_sap_host_agent_metrics": false, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone" } }`
				return []byte(fileContent), nil
			},
			want: &cpb.Configuration{
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
			},
		},
		{
			name: "MalformedFConfigurationJsonFile",
			readFunc: func(p string) ([]byte, error) {
				fileContent := `{"provide_sap_host_agent_metrics": true, "cloud_properties": {"project_id": "config-project-id", "instance_id": "config-instance-id", "zone": "config-zone", } }`
				return []byte(fileContent), nil
			},
			want: &cpb.Configuration{ProvideSapHostAgentMetrics: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ReadFromFile(test.path, test.readFunc)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Test: %s ReadFromFile() for path: %s\n(-want +got):\n%s",
					test.name, test.path, diff)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name           string
		configFromFile *cpb.Configuration
		want           *cpb.Configuration
	}{
		{
			name:           "EmptyConfigFile",
			configFromFile: nil,
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: true,
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
			},
		},
		{
			name: "ConfigFileWithOverride",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: false,
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: false,
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
			},
		},
		{
			name: "ConfigWithDefaultOvetride",
			configFromFile: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            true,
				},
			},
			want: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:   true,
					WorkloadValidationMetricsFrequency: 300,
					CollectProcessMetrics:              true,
					ProcessMetricsFrequency:            5,
				},
				AgentProperties: testAgentProps,
				CloudProperties: testCloudProps,
			},
		},
		{
			name: "ConfigWithCloudProperties",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: false,
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: false,
				AgentProperties:            testAgentProps,
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
			},
		},
		{
			name: "ConfigWithoutHANAMonitoringDefaults",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: true,
				CloudProperties:            testCloudProps,
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					HanaInstances: []*cpb.HANAInstance{
						testHANAInstance,
					},
					Queries: []*cpb.Query{
						&cpb.Query{
							Name: "host_query",
							Sql:  "sample sql",
						},
						&cpb.Query{
							Name: "service_query",
							Sql:  "sample_sql",
						},
					},
				},
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: true,
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					SampleIntervalSec: 300,
					QueryTimeoutSec:   300,
					ExecutionThreads:  10,
					HanaInstances: []*cpb.HANAInstance{
						testHANAInstance,
					},
					Queries: []*cpb.Query{
						&cpb.Query{
							Name: "host_query",
							Sql:  "sample sql",
						},
						&cpb.Query{
							Name: "service_query",
							Sql:  "sample_sql",
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ApplyDefaults(test.configFromFile, testCloudProps)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Test: %s ApplyDefaults() (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestReadHANAMonitoringConfiguration(t *testing.T) {
	tests := []struct {
		name                 string
		path                 string
		testDefaultHMContent []byte
		readFunc             ReadConfigFile
		want                 *cpb.HANAMonitoringConfiguration
	}{
		{
			name:                 "MalformedDefaultQueriesConfig",
			path:                 "/sample/path",
			testDefaultHMContent: []byte("{"),
			readFunc: func(p string) ([]byte, error) {
				return nil, cmpopts.AnyError
			},
			want: nil,
		},
		{
			name:                 "UnableToReadCustomConfig",
			path:                 "/sample/path",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			readFunc: func(p string) ([]byte, error) {
				return nil, cmpopts.AnyError
			},
			want: nil,
		},
		{
			name:                 "EmptyCustomConfig",
			path:                 "/sample/path",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			readFunc: func(p string) ([]byte, error) {
				return nil, nil
			},
			want: nil,
		},
		{
			name:                 "MalformedCustomConfig",
			path:                 "/sample/path",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			readFunc: func(p string) ([]byte, error) {
				return []byte("{"), nil
			},
			want: nil,
		},
		{
			name:                 "ReadHANAMonitoringConfigSuccessfully",
			path:                 "/sample/path",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			readFunc: func(p string) ([]byte, error) {
				return testCustomHANAMonitoringConfigQueriesJSON, nil
			},
			want: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                "127.0.0.1",
						Port:                "30015",
						User:                "SYSTEM",
						Password:            "PASSWORD",
						EnableSsl:           false,
						ValidateCertificate: true,
					},
				},
				Queries: []*cpb.Query{
					&cpb.Query{
						Name:    "host_queries",
						Sql:     "sample sql",
						Enabled: true,
						Columns: []*cpb.Column{
							&cpb.Column{Name: "host", MetricType: cpb.MetricType_METRIC_LABEL, ValueType: cpb.ValueType_VALUE_STRING},
						},
					},
					&cpb.Query{Name: "custom_memory_utilization",
						Description: "Custom Total memory utilization by services\n",
						Enabled:     true,
						Sql:         "sample sql",
						Columns: []*cpb.Column{
							&cpb.Column{Name: "mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64, Units: "By"},
							&cpb.Column{Name: "resident_mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64, Units: "By"},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaultHMQueriesContent = test.testDefaultHMContent
			got := readConfig(test.path, test.readFunc)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Test: %s readHANAMonitoringConfiguration() (-want +got):\n%s", test.name, diff)
			}
		})
	}
}

func TestApplyOverrides(t *testing.T) {
	tests := []struct {
		name             string
		defaultQueryList []*cpb.Query
		customQueryList  []*cpb.Query
		wantCount        int
	}{
		{
			name: "OnlyDefaultQueryEnabled",
			defaultQueryList: []*cpb.Query{
				&cpb.Query{
					Name: "host_query",
					Sql:  "sample sql",
				},
				&cpb.Query{
					Name: "service_query",
					Sql:  "sample_sql",
				},
			},
			customQueryList: []*cpb.Query{
				&cpb.Query{
					Name:    "default_service_query",
					Sql:     "sample_sql",
					Enabled: true,
				},
				&cpb.Query{
					Name:    "default_host_query",
					Sql:     "sample sql",
					Enabled: true,
				},
				&cpb.Query{
					Name:    "custom_query",
					Enabled: false,
				},
			},
			wantCount: 2,
		},
		{
			name: "OnlyCustomQueryEnabled",
			defaultQueryList: []*cpb.Query{
				&cpb.Query{
					Name: "host_query",
					Sql:  "sample sql",
				},
				&cpb.Query{
					Name: "service_query",
					Sql:  "sample_sql",
				},
			},
			customQueryList: []*cpb.Query{
				&cpb.Query{
					Name:    "default_service_query",
					Sql:     "sample_sql",
					Enabled: false,
				},
				&cpb.Query{
					Name:    "default_host_query",
					Sql:     "sample sql",
					Enabled: false,
				},
				&cpb.Query{
					Name:    "custom_query",
					Enabled: true,
				},
			},
			wantCount: 1,
		},
		{
			name: "OnlyDefaultQueryEnabled",
			defaultQueryList: []*cpb.Query{
				&cpb.Query{
					Name: "host_query",
					Sql:  "sample sql",
				},
				&cpb.Query{
					Name: "service_query",
					Sql:  "sample_sql",
				},
			},
			customQueryList: []*cpb.Query{
				&cpb.Query{
					Name:    "default_service_query",
					Sql:     "sample_sql",
					Enabled: true,
				},
				&cpb.Query{
					Name:    "default_host_query",
					Sql:     "sample sql",
					Enabled: false,
				},
				&cpb.Query{
					Name:    "custom_query",
					Enabled: true,
				},
			},
			wantCount: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := applyOverrides(test.defaultQueryList, test.customQueryList)
			if len(got) != test.wantCount {
				t.Errorf("Test: %s applyOverrides() (-want +got): \n%s", test.name, cmp.Diff(test.wantCount, len(got)))
			}
		})
	}
}
