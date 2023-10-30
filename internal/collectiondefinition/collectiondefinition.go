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

// Package collectiondefinition provides utility functions for managing the
// CollectionDefinition protobuf objects that are used to define a set of
// configurable metrics that are collected by the Agent for SAP.
package collectiondefinition

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/heartbeat"
	"github.com/GoogleCloudPlatform/sapagent/internal/usagemetrics"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	cpb "github.com/GoogleCloudPlatform/sapagent/protos/configuration"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const (
	// LinuxConfigPath is the path to the customer-defined collection definition file on Linux.
	LinuxConfigPath = `/etc/google-cloud-sap-agent/collection-definition.json`
	// WindowsConfigPath is the path to the customer-defined collection definition file on Windows.
	WindowsConfigPath = `C:\Program Files\Google\google-cloud-sap-agent\conf\collection-definition.json`
)

// A metricsMap can be used to uniquely identify a full set of metrics in
// a workload validation definition. The map is keyed by metric type and label,
// and each entry contains an inner map which specifies the OSVendor values
// which apply for that metric.
//
// In the following example, a metric with type "foo" and label "bar" has
// definitions for RHEL and SLES vendors:
//
//	"foo:bar": map[cmpb.OSVendor]bool{
//	  cmpb.OSVendor_RHEL: true
//	  cmpb.OSVendor_SLES: true
//	}
type metricsMap map[string]map[cmpb.OSVendor]bool

// ReadFile abstracts the os.ReadFile function for testability.
type ReadFile func(string) ([]byte, error)

// LoadOptions define the parameters required to load a collection definition.
type LoadOptions struct {
	CollectionConfig *cpb.CollectionConfiguration
	FetchOptions     FetchOptions
	ReadFile         ReadFile
	OSType           string
	Version          string
	DisableFallback  bool
}

// StartOptions define the parameters required to call collectiondefinition.Start
type StartOptions struct {
	HeartbeatSpec *heartbeat.Spec
	LoadOptions   LoadOptions
}

// metricInfoMapper describes the structure of a map function which operates on a MetricInfo struct.
type metricInfoMapper func(*cmpb.MetricInfo, cmpb.OSVendor)

type validationMetric interface {
	GetMetricInfo() *cmpb.MetricInfo
}

// Start prepares an initial CollectionDefinition ready for use by Agent for
// SAP services, then sets up a goroutine that will perform a periodic
// refresh to load new content from an external distribution mechanism.
//
// Services that wish to keep up to date with the latest collection definition
// should supply a channel that this function will use to broadcast updates.
func Start(ctx context.Context, chs []chan<- *cdpb.CollectionDefinition, opts StartOptions) *cdpb.CollectionDefinition {
	log.CtxLogger(ctx).Info("Starting initial load of collection definition")
	cd, err := Load(ctx, opts.LoadOptions)
	if err != nil {
		// In the event of an error, log the problem that occurred but allow other
		// agent services to attempt to start up.
		id := usagemetrics.CollectionDefinitionLoadFailure
		if _, ok := err.(ValidationError); ok {
			id = usagemetrics.CollectionDefinitionValidateFailure
		}
		usagemetrics.Error(id)
		log.CtxLogger(ctx).Error(err)
		return nil
	}

	if opts.LoadOptions.CollectionConfig.GetWorkloadValidationCollectionDefinition().GetDisableFetchLatestConfig() {
		log.CtxLogger(ctx).Debug("Fetch latest config option disabled, will not periodically refresh collection definition")
		return cd
	}

	go periodicRefresh(ctx, chs, opts)
	return cd
}

// periodicRefresh sets up an indefinite loop to retrieve the latest
// CollectionDefinition configuration and broadcast the updates to a series of
// subscribed channels.
//
// The periodic loop is safeguarded by a heartbeat monitor.
func periodicRefresh(ctx context.Context, chs []chan<- *cdpb.CollectionDefinition, opts StartOptions) {
	cdFetchTicker := time.NewTicker(24 * time.Hour)
	defer cdFetchTicker.Stop()

	heartbeatTicker := opts.HeartbeatSpec.CreateTicker()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-cdFetchTicker.C:
			loadAndBroadcast(ctx, chs, opts)
		case <-heartbeatTicker.C:
			opts.HeartbeatSpec.Beat()
		case <-ctx.Done():
			log.CtxLogger(ctx).Debug("Collection definition periodic fetch cancellation requested")
			return
		}
	}
}

