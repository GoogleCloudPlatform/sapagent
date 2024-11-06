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

package preprocessor

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func TestReadRules(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		want    []*rpb.Rule
		wantErr error
	}{
		{
			name:    "InvalidFile",
			files:   []string{"no-file.txt"},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "InvalidFileContent",
			files:   []string{"testrules/test-rule-invalid.json"},
			wantErr: cmpopts.AnyError,
		},
		{
			name: "InvalidQueryInARule",
			files: []string{
				"testrules/test-invalid-query-rule.json",
			},
		},
		{
			name:    "RuleWithCyclicDependency",
			files:   []string{"testrules/test-rule-cyclic-dependency.json"},
			wantErr: nil,
		},
		{
			name: "ReadGlobalKnowledgeBase",
			files: []string{
				"testrules/test-knowledge-base.json",
				"testrules/test-query-using-global-kb.json",
			},
			want: []*rpb.Rule{
				&rpb.Rule{
					Id:          "knowledgebase",
					Description: "Knowledgebase which contains queries which are frequently used in rules.",
					Labels:      []string{"internal"},
					Queries: []*rpb.Query{
						&rpb.Query{
							Name:    "q_system_usage",
							Sql:     "SELECT VALUE as value from M_INIFILE_CONTENTS  WHERE FILE_NAME = 'global.ini' AND SECTION = 'system_information' AND KEY = 'usage'",
							Columns: []string{"value"},
						},
					},
				},
				&rpb.Rule{
					Id: "test-query-using-global-kb",
					Queries: []*rpb.Query{
						&rpb.Query{
							Name:    "q_development_users",
							Sql:     "SELECT GRANTEE as grantee FROM EFFECTIVE_PRIVILEGE_GRANTEES WHERE OBJECT_TYPE = 'SYSTEMPRIVILEGE' AND PRIVILEGE = 'DEVELOPMENT' AND GRANTEE NOT IN ('SYSTEM','_SYS_REPO')",
							Columns: []string{"grantee"},
						},
					},
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Id: "rec_1",
							Trigger: &rpb.EvalNode{
								Operation: rpb.EvalNode_AND,
								ChildEvals: []*rpb.EvalNode{
									&rpb.EvalNode{
										Lhs:       "count(q_system_usage:value)",
										Operation: rpb.EvalNode_EQ,
										Rhs:       "Production",
									},
									&rpb.EvalNode{
										Lhs:       "count(q_development_users:grantee)",
										Operation: rpb.EvalNode_GT,
										Rhs:       "0",
									},
								},
							},
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name:  "SingleRuleSuccess",
			files: []string{"rules/security/r_sap_hana_internal_support_role.json"},
			want: []*rpb.Rule{
				&rpb.Rule{
					Id:          "r_sap_hana_internal_support_role",
					Description: "Users with SAP_INTERNAL_HANA_SUPPORT role",
					Labels:      []string{"security"},
					Queries: []*rpb.Query{
						&rpb.Query{
							Name:    "q_users_sap_hana_internal_support",
							Sql:     "SELECT COUNT(*) as count FROM SYS.EFFECTIVE_ROLE_GRANTEES WHERE ROLE_NAME = 'SAP_INTERNAL_HANA_SUPPORT'",
							Columns: []string{"count"},
						},
					},
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Id: "rec_1",
							Trigger: &rpb.EvalNode{
								Lhs:       "q_users_sap_hana_internal_support:count",
								Operation: rpb.EvalNode_GT,
								Rhs:       "0",
							},
							Actions: []*rpb.Action{
								&rpb.Action{
									Description: "At least one account has the SAP_INTERNAL_HANA_SUPPORT role. This is an internal role that enables low level access to data. It should only be assigned to admin or support at the request of SAP Development and during an active SAP support request.",
								},
							},
							References: []string{"SAP HANA Database Checklists and Recommendations: https://help.sap.com/docs/SAP_HANA_PLATFORM/742945a940f240f4a2a0e39f93d3e2d4/45955420940c4e80a1379bc7270cead6.html#predefined-catalog-role-sap_internal_hana_support"},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := ReadRules(test.files)

			if !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("ReadRules(%v)=%v want: %v", test.files, gotErr, test.wantErr)
			}

			if diff := cmp.Diff(test.want, got, protocmp.Transform()); diff != "" {
				fmt.Println("Got ", got, "Want ", test.want)
				t.Errorf("ReadRules(%v) diff: (-want +got)\n %v", test.files, diff)
			}
		})
	}

}

func TestQueryExecutionOrder(t *testing.T) {
	tests := []struct {
		name    string
		queries []*rpb.Query
		want    []*rpb.Query
		wantErr error
	}{
		{
			name: "NoDependentQueries",
			queries: []*rpb.Query{
				&rpb.Query{
					Name:        "sampleQuery1",
					Description: "Sample Query 1",
					Sql:         "sample_sql",
					Columns:     []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
			},
			want: []*rpb.Query{
				&rpb.Query{
					Name:        "sampleQuery1",
					Description: "Sample Query 1",
					Sql:         "sample_sql",
					Columns:     []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
			},
			wantErr: nil,
		},
		{
			name: "SimpleGraphWithDependentQueries",
			queries: []*rpb.Query{
				&rpb.Query{
					Name:        "sampleQuery1",
					Description: "Sample Query 1",
					Sql:         "sample_sql",
					Columns:     []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
				&rpb.Query{
					Name:               "sampleQuery2",
					Description:        "Sample Query 2",
					Sql:                "sample_sql",
					DependentOnQueries: []string{"sampleQuery1"},
					Columns:            []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
			},
			want: []*rpb.Query{
				&rpb.Query{
					Name:        "sampleQuery1",
					Description: "Sample Query 1",
					Sql:         "sample_sql",
					Columns:     []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
				&rpb.Query{
					Name:               "sampleQuery2",
					Description:        "Sample Query 2",
					Sql:                "sample_sql",
					DependentOnQueries: []string{"sampleQuery1"},
					Columns:            []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
				},
			},
			wantErr: nil,
		},
		{
			name: "CyclicDependency",
			queries: []*rpb.Query{
				&rpb.Query{
					Name:               "sampleQuery1",
					Description:        "Sample Query 1",
					Sql:                "sample_sql",
					DependentOnQueries: []string{"sampleQuery2"},
				},
				&rpb.Query{
					Name:               "sampleQuery2",
					Description:        "Sample Query 2",
					Sql:                "sample_sql",
					DependentOnQueries: []string{"sampleQuery3"},
				},
				&rpb.Query{
					Name:               "sampleQuery3",
					Description:        "Sample Query 3",
					Sql:                "sample_sql",
					DependentOnQueries: []string{"sampleQuery1"},
				},
			},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := QueryExecutionOrder(tc.queries)
			if !cmp.Equal(gotErr, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("QueryExecutionOrder(%v)=%v want: %v", tc.queries, gotErr, tc.wantErr)
			}
			if !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("QueryExecutionOrder(%v)=%v want: %v", tc.queries, got, tc.want)
			}
		})
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name     string
		ruleIds  map[string]bool
		globalKb map[string]bool
		rule     *rpb.Rule
		wantErr  error
	}{
		{
			name:    "DuplicateRuleId",
			ruleIds: map[string]bool{"r_sap_hana_internal_support_role": true},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule:    &rpb.Rule{Id: "r_sap_hana_internal_support_role"},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "QueryWithNoName",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role", Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "",
						Sql:     "sample_sql",
						Columns: []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "QueryWithEmptySQL",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role", Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "",
						Columns: []string{"sampleColumn1", "sampleColumn2", "sampleColumn3"},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "QueryWithNoColumns",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role", Queries: []*rpb.Query{
					&rpb.Query{
						Name: "sampleQuery1",
						Sql:  "sample_sql",
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "DuplicateQueryName",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role",
				Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "select sampleColumn1 from table",
						Columns: []string{"sampleColumn1"},
					},
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "select sampleColumn1 from table",
						Columns: []string{"sampleColumn1"},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "InvalidColumnQuery",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role", Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "sample_sql",
						Columns: []string{"sampleColumn1"},
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "DuplicateRecommendationId",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role",
				Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "select sample_column from table",
						Columns: []string{"sample_column"},
					},
				},
				Recommendations: []*rpb.Recommendation{
					&rpb.Recommendation{
						Id: "r_sap_hana_internal_support_role",
					},
					&rpb.Recommendation{
						Id: "r_sap_hana_internal_support_role",
					},
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ValidQuery",
			ruleIds: map[string]bool{},
			globalKb: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			rule: &rpb.Rule{
				Id: "r_sap_hana_internal_support_role", Queries: []*rpb.Query{
					&rpb.Query{
						Name:    "sampleQuery1",
						Sql:     "select sample_column from table",
						Columns: []string{"sample_column"},
					},
				},
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateRule(tc.rule, tc.ruleIds, tc.globalKb)
			if !cmp.Equal(got, tc.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("validateRule(%v, %v)=%v want: %v", tc.rule, tc.ruleIds, got, tc.wantErr)
			}
		})
	}
}

func TestValidateRecommendations(t *testing.T) {
	tests := []struct {
		name            string
		recomms         []*rpb.Recommendation
		queryNametoCols map[string]bool
		globalKB        map[string]bool
		want            error
	}{
		{
			name: "InvalidLeafNode",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_AND,
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "InvalidNonLeafNode",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						ChildEvals: []*rpb.EvalNode{
							&rpb.EvalNode{
								Lhs:       "sample_lhs",
								Operation: rpb.EvalNode_EQ,
								Rhs:       "sample_rhs",
							},
							&rpb.EvalNode{
								Lhs:       "sample_lhs",
								Operation: rpb.EvalNode_EQ,
								Rhs:       "sample_rhs",
							},
						},
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "InvalidLHS",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "count(sample_lhs:value)",
						Rhs:       "sample_rhs",
					},
				},
			},
			globalKB: map[string]bool{
				"q_users_sap_hana_internal_support:count": true,
			},
			queryNametoCols: map[string]bool{
				"sample_lhs": true,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "InvalidRHS",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "sample_lhs",
						Rhs:       "count(sample_rhs:value)",
					},
				},
			},
			want: cmpopts.AnyError,
		},
		{
			name: "MutilevelInvalidTriggerTree",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_AND,
						ChildEvals: []*rpb.EvalNode{
							&rpb.EvalNode{
								Lhs:       "count(sample_lhs)",
								Operation: rpb.EvalNode_EQ,
								Rhs:       "sample_rhs",
							},
						},
					},
				},
			},
			queryNametoCols: map[string]bool{
				"sample_lhs:value": true,
			},
			globalKB: map[string]bool{
				"sample_lhs:value": true,
			},
			want: cmpopts.AnyError,
		},
		{
			name: "ValidQuery",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "count(sample_lhs:value)",
						Rhs:       "sample_rhs",
					},
				},
			},
			queryNametoCols: map[string]bool{
				"sample_lhs:value": true,
			},
			want: nil,
		},
		{
			name: "ValidQueryFetchedFromGlobalKB",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "count(sample_lhs:value)",
						Rhs:       "sample_rhs",
					},
				},
			},
			queryNametoCols: map[string]bool{
				"sample_key": true,
			},
			globalKB: map[string]bool{
				"sample_lhs:value": true,
			},
			want: nil,
		},
		{
			name: "ValidScalarReference",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "sample_lhs:value",
						Rhs:       "sample_rhs",
					},
				},
			},
			queryNametoCols: map[string]bool{
				"sample_lhs:value": true,
			},
			want: nil,
		},
		{
			name: "ValidScalarReferenceFetchedFromGlobalKB",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "sample_lhs:value",
						Rhs:       "sample_rhs",
					},
				},
			},
			globalKB: map[string]bool{
				"sample_lhs:value": true,
			},
			want: nil,
		},
		{
			name: "InvalidScalarReference",
			recomms: []*rpb.Recommendation{
				&rpb.Recommendation{
					Id: "r_sap_hana_internal_support_role",
					Trigger: &rpb.EvalNode{
						Operation: rpb.EvalNode_EQ,
						Lhs:       "sample_lhs:value",
						Rhs:       "sample_rhs",
					},
				},
			},
			want: cmpopts.AnyError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateRecommendations(tc.recomms, tc.queryNametoCols, tc.globalKB)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("validateRecommendations(%v, %v, %v)=%v want: %v", tc.recomms, tc.queryNametoCols, tc.globalKB, got, tc.want)
			}
		})
	}
}
