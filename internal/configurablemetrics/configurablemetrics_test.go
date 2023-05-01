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

package configurablemetrics

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/GoogleCloudPlatform/sapagent/internal/commandlineexecutor"

	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
)

var (
	andEvalMetricWithRules = func(rules []*cmpb.EvalRule) *cmpb.EvalMetric {
		return &cmpb.EvalMetric{
			EvalRuleTypes: &cmpb.EvalMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: rules,
					IfTrue: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Value is true"},
					},
					IfFalse: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Value is false"},
					},
				},
			},
		}
	}
	andEvalMetricIfTrue = func(ifTrue *cmpb.EvalResult) *cmpb.EvalMetric {
		return &cmpb.EvalMetric{
			MetricInfo: &cmpb.MetricInfo{
				Label: "foo",
			},
			EvalRuleTypes: &cmpb.EvalMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							OutputSource:  cmpb.OutputSource_STDOUT,
							EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
						},
					},
					IfTrue: ifTrue,
					IfFalse: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Value is false"},
					},
				},
			},
		}
	}
	defaultOSCommandMetric = func(vendor cmpb.OSVendor, source cmpb.OutputSource, output string) *cmpb.OSCommandMetric {
		return &cmpb.OSCommandMetric{
			MetricInfo: &cmpb.MetricInfo{Label: "foo"},
			OsVendor:   vendor,
			Command:    "foo",
			EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							OutputSource:  source,
							EvalRuleTypes: &cmpb.EvalRule_OutputEquals{OutputEquals: output},
						},
					},
					IfTrue: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
					},
					IfFalse: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "not foobar"},
					},
				},
			},
		}
	}
	testCommandExecute = func(stdout, stderr string, err error) commandlineexecutor.Execute {
		return func(commandlineexecutor.Params) commandlineexecutor.Result {
			exitCode := 0
			var exitErr *exec.ExitError
			if err != nil && errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
			return commandlineexecutor.Result{
				StdOut:   stdout,
				StdErr:   stderr,
				Error:    err,
				ExitCode: exitCode,
			}
		}
	}
)

func TestBuildMetricMap(t *testing.T) {
	tests := []struct {
		name    string
		metrics []*cmpb.EvalMetric
		want    map[string]string
	}{
		{
			name: "Success",
			metrics: []*cmpb.EvalMetric{
				&cmpb.EvalMetric{
					MetricInfo: &cmpb.MetricInfo{Label: "foo"},
				},
				&cmpb.EvalMetric{
					MetricInfo: &cmpb.MetricInfo{Label: "bar"},
				},
			},
			want: map[string]string{
				"foo": "",
				"bar": "",
			},
		},
		{
			name:    "EmptyMap",
			metrics: []*cmpb.EvalMetric{},
			want:    map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := BuildMetricMap(test.metrics)
			if d := cmp.Diff(test.want, got); d != "" {
				t.Errorf("BuildMetricMap(%v) mismatch (-want, +got):\n%s", test.metrics, d)
			}
		})
	}
}

func TestCollectOSCommandMetric(t *testing.T) {
	tests := []struct {
		name      string
		metric    *cmpb.OSCommandMetric
		exec      commandlineexecutor.Execute
		vendor    string
		wantLabel string
		wantValue string
	}{
		{
			name:      "OSVendor_RHEL_Mismatch",
			metric:    defaultOSCommandMetric(cmpb.OSVendor_RHEL, cmpb.OutputSource_STDOUT, "bar"),
			exec:      testCommandExecute("bar", "", nil),
			vendor:    "not_rhel",
			wantLabel: "",
			wantValue: "",
		},
		{
			name:      "OSVendor_SLES_Mismatch",
			metric:    defaultOSCommandMetric(cmpb.OSVendor_SLES, cmpb.OutputSource_STDOUT, "bar"),
			exec:      testCommandExecute("bar", "", nil),
			vendor:    "not_sles",
			wantLabel: "",
			wantValue: "",
		},
		{
			name:      "OutputSource_StdOut",
			metric:    defaultOSCommandMetric(cmpb.OSVendor_ALL, cmpb.OutputSource_STDOUT, "bar"),
			exec:      testCommandExecute("bar", "", nil),
			vendor:    "rhel",
			wantLabel: "foo",
			wantValue: "foobar",
		},
		{
			name:      "OutputSource_StdErr",
			metric:    defaultOSCommandMetric(cmpb.OSVendor_OS_VENDOR_UNSPECIFIED, cmpb.OutputSource_STDERR, "bar"),
			exec:      testCommandExecute("", "bar", nil),
			vendor:    "rhel",
			wantLabel: "foo",
			wantValue: "foobar",
		},
		{
			name:      "OutputSource_ExitCode",
			metric:    defaultOSCommandMetric(cmpb.OSVendor_ALL, cmpb.OutputSource_EXIT_CODE, "-1"),
			exec:      testCommandExecute("", "", &exec.ExitError{}),
			vendor:    "debian",
			wantLabel: "foo",
			wantValue: "foobar",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotLabel, gotValue := CollectOSCommandMetric(test.metric, test.exec, test.vendor)
			if gotLabel != test.wantLabel {
				t.Errorf("CollectOSCommandMetric() unexpected metric label, got %q want %q", gotLabel, test.wantLabel)
			}
			if gotValue != test.wantValue {
				t.Errorf("CollectOSCommandMetric() unexpected metric value, got %q want %q", gotValue, test.wantValue)
			}
		})
	}
}

