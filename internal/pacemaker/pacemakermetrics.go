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

package pacemaker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/configurablemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/osinfo"

	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	sapInstanceRegex = regexp.MustCompile("Started ([A-Za-z0-9-]+)")
	sapstartsrvRegex = regexp.MustCompile("sapstartsrv pf=(?:/sapmnt|/usr/sap)/([A-Z][A-Z0-9]{2})/[/a-zA-Z0-9]*profile/")
)

type (
	/*ConfigFileReader abstracts loading and reading files into an io.ReadCloser object. ConfigFileReader Example usage:

	ConfigFileReader(func(path string) (io.ReadCloser, error) {
			file, err := os.Open(path)
			var f io.ReadCloser = file
			return f, err
		})
	*/
	ConfigFileReader func(string) (io.ReadCloser, error)

	// DefaultTokenGetter obtains a "default" oauth2 token source within the getDefaultBearerToken function.
	DefaultTokenGetter func(context.Context, ...string) (oauth2.TokenSource, error)

	// JSONCredentialsGetter obtains a JSON oauth2 google credentials within the getJSONBearerToken function.
	JSONCredentialsGetter func(context.Context, []byte, ...string) (*google.Credentials, error)

	// Parameters holds the parameters required for pacemaker metrics collection.
	Parameters struct {
		Config                *cpb.Configuration
		WorkloadConfig        *wlmpb.WorkloadValidation
		ConfigFileReader      ConfigFileReader
		Execute               commandlineexecutor.Execute
		Exists                commandlineexecutor.Exists
		DefaultTokenGetter    DefaultTokenGetter
		JSONCredentialsGetter JSONCredentialsGetter
		OSVendorID            string
	}
)

// CollectPacemakerMetrics collects the pacemaker metrics as specified by the WorkloadValidation config.
func CollectPacemakerMetrics(ctx context.Context, params Parameters) (float64, map[string]string) {
	if params.OSVendorID == "" {
		var err error
		osData, err := osinfo.ReadData(ctx, osinfo.FileReadCloser(params.ConfigFileReader), osinfo.OSName, osinfo.OSReleaseFilePath)
		if err != nil {
			log.CtxLogger(ctx).Debugw(fmt.Sprintf("Could not read OS release info from %s", osinfo.OSReleaseFilePath), "error", err)
		}
		params.OSVendorID = osData.OSVendor
	}

	// Prune the configurable labels depending on what is defined in the workload config.
	pruneLabels := map[string]bool{
		"pcmk_delay_base":                    true,
		"pcmk_delay_max":                     true,
		"pcmk_monitor_retries":               true,
		"pcmk_reboot_timeout":                true,
		"location_preference_set":            true,
		"migration_threshold":                true,
		"resource_stickiness":                true,
		"saphana_start_timeout":              true,
		"saphana_stop_timeout":               true,
		"saphana_promote_timeout":            true,
		"saphana_demote_timeout":             true,
		"saphana_primary_monitor_interval":   true,
		"saphana_primary_monitor_timeout":    true,
		"saphana_secondary_monitor_interval": true,
		"saphana_secondary_monitor_timeout":  true,
		"fence_agent":                        true,
		"fence_agent_compute_api_access":     true,
		"fence_agent_logging_api_access":     true,
		"maintenance_mode_active":            true,
		"saphanatopology_monitor_interval":   true,
		"saphanatopology_monitor_timeout":    true,
		"saphanatopology_start_timeout":      true,
		"saphanatopology_stop_timeout":       true,
		"ascs_instance":                      true,
		"ers_instance":                       true,
		"enqueue_server":                     true,
		"ascs_automatic_recover":             true,
		"ascs_failure_timeout":               true,
		"ascs_migration_threshold":           true,
		"ascs_resource_stickiness":           true,
		"ascs_monitor_interval":              true,
		"ascs_monitor_timeout":               true,
		"ensa2_capable":                      true,
		"ers_automatic_recover":              true,
		"is_ers":                             true,
		"ers_monitor_interval":               true,
		"ers_monitor_timeout":                true,
		"op_timeout":                         true,
		"stonith_enabled":                    true,
		"stonith_timeout":                    true,
		"saphana_automated_register":         true,
		"saphana_duplicate_primary_timeout":  true,
		"saphana_prefer_site_takeover":       true,
		"saphana_notify":                     true,
		"saphana_clone_max":                  true,
		"saphana_clone_node_max":             true,
		"saphana_interleave":                 true,
		"saphanatopology_clone_node_max":     true,
		"saphanatopology_interleave":         true,
		"healthcheck_monitor_interval":       true,
		"healthcheck_monitor_timeout":        true,
		"ilb_monitor_interval":               true,
		"ilb_monitor_timeout":                true,
		"ascs_healthcheck_monitor_interval":  true,
		"ascs_healthcheck_monitor_timeout":   true,
		"ascs_ilb_monitor_interval":          true,
		"ascs_ilb_monitor_timeout":           true,
		"ers_healthcheck_monitor_interval":   true,
		"ers_healthcheck_monitor_timeout":    true,
		"ers_ilb_monitor_interval":           true,
		"ers_ilb_monitor_timeout":            true,
		"has_alias_ip":                       true,
		"cluster_healthy":                    true,
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
	for _, m := range pconfig.GetAscsMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}
	for _, m := range pconfig.GetOpOptionMetrics() {
		delete(pruneLabels, m.GetMetricInfo().GetLabel())
	}

	pacemakerVal, labels := collectPacemakerValAndLabels(ctx, params)
	for label := range pruneLabels {
		delete(labels, label)
	}

	// Add OS command metrics to the labels.
	for _, m := range pacemaker.GetOsCommandMetrics() {
		k, v := configurablemetrics.CollectOSCommandMetric(ctx, m, params.Execute, params.OSVendorID)
		if k != "" {
			labels[k] = v
		}
	}
	return pacemakerVal, labels
}

