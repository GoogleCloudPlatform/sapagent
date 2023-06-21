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

// Package ruleengine executes the hanainsights rules and generates recommendations.
package ruleengine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

type (
	// queryFunc provides an easily testable translation to the SQL API.
	queryFunc func(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// knowledgeBase is a map with key=<query_name:column_name> and value=<slice of string values read from HANA DB>.
	knowledgeBase map[string][]string

	// ValidationResult has results of each of the recommendation evaluation.
	ValidationResult struct {
		RecommendationID string
		Result           bool // True if validation failed, false otherwise.
	}

	// Insights is a map of rules that evaluated to true with key=<rule-id> and value=<slice of recommendation-ids that evaluated to true>
	Insights map[string][]ValidationResult
)

// Run starts the rule engine execution - executes the rules and generate insights.
func Run(ctx context.Context, db *sql.DB, rules []*rpb.Rule) (Insights, error) {
	// gkb is the global knowledge base with frequently accessed data.
	gkb := buildGlobalKnowledgeBase(ctx, db, rules)

	// Execute the queries to build knowledge base.
	insights := make(Insights)
	for _, rule := range rules {
		log.Logger.Debugw("Building knowledgebase for rule", "rule", rule.Id)
		rkb := deepCopy(gkb)
		if err := buildKnowledgeBase(ctx, db, rule.Queries, rkb); err != nil {
			log.Logger.Debugw("Error building knowledge base", "rule", rule.Id, "error", err)
			continue
		}
		buildInsights(rule, rkb, insights)
		log.Logger.Debugw("Evaluation completed", "rule", rule.Id)
	}
	log.Logger.Infow("HANA Insights", "insights", insights)
	return insights, nil
}

func buildGlobalKnowledgeBase(ctx context.Context, db *sql.DB, rules []*rpb.Rule) knowledgeBase {
	gkb := make(knowledgeBase)
	for _, rule := range rules {
		if rule.Id == "knowledgebase" {
			if err := buildKnowledgeBase(ctx, db, rule.Queries, gkb); err != nil {
				log.Logger.Errorw("Error building global knowledge base", "rule", rule.Id, "error", err)
			}
			break
		}
	}
	return gkb
}

func deepCopy(gkb knowledgeBase) knowledgeBase {
	rkb := make(knowledgeBase)
	for k, v := range gkb {
		rkb[k] = append(rkb[k], v...)
	}
	return rkb
}

// buildKnowledgeBase runs all the queries and generates a result knowledge base for each rule.
func buildKnowledgeBase(ctx context.Context, db *sql.DB, queries []*rpb.Query, kb knowledgeBase) error {
	for _, query := range queries {
		cols := createColumns(len(query.Columns))
		rows, err := QueryDatabase(ctx, db.QueryContext, query.Sql)
		if err != nil {
			log.Logger.Debugw("Error running query", "query", query.Sql)
			return err
		}
		for rows.Next() {
			if err := rows.Scan(cols...); err != nil {
				log.Logger.Debugw("Error scanning query result", "query", query.Sql)
				return err
			}
			addRow(cols, query, kb)
		}
		log.Logger.Infow("Knowledge base built successfully", "query", query.Sql, "result", cols, "knowledgebase", kb)
	}
	return nil
}

// addRow adds the response row data to the knowledgebase
func addRow(cols []any, q *rpb.Query, kb knowledgeBase) {
	for i, col := range cols {
		if val, ok := col.(*string); ok {
			key := fmt.Sprintf("%s:%s", q.Name, q.Columns[i])
			kb[key] = append(kb[key], *val)
		}
	}
}

// QueryDatabase attempts to execute the specified query, returning a sql.Rows iterator.
func QueryDatabase(ctx context.Context, queryFunc queryFunc, sql string) (*sql.Rows, error) {
	if sql == "" {
		return nil, errors.New("no query specified")
	}
	log.Logger.Debugw("Executing query", "query", sql)

	rows, err := queryFunc(ctx, sql)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func createColumns(count int) []any {
	cols := make([]any, count)
	for i := range cols {
		cols[i] = new(string)
	}
	return cols
}

// buildInsights evaluates all the trigger conditions in a rule and updates the insights map.
func buildInsights(rule *rpb.Rule, kb knowledgeBase, insights Insights) {
	for _, r := range rule.Recommendations {
		vr := ValidationResult{
			RecommendationID: r.Id,
		}
		if evaluateTrigger(r.Trigger, kb) {
			vr.Result = true
		}
		log.Logger.Debugw("Recommendation evaluation result", "rule", rule.Id, "recommendation", r.Id, "result", vr.Result)
		insights[rule.Id] = append(insights[rule.Id], vr)
	}
	log.Logger.Infow("Insights successfully generated", "insights", insights)
}

// evaluateTrigger recursively evaluates the trigger condition tree and returns boolean result.
func evaluateTrigger(t *rpb.EvalNode, kb knowledgeBase) bool {
	if t == nil {
		return false
	}
	if len(t.ChildEvals) == 0 {
		return compare(t.Lhs, t.Rhs, t.Operation, kb)
	}
	switch t.Operation {
	case rpb.EvalNode_OR:
		return evaluateOR(t, kb)
	case rpb.EvalNode_AND:
		return evaluateAND(t, kb)
	}
	return false
}

func evaluateOR(t *rpb.EvalNode, kb knowledgeBase) bool {
	for _, c := range t.ChildEvals {
		if evaluateTrigger(c, kb) {
			return true
		}
	}
	return false
}

func evaluateAND(t *rpb.EvalNode, kb knowledgeBase) bool {
	for _, c := range t.ChildEvals {
		if !evaluateTrigger(c, kb) {
			return false
		}
	}
	return true
}

// compare does the leaf node evaluation of the eval tree.
func compare(lhs, rhs string, op rpb.EvalNode_EvalType, kb knowledgeBase) bool {
	// Replace values from knowledge base before comparison
	var err error
	if lhs, err = insertFromKB(lhs, kb); err != nil {
		log.Logger.Error(err)
		return false
	}
	if rhs, err = insertFromKB(rhs, kb); err != nil {
		log.Logger.Error(err)
		return false
	}

	// TODO: strengthen the EQ/NEQ comparisons for numerical values.
	// Evaluate equality comparisons.
	switch op {
	case rpb.EvalNode_UNDEFINED:
		return false
	case rpb.EvalNode_EQ:
		return lhs == rhs
	case rpb.EvalNode_NEQ:
		return lhs != rhs
	}

	// Evaluate float comparisons.
	var l, r float64
	if l, err = strconv.ParseFloat(lhs, 64); err != nil {
		return false
	}
	if r, err = strconv.ParseFloat(rhs, 64); err != nil {
		return false
	}

	switch op {
	case rpb.EvalNode_LT:
		return l < r
	case rpb.EvalNode_LTE:
		return l <= r
	case rpb.EvalNode_GT:
		return l > r
	case rpb.EvalNode_GTE:
		return l >= r
	}
	return false
}

// insertFromKB refers to the knowledgebase and inserts the values where functions are used.
// Possible functions include: count(query_name:column_name)
func insertFromKB(s string, kb knowledgeBase) (string, error) {
	countPattern := regexp.MustCompile(`^count\(([^)]+)\)$`)
	match := countPattern.FindStringSubmatch(s)
	if len(match) != 2 {
		// The string does not use count() function, no insertion necessary.
		return s, nil
	}
	k := match[1]
	if _, ok := kb[k]; ok {
		return strconv.Itoa(len(kb[k])), nil
	}
	return "", fmt.Errorf("could not insert values from knowledge base for function: %s", s)
}
