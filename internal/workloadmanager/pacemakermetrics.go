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
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/configurablemetrics"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"github.com/GoogleCloudPlatform/sapagent/internal/pacemaker"

	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const pacemakerAPILabel = "workload.googleapis.com/sap/validation/pacemaker"

// CollectPacemakerMetricsFromConfig collects the pacemaker metrics as specified
// by the WorkloadValidation config and formats the results as a time series to
// be uploaded to a Collection Storage mechanism.
func CollectPacemakerMetricsFromConfig(ctx context.Context, params Parameters) WorkloadMetrics {
	// Prune the configurable labels depending on what is defined in the workload config.
	pruneLabels := map[string]bool{
		"pcmk_delay_base":                true,
		"pcmk_delay_max":                 true,
		"pcmk_monitor_retries":           true,
		"pcmk_reboot_timeout":            true,
		"location_preference_set":        true,
		"migration_threshold":            true,
		"resource_stickiness":            true,
		"saphana_start_timeout":          true,
		"saphana_stop_timeout":           true,
		"saphana_promote_timeout":        true,
		"saphana_demote_timeout":         true,
		"fence_agent":                    true,
		"fence_agent_compute_api_access": true,
		"fence_agent_logging_api_access": true,
	}
	pacemaker := params.WorkloadConfig.GetValidationPacemaker().GetConfigMetrics()
	for _, m := range pacemaker.GetPrimitiveMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pacemaker.GetRscLocationMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pacemaker.GetRscOptionMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pacemaker.GetHanaOperationMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pacemaker.GetFenceAgentMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}

	pacemakerVal, l := collectPacemakerValAndLabels(ctx, params)
	for label := range pruneLabels {
		delete(l, label)
	}
	// Add OS command metrics to the labels.
	for _, m := range params.WorkloadConfig.GetValidationPacemaker().GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(m, params.Execute, params.osVendorID)
		if k != "" {
			l[k] = v
		}
	}

	return WorkloadMetrics{Metrics: createTimeSeries(pacemakerAPILabel, l, pacemakerVal, params.Config)}
}

// CollectPacemakerMetrics collects the pacemaker configuration and runtime metrics from the OS.
func CollectPacemakerMetrics(ctx context.Context, params Parameters, wm chan<- WorkloadMetrics) {
	log.Logger.Info("Collecting workload pacemaker metrics...")
	t := pacemakerAPILabel
	pacemakerVal, l := collectPacemakerValAndLabels(ctx, params)
	wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, pacemakerVal, params.Config)}
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
	pacemakerXMLString := pacemaker.XMLString(params.Execute, crmAvailable)

	if pacemakerXMLString == nil {
		log.Logger.Debug("No pacemaker xml")
		return 0.0, l
	}
	pacemakerDocument, err := ParseXML([]byte(*pacemakerXMLString))

	if err != nil {
		log.Logger.Debugw("Could not parse the pacemaker configuration xml", "xml", *pacemakerXMLString, "error", err)
		return 0.0, l
	}

	primitives := pacemakerDocument.Configuration.Resources.Primitives
	results := setPacemakerPrimitives(l, primitives, params.Config)

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

	setPacemakerAPIAccess(l, projectID, bearerToken, params.Execute)
	setPacemakerMaintenanceMode(l, crmAvailable, params.Execute)

	return 1.0, l
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

func setPacemakerAPIAccess(l map[string]string, projectID string, bearerToken string, exec commandlineexecutor.Execute) {
	fenceAgentComputeAPIAccess, err := checkAPIAccess(exec,
		"-H",
		fmt.Sprintf("Authorization: Bearer %s ", bearerToken),
		fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s?fields=id", projectID))
	if err != nil {
		log.Logger.Debugw("Could not obtain fence agent compute API Access", log.Error(err))
	}

	fenceAgentLoggingAPIAccess, err := checkAPIAccess(exec,
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

func checkAPIAccess(exec commandlineexecutor.Execute, args ...string) (bool, error) {
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

	result := exec(commandlineexecutor.Params{
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
func setPacemakerMaintenanceMode(l map[string]string, crmAvailable bool, exec commandlineexecutor.Execute) {
	result := commandlineexecutor.Result{}
	if crmAvailable {
		result = exec(commandlineexecutor.Params{
			Executable:  "sh",
			ArgsToSplit: "-c 'crm configure show | grep maintenance | grep true'",
		})
	} else {
		result = exec(commandlineexecutor.Params{
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

func setPacemakerPrimitives(l map[string]string, primitives []PrimitiveClass, c *cnfpb.Configuration) map[string]string {
	returnMap := map[string]string{}
	serviceAccountJSONFile := ""
	properties := c.GetCloudProperties()

	for _, primitive := range primitives {
		idNode := primitive.ID
		classNode := primitive.Class
		typeNode := primitive.ClassType

		attribute := primitive.InstanceAttributes
		if typeNode != "fence_gce" && !strings.HasSuffix(attribute.ID, properties.GetInstanceName()+"-instance_attributes") {
			continue
		}
		serviceAccountJSONFile = iteratePrimitiveChild(l, attribute, classNode, typeNode, idNode, returnMap, properties.GetInstanceName())
		if serviceAccountJSONFile != "" {
			break
		}
	}
	returnMap["serviceAccountJsonFile"] = serviceAccountJSONFile
	return returnMap
}

func iteratePrimitiveChild(l map[string]string, attribute ClusterPropertySet, classNode string, typeNode string, idNode string, returnMap map[string]string, instanceName string) string {
	attributeChildren := attribute.NVPairs
	fenceAttributes := map[string]string{}
	portMatchesInstanceName := false
	serviceAccountPath := ""
	fenceKeys := map[string]struct{}{
		"pcmk_delay_max":       {},
		"pcmk_delay_base":      {},
		"pcmk_reboot_timeout":  {},
		"pcmk_monitor_retries": {},
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
	jsonData, err := ioutil.ReadAll(jsonStream)
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
