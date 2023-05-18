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

// Package configurablemetrics handles the collection of arbitrary metric data
// in the agent, as defined by a collection definition configuration file.
package configurablemetrics

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
)

// Output holds the values of various output sources that can be evaluated.
type Output struct {
	StdOut   string
	StdErr   string
	ExitCode string
}

// FileReader abstracts loading and reading files into an io.ReadCloser object.
type FileReader func(string) (io.ReadCloser, error)

// BuildMetricMap iterates over an EvalMetric slice and returns a map of
// metric labels defaulted to an empty string value.
func BuildMetricMap(metrics []*cmpb.EvalMetric) map[string]string {
	m := make(map[string]string, len(metrics))
	for _, metric := range metrics {
		m[metric.GetMetricInfo().GetLabel()] = ""
	}
	return m
}

// CollectOSCommandMetric executes a command, evaluates the output, and returns
// the metric label and resulting value of the evaluation.
//
// If a specific os_vendor is supplied for a metric, then the command will only
// be run if the system is using the same vendor. Otherwise, the metric should
// be excluded from collection.
func CollectOSCommandMetric(ctx context.Context, m *cmpb.OSCommandMetric, exec commandlineexecutor.Execute, vendor string) (label, value string) {
	osVendor := m.GetOsVendor()
	switch {
	case osVendor == cmpb.OSVendor_RHEL && vendor != "rhel":
		log.Logger.Warnw(fmt.Sprintf("Skip metric collection, OS vendor of %q not detected for this system", cmpb.OSVendor_RHEL.String()), "vendor", vendor, "metric", m)
		return "", ""
	case osVendor == cmpb.OSVendor_SLES && vendor != "sles":
		log.Logger.Warnw(fmt.Sprintf("Skip metric collection, OS vendor of %q not detected for this system", cmpb.OSVendor_SLES.String()), "vendor", vendor, "metric", m)
		return "", ""
	}

	result := exec(ctx, commandlineexecutor.Params{
		Executable: m.GetCommand(),
		Args:       m.GetArgs(),
	})

	label = m.GetMetricInfo().GetLabel()
	value, _ = Evaluate(m, Output{StdOut: result.StdOut, StdErr: result.StdErr, ExitCode: strconv.Itoa(result.ExitCode)})
	return label, value
}