// collectPacemakerValAndLabels collects the pacemaker metrics and labels as specified by the WorkloadValidation config.
func collectPacemakerValAndLabels(ctx context.Context, params Parameters) (float64, map[string]string) {
	labels := map[string]string{}

	if params.Config.GetCloudProperties() == nil {
		log.CtxLogger(ctx).Debug("No cloud properties")
		return 0.0, labels
	}
	properties := params.Config.GetCloudProperties()
	projectID := properties.GetProjectId()
	crmAvailable := params.Exists("crm")
	pacemakerXMLString := XMLString(ctx, params.Execute, crmAvailable)

	if pacemakerXMLString == nil {
		log.CtxLogger(ctx).Debug("No pacemaker xml")
		return 0.0, labels
	}
	pacemakerDocument, err := ParseXML([]byte(*pacemakerXMLString))

	if err != nil {
		log.CtxLogger(ctx).Debugw("Could not parse the pacemaker configuration xml", "xml", *pacemakerXMLString, "error", err)
		return 0.0, labels
	}

	instances := clusterNodes(pacemakerDocument.Configuration.Nodes)
	// Sort VM instance names by length, descending.
	// This should prevent collisions when searching within a substring.
	// Ex: instance11, ... , instance1
	sort.Slice(instances, func(i, j int) bool { return len(instances[i]) > len(instances[j]) })

	results := setPacemakerPrimitives(ctx, labels, pacemakerDocument.Configuration.Resources, instances, params.Config)

	if id, ok := results["projectId"]; ok {
		projectID = id
	}

	bearerToken, err := getBearerToken(ctx, results["serviceAccountJsonFile"], params.ConfigFileReader,
		params.JSONCredentialsGetter, params.DefaultTokenGetter)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Could not parse the pacemaker configuration xml", "xml", *pacemakerXMLString, "error", err)
		return 0.0, labels
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
	labels["location_preference_set"] = locationPreferenceSet

	rscOptionNvPairs := pacemakerDocument.Configuration.RSCDefaults.NVPairs
	setLabelsForRSCNVPairs(labels, rscOptionNvPairs, "migration-threshold")
	setLabelsForRSCNVPairs(labels, rscOptionNvPairs, "resource-stickiness")

	// Aggregate resources specified as <clone> or <master> into a single slice.
	// This is necessary because of differences in the Pacemaker XML config
	// between RHEL and SLES.
	var cloneResources []Clone
	cloneResources = append(cloneResources, pacemakerDocument.Configuration.Resources.Clone...)
	cloneResources = append(cloneResources, pacemakerDocument.Configuration.Resources.Master)

	var clonePrimitives []PrimitiveClass
	for _, cloneResource := range cloneResources {
		clonePrimitives = append(clonePrimitives, cloneResource.Primitives...)
	}

	// This will get metrics for the <primitive> with type=SAPHana.
	setPacemakerHanaOperations(labels, filterPrimitiveOpsByType(clonePrimitives, "SAPHana"))
	setPacemakerHANACloneAttrs(labels, cloneResources)

	// This will get metrics for the <primitive> with type=SAPHanaTopology.
	pacemakerHanaTopology(labels, filterPrimitiveOpsByType(clonePrimitives, "SAPHanaTopology"))
	setPacemakerHANATopologyCloneAttrs(labels, cloneResources)

	setPacemakerAPIAccess(ctx, labels, projectID, bearerToken, params.Execute)
	setPacemakerMaintenanceMode(ctx, labels, crmAvailable, params.Execute)
	setPacemakerClusterHealthy(ctx, labels, params.Execute)

	setPacemakerStonithClusterProperty(labels, pacemakerDocument.Configuration.CRMConfig.ClusterPropertySets)

	collectASCSInstance(ctx, labels, params.Exists, params.Execute)
	collectEnqueueServer(ctx, labels, params.Execute)
	setASCSConfigMetrics(ctx, labels, pacemakerDocument.Configuration.Resources.Groups, params.Execute)
	setERSConfigMetrics(ctx, labels, pacemakerDocument.Configuration.Resources.Groups)

	// sets the OP options from the pacemaker configuration.
	setOPOptions(labels, pacemakerDocument.Configuration.OPDefaults)

	setHealthCheckInternalLoadBalancerMetrics(labels, pacemakerDocument.Configuration.Resources.Groups)

	return 1.0, labels
}