func TestCollectMetricsFromFile(t *testing.T) {
	tests := []struct {
		name    string
		reader  FileReader
		path    string
		metrics []*cmpb.EvalMetric
		want    map[string]string
	}{
		{
			name: "EmptyMetrics",
			want: map[string]string{},
		},
		{
			name: "FileReadError",
			metrics: []*cmpb.EvalMetric{
				andEvalMetricIfTrue(&cmpb.EvalResult{
					EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Literal Value"},
				}),
			},
			reader: FileReader(func(data string) (io.ReadCloser, error) {
				return nil, errors.New("error")
			}),
			want: map[string]string{
				"foo": "",
			},
		},
		{
			name: "Success",
			metrics: []*cmpb.EvalMetric{
				andEvalMetricIfTrue(&cmpb.EvalResult{
					EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Literal Value"},
				}),
			},
			reader: FileReader(func(data string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(data)), nil
			}),
			path: "foobar",
			want: map[string]string{
				"foo": "Literal Value",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CollectMetricsFromFile(test.reader, test.path, test.metrics)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("CollectMetricsFromFile(%v) mismatch (-want, +got):\n%s", test.metrics, diff)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		metric     *cmpb.EvalMetric
		output     Output
		wantValue  string
		wantResult bool
	}{
		{
			name:       "EvalMetric_MissingFields",
			metric:     &cmpb.EvalMetric{},
			output:     Output{StdOut: "foobar"},
			wantValue:  "",
			wantResult: false,
		},
		{
			name: "EvalRule_OutputEquals",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputEquals{OutputEquals: "foobar"},
				},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputNotEquals",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputNotEquals{OutputNotEquals: "foobar"},
				},
			}),
			output:     Output{StdOut: "not foobar"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputLessThan",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputLessThan{OutputLessThan: 100},
				},
			}),
			output:     Output{StdOut: "99"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputLessThan_FailsToParse",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputLessThan{OutputLessThan: 100},
				},
			}),
			output:     Output{StdOut: "ninety nine"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "EvalRule_OutputLessThanOrEqual",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputLessThanOrEqual{OutputLessThanOrEqual: 100},
				},
			}),
			output:     Output{StdOut: "100"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputLessThanOrEqual_FailsToParse",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputLessThanOrEqual{OutputLessThanOrEqual: 100},
				},
			}),
			output:     Output{StdOut: "one hundred"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "EvalRule_OutputGreaterThan",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputGreaterThan{OutputGreaterThan: 99},
				},
			}),
			output:     Output{StdOut: "100"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputGreaterThan_FailsToParse",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputGreaterThan{OutputGreaterThan: 99},
				},
			}),
			output:     Output{StdOut: "one hundred"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "EvalRule_OutputGreaterThanOrEqual",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputGreaterThanOrEqual{OutputGreaterThanOrEqual: 100},
				},
			}),
			output:     Output{StdOut: "100"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputGreaterThanOrEqual_FailsToParse",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputGreaterThanOrEqual{OutputGreaterThanOrEqual: 100},
				},
			}),
			output:     Output{StdOut: "one hundred"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "EvalRule_OutputStartsWith",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputStartsWith{OutputStartsWith: "foobar"},
				},
			}),
			output:     Output{StdOut: "foobar at the start"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputEndsWith",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputEndsWith{OutputEndsWith: "foobar"},
				},
			}),
			output:     Output{StdOut: "at the end is foobar"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputContains",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
				},
			}),
			output:     Output{StdOut: "start foobar end"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_OutputNotContains",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputNotContains{OutputNotContains: "foobar"},
				},
			}),
			output:     Output{StdOut: "start foobaz end"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "EvalRule_Missing",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "EvalResult_ValueFromLiteral",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Literal Value"},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "Literal Value",
			wantResult: true,
		},
		{
			name: "EvalResult_ValueFromOutput",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource:    cmpb.OutputSource_EXIT_CODE,
				EvalResultTypes: &cmpb.EvalResult_ValueFromOutput{ValueFromOutput: true},
			}),
			output: Output{
				StdOut:   "foobar",
				ExitCode: "1",
			},
			wantValue:  "1",
			wantResult: true,
		},
		{
			name: "EvalResult_ValueFromRegex",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource:    cmpb.OutputSource_STDOUT,
				EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foobar: (\\w+)"},
			}),
			output:     Output{StdOut: "foobar: enabled"},
			wantValue:  "enabled",
			wantResult: true,
		},
		{
			name: "EvalResult_ValueFromRegex_FailsToCompile",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource:    cmpb.OutputSource_STDOUT,
				EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: ")("},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "",
			wantResult: true,
		},
		{
			name: "EvalResult_ValueFromRegex_NoSubmatch",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource:    cmpb.OutputSource_STDOUT,
				EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foobar: \\w+"},
			}),
			output:     Output{StdOut: "foobar: not enabled"},
			wantValue:  "foobar: not",
			wantResult: true,
		},
		{
			name: "EvalResult_ValueFromRegex_NoMatch",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource:    cmpb.OutputSource_STDOUT,
				EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foobar: \\d+"},
			}),
			output:     Output{StdOut: "foobar: enabled"},
			wantValue:  "",
			wantResult: true,
		},
		{
			name: "EvalResult_Missing",
			metric: andEvalMetricIfTrue(&cmpb.EvalResult{
				OutputSource: cmpb.OutputSource_STDERR,
			}),
			output: Output{
				StdOut: "foobar",
				StdErr: "foobar not found",
			},
			wantValue:  "",
			wantResult: true,
		},
		{
			name: "AndEvalRules_AllTrue",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputStartsWith{OutputStartsWith: "foo"},
				},
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputEndsWith{OutputEndsWith: "bar"},
				},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "Value is true",
			wantResult: true,
		},
		{
			name: "AndEvalRules_OneFalse",
			metric: andEvalMetricWithRules([]*cmpb.EvalRule{
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputStartsWith{OutputStartsWith: "foo"},
				},
				&cmpb.EvalRule{
					EvalRuleTypes: &cmpb.EvalRule_OutputEndsWith{OutputEndsWith: "bard"},
				},
			}),
			output:     Output{StdOut: "foobar"},
			wantValue:  "Value is false",
			wantResult: false,
		},
		{
			name: "OrEvalRules_OneTrue",
			metric: &cmpb.EvalMetric{
				EvalRuleTypes: &cmpb.EvalMetric_OrEvalRules{
					OrEvalRules: &cmpb.OrEvalMetricRule{
						OrEvalRules: []*cmpb.EvalMetricRule{
							&cmpb.EvalMetricRule{
								EvalRules: []*cmpb.EvalRule{
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDOUT,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "error"},
									},
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDERR,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "unauthorized"},
									},
								},
								IfTrue: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "First Value True"},
								},
								IfFalse: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "First Value False"},
								},
							},
							&cmpb.EvalMetricRule{
								EvalRules: []*cmpb.EvalRule{
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDOUT,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "error"},
									},
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDERR,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "not found"},
									},
								},
								IfTrue: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Second Value True"},
								},
								IfFalse: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Second Value False"},
								},
							},
						},
					},
				},
			},
			output: Output{
				StdOut: "an error occurred",
				StdErr: "foobar: not found",
			},
			wantValue:  "Second Value True",
			wantResult: true,
		},
		{
			name: "OrEvalRules_AllFalse",
			metric: &cmpb.EvalMetric{
				EvalRuleTypes: &cmpb.EvalMetric_OrEvalRules{
					OrEvalRules: &cmpb.OrEvalMetricRule{
						OrEvalRules: []*cmpb.EvalMetricRule{
							&cmpb.EvalMetricRule{
								EvalRules: []*cmpb.EvalRule{
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDOUT,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "error"},
									},
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDERR,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "unauthorized"},
									},
								},
								IfTrue: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "First Value True"},
								},
								IfFalse: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "First Value False"},
								},
							},
							&cmpb.EvalMetricRule{
								EvalRules: []*cmpb.EvalRule{
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDOUT,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "error"},
									},
									&cmpb.EvalRule{
										OutputSource:  cmpb.OutputSource_STDERR,
										EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "forbidden"},
									},
								},
								IfTrue: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Second Value True"},
								},
								IfFalse: &cmpb.EvalResult{
									EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "Second Value False"},
								},
							},
						},
					},
				},
			},
			output: Output{
				StdOut: "an error occurred",
				StdErr: "foobar: not found",
			},
			wantValue:  "Second Value False",
			wantResult: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotValue, gotResult := Evaluate(test.metric, test.output)
			if gotValue != test.wantValue {
				t.Errorf("Evaluate() returned unexpected value got %s want %s", gotValue, test.wantValue)
			}
			if gotResult != test.wantResult {
				t.Errorf("Evaluate() returned unexpcted result got %t want %t", gotResult, test.wantResult)
			}
		})
	}
}
