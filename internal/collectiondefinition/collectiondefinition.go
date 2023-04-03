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
	"regexp"
	"strconv"

	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

// A Validator holds information necessary for validating a CollectionDefinition.
//
// A Validator should be instantiated using NewValidator() and supplying the
// agent version and a CollectionDefinition to be validated. Validation is
// performed by invoking the Validate() method, and the state of the validation
// can be obtained by invoking the Valid() method to get a boolean result, or
// the FailureCount() method to get a total number of validation failures.
// Information regarding each validation failure is output to a log file.
type Validator struct {
	agentVersion              float64
	collectionDefinition      *cdpb.CollectionDefinition
	failureCount              int
	valid                     bool
	workloadValidationMetrics map[string]map[cmpb.OSVendor]bool
}

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

// mergeWorkloadValidations constructs a merged workload validation definition
// that is a copy of a primary definition with additional metric fields
// specified in a secondary definition.
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

// NewValidator instantiates Validator for a CollectionDefinition.
func NewValidator(version float64, collectionDefinition *cdpb.CollectionDefinition) *Validator {
	return &Validator{
		agentVersion:              version,
		collectionDefinition:      collectionDefinition,
		failureCount:              0,
		valid:                     false,
		workloadValidationMetrics: make(map[string]map[cmpb.OSVendor]bool),
	}
}

// FailureCount returns the number of validation failures in the CollectionDefinition.
func (v Validator) FailureCount() int {
	return v.failureCount
}

// Valid returns true if the CollectionDefinition has been successfully validated, false otherwise.
func (v Validator) Valid() bool {
	return v.valid
}

// Validate checks that a CollectionDefinition is well-formed. (i.e. are there
// any issues in the definition which would interfere with metric collection.)
//
// The validations performed in this function will be aimed at preventing
// runtime errors in the agent. Any issues found during validation will be
// reported in the agent log file. The following validations will be performed:
//
//   - Each metric should have a type and a label specified.
//   - Metric type+label should be unique for all metrics within the definition.
//   - An OSCommandMetric may reuse a metric type+label if os_vendor is distinct for each entry.
//   - A metric should not have a min_version that exceeds the agent version.
//   - Regular expressions should compile.
//   - A metric should always provide accompanying evaluation rule(s).
//   - An evaluation rule should always provide a value to use if true.
//   - An OSCommandMetric should always provide a command to run.
//   - An XPathMetric should always provide an xpath to evaluate.
func (v *Validator) Validate() {
	// Reset validation state.
	v.valid = true
	v.failureCount = 0
	v.workloadValidationMetrics = make(map[string]map[cmpb.OSVendor]bool)

	cd := v.collectionDefinition
	workload := cd.GetWorkloadValidation()
	if workload != nil {
		v.validateWorkloadValidation(workload)
	}
}

// validateWorkloadValidation runs a series of validation checks against a
// WorkloadValidation proto definition.
func (v *Validator) validateWorkloadValidation(wlm *wlmpb.WorkloadValidation) {
	system := wlm.GetValidationSystem()
	for _, m := range system.GetSystemMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.SystemVariable_SYSTEM_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "SystemVariable metric has no value specified")
		}
	}
	v.validateOSCommandMetrics(system.GetOsCommandMetrics())

	corosync := wlm.GetValidationCorosync()
	v.validateEvalMetrics(corosync.GetConfigMetrics())
	v.validateOSCommandMetrics(corosync.GetOsCommandMetrics())

	hana := wlm.GetValidationHana()
	v.validateEvalMetrics(hana.GetGlobalIniMetrics())
	for _, d := range hana.GetHanaDiskVolumeMetrics() {
		for _, m := range d.GetMetrics() {
			validateMetricInfo(v, m)
			if m.GetValue() == wlmpb.DiskVariable_DISK_VARIABLE_UNSPECIFIED {
				validationFailure(v, m, "DiskVariable metric has no value specified")
			}
		}
	}
	v.validateOSCommandMetrics(hana.GetOsCommandMetrics())

	netweaver := wlm.GetValidationNetweaver()
	v.validateOSCommandMetrics(netweaver.GetOsCommandMetrics())

	pacemaker := wlm.GetValidationPacemaker()
	v.validatePacemakerConfigMetrics(pacemaker.GetConfigMetrics())
	v.validateOSCommandMetrics(pacemaker.GetOsCommandMetrics())

	custom := wlm.GetValidationCustom()
	v.validateOSCommandMetrics(custom.GetOsCommandMetrics())
}

