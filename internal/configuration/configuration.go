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

// Package configuration provides configuration reading capabilities.
package configuration

import (
	// Enable file embedding, see also http://go/go-embed.
	_ "embed"
	"runtime"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	iipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
)

// ReadConfigFile abstracts os.ReadFile function for testability.
type ReadConfigFile func(string) ([]byte, error)

var ros = runtime.GOOS

//go:embed defaultconfigs/hanamonitoring/default_queries.json
var defaultHMQueriesContent []byte

const (
	// AgentName is a short-hand name of the agent.
	AgentName = "sapagent"
	// AgentVersion is the version of the agent.
	// LINT.IfChange
	AgentVersion = "1.1"
	// LINT.ThenChange(//depot/google3/third_party/sapagent/BUILD)
	linuxConfigPath               = "/etc/google-cloud-sap-agent/configuration.json"
	linuxHANAMonitoringConfigPath = "/etc/google-cloud-sap-agent/hm-configuration.json"
	windowsConfigPath             = "C:\\Program Files\\Google\\google-cloud-sap-agent\\conf\\configuration.json"
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
		log.Logger.Errorw("Could not read from configuration file", "file", p, "error", err)
		usagemetrics.Error(usagemetrics.ConfigFileReadFailure)
		return nil
	}

	// The field provide_sap_host_agent_metrics is a special default that needs to be
	// initialized before reading into proto. All other defaults are set later.
	config := &cpb.Configuration{ProvideSapHostAgentMetrics: true}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		log.Logger.Errorw("Invalid content in the configuration file", "file", p, "content", string(content), "error", err)
	}
	config.HanaMonitoringConfiguration = readConfig(linuxHANAMonitoringConfigPath, read)
	log.Logger.Infow("Configuration read for the agent", "Configuration", config)
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
		cc.ProcessMetricsFrequency = 5
	}
	if cc != nil && cc.GetCollectAgentMetrics() && cc.GetAgentMetricsFrequency() <= 0 {
		cc.AgentMetricsFrequency = 60
	}
	if cc.GetCollectAgentMetrics() && cc.GetAgentHealthFrequency() <= 0 {
		cc.AgentHealthFrequency = 60
	}
	if cc.GetCollectAgentMetrics() && cc.GetHeartbeatFrequency() <= 0 {
		cc.HeartbeatFrequency = 60
	}
	if cc.GetCollectAgentMetrics() && cc.GetMissedHeartbeatThreshold() <= 0 {
		cc.MissedHeartbeatThreshold = 10
	}
	// If the user did not pass cloud properties, set the values read from the metadata server.
	if config.GetCloudProperties() == nil {
		config.CloudProperties = cloudProps
	}

	hmConfig := config.GetHanaMonitoringConfiguration()
	if hmConfig != nil && hmConfig.GetQueryTimeoutSec() <= 0 {
		hmConfig.QueryTimeoutSec = 300
	}
	if hmConfig != nil && hmConfig.GetSampleIntervalSec() <= 5 {
		hmConfig.SampleIntervalSec = 300
	}
	if hmConfig != nil && hmConfig.GetExecutionThreads() <= 0 {
		hmConfig.ExecutionThreads = 10
	}
	return config
}

// readConfig reads the default HANA Monitoring queries and custom HANA Monitoring
// configuration, parses them into a proto, applies overrides and returns final HANA Monitoring Configuration.
func readConfig(path string, read ReadConfigFile) *cpb.HANAMonitoringConfiguration {
	defaultConfig := &cpb.HANAMonitoringConfiguration{}
	err := protojson.Unmarshal(defaultHMQueriesContent, defaultConfig)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedDefaultHANAMonitoringQueriesFile)
		log.Logger.Errorw("Invalid content in the embeded default_queries.json file", "content", string(defaultHMQueriesContent), "error", err)
		return nil
	}
	configContent, err := read(path)
	if err != nil || len(configContent) == 0 {
		usagemetrics.Error(usagemetrics.HANAMonitoringConfigReadFailure)
		log.Logger.Errorw("Unable to read the configuration file", "file", path, "content", string(configContent), "error", err)
		return nil
	}
	customConfig := &cpb.HANAMonitoringConfiguration{}
	err = protojson.Unmarshal(configContent, customConfig)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedHANAMonitoringConfigFile)
		log.Logger.Errorw("Invalid content in the configuration file", "file", path, "content", string(configContent), "error", err)
		return nil
	}
	customConfig.Queries = applyOverrides(defaultConfig.GetQueries(), customConfig.GetQueries())
	return customConfig
}

// applyOverrides takes defaultHMQueriesList and CustomHMQueriesList to control which queries are
// enabled/disabled. In case of default queries if there is no override item in the custom query list
// then default query is treated as enabled.
func applyOverrides(defaultHMQueriesList, customHMQueriesList []*cpb.Query) []*cpb.Query {
	result := []*cpb.Query{}
	for _, query := range defaultHMQueriesList {
		q := query
		q.Enabled = true
		for _, customQuery := range customHMQueriesList {
			if customQuery.GetName() == ("default_" + query.GetName()) {
				// every override query's name is of the form `default_` + queryName
				log.Logger.Debugw("Overriding query", "Query", query, "enabled", customQuery.GetEnabled())
				q.Enabled = customQuery.GetEnabled()
				break
			}
		}
		if q.GetEnabled() {
			result = append(result, q)
		}
	}
	for _, query := range customHMQueriesList {
		if !strings.HasPrefix(query.GetName(), "default_") {
			if query.GetEnabled() {
				result = append(result, query)
			}
		}
	}
	return result
}
