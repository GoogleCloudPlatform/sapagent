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
	"time"

	wpb "google.golang.org/protobuf/types/known/wrapperspb"
	"google.golang.org/protobuf/encoding/protojson"
	"go.uber.org/zap/zapcore"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	dpb "google.golang.org/protobuf/types/known/durationpb"
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

// AgentBuildChange is the change number that the agent was built at
// this will be replaced using "-X github.com/GoogleCloudPlatform/sapagent/internal/configuration.AgentBuildChange=$CLNUMBER" by the build process
var AgentBuildChange = `0`

const (
	// AgentName is a short-hand name of the agent.
	AgentName = `sapagent`

	

	// AgentVersion is the version of the agent.
	AgentVersion = `3.3`
	

	// LinuxConfigPath is the default path to agent configuration file on linux.
	LinuxConfigPath = `/etc/google-cloud-sap-agent/configuration.json`
	// WindowsConfigPath is the default path to agent configuration file on linux.
	WindowsConfigPath = `C:\Program Files\Google\google-cloud-sap-agent\conf\configuration.json`
)

// Read just reads configuration from given file and parses it into config proto.
func Read(path string, read ReadConfigFile) *cpb.Configuration {
	content, err := read(path)
	if err != nil || len(content) == 0 {
		log.Logger.Errorw("Could not read from configuration file", "file", path, "error", err)
		usagemetrics.Error(usagemetrics.ConfigFileReadFailure)
		return nil
	}

	config := &cpb.Configuration{}
	err = protojson.Unmarshal(content, config)
	if err != nil {
		usagemetrics.Error(usagemetrics.MalformedConfigFile)
		log.Logger.Errorw("Invalid content in the configuration file", "file", path, "content", string(content))
		log.Logger.Errorf("Configuration JSON at '%s' has error: %v. Only hostmetrics will be started. Please fix the JSON and restart the agent", path, err)
	}
	return config
}

// ReadFromFile reads the final configuration from the given file. Besides parsing the file,
// it consists of the final HANA Monitoring configuration after parsing all the enabled
// HANA Monitoring queries, by applying overrides wherever necessary, into a proto.
func ReadFromFile(path string, read ReadConfigFile) *cpb.Configuration {
	p := path
	if len(p) == 0 {
		p = LinuxConfigPath
		if ros == "windows" {
			p = WindowsConfigPath
		}
	}

	config := Read(p, read)
	if config == nil {
		return nil
	}

	config.HanaMonitoringConfiguration = prepareHMConf(config.HanaMonitoringConfiguration)
	log.Logger.Debugw("Configuration read for the agent", "Configuration", config)
	return config
}

// LogLevelToZapcore returns the zapcore equivalent of the configuration log level.
func LogLevelToZapcore(level cpb.Configuration_LogLevel) zapcore.Level {
	switch level {
	case cpb.Configuration_DEBUG:
		return zapcore.DebugLevel
	case cpb.Configuration_INFO:
		return zapcore.InfoLevel
	case cpb.Configuration_WARNING:
		return zapcore.WarnLevel
	case cpb.Configuration_ERROR:
		return zapcore.ErrorLevel
	default:
		log.Logger.Warnw("Unsupported log level, defaulting to INFO", "level", level.String())
		return zapcore.InfoLevel
	}
}

// ApplyDefaults will apply the default configuration settings to the configuration passed.
// The defaults are set only if the values passed are UNDEFINED or invalid.
func ApplyDefaults(configFromFile *cpb.Configuration, cloudProps *iipb.CloudProperties) *cpb.Configuration {
	config := configFromFile
	if config == nil {
		config = &cpb.Configuration{}
	}
	// Always set the agent name and version.
	config.AgentProperties = &cpb.AgentProperties{Name: AgentName, Version: AgentVersion}

	// The fields provide_sap_host_agent_metrics and log_to_cloud will be
	// defaulted to true if a user does not provide a value in the config.
	if config.GetProvideSapHostAgentMetrics() == nil {
		config.ProvideSapHostAgentMetrics = &wpb.BoolValue{Value: true}
	}
	if config.GetLogToCloud() == nil {
		config.LogToCloud = &wpb.BoolValue{Value: true}
	}

	// If the user did not pass cloud properties, set the values read from the metadata server.
	if config.GetCloudProperties() == nil {
		config.CloudProperties = cloudProps
	}

	// Special logic for handling two flags to disable system discovery
	if config.GetCollectionConfiguration().GetSapSystemDiscovery() != nil {
		if config.GetDiscoveryConfiguration() == nil {
			config.DiscoveryConfiguration = &cpb.DiscoveryConfiguration{}
		}
		if config.GetDiscoveryConfiguration().GetEnableDiscovery().GetValue() != config.GetCollectionConfiguration().GetSapSystemDiscovery().GetValue() {
			// Flags differ, assume disable.
			config.DiscoveryConfiguration.EnableDiscovery = &wpb.BoolValue{Value: false}
		} else {
			config.DiscoveryConfiguration.EnableDiscovery = config.GetCollectionConfiguration().GetSapSystemDiscovery()
		}
	}

	config.CollectionConfiguration = applyDefaultCollectionConfiguration(config.GetCollectionConfiguration())
	config.HanaMonitoringConfiguration = applyDefaultHMConfiguration(config.GetHanaMonitoringConfiguration())
	config.DiscoveryConfiguration = applyDefaultDiscoveryConfiguration(config.GetDiscoveryConfiguration())
	config.SupportConfiguration = applyDefaultSupportConfiguration(config.GetSupportConfiguration())

	return config
}

