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
