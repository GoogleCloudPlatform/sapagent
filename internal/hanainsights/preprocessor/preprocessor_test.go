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
			files: []string{"rules/r_host_status_check.json"},
			want: []*rpb.Rule{
				&rpb.Rule{
					Name:   "Host Status Check",
					Id:     "r_host_status_check",
					Labels: []string{"test-rule"},
					Queries: []*rpb.Query{
						&rpb.Query{
							Name:    "q_host_status_check",
							Sql:     "select HOST as hostname, HOST_STATUS as status from M_landscape_host_configuration where HOST_STATUS = 'ERROR'",
							Columns: []string{"hostname", "status"},
						},
					},
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Name:        "Host Status",
							Id:          "r_host_status_check",
							Description: "Check the status of each host in the SAP HANA environment to see if they are online and working",
							Trigger: &rpb.EvalNode{
								Lhs:       "size(q_host_status_check:hostname)",
								Operation: rpb.EvalNode_GT,
								Rhs:       "0",
							},
							Actions: []*rpb.Action{
								&rpb.Action{
									Description: "Not all hosts are in a healthy state. Check that all hosts are online with their services started. \nThe following hosts are in an error state: ${q_host_status_check}\n",
								},
							},
							References: []string{"PlaceHolder for SAP Note :: http:<SAP Note URL>"},
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