// CollectMetricsFromFile scans a configuration file and returns a map
// of collected metric values, keyed by metric label.
func CollectMetricsFromFile(reader FileReader, path string, metrics []*cmpb.EvalMetric) map[string]string {
	labels := BuildMetricMap(metrics)
	if len(metrics) == 0 {
		return labels
	}

	file, err := reader(path)
	if err != nil {
		log.Logger.Warnw("Could not read the file", log.Error(err))
		return labels
	}
	defer file.Close()

	metricsByLabel := make(map[string]*cmpb.EvalMetric, len(metrics))
	for _, m := range metrics {
		metricsByLabel[m.GetMetricInfo().GetLabel()] = m
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(metricsByLabel) == 0 {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		for l, m := range metricsByLabel {
			v, ok := Evaluate(m, Output{StdOut: line})
			labels[l] = v
			// For a result that evaluates as true, do not attempt to collect this metric again.
			// This assumes that at most one metric will be collected per line scanned.
			if ok {
				delete(metricsByLabel, l)
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Logger.Warnw("Could not read the file", "path", path, log.Error(err))
	}

	return labels
}

// Evaluate runs a series of evaluation rules against an Output source and
// returns a derived metric value, as well as a boolean indicating whether
// the evaluation rules were resolved as true or as false.
func Evaluate[M proto.Message](metric M, output Output) (string, bool) {
	andFD := metric.ProtoReflect().Descriptor().Fields().ByName("and_eval_rules")
	orFD := metric.ProtoReflect().Descriptor().Fields().ByName("or_eval_rules")
	if metric.ProtoReflect().Has(andFD) {
		andEval := metric.ProtoReflect().Get(andFD).Message().Interface().(*cmpb.EvalMetricRule)
		return andEvaluation(andEval, output)
	} else if metric.ProtoReflect().Has(orFD) {
		orEvals := metric.ProtoReflect().Get(orFD).Message().Interface().(*cmpb.OrEvalMetricRule)
		return orEvaluation(orEvals.GetOrEvalRules(), output)
	} else {
		log.Logger.Warnw("No evaluation rules found for metric", "metric", metric)
		return "", false
	}
}

// andEvaluation returns the results of a logical AND evaluation for a metric.
//
// Each of the evaluation rules must resolve to true for the evaluation result
// to be considered true. Otherwise, the evaluation result will be reported as
// false.
func andEvaluation(eval *cmpb.EvalMetricRule, output Output) (string, bool) {
	for _, rule := range eval.GetEvalRules() {
		if result := evaluateRule(rule, output); result == false {
			return evaluationResult(eval.GetIfFalse(), output), false
		}
	}
	return evaluationResult(eval.GetIfTrue(), output), true
}

// orEvaluation returns the results of a logical OR evaluation for a metric.
//
// A slice of evaluations will be tested one at a time. The first evaluation
// which resolves to true will be used to set the evaluation result. Within an
// individual evaluation, each of the evaluation rules must all resolve to true
// for the evaluation as a whole to be considered true. If none of the
// evaluations resolve to true, the result from the last evaluation will be
// used, and the evaluation will be reported as false.
func orEvaluation(evals []*cmpb.EvalMetricRule, output Output) (string, bool) {
	value := ""
	for _, eval := range evals {
		v, ok := andEvaluation(eval, output)
		if ok {
			return v, true
		}
		value = v
	}
	return value, false
}

// evaluateRule applies an evaluation rule to a given Output source and returns a boolean result.
func evaluateRule(rule *cmpb.EvalRule, output Output) bool {
	source := outputSource(output, rule.GetOutputSource())
	switch rule.GetEvalRuleTypes().(type) {
	case *cmpb.EvalRule_OutputEquals:
		return rule.GetOutputEquals() == source
	case *cmpb.EvalRule_OutputNotEquals:
		return rule.GetOutputNotEquals() != source
	case *cmpb.EvalRule_OutputLessThan:
		f, err := strconv.ParseFloat(source, 64)
		if err != nil {
			log.Logger.Warnw("Failed to parse output as float", log.Error(err))
			return false
		}
		return f < rule.GetOutputLessThan()
	case *cmpb.EvalRule_OutputLessThanOrEqual:
		f, err := strconv.ParseFloat(source, 64)
		if err != nil {
			log.Logger.Warnw("Failed to parse output as float", log.Error(err))
			return false
		}
		return f <= rule.GetOutputLessThanOrEqual()
	case *cmpb.EvalRule_OutputGreaterThan:
		f, err := strconv.ParseFloat(source, 64)
		if err != nil {
			log.Logger.Warnw("Failed to parse output as float", log.Error(err))
			return false
		}
		return f > rule.GetOutputGreaterThan()
	case *cmpb.EvalRule_OutputGreaterThanOrEqual:
		f, err := strconv.ParseFloat(source, 64)
		if err != nil {
			log.Logger.Warnw("Failed to parse output as float", log.Error(err))
			return false
		}
		return f >= rule.GetOutputGreaterThanOrEqual()
	case *cmpb.EvalRule_OutputStartsWith:
		return strings.HasPrefix(source, rule.GetOutputStartsWith())
	case *cmpb.EvalRule_OutputEndsWith:
		return strings.HasSuffix(source, rule.GetOutputEndsWith())
	case *cmpb.EvalRule_OutputContains:
		return strings.Contains(source, rule.GetOutputContains())
	case *cmpb.EvalRule_OutputNotContains:
		return !strings.Contains(source, rule.GetOutputNotContains())
	default:
		log.Logger.Debug("No evaluation rule detected, defaulting to false")
		return false
	}
}

// evaluationResult returns a string result value for a given Output source.
func evaluationResult(res *cmpb.EvalResult, output Output) string {
	source := outputSource(output, res.GetOutputSource())

	switch res.GetEvalResultTypes().(type) {
	case *cmpb.EvalResult_ValueFromLiteral:
		return res.GetValueFromLiteral()
	case *cmpb.EvalResult_ValueFromOutput:
		return source
	case *cmpb.EvalResult_ValueFromRegex:
		pattern, err := regexp.Compile(res.GetValueFromRegex())
		if err != nil {
			log.Logger.Warnw("Regular Expression failed to compile", log.Error(err))
			return ""
		}
		// Return the first capture group found in a regular expression match,
		// or the full match string if no capture groups are specified.
		match := pattern.FindStringSubmatch(source)
		if len(match) > 1 {
			return match[1]
		} else if len(match) > 0 {
			return match[0]
		}
		return ""
	default:
		log.Logger.Debug("No evaluation result detected, defaulting to empty string.")
		return ""
	}
}

// outputSource returns the selected value to use from an Output struct.
//
// If no source is specified, the output will be defaulted to the value in Output.StdOut.
func outputSource(output Output, source cmpb.OutputSource) string {
	switch source {
	case cmpb.OutputSource_STDOUT:
		return output.StdOut
	case cmpb.OutputSource_STDERR:
		return output.StdErr
	case cmpb.OutputSource_EXIT_CODE:
		return output.ExitCode
	default:
		return output.StdOut
	}
}
