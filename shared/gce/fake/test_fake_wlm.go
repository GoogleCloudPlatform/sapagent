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

package fake

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	workloadmanager "google.golang.org/api/workloadmanager/v1"
)

var (
	insightArgsDiffOpts = []cmp.Option{cmpopts.SortSlices(validationDetailSort),
		cmpopts.IgnoreFields(workloadmanager.SapDiscovery{}, "UpdateTime")}
)

// WriteInsightArgs is a structure storing arguments sent with WriteInsight.
type WriteInsightArgs struct {
	Project  string
	Location string
	Req      *workloadmanager.WriteInsightRequest
}

// TestWLM is a fake implementation of the gce.WLM struct.
type TestWLM struct {
	T                     *testing.T
	WriteInsightArgs      []WriteInsightArgs
	WriteInsightErrs      []error
	WriteInsightCallCount int
}

func validationDetailSort(a, b *workloadmanager.SapValidationValidationDetail) bool {
	return a.SapValidationType < b.SapValidationType
}

// WriteInsight is a fake implementation of the gce.WLM.WriteInsight call.
func (w *TestWLM) WriteInsight(project, location string, req *workloadmanager.WriteInsightRequest) error {
	defer func() { w.WriteInsightCallCount++ }()

	if w.WriteInsightCallCount < len(w.WriteInsightArgs) {
		if diff := cmp.Diff(w.WriteInsightArgs[w.WriteInsightCallCount], WriteInsightArgs{project, location, req}, insightArgsDiffOpts...); diff != "" {
			w.T.Errorf("WriteInsight() arguments diff (-want +got):\n%s", diff)
		}
	}

	return w.WriteInsightErrs[w.WriteInsightCallCount]
}