// clusterNodes returns a list of VM instance names from a list of CIBNode objects.
func clusterNodes(nodes []CIBNode) []string {
	instances := make([]string, 0, len(nodes))
	for _, node := range nodes {
		instances = append(instances, node.Uname)
	}
	return instances
}

// filterPrimitiveOpsByType returns a list of Op objects from a list of PrimitiveClass objects
// that match the given primitive type.
func filterPrimitiveOpsByType(primitives []PrimitiveClass, primitiveType string) []Op {
	ops := []Op{}
	for _, primitive := range primitives {
		if primitive.ClassType == primitiveType {
			ops = append(ops, primitive.Operations...)
		}
	}
	return ops
}

// setPacemakerHanaOperations sets the pacemaker hana operations labels for the metric validation
// collector.
func setPacemakerHanaOperations(labels map[string]string, sapHanaOperations []Op) {
	for _, sapHanaOperation := range sapHanaOperations {
		name := sapHanaOperation.Name
		switch name {
		case "start":
			labels["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "stop":
			labels["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "promote":
			labels["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "demote":
			labels["saphana_"+name+"_timeout"] = sapHanaOperation.Timeout
		case "monitor":
			switch sapHanaOperation.Role {
			case "Master":
				labels["saphana_primary_monitor_interval"] = sapHanaOperation.Interval
				labels["saphana_primary_monitor_timeout"] = sapHanaOperation.Timeout
			case "Slave":
				labels["saphana_secondary_monitor_interval"] = sapHanaOperation.Interval
				labels["saphana_secondary_monitor_timeout"] = sapHanaOperation.Timeout
			}
		default:
			// fall through
		}
	}
}

// setPacemakerAPIAccess sets the pacemaker fence agent API access labels for the metric validation
// collector.
func setPacemakerAPIAccess(ctx context.Context, labels map[string]string, projectID string, bearerToken string, exec commandlineexecutor.Execute) {
	fenceAgentComputeAPIAccess, err := checkAPIAccess(ctx, exec,
		"-H",
		fmt.Sprintf("Authorization: Bearer %s ", bearerToken),
		fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s?fields=id", projectID))
	if err != nil {
		log.CtxLogger(ctx).Debugw("Could not obtain fence agent compute API Access", log.Error(err))
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
		log.CtxLogger(ctx).Debugw("Could not obtain fence agent logging API Access", log.Error(err))
	}
	labels["fence_agent_compute_api_access"] = strconv.FormatBool(fenceAgentComputeAPIAccess)
	labels["fence_agent_logging_api_access"] = strconv.FormatBool(fenceAgentLoggingAPIAccess)
}

// checkAPIAccess checks if the given API endpoint is accessible.
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

// setPacemakerMaintenanceMode defines the pacemaker maintenance mode label for the metric validation
// collector.
func setPacemakerMaintenanceMode(ctx context.Context, labels map[string]string, crmAvailable bool, exec commandlineexecutor.Execute) {
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
	log.CtxLogger(ctx).Debugw("Pacemaker maintenance mode", "maintenanceMode", result.StdOut, "stderr", result.StdErr, "err", result.Error)
	maintenanceModeLabel := "false"
	if result.Error == nil && result.StdOut != "" {
		maintenanceModeLabel = "true"
	}
	labels["maintenance_mode_active"] = maintenanceModeLabel
}

func setPacemakerClusterHealthy(ctx context.Context, labels map[string]string, exec commandlineexecutor.Execute) {
	labels["cluster_healthy"] = "command_failed"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "crm_mon",
		ArgsToSplit: "-1 -r -f",
	})
	log.CtxLogger(ctx).Debugw("Pacemaker cluster_healthy check crm_mon output", "stdout", result.StdOut, "stderr", result.StdErr, "err", result.Error)
	if result.Error != nil {
		return
	}

	// If the unhealthyRegex is matched after 'Node List', before 'Failed Fencing Actions', then the
	// cluster is unhealthy.
	startProcessing := false
	healthyStatus := true
	unhealthyRegex := regexp.MustCompile(`\b(offline|unclean|stopped|failed)\b`)

	for _, line := range strings.Split(result.StdOut, "\n") {
		if strings.Contains(line, "Node List") {
			startProcessing = true
			continue
		}
		if strings.Contains(line, "Failed Fencing Actions") {
			break
		}
		if startProcessing && unhealthyRegex.MatchString(strings.ToLower(line)) {
			healthyStatus = false
			break
		}
	}
	log.CtxLogger(ctx).Debugw("Pacemaker cluster_healthy check string match output", "cluster_healthy", healthyStatus, "stderr", result.StdErr, "err", result.Error)
	labels["cluster_healthy"] = strconv.FormatBool(healthyStatus)
}

// setLabelsForRscNvPairs converts a list of pacemaker name/value XML nodes to metric labels if they
// match a specific name.
func setLabelsForRSCNVPairs(labels map[string]string, rscOptionNvPairs []NVPair, nameToFind string) {
	for _, rscOptionNvPair := range rscOptionNvPairs {
		if rscOptionNvPair.Name == nameToFind {
			labels[strings.ReplaceAll(nameToFind, "-", "_")] = rscOptionNvPair.Value
		}
	}
}

// setPacemakerPrimitives sets the pacemaker primitives labels for the metric validation collector.
func setPacemakerPrimitives(ctx context.Context, labels map[string]string, resources Resources, instances []string, c *cpb.Configuration) map[string]string {
	primitives := resources.Primitives
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
		serviceAccountJSONFile = iteratePrimitiveChild(labels, attribute, classNode, typeNode, idNode, returnMap, properties.GetInstanceName())
		if serviceAccountJSONFile != "" {
			break
		}
	}
	if len(pcmkDelayMax) > 0 {
		labels["pcmk_delay_max"] = strings.Join(pcmkDelayMax, ",")
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

// iteratePrimitiveChild iterates through the primitive child nodes and sets the pacemaker primitives labels for the metric validation collector.
func iteratePrimitiveChild(labels map[string]string, attribute ClusterPropertySet, classNode string, typeNode string, idNode string, returnMap map[string]string, instanceName string) string {
	attributeChildren := attribute.NVPairs
	fenceAttributes := map[string]string{
		"pcmk_delay_base":      "",
		"pcmk_reboot_timeout":  "",
		"pcmk_monitor_retries": "",
	}
	portMatchesInstanceName := false
	serviceAccountPath := ""

	for _, nvPair := range attributeChildren {
		if _, ok := fenceAttributes[nvPair.Name]; ok {
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
			labels[k] = v
		}
		if classNode == "stonith" {
			labels["fence_agent"] = strings.ReplaceAll(typeNode, "external/", "")
		}
	}

	return serviceAccountPath
}

// pacemakerHanaTopology sets the pacemaker hana topology labels for the metric validation collector.
func pacemakerHanaTopology(labels map[string]string, sapHanaOperations []Op) {
	for _, sapHanaOperation := range sapHanaOperations {
		switch sapHanaOperation.Name {
		case "monitor":
			labels["saphanatopology_monitor_interval"] = sapHanaOperation.Interval
			labels["saphanatopology_monitor_timeout"] = sapHanaOperation.Timeout
		case "start":
			labels["saphanatopology_start_timeout"] = sapHanaOperation.Timeout
		case "stop":
			labels["saphanatopology_stop_timeout"] = sapHanaOperation.Timeout
		}
	}
}

// getDefaultBearerToken obtains a "default" oauth2 token source within the getDefaultBearerToken function.
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

// getJSONBearerToken obtains a JSON oauth2 google credentials within the getJSONBearerToken function.
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

// getBearerToken returns a bearer token for the given service account JSON file.
// If the service account JSON file is empty, it will return a default bearer token.
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

// collectASCSInstance determines the VM instances serving as the ASCS/ERS resource group.
func collectASCSInstance(ctx context.Context, labels map[string]string, exists commandlineexecutor.Exists, exec commandlineexecutor.Execute) {
	labels["ascs_instance"] = ""
	labels["ers_instance"] = ""

	var command string
	switch {
	case exists("crm"):
		command = "crm"
	case exists("pcs"):
		command = "pcs"
	default:
		log.CtxLogger(ctx).Debug("Could not determine Pacemaker command-line tool. Skipping ascs_instance metric collection.")
		return
	}

	result := exec(ctx, commandlineexecutor.Params{
		Executable: command,
		Args:       []string{"status"},
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw(fmt.Sprintf("Failed to get %s status. Skipping ascs_instance metric collection.", command), "error", result.Error)
	}

	lines := strings.Split(result.StdOut, "\n")
	inASCSResourceGroup, inERSResourceGroup := false, false
	for _, line := range lines {
		switch {
		case strings.Contains(line, "Resource Group: ascs"):
			inASCSResourceGroup = true
			inERSResourceGroup = false
		case strings.Contains(line, "Resource Group: ers"):
			inERSResourceGroup = true
			inASCSResourceGroup = false
		case strings.Contains(line, "Resource Group:"):
			inASCSResourceGroup = false
			inERSResourceGroup = false
		case inASCSResourceGroup && strings.Contains(line, "ocf::heartbeat:SAPInstance"):
			match := sapInstanceRegex.FindStringSubmatch(line)
			if len(match) != 2 {
				log.CtxLogger(ctx).Debugw(fmt.Sprintf("Unexpected output from %s status: could not parse ASCS instance name.", command), "line", line)
				continue
			}
			labels["ascs_instance"] = match[1]
		case inERSResourceGroup && strings.Contains(line, "ocf::heartbeat:SAPInstance"):
			match := sapInstanceRegex.FindStringSubmatch(line)
			if len(match) != 2 {
				log.CtxLogger(ctx).Debugw(fmt.Sprintf("Unexpected output from %s status: could not parse ERS instance name.", command), "line", line)
				continue
			}
			labels["ers_instance"] = match[1]
		}
	}
}

// collectEnqueueServer determines the enqueue server (ENSA or ENSA2) in use by the Pacemaker cluster.
func collectEnqueueServer(ctx context.Context, labels map[string]string, exec commandlineexecutor.Execute) {
	labels["enqueue_server"] = ""

	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: "'pf=' /usr/sap/sapservices",
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Could not grep /usr/sap/sapservices. Skipping enqueue_server metric collection.", "error", result.Error)
		return
	}

	var sapsid string
	lines := strings.Split(strings.TrimSuffix(result.StdOut, "\n"), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		match := sapstartsrvRegex.FindStringSubmatch(line)
		if len(match) == 2 {
			sapsid = match[1]
			break
		}
	}
	if sapsid == "" {
		log.CtxLogger(ctx).Debug("Could not determine SAP SID from /usr/sap/sapservices. Skipping enqueue_server metric collection.")
		return
	}

	result = exec(ctx, commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: fmt.Sprintf("'^enq/replicator' /sapmnt/%s/profile/DEFAULT.PFL", sapsid),
	})
	switch {
	case result.Error == nil && result.ExitCode == 0: // Pattern found
		labels["enqueue_server"] = "ENSA2"
	case result.ExitCode == 1: // Pattern not found
		labels["enqueue_server"] = "ENSA1"
	default: // File not found
		log.CtxLogger(ctx).Debugw("Could not grep DEFAULT.PFL. Skipping enqueue_server metric collection.", "error", result.Error)
	}
}

// setASCSMetrics sets the metrics collected from the ASCS resource group.
func setASCSConfigMetrics(ctx context.Context, labels map[string]string, groups []Group, exec commandlineexecutor.Execute) {
	labels["ascs_automatic_recover"] = ""
	labels["ascs_failure_timeout"] = ""
	labels["ascs_migration_threshold"] = ""
	labels["ascs_resource_stickiness"] = ""
	labels["ascs_monitor_interval"] = ""
	labels["ascs_monitor_timeout"] = ""
	labels["ensa2_capable"] = ""
	metaAttributesKeys := map[string]string{
		"failure-timeout":     "ascs_failure_timeout",
		"migration-threshold": "ascs_migration_threshold",
		"resource-stickiness": "ascs_resource_stickiness",
	}

	// Identify the resource group containing the ASCS resource.
	var ascsGroup *Group
ASCSLoop:
	for _, group := range groups {
		for _, primitive := range group.Primitives {
			if primitive.ClassType != "SAPInstance" {
				continue
			}
			for _, nvPair := range primitive.InstanceAttributes.NVPairs {
				if nvPair.Name == "START_PROFILE" && strings.Contains(nvPair.Value, "SCS") {
					ascsGroup = &group
					break ASCSLoop
				}
			}
		}
	}

	if ascsGroup == nil {
		log.CtxLogger(ctx).Debug("Could not find ASCS resource group. Skipping ASCS metric collection.")
		return
	}

	var ascsProfile string
	for _, primitive := range ascsGroup.Primitives {
		if primitive.ClassType != "SAPInstance" {
			continue
		}
		for _, nvPair := range primitive.InstanceAttributes.NVPairs {
			switch nvPair.Name {
			case "AUTOMATIC_RECOVER":
				labels["ascs_automatic_recover"] = nvPair.Value
			case "START_PROFILE":
				ascsProfile = nvPair.Value
			}
		}
		for _, nvPair := range primitive.MetaAttributes.NVPairs {
			if key, ok := metaAttributesKeys[nvPair.Name]; ok {
				labels[key] = nvPair.Value
			}
		}
		for _, op := range primitive.Operations {
			if op.Name == "monitor" {
				labels["ascs_monitor_interval"] = op.Interval
				labels["ascs_monitor_timeout"] = op.Timeout
			}
		}
	}

	if ascsProfile == "" {
		return
	}

	labels["ensa2_capable"] = "false"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "grep",
		ArgsToSplit: fmt.Sprintf("'^_ENQ' %s", ascsProfile),
	})
	switch {
	case result.Error != nil && !result.ExitStatusParsed:
		log.CtxLogger(ctx).Debugw("Could not grep ASCS profile.", "error", result.Error, "start_profile", ascsProfile)
	case result.ExitCode == 0:
		labels["ensa2_capable"] = "true"
	}
}