// loadAndBroadcast fetches a collection definition and broadcasts the result
// to a series of subscribed channels.
//
// The 'DisableFallback' option is enforced so that we do not overwrite with a
// default collection definition configuration.
func loadAndBroadcast(ctx context.Context, chs []chan<- *cdpb.CollectionDefinition, opts StartOptions) {
	log.CtxLogger(ctx).Info("Perform periodic refresh of collection definition configuration")
	opts.HeartbeatSpec.Beat()
	cd, err := Load(ctx, LoadOptions{
		CollectionConfig: opts.LoadOptions.CollectionConfig,
		FetchOptions:     opts.LoadOptions.FetchOptions,
		ReadFile:         opts.LoadOptions.ReadFile,
		OSType:           opts.LoadOptions.OSType,
		Version:          opts.LoadOptions.Version,
		DisableFallback:  true,
	})
	if err != nil {
		log.CtxLogger(ctx).Warnw("Failed to retrieve updated collection definition", "error", err)
		return
	}
	for _, ch := range chs {
		ch <- cd
	}
}

// FromJSONFile reads a CollectionDefinition JSON configuration file and
// unmarshals the data into a CollectionDefinition proto.
func FromJSONFile(ctx context.Context, read ReadFile, path string) (*cdpb.CollectionDefinition, error) {
	data, err := read(path)
	if errors.Is(err, fs.ErrNotExist) {
		log.CtxLogger(ctx).Infow("No collection definition file defined", "path", path)
		return nil, nil
	}
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to read collection definition file", "path", path, "error", err)
		return nil, err
	}
	cd, err := unmarshal(data)
	if err != nil {
		log.CtxLogger(ctx).Errorw("Failed to unmarshal collection definition data", "path", path, "error", err)
		return nil, err
	}
	return cd, nil
}

// Load prepares a definitive CollectionDefinition ready for use by Agent for SAP services.
//
// The process of loading a collection definition involves the following steps:
//  1. Retrieve the default CollectionDefinition for the agent.
//  2. Retrieve the configurable CollectionDefinition from the local filesystem.
//  3. Merge the two definitions, giving preference to the agent defaults.
//  4. Validate the merged CollectionDefinition and log any errors found.
func Load(ctx context.Context, opts LoadOptions) (*cdpb.CollectionDefinition, error) {
	var agentCD *cdpb.CollectionDefinition
	var err error

	if !opts.CollectionConfig.GetWorkloadValidationCollectionDefinition().GetDisableFetchLatestConfig() {
		agentCD = fetchFromGCS(ctx, opts.FetchOptions)
	}

	if agentCD == nil && !opts.DisableFallback {
		log.CtxLogger(ctx).Info("Falling back on default agent collection definition")
		agentCD, _ = unmarshal(configuration.DefaultCollectionDefinition)
	}
	if agentCD == nil {
		return nil, errors.New("Failed to load agent collection definition file")
	}

	path := LinuxConfigPath
	if opts.OSType == "windows" {
		path = WindowsConfigPath
	}
	localCD, err := FromJSONFile(ctx, opts.ReadFile, path)
	if err != nil {
		return nil, errors.New("Failed to load local collection definition file")
	}

	cd := Merge(agentCD, localCD)
	v := NewValidator(opts.Version, cd)
	v.Validate()
	if !v.Valid() {
		return nil, ValidationError{FailureCount: v.FailureCount()}
	}
	return cd, nil
}

// Merge produces a CollectionDefinition which is the result of combining a
// primary definition with additional metric fields specified in a secondary
// definition.
//
// Said another way, Merge returns a result that is similar to the secondary
// definition overlayed by the primary definition. The dimension of uniqueness
// that will be used to compare metrics between the primary and secondary
// definitions is the composite value of metric type and label. In the event
// that a particular type + label combination exists in both the primary and
// secondary definition, then the primary definition for this metric will take
// precedence, the secondary definition for this metric will be ignored, and a
// warning will be reported in the agent log file.
func Merge(primary, secondary *cdpb.CollectionDefinition) (merged *cdpb.CollectionDefinition) {
	merged = &cdpb.CollectionDefinition{
		WorkloadValidation: mergeWorkloadValidations(primary.GetWorkloadValidation(), secondary.GetWorkloadValidation()),
	}

	return filterMetrics(merged)
}

