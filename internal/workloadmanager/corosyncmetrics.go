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
	"bufio"
	"strings"

	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
)

const csConfigPath = "/etc/corosync/corosync.conf"

/*
CollectCorosyncMetrics collects Corosync metrics for Workload Manager sends them to the wm
channel
*/
func CollectCorosyncMetrics(params Parameters, wm chan<- WorkloadMetrics, csConfig string) {
	t := "workload.googleapis.com/sap/validation/corosync"
	l := map[string]string{}
	if params.OSType == "windows" {
		wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, 0, params.Config)}
		return
	}

	log.Logger.Info("Collecting workload corosync metrics...")

	for k, v := range readCorosyncConfig(params.ConfigFileReader, csConfig) {
		l[k] = v
	}
	for k, v := range readCorosyncRuntime(params.CommandRunnerNoSpace) {
		l[k] = v
	}
	wm <- WorkloadMetrics{Metrics: createTimeSeries(t, l, 1, params.Config)}
}

/*
readCorosyncConfig loads a Corosync configuration file and processes it into a configuration map
*/
func readCorosyncConfig(reader ConfigFileReader, csConfig string) map[string]string {
	config := createCorosyncConfigMap()
	file, err := reader(csConfig)

	if err != nil {
		log.Logger.Debugw("Could not read the corosync config file", log.Error(err))
		return config
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		setConfigMapValueForLine(config, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Logger.Warnw("Could not read the corosync config from /etc/corosync/corosync.conf", log.Error(err))
	}

	return config
}

/*
readCorosyncRuntime loads Corosync runtime configuration data and converts it to a configuration
map
*/
func readCorosyncRuntime(runner commandlineexecutor.CommandRunnerNoSpace) map[string]string {
	config := map[string]string{}

	runtimeKeys := []string{
		"totem.token",
		"totem.token_retransmits_before_loss_const",
		"totem.consensus",
		"totem.join",
		"totem.max_messages",
		"totem.transport",
		"totem.fail_recv_const",
		"quorum.two_node",
	}

	for _, key := range runtimeKeys {
		result, _, err := runner("corosync-cmapctl", "-g", key)
		value := ""
		if err == nil {
			value = strings.Trim(result, " ")
		}
		if strings.HasPrefix(value, "Can't get key") {
			value = ""
		}
		key = strings.Replace(key, "totem.", "", -1)
		key = strings.Replace(key, "quorum.", "", -1) + "_runtime"
		if value != "" {
			arr := strings.Fields(value)
			value = arr[len(arr)-1]
		}
		config[key] = value
	}

	return config
}

/*
setConfigMapValueForLine processes a single line from a Corosync configuration file and converts it
to a single entry in a configuration map
*/
func setConfigMapValueForLine(config map[string]string, line string) {
	line = strings.TrimSpace(line)
	for key := range config {
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		value := ""
		arr := strings.Fields(line)
		if len(arr) > 1 {
			value = strings.Trim(arr[1], " ")
		}
		config[key] = value
		break
	}
}

func createCorosyncConfigMap() map[string]string {
	config := map[string]string{
		"token":                               "",
		"token_retransmits_before_loss_const": "",
		"consensus":                           "",
		"join":                                "",
		"max_messages":                        "",
		"transport":                           "",
		"fail_recv_const":                     "",
		"two_node":                            "",
	}
	return config
}