// setERSConfigMetrics sets the metrics collected from the ERS resource group.
func setERSConfigMetrics(ctx context.Context, labels map[string]string, groups []Group) {
	labels["ers_automatic_recover"] = ""
	labels["is_ers"] = ""
	labels["ers_monitor_interval"] = ""
	labels["ers_monitor_timeout"] = ""
	instanceAttributes := map[string]string{
		"AUTOMATIC_RECOVER": "ers_automatic_recover",
		"IS_ERS":            "is_ers",
	}

	// Identify the resource group containing the ERS resource.
	var ersGroup *Group
ERSLoop:
	for _, group := range groups {
		for _, primitive := range group.Primitives {
			if primitive.ClassType != "SAPInstance" {
				continue
			}
			for _, nvPair := range primitive.InstanceAttributes.NVPairs {
				if nvPair.Name == "START_PROFILE" && strings.Contains(nvPair.Value, "ERS") {
					ersGroup = &group
					break ERSLoop
				}
			}
		}
	}

	if ersGroup == nil {
		log.CtxLogger(ctx).Debug("Could not find ERS resource group. Skipping ERS metric collection.")
		return
	}

	for _, primitive := range ersGroup.Primitives {
		if primitive.ClassType != "SAPInstance" {
			continue
		}
		for _, nvPair := range primitive.InstanceAttributes.NVPairs {
			if key, ok := instanceAttributes[nvPair.Name]; ok {
				labels[key] = nvPair.Value
			}
		}
		for _, op := range primitive.Operations {
			if op.Name == "monitor" {
				labels["ers_monitor_interval"] = op.Interval
				labels["ers_monitor_timeout"] = op.Timeout
			}
		}
	}
}