// Filter metrics to make sure we only keep metrics with valid minVersions that are not newer than the Agent Version.
func filterMetrics(cd *cdpb.CollectionDefinition) *cdpb.CollectionDefinition {
	if cd.WorkloadValidation == nil {
		return cd
	}
	if cd.WorkloadValidation.GetValidationSystem() != nil {
		cd.WorkloadValidation.GetValidationSystem().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationSystem().GetOsCommandMetrics())
		cd.WorkloadValidation.GetValidationSystem().SystemMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationSystem().GetSystemMetrics())
	}
	if cd.WorkloadValidation.GetValidationCorosync() != nil {
		cd.WorkloadValidation.GetValidationCorosync().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationCorosync().GetOsCommandMetrics())
		cd.WorkloadValidation.GetValidationCorosync().ConfigMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationCorosync().GetConfigMetrics())
	}
	if cd.WorkloadValidation.GetValidationHana() != nil {
		cd.WorkloadValidation.GetValidationHana().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationHana().GetOsCommandMetrics())
		cd.WorkloadValidation.GetValidationHana().GlobalIniMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationHana().GetGlobalIniMetrics())
		for _, m := range cd.WorkloadValidation.GetValidationHana().GetHanaDiskVolumeMetrics() {
			if m != nil {
				m.Metrics = filterBadVersionMetrics(m.GetMetrics())
			}
		}
	}
	if cd.WorkloadValidation.GetValidationNetweaver() != nil {
		cd.WorkloadValidation.GetValidationNetweaver().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationNetweaver().GetOsCommandMetrics())
	}
	if cd.WorkloadValidation.GetValidationPacemaker() != nil {
		cd.WorkloadValidation.GetValidationPacemaker().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetOsCommandMetrics())
		cd.WorkloadValidation.GetValidationPacemaker().CibBootstrapOptionMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetCibBootstrapOptionMetrics())
		if cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics() != nil {
			cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().PrimitiveMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().GetPrimitiveMetrics())
			cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().RscLocationMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().GetRscLocationMetrics())
			cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().RscOptionMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().GetRscOptionMetrics())
			cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().HanaOperationMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().GetHanaOperationMetrics())
			cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().FenceAgentMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationPacemaker().GetConfigMetrics().GetFenceAgentMetrics())
		}
	}
	if cd.WorkloadValidation.GetValidationCustom() != nil {
		cd.WorkloadValidation.GetValidationCustom().OsCommandMetrics = filterBadVersionMetrics(cd.WorkloadValidation.GetValidationCustom().GetOsCommandMetrics())
	}
	return cd
}

func filterBadVersionMetrics[M validationMetric](metrics []M) []M {
	var filteredMetrics []M
	for _, m := range metrics {
		if isValidVersion(m) {
			filteredMetrics = append(filteredMetrics, m)
		}
	}
	return filteredMetrics
}

func isValidVersion(metric validationMetric) bool {
	info := metric.GetMetricInfo()
	t := info.GetType()
	l := info.GetLabel()
	agentVersion := configuration.AgentVersion

	// A metric should not have a min_version that exceeds the agent version.
	minVersion := info.GetMinVersion()
	if minVersion == "" {
		return true
	}
	if !versionPattern.MatchString(minVersion) {
		log.Logger.Warnw("Metric minVersion is of invalid format.", "minVersion", minVersion, "type", t, "label", l)
		return false
	} else if compareVersions(minVersion, agentVersion) == 1 {
		log.Logger.Warnw("Metric minVersion exceeds the agent minVersion.", "metricVersion", minVersion, "agentVersion", agentVersion, "type", t, "label", l)
		return false
	}
	return true
}

