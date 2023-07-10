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

// Package preprocessor reads the rules and validates their correctness.
package preprocessor

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

var (
	//go:embed rules/*.json testrules/*.json rules/security/*.json
	rulesDir embed.FS

	// RuleFilenames has list of filenames containing rule definitions.
	RuleFilenames = []string{
		"rules/knowledgebase.json",
		"rules/security/r_sap_hana_internal_support_role.json",
		"rules/security/r_dev_privs_in_prod.json",
		"rules/security/r_system_replication_allowed_sender.json",
		"rules/security/r_vulnerability_cve_2019_0357.json",
		"rules/security/r_backup_encryption.json",
		"rules/security/r_log_encryption.json",
		"rules/security/r_password_policy_force_first_password_change.json",
		"rules/security/r_password_policy_minimal_password_length.json",
		"rules/security/r_password_policy_password_expire_warning_time.json",
		"rules/security/r_password_policy_password_layout.json",
		"rules/security/r_persistance_encryption.json",
		"rules/security/r_prod_users_with_debug_roles.json",
		"rules/security/r_password_policy_last_used_passwords.json",
		"rules/security/r_password_policy_maximum_invalid_connect_attempts.json",
		"rules/security/r_password_policy_password_lock_time.json",
	}

	// CountPattern regex is used to identify possible matches in the trigger condition using count()
	// function on the knowledge base which returns the size of slice read from the HANA DB by a query
	// for a column.
	CountPattern = regexp.MustCompile(`^count\(([^)]+)\)$`)
)

// ReadRules returns preprocessed rules ready for execution by rule engine.
// Pre-processing includes:
//   - Read json and marshall it to protos.
//   - Validate the rule syntax.
//   - Order the rules starting global rules(knowledgebase.json) first.
//   - Within each rule, queries are ordered for a successful execution.
func ReadRules(files []string) ([]*rpb.Rule, error) {
	var rules []*rpb.Rule

	ruleIds := make(map[string]bool)
	globalKBKeys := make(map[string]bool)
	for _, filename := range files {
		rule := &rpb.Rule{}
		c, err := rulesDir.ReadFile(filename)
		if err != nil {
			log.Logger.Infow("Could not read file", "filename", filename)
			return nil, err
		}
		if err := protojson.Unmarshal(c, rule); err != nil {
			log.Logger.Infow("Could not unmarshal rule from", "filename", filename)
			return nil, err
		}
		if rule.GetId() == "knowledgebase" {
			globalKBKeys = buildQueryNameToCols(rule)
		}
		err = validateRule(rule, ruleIds, globalKBKeys)
		if err != nil {
			log.Logger.Warnf("Skipping rule: ", "id", rule.GetId(), "err", err.Error())
			continue
		}

		if rule.Queries, err = QueryExecutionOrder(rule.Queries); err != nil {
			log.Logger.Errorf("Error ordering queries", "rule", rule.Id, "error", err)
			continue
		}
		rules = append(rules, rule)
	}
	log.Logger.Debugw("Pre-processed rules", "rules", rules)
	return rules, nil
}

// validateRule checks if a rule is valid or not. The following validations are ran to ensure validity:
//
//   - Each rule must have a unique Id.
//
//   - Each query should have a non-empty name, sql query and slice representing the expected columns.
//
//   - Each column should have the same name in the slice as it is in the query.
//
//   - Each recommendation should have a unique Id.
//
//   - The trigger condition should be valid for each recommendation.
func validateRule(rule *rpb.Rule, ruleIds map[string]bool, globalKBKeys map[string]bool) error {
	if _, ok := ruleIds[rule.GetId()]; ok {
		return fmt.Errorf("rule with ruleID %s already exists - ruleID must be unique", rule.GetId())
	}
	ruleIds[rule.GetId()] = true
	if err := validateQueries(rule.GetQueries()); err != nil {
		return err
	}
	return validateRecommendations(rule.GetRecommendations(), buildQueryNameToCols(rule), globalKBKeys)
}

// validateQueries checks if each query in a rule is valid.
func validateQueries(queries []*rpb.Query) error {
	queryName := make(map[string]bool)
	for _, q := range queries {
		if q.GetName() == "" || q.GetSql() == "" || len(q.GetColumns()) == 0 {
			return fmt.Errorf("invalid query Name: %s, SQL %s, columns %v", q.GetName(), q.GetSql(), q.GetColumns())
		}

		if _, ok := queryName[q.GetName()]; ok {
			return fmt.Errorf("query with name %s already exists", q.GetName())
		}
		queryName[q.GetName()] = true
		for _, col := range q.GetColumns() {
			if !strings.Contains(q.GetSql(), col) {
				return fmt.Errorf("column %s does not exist in the query %s", col, q.GetSql())
			}
		}
	}
	return nil
}

// validateRecommendations checks if the recommendations to a rule are valid or not.
func validateRecommendations(recs []*rpb.Recommendation, queryNameToCols map[string]bool, globalKBKeys map[string]bool) error {
	recsIds := make(map[string]bool)
	for _, r := range recs {
		if _, ok := recsIds[r.GetId()]; ok {
			return fmt.Errorf("recommendation with ID %s already exists", r.GetId())
		}
		recsIds[r.GetId()] = true
		if err := validateTriggerCondition(r.GetTrigger(), queryNameToCols, globalKBKeys); err != nil {
			return err
		}
	}
	return nil
}