func setOPOptions(labels map[string]string, opOptions ClusterPropertySet) {
	labels["op_timeout"] = ""
	opOptionsKeys := map[string]bool{
		"timeout": true,
	}

	for _, nvPair := range opOptions.NVPairs {
		if _, ok := opOptionsKeys[nvPair.Name]; ok {
			key := "op_" + strings.ReplaceAll(nvPair.Name, "-", "_")
			labels[key] = strings.ReplaceAll(nvPair.Value, "s", "")
		}
	}
}

func setPacemakerStonithClusterProperty(labels map[string]string, cps []ClusterPropertySet) {
	labels["stonith_enabled"] = ""
	labels["stonith_timeout"] = ""
	stonithClusterPropertyKeys := map[string]bool{
		"stonith-enabled": true,
		"stonith-timeout": true,
	}
	for _, cp := range cps {
		if cp.ID == "cib-bootstrap-options" {
			for _, nvPair := range cp.NVPairs {
				if _, ok := stonithClusterPropertyKeys[nvPair.Name]; ok {
					key := strings.ReplaceAll(nvPair.Name, "-", "_")
					labels[key] = nvPair.Value
					if strings.HasSuffix(key, "timeout") {
						labels[key] = strings.ReplaceAll(nvPair.Value, "s", "")
					}
				}
			}
			return
		}
	}
}

