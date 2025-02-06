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

package yamlpb

import (
	"os"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	test_pb "github.com/GoogleCloudPlatform/sapagent/protos/yamlpbtest"
	"github.com/GoogleCloudPlatform/workloadagentplatform/sharedlibraries/log"
)

func TestMain(t *testing.M) {
	log.SetupLoggingForTest()
	os.Exit(t.Run())
}

var (
	defaultYAML string = `
uint32_v: 10
float_v: 0.3
string_v: hello
bool_v: false
enum_v: VAL_ONE
nested_v:
  uint32_v: 1
uint32_r:
- 5
- 10
- 20
nested_r:
- uint32_v: 5
- uint32_v: 10
- uint32_v: 20
`

	nestedYAML string = `
nested_r:
- &anchor_nested_r
  uint32_v: 5
- uint32_v: 7
- *anchor_nested_r
`

	aliasYAML string = `
uint32_r:
- &num_alias 10
- 5
- *num_alias
`

	duplicateEntryYAML = `
uint32_v: 11
uint32_v: 12
`
)

func TestToJSONCompatibilityView(t *testing.T) {
	tests := []struct {
		name    string
		input   map[any]any
		want    map[string]any
		wantErr error
	}{
		{
			name: "ValidInput",
			input: map[any]any{
				"str_key": "str_value",
				"int_key": 10,
				5:         20.5,
				20:        []int{1, 2, 3},
				"dict": map[any]any{
					5: 10,
				},
			},
			want: map[string]any{
				"str_key": "str_value",
				"int_key": 10,
				"5":       20.5,
				"20":      []int{1, 2, 3},
				"dict": map[string]any{
					"5": 10,
				},
			},
			wantErr: nil,
		},
		{
			name:    "ToStrMapRejectsInvalidKey",
			input:   map[any]any{5.5: "this is messed up"},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
		{
			name: "ToStrMapRejectsInvalidKeyInNestedMap",
			input: map[any]any{
				"dict": map[any]any{
					5.5: 10,
				},
			},
			want:    nil,
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := toJSONCompatibleValue(test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("Test: %s toJSONCompatibleView() (-want %v +got %v):\n", test.name, test.want, got)
			}
			if test.wantErr != nil && !cmp.Equal(err, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Test: %s toJSONCompatibleView() (-want %v +got %v):\n", test.name, test.wantErr, err)
			}
		})
	}
}

func TestUnmarshalString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *test_pb.TestMessage
		wantErr error
	}{
		{
			name:  "ValidInput",
			input: defaultYAML,
			want: &test_pb.TestMessage{
				Uint32V: 10,
				FloatV:  0.3,
				StringV: "hello",
				BoolV:   false,
				EnumV:   test_pb.TestEnum_VAL_ONE,
				NestedV: &test_pb.NestedTestMessage{Uint32V: 1},
				Uint32R: []uint32{5, 10, 20},
				NestedR: []*test_pb.NestedTestMessage{
					{Uint32V: 5},
					{Uint32V: 10},
					{Uint32V: 20},
				},
			},
			wantErr: nil,
		},
		{
			name:  "ValidInputWithNested",
			input: nestedYAML,
			want: &test_pb.TestMessage{
				NestedR: []*test_pb.NestedTestMessage{
					{Uint32V: 5},
					{Uint32V: 7},
					{Uint32V: 5},
				},
			},
		},
		{
			name:  "InputWithAlias",
			input: aliasYAML,
			want: &test_pb.TestMessage{
				Uint32R: []uint32{10, 5, 10},
			},
			wantErr: nil,
		},
		{
			name:    "InvalidInput",
			input:   duplicateEntryYAML,
			want:    &test_pb.TestMessage{},
			wantErr: cmpopts.AnyError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tpb := &test_pb.TestMessage{}
			gotErr := UnmarshalString(test.input, tpb)
			if !proto.Equal(tpb, test.want) {
				t.Errorf("Test: %s Unmarshal() (-want %v, +got %v):\n", test.name, test.want, tpb)
			}
			if gotErr != nil && !cmp.Equal(gotErr, test.wantErr, cmpopts.EquateErrors()) {
				t.Errorf("Test: %s Unmarshal() (-want %v, +got %v)", test.name, test.wantErr, gotErr)
			}
		})
	}
}