func applyDefaultCollectionConfiguration(configFromFile *cpb.CollectionConfiguration) *cpb.CollectionConfiguration {
	cc := configFromFile
	if cc == nil {
		cc = &cpb.CollectionConfiguration{}
		cc.CollectReliabilityMetrics = &wpb.BoolValue{Value: false}
	}
	
	if cc.GetCollectWorkloadValidationMetrics() == nil {
		cc.CollectWorkloadValidationMetrics = &wpb.BoolValue{Value: true}
	}
	if cc.GetCollectWorkloadValidationMetrics().GetValue() && cc.GetWorkloadValidationMetricsFrequency() <= 0 {
		cc.WorkloadValidationMetricsFrequency = 300
	}
	if cc.GetCollectWorkloadValidationMetrics().GetValue() && cc.GetWorkloadValidationDbMetricsFrequency() <= 0 {
		cc.WorkloadValidationDbMetricsFrequency = 3600 // Default frequency is 1 hour.
	}
	if cc.GetCollectProcessMetrics() && cc.GetProcessMetricsFrequency() <= 0 {
		cc.ProcessMetricsFrequency = 5
	}
	if cc.GetCollectProcessMetrics() && cc.GetSlowProcessMetricsFrequency() <= 0 {
		cc.SlowProcessMetricsFrequency = 30
	}
	if cc.GetCollectReliabilityMetrics().GetValue() && cc.GetReliabilityMetricsFrequency() <= 0 {
		cc.ReliabilityMetricsFrequency = 60
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
	if cc.GetDataWarehouseEndpoint() == "" {
		cc.DataWarehouseEndpoint = "https://workloadmanager-datawarehouse.googleapis.com/"
	}
	if cc.GetWorkloadValidationCollectionDefinition() == nil {
		cc.WorkloadValidationCollectionDefinition = &cpb.WorkloadValidationCollectionDefinition{
			FetchLatestConfig:       &wpb.BoolValue{Value: true},
			ConfigTargetEnvironment: cpb.TargetEnvironment_PRODUCTION,
		}
	}
	if cc.GetWorkloadValidationCollectionDefinition().GetConfigTargetEnvironment() == cpb.TargetEnvironment_TARGET_ENVIRONMENT_UNSPECIFIED {
		cc.WorkloadValidationCollectionDefinition.ConfigTargetEnvironment = cpb.TargetEnvironment_PRODUCTION
	}
	if cc.GetWorkloadValidationCollectionDefinition().GetFetchLatestConfig() == nil {
		cc.WorkloadValidationCollectionDefinition.FetchLatestConfig = &wpb.BoolValue{Value: true}
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

func applyDefaultDiscoveryConfiguration(configFromFile *cpb.DiscoveryConfiguration) *cpb.DiscoveryConfiguration {
	discoveryConfig := configFromFile
	if discoveryConfig == nil {
		discoveryConfig = &cpb.DiscoveryConfiguration{}
	}
	if discoveryConfig.GetEnableDiscovery() == nil {
		discoveryConfig.EnableDiscovery = &wpb.BoolValue{Value: true}
	}
	if discoveryConfig.GetSapInstancesUpdateFrequency() == nil {
		discoveryConfig.SapInstancesUpdateFrequency = dpb.New(time.Duration(1 * time.Minute))
	}
	if discoveryConfig.GetSystemDiscoveryUpdateFrequency() == nil {
		discoveryConfig.SystemDiscoveryUpdateFrequency = dpb.New(time.Duration(4 * time.Hour))
	}
	if discoveryConfig.GetEnableWorkloadDiscovery() == nil {
		discoveryConfig.EnableWorkloadDiscovery = &wpb.BoolValue{Value: true}
	}
	return discoveryConfig
}

func applyDefaultSupportConfiguration(configFromFile *cpb.SupportConfiguration) *cpb.SupportConfiguration {
	supportConfig := configFromFile
	if supportConfig == nil {
		supportConfig = &cpb.SupportConfiguration{}
	}
	return supportConfig
}

// PrepareHMConf reads the default HANA Monitoring queries, parses them into a proto,
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
