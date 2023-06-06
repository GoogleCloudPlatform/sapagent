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
	_ "embed" // Enable file embedding, see also http://go/go-embed.
	"errors"
	"fmt"
	"os"
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

// WriteConfigFile abstracts os.WriteFile function for testability.
type WriteConfigFile func(string, []byte, os.FileMode) error

var ros = runtime.GOOS

//go:embed defaultconfigs/hanamonitoring/default_queries.json
var defaultHMQueriesContent []byte

// DefaultCollectionDefinition embeds the contents of the file located at:
//
//go:embed defaultconfigs/collectiondefinition/collection_definition.json
var DefaultCollectionDefinition []byte

const (
	// AgentName is a short-hand name of the agent.
	AgentName = `sapagent`

	

	// AgentVersion is the version of the agent.
	AgentVersion = `2.1`
	// LINT.ThenChange(//depot/google3/third_party/sapagent/BUILD, //depot/google3/third_party/sapagent/google-cloud-sap-agent.blueprint)

	// LinuxConfigPath is the default path to agent configuration file on linux.
	LinuxConfigPath = `/etc/google-cloud-sap-agent/configuration.json`
	// WindowsConfigPath is the default path to agent configuration file on linux.
	WindowsConfigPath = `C:\Program Files\Google\google-cloud-sap-agent\conf\configuration.json`
)

// ReadFromFile reads configuration from given file into proto.
func ReadFromFile(path string, read ReadConfigFile) *cpb.Configuration {
	p := path
	if len(p) == 0 {
		p = LinuxConfigPath
		if ros == "windows" {
			p = WindowsConfigPath
		}
	}
	content, err := read(p)
	if err != nil || len(content) == 0 {
		log.Logger.Errorw("Could not read from configuration file", "file", p, "error", err)
		usagemetrics.Error(usagemetrics.ConfigFileReadFailure)
		return nil
	}

	// The fields provide_sap_host_agent_metrics and log_to_cloud are special defaults that need to be
	// initialized before reading into proto. All other defaults are set later.
	config := &cpb.Configuration{ProvideSapHostAgentMetrics: true, LogToCloud: true}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		log.Logger.Errorw("Invalid content in the configuration file", "file", p, "content", string(content))
		log.Logger.Errorf("Configuration JSON at '%s' has error: %v. Only hostmetrics will be started. Please fix the JSON and restart the agent", p, err)
	}
	config.HanaMonitoringConfiguration = prepareHMConf(config.HanaMonitoringConfiguration)
	log.Logger.Debugw("Configuration read for the agent", "Configuration", config)
	return config
}

// ApplyDefaults will apply the default configuration settings to the configuration passed.
// The defaults are set only if the values passed are UNDEFINED or invalid.
func ApplyDefaults(configFromFile *cpb.Configuration, cloudProps *iipb.CloudProperties) *cpb.Configuration {
	config := configFromFile
	if config == nil {
		config = &cpb.Configuration{ProvideSapHostAgentMetrics: true, LogToCloud: true}
	}
	// Always set the agent name and version.
	config.AgentProperties = &cpb.AgentProperties{Name: AgentName, Version: AgentVersion}

	// If the user did not pass cloud properties, set the values read from the metadata server.
	if config.GetCloudProperties() == nil {
		config.CloudProperties = cloudProps
	}

	config.CollectionConfiguration = applyDefaultCollectionConfiguration(config.GetCollectionConfiguration())
	config.HanaMonitoringConfiguration = applyDefaultHMConfiguration(config.GetHanaMonitoringConfiguration())
	return config
}

