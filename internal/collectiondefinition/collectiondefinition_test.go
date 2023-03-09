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
			OrEvalRules: []*cmpb.EvalMetricRule{
				&cmpb.EvalMetricRule{
					EvalRules: []*cmpb.EvalRule{
						&cmpb.EvalRule{
							OutputContains: contains,
						},
					},
					IfTrue: &cmpb.MetricValue{
						ValueFromLiteral: "true",
					},
					IfFalse: &cmpb.MetricValue{
						ValueFromLiteral: "false",
					},
				},
			},
		}
	}
	createOSCommandMetric = func(metricType, label, command string) *cmpb.OSCommandMetric {
		return &cmpb.OSCommandMetric{
			OsVendor: cmpb.OSVendor_ALL,
			MetricInfo: &cmpb.MetricInfo{
				Type:  metricType,
				Label: label,
			},
			Command: command,
			Args:    []string{"-v"},
			AndEvalRules: &cmpb.EvalMetricRule{
				EvalRules: []*cmpb.EvalRule{
					&cmpb.EvalRule{
						OutputSource:   cmpb.OutputSource_STDOUT,
						OutputContains: "Contains Text",
					},
				},
				IfTrue: &cmpb.MetricValue{
					ValueFromLiteral: "true",
				},
				IfFalse: &cmpb.MetricValue{
					ValueFromLiteral: "false",
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
			AndEvalRules: &cmpb.EvalMetricRule{
				EvalRules: []*cmpb.EvalRule{
					&cmpb.EvalRule{
						OutputContains: "Contains Text",
					},
				},
				IfTrue: &cmpb.MetricValue{
					ValueFromLiteral: "true",
				},
				IfFalse: &cmpb.MetricValue{
					ValueFromLiteral: "false",
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud2", "gcloud2"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationSystem: &wlmpb.ValidationSystem{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/system", "gcloud", "gcloud"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync2", "token_runtime", "corosync-cmapctl2"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCorosync: &wlmpb.ValidationCorosync{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/corosync", "token_runtime", "corosync-cmapctl"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana2", "numa_balancing2", "dog"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "dog"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationHana: &wlmpb.ValidationHANA{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/hana", "numa_balancing", "cat"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo2", "baz"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "baz"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationNetweaver: &wlmpb.ValidationNetweaver{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/netweaver", "foo", "bar"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker2", "maintenance_mode_active", "pcs2"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs2"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationPacemaker: &wlmpb.ValidationPacemaker{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/pacemaker", "maintenance_mode_active", "pcs"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar"),
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom2", "foo2", "baz"),
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
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar"),
						},
					},
				},
			},
			secondary: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "baz"),
						},
					},
				},
			},
			want: &cdpb.CollectionDefinition{
				WorkloadValidation: &wlmpb.WorkloadValidation{
					ValidationCustom: &wlmpb.ValidationCustom{
						OsCommandMetrics: []*cmpb.OSCommandMetric{
							createOSCommandMetric("workload.googleapis.com/sap/validation/custom", "foo", "bar"),
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
