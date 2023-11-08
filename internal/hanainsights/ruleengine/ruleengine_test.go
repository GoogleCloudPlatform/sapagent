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

package ruleengine

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmpopts/cmpopts"

	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

func fakeQueryExec(context.Context, string, ...any) (*sql.Rows, error) {
	return &sql.Rows{}, nil
}

func fakeQueryExecError(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, cmpopts.AnyError
}

type (
	fakeSQLDB struct {
		err error
	}
)

func (fdb *fakeSQLDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	if fdb.err != nil {
		return nil, fdb.err
	}
	return &sql.Rows{}, nil
}

func TestAddRow(t *testing.T) {
	cols := createColumns(2)
	for i := range cols {
		val := fmt.Sprintf("value-%d", i)
		cols[i] = &val
	}

	kb := make(knowledgeBase)
	want := make(knowledgeBase)
	want["test-query:Col-0"] = []string{"value-0"}
	want["test-query:Col-1"] = []string{"value-1"}

	q := &rpb.Query{Name: "test-query", Columns: []string{"Col-0", "Col-1"}}
	addRow(cols, q, kb)

	if !reflect.DeepEqual(kb, want) {
		t.Errorf("addRow()=%v want %v", kb, want)
	}
}

func TestCreateColumns(t *testing.T) {
	cols := createColumns(1)
	if _, ok := cols[0].(*string); !ok {
		t.Errorf("createColumns failed to create feilds")
	}
}

func TestEvaluateTrigger(t *testing.T) {

	tests := []struct {
		name    string
		trigger *rpb.EvalNode
		want    bool
	}{
		{
			name: "FloatCompare", // Trigger -> 1.0 == 2.0
			trigger: &rpb.EvalNode{
				Lhs:       "1.0",
				Rhs:       "2.0",
				Operation: rpb.EvalNode_EQ,
			},
			want: false,
		},
		{
			name: "StringCompare", // Trigger -> "Abc" == "Abc"
			trigger: &rpb.EvalNode{
				Lhs:       "Abc",
				Rhs:       "Abc",
				Operation: rpb.EvalNode_EQ,
			},
			want: true,
		},
		{
			name: "FloatStringCompareWithLogicalAND", // Trigger -> 3 > 2 AND "ABC" == "ABC"
			trigger: &rpb.EvalNode{
				Operation: rpb.EvalNode_AND,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "3", Rhs: "2", Operation: rpb.EvalNode_GT},
					&rpb.EvalNode{Lhs: "ABC", Rhs: "ABC", Operation: rpb.EvalNode_EQ},
				},
			},
			want: true,
		},
		{
			// Trigger -> ( (2 > 1) OR (1 >= 0) ) AND ( (1.5 < 1) OR (1.99 <= 1.0) OR (1.0 != 1) OR ("xyz" == "xyz") )
			name: "NestedLogicalConditionWithCompares",
			trigger: &rpb.EvalNode{
				Operation: rpb.EvalNode_AND,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{
						Operation: rpb.EvalNode_OR,
						ChildEvals: []*rpb.EvalNode{
							&rpb.EvalNode{Lhs: "2", Rhs: "1", Operation: rpb.EvalNode_GT},
							&rpb.EvalNode{Lhs: "ABC", Rhs: "ABC", Operation: rpb.EvalNode_GTE},
						},
					},
					&rpb.EvalNode{
						Operation: rpb.EvalNode_OR,
						ChildEvals: []*rpb.EvalNode{
							&rpb.EvalNode{Lhs: "1.5", Rhs: "1", Operation: rpb.EvalNode_LT},
							&rpb.EvalNode{Lhs: "1.99", Rhs: "1.0", Operation: rpb.EvalNode_LTE},
							&rpb.EvalNode{Lhs: "1.0", Rhs: "1", Operation: rpb.EvalNode_NEQ},
							&rpb.EvalNode{Lhs: "xyz", Rhs: "xyz", Operation: rpb.EvalNode_LTE},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "TriggerNil",
		},
		{
			name: "InvalidOperation",
			trigger: &rpb.EvalNode{
				Operation: rpb.EvalNode_EQ,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "3", Rhs: "2", Operation: rpb.EvalNode_GT},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateTrigger(test.trigger, nil)
			if got != test.want {
				t.Errorf("evaluateTrigger()=%v want %t", got, test.want)
			}
		})
	}
}