func setPacemakerHANACloneAttrs(labels map[string]string, cloneResources []Clone) {
	labels["saphana_automated_register"] = ""
	labels["saphana_duplicate_primary_timeout"] = ""
	labels["saphana_prefer_site_takeover"] = ""
	labels["saphana_notify"] = ""
	labels["saphana_clone_max"] = ""
	labels["saphana_clone_node_max"] = ""
	labels["saphana_interleave"] = ""
	instanceAttributeKeys := map[string]bool{
		"AUTOMATED_REGISTER":        true,
		"DUPLICATE_PRIMARY_TIMEOUT": true,
		"PREFER_SITE_TAKEOVER":      true,
	}
	metaAttributeKeys := map[string]bool{
		"notify":         true,
		"clone-max":      true,
		"clone-node-max": true,
		"interleave":     true,
	}

	var instanceAttrs []NVPair
	var metaAttrs []NVPair
	for _, clone := range cloneResources {
		for _, p := range clone.Primitives {
			if p.ClassType == "SAPHana" {
				instanceAttrs = p.InstanceAttributes.NVPairs
				// For RHEL, the meta attributes exist within the <primitive> tag.
				// For SLES, the meta attributes exist at the same level as the <primitive> tag.
				// For simplicity, just combine all attributes into a single slice.
				metaAttrs = append(metaAttrs, clone.Attributes.NVPairs...)
				metaAttrs = append(metaAttrs, p.MetaAttributes.NVPairs...)
				break
			}
		}
	}

	for _, nvPair := range instanceAttrs {
		if _, ok := instanceAttributeKeys[nvPair.Name]; ok {
			key := "saphana_" + strings.ToLower(nvPair.Name)
			labels[key] = nvPair.Value
		}
	}
	for _, nvPair := range metaAttrs {
		if _, ok := metaAttributeKeys[nvPair.Name]; ok {
			key := "saphana_" + strings.ReplaceAll(nvPair.Name, "-", "_")
			labels[key] = nvPair.Value
		}
	}
}

