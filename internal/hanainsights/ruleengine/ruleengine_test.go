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
	"fmt"
	"reflect"
	"testing"

	rpb "github.com/GoogleCloudPlatform/sapagent/protos/hanainsights/rule"
)

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