// mergeWorkloadValidations constructs a merged workload validation definition
// that is a copy of a primary definition with additional metric fields
// specified in a secondary definition.
//
// Metrics specified in the secondary definition cannot override those in the primary definition.
func mergeWorkloadValidations(primary, secondary *wlmpb.WorkloadValidation) *wlmpb.WorkloadValidation {
	merged := &wlmpb.WorkloadValidation{}

	// Construct a map of existing metrics in the primary workload validation definition.
	existing := mapWorkloadValidationMetrics(primary)

	// Include additional metrics from the secondary definition that do not override primary metrics.
	log.Logger.Debug("Merging secondary workload validation definition.")
	merged.ValidationSystem = mergeSystemValidations(primary.GetValidationSystem(), secondary.GetValidationSystem(), existing)
	merged.ValidationCorosync = mergeCorosyncValidations(primary.GetValidationCorosync(), secondary.GetValidationCorosync(), existing)
	merged.ValidationHana = mergeHanaValidations(primary.GetValidationHana(), secondary.GetValidationHana(), existing)
	merged.ValidationNetweaver = mergeNetweaverValidations(primary.GetValidationNetweaver(), secondary.GetValidationNetweaver(), existing)
	merged.ValidationPacemaker = mergePacemakerValidations(primary.GetValidationPacemaker(), secondary.GetValidationPacemaker(), existing)
	merged.ValidationCustom = mergeCustomValidations(primary.GetValidationCustom(), secondary.GetValidationCustom(), existing)

	return merged
}

// mapWorkloadValidationMetrics constructs a map of metrics in a workload validation definition.
func mapWorkloadValidationMetrics(wlm *wlmpb.WorkloadValidation) metricsMap {
	metrics := make(metricsMap)

	mapper := func(i *cmpb.MetricInfo, v cmpb.OSVendor) {
		k := fmt.Sprintf("%s:%s", i.GetType(), i.GetLabel())
		if _, ok := metrics[k]; !ok {
			metrics[k] = make(map[cmpb.OSVendor]bool)
		}
		metrics[k][v] = true
	}

	system := wlm.GetValidationSystem()
	iterator(system.GetSystemMetrics(), mapper)
	iterator(system.GetOsCommandMetrics(), mapper)

	corosync := wlm.GetValidationCorosync()
	iterator(corosync.GetConfigMetrics(), mapper)
	iterator(corosync.GetOsCommandMetrics(), mapper)

	hana := wlm.GetValidationHana()
	iterator(hana.GetGlobalIniMetrics(), mapper)
	hanaDiskVolumes := hana.GetHanaDiskVolumeMetrics()
	for _, d := range hanaDiskVolumes {
		iterator(d.GetMetrics(), mapper)
	}
	iterator(hana.GetOsCommandMetrics(), mapper)

	netweaver := wlm.GetValidationNetweaver()
	iterator(netweaver.GetOsCommandMetrics(), mapper)

	pacemaker := wlm.GetValidationPacemaker()
	pacemakerConfig := pacemaker.GetConfigMetrics()
	iterator(pacemakerConfig.GetPrimitiveMetrics(), mapper)
	iterator(pacemakerConfig.GetRscLocationMetrics(), mapper)
	iterator(pacemakerConfig.GetRscOptionMetrics(), mapper)
	iterator(pacemakerConfig.GetHanaOperationMetrics(), mapper)
	iterator(pacemakerConfig.GetFenceAgentMetrics(), mapper)
	iterator(pacemaker.GetCibBootstrapOptionMetrics(), mapper)
	iterator(pacemaker.GetOsCommandMetrics(), mapper)

	custom := wlm.GetValidationCustom()
	iterator(custom.GetOsCommandMetrics(), mapper)

	return metrics
}

// osVendor retrieves the vendor for a given metric.
//
// The default vendor is "ALL" (i.e. the collection of the metric is vendor
// agnostic). This applies to both OSCommandMetric messages where a vendor is
// not set, and non OS-based metrics that do not have a vendor value.
func osVendor[M proto.Message](metric M) cmpb.OSVendor {
	vendor := cmpb.OSVendor_ALL
	if _, ok := metric.ProtoReflect().Interface().(*cmpb.OSCommandMetric); !ok {
		return vendor
	}
	vendorFD := metric.ProtoReflect().Descriptor().Fields().ByName("os_vendor")
	if metric.ProtoReflect().Has(vendorFD) {
		vendor = cmpb.OSVendor(int32(metric.ProtoReflect().Get(vendorFD).Enum()))
	}
	return vendor
}