// validatePacemakerConfigMetrics runs a series of validation checks against a
// PacemakerConfigMetrics proto definition.
func (v *Validator) validatePacemakerConfigMetrics(config *wlmpb.PacemakerConfigMetrics) {
	for _, m := range config.GetPrimitiveMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.PrimitiveVariable_PRIMITIVE_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "PrimitiveVariable metric has no value specified")
		}
	}
	for _, m := range config.GetRscLocationMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.RSCLocationVariable_RSC_LOCATION_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "RSCLocationVariable metric has no value specified")
		}
	}
	for _, m := range config.GetRscOptionMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.RSCOptionVariable_RSC_OPTION_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "RSCOptionVariable metric has no value specified")
		}
	}
	for _, m := range config.GetHanaOperationMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.HANAOperationVariable_HANA_OPERATION_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "HANAOperationVariable metric has no value specified")
		}
	}
	for _, m := range config.GetFenceAgentMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.FenceAgentVariable_FENCE_AGENT_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "FenceAgentVariable metric has no value specified")
		}
	}
	v.validateXPathMetrics(config.GetXpathMetrics())
}

// validateEvalMetrics runs a series of validation checks against an EvalMetric slice.
func (v *Validator) validateEvalMetrics(metrics []*cmpb.EvalMetric) {
	for _, m := range metrics {
		validateMetricInfo(v, m)

		// A metric should always provide accompanying evaluation rule(s).
		validateMetricEvaluation(v, m)
	}
}

// validateOSCommandMetrics runs a series of validation checks against an OSCommandMetric slice.
func (v *Validator) validateOSCommandMetrics(metrics []*cmpb.OSCommandMetric) {
	for _, m := range metrics {
		validateMetricInfo(v, m)

		// An OSCommandMetric should always provide a command to run.
		if m.GetCommand() == "" {
			validationFailure(v, m, "OSCommandMetric has no command to run")
		}

		// A metric should always provide accompanying evaluation rule(s).
		validateMetricEvaluation(v, m)
	}
}

// validateXpathMetrics runs a series of validation checks against an XPathMetric slice.
func (v *Validator) validateXPathMetrics(metrics []*cmpb.XPathMetric) {
	for _, m := range metrics {
		validateMetricInfo(v, m)

		// An XPathMetric should always provide an xpath to evaluate.
		if m.GetXpath() == "" {
			validationFailure(v, m, "XPathMetric has no xpath to evaluate")
		}

		// A metric should always provide accompanying evaluation rule(s).
		validateMetricEvaluation(v, m)
	}
}