func validateTriggerCondition(node *rpb.EvalNode, queryNameToCols map[string]bool, globalKBKeys map[string]bool) error {
	if node == nil {
		return nil
	}
	if len(node.GetChildEvals()) == 0 {
		// a leaf node should have a trigger condition evaluated i.e lhs, rhs and operation.
		if node.GetLhs() == "" || node.GetRhs() == "" || node.GetOperation() == rpb.EvalNode_UNDEFINED {
			return fmt.Errorf("invalid eval node with lhs: %s, rhs: %s, operation: %s", node.GetLhs(), node.GetRhs(), node.GetOperation().String())
		}
		if !matchTriggerKeys(node.GetLhs(), queryNameToCols, globalKBKeys) {
			return fmt.Errorf("invalid lhs condition in the trigger condition lhs: %s", node.GetLhs())
		}
		if !matchTriggerKeys(node.GetRhs(), queryNameToCols, globalKBKeys) {
			return fmt.Errorf("invalid rhs condition in the trigger condition rhs: %s", node.GetRhs())
		}
		return nil
	}
	if node.GetOperation() != rpb.EvalNode_OR && node.GetOperation() != rpb.EvalNode_AND {
		return fmt.Errorf("invalid eval node operation: %s, in case of trigger condition having multiple child evals, allowed operations are AND|OR", node.GetOperation().String())
	}
	for _, child := range node.GetChildEvals() {
		if err := validateTriggerCondition(child, queryNameToCols, globalKBKeys); err != nil {
			return err
		}
	}
	return nil
}

// QueryExecutionOrder sorts the interdependent rule queries to produce an ordered list which is used
// for query execution. In case, a cyclic dependency is found in queries we return an empty list of queries.
// Queries are represented in the graph as nodes, and each edge in the Graph indicates a dependency
// ex: An edge (u, v) means query v depends on query u's result.
// To get an execution order for the queries we topologically sort the graph and return an order
// list of queries.
// Algorithm:
//
// Create a list of nodes with zero incoming edges (means they are not dependent on any other nodes).
// While there are nodes with zero incoming edges
//   - Remove a node from the list of zero incoming edges
//   - Add it to the result list
//   - visited_nodes++
//   - For each edge (u, v) where u is the node removed decrease the incoming edge count of v.
//   - If the incoming edge count of v reaches zero,add it to the list zero incoming edges.
//
// If visited_nodes != number_of_nodes
//   - return an error because a cycle is found.
//
// else
//   - The nodes in the result list will be topologically sorted.
func QueryExecutionOrder(queries []*rpb.Query) ([]*rpb.Query, error) {
	qg := prepareGraph(queries)

	// edgeCount keeps count of the number of incoming edges each node has in the query graph.
	edgeCount := prepareEdgeCount(qg)

	// zeroEdgeNodes keeps a list of nodes with zero incoming edges.
	zeroEdgeNodes := []string{}

	// queryNameToQuery is a mapping kept to quickly index the *rpb.Query object from queryName.
	queryNameToQuery := prepareQueryNameToQuery(queries)
	res := []*rpb.Query{}
	visited := 0

	for node, edges := range edgeCount {
		if edges == 0 {
			zeroEdgeNodes = append(zeroEdgeNodes, node)
		}
	}

	for len(zeroEdgeNodes) != 0 {
		node := zeroEdgeNodes[0]
		zeroEdgeNodes = zeroEdgeNodes[1:]
		visited++
		for _, v := range qg[node] {
			edgeCount[v]--
			if edgeCount[v] == 0 {
				zeroEdgeNodes = append(zeroEdgeNodes, v)
			}
		}
		res = append(res, queryNameToQuery[node])
	}

	if visited != len(queries) {
		return nil, errors.New("cyclic dependency found for the rule")
	}
	return res, nil
}

// prepareGraph is function which returns a data structure created to represent the rule
// queries in a form of nodes in a graph, each edge (u, v) in graph represents query v depends on
// query u i.e u should be executed before v.
func prepareGraph(queries []*rpb.Query) map[string][]string {
	queryGraph := make(map[string][]string)
	for _, q := range queries {
		if _, ok := queryGraph[q.GetName()]; !ok {
			queryGraph[q.GetName()] = []string{}
		}
		for _, deps := range q.GetDependentOnQueries() {
			if _, ok := queryGraph[deps]; ok {
				queryGraph[deps] = append(queryGraph[deps], q.GetName())
			} else {
				queryGraph[deps] = []string{q.GetName()}
			}
		}
	}
	return queryGraph
}

func prepareEdgeCount(qg map[string][]string) map[string]int {
	edgeCount := make(map[string]int)
	for u, edges := range qg {
		if _, ok := edgeCount[u]; !ok {
			edgeCount[u] = 0
		}
		for _, v := range edges {
			edgeCount[v]++
		}
	}
	return edgeCount
}

func prepareQueryNameToQuery(queries []*rpb.Query) map[string]*rpb.Query {
	queryNameToQuery := make(map[string]*rpb.Query)
	for _, q := range queries {
		queryNameToQuery[q.GetName()] = q
	}
	return queryNameToQuery
}

// In order to check trigger conditions, we need to ensure that each key prepared for accessing
// knowledge base is valid. Key is of the form `queryname:columnname`.
// buildQueryNameToCols prepares a map for each query and column combination to make it easy to
// key validation.
func buildQueryNameToCols(rule *rpb.Rule) map[string]bool {
	queryNameToCols := make(map[string]bool)
	for _, q := range rule.GetQueries() {
		for _, col := range q.GetColumns() {
			queryNameToCols[q.GetName()+":"+col] = true
		}
	}
	return queryNameToCols
}

func matchTriggerKeys(cond string, queryNameToCols map[string]bool, globalKBKeys map[string]bool) bool {
	match := CountPattern.FindStringSubmatch(cond)
	if len(match) != 2 {
		return true
	}
	key := match[1]
	if _, ok := queryNameToCols[key]; ok {
		return true
	}
	if _, ok := globalKBKeys[key]; ok {
		return true
	}
	return false
}
