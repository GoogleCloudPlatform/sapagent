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

	"google.golang.org/protobuf/encoding/protojson"
	"github.com/GoogleCloudPlatform/sapagent/internal/log"
	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

var (
	//go:embed rules/*.json
	rulesDir embed.FS

	// RuleFilenames has list of filenames containing rule definitions.
	RuleFilenames = []string{"rules/r_sap_hana_internal_support_role.json"}
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