// iterator loops over a slice of proto messages and invokes a mapper function.
func iterator[M proto.Message](metrics []M, mapper metricInfoMapper) {
	for _, m := range metrics {
		infoFD := m.ProtoReflect().Descriptor().Fields().ByName("metric_info")
		info := m.ProtoReflect().Get(infoFD).Message().Interface().(*cmpb.MetricInfo)
		vendor := osVendor(m)
		mapper(info, vendor)
	}
}

// mergeSystemValidations clones the primary ValidationSystem definition,
// and appends additional metrics from the secondary definition.
func mergeSystemValidations(primary, secondary *wlmpb.ValidationSystem, existing metricsMap) *wlmpb.ValidationSystem {
	merged := proto.Clone(primary).(*wlmpb.ValidationSystem)
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// mergeCorosyncValidations clones the primary ValidationCorosync definition,
// and appends additional metrics from the secondary definition.
func mergeCorosyncValidations(primary, secondary *wlmpb.ValidationCorosync, existing metricsMap) *wlmpb.ValidationCorosync {
	merged := proto.Clone(primary).(*wlmpb.ValidationCorosync)
	for _, m := range secondary.GetConfigMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.ConfigMetrics = append(merged.GetConfigMetrics(), m)
		}
	}
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// mergeHanaValidations clones the primary ValidationHANA definition,
// and appends additional metrics from the secondary definition.
func mergeHanaValidations(primary, secondary *wlmpb.ValidationHANA, existing metricsMap) *wlmpb.ValidationHANA {
	merged := proto.Clone(primary).(*wlmpb.ValidationHANA)
	for _, m := range secondary.GetGlobalIniMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.GlobalIniMetrics = append(merged.GetGlobalIniMetrics(), m)
		}
	}
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// mergeNetweaverValidations clones the primary ValidationNetweaver definition,
// and appends additional metrics from the secondary definition.
func mergeNetweaverValidations(primary, secondary *wlmpb.ValidationNetweaver, existing metricsMap) *wlmpb.ValidationNetweaver {
	merged := proto.Clone(primary).(*wlmpb.ValidationNetweaver)
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// mergePacemakerValidations clones the primary ValidationPacemaker definition,
// and appends additional metrics from the secondary definition.
func mergePacemakerValidations(primary, secondary *wlmpb.ValidationPacemaker, existing metricsMap) *wlmpb.ValidationPacemaker {
	merged := proto.Clone(primary).(*wlmpb.ValidationPacemaker)
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// mergeCustomValidations clones the primary ValidationCustom definition,
// and appends additional metrics from the secondary definition.
func mergeCustomValidations(primary, secondary *wlmpb.ValidationCustom, existing metricsMap) *wlmpb.ValidationCustom {
	merged := proto.Clone(primary).(*wlmpb.ValidationCustom)
	for _, m := range secondary.GetOsCommandMetrics() {
		if ok := shouldMerge(m, existing); ok {
			merged.OsCommandMetrics = append(merged.GetOsCommandMetrics(), m)
		}
	}
	return merged
}

// shouldMerge determines whether a given metric is eligible to be merged into
// an existing metric collection.
//
// A metric may be merged so long as it is not a duplicate of an existing metric.
func shouldMerge[M proto.Message](metric M, existing metricsMap) bool {
	infoFD := metric.ProtoReflect().Descriptor().Fields().ByName("metric_info")
	info := metric.ProtoReflect().Get(infoFD).Message().Interface().(*cmpb.MetricInfo)
	t := info.GetType()
	l := info.GetLabel()
	k := fmt.Sprintf("%s:%s", t, l)

	if vendors, ok := existing[k]; ok {
		vendor := osVendor(metric)
		_, hasVendor := vendors[vendor]
		_, hasAll := vendors[cmpb.OSVendor_ALL]
		isAll := vendor == cmpb.OSVendor_ALL
		if hasVendor || hasAll || isAll {
			log.Logger.Warnw("Ignoring workload validation definition for metric.", "type", t, "label", l, "reason", "This metric definition is specified by the agent and cannot be overridden.")
			return false
		}
	}
	return true
}

// unmarshal encapsulates the functionality of protojson.Unmarshal and supplies
// a CollectionDefinition message.
func unmarshal(b []byte) (*cdpb.CollectionDefinition, error) {
	cd := &cdpb.CollectionDefinition{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(b, cd)); err != nil {
		return nil, err
	}
	return cd, nil
}
