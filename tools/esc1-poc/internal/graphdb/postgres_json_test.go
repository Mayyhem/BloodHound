/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/
package graphdb

import (
	"reflect"
	"testing"
)

func TestPostgresJSONPreservesExactNestedIntegers(t *testing.T) {
	var (
		decoded  any
		expected = map[string]any{"graph_id": int64(9007199254740993), "nested": map[string]any{"low": int64(-9223372036854775808), "values": []any{int64(7), float64(1.5), float64(2), true, nil}}}
	)
	if err := decodePostgresJSON([]byte(`{"graph_id":9007199254740993,"nested":{"low":-9223372036854775808,"values":[7,1.5,2.0,true,null]}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("decoded values %#v", decoded)
	}
}

func TestPostgresJSONRejectsInvalidOrOutOfRangeAnswers(t *testing.T) {
	for _, input := range []string{`{"id":9223372036854775808}`, `{"id":-9223372036854775809}`, `[1,1e400]`, `{`, `1 2`} {
		var decoded any
		if err := decodePostgresJSON([]byte(input), &decoded); err == nil {
			t.Fatalf("accepted invalid numeric answer %s", input)
		}
	}
}
