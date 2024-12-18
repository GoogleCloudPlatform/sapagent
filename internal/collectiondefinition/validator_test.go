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
	"testing"

	"github.com/GoogleCloudPlatform/sapagent/internal/configuration"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
	cmpb "github.com/GoogleCloudPlatform/workloadagentplatform/integration/common/shared/protos/configurablemetrics"
)

func TestValidate(t *testing.T) {
	validCollectionDefinition, err := unmarshal(testCollectionDefinition1)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}

	tests := []struct {
		name       string
		definition *cdpb.CollectionDefinition
		wantValid  bool
		wantCount  int
	}{
		{
			name:       "ValidationSuccess",
			definition: validCollectionDefinition,
			wantValid:  true,
			wantCount:  0,
		},
		{
			name:       "EmptyCollectionDefinition",
			definition: &cdpb.CollectionDefinition{},
			wantValid:  true,
			wantCount:  0,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_MinVersionInvalid",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "invalid",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_MinVersionExceedsAgentVersion",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "99.9",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_TypeMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_LabelMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_Duplicate_OSVendor_HasAll",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_RHEL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_Duplicate_OSVendor_IsAll",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_SLES,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_Duplicate_OSVendor_HasVendor",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_RHEL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								OsVendor: cmpb.OSVendor_RHEL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_MetricInfo_Duplicate_OSVendor_UNSPECIFIED",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								Command: "foo",
								Args:    []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
									Label:      "foo",
								},
								Command: "foo",
								Args:    []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_CommandMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								Args: []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_EvalRulesMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_AndEvalRulesMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_OrEvalRulesMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_OrEvalRules{
									OrEvalRules: &cmpb.OrEvalMetricRule{
										OrEvalRules: []*cmpb.EvalMetricRule{
											&cmpb.EvalMetricRule{
												IfTrue: &cmpb.EvalResult{
													OutputSource:    cmpb.OutputSource_STDOUT,
													EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_EvalMetricRule_IfTrueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_EvalMetricRule_IfTrue_RegexInvalid",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foo)bar("},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_OSCommandMetrics_EvalMetricRule_IfFalse_RegexInvalid",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							&cmpb.OSCommandMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/custom",
									Label: "foo",
								},
								OsVendor: cmpb.OSVendor_ALL,
								Command:  "foo",
								Args:     []string{"--bar"},
								EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foo(bar)"},
										},
										IfFalse: &cmpb.EvalResult{
											OutputSource:    cmpb.OutputSource_STDOUT,
											EvalResultTypes: &cmpb.EvalResult_ValueFromRegex{ValueFromRegex: "foo)bar("},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_EvalMetrics_MetricInfo_TypeMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							&cmpb.EvalMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Label:      "foo",
								},
								EvalRuleTypes: &cmpb.EvalMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_EvalMetrics_MetricInfo_LabelMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							&cmpb.EvalMetric{
								MetricInfo: &cmpb.MetricInfo{
									MinVersion: "1.0",
									Type:       "workload.googleapis.com/sap/validation/custom",
								},
								EvalRuleTypes: &cmpb.EvalMetric_AndEvalRules{
									AndEvalRules: &cmpb.EvalMetricRule{
										EvalRules: []*cmpb.EvalRule{
											&cmpb.EvalRule{
												OutputSource:  cmpb.OutputSource_STDOUT,
												EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "foobar"},
											},
										},
										IfTrue: &cmpb.EvalResult{
											EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "foobar"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationSystem_SystemMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						SystemMetrics: []*wlmpb.SystemMetric{
							&wlmpb.SystemMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/system",
									Label: "foo",
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationHana_DiskMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						HanaDiskVolumeMetrics: []*wlmpb.HANADiskVolumeMetric{
							&wlmpb.HANADiskVolumeMetric{
								Metrics: []*wlmpb.HANADiskMetric{
									&wlmpb.HANADiskMetric{
										MetricInfo: &cmpb.MetricInfo{
											Type:  "workload.googleapis.com/sap/validation/hana",
											Label: "foo",
										},
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationHana_BackupMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						HanaBackupMetrics: []*wlmpb.HANABackupMetric{
							&wlmpb.HANABackupMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/hana",
									Label: "foo",
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_PrimitiveMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							PrimitiveMetrics: []*wlmpb.PacemakerPrimitiveMetric{
								&wlmpb.PacemakerPrimitiveMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_RSCLocationMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							RscLocationMetrics: []*wlmpb.PacemakerRSCLocationMetric{
								&wlmpb.PacemakerRSCLocationMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_RSCOptionMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							RscOptionMetrics: []*wlmpb.PacemakerRSCOptionMetric{
								&wlmpb.PacemakerRSCOptionMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_HanaOperationMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							HanaOperationMetrics: []*wlmpb.PacemakerHANAOperationMetric{
								&wlmpb.PacemakerHANAOperationMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_FenceAgentMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							FenceAgentMetrics: []*wlmpb.PacemakerFenceAgentMetric{
								&wlmpb.PacemakerFenceAgentMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_ASCSMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							AscsMetrics: []*wlmpb.PacemakerASCSMetric{
								&wlmpb.PacemakerASCSMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_OPOptionMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							OpOptionMetrics: []*wlmpb.OPOptionMetric{
								&wlmpb.OPOptionMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_CIBBootstrapOptionMetrics_ValueMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						CibBootstrapOptionMetrics: []*wlmpb.CIBBootstrapOptionMetric{
							&wlmpb.CIBBootstrapOptionMetric{
								MetricInfo: &cmpb.MetricInfo{
									Type:  "workload.googleapis.com/sap/validation/pacemaker",
									Label: "foo",
								},
							},
						},
					},
				},
			},
			wantValid: false,
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := NewValidator(configuration.AgentVersion, test.definition)
			v.Validate()
			gotValid := v.Valid()
			if gotValid != test.wantValid {
				t.Errorf("Valid() got %t want %t", gotValid, test.wantValid)
			}
			gotCount := v.FailureCount()
			if gotCount != test.wantCount {
				t.Errorf("FailureCount() got %d want %d", gotCount, test.wantCount)
			}
		})
	}
}

func TestValidate_ResetValidationState(t *testing.T) {
	cd := &cdpb.CollectionDefinition{
		WorkloadValidation: &wlmpb.WorkloadValidation{
			ValidationCustom: &wlmpb.ValidationCustom{
				OsCommandMetrics: []*cmpb.OSCommandMetric{
					createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "", cmpb.OSVendor_ALL),
				},
			},
		},
	}
	// Run validation once and expect a single failure.
	v := NewValidator("1.1", cd)
	v.Validate()
	gotValid := v.Valid()
	if gotValid != false {
		t.Errorf("Valid() got %t want %t", gotValid, false)
	}
	gotCount := v.FailureCount()
	if gotCount != 1 {
		t.Errorf("FailureCount() got %d want %d", gotCount, 1)
	}
	// Correct the failure and re-run validation, expect success
	cd.WorkloadValidation.ValidationCustom.OsCommandMetrics[0].Command = "foo"
	v.Validate()
	gotValid = v.Valid()
	if gotValid != true {
		t.Errorf("Valid() got %t want %t", gotValid, false)
	}
	gotCount = v.FailureCount()
	if gotCount != 0 {
		t.Errorf("FailureCount() got %d want %d", gotCount, 0)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{
			name: "Equal1",
			a:    "1.1",
			b:    "1.1",
			want: 0,
		},
		{
			name: "Equal2",
			a:    "1.1.0",
			b:    "1.1",
			want: 0,
		},
		{
			name: "Equal3",
			a:    "1.1",
			b:    "1.1.0",
			want: 0,
		},
		{
			name: "LessThan1",
			a:    "1.9",
			b:    "1.10",
			want: -1,
		},
		{
			name: "LessThan2",
			a:    "1.1.10",
			b:    "1.2",
			want: -1,
		},
		{
			name: "GreaterThan1",
			a:    "1.10",
			b:    "1.9",
			want: 1,
		},
		{
			name: "GreaterThan2",
			a:    "1.2",
			b:    "1.1.10",
			want: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compareVersions(test.a, test.b)
			if got != test.want {
				t.Errorf("compareVersions(%s, %s) got %d want %d", test.a, test.b, got, test.want)
			}
		})
	}
}
