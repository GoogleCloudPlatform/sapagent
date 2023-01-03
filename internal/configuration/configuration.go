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

// Package configuration provides configuration reading cabailities.
package configuration

import (
	"fmt"
	"runtime"

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sap-agent/protos/instanceinfo"
)

// ReadConfigFile abstracts os.ReadFile function for testability.
type ReadConfigFile func(string) ([]byte, error)

var ros = runtime.GOOS

const (
	// AgentName is a short-hand name of the agent.
	AgentName = "sapagent"
	// AgentVersion is the version of the agent.
	// LINT.IfChange
	AgentVersion = "1.0"
	// LINT.ThenChange(//depot/google3/third_party/sapagent/BUILD)
	linuxConfigPath   = "/usr/sap/google-cloud-sap-agent/conf/configuration.json"
	windowsConfigPath = "C:\\Program Files\\Google\\google-cloud-sap-agent\\conf\\configuration.json"
)

// ReadFromFile reads configuration from given file into proto.
func ReadFromFile(path string, read ReadConfigFile) *cpb.Configuration {
	p := path
	if len(p) == 0 {
		p = linuxConfigPath
		if ros == "windows" {
			p = windowsConfigPath
		}
	}
	content, err := read(p)
	if err != nil || len(content) == 0 {
		log.Logger.Error(fmt.Sprintf("Could not read from configuration file: %s", p), log.Error(err))
		usagemetrics.Error(1) // Invalid configuration
		return nil
	}

	// The field provide_sap_host_agent_metrics is a special default that needs to be
	// initialized before reading into proto. All other defaults are set later.
	config := &cpb.Configuration{ProvideSapHostAgentMetrics: true}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		usagemetrics.Error(1) // Invalid configuration
		log.Logger.Error(fmt.Sprintf("Invalid content in the configuration file: %s, content: %v", p, string(content)), log.Error(err))
	}
	return config
}

// ApplyDefaults will apply the default configuration settings to the configuration passed.
// The defaults are set only if the values passed are UNDEFINED or invalid.
func ApplyDefaults(configFromFile *cpb.Configuration, cloudProps *iipb.CloudProperties) *cpb.Configuration {
	config := configFromFile
	if config == nil {
		config = &cpb.Configuration{ProvideSapHostAgentMetrics: true}
	}
	// always set the agent name and version
	config.AgentProperties = &cpb.AgentProperties{Name: AgentName, Version: AgentVersion}

	cc := config.GetCollectionConfiguration()
	if cc != nil && cc.GetCollectWorkloadValidationMetrics() == true && cc.GetWorkloadValidationMetricsFrequency() <= 0 {
		cc.WorkloadValidationMetricsFrequency = 300
	}
	if cc != nil && cc.GetCollectProcessMetrics() == true && cc.GetProcessMetricsFrequency() <= 0 {
		cc.ProcessMetricsFrequency = 60
	}
	// If the user did not pass cloud properties, set the values read from metadata server.
	if config.GetCloudProperties() == nil {
		config.CloudProperties = cloudProps
	}

	return config
}
