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
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

const (
	// LinuxConfigPath is the path to the customer-defined collection definition file on Linux.
	LinuxConfigPath = `/etc/google-cloud-sap-agent/collection-definition.json`
	// WindowsConfigPath is the path to the customer-defined collection definition file on Windows.
	WindowsConfigPath = `C:\Program Files\Google\google-cloud-sap-agent\conf\collection-definition.json`
)

// versionPattern matches version strings of type: MAJOR.MINOR or MAJOR.MINOR.RELEASE.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

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

// ReadFile abstracts the os.ReadFile function for testability.
type ReadFile func(string) ([]byte, error)

// LoadOptions define the parameters required to load a collection definition.
type LoadOptions struct {
	ReadFile ReadFile
	OSType   string
	Version  string
}

// metricInfoMapper describes the structure of a map function which operates on a MetricInfo struct.
type metricInfoMapper func(*cmpb.MetricInfo, cmpb.OSVendor)

// FromJSONFile reads a CollectionDefinition JSON configuration file and
// unmarshals the data into a CollectionDefinition proto.
func FromJSONFile(read ReadFile, path string) (*cdpb.CollectionDefinition, error) {
	data, err := read(path)
	if errors.Is(err, fs.ErrNotExist) {
		log.Logger.Infow("No collection definition file defined", "path", path)
		return nil, nil
	}
	if err != nil {
		log.Logger.Errorw("Failed to read collection definition file", "path", path, "error", err)
		return nil, err
	}
	cd, err := unmarshal(data)
	if err != nil {
		log.Logger.Errorw("Failed to unmarshal collection definition data", "path", path, "error", err)
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
func Load(opts LoadOptions) (*cdpb.CollectionDefinition, error) {
	agentCD, err := unmarshal(configuration.DefaultCollectionDefinition)
	if err != nil {
		return nil, errors.New("Failed to load agent collection definition file")
	}

	path := LinuxConfigPath
	if opts.OSType == "windows" {
		path = WindowsConfigPath
	}
	localCD, err := FromJSONFile(opts.ReadFile, path)
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
	return merged
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

// unmarshal encapsulates the functionality of protojson.Unmarshal and supplies
// a CollectionDefinition message.
func unmarshal(b []byte) (*cdpb.CollectionDefinition, error) {
	cd := &cdpb.CollectionDefinition{}
	if err := protojson.Unmarshal(b, cd); err != nil {
		return nil, err
	}
	return cd, nil
}
