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
	"fmt"
	"strings"
	"sync"

	monitoringresourcespb "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
)

const agentBinary = "/usr/bin/google_cloud_sap_agent"
const remoteAgentBinary = "/tmp/google_cloud_sap_agent"

// CollectMetricsToJSON will collect all of the workload manager metrics and return the
// JSON representation of them, this is only called on remote instances for metric collection
// only called through the google_cloud_sap_agent binary using remote mode
func CollectMetricsToJSON(ctx context.Context, params Parameters) string {
	wm := collectMetrics(ctx, params, metricOverridePath)

	var sb strings.Builder
	for _, t := range wm.Metrics {
		b, err := protojson.Marshal(t)
		if err != nil {
			return fmt.Sprintf("ERROR Could not create metrics JSON; %v", err)
		}
		sb.WriteString(fmt.Sprintf("%s\n", string(b)))
	}
	return fmt.Sprint(sb.String())
}

/*
The collectAndSendRemoteMetrics runs in the local binary and collects validation metrics
from remote hosts.
Returns the total number of metrics sent for all remotely collected hosts.
*/
func collectAndSendRemoteMetrics(ctx context.Context, params Parameters) int {
	rc := params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection()
	// make sure collection via gcloud or ssh is defined
	if rc.GetRemoteCollectionSsh() == nil && rc.GetRemoteCollectionGcloud() == nil {
		log.Logger.Error("remote_collection_gcloud and remote_collection_ssh are undefined for remote collection, one of them must be defined")
		return 0
	}
	wp := workerpool.New(int(params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection().GetConcurrentCollections()))

	mu := &sync.Mutex{}
	metricsSent := 0
	for _, i := range rc.GetRemoteCollectionInstances() {
		inst := i
		ch := make(chan WorkloadMetrics)
		wp.Submit(func() {
			log.Logger.Infof("Collecting metrics from %v", inst)
			if rc.GetRemoteCollectionSsh() != nil {
				go collectRemoteSSH(params, rc, inst, ch)
			} else if rc.GetRemoteCollectionGcloud() != nil {
				go collectRemoteGcloud(params, rc, inst, ch)
			}
			wm := <-ch
			// lock so we can update the metricsSent
			mu.Lock()
			defer mu.Unlock()
			metricsSent += sendMetrics(ctx, wm, inst.GetProjectId(), &params.TimeSeriesCreator, params.BackOffs)
		})
	}
	wp.StopWait()
	return metricsSent
}

// parseRemoteJSON parses JSON strings from remote host collection
func parseRemoteJSON(output string, metrics *[]*monitoringresourcespb.TimeSeries) error {
	for _, s := range strings.Split(output, "\n") {
		if len(s) == 0 {
			// blank line, continue
			continue
		}
		metric := &monitoringresourcespb.TimeSeries{}
		if err := protojson.Unmarshal([]byte(s), metric); err != nil {
			return err
		}
		*metrics = append(*metrics, metric)
	}
	return nil
}

func appendCommonGcloudArgs(args []string, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance) []string {
	args = append(args, "--project", i.ProjectId, "--zone", i.Zone)
	if rc.GetRemoteCollectionGcloud().GetTunnelThroughIap() {
		args = append(args, "--tunnel-through-iap")
	}
	if rc.GetRemoteCollectionGcloud().GetUseInternalIp() {
		args = append(args, "--internal-ip")
	}
	if rc.GetRemoteCollectionGcloud().GcloudArgs != "" {
		args = append(args, strings.Split(rc.GetRemoteCollectionGcloud().GetGcloudArgs(), " ")...)
	}
	return args
}

// The collectRemoteGcloud function will:
//   - copy the google_cloud_sap_agent binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteGcloud(params Parameters, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, wm chan<- WorkloadMetrics) {

	log.Logger.Debug("Collecting remote metrics using gcloud")
	log.Logger.Infof("Collecting metrics from %s", i.GetInstanceName())
	metrics := []*monitoringresourcespb.TimeSeries{}

	// remove the binary just in case it still exists on the remote
	sshArgs := []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, rc, i)
	sshArgs = append(sshArgs, i.GetInstanceName(), "--command", "rm -f "+remoteAgentBinary)
	output, stdErr, err := params.CommandRunnerNoSpace("gcloud", sshArgs...)
	if err != nil {
		log.Logger.Errorf("Could not ssh to remote host %s to remove existing tmp binary, err: %w, stdErr: %s, output: %s", i.GetInstanceName(), err, stdErr, output)
	}

	// gcloud compute scp --project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] filetotranser instancename:path
	scpArgs := []string{"compute", "scp"}
	scpArgs = appendCommonGcloudArgs(scpArgs, rc, i)
	scpArgs = append(scpArgs, agentBinary, fmt.Sprintf("%s:%s", i.GetInstanceName(), remoteAgentBinary))
	log.Logger.Debugf("Sending binary to %s", i.GetInstanceName())
	output, stdErr, err = params.CommandRunnerNoSpace("gcloud", scpArgs...)
	if err != nil {
		log.Logger.Errorf("Could not copy binary to %s, err: %w, stdErr: %s, output: %s", i.GetInstanceName(), err, stdErr, output)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	// gcloud compute ssh ---project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] instancename --command="commandtoexec"
	sshArgs = []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, rc, i)
	sshArgs = append(sshArgs, i.GetInstanceName(), "--command", remoteAgentBinary+fmt.Sprintf(" --remote -p=%s -z=%s -i=%s -n=%s; rm "+remoteAgentBinary, i.GetProjectId(), i.GetZone(), i.GetInstanceId(), i.GetInstanceName()))
	output, stdErr, err = params.CommandRunnerNoSpace("gcloud", sshArgs...)
	if err != nil {
		log.Logger.Errorf("Could not execute remote collection from %s, err: %w, stdErr: %s, output: %s", i.GetInstanceName(), err, stdErr, output)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}
	if strings.HasPrefix(output, "ERROR") {
		log.Logger.Errorf("Error encountered on remote host %s, output: %s", i.GetInstanceName(), output)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	err = parseRemoteJSON(output, &metrics)
	if err != nil {
		log.Logger.Errorf("Error in parsing metrics collected from %s, err: %w", i.GetInstanceName(), err)
	}

	if len(metrics) == 0 {
		log.Logger.Warnf("Error collected from %s has no data", i.GetInstanceName())
	}

	wm <- WorkloadMetrics{Metrics: metrics}
}

// The collectRemoteSSH function will:
//   - copy the google_cloud_sap_agent binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteSSH(params Parameters, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, wm chan<- WorkloadMetrics) {
	// TODO

}
