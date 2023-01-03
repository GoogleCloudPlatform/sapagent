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

	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	"google.golang.org/protobuf/encoding/protojson"
	"github.com/gammazero/workerpool"
	cnfpb "github.com/GoogleCloudPlatform/sap-agent/protos/configuration"
)

// CollectMetricsToJSON will collect all of the workload manager metrics and return the
// JSON representation of them, this is only called on remote instances for metric collection
// only called through the google-cloud-sap-agent-remote binary
func CollectMetricsToJSON(ctx context.Context, params Parameters) string {
	wm := collectMetrics(ctx, params, metricOverridePath)

	var sb strings.Builder
	sb.WriteString("[")
	for _, t := range wm.Metrics {
		b, err := protojson.Marshal(t)
		if err != nil {
			return fmt.Sprintf("ERROR Could not create metrics JSON; %v", err)
		}
		sb.WriteString(string(b))
	}
	sb.WriteString("]")
	return fmt.Sprint(sb.String())
}

// The collectAndSendRemoteMetrics function runs in the google-cloud-sap-agent binary.
// This will iterate through the remote hosts using a WorkerPool for async operation.
// The WorkerPool will run the collectRemote function below.
func collectAndSendRemoteMetrics(ctx context.Context, params Parameters) {
	wp := workerpool.New(int(params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection().GetConcurrentCollections()))

	rc := params.Config.GetCollectionConfiguration().GetWorkloadValidationRemoteCollection()
	for _, i := range rc.GetRemoteCollectionInstances() {
		inst := i
		ch := make(chan WorkloadMetrics)
		wp.Submit(func() {
			log.Logger.Infof("Collecting metrics from %v", inst)
			collectRemote(rc, inst, ch)
			wm := <-ch
			// TODO need to modify sendMetrics to allow for project, instance, and zone
			sendMetrics(ctx, wm, "TODO project", &params.TimeSeriesCreator)
		})
	}
	wp.StopWait()
}

// The collectRemote function will:
//   - copy the google-sap-agent-remote binary to the remote host,
//   - execute the binary to collect the metrics in JSON format in stdout
//   - read the stdout and parse errors or JSON into Metrics
//   - return the metrics from the host to the caller
func collectRemote(rc *cnfpb.WorkloadValidationRemoteCollection, i *cnfpb.RemoteCollectionInstance, wm chan<- WorkloadMetrics) {
	// TODO implement the remote collect functionality

	wm <- WorkloadMetrics{}
}
