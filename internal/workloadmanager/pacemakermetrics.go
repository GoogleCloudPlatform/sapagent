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

package workloadmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const sapValidationPacemaker = "workload.googleapis.com/sap/validation/pacemaker"

// CollectPacemakerMetricsFromConfig collects the pacemaker metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectPacemakerMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	// Prune the configurable labels depending on what is defined in the workload config.
	pruneLabels := map[string]bool{
		"pcmk_delay_base":                  true,
		"pcmk_delay_max":                   true,
		"pcmk_monitor_retries":             true,
		"pcmk_reboot_timeout":              true,
		"location_preference_set":          true,
		"migration_threshold":              true,
		"resource_stickiness":              true,
		"saphana_start_timeout":            true,
		"saphana_stop_timeout":             true,
		"saphana_promote_timeout":          true,
		"saphana_demote_timeout":           true,
		"fence_agent":                      true,
		"fence_agent_compute_api_access":   true,
		"fence_agent_logging_api_access":   true,
		"maintenance_mode_active":          true,
		"saphanatopology_monitor_interval": true,
		"saphanatopology_monitor_timeout":  true,
	}
	pacemaker := params.WorkloadConfig.GetValidationPacemaker()
	pconfig := params.WorkloadConfig.GetValidationPacemaker().GetConfigMetrics()
	for _, m := range pconfig.GetPrimitiveMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pconfig.GetRscLocationMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pconfig.GetRscOptionMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pconfig.GetHanaOperationMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pconfig.GetFenceAgentMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pacemaker.GetCibBootstrapOptionMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}

	pacemakerVal, l := collectPacemakerValAndLabels(ctx, params)
	for label := range pruneLabels {
		delete(l, label)
	}
	// Add OS command metrics to the labels.
	for _, m := range pacemaker.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(sapValidationPacemaker, l, pacemakerVal, params.Config)}
}

func collectPacemakerValAndLabels(ctx context.Context, params Parameters) (float64, map[string]string) {
	l := map[string]string{}

	if params.Config.GetCloudProperties() == nil {
		log.Logger.Debug("No cloud properties")
		return 0.0, l
	}
	properties := params.Config.GetCloudProperties()
	projectID := properties.GetProjectId()
	crmAvailable := params.Exists("crm")
	pacemakerXMLString := pacemaker.XMLString(ctx, params.Execute, crmAvailable)

	if pacemakerXMLString == nil {
		log.Logger.Debug("No pacemaker xml")
		return 0.0, l
	}
	pacemakerDocument, err := ParseXML([]byte(*pacemakerXMLString))

	if err != nil {
		log.Logger.Debugw("Could not parse the pacemaker configuration xml", "xml", *pacemakerXMLString, "error", err)
		return 0.0, l
	}

	instances := clusterNodes(pacemakerDocument.Configuration.Nodes)
	// Sort VM instance names by length, descending.
	// This should prevent collisions when searching within a substring.
	// Ex: instance11, ... , instance1
	sort.Slice(instances, func(i, j int) bool { return len(instances[i]) > len(instances[j]) })

	primitives := pacemakerDocument.Configuration.Resources.Primitives
	results := setPacemakerPrimitives(l, primitives, instances, params.Config)

	if id, ok := results["projectId"]; ok {
		projectID = id
	}

	bearerToken, err := getBearerToken(ctx, results["serviceAccountJsonFile"], params.ConfigFileReader,
		params.JSONCredentialsGetter, params.DefaultTokenGetter)
	if err != nil {
		log.Logger.Debugw("Could not parse the pacemaker configuration xml", "xml", *pacemakerXMLString, "error", err)
		return 0.0, l
	}
	rscLocations := pacemakerDocument.Configuration.Constraints.RSCLocations
	locationPreferenceSet := "false"

	for _, rscLocation := range rscLocations {
		idNode := rscLocation.ID
		if strings.HasPrefix(idNode, "cli-prefer-") {
			locationPreferenceSet = "true"
			break
		}
	}
	l["location_preference_set"] = locationPreferenceSet

	rscOptionNvPairs := pacemakerDocument.Configuration.RSCDefaults.NVPairs
	setLabelsForRSCNVPairs(l, rscOptionNvPairs, "migration-threshold")
	setLabelsForRSCNVPairs(l, rscOptionNvPairs, "resource-stickiness")

	// This will get any <primitive> with type=SAPHana, these can be under <clone> or <master>.
	setPacemakerHanaOperations(l, filterPrimitiveOpsByType(pacemakerDocument.Configuration.Resources.Clone.Primitives, "SAPHana"))
	setPacemakerHanaOperations(l, filterPrimitiveOpsByType(pacemakerDocument.Configuration.Resources.Master.Primitives, "SAPHana"))

	setPacemakerAPIAccess(ctx, l, projectID, bearerToken, params.Execute)
	setPacemakerMaintenanceMode(ctx, l, crmAvailable, params.Execute)

	// This will get any <primitive> with type=SAPHanaTopology, these can be under <clone> or <master>.
	pacemakerHanaTopology(l, filterPrimitiveOpsByType(pacemakerDocument.Configuration.Resources.Clone.Primitives, "SAPHanaTopology"))
	pacemakerHanaTopology(l, filterPrimitiveOpsByType(pacemakerDocument.Configuration.Resources.Master.Primitives, "SAPHanaTopology"))

	return 1.0, l
}

