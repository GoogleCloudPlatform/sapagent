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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

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
			files:   []string{"rules/test/test-rule-invalid.json"},
			wantErr: cmpopts.AnyError,
		},
		{
			name:  "SingleRuleSuccess",
			files: []string{"rules/r_sap_hana_internal_support_role.json"},
			want: []*rpb.Rule{
				&rpb.Rule{
					Id:          "r_sap_hana_internal_support_role",
					Description: "Check to see if any users have the SAP_INTERNAL_HANA_SUPPORT role",
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
							Id: "rec_check_users_with_support_role",
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
							References: []string{"SAP HANA Database Checklists and Recommendations,https://help.sap.com/docs/SAP_HANA_PLATFORM/742945a940f240f4a2a0e39f93d3e2d4/45955420940c4e80a1379bc7270cead6.html#predefined-catalog-role-sap_internal_hana_support"},
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
				t.Errorf("ReadRules(%v) diff: (-want +got)\n %v", test.files, diff)
			}
		})
	}

}