func TestInsertFromKB(t *testing.T) {
	kb := make(knowledgeBase)
	kb["my_query:my_col"] = []string{"1", "2", "3", "4", "5"}
	kb["my_query:my_col1"] = []string{"9"}

	tests := []struct {
		name    string
		s       string
		want    string
		wantErr error
	}{
		{
			name: "NoInsertion",
			s:    "100",
			want: "100",
		},
		{
			name: "InsertSizeFunction",
			s:    "count(my_query:my_col)",
			want: "5",
		},
		{
			name:    "ValueNotFound",
			s:       "count(my_query:your_column)",
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ScalarInsertion",
			s:    "my_query:my_col1",
			want: "9",
		},
		{
			name:    "ScalarInsertionKeyNotFound",
			s:       "my_query:my_col2",
			wantErr: cmpopts.AnyError,
		},
		{
			name:    "ScalarInsertionInvalidValues",
			s:       "my_query:my_col",
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := insertFromKB(test.s, kb)
			if !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("insertFromKB(%s, %v)=%v want: %v", test.s, kb, err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("insertFromKB(%s, %v)=%v want: %v", test.s, kb, got, test.want)
			}
		})
	}
}

func TestBuildInsights(t *testing.T) {
	rule := &rpb.Rule{
		Id: "abc",
		Recommendations: []*rpb.Recommendation{
			&rpb.Recommendation{
				Id:      "my-recommendation-1",
				Trigger: &rpb.EvalNode{Lhs: "1", Rhs: "1", Operation: rpb.EvalNode_EQ},
			},
			&rpb.Recommendation{
				Id:      "my-recommendation-2",
				Trigger: &rpb.EvalNode{Lhs: "1", Rhs: "0", Operation: rpb.EvalNode_EQ},
			},
			&rpb.Recommendation{
				Id:           "my-recommendation-3",
				ForceTrigger: true,
			},
		},
	}

	want := make(Insights)
	want["abc"] = []ValidationResult{
		ValidationResult{RecommendationID: "my-recommendation-1", Result: true},
		ValidationResult{RecommendationID: "my-recommendation-2", Result: false},
		ValidationResult{RecommendationID: "my-recommendation-3", Result: true},
	}

	got := make(Insights)
	buildInsights(rule, nil, got)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected result from buildInsights (-want,+got):\n%s", diff)
	}
}

func TestEvaluateOR(t *testing.T) {
	tests := []struct {
		name string
		t    *rpb.EvalNode
		want bool
	}{
		{
			name: "FirstConditionTrue",
			t: &rpb.EvalNode{
				Operation: rpb.EvalNode_OR,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "1", Rhs: "1", Operation: rpb.EvalNode_EQ},
					&rpb.EvalNode{Lhs: "1.99", Rhs: "1.0", Operation: rpb.EvalNode_LTE},
				},
			},
			want: true,
		},
		{
			name: "FalseEval",
			t: &rpb.EvalNode{
				Operation: rpb.EvalNode_OR,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "1", Rhs: "1", Operation: rpb.EvalNode_NEQ},
					&rpb.EvalNode{Lhs: "1.99", Rhs: "1.0", Operation: rpb.EvalNode_LTE},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateOR(test.t, nil)
			if got != test.want {
				t.Errorf("evaluateOR()=%v want %t", got, test.want)
			}
		})
	}
}

