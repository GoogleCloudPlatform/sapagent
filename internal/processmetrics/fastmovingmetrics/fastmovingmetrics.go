/*
Copyright 2023 Google LLC

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

// Package fastmovingmetrics collects the availability metrics sap/hana/availability,
// sap/hana/ha/availability, sap/nw/availability.
package fastmovingmetrics

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/GoogleCloudPlatform/sapagent/internal/processmetrics/sapcontrol"
	"github.com/GoogleCloudPlatform/sapagent/internal/sapcontrolclient"
	"github.com/GoogleCloudPlatform/sapagent/internal/system/sapdiscovery"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/cloudmonitoring"
	"github.com/GoogleCloudPlatform/sapagent/shared/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
	"github.com/GoogleCloudPlatform/sapagent/shared/metricevents"
	"github.com/GoogleCloudPlatform/sapagent/shared/timeseries"

	mrpb "google.golang.org/genproto/googleapis/monitoring/v3"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
	cnfpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	sapb "github.com/GoogleCloudPlatform/sapagent/protos/sapapp"
)

type (

	// InstanceProperties has necessary context for Metrics collection.
	// InstanceProperties implements Collector interface for HANA and Netweaver.
	InstanceProperties struct {
		SAPInstance       *sapb.SAPInstance
		Config            *cnfpb.Configuration
		Client            cloudmonitoring.TimeSeriesCreator
		SkippedMetrics    map[string]bool
		PMBackoffPolicy   backoff.BackOffContext
		ReliabilityMetric bool
		ReplicationConfig sapdiscovery.ReplicationConfig
	}
)

// HANA HA replication: Any code from 10-15 is a valid return code. Anything else needs to be treated as failure.
// Ref: go/hana-ha-replication-codes
const (
	replicationOff             int64 = 10
	replicationConnectionError int64 = 11
	replicationUnknown         int64 = 12
	replicationInitialization  int64 = 13
	replicationSyncing         int64 = 14
	replicationActive          int64 = 15
)

// HANA HA availability.
const (
	unknownState                          int64 = 0
	currentNodeSecondary                  int64 = 1
	primaryHasError                       int64 = 2
	primaryOnlineReplicationNotFunctional int64 = 3
	primaryOnlineReplicationRunning       int64 = 4
)

// SAP control results.
const (
	sapControlAllProcessesRunning = 3
	sapControlAllProcessesStopped = 4
)

// HANA system availability.
const (
	systemAtLeastOneProcessNotGreen = 0
	systemAllProcessesGreen         = 1
)

const (
	metricURL              = "workload.googleapis.com"
	pmHANAAvailabilityPath = "/sap/hana/availability"
	pmHAReplicationPath    = "/sap/hana/ha/replication"
	pmHAAvailabilityPath   = "/sap/hana/ha/availability"
	pmNWAvailabilityPath   = "/sap/nw/availability"
)

// Collect is an implementation of Collector interface from processmetrics.go for fast moving
// process metrics.
// - /sap/hana/availability
// - /sap/hana/ha/availability
// - /sap/nw/availability
// Returns a list of HANA and Netweaver related availability metrics.
func (p *InstanceProperties) Collect(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	scc := sapcontrolclient.New(p.SAPInstance.GetInstanceNumber())
	var (
		metrics []*mrpb.TimeSeries
		err     error
	)
	switch p.SAPInstance.GetType() {
	case sapb.InstanceType_HANA:
		processListParams := commandlineexecutor.Params{
			User:        p.SAPInstance.GetUser(),
			Executable:  p.SAPInstance.GetSapcontrolPath(),
			ArgsToSplit: fmt.Sprintf("-nr %s -function GetProcessList -format script", p.SAPInstance.GetInstanceNumber()),
			Env:         []string{"LD_LIBRARY_PATH=" + p.SAPInstance.GetLdLibraryPath()},
		}
		metrics, err = collectHANAAvailabilityMetrics(ctx, p, commandlineexecutor.ExecuteCommand, processListParams, scc)
	case sapb.InstanceType_NETWEAVER:
		metrics, err = collectNetWeaverMetrics(ctx, p, scc)
	}
	return metrics, err
}

// CollectWithRetry decorates the Collect method with retry mechanism.
func (p *InstanceProperties) CollectWithRetry(ctx context.Context) ([]*mrpb.TimeSeries, error) {
	var (
		attempt = 1
		res     []*mrpb.TimeSeries
	)
	err := backoff.Retry(func() error {
		select {
		case <-ctx.Done():
			log.CtxLogger(ctx).Debugw("Context cancelled, exiting CollectWithRetry", "InstanceId", p.SAPInstance.GetInstanceId())
			return nil
		default:
			var err error
			res, err = p.Collect(ctx)
			if err != nil {
				log.CtxLogger(ctx).Debugw("Error in Collection", "attempt", attempt, "error", err)
				attempt++
			}
			return err
		}
	}, p.PMBackoffPolicy)
	if err != nil {
		if p.SAPInstance.GetType() == sapb.InstanceType_NETWEAVER {
			return nil, err
		}
		if p.ReliabilityMetric {
			return nil, err
		}

		// This is a special case in which fast moving metrics should return the HANA HA Availability
		// and HA Replication metrics even after errors following retries because Smoke detector systems
		// rely on these metrics.
		extraLabels := map[string]string{
			"ha_members": strings.Join(p.SAPInstance.GetHanaHaMembers(), ","),
		}
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:    metricURL + pmHAReplicationPath,
			Message: "HA Replication",
			Value:   "0",
			Labels:  appendLabels(p, extraLabels),
		})
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:    metricURL + pmHAAvailabilityPath,
			Message: "HA Availability",
			Value:   "0",
			Labels:  appendLabels(p, nil),
		})
		now := tspb.Now()
		res = append(res, createMetrics(p, pmHAReplicationPath, extraLabels, now, 0))
		res = append(res, createMetrics(p, pmHAAvailabilityPath, nil, now, 0))
	}
	return res, nil
}

func collectHANAAvailabilityMetrics(ctx context.Context, ip *InstanceProperties, e commandlineexecutor.Execute, p commandlineexecutor.Params, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	log.CtxLogger(ctx).Debugw("Collecting HANA Availability and HA Availability metrics for instance", "instanceid", ip.SAPInstance.GetInstanceId())

	now := tspb.Now()
	sc := &sapcontrol.Properties{Instance: ip.SAPInstance}
	var (
		err               error
		sapControlResult  int
		processes         map[int]*sapcontrol.ProcessStatus
		metrics           []*mrpb.TimeSeries
		availabilityValue int64
	)
	if _, ok := ip.SkippedMetrics[pmHANAAvailabilityPath]; !ok {
		processes, err = sc.GetProcessList(ctx, scc)
		if err != nil {
			return nil, err
		}
		// If GetProcessList API didn't return an error.
		availabilityValue = hanaAvailability(ip, processes)
		mPath := pmHANAAvailabilityPath
		if ip.ReliabilityMetric {
			if availabilityValue == 0 {
				usagemetrics.Action(usagemetrics.ReliabilityHANANotAvailable)
			} else {
				usagemetrics.Action(usagemetrics.ReliabilityHANAAvailable)
			}
		} else {
			metricevents.AddEvent(ctx, metricevents.Parameters{
				Path:    mPath,
				Message: "HANA System Availability",
				Value:   strconv.FormatInt(availabilityValue, 10),
			})
			metrics = append(metrics, createMetrics(ip, mPath, nil, now, availabilityValue))
		}
	}

	skipHAAvailability := ip.SkippedMetrics[pmHAAvailabilityPath]
	skipHAReplication := ip.SkippedMetrics[pmHAReplicationPath]
	if !skipHAAvailability && !skipHAReplication {
		haReplicationValue, err := refreshHAReplicationConfig(ctx, ip)
		if err != nil {
			return nil, err
		}
		_, sapControlResult, err = sapcontrol.ExecProcessList(ctx, e, p)
		if err != nil {
			log.CtxLogger(ctx).Debugw("Error executing GetProcessList SAPControl command, failed to get exitStatus", log.Error(err))
			return nil, err
		}
		haAvailabilityValue := haAvailabilityValue(ip, int64(sapControlResult), haReplicationValue)
		extraLabels := map[string]string{
			"ha_members": strings.Join(ip.SAPInstance.GetHanaHaMembers(), ","),
		}
		if ip.ReliabilityMetric {
			if haAvailabilityValue == 0 {
				usagemetrics.Action(usagemetrics.ReliabilityHANAHANotAvailable)
			} else {
				usagemetrics.Action(usagemetrics.ReliabilityHANAHAAvailable)
			}
		} else {
			metricevents.AddEvent(ctx, metricevents.Parameters{
				Path:    metricURL + pmHAReplicationPath,
				Message: "HA Replication",
				Value:   strconv.FormatInt(int64(haReplicationValue), 10),
				Labels:  appendLabels(ip, extraLabels),
			})
			metricevents.AddEvent(ctx, metricevents.Parameters{
				Path:    metricURL + pmHAAvailabilityPath,
				Message: "HA Availability",
				Value:   strconv.FormatInt(int64(haAvailabilityValue), 10),
				Labels:  appendLabels(ip, nil),
			})
			metrics = append(metrics, createMetrics(ip, pmHAReplicationPath, extraLabels, now, haReplicationValue))
			metrics = append(metrics, createMetrics(ip, pmHAAvailabilityPath, nil, now, haAvailabilityValue))
		}
	}

	log.CtxLogger(ctx).Debugw("Time taken to collect metrics in CollectReplicationHA()", "duration", time.Since(now.AsTime()))
	return metrics, nil
}

func hanaAvailability(p *InstanceProperties, processes map[int]*sapcontrol.ProcessStatus) (value int64) {
	if len(processes) == 0 {
		return 0
	}
	value = systemAllProcessesGreen
	for _, process := range processes {
		if !process.IsGreen {
			value = systemAtLeastOneProcessNotGreen
			break
		}
	}
	return value
}

func haAvailabilityValue(p *InstanceProperties, sapControlResult int64, replicationStatus int64) int64 {
	var value int64 = unknownState
	switch replicationStatus {
	case replicationActive:
		if sapControlResult == sapControlAllProcessesRunning {
			value = primaryOnlineReplicationRunning
		} else {
			value = primaryHasError
		}
	case replicationUnknown:
		if p.SAPInstance.GetSite() == sapb.InstanceSite_HANA_PRIMARY {
			if sapControlResult == sapControlAllProcessesRunning {
				value = primaryOnlineReplicationNotFunctional
			} else {
				value = primaryHasError
			}
		} else {
			value = currentNodeSecondary
		}
	case replicationOff, replicationConnectionError, replicationInitialization, replicationSyncing:
		if sapControlResult == sapControlAllProcessesRunning {
			value = primaryOnlineReplicationNotFunctional
		} else {
			value = primaryHasError
		}
	default:
		log.Logger.Warn("HANA HA availability state unknown.")
	}
	log.Logger.Debugw("HANA HA availability for sapcontrol",
		"availability", value, "returncode", sapControlResult, "replicationcode", replicationStatus)
	return value
}

func refreshHAReplicationConfig(ctx context.Context, p *InstanceProperties) (int64, error) {
	mode, haMembers, replicationStatus, _, err := p.ReplicationConfig(
		ctx,
		p.SAPInstance.GetUser(),
		p.SAPInstance.GetSapsid(),
		p.SAPInstance.GetInstanceId())

	// This is not in-band error handling. Metric should be zero in case of failures.
	if err != nil {
		log.CtxLogger(ctx).Debugw("Failed to refresh HANA HA Replication config for instance", "instanceid", p.SAPInstance.GetInstanceId())
		return 0, err
	}

	p.SAPInstance.Site = sapdiscovery.HANASite(mode)
	p.SAPInstance.HanaHaMembers = haMembers
	return int64(replicationStatus), nil
}

// collectNetWeaverMetrics builds a slice of SAP metrics containing all relevant NetWeaver metrics
func collectNetWeaverMetrics(ctx context.Context, p *InstanceProperties, scc sapcontrol.ClientInterface) ([]*mrpb.TimeSeries, error) {
	if _, ok := p.SkippedMetrics[pmNWAvailabilityPath]; ok {
		return nil, nil
	}
	now := tspb.Now()
	sc := &sapcontrol.Properties{Instance: p.SAPInstance}
	procs, err := sc.GetProcessList(ctx, scc)
	if err != nil {
		log.CtxLogger(ctx).Debugw("Error performing GetProcessList web method", log.Error(err))
		return nil, err
	}

	var metrics []*mrpb.TimeSeries
	availabilityValue := collectNWAvailability(p, procs)
	if p.ReliabilityMetric {
		if availabilityValue == 0 {
			usagemetrics.Action(usagemetrics.ReliabilitySAPNWNotAvailable)
		} else {
			usagemetrics.Action(usagemetrics.ReliabilitySAPNWAvailable)
		}
	} else {
		metricevents.AddEvent(ctx, metricevents.Parameters{
			Path:    pmNWAvailabilityPath,
			Message: "NetWeaver Availability",
			Value:   strconv.FormatInt(availabilityValue, 10),
		})
		metrics = append(metrics, createMetrics(p, pmNWAvailabilityPath, nil, now, availabilityValue))
	}
	return metrics, nil
}

func collectNWAvailability(p *InstanceProperties, procs map[int]*sapcontrol.ProcessStatus) (availabilityValue int64) {
	start := tspb.Now()
	availabilityValue = systemAllProcessesGreen

	processNames := []string{"msg_server", "enserver", "enrepserver", "disp+work", "gwrd", "icman", "jstart", "jcontrol", "enq_replicator", "enq_server", "sapwebdisp"}
	for _, proc := range procs {
		if contains(processNames, proc.Name) && !proc.IsGreen {
			availabilityValue = systemAtLeastOneProcessNotGreen
		}
	}
	log.Logger.Debugw("Time taken to collect metrics in collectServiceMetrics()", "time", time.Since(start.AsTime()))
	return availabilityValue
}

// createMetrics - create mrpb.TimeSeries object for the given metric.
func createMetrics(p *InstanceProperties, mPath string, extraLabels map[string]string, now *tspb.Timestamp, val int64) *mrpb.TimeSeries {
	mLabels := appendLabels(p, extraLabels)
	params := timeseries.Params{
		CloudProp:    p.Config.CloudProperties,
		MetricType:   metricURL + mPath,
		MetricLabels: mLabels,
		Timestamp:    now,
		Int64Value:   val,
		BareMetal:    p.Config.BareMetal,
	}
	log.Logger.Debugw("Create metric for instance", "key", mPath, "value", val, "instanceid", p.SAPInstance.GetInstanceId(), "labels", mLabels)
	return timeseries.BuildInt(params)
}

// appendLabels appends the default SAP Instance labels and extra labels
// to return a consolidated map of metric labels.
func appendLabels(p *InstanceProperties, extraLabels map[string]string) map[string]string {
	defaultLabels := map[string]string{
		"sid":         p.SAPInstance.GetSapsid(),
		"instance_nr": p.SAPInstance.GetInstanceNumber(),
	}
	for k, v := range extraLabels {
		defaultLabels[k] = v
	}
	return defaultLabels
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}
