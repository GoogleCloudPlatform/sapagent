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

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmpopts/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"go.uber.org/zapcore/zapcore"

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
		Host:      "127.0.0.1",
		Port:      "30015",
		User:      "SYSTEM",
		Password:  "PASSWORD#",
		EnableSsl: false,
	}

	//go:embed testdata/defaultHANAMonitoringQueries.json
	sampleHANAMonitoringConfigQueriesJSON []byte
	//go:embed testdata/sampleConfig.json
	testConfigWithHANAMonitoringConfigJSON []byte
	//go:embed testdata/systemConfig.json
	testConfigWithSapSystemConfigJSON []byte
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
				ProvideSapHostAgentMetrics: wpb.Bool(false),
			},
		},
		{
			name: "ConfigWithHANAMonitoring",
			readFunc: func(p string) ([]byte, error) {
				return testConfigWithHANAMonitoringConfigJSON, nil
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				HanaMonitoringConfiguration: &cpb.HANAMonitoringConfiguration{
					SampleIntervalSec: 300,
					QueryTimeoutSec:   300,
					HanaInstances: []*cpb.HANAInstance{
						&cpb.HANAInstance{Name: "sample_instance1",
							Host:      "127.0.0.1",
							Port:      "30015",
							User:      "SYSTEM",
							Password:  "PASSWORD",
							EnableSsl: false,
						},
					},
					Queries: []*cpb.Query{
						&cpb.Query{
							Name:    "host_queries",
							Enabled: true,
							Sql:     "sample sql",
							Columns: []*cpb.Column{
								&cpb.Column{
									Name:       "host",
									MetricType: cpb.MetricType_METRIC_LABEL,
									ValueType:  cpb.ValueType_VALUE_STRING,
								},
							},
						},
						&cpb.Query{Name: "custom_memory_utilization",
							Enabled: true,
							Sql:     "sample sql",
							Columns: []*cpb.Column{
								&cpb.Column{Name: "mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
								&cpb.Column{Name: "resident_mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
							},
						},
					},
				},
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
			want: &cpb.Configuration{ProvideSapHostAgentMetrics: wpb.Bool(true)},
		},
		{
			name: "ConfigWithSapSystem",
			readFunc: func(p string) ([]byte, error) {
				return testConfigWithSapSystemConfigJSON, nil
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery: wpb.Bool(false),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaultHMQueriesContent = sampleHANAMonitoringConfigQueriesJSON
			got := ReadFromFile(test.path, test.readFunc)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ReadFromFile() for path: %s\n(-want +got):\n%s", test.path, diff)
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
				ProvideSapHostAgentMetrics: wpb.Bool(true),
				LogToCloud:                 wpb.Bool(true),
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:    wpb.Bool(true),
					DataWarehouseEndpoint: "https://workloadmanager-datawarehouse.googleapis.com/",
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						DisableFetchLatestConfig: true,
						ConfigTargetEnvironment:  cpb.TargetEnvironment_PRODUCTION,
					},
				},
			},
		},
		{
			name: "ConfigFileWithOverride",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				LogToCloud:                 wpb.Bool(false),
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				LogToCloud:                 wpb.Bool(false),
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:    wpb.Bool(true),
					DataWarehouseEndpoint: "https://workloadmanager-datawarehouse.googleapis.com/",
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						DisableFetchLatestConfig: true,
						ConfigTargetEnvironment:  cpb.TargetEnvironment_PRODUCTION,
					},
				},
			},
		},
		{
			name: "ConfigWithDefaultOverride",
			configFromFile: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics: true,
					CollectProcessMetrics:            true,
					CollectAgentMetrics:              true,
					SapSystemDiscovery:               wpb.Bool(false),
					DataWarehouseEndpoint:            "https://other-workloadmanager-datawarehouse.googleapis.com/",
				},
			},
			want: &cpb.Configuration{
				CollectionConfiguration: &cpb.CollectionConfiguration{
					CollectWorkloadValidationMetrics:     true,
					WorkloadValidationMetricsFrequency:   300,
					WorkloadValidationDbMetricsFrequency: 3600,
					CollectProcessMetrics:                true,
					ProcessMetricsFrequency:              5,
					SlowProcessMetricsFrequency:          30,
					CollectAgentMetrics:                  true,
					AgentMetricsFrequency:                60,
					AgentHealthFrequency:                 60,
					HeartbeatFrequency:                   60,
					MissedHeartbeatThreshold:             10,
					SapSystemDiscovery:                   wpb.Bool(false),
					DataWarehouseEndpoint:                "https://other-workloadmanager-datawarehouse.googleapis.com/",
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						DisableFetchLatestConfig: true,
						ConfigTargetEnvironment:  cpb.TargetEnvironment_PRODUCTION,
					},
				},
				ProvideSapHostAgentMetrics: wpb.Bool(true),
				LogToCloud:                 wpb.Bool(true),
				AgentProperties:            testAgentProps,
				CloudProperties:            testCloudProps,
			},
		},
		{
			name: "ConfigWithCloudProperties",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				LogToCloud:                 wpb.Bool(false),
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
			},
			want: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(false),
				LogToCloud:                 wpb.Bool(false),
				AgentProperties:            testAgentProps,
				CloudProperties: &iipb.CloudProperties{
					ProjectId:  "config-project-id",
					InstanceId: "config-instance-id",
					Zone:       "config-zone",
				},
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:    wpb.Bool(true),
					DataWarehouseEndpoint: "https://workloadmanager-datawarehouse.googleapis.com/",
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						DisableFetchLatestConfig: true,
						ConfigTargetEnvironment:  cpb.TargetEnvironment_PRODUCTION,
					},
				},
			},
		},
		{
			name: "ConfigWithoutHANAMonitoringDefaults",
			configFromFile: &cpb.Configuration{
				ProvideSapHostAgentMetrics: wpb.Bool(true),
				LogToCloud:                 wpb.Bool(true),
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
				ProvideSapHostAgentMetrics: wpb.Bool(true),
				LogToCloud:                 wpb.Bool(true),
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
				CollectionConfiguration: &cpb.CollectionConfiguration{
					SapSystemDiscovery:    wpb.Bool(true),
					DataWarehouseEndpoint: "https://workloadmanager-datawarehouse.googleapis.com/",
					WorkloadValidationCollectionDefinition: &cpb.WorkloadValidationCollectionDefinition{
						DisableFetchLatestConfig: true,
						ConfigTargetEnvironment:  cpb.TargetEnvironment_PRODUCTION,
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ApplyDefaults(test.configFromFile, testCloudProps)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("ApplyDefaults() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPrepareHMConfig(t *testing.T) {
	tests := []struct {
		name                 string
		testDefaultHMContent []byte
		config               *cpb.HANAMonitoringConfiguration
		want                 *cpb.HANAMonitoringConfiguration
	}{
		{
			name:                 "MalformedDefaultQueriesConfig",
			testDefaultHMContent: []byte("{"),
		},
		{
			name:                 "NilHANAMonitoringConfiguration",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
		},
		{
			name:                 "ReadHANAMonitoringConfigSuccessfully",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			config: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:      "127.0.0.1",
						Port:      "30015",
						User:      "SYSTEM",
						Password:  "PASSWORD#",
						EnableSsl: false,
					},
				},
				Queries: []*cpb.Query{
					&cpb.Query{
						Name:    "default_host_queries",
						Enabled: true,
					},
					&cpb.Query{
						Name:    "default_cpu_queries",
						Enabled: false,
					},
					&cpb.Query{Name: "custom_memory_utilization",
						Enabled: true,
						Sql:     "sample sql",
						Columns: []*cpb.Column{
							&cpb.Column{Name: "mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
							&cpb.Column{Name: "resident_mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
						},
					},
				},
			},
			want: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:      "127.0.0.1",
						Port:      "30015",
						User:      "SYSTEM",
						Password:  "PASSWORD#",
						EnableSsl: false,
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
						Enabled: true,
						Sql:     "sample sql",
						Columns: []*cpb.Column{
							&cpb.Column{Name: "mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
							&cpb.Column{Name: "resident_mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
						},
					},
				},
			},
		},
		{
			name:                 "InvalidMetricAndValueTypeCombinationInCustomQueries",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			config: &cpb.HANAMonitoringConfiguration{
				Queries: []*cpb.Query{
					&cpb.Query{Name: "host_queries", Sql: "sample sql", Enabled: true, Columns: []*cpb.Column{
						&cpb.Column{Name: "host", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_STRING},
					}},
				},
			},
		},
		{
			name:                 "InvalidSSLConfigHANAInstance",
			testDefaultHMContent: sampleHANAMonitoringConfigQueriesJSON,
			config: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:      "127.0.0.1",
						Port:      "30015",
						User:      "SYSTEM",
						Password:  "PASSWORD#",
						EnableSsl: true,
					},
				},
				Queries: []*cpb.Query{
					&cpb.Query{
						Name:    "default_host_queries",
						Enabled: true,
					},
					&cpb.Query{
						Name:    "default_cpu_queries",
						Enabled: false,
					},
					&cpb.Query{Name: "custom_memory_utilization",
						Enabled: true,
						Sql:     "sample sql",
						Columns: []*cpb.Column{
							&cpb.Column{Name: "mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
							&cpb.Column{Name: "resident_mem_used", MetricType: cpb.MetricType_METRIC_GAUGE, ValueType: cpb.ValueType_VALUE_INT64},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defaultHMQueriesContent = test.testDefaultHMContent
			got := prepareHMConf(test.config)
			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("readHANAMonitoringConfiguration() (-want +got):\n%s", diff)
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
				t.Errorf("applyOverrides() (-want +got): \n%s", cmp.Diff(test.wantCount, len(got)))
			}
		})
	}
}

func TestValidateCustomQueries(t *testing.T) {
	tests := []struct {
		name    string
		queries []*cpb.Query
		want    bool
	}{
		{
			name: "ValidCustomQuery",
			queries: []*cpb.Query{
				&cpb.Query{
					Name: "testQuery",
					Columns: []*cpb.Column{
						&cpb.Column{
							Name:       "testCol1",
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
						&cpb.Column{
							Name:       "testCol2",
							MetricType: cpb.MetricType_METRIC_GAUGE,
							ValueType:  cpb.ValueType_VALUE_INT64,
						},
						&cpb.Column{
							Name:       "testCol3",
							MetricType: cpb.MetricType_METRIC_CUMULATIVE,
							ValueType:  cpb.ValueType_VALUE_INT64,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "DuplicateQueryNames",
			queries: []*cpb.Query{
				{
					Name: "testQuery",
					Columns: []*cpb.Column{
						{
							Name:       "testCol1",
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
					},
				},
				{
					Name: "testQuery",
					Columns: []*cpb.Column{
						{
							Name:       "testCol1",
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "DuplicateColumnNames",
			queries: []*cpb.Query{
				{
					Name: "testQuery",
					Columns: []*cpb.Column{
						{
							Name:       "testCol1",
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
						{
							Name:       "testCol1",
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MetricTypeGaugeAndValueTypeString",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_GAUGE,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MetricTypeCumulativeAndValueTypeString",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_CUMULATIVE,
							ValueType:  cpb.ValueType_VALUE_STRING,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MetricTypeCumulativeAndValueTypeBool",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_CUMULATIVE,
							ValueType:  cpb.ValueType_VALUE_BOOL,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MetricTypeCumulativeAndValueTypeINT64",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_CUMULATIVE,
							ValueType:  cpb.ValueType_VALUE_INT64,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "MissingColumnTypeInQuery",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_CUMULATIVE,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MissingMetricTypeInQuery",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							ValueType: cpb.ValueType_VALUE_STRING,
						},
					},
				},
			},
			want: false,
		},
		{
			name: "MetricTypeLabelValueTypeInt",
			queries: []*cpb.Query{
				&cpb.Query{
					Columns: []*cpb.Column{
						&cpb.Column{
							MetricType: cpb.MetricType_METRIC_LABEL,
							ValueType:  cpb.ValueType_VALUE_INT64,
						},
					},
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ValidateQueries(test.queries)
			if got != test.want {
				t.Errorf("checkInvalidCustomMetrics() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidateHANASSLConfig(t *testing.T) {
	tests := []struct {
		name string
		conf *cpb.HANAMonitoringConfiguration
		want bool
	}{
		{
			name: "ValidHANASSLConfig",
			conf: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                  "127.0.0.1",
						Port:                  "30015",
						User:                  "SYSTEM",
						Password:              "PASSWORD#",
						EnableSsl:             true,
						HostNameInCertificate: "hostname",
						TlsRootCaFile:         "/path/",
					},
				},
			},
			want: true,
		},
		{
			name: "ValidHANASSLConfigSSLOff",
			conf: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                  "127.0.0.1",
						Port:                  "30015",
						User:                  "SYSTEM",
						Password:              "PASSWORD#",
						EnableSsl:             false,
						HostNameInCertificate: "hostname",
						TlsRootCaFile:         "/path/",
					},
				},
			},
			want: true,
		},
		{
			name: "InvalidHANASSLConfigEmptyHostNameInCertificate",
			conf: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                  "127.0.0.1",
						Port:                  "30015",
						User:                  "SYSTEM",
						Password:              "PASSWORD#",
						EnableSsl:             true,
						HostNameInCertificate: "",
						TlsRootCaFile:         "/path/",
					},
				},
			},
			want: false,
		},
		{
			name: "InvalidHANASSLConfigNoTLSRootPath",
			conf: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                  "127.0.0.1",
						Port:                  "30015",
						User:                  "SYSTEM",
						Password:              "PASSWORD#",
						EnableSsl:             true,
						HostNameInCertificate: "",
						TlsRootCaFile:         "",
					},
				},
			},
			want: false,
		},
		{
			name: "InvalidHANASSLConfigNoTLSRootPathNoHostNameInCertificate",
			conf: &cpb.HANAMonitoringConfiguration{
				SampleIntervalSec: 300,
				QueryTimeoutSec:   300,
				HanaInstances: []*cpb.HANAInstance{
					&cpb.HANAInstance{Name: "sample_instance1",
						Host:                  "127.0.0.1",
						Port:                  "30015",
						User:                  "SYSTEM",
						Password:              "PASSWORD#",
						EnableSsl:             true,
						HostNameInCertificate: "",
						TlsRootCaFile:         "",
					},
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validateHANASSLConfig(test.conf); got != test.want {
				t.Errorf("validateHANASSLConfig() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLogLevelToZapcore(t *testing.T) {
	tests := []struct {
		name  string
		level cpb.Configuration_LogLevel
		want  zapcore.Level
	}{
		{
			name:  "INFO",
			level: cpb.Configuration_INFO,
			want:  zapcore.InfoLevel,
		},
		{
			name:  "DEBUG",
			level: cpb.Configuration_DEBUG,
			want:  zapcore.DebugLevel,
		},
		{
			name:  "WARNING",
			level: cpb.Configuration_WARNING,
			want:  zapcore.WarnLevel,
		},
		{
			name:  "ERROR",
			level: cpb.Configuration_ERROR,
			want:  zapcore.ErrorLevel,
		},
		{
			name:  "UNKNOWN",
			level: cpb.Configuration_UNDEFINED,
			want:  zapcore.InfoLevel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := LogLevelToZapcore(test.level)
			if got != test.want {
				t.Errorf("LogLevelToZapcore(%v) = %v, want: %v", test.level, got, test.want)
			}
		})
	}
}
