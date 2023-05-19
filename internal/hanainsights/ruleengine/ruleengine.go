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

	"github.com/GoogleCloudPlatform/sapagent/internal/hanainsights/preprocessor"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"

	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

type (
	// queryFunc provides an easily testable translation to the SQL API.
	queryFunc func(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	knowledgeBase map[string][]string
)

// Run starts the rule engine execution - reads the rules, executes them and generate results.
func Run(ctx context.Context, db *sql.DB) error {
	// Read and pre-process the rules.
	rules, err := preprocessor.ReadRules(preprocessor.RuleFilenames)
	if err != nil {
		return err
	}

	// Execute the queries to build knowledge base.
	for _, rule := range rules {
		log.Logger.Debugw("Building knowledgebase for rule", "rule", rule.Id)
		rkb := make(knowledgeBase)
		if err := buildKnowledgeBase(ctx, db, rule.Queries, rkb); err != nil {
			log.Logger.Debugw("Error building knowledge base", "rule", rule.Id, "error", err)
			continue
		}
	}
	return nil
}

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
