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

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	"google3/third_party/sapagent/shared/log"
)

const agentBinary = "/usr/bin/google_cloud_sap_agent"
const remoteAgentBinary = "/tmp/google_cloud_sap_agent"

// CollectMetricsToJSON will collect all of the workload manager metrics and return the
// JSON representation of them, this is only called on remote instances for metric collection
// only called through the google_cloud_sap_agent binary using remote mode
func CollectMetricsToJSON(ctx context.Context, params Parameters) string {
	wm := collectMetricsFromConfig(ctx, params, metricOverridePath)

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
	for _, inst := range rc.GetRemoteCollectionInstances() {
		inst := inst
		ch := make(chan WorkloadMetrics)
		wp.Submit(func() {
			log.Logger.Infow("Collecting metrics from", "instance", inst)
			if rc.GetRemoteCollectionSsh() != nil {
				go collectRemoteSSH(ctx, params, rc, inst, ch)
			} else if rc.GetRemoteCollectionGcloud() != nil {
				go collectRemoteGcloud(ctx, params, rc, inst, ch)
			}
			wm := <-ch
			// lock so we can update the metricsSent
			mu.Lock()
			defer mu.Unlock()
			metricsSent += sendMetrics(ctx, sendMetricsParams{
				wm:                wm,
				cp:                params.Config.GetCloudProperties(),
				bareMetal:         params.Config.GetBareMetal(),
				timeSeriesCreator: params.TimeSeriesCreator,
				backOffIntervals:  params.BackOffs,
				wlmService:        params.WLMService,
			})
		})
	}
	wp.StopWait()
	return metricsSent
}

