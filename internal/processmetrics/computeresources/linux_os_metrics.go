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
	"go.uber.org/multierr"
	"github.com/GoogleCloudPlatform/sapagent/internal/utils/protostruct"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/cloudmonitoring"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/commandlineexecutor"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/timeseries"
)

const (
	memFreeKBPath                = "/sap/compute/os/memory/mem_free_kb"
	memAvailKBPath               = "/sap/compute/os/memory/mem_available_kb"
	memTotalKBPath               = "/sap/compute/os/memory/mem_total_kb"
	buffersKBPath                = "/sap/compute/os/memory/buffers_kb"
	cachedKBPath                 = "/sap/compute/os/memory/cached_kb"
	swapCachedKBPath             = "/sap/compute/os/memory/swap_cached_kb"
	commitKBPath                 = "/sap/compute/os/memory/commit_kb"
	commitPercentPath            = "/sap/compute/os/memory/commit_percent"
	activeKBPath                 = "/sap/compute/os/memory/active_kb"
	inactiveKBPath               = "/sap/compute/os/memory/inactive_kb"
	dirtyKBPath                  = "/sap/compute/os/memory/dirty_kb"
	shMemKBPath                  = "/sap/compute/os/memory/shmem_kb"
	freeMemUsedKBPath            = "/sap/compute/os/memory/freemem_used_kb"
	freeMemTotalKBPath           = "/sap/compute/os/memory/freemem_total_kb"
	freeMemFreeKBPath            = "/sap/compute/os/memory/freemem_free_kb"
	freeMemSharedKBPath          = "/sap/compute/os/memory/freemem_shared_kb"
	freeMemBuffersAndCacheKBPath = "/sap/compute/os/memory/freemem_buffers_and_cache_kb"
	freeMemAvailableKBPath       = "/sap/compute/os/memory/freemem_available_kb"
	freeSwapTotalKBPath          = "/sap/compute/os/memory/freeswap_total_kb"
	freeSwapUsedKBPath           = "/sap/compute/os/memory/freeswap_used_kb"
	freeSwapFreeKBPath           = "/sap/compute/os/memory/freeswap_free_kb"
)