func applyDefaultCollectionConfiguration(configFromFile *cpb.CollectionConfiguration) *cpb.CollectionConfiguration {
	cc := configFromFile
	if cc.GetCollectWorkloadValidationMetrics() && cc.GetWorkloadValidationMetricsFrequency() <= 0 {
		cc.WorkloadValidationMetricsFrequency = 300
	}
	if cc.GetCollectProcessMetrics() && cc.GetProcessMetricsFrequency() <= 0 {
		cc.ProcessMetricsFrequency = 5
	}
	if cc.GetCollectAgentMetrics() && cc.GetAgentMetricsFrequency() <= 0 {
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
	return cc
}

func applyDefaultHMConfiguration(configFromFile *cpb.HANAMonitoringConfiguration) *cpb.HANAMonitoringConfiguration {
	hmConfig := configFromFile
	if hmConfig != nil && hmConfig.GetQueryTimeoutSec() <= 0 {
		hmConfig.QueryTimeoutSec = 300
	}
	if hmConfig != nil && hmConfig.GetSampleIntervalSec() < 5 {
		hmConfig.SampleIntervalSec = 300
	}
	if hmConfig != nil && hmConfig.GetExecutionThreads() <= 0 {
		hmConfig.ExecutionThreads = 10
	}
	return hmConfig
}

// prepareHMConf reads the default HANA Monitoring queries, parses them into a proto,
// applies overrides from user configuration and returns final HANA Monitoring Configuration.
func prepareHMConf(config *cpb.HANAMonitoringConfiguration) *cpb.HANAMonitoringConfiguration {
	defaultConfig := &cpb.HANAMonitoringConfiguration{}
	err := protojson.Unmarshal(defaultHMQueriesContent, defaultConfig)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedDefaultHANAMonitoringQueriesFile)
		log.Logger.Errorw("Invalid content in the embeded default_queries.json file", "content", string(defaultHMQueriesContent), "error", err)
		return nil
	}
	if config == nil {
		log.Logger.Debugw("HANA Monitoring Configuration not set in config file", "file", LinuxConfigPath)
		return nil
	}
	if !validateHANASSLConfig(config) {
		return nil
	}
	config.Queries = applyOverrides(defaultConfig.GetQueries(), config.GetQueries())
	if !ValidateQueries(config.Queries) {
		return nil
	}
	return config
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

// validateHANASSLConfig ensures that if a HANA instance wants to use SSL connection,
// the certificate path and host name in certificate should be set.
func validateHANASSLConfig(config *cpb.HANAMonitoringConfiguration) bool {
	var errs []string
	for _, i := range config.GetHanaInstances() {
		if !i.GetEnableSsl() {
			continue
		}
		if i.GetHostNameInCertificate() == "" {
			errs = append(errs, fmt.Sprintf("missing hostname in certificate for HANA instance: %#q", i.GetName()))
		}
		if i.GetTlsRootCaFile() == "" {
			errs = append(errs, fmt.Sprintf("missing tls root ca file for HANA instance: %#q", i.GetName()))
		}
	}
	if len(errs) > 0 {
		log.Logger.Errorw("Invalid Config", "err", strings.Join(errs, ", "))
		return false
	}
	return true
}

// ValidateQueries is responsible for making sure that the custom queries have the correct metric
// and value type for the columns. In case of invalid combination it returns false.
// Query names and column names within each query must be unique as they are both used to path the metric URL to cloud monitoring.
func ValidateQueries(queries []*cpb.Query) bool {
	queryNames := make(map[string]bool)
	for _, q := range queries {
		if queryNames[q.Name] {
			usagemetrics.Error(usagemetrics.MalformedHANAMonitoringConfigFile)
			log.Logger.Errorw("Duplicate query name", "queryName", q.Name)
			return false
		}
		queryNames[q.Name] = true

		if !validateColumns(q) {
			return false
		}
	}
	return true
}

func validateColumns(q *cpb.Query) bool {
	columnNames := make(map[string]bool)
	for _, col := range q.GetColumns() {
		if columnNames[col.Name] {
			usagemetrics.Error(usagemetrics.MalformedHANAMonitoringConfigFile)
			log.Logger.Errorw("Duplicate column name", "queryName", q.Name, "column", col.Name)
			return false
		}
		columnNames[col.Name] = true

		if err := validateColumnTypes(col); err != nil {
			usagemetrics.Error(usagemetrics.MalformedHANAMonitoringConfigFile)
			log.Logger.Errorw("Invalid config", "error", err, "queryName", q.Name, "column", col.Name, "metricType", col.MetricType, "valueType", col.ValueType)
			return false
		}
	}
	return true
}

func validateColumnTypes(col *cpb.Column) error {
	if col.MetricType == cpb.MetricType_METRIC_UNSPECIFIED || col.ValueType == cpb.ValueType_VALUE_UNSPECIFIED {
		return errors.New("required fields for column not set")
	}
	if col.MetricType == cpb.MetricType_METRIC_LABEL && col.ValueType != cpb.ValueType_VALUE_STRING {
		return errors.New("incompatible metric and value type for column")
	}
	if col.MetricType == cpb.MetricType_METRIC_GAUGE && col.ValueType == cpb.ValueType_VALUE_STRING {
		return errors.New("the value type is not supported for GAUGE custom metrics on column")
	}
	if col.MetricType == cpb.MetricType_METRIC_CUMULATIVE && (col.ValueType == cpb.ValueType_VALUE_STRING || col.ValueType == cpb.ValueType_VALUE_BOOL) {
		return errors.New("the value type is not supported for CUMULATIVE custom metrics on column")
	}
	return nil
}
