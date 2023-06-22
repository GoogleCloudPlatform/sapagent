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

package parse

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []string
	}{
		{
			name: "EmptyString",
			s:    "",
			want: []string{""},
		},
		{
			name: "OneParameter",
			s:    "Hello",
			want: []string{"Hello"},
		},
		{
			name: "MultipleParameters",
			s:    "backup restore inquire delete",
			want: []string{"backup", "restore", "inquire", "delete"},
		},
		{
			name: "ParameterWithSpaces",
			s:    `"Hello World"`,
			want: []string{`"Hello World"`},
		},
		{
			name: "ParameterWithDoubleQuote",
			s:    `"\"Hello \"World"`,
			want: []string{`"\"Hello \"World"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Split(test.s)
			if diff := cmp.Diff(test.want, got, protocmp.Transform(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("split(%v) had unexpected diff (-want +got):\n%s", test.s, diff)
			}
		})
	}
}