func TestEvaluateAND(t *testing.T) {
	tests := []struct {
		name string
		t    *rpb.EvalNode
		want bool
	}{
		{
			name: "FirstEvalFalse",
			t: &rpb.EvalNode{
				Operation: rpb.EvalNode_AND,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "1", Rhs: "0", Operation: rpb.EvalNode_EQ},
					&rpb.EvalNode{Lhs: "1", Rhs: "1.0", Operation: rpb.EvalNode_LTE},
				},
			},
		},
		{
			name: "TrueEval",
			t: &rpb.EvalNode{
				Operation: rpb.EvalNode_AND,
				ChildEvals: []*rpb.EvalNode{
					&rpb.EvalNode{Lhs: "1", Rhs: "1", Operation: rpb.EvalNode_EQ},
					&rpb.EvalNode{Lhs: "1.99", Rhs: "1.0", Operation: rpb.EvalNode_GTE},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateAND(test.t, nil)
			if got != test.want {
				t.Errorf("evaluateOR()=%v want %t", got, test.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	kb := make(knowledgeBase)
	kb["my_query:my_col"] = []string{"1"}

	tests := []struct {
		name string
		lhs  string
		rhs  string
		op   rpb.EvalNode_EvalType
		want bool
	}{
		{
			name: "InvalidLHS",
			lhs:  "count(a)",
		},
		{
			name: "InvalidRHS",
			rhs:  "count(a)",
		},
		{
			name: "InvalidOp",
			op:   rpb.EvalNode_UNDEFINED,
		},
		{
			name: "StringEQ",
			lhs:  "aaa",
			rhs:  "aaa",
			op:   rpb.EvalNode_EQ,
			want: true,
		},
		{
			name: "FloatEQ",
			lhs:  "1.0",
			rhs:  "1.0",
			op:   rpb.EvalNode_EQ,
			want: true,
		},
		{
			name: "StringNEQ",
			lhs:  "aaa",
			rhs:  "bbb",
			op:   rpb.EvalNode_NEQ,
			want: true,
		},
		{
			name: "FloatNEQ",
			lhs:  "1.0",
			rhs:  "1.011",
			op:   rpb.EvalNode_NEQ,
			want: true,
		},
		{
			name: "NonFloatLHS",
			lhs:  "aaa",
			rhs:  "1.0",
			op:   rpb.EvalNode_LT,
		},
		{
			name: "NonFloatRHS",
			lhs:  "1.0",
			rhs:  "aaa",
			op:   rpb.EvalNode_LT,
		},
		{
			name: "FloatLT",
			lhs:  "1.0",
			rhs:  "1.011",
			op:   rpb.EvalNode_LT,
			want: true,
		},
		{
			name: "FloatLTE",
			lhs:  "1.0",
			rhs:  "1.011",
			op:   rpb.EvalNode_LTE,
			want: true,
		},
		{
			name: "FloatGT",
			lhs:  "9.0",
			rhs:  "1.011",
			op:   rpb.EvalNode_GT,
			want: true,
		},
		{
			name: "FloatGTE",
			lhs:  "2.0",
			rhs:  "1.011",
			op:   rpb.EvalNode_GTE,
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compare(test.lhs, test.rhs, test.op, kb)
			if got != test.want {
				t.Errorf("compare(%s, %s, %v, %v)=%t, want=%t", test.lhs, test.rhs, test.op, kb, got, test.want)
			}
		})
	}
}

func TestCopy(t *testing.T) {
	want := make(knowledgeBase)
	want["sample_query:sample_col"] = []string{"1", "2", "3", "4", "5"}
	want["sample_quey1:sample_col1"] = []string{"1", "2", "3"}

	got := deepCopy(want)
	if !cmp.Equal(got, want) {
		t.Errorf("copy(%v)=%v want: %v", want, got, want)
	}
}

func TestQueryDatabase(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		testQueryFunc queryFunc
		want          error
	}{
		{
			name: "EmptySQLQuery",
			sql:  "",
			want: cmpopts.AnyError,
		},
		{
			name:          "QueryError",
			sql:           "sample_sql",
			testQueryFunc: fakeQueryExecError,
			want:          cmpopts.AnyError,
		},
		{
			name:          "QuerySuccess",
			sql:           "sample_sql",
			testQueryFunc: fakeQueryExec,
			want:          nil,
		},
	}
	for _, test := range tests {
		_, got := QueryDatabase(context.Background(), test.testQueryFunc, test.sql)
		if !cmp.Equal(got, test.want, cmpopts.EquateErrors()) {
			t.Errorf("QueryDatabase()=%v want: %v", got, test.want)
		}
	}
}

func TestRun(t *testing.T) {
	var want error
	rules := []*rpb.Rule{
		&rpb.Rule{Id: "abc", Queries: []*rpb.Query{&rpb.Query{}}, Recommendations: []*rpb.Recommendation{&rpb.Recommendation{}}},
		&rpb.Rule{Id: "knowledgebase", Queries: []*rpb.Query{&rpb.Query{}}},
	}
	fdb := &fakeSQLDB{err: nil}
	_, got := Run(context.Background(), fdb, rules)
	if !cmp.Equal(got, want, cmpopts.EquateErrors()) {
		t.Errorf("Run()=%v want: %v", got, want)
	}
}