func setPacemakerHANATopologyCloneAttrs(labels map[string]string, cloneResources []Clone) {
	labels["saphanatopology_clone_node_max"] = ""
	labels["saphanatopology_interleave"] = ""
	keys := map[string]bool{
		"clone-node-max": true,
		"interleave":     true,
	}

	var metaAttrs []NVPair
	for _, clone := range cloneResources {
		for _, p := range clone.Primitives {
			if p.ClassType == "SAPHanaTopology" {
				// For simplicity, combine all meta attributes into a single slice.
				metaAttrs = append(metaAttrs, clone.Attributes.NVPairs...)
				metaAttrs = append(metaAttrs, p.MetaAttributes.NVPairs...)
				break
			}
		}
	}

	for _, nvPair := range metaAttrs {
		if _, ok := keys[nvPair.Name]; ok {
			key := "saphanatopology_" + strings.ReplaceAll(nvPair.Name, "-", "_")
			labels[key] = nvPair.Value
		}
	}
}

func setHealthCheckInternalLoadBalancerMetrics(labels map[string]string, groups []Group) {
	labels["healthcheck_monitor_interval"] = ""
	labels["healthcheck_monitor_timeout"] = ""
	labels["ilb_monitor_interval"] = ""
	labels["ilb_monitor_timeout"] = ""
	labels["ascs_healthcheck_monitor_interval"] = ""
	labels["ascs_healthcheck_monitor_timeout"] = ""
	labels["ascs_ilb_monitor_interval"] = ""
	labels["ascs_ilb_monitor_timeout"] = ""
	labels["ers_healthcheck_monitor_interval"] = ""
	labels["ers_healthcheck_monitor_timeout"] = ""
	labels["ers_ilb_monitor_interval"] = ""
	labels["ers_ilb_monitor_timeout"] = ""
	labels["has_alias_ip"] = "false"

	for _, group := range groups {
		var settings = make(map[string]string)
		var prefix string
		for _, primitive := range group.Primitives {
			switch primitive.ClassType {
			case "anything", "haproxy":
				// Health Check settings
				for _, op := range primitive.Operations {
					if op.Name == "monitor" {
						settings["healthcheck_monitor_interval"] = op.Interval
						settings["healthcheck_monitor_timeout"] = op.Timeout
					}
				}
			case "IPaddr2":
				// Internal Load Balancer settings
				for _, op := range primitive.Operations {
					if op.Name == "monitor" {
						settings["ilb_monitor_interval"] = op.Interval
						settings["ilb_monitor_timeout"] = op.Timeout
					}
				}
				if primitive.Provider == "gcp" {
					for _, nvPair := range primitive.InstanceAttributes.NVPairs {
						if nvPair.Name == "type" && nvPair.Value == "alias" {
							labels["has_alias_ip"] = "true"
						}
					}
				}
			case "SAPInstance":
				// If this primitive is present, determine if the resource group is ASCS or ERS.
				for _, nvPair := range primitive.InstanceAttributes.NVPairs {
					if nvPair.Name == "START_PROFILE" {
						switch {
						case strings.Contains(nvPair.Value, "SCS"):
							prefix = "ascs_"
						case strings.Contains(nvPair.Value, "ERS"):
							prefix = "ers_"
						}
					}
				}
			}
		}
		for k, v := range settings {
			labels[prefix+k] = strings.ReplaceAll(v, "s", "")
		}
	}
}
