/*
Copyright 2025 Google LLC

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

package computeresources

import (
	"context"
	"strconv"
	"strings"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	"github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/timeseries"
)

const (
	sarkBMemFreePath           = "/sap/compute/os/memory/sar_kbmemfree"
	sarkBMemAvailPath          = "/sap/compute/os/memory/sar_kbmemavail"
	sarkBMemUsedPath           = "/sap/compute/os/memory/sar_kbmemused"
	sarPercentMemusedPath      = "/sap/compute/os/memory/sar_perecent_memused"
	sarKbBuffersPath           = "/sap/compute/os/memory/sar_kbbuffers"
	sarKbCachedPath            = "/sap/compute/os/memory/sar_kbcached"
	sarKbCommitPath            = "/sap/compute/os/memory/sar_kbcommit"
	sarPercentCommitPath       = "/sap/compute/os/memory/sar_perecent_commit"
	sarKbActivePath            = "/sap/compute/os/memory/sar_kbactive"
	sarKbInactPath             = "/sap/compute/os/memory/sar_kbinact"
	sarKbDirtyPath             = "/sap/compute/os/memory/sar_kbdirty"
	freeMemUsedPath            = "/sap/compute/os/memory/freemem_used"
	freeMemTotalPath           = "/sap/compute/os/memory/freemem_total"
	freeMemFreePath            = "/sap/compute/os/memory/freemem_free"
	freeMemSharedPath          = "/sap/compute/os/memory/freemem_shared"
	freeMemBuffersAndCachePath = "/sap/compute/os/memory/freemem_buffers_and_cache"
	freeMemAvailablePath       = "/sap/compute/os/memory/freemem_available"
	freeSwapTotalPath          = "/sap/compute/os/memory/freeswap_total"
	freeSwapUsedPath           = "/sap/compute/os/memory/freeswap_used"
	freeSwapFreePath           = "/sap/compute/os/memory/freeswap_free"
)

var (
	sarFieldToMetricPathMap = map[string]string{
		"kbmemfree": sarkBMemFreePath,
		"kbavail":   sarkBMemAvailPath,
		"kbmemused": sarkBMemUsedPath,
		"%memused":  sarPercentMemusedPath,
		"kbbuffers": sarKbBuffersPath,
		"kbcached":  sarKbCachedPath,
		"kbcommit":  sarKbCommitPath,
		"%commit":   sarPercentCommitPath,
		"kbactive":  sarKbActivePath,
		"kbinact":   sarKbInactPath,
		"kbdirty":   sarKbDirtyPath,
	}

	freeMemFieldToMetricPathMap = map[string]string{
		"total":      freeMemTotalPath,
		"used":       freeMemUsedPath,
		"free":       freeMemFreePath,
		"shared":     freeMemSharedPath,
		"buff/cache": freeMemBuffersAndCachePath,
		"available":  freeMemAvailablePath,
	}

	freeSwapFieldToMetricPathMap = map[string]string{
		"total": freeSwapTotalPath,
		"used":  freeSwapUsedPath,
		"free":  freeSwapFreePath,
	}
)

type (
	// LinuxOsMetricsInstanceProperties has the required context for collecting linux os related metrics.
	LinuxOsMetricsInstanceProperties struct {
		Config          *cnfpb.Configuration
		Client          cloudmonitoring.TimeSeriesCreator
		Executor        commandlineexecutor.Execute
		SAPInstance     *sapb.SAPInstance
		SkippedMetrics  map[string]bool
		PMBackoffPolicy backoff.BackOffContext
	}
)

// Collect is an implementation of Collector interface from processmetrics.go for linux os metrics.
// Returns a list of os related metrics and returns last error encountered while collecting metrics.
func (l *LinuxOsMetricsInstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		metrics              []*mrpb.TimeSeries
		metricsCollectionErr error
	)
	labels := make(map[string]string)
	overcommitMemory, err := fetchOverCommitMemory(ctx, l.Executor)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error while fetching overcommit memory. Skipping overcommit memory label collection for sar and free metrics", "error", err)
	} else {
		labels["overcommit_memory"] = overcommitMemory
	}

	overcommitRatio, err := fetchOverCommitRatio(ctx, l.Executor)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error while fetching overcommit ratio. Skipping overcommit ratio label collection for sar and free metrics", "error", err)
	} else {
		labels["overcommit_ratio"] = overcommitRatio
	}

	sarMetrics, err := collectSarMetrics(ctx, l, labels)
	metrics = append(metrics, sarMetrics...)
	if err != nil {
		metricsCollectionErr = err
	}

	freeMemMetrics, err := collectFreeMemMetrics(ctx, l, labels)
	metrics = append(metrics, freeMemMetrics...)
	if err != nil {
		metricsCollectionErr = err
	}

	return metrics, metricsCollectionErr
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (l *LinuxOsMetricsInstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debugw("Context cancelled, exiting CollectWithRetry")
			return nil
		default:
			var err error
			res, err = l.Collect(ctx)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
				attempt++
			}
			return err
		}
	}, l.PMBackoffPolicy)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Retry limit exceeded", "InstanceId", l.SAPInstance.GetInstanceId(), "error", err)
	}
	return res, err
}

// collectSarMetrics collects memory metrics from the system using sar command.
func collectSarMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, labels map[string]string) ([]*mrpb.TimeSeries, error) {
	args := "-r 1 1"
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "sar",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command sar", "args", args, "error", result.Error)
		return nil, result.Error
	}
	outputLines := strings.Split(string(result.StdOut), "\n")
	if len(outputLines) < 4 {
		log.CtxLogger(ctx).Debugw("Command sar returned lesser rows than expected. Skipping collection of sar command memory metrics", "stdout", result.StdOut)
		return nil, nil
	}
	headerFields := strings.Fields(outputLines[2])
	sarFields := strings.Fields(outputLines[3])

	// Unexpected output from sar command when number of header fields don't match number of value fields.
	if len(headerFields) != len(sarFields) {
		log.CtxLogger(ctx).Debugw("Command sar returned different number of header fields and value fields. Skipping collection of sar command memory metrics", "headerFields", headerFields, "sarFields", sarFields)
		return nil, nil
	}

	sarFieldValues := make(map[string]string)
	for i, field := range headerFields {
		// We are skipping the timestamp field.
		if i != 0 {
			sarFieldValues[field] = sarFields[i]
		}
	}
	return populateSarFieldMetrics(ctx, p, sarFieldValues, labels), nil
}

// collectFreeMemMetrics collects free memory metrics from the system using free -k command.
func collectFreeMemMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, labels map[string]string) ([]*mrpb.TimeSeries, error) {
	args := "-k"
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "free",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command free", "args", args, "error", result.Error)
		return nil, result.Error
	}
	outputLines := strings.Split(string(result.StdOut), "\n")
	if len(outputLines) < 3 {
		log.CtxLogger(ctx).Debugw("Command free returned lesser rows than expected. Skipping collection of free command memory metrics", "stdout", result.StdOut)
		return nil, nil
	}
	headerFields := strings.Fields(outputLines[0])
	memValues := strings.Fields(outputLines[1])
	swapValues := strings.Fields(outputLines[2])

	memFieldToValueMap := make(map[string]string)
	if len(headerFields) < len(memValues)-1 || len(headerFields) < len(swapValues)-1 {
		log.CtxLogger(ctx).Debugw("Command free returned unexpected number of header fields and value fields. Skipping collection of free command memory metrics", "headerFields", headerFields, "memValues", memValues, "swapValues", swapValues)
		return nil, nil
	}
	for i, value := range memValues {
		if i != 0 {
			// headerFields[i-1] because rows after header row have one extra column. Output of free -k looks like this:
			//                total        used        free      shared  buff/cache   available
			// Mem:       131902960    18861708   109490620     4807332     9611728   113041252
			// Swap:        2097148           0     2097148
			// So we need to skip the first column of mem and swap rows.
			memFieldToValueMap[headerFields[i-1]] = value
		}
	}

	swapFieldToValueMap := make(map[string]string)
	for i, value := range swapValues {
		if i != 0 {
			swapFieldToValueMap[headerFields[i-1]] = value
		}
	}

	return populateFreeFieldMetrics(ctx, p, memFieldToValueMap, swapFieldToValueMap, labels), nil
}

// fetchOverCommitMemory fetches the value of overcommit_memory using proc /proc/sys/vm/overcommit_memory.
func fetchOverCommitMemory(ctx context.Context, exec commandlineexecutor.Execute) (string, error) {
	args := "/proc/sys/vm/overcommit_memory"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "cat",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command cat", "args", args, "error", result.Error)
		return "", result.Error
	}
	return strings.TrimSpace(result.StdOut), nil
}

// fetchOverCommitRatio fetches the value of overcommit_ratio using proc /proc/sys/vm/overcommit_ratio.
func fetchOverCommitRatio(ctx context.Context, exec commandlineexecutor.Execute) (string, error) {
	args := "/proc/sys/vm/overcommit_ratio"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "cat",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command cat", "args", args, "error", result.Error)
		return "", result.Error
	}
	return strings.TrimSpace(result.StdOut), nil
}

func populateSarFieldMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, sarFieldValues map[string]string, labels map[string]string) []*mrpb.TimeSeries {
	sarMetrics := make([]*mrpb.TimeSeries, 0)
	for field, valueStr := range sarFieldValues {
		if metricPath, ok := sarFieldToMetricPathMap[field]; ok {
			if _, skip := p.SkippedMetrics[metricPath]; skip {
				continue
			}
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error while parsing sar output field to float64", "field", field, "value", valueStr, "error", err)
				continue
			}
			sarMetrics = append(sarMetrics, createLinuxOsMetrics(p, metricPath, labels, value))
		}
	}
	return sarMetrics
}

func populateFreeFieldMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, memFieldToValueMap map[string]string, swapFieldToValueMap map[string]string, labels map[string]string) []*mrpb.TimeSeries {
	freeMemMetrics := make([]*mrpb.TimeSeries, 0)
	for field, valueStr := range memFieldToValueMap {
		if metricPath, ok := freeMemFieldToMetricPathMap[field]; ok {
			if _, skip := p.SkippedMetrics[metricPath]; skip {
				continue
			}
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error while parsing free output field to float64", "field", field, "value", valueStr, "error", err)
				continue
			}
			freeMemMetrics = append(freeMemMetrics, createLinuxOsMetrics(p, metricPath, labels, value))
		}
	}

	for field, valueStr := range swapFieldToValueMap {
		if metricPath, ok := freeSwapFieldToMetricPathMap[field]; ok {
			if _, skip := p.SkippedMetrics[metricPath]; skip {
				continue
			}
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error while parsing swap output field to float64", "field", field, "value", valueStr, "error", err)
				continue
			}
			freeMemMetrics = append(freeMemMetrics, createLinuxOsMetrics(p, metricPath, labels, value))
		}
	}
	return freeMemMetrics
}

func createLinuxOsMetrics(p *LinuxOsMetricsInstanceProperties, mPath string, labels map[string]string, val float64) *mrpb.TimeSeries {
	ts := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(p.Config.CloudProperties),
		MetricType:   metricURL + mPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		Float64Value: val,
	}
	log.Logger.Debugw("Creating metric for instance", "metric", mPath, "value", val, "instancenumber", p.SAPInstance.GetInstanceNumber(), "labels", labels)
	return timeseries.BuildFloat64(ts)
}
