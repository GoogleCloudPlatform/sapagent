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

package collectiondefinition

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/sharedprotos/configurablemetrics"
)

// versionPattern matches version strings of type: MAJOR.MINOR or MAJOR.MINOR.RELEASE.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// A Validator holds information necessary for validating a CollectionDefinition.
//
// A Validator should be instantiated using NewValidator() and supplying the
// agent version and a CollectionDefinition to be validated. Validation is
// performed by invoking the Validate() method, and the state of the validation
// can be obtained by invoking the Valid() method to get a boolean result, or
// the FailureCount() method to get a total number of validation failures.
// Information regarding each validation failure is output to a log file.
type Validator struct {
	agentVersion              string
	collectionDefinition      *cdpb.CollectionDefinition
	failureCount              int
	valid                     bool
	workloadValidationMetrics metricsMap
}

// A ValidationError should be thrown when validation of a CollectionDefinition fails.
type ValidationError struct {
	FailureCount int
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("Validation of the Agent for SAP configurable collection definition uncovered %d total issues.", e.FailureCount)
}

// NewValidator instantiates Validator for a CollectionDefinition.
func NewValidator(version string, collectionDefinition *cdpb.CollectionDefinition) *Validator {
	return &Validator{
		agentVersion:              version,
		collectionDefinition:      collectionDefinition,
		failureCount:              0,
		valid:                     false,
		workloadValidationMetrics: make(metricsMap),
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
func (v *Validator) Validate() {
	// Reset validation state.
	v.valid = true
	v.failureCount = 0
	v.workloadValidationMetrics = make(metricsMap)

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
	for _, m := range hana.GetHanaBackupMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.HANABackupVariable_HANA_BACKUP_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "HANABackupVariable metric has no value specified")
		}
	}
	for _, m := range hana.GetHaMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.HANAHighAvailabilityVariable_HA_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "HANAHighAvailabilityVariable metric has no value specified")
		}
	}
	for _, m := range hana.GetDrMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.HANADisasterRecoveryVariable_DR_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "HANADisasterRecoveryVariable metric has no value specified")
		}
	}
	v.validateOSCommandMetrics(hana.GetOsCommandMetrics())

	netweaver := wlm.GetValidationNetweaver()
	v.validateOSCommandMetrics(netweaver.GetOsCommandMetrics())

	pacemaker := wlm.GetValidationPacemaker()
	v.validatePacemakerConfigMetrics(pacemaker.GetConfigMetrics())
	for _, m := range pacemaker.GetCibBootstrapOptionMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.CIBBootstrapOptionVariable_CIB_BOOTSTRAP_OPTION_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "CIBBootstrapOptionVariable metric has no value specified")
		}
	}
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
	for _, m := range config.GetAscsMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.ASCSVariable_ASCS_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "ASCSVariable metric has no value specified")
		}
	}
	for _, m := range config.GetOpOptionMetrics() {
		validateMetricInfo(v, m)
		if m.GetValue() == wlmpb.OPOptionVariable_OP_OPTION_VARIABLE_UNSPECIFIED {
			validationFailure(v, m, "OPOptionVariable metric has no value specified")
		}
	}
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

// validateMetricInfo runs a series of validation checks against the MetricInfo
// field of a given metric, using the supplied Validator to keep track of the
// validation state.
func validateMetricInfo[M proto.Message](v *Validator, metric M) {
	infoFD := metric.ProtoReflect().Descriptor().Fields().ByName("metric_info")
	info := metric.ProtoReflect().Get(infoFD).Message().Interface().(*cmpb.MetricInfo)

	// A metric should not have a min_version that exceeds the agent version.
	minVersion := info.GetMinVersion()
	if minVersion != "" {
		if !versionPattern.MatchString(minVersion) {
			validationFailure(v, metric, fmt.Sprintf("Metric min_version of %q is invalid", minVersion))
		} else if c := compareVersions(minVersion, v.agentVersion); c == 1 {
			validationFailure(v, metric, fmt.Sprintf("Metric min_version %q which exceeds agent version %q", minVersion, v.agentVersion))
		}
	}

	// Each metric should have a type and a label specified.
	shouldReturn := false
	metricType := info.GetType()
	if metricType == "" {
		validationFailure(v, metric, "Metric has no type specified")
		shouldReturn = true
	}
	label := info.GetLabel()
	if label == "" {
		validationFailure(v, metric, "Metric has no label specified")
		shouldReturn = true
	}

	if shouldReturn {
		return
	}

	// Metric type+label should be unique for all metrics within the definition.
	vendor := osVendor(metric)
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

func compareVersions(a, b string) int {
	aSplit := strings.Split(a, ".")
	bSplit := strings.Split(b, ".")
	for len(aSplit) < len(bSplit) {
		aSplit = append(aSplit, "0")
	}
	for len(bSplit) < len(aSplit) {
		bSplit = append(bSplit, "0")
	}
	for i := 0; i < len(aSplit); i++ {
		// Suppressing errors because these version numbers should already be sanitized.
		aNum, _ := strconv.Atoi(aSplit[i])
		bNum, _ := strconv.Atoi(bSplit[i])
		if aNum < bNum {
			return -1
		} else if aNum > bNum {
			return 1
		}
	}
	return 0
}

// validationFailure updates the state of the Validator and logs a message to
// the agent indicating the nature of the failure.
func validationFailure[M proto.Message](v *Validator, metric M, reason string) {
	log.Logger.Warnw("Collection Definition validation failure", "reason", reason, "metric", metric)
	v.failureCount++
	v.valid = false
}
