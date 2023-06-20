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

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

var (
	//go:embed rules/*.json
	rulesDir embed.FS

	// RuleFilenames has list of filenames containing rule definitions.
	RuleFilenames = []string{
		"rules/knowledgebase.json",
		"rules/r_sap_hana_internal_support_role.json",
		"rules/r_dev_privs_in_prod.json",
		"rules/r_system_replication_allowed_sender.json",
		"rules/r_vulnerability_cve_2019_0357.json",
	}
)

// ReadRules reads the rules, pre-processes them.
func ReadRules(files []string) ([]*rpb.Rule, error) {
	var rules []*rpb.Rule

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
		// TODO: Validate required entries in rules.
		rules = append(rules, rule)
	}

	log.Logger.Debugw("All rules to be executed by the engine", "rules", rules)
	return rules, nil
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