// parseRemoteJSON parses JSON strings from remote host collection
func parseRemoteJSON(output string, metrics *[]*mrpb.TimeSeries) error {
	for _, s := range strings.Split(output, "\n") {
		if len(s) == 0 {
			// blank line, continue
			continue
		}
		metric := &mrpb.TimeSeries{}
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
	if rc.GetRemoteCollectionGcloud().GetGcloudArgs() != "" {
		args = append(args, strings.Split(rc.GetRemoteCollectionGcloud().GetGcloudArgs(), " ")...)
	}
	return args
}

func gcloudInstanceName(rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance) string {
	if rc.GetRemoteCollectionGcloud().GetSshUsername() != "" {
		return fmt.Sprintf("%s@%s", rc.GetRemoteCollectionGcloud().GetSshUsername(), i.GetInstanceName())
	}
	return i.GetInstanceName()
}

// The collectRemoteGcloud function will:
//   - copy the google_cloud_sap_agent binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteGcloud(ctx context.Context, params Parameters, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, wm chan<- WorkloadMetrics) {
	var metrics []*mrpb.TimeSeries
	if !params.Exists("gcloud") {
		log.Logger.Error("gcloud command not found. Ensure the google cloud SDK is installed and that the gcloud command is in systemd's PATH environment variable: `systemctl show-environment`, `systemctl set-environment PATH=</path:/another/path>")
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	log.Logger.Infow("Collecting remote metrics using gcloud", "instance", i)
	iName := gcloudInstanceName(rc, i)
	// remove the binary just in case it still exists on the remote
	sshArgs := []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, rc, i)
	sshArgs = append(sshArgs, iName, "--command", "sudo rm -f "+remoteAgentBinary)
	result := params.Execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       sshArgs,
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not ssh to remote instance to remove existing tmp binary", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
	}

	// gcloud compute scp --project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] filetotranser [user@]instancename:path
	scpArgs := []string{"compute", "scp"}
	scpArgs = appendCommonGcloudArgs(scpArgs, rc, i)
	scpArgs = append(scpArgs, agentBinary, fmt.Sprintf("%s:%s", iName, remoteAgentBinary))
	log.Logger.Debugw("Sending binary to remote host", "instance", i)
	result = params.Execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not copy binary to remote instance", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	// gcloud compute ssh ---project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] [user@]instancename --command="commandtoexec"
	sshArgs = []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, rc, i)
	sshArgs = append(sshArgs, iName, "--command", "sudo "+remoteAgentBinary+fmt.Sprintf(" remote -p=%s -z=%s -i=%s -n=%s; rm "+remoteAgentBinary, i.GetProjectId(), i.GetZone(), i.GetInstanceId(), i.GetInstanceName()))
	result = params.Execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       sshArgs,
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not execute remote collection on instance", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}
	if strings.HasPrefix(result.StdOut, "ERROR") {
		log.Logger.Errorw("Error encountered on remote instance", "instance", i, "error", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	err := parseRemoteJSON(result.StdOut, &metrics)
	if err != nil {
		log.Logger.Errorw("Error parsing metrics collected from remote instance", "instance", i, "error", err)
	}

	if len(metrics) == 0 {
		log.Logger.Warnw("No data collected from remote instance", "instance", i)
	}

	wm <- WorkloadMetrics{Metrics: metrics}
}

func appendSSHArgs(args []string, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, isScp bool) []string {

	hostAddr := i.SshHostAddress
	pkPath := rc.RemoteCollectionSsh.GetSshPrivateKeyPath()
	userName := rc.RemoteCollectionSsh.GetSshUsername()
	sshArg := userName + "@" + hostAddr

	args = append(args, "-i", pkPath)
	if isScp {
		// append "-i /root/.ssh/sap-agent-key /usr/sap/google-cloud-sap-agent/google-cloud-sap-agent-remote username@10.128.0.36:remoteAgentBinary"
		args = append(args, agentBinary, sshArg+":"+remoteAgentBinary)
	} else {
		// append "-i /root/.ssh/sap-agent-key username@10.128.0.36"
		args = append(args, sshArg)
	}

	return args
}

// The collectRemoteSSH function will:
//   - copy the google_cloud_sap_agent binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteSSH(ctx context.Context, params Parameters, rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, wm chan<- WorkloadMetrics) {
	projectID := i.ProjectId
	zone := i.Zone
	instanceID := i.InstanceId
	instanceName := i.InstanceName

	log.Logger.Infow("Collecting remote metrics using ssh", "instance", i)

	rmArgs := []string{}
	rmArgs = appendSSHArgs(rmArgs, rc, i, false)
	// append "rm -f remoteAgentBinary"
	rmArgs = append(rmArgs, "rm -f "+remoteAgentBinary)
	result := params.Execute(ctx, commandlineexecutor.Params{
		Executable: "scp",
		Args:       rmArgs,
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not ssh to remote instance to remove existing tmp binary", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
	}

	metrics := []*mrpb.TimeSeries{}

	scpArgs := []string{}
	scpArgs = appendSSHArgs(scpArgs, rc, i, true)
	result = params.Execute(ctx, commandlineexecutor.Params{
		Executable: "scp",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.Logger.Errorw("Could not copy binary to remote instance", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	sshArgs := []string{}
	sshArgs = appendSSHArgs(sshArgs, rc, i, false)
	//append "remoteAgentBinary remote -h=false -p=projectID -i=instanceID -n=instanceName -z=zone"
	sshArgs = append(sshArgs, remoteAgentBinary, "remote", "-h=false", "-p="+projectID+" -i="+instanceID+" -n="+instanceName+" -z="+zone)
	result = params.Execute(ctx, commandlineexecutor.Params{
		Executable: "ssh",
		Args:       sshArgs,
	})

	if result.Error != nil {
		log.Logger.Errorw("Could not execute remote collection on instance", "instance", i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	if strings.HasPrefix(result.StdOut, "ERROR") {
		log.Logger.Errorw("Error encountered on remote instance", "instance", i, "error", result.StdOut)
		wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	err := parseRemoteJSON(result.StdOut, &metrics)
	if err != nil {
		log.Logger.Errorw("Error parsing metrics collected from remote instance", "instance", i, "error", err)
	}

	if len(metrics) == 0 {
		log.Logger.Warnw("No data collected from remote instance", "instance", i)
	}

	wm <- WorkloadMetrics{Metrics: metrics}
}
