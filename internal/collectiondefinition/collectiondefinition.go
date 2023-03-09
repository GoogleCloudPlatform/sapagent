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
	"fmt"

	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

type metricInfoMapper func(i *cmpb.MetricInfo)

// iterator loops over a slice of proto messages and invokes a metric info mapper function.
func iterator[M proto.Message](metrics []M, mapper metricInfoMapper) {
	for _, m := range metrics {
		fd := m.ProtoReflect().Descriptor().Fields().ByName("metric_info")
		mapper(m.ProtoReflect().Get(fd).Message().Interface().(*cmpb.MetricInfo))
	}
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
	return merged
}

// mergeWorkloadValidations constructs a merged workload validation definition that is a copy of
// a primary definition with additional metric fields specified in a secondary definition.
//
// Metrics specified in the secondary definition cannot override those in the primary definition.
func mergeWorkloadValidations(primary, secondary *wlmpb.WorkloadValidation) *wlmpb.WorkloadValidation {
	merged := proto.Clone(primary).(*wlmpb.WorkloadValidation)

	// Construct a set of existing metrics in the primary workload validation definition.
	existing := createWorkloadValidationSet(primary)

	// Include additional metrics from the secondary definition that do not override primary metrics.
	shouldMerge := func(i *cmpb.MetricInfo) bool {
		t := i.GetType()
		l := i.GetLabel()
		k := fmt.Sprintf("%s:%s", t, l)
		if ok := existing[k]; ok {
			log.Logger.Warnw("Ignoring workload validation definition for metric.", "type", t, "label", l, "reason", "This metric definition is defined by the agent and cannot be overridden.")
			return false
		}
		return true
	}
	log.Logger.Debug("Merging secondary workload validation definition.")

	system := secondary.GetValidationSystem()
	for _, m := range system.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationSystem.OsCommandMetrics = append(merged.GetValidationSystem().GetOsCommandMetrics(), m)
		}
	}

	corosync := secondary.GetValidationCorosync()
	for _, m := range corosync.GetConfigMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationCorosync.ConfigMetrics = append(merged.GetValidationCorosync().GetConfigMetrics(), m)
		}
	}
	for _, m := range corosync.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationCorosync.OsCommandMetrics = append(merged.GetValidationCorosync().GetOsCommandMetrics(), m)
		}
	}

	hana := secondary.GetValidationHana()
	for _, m := range hana.GetGlobalIniMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationHana.GlobalIniMetrics = append(merged.GetValidationHana().GetGlobalIniMetrics(), m)
		}
	}
	for _, m := range hana.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationHana.OsCommandMetrics = append(merged.GetValidationHana().GetOsCommandMetrics(), m)
		}
	}

	netweaver := secondary.GetValidationNetweaver()
	for _, m := range netweaver.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationNetweaver.OsCommandMetrics = append(merged.GetValidationNetweaver().GetOsCommandMetrics(), m)
		}
	}

	pacemaker := secondary.GetValidationPacemaker()
	pacemakerConfig := pacemaker.GetConfigMetrics()
	for _, m := range pacemakerConfig.GetXpathMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationPacemaker.ConfigMetrics.XpathMetrics = append(merged.GetValidationPacemaker().GetConfigMetrics().GetXpathMetrics(), m)
		}
	}
	for _, m := range pacemaker.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationPacemaker.OsCommandMetrics = append(merged.GetValidationPacemaker().GetOsCommandMetrics(), m)
		}
	}

	custom := secondary.GetValidationCustom()
	for _, m := range custom.GetOsCommandMetrics() {
		if ok := shouldMerge(m.GetMetricInfo()); ok {
			merged.ValidationCustom.OsCommandMetrics = append(merged.GetValidationCustom().GetOsCommandMetrics(), m)
		}
	}

	return merged
}

// createWorkloadValidationSet constructs a set of metrics in a workload validation definition.
//
// The resultant map is keyed by the metric type and label as follows: "type:label".
func createWorkloadValidationSet(wlm *wlmpb.WorkloadValidation) map[string]bool {
	metrics := make(map[string]bool)

	mapper := func(i *cmpb.MetricInfo) {
		t := i.GetType()
		l := i.GetLabel()
		k := fmt.Sprintf("%s:%s", t, l)
		metrics[k] = true
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
	iterator(pacemakerConfig.GetXpathMetrics(), mapper)
	iterator(pacemaker.GetOsCommandMetrics(), mapper)

	custom := wlm.GetValidationCustom()
	iterator(custom.GetOsCommandMetrics(), mapper)

	return metrics
}
