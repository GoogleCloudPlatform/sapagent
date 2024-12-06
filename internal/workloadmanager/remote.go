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
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/gammazero/workerpool"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/recovery"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const (
	agentBinary            = "/usr/bin/google_cloud_sap_agent"
	remoteAgentBinary      = "/tmp/google_cloud_sap_agent"
	remoteValidationConfig = "/tmp/workload-validation.json"
)

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
		log.CtxLogger(ctx).Error("One of remote_collection_gcloud or remote_collection_ssh must be defined for remote collection")
		return 0
	}

	tempFile, err := createWorkloadValidationFile(ctx, params.WorkloadConfig)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Could not create temporary file for workload validation", "error", err)
		return 0
	}
	defer os.Remove(tempFile.Name())

	wp := workerpool.New(int(params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection().GetConcurrentCollections()))
	mu := &sync.Mutex{}
	metricsSent := 0
	var routines []*recovery.RecoverableRoutine
	for _, inst := range rc.GetRemoteCollectionInstances() {
		inst := inst
		ch := make(chan WorkloadMetrics)
		wp.Submit(func() {
			log.CtxLogger(ctx).Infow("Collecting metrics from", "instance", inst)
			var r *recovery.RecoverableRoutine
			if rc.GetRemoteCollectionSsh() != nil {
				r = &recovery.RecoverableRoutine{
					Routine: collectRemoteSSH,
					RoutineArg: collectOptions{
						exists:     params.Exists,
						execute:    params.Execute,
						configPath: tempFile.Name(),
						rc:         rc,
						i:          inst,
						wm:         ch,
					},
					ErrorCode:           usagemetrics.RemoteCollectSSHFailure,
					UsageLogger:         *usagemetrics.Logger,
					ExpectedMinDuration: time.Minute,
				}
			} else if rc.GetRemoteCollectionGcloud() != nil {
				r = &recovery.RecoverableRoutine{
					Routine: collectRemoteGcloud,
					RoutineArg: collectOptions{
						exists:     params.Exists,
						execute:    params.Execute,
						configPath: tempFile.Name(),
						rc:         rc,
						i:          inst,
						wm:         ch,
					},
					ErrorCode:           usagemetrics.RemoteCollectGcloudFailure,
					UsageLogger:         *usagemetrics.Logger,
					ExpectedMinDuration: time.Minute,
				}
			}
			if r != nil {
				routines = append(routines, r)
				r.StartRoutine(ctx)
			}
			wm := <-ch
			// lock so we can update the metricsSent
			mu.Lock()
			defer mu.Unlock()
			remoteCp := &ipb.CloudProperties{
				ProjectId:    inst.GetProjectId(),
				InstanceId:   inst.GetInstanceId(),
				Zone:         inst.GetZone(),
				InstanceName: inst.GetInstanceName(),
			}
			metricsSent += sendMetrics(ctx, sendMetricsParams{
				wm:                    wm,
				cp:                    remoteCp,
				bareMetal:             params.Config.GetBareMetal(),
				sendToCloudMonitoring: params.Config.GetSupportConfiguration().GetSendWorkloadValidationMetricsToCloudMonitoring().GetValue(),
				timeSeriesCreator:     params.TimeSeriesCreator,
				backOffIntervals:      params.BackOffs,
				wlmService:            params.WLMService,
			})
		})
	}
	wp.StopWait()
	return metricsSent
}

