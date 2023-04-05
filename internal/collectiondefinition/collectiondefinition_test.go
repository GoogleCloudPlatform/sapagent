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
	_ "embed"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"

	cdpb "github.com/GoogleCloudPlatform/sapagent/protos/collectiondefinition"
	cmpb "github.com/GoogleCloudPlatform/sapagent/protos/configurablemetrics"
	wlmpb "github.com/GoogleCloudPlatform/sapagent/protos/wlmvalidation"
)

var (
	//go:embed test_data/test_collectiondefinition1.json
	testCollectionDefinition1 []byte

	createEvalMetric = func(metricType, label, contains string) *cmpb.EvalMetric {
		return &cmpb.EvalMetric{
			MetricInfo: &cmpb.MetricInfo{
				Type:  metricType,
				Label: label,
			},
			EvalRuleTypes: &cmpb.EvalMetric_OrEvalRules{
				OrEvalRules: &cmpb.OrEvalMetricRule{
					OrEvalRules: []*cmpb.EvalMetricRule{
						&cmpb.EvalMetricRule{
							EvalRules: []*cmpb.EvalRule{
								&cmpb.EvalRule{
									EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: contains},
								},
							},
							IfTrue: &cmpb.EvalResult{
								EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
							},
							IfFalse: &cmpb.EvalResult{
								EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
							},
						},
					},
				},
			},
		}
	}
	createOSCommandMetric = func(metricType, label, command string, vendor cmpb.OSVendor) *cmpb.OSCommandMetric {
		return &cmpb.OSCommandMetric{
			MetricInfo: &cmpb.MetricInfo{Type: metricType, Label: label},
			OsVendor:   vendor,
			Command:    command,
			Args:       []string{"-v"},
			EvalRuleTypes: &cmpb.OSCommandMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							OutputSource:  cmpb.OutputSource_STDOUT,
							EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "Contains Text"},
						},
					},
					IfTrue: &cmpb.EvalResult{
						OutputSource:    cmpb.OutputSource_STDOUT,
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
					},
					IfFalse: &cmpb.EvalResult{
						OutputSource:    cmpb.OutputSource_STDOUT,
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
					},
				},
			},
		}
	}
	createXPathMetric = func(metricType, label, xpath string) *cmpb.XPathMetric {
		return &cmpb.XPathMetric{
			MetricInfo: &cmpb.MetricInfo{
				Type:  metricType,
				Label: label,
			},
			Xpath: xpath,
			EvalRuleTypes: &cmpb.XPathMetric_AndEvalRules{
				AndEvalRules: &cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							EvalRuleTypes: &cmpb.EvalRule_OutputContains{OutputContains: "Contains Text"},
						},
					},
					IfTrue: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "true"},
					},
					IfFalse: &cmpb.EvalResult{
						EvalResultTypes: &cmpb.EvalResult_ValueFromLiteral{ValueFromLiteral: "false"},
					},
				},
			},
		}
	}
)

func TestMerge(t *testing.T) {
	defaultPrimaryDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(testCollectionDefinition1, defaultPrimaryDefinition)
	if err != nil {
		t.Fatalf("Failed to load collection definition. %v", err)
	}
	all := cmpb.OSVendor_ALL

	tests := []struct {
		name      string
		primary   *cdpb.CollectionDefinition
		secondary *cdpb.CollectionDefinition
		want      *cdpb.CollectionDefinition
	}{
		{
			name:      "WorkloadValidation_NoSecondaryDefinition",
			primary:   defaultPrimaryDefinition,
			secondary: nil,
			want:      defaultPrimaryDefinition,
		},
		{
			name: "WorkloadValidation_ValidationSystem_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationSystem_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_ConfigMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token2", "token2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token2", "token2"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_ConfigMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						ConfigMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/corosync", "token", "token"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCorosync_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_GlobalINIMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana2", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
							createEvalMetric("workload.googleapis.com/sap/validation/hana2", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_GlobalINIMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						GlobalIniMetrics: []*cmpb.EvalMetric{
							createEvalMetric("workload.googleapis.com/sap/validation/hana", "fast_restart", "fast_restart"),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationHANA_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "dog", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationNetweaver_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationNetweaver_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_XPathMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath", "//some/path[@id=1]"),
							},
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath2", "//some/path[@id=2]"),
							},
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath", "//some/path[@id=1]"),
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath2", "//some/path[@id=2]"),
							},
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_XPathMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath", "//some/path[@id=1]"),
							},
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath", "//some/path[@id=2]"),
							},
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								createXPathMetric("workload.googleapis.com/sap/validation/pacemaker", "xpath", "//some/path[@id=1]"),
							},
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationPacemaker_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs2", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_OSCommandMetrics_Merge",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_OSCommandMetrics_Override",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_HasAll",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", all),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_IsAll",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_SLES),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", all),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_SLES),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_HasVendor",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_RHEL),
						},
					},
				},
			},
		},
		{
			name: "WorkloadValidation_ValidationCustom_Duplicate_OSVendor_UNSPECIFIED",
			primary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar", cmpb.OSVendor_OS_VENDOR_UNSPECIFIED),
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Merge(test.primary, test.secondary)
			if d := cmp.Diff(test.want, got, protocmp.Transform()); d != "" {
				t.Errorf("Merge() mismatch (-want, +got):\n%s", d)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	defaultPrimaryDefinition := &cdpb.CollectionDefinition{}
	err := protojson.Unmarshal(testCollectionDefinition1, defaultPrimaryDefinition)
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
			definition: defaultPrimaryDefinition,
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
			name: "WorkloadValidation_ValidationPacemaker_XPathMetrics_XPathMissing",
			definition: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						ConfigMetrics: &wlmpb.PacemakerConfigMetrics{
							XpathMetrics: []*cmpb.XPathMetric{
								&cmpb.XPathMetric{
									MetricInfo: &cmpb.MetricInfo{
										Type:  "workload.googleapis.com/sap/validation/pacemaker",
										Label: "foo",
									},
									EvalRuleTypes: &cmpb.XPathMetric_AndEvalRules{
										AndEvalRules: &cmpb.EvalMetricRule{
											EvalRules: []*cmpb.EvalRule{
												&cmpb.EvalRule{
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
			},
			wantValid: false,
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agentVersion := float64(1.1)
			v := NewValidator(agentVersion, test.definition)
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