var (
	procMemInfoFieldToMetricPathMap = map[string]string{
		"MemTotal":     memTotalKBPath,
		"MemFree":      memFreeKBPath,
		"MemAvailable": memAvailKBPath,
		"Buffers":      buffersKBPath,
		"Cached":       cachedKBPath,
		"SwapCached":   swapCachedKBPath,
		"Active":       activeKBPath,
		"Inactive":     inactiveKBPath,
		"Dirty":        dirtyKBPath,
		"Committed_AS": commitKBPath,
		"Shmem":        shMemKBPath,
	}

	freeMemFieldToMetricPathMap = map[string]string{
		"total":      freeMemTotalKBPath,
		"used":       freeMemUsedKBPath,
		"free":       freeMemFreeKBPath,
		"shared":     freeMemSharedKBPath,
		"buff/cache": freeMemBuffersAndCacheKBPath,
		"available":  freeMemAvailableKBPath,
	}

	freeSwapFieldToMetricPathMap = map[string]string{
		"total": freeSwapTotalKBPath,
		"used":  freeSwapUsedKBPath,
		"free":  freeSwapFreeKBPath,
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
// Returns a list of os related metrics and a combined error message for the errors encountered while collecting metrics.
func (l *LinuxOsMetricsInstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		metrics              []*mrpb.TimeSeries
		metricsCollectionErr error
	)
	labels := make(map[string]string)
	overcommitMemory, ok := fetchOvercommitMemory(ctx, l.Executor)
	if ok {
		labels["overcommit_memory"] = overcommitMemory
	}

	overcommitRatio, ok := fetchOvercommitRatio(ctx, l.Executor)
	if ok {
		labels["overcommit_ratio"] = overcommitRatio
	}

	procMemInfoMetrics, err := collectProcMemInfoMetrics(ctx, l, labels)
	metrics = append(metrics, procMemInfoMetrics...)
	if err != nil {
		metricsCollectionErr = multierr.Append(metricsCollectionErr, err)
	}

	freeMemMetrics, err := collectFreeMemMetrics(ctx, l, labels)
	metrics = append(metrics, freeMemMetrics...)
	if err != nil {
		metricsCollectionErr = multierr.Append(metricsCollectionErr, err)
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

// collectProcMemInfoMetrics collects memory metrics from the system using proc/meminfo.
func collectProcMemInfoMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, labels map[string]string) ([]*mrpb.TimeSeries, error) {
	args := "/proc/meminfo"
	result := p.Executor(ctx, commandlineexecutor.Params{
		Executable:  "cat",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command cat", "args", args, "error", result.Error)
		return nil, result.Error
	}
	outputLines := strings.Split(string(result.StdOut), "\n")

	fieldToValueMap := make(map[string]string)
	for _, line := range outputLines {
		outputFields := strings.Fields(line)
		if len(outputFields) >= 2 {
			field := strings.TrimSuffix(outputFields[0], ":")
			fieldToValueMap[field] = outputFields[1]
		}
	}

	return populateProcMemInfoMetrics(ctx, p, fieldToValueMap, labels), nil
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

// fetchOvercommitMemory fetches the value of overcommit_memory using proc /proc/sys/vm/overcommit_memory.
func fetchOvercommitMemory(ctx context.Context, exec commandlineexecutor.Execute) (string, bool) {
	args := "/proc/sys/vm/overcommit_memory"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "cat",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command cat", "args", args, "error", result.Error)
		return "", false
	}
	return strings.TrimSpace(result.StdOut), true
}

// fetchOvercommitRatio fetches the value of overcommit_ratio using proc /proc/sys/vm/overcommit_ratio.
func fetchOvercommitRatio(ctx context.Context, exec commandlineexecutor.Execute) (string, bool) {
	args := "/proc/sys/vm/overcommit_ratio"
	result := exec(ctx, commandlineexecutor.Params{
		Executable:  "cat",
		ArgsToSplit: args,
	})
	if result.Error != nil {
		log.CtxLogger(ctx).Debugw("Error while executing command cat", "args", args, "error", result.Error)
		return "", false
	}
	return strings.TrimSpace(result.StdOut), true
}

func populateProcMemInfoMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, fieldToValueMap, labels map[string]string) []*mrpb.TimeSeries {
	procMemInfoMetrics := make([]*mrpb.TimeSeries, 0)
	for field, valueStr := range fieldToValueMap {
		if metricPath, ok := procMemInfoFieldToMetricPathMap[field]; ok {
			if p.SkippedMetrics[metricPath] {
				continue
			}
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error while parsing proc mem info output field to float64", "field", field, "value", valueStr, "error", err)
				continue
			}
			procMemInfoMetrics = append(procMemInfoMetrics, createLinuxOsMetrics(p, metricPath, labels, value))
		}
	}

	// We are calculating this field separately because commit% is not directly derived from the output of the proc mem info.
	if !p.SkippedMetrics[commitPercentPath] {
		commitPercent, ok := calculateCommitPercent(fieldToValueMap["MemTotal"], fieldToValueMap["Committed_AS"])
		if ok {
			procMemInfoMetrics = append(procMemInfoMetrics, createLinuxOsMetrics(p, commitPercentPath, labels, commitPercent))
		}
	}
	return procMemInfoMetrics
}

func populateFreeFieldMetrics(ctx context.Context, p *LinuxOsMetricsInstanceProperties, memFieldToValueMap, swapFieldToValueMap, labels map[string]string) []*mrpb.TimeSeries {
	freeMemMetrics := make([]*mrpb.TimeSeries, 0)
	for field, valueStr := range memFieldToValueMap {
		if metricPath, ok := freeMemFieldToMetricPathMap[field]; ok {
			if p.SkippedMetrics[metricPath] {
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
			if p.SkippedMetrics[metricPath] {
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

func calculateCommitPercent(memTotalKBStr string, commitKBStr string) (float64, bool) {
	memTotalKB, err := strconv.ParseFloat(memTotalKBStr, 64)
	if err != nil {
		log.Logger.Debugw("Error while parsing mem total kb to float64", "memTotalKBStr", memTotalKBStr, "error", err)
		return 0, false
	}
	commitKB, err := strconv.ParseFloat(commitKBStr, 64)
	if err != nil {
		log.Logger.Debugw("Error while parsing commit kb to float64", "commitKBStr", commitKBStr, "error", err)
		return 0, false
	}
	if memTotalKB == 0 {
		log.Logger.Debugw("Mem total kb is 0. Division by zero error")
		return 0, false
	}
	return (commitKB / memTotalKB) * 100, true
}

func createLinuxOsMetrics(p *LinuxOsMetricsInstanceProperties, metricPath string, labels map[string]string, val float64) *mrpb.TimeSeries {
	ts := timeseries.Params{
		CloudProp:    protostruct.ConvertCloudPropertiesToStruct(p.Config.CloudProperties),
		MetricType:   metricURL + metricPath,
		MetricLabels: labels,
		Timestamp:    tspb.Now(),
		Float64Value: val,
	}
	log.Logger.Debugw("Creating metric for instance", "metric", metricPath, "value", val, "instancenumber", p.SAPInstance.GetInstanceNumber(), "labels", labels)
	return timeseries.BuildFloat64(ts)
}