// createWorkloadValidationFile stores a workload validation definition in a
// temporary file. Callers should remove the file after use.
func createWorkloadValidationFile(ctx context.Context, workloadConfig *wlmpb.WorkloadValidation) (*os.File, error) {
	log.CtxLogger(ctx).Info("Creating temporary file containing workload validation configuration")
	tempFile, err := os.CreateTemp("", "workload-validation.*.json")
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()
	configJSON, err := protojson.Marshal(workloadConfig)
	if err != nil {
		os.Remove(tempFile.Name())
		return nil, err
	}
	if _, err = tempFile.Write(configJSON); err != nil {
		os.Remove(tempFile.Name())
		return nil, err
	}
	return tempFile, nil
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

// appendCommonGcloudArgs appends common gcloud args to the given args slice.
func appendCommonGcloudArgs(args []string, rc *cpb.WorkloadValidationRemoteCollection, i *cpb.RemoteCollectionInstance) []string {
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

// gcloudInstanceName returns the instance name to use for gcloud commands.
// If the SSH username is set, it will be prepended to the instance name.
func gcloudInstanceName(rc *cpb.WorkloadValidationRemoteCollection, i *cpb.RemoteCollectionInstance) string {
	if rc.GetRemoteCollectionGcloud().GetSshUsername() != "" {
		return fmt.Sprintf("%s@%s", rc.GetRemoteCollectionGcloud().GetSshUsername(), i.GetInstanceName())
	}
	return i.GetInstanceName()
}

// collectOptions is a struct that contains the parameters needed to collect metrics from a remote host.
type collectOptions struct {
	exists     commandlineexecutor.Exists
	execute    commandlineexecutor.Execute
	configPath string
	rc         *cpb.WorkloadValidationRemoteCollection
	i          *cpb.RemoteCollectionInstance
	wm         chan<- WorkloadMetrics
}

// The collectRemoteGcloud function will:
//   - copy the workload validation configuration to the remote host
//   - copy the google_cloud_sap_agent binary to the remote host
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteGcloud(ctx context.Context, a any) {
	var opts collectOptions
	var ok bool
	if opts, ok = a.(collectOptions); !ok {
		log.CtxLogger(ctx).Errorw("Cannot collect remote metrics using gcloud", "reason", fmt.Sprintf("args of type %T does not match collectOptions", a))
		return
	}

	var metrics []*mrpb.TimeSeries
	if !opts.exists("gcloud") {
		log.CtxLogger(ctx).Error("gcloud command not found. Ensure the google cloud SDK is installed and that the gcloud command is in systemd's PATH environment variable: `systemctl show-environment`, `systemctl set-environment PATH=</path:/another/path>")
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	log.CtxLogger(ctx).Infow("Collecting remote metrics using gcloud", "instance", opts.i)
	iName := gcloudInstanceName(opts.rc, opts.i)
	// remove the binary just in case it still exists on the remote
	sshArgs := []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, opts.rc, opts.i)
	sshArgs = append(sshArgs, iName, "--command", "sudo rm -f "+remoteAgentBinary)
	result := opts.execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       sshArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not ssh to remote instance to remove existing tmp binary", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
	}

	// gcloud compute scp --project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] filetotransfer [user@]instancename:path
	scpArgs := []string{"compute", "scp"}
	scpArgs = appendCommonGcloudArgs(scpArgs, opts.rc, opts.i)
	scpArgs = append(scpArgs, opts.configPath, fmt.Sprintf("%s:%s", iName, remoteValidationConfig))
	log.CtxLogger(ctx).Debugw("Sending workload validation config to remote host", "instance", opts.i)
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not copy workload validation config to remote instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	// gcloud compute scp --project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] filetotransfer [user@]instancename:path
	scpArgs = []string{"compute", "scp"}
	scpArgs = appendCommonGcloudArgs(scpArgs, opts.rc, opts.i)
	scpArgs = append(scpArgs, agentBinary, fmt.Sprintf("%s:%s", iName, remoteAgentBinary))
	log.CtxLogger(ctx).Debugw("Sending binary to remote host", "instance", opts.i)
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not copy binary to remote instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	// gcloud compute ssh ---project someproject --zone somezone [--tunnel-through-iap] [--internal-ip] [otherargs] [user@]instancename --command="commandtoexec"
	command := "sudo " + remoteAgentBinary + fmt.Sprintf(" remote -c=%s -p=%s -z=%s -i=%s -n=%s", remoteValidationConfig, opts.i.GetProjectId(), opts.i.GetZone(), opts.i.GetInstanceId(), opts.i.GetInstanceName()) + "; rm " + remoteAgentBinary + "; rm " + remoteValidationConfig
	sshArgs = []string{"compute", "ssh"}
	sshArgs = appendCommonGcloudArgs(sshArgs, opts.rc, opts.i)
	sshArgs = append(sshArgs, iName, "--command", command)
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "gcloud",
		Args:       sshArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not execute remote collection on instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}
	if strings.HasPrefix(result.StdOut, "ERROR") {
		log.CtxLogger(ctx).Errorw("Error encountered on remote instance", "instance", opts.i, "error", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	err := parseRemoteJSON(result.StdOut, &metrics)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error parsing metrics collected from remote instance", "instance", opts.i, "error", err)
	}

	if len(metrics) == 0 {
		log.CtxLogger(ctx).Warnw("No data collected from remote instance", "instance", opts.i)
	}

	opts.wm <- WorkloadMetrics{Metrics: metrics}
}

// appendSSHArgs appends SSH arguments to the given args slice.
func appendSSHArgs(args []string, rc *cpb.WorkloadValidationRemoteCollection, i *cpb.RemoteCollectionInstance, isScp bool) []string {
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
//   - copy the workload validation configuration to the remote host
//   - copy the google_cloud_sap_agent binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemoteSSH(ctx context.Context, a any) {
	var opts collectOptions
	var ok bool
	if opts, ok = a.(collectOptions); !ok {
		log.CtxLogger(ctx).Errorw("Cannot collect remote metrics using ssh", "reason", fmt.Sprintf("args of type %T does not match collectOptions", a))
		return
	}

	projectID := opts.i.ProjectId
	zone := opts.i.Zone
	instanceID := opts.i.InstanceId
	instanceName := opts.i.InstanceName

	log.CtxLogger(ctx).Infow("Collecting remote metrics using ssh", "instance", opts.i)

	rmArgs := []string{}
	rmArgs = appendSSHArgs(rmArgs, opts.rc, opts.i, false)
	// append "rm -f remoteAgentBinary"
	rmArgs = append(rmArgs, "rm -f "+remoteAgentBinary)
	result := opts.execute(ctx, commandlineexecutor.Params{
		Executable: "ssh",
		Args:       rmArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not ssh to remote instance to remove existing tmp binary", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
	}

	var metrics []*mrpb.TimeSeries

	scpArgs := []string{"-i", opts.rc.RemoteCollectionSsh.GetSshPrivateKeyPath()}
	scpArgs = append(scpArgs, opts.configPath)
	scpArgs = append(scpArgs, fmt.Sprintf("%s@%s:%s", opts.rc.RemoteCollectionSsh.GetSshUsername(), opts.i.SshHostAddress, remoteValidationConfig))
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "scp",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not copy workload validation config to remote instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	scpArgs = []string{}
	scpArgs = appendSSHArgs(scpArgs, opts.rc, opts.i, true)
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "scp",
		Args:       scpArgs,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not copy binary to remote instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	sshArgs := []string{}
	sshArgs = appendSSHArgs(sshArgs, opts.rc, opts.i, false)
	// append "remoteAgentBinary remote -h=false -p=projectID -i=instanceID -n=instanceName -z=zone"
	sshArgs = append(sshArgs, remoteAgentBinary, "remote", fmt.Sprintf("-c=%s -p=%s -i=%s -n=%s -z=%s", remoteValidationConfig, projectID, instanceID, instanceName, zone))
	sshArgs = append(sshArgs, "; rm "+remoteAgentBinary, "; rm "+remoteValidationConfig)
	result = opts.execute(ctx, commandlineexecutor.Params{
		Executable: "ssh",
		Args:       sshArgs,
	})

	if result.Error != nil {
		log.CtxLogger(ctx).Errorw("Could not execute remote collection on instance", "instance", opts.i, "error", result.Error, "stderr", result.StdErr, "stdout", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	if strings.HasPrefix(result.StdOut, "ERROR") {
		log.CtxLogger(ctx).Errorw("Error encountered on remote instance", "instance", opts.i, "error", result.StdOut)
		opts.wm <- WorkloadMetrics{Metrics: metrics}
		return
	}

	err := parseRemoteJSON(result.StdOut, &metrics)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Error parsing metrics collected from remote instance", "instance", opts.i, "error", err)
	}

	if len(metrics) == 0 {
		log.CtxLogger(ctx).Warnw("No data collected from remote instance", "instance", opts.i)
	}

	opts.wm <- WorkloadMetrics{Metrics: metrics}
}