func clusterNodes(nodes []CIBNode) []string {
	instances := make([]string, 0, len(nodes))
	for _, node := range nodes {
		instances = append(instances, node.Uname)
	}
	return instances
}

func filterPrimitiveOpsByType(primitives []PrimitiveClass, primitiveType string) []Op {
	ops := []Op{}
	for _, primitive := range primitives {
		if primitive.ClassType == primitiveType {
			ops = append(ops, primitive.Operations...)
		}
	}
	return ops
}

func setPacemakerHanaOperations(l map[string]string, sapHanaOperations []Op) {
	for _, sapHanaOperation := range sapHanaOperations {
		name := sapHanaOperation.Name
		switch name {
		case "start":
			l["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "stop":
			l["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "promote":
			l["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "demote":
			l["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		default:
			// fall through
		}
	}
}

func setPacemakerAPIAccess(ctx context.Context, l map[string]string, projectID string, bearerToken string, exec commandlineexecutor.Execute) {
	fenceAgentComputeAPIAccess, err := checkAPIAccess(ctx, exec,
		"-H",
		fmt.Sprintf("Authorization: Bearer %s ", bearerToken),
		fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s?fields=id", projectID))
	if err != nil {
		log.Logger.Debugw("Could not obtain fence agent compute API Access", log.Error(err))
	}

	fenceAgentLoggingAPIAccess, err := checkAPIAccess(ctx, exec,
		"-H",
		fmt.Sprintf("Authorization: Bearer %s", bearerToken),
		"https://logging.googleapis.com/v2/entries:write",
		"-X",
		"POST",
		"-H",
		"Content-Type: application/json",
		"-d",
		fmt.Sprintf(`{"dryRun": true, "entries": [{"logName": "projects/%s`, projectID)+
			`/logs/test-log", "resource": {"type": "gce_instance"}, "textPayload": "foo"}]}"`)
	if err != nil {
		log.Logger.Debugw("Could not obtain fence agent logging API Access", log.Error(err))
	}
	l["fence_agent_compute_api_access"] = strconv.FormatBool(fenceAgentComputeAPIAccess)
	l["fence_agent_logging_api_access"] = strconv.FormatBool(fenceAgentLoggingAPIAccess)
}

func checkAPIAccess(ctx context.Context, exec commandlineexecutor.Execute, args ...string) (bool, error) {
	/*
	   ResponseError encodes a potential response error returned via the pacemaker authorization token
	   google API check.
	*/
	type ResponseError struct {
		Code    string
		Message string
	}

	/*
	   JSONResponse stores an encoded JSON response payload from a google API endpoint.
	*/
	type JSONResponse struct {
		ResponseError *ResponseError `json:"error"`
	}

	result := exec(ctx, commandlineexecutor.Params{
		Executable: "curl",
		Args:       args,
	})
	if result.Error != nil {
		// Curl failed. We can't conclude anything about the ACL.
		return false, result.Error
	}

	jsonResponse := new(JSONResponse)

	if err := json.Unmarshal([]byte(result.StdOut), jsonResponse); err != nil {
		// Malformed JSON response.  We can't conclude anything about the ACL
		return false, err
	}

	return jsonResponse.ResponseError == nil, nil
}

/*
setPacemakerMaintenanceMode defines the pacemaker maintenance mode label for the metric validation
collector.
*/
func setPacemakerMaintenanceMode(ctx context.Context, l map[string]string, crmAvailable bool, exec commandlineexecutor.Execute) {
	result := commandlineexecutor.Result{}
	if crmAvailable {
		result = exec(ctx, commandlineexecutor.Params{
			Executable:  "sh",
			ArgsToSplit: "-c 'crm configure show | grep maintenance | grep true'",
		})
	} else {
		result = exec(ctx, commandlineexecutor.Params{
			Executable:  "sh",
			ArgsToSplit: "-c 'pcs property show | grep maintenance | grep true'",
		})
	}
	log.Logger.Debugw("Pacemaker maintenance mode", "maintenanceMode", result.StdOut, "stderr", result.StdErr, "err", result.Error)
	maintenanceModeLabel := "false"
	if result.Error == nil && result.StdOut != "" {
		maintenanceModeLabel = "true"
	}
	l["maintenance_mode_active"] = maintenanceModeLabel
}

/*
setLabelsForRscNvPairs converts a list of pacemaker name/value XML nodes to metric labels if they
match a specific name.
*/
func setLabelsForRSCNVPairs(l map[string]string, rscOptionNvPairs []NVPair, nameToFind string) {
	for _, rscOptionNvPair := range rscOptionNvPairs {
		if rscOptionNvPair.Name == nameToFind {
			l[strings.ReplaceAll(nameToFind, "-", "_")] = rscOptionNvPair.Value
		}
	}
}

func setPacemakerPrimitives(l map[string]string, primitives []PrimitiveClass, instances []string, c *cnfpb.Configuration) map[string]string {
	returnMap := map[string]string{}
	var pcmkDelayMax []string
	serviceAccountJSONFile := ""
	properties := c.GetCloudProperties()

	for _, primitive := range primitives {
		idNode := primitive.ID
		classNode := primitive.Class
		typeNode := primitive.ClassType
		attribute := primitive.InstanceAttributes
		instanceName := instanceNameFromPrimitiveID(idNode, instances)

		// Collector for pcmk_delay_max should report the value for each instance
		// in a HA cluster, rather than just the value for the local instance.
		v := instanceAttributeValue("pcmk_delay_max", attribute, instanceName)
		if v != "" {
			pcmkDelayMax = append(pcmkDelayMax, v)
		}

		if typeNode != "fence_gce" && !strings.HasSuffix(attribute.ID, properties.GetInstanceName()+"-instance_attributes") {
			continue
		}
		serviceAccountJSONFile = iteratePrimitiveChild(l, attribute, classNode, typeNode, idNode, returnMap, properties.GetInstanceName())
		if serviceAccountJSONFile != "" {
			break
		}
	}
	if len(pcmkDelayMax) > 0 {
		l["pcmk_delay_max"] = strings.Join(pcmkDelayMax, ",")
	}
	returnMap["serviceAccountJsonFile"] = serviceAccountJSONFile
	return returnMap
}

// instanceNameFromPrimitiveID extracts the instance name from a Primitive ID.
//
// The ID string is expected to contain the instance name as a substring.
// There does not appear to be a way to programmatically parse the instance
// name from the primitive ID alone.
func instanceNameFromPrimitiveID(id string, instances []string) string {
	for _, instance := range instances {
		if strings.Contains(id, instance) {
			return instance
		}
	}
	return ""
}

// instanceAttributeValue extracts the value of a given instance attribute key.
//
// The return value is formatted as: "instance=value".
func instanceAttributeValue(key string, attribute ClusterPropertySet, instance string) string {
	if instance == "" {
		return ""
	}
	for _, nvPair := range attribute.NVPairs {
		if nvPair.Name == key && strings.HasSuffix(nvPair.ID, fmt.Sprintf("%s-instance_attributes-%s", instance, key)) {
			return instance + "=" + nvPair.Value
		}
	}
	return ""
}

func iteratePrimitiveChild(l map[string]string, attribute ClusterPropertySet, classNode string, typeNode string, idNode string, returnMap map[string]string, instanceName string) string {
	attributeChildren := attribute.NVPairs
	fenceAttributes := map[string]string{}
	portMatchesInstanceName := false
	serviceAccountPath := ""
	fenceKeys := map[string]bool{
		"pcmk_delay_base":      true,
		"pcmk_reboot_timeout":  true,
		"pcmk_monitor_retries": true,
	}

	for _, nvPair := range attributeChildren {
		if _, ok := fenceKeys[nvPair.Name]; ok {
			fenceAttributes[nvPair.Name] = nvPair.Value
		} else if nvPair.Name == "serviceaccount" {
			serviceAccountPath = nvPair.Value
		} else if nvPair.Name == "project" {
			returnMap["projectId"] = nvPair.Value
		} else if nvPair.Name == "port" && nvPair.Value == instanceName {
			portMatchesInstanceName = true
		}
	}

	// If the values for the stonith device are for the current instance then write the labels
	if (typeNode == "fence_gce" && portMatchesInstanceName) || strings.HasSuffix(idNode, instanceName) {
		for k, v := range fenceAttributes {
			l[k] = v
		}
		if classNode == "stonith" {
			l["fence_agent"] = strings.ReplaceAll(typeNode, "external/", "")
		}
	}

	return serviceAccountPath
}

func pacemakerHanaTopology(l map[string]string, sapHanaOperations []Op) {
	for _, sapHanaOperation := range sapHanaOperations {
		if sapHanaOperation.Name == "monitor" {
			l["saphanatopology_monitor_interval"] = sapHanaOperation.Interval
			l["saphanatopology_monitor_timeout"] = sapHanaOperation.Timeout
			return
		}
	}
}

func getDefaultBearerToken(ctx context.Context, tokenGetter DefaultTokenGetter) (string, error) {
	credentials, err := tokenGetter(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("could not obtain default credentials: %#v", err)
	}
	token, err := credentials.Token()
	if err != nil {
		return "", fmt.Errorf("could not obtain default bearer token: %#v", err)
	}
	return token.AccessToken, nil
}

func getJSONBearerToken(ctx context.Context, serviceAccountJSONFile string, fileReader ConfigFileReader, credGetter JSONCredentialsGetter) (string, error) {
	jsonStream, err := fileReader(serviceAccountJSONFile)
	if err != nil {
		return "", fmt.Errorf("Could not load credentials file: %#v", err)
	}
	jsonData, err := io.ReadAll(jsonStream)
	if err != nil {
		return "", fmt.Errorf("could not read JSON data: %#v", err)
	}
	jsonStream.Close()
	credentials, err := credGetter(ctx, jsonData, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("could not obtain credentials from JSON File: %s due to %#v", serviceAccountJSONFile, err)
	}
	token, err := credentials.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("could not obtain bearer token: %#v", err)
	}
	return token.AccessToken, nil
}

func getBearerToken(ctx context.Context, serviceAccountJSONFile string, fileReader ConfigFileReader, credGetter JSONCredentialsGetter, tokenGetter DefaultTokenGetter) (string, error) {
	token := ""
	err := error(nil)
	if serviceAccountJSONFile == "" {
		token, err = getDefaultBearerToken(ctx, tokenGetter)
	} else {
		token, err = getJSONBearerToken(ctx, serviceAccountJSONFile, fileReader, credGetter)
	}
	return token, err
}
