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

package hanainsights

import (
	"context"
	"os"
	"testing"

	"flag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/subcommands"
	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/ruleengine"
	"github.com/GoogleCloudPlatform/sapagent/internal/onetime"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
	ipb "github.com/GoogleCloudPlatform/sapagent/protos/instanceinfo"
	"github.com/GoogleCloudPlatform/sapagent/shared/gce"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

func fakeWritefileSuccess(name string, content []byte, perm os.FileMode) error {
	return nil
}

func fakeWritefileFail(name string, content []byte, perm os.FileMode) error {
	return cmpopts.AnyError
}

func fakeCreateDirSuccess(path string, perm os.FileMode) error { return nil }

func fakeCreateDirFail(path string, perm os.FileMode) error { return cmpopts.AnyError }

func TestExecuteHANAInsights(t *testing.T) {
	tests := []struct {
		name         string
		hanainsights HANAInsights
		want         subcommands.ExitStatus
		args         []any
	}{
		{
			name: "FailLengthArgs",
			want: subcommands.ExitUsageError,
			args: []any{},
		},
		{
			name: "FailAssertFirstArgs",
			want: subcommands.ExitUsageError,
			args: []any{
				"test",
				"test2",
				"test3",
			},
		},
		{
			name:         "SuccessfullyParseArgs",
			hanainsights: HANAInsights{},
			want:         subcommands.ExitUsageError,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForAgentVersion",
			hanainsights: HANAInsights{
				version: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
		{
			name: "SuccessForHelp",
			hanainsights: HANAInsights{
				help: true,
			},
			want: subcommands.ExitSuccess,
			args: []any{
				"test",
				log.Parameters{},
				&ipb.CloudProperties{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.hanainsights.Execute(context.Background(), &flag.FlagSet{Usage: func() { return }}, test.args...)
			if got != test.want {
				t.Errorf("Execute(%v, %v)=%v, want %v", test.hanainsights, test.args, got, test.want)
			}
		})
	}
}

func TestValidateParametersHANAInsights(t *testing.T) {
	tests := []struct {
		name         string
		hanainsights HANAInsights
		os           string
		want         error
	}{
		{
			name: "WindowsUnSupported",
			os:   "windows",
			want: cmpopts.AnyError,
		},
		{
			name:         "EmptyHost",
			hanainsights: HANAInsights{host: ""},
			want:         cmpopts.AnyError,
		},
		{
			name:         "EmptyPort",
			hanainsights: HANAInsights{host: "localhost", port: ""},
			want:         cmpopts.AnyError,
		},
		{
			name:         "EmptySID",
			hanainsights: HANAInsights{host: "localhost", port: "123", sid: ""},
			want:         cmpopts.AnyError,
		},
		{
			name:         "EmptyUser",
			hanainsights: HANAInsights{host: "localhost", port: "123", sid: "HDB", user: ""},
			want:         cmpopts.AnyError,
		},
		{
			name: "EmptyPasswordAndSecret",
			hanainsights: HANAInsights{
				host:           "localhost",
				port:           "123",
				sid:            "HDB",
				user:           "system",
				password:       "",
				passwordSecret: "",
			},
			want: cmpopts.AnyError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.hanainsights.validateParameters(test.os)
			if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
				t.Errorf("validateParameters(hanainsights=%v, os=%v)=%v, want=%v", test.hanainsights, test.os, got, test.want)
			}
		})
	}
}

var defaultHANAInsights = HANAInsights{
	project:  "my-project",
	host:     "localhost",
	port:     "123",
	sid:      "HDB",
	user:     "system",
	password: "password",
}

func TestHANAInsightsHandler(t *testing.T) {
	tests := []struct {
		name         string
		hanainsights HANAInsights
		fakeNewGCE   onetime.GCEServiceFunc
		want         subcommands.ExitStatus
	}{
		{
			name:         "InvalidParams",
			hanainsights: HANAInsights{},
			want:         subcommands.ExitUsageError,
		},
		{
			name:         "GCEServiceCreationFailure",
			hanainsights: defaultHANAInsights,
			fakeNewGCE:   func(context.Context) (*gce.GCE, error) { return nil, cmpopts.AnyError },
			want:         subcommands.ExitFailure,
		},
		{
			name:         "runEngine",
			hanainsights: defaultHANAInsights,
			fakeNewGCE:   func(context.Context) (*gce.GCE, error) { return &gce.GCE{}, nil },
			want:         subcommands.ExitSuccess,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.hanainsights.hanaInsightsHandler(context.Background(), test.fakeNewGCE, fakeWritefileSuccess, fakeCreateDirSuccess)
			if got != test.want {
				t.Errorf("hanainsightsHandler(%v)=%v want %v", test.name, got, test.want)
			}
		})
	}
}

func TestGenerateLocalHANAInsights(t *testing.T) {
	tests := []struct {
		name        string
		rules       []*rpb.Rule
		insights    ruleengine.Insights
		wf          writeFile
		c           createDir
		expectedErr error
	}{
		{
			name:        "EmptyRules",
			rules:       nil,
			insights:    ruleengine.Insights{},
			wf:          nil,
			expectedErr: nil,
		},
		{
			name:        "EmptyInsights",
			rules:       nil,
			insights:    ruleengine.Insights{},
			wf:          nil,
			expectedErr: nil,
		},
		{
			name: "WriteFileSuccess",
			rules: []*rpb.Rule{
				&rpb.Rule{
					Id:          "rule1",
					Description: "rule1",
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Id:          "recommendation1",
							Description: "recommendation1",
							Actions: []*rpb.Action{
								&rpb.Action{
									Description: "action1",
								},
							},
						},
					},
				},
			},
			insights: ruleengine.Insights{
				"rule1": []ruleengine.ValidationResult{
					ruleengine.ValidationResult{
						RecommendationID: "recommendation1",
						Result:           true,
					},
				},
			},
			wf: fakeWritefileSuccess,
			c:  fakeCreateDirSuccess,
		},
		{
			name: "WriteFileFailure",
			rules: []*rpb.Rule{
				&rpb.Rule{
					Id:          "rule1",
					Description: "rule1",
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Id:          "recommendation1",
							Description: "recommendation1",
							Actions: []*rpb.Action{
								&rpb.Action{
									Description: "action1",
								},
							},
						},
					},
				},
			},
			insights: ruleengine.Insights{
				"rule1": []ruleengine.ValidationResult{
					ruleengine.ValidationResult{
						RecommendationID: "recommendation1",
						Result:           true,
					},
				},
			},
			wf:          fakeWritefileFail,
			c:           fakeCreateDirSuccess,
			expectedErr: cmpopts.AnyError,
		},
		{
			name: "NoRecommendations",
			rules: []*rpb.Rule{
				&rpb.Rule{
					Id:          "rule1",
					Description: "rule1",
				},
			},
			insights: ruleengine.Insights{
				"rule1": []ruleengine.ValidationResult{
					ruleengine.ValidationResult{
						RecommendationID: "recommendation1",
						Result:           false,
					},
				},
			},
			wf: fakeWritefileSuccess,
			c:  fakeCreateDirSuccess,
		},
		{
			name: "CreateDirFail",
			rules: []*rpb.Rule{
				&rpb.Rule{
					Id:          "rule1",
					Description: "rule1",
					Recommendations: []*rpb.Recommendation{
						&rpb.Recommendation{
							Id:          "recommendation1",
							Description: "recommendation1",
							Actions: []*rpb.Action{
								&rpb.Action{
									Description: "action1",
								},
							},
						},
					},
				},
			},
			insights: ruleengine.Insights{
				"rule1": []ruleengine.ValidationResult{
					ruleengine.ValidationResult{
						RecommendationID: "recommendation1",
						Result:           true,
					},
				},
			},
			wf:          fakeWritefileSuccess,
			c:           fakeCreateDirFail,
			expectedErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotErr := generateLocalHANAInsights(test.rules, test.insights, test.wf, test.c)
			if !cmp.Equal(gotErr, test.expectedErr, cmpopts.EquateErrors()) {
				t.Errorf("generateLocalHANAInsights(%v, %v)=%v, want %v", test.rules, test.insights, gotErr, test.expectedErr)
			}
		})
	}
}
