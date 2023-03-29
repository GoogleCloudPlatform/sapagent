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

// Package yamlpb contains util methods to unmarshall a YAML string to a proto message
package yamlpb

import (
	"encoding/json"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"github.com/go-yaml/yaml"
)

// toJSONCompatibleValue recursively converts the YAML-compatible value to a
// JSON-comptaible value if possible, else returns an error.
func toJSONCompatibleValue(i any) (o any, err error) {
	switch t := i.(type) {
	case map[any]any:
		return toStrMap(t)
	case []byte:
		return t, nil
	case []any:
		var arr []any
		for _, v := range t {
			var j any
			j, err = toJSONCompatibleValue(v)
			if err != nil {
				return nil, err
			}
			arr = append(arr, j)
		}
		return arr, nil
	}
	return i, nil
}

// toStrMap creates a string-indexed map based on the provided map if possible.
// YAML supports many different kinds of indices in maps. JSON only supports
// string-indexed maps and protobuf only supports string and integer-indexed
// maps. So, we convert integer-indexed maps to string maps according to
// https://developers.google.com/protocol-buffers/docs/proto3#json
// and recursively convert values to JSON-compatible types.
func toStrMap(i map[any]any) (o map[string]any, err error) {
	o = make(map[string]any)
	for k, v := range i {
		var s string
		switch t := k.(type) {
		case string:
			s = t
		case int:
			s = strconv.FormatInt(int64(t), 10)
		default:
			err = fmt.Errorf("'%v' is not a string or integer. Only string and integer keys are supported", k)
			return nil, err
		}
		o[s], err = toJSONCompatibleValue(v)
		if err != nil {
			return nil, err
		}
	}
	return o, nil
}

// UnmarshalString will populate the fields of a protocol buffer based on a YAML
// string.
func UnmarshalString(s string, pb proto.Message) error {
	var m any
	if err := yaml.UnmarshalStrict([]byte(s), &m); err != nil {
		return fmt.Errorf("cannot unmarshal yaml string: %v", err)
	}

	v, err := toJSONCompatibleValue(m)
	if err != nil {
		return err
	}

	var j []byte
	if j, err = json.Marshal(v); err != nil {
		return err
	}

	if err := protojson.Unmarshal(j, pb); err != nil {
		return err
	}

	return nil
}