// validateMetricInfo runs a series of validation checks against the MetricInfo
// field of a given metric, using the supplied Validator to keep track of the
// validation state.
func validateMetricInfo[M proto.Message](v *Validator, metric M) {
	infoFD := metric.ProtoReflect().Descriptor().Fields().ByName("metric_info")
	info := metric.ProtoReflect().Get(infoFD).Message().Interface().(*cmpb.MetricInfo)

	// A metric should not have a min_version that exceeds the agent version.
	minVersion := info.GetMinVersion()
	if minVersion != "" {
		version, err := strconv.ParseFloat(minVersion, 64)
		if err != nil {
			validationFailure(v, metric, fmt.Sprintf("Metric min_version of %s is invalid", minVersion))
		} else if version > v.agentVersion {
			validationFailure(v, metric, fmt.Sprintf("Metric min_version %g which exceeds agent version %g", version, v.agentVersion))
		}
	}

	// Each metric should have a type and a label specified.
	metricType := info.GetType()
	if metricType == "" {
		validationFailure(v, metric, "Metric has no type specified")
	}
	label := info.GetLabel()
	if label == "" {
		validationFailure(v, metric, "Metric has no label specified")
	}

	// Metric type+label should be unique for all metrics within the definition.
	if metricType != "" && label != "" {
		vendor := cmpb.OSVendor_ALL
		if _, ok := metric.ProtoReflect().Interface().(*cmpb.OSCommandMetric); ok {
			vendorFD := metric.ProtoReflect().Descriptor().Fields().ByName("os_vendor")
			if metric.ProtoReflect().Has(vendorFD) {
				vendor = cmpb.OSVendor(int32(metric.ProtoReflect().Get(vendorFD).Enum()))
			}
		}
		k := fmt.Sprintf("%s:%s", metricType, label)
		if vendors, ok := v.workloadValidationMetrics[k]; ok {
			// An OSCommandMetric may reuse a metric type+label if os_vendor is distinct for each entry.
			_, hasVendor := vendors[vendor]
			_, hasAll := vendors[cmpb.OSVendor_ALL]
			isAll := vendor == cmpb.OSVendor_ALL
			if hasVendor || hasAll || isAll {
				validationFailure(v, metric, "Metric has a type and label which is already defined elsewhere in the collection definition")
			}
			vendors[vendor] = true
		} else {
			v.workloadValidationMetrics[k] = map[cmpb.OSVendor]bool{
				vendor: true,
			}
		}
	}
}

// validateMetricEvaluation runs a series of validation checks against the
// various evaluation fields of a given metric, using the supplied Validator to
// keep track of the validation state.
func validateMetricEvaluation[M proto.Message](v *Validator, metric M) {
	andFD := metric.ProtoReflect().Descriptor().Fields().ByName("and_eval_rules")
	orFD := metric.ProtoReflect().Descriptor().Fields().ByName("or_eval_rules")
	if metric.ProtoReflect().Has(andFD) {
		andEval := metric.ProtoReflect().Get(andFD).Message().Interface().(*cmpb.EvalMetricRule)
		validateEvalMetricRule(v, andEval, metric)
	} else if metric.ProtoReflect().Has(orFD) {
		orEvals := metric.ProtoReflect().Get(orFD).Message().Interface().(*cmpb.OrEvalMetricRule)
		for _, orEval := range orEvals.GetOrEvalRules() {
			validateEvalMetricRule(v, orEval, metric)
		}
	} else {
		validationFailure(v, metric, "Metric has no evaluation rules")
	}
}

// validateEvalMetricRule runs a series of validation checks against the
// EvalMetricRule field of a given metric, using the supplied Validator to keep
// track of the validation state.
func validateEvalMetricRule[M proto.Message](v *Validator, eval *cmpb.EvalMetricRule, metric M) {
	if len(eval.GetEvalRules()) == 0 {
		validationFailure(v, metric, "Metric has no evaluation rules")
	}

	// An evaluation rule should always provide a value to use if true.
	ifTrue := eval.GetIfTrue()
	if ifTrue == nil {
		validationFailure(v, metric, "Metric has an evaluation rule that does not specify a value to set if true")
	}

	// Regular expressions should compile.
	regexIfTrue := ifTrue.GetValueFromRegex()
	if regexIfTrue != "" {
		if _, err := regexp.Compile(regexIfTrue); err != nil {
			validationFailure(v, metric, fmt.Sprintf(`Regular expression "%s" failed to compile`, regexIfTrue))
		}
	}
	regexIfFalse := eval.GetIfFalse().GetValueFromRegex()
	if regexIfFalse != "" {
		if _, err := regexp.Compile(regexIfFalse); err != nil {
			validationFailure(v, metric, fmt.Sprintf(`Regular expression "%s" failed to compile`, regexIfFalse))
		}
	}
}

// validationFailure updates the state of the Validator and logs a message to
// the agent indicating the nature of the failure.
func validationFailure[M proto.Message](v *Validator, metric M, reason string) {
	log.Logger.Warnw("Collection Definition validation failure", "reason", reason, "metric", metric)
	v.failureCount++
	v.valid = false
}
