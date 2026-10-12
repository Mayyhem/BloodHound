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
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"reflect"
	"testing"
)

func TestRelationshipCreationPropertiesSeparateOwnerAndRemoveNulls(t *testing.T) {
	var (
		properties = map[string]any{"integer": int64(9007199254740993), "decimal": 1.25, "remove": nil}
		expected   = map[string]any{"integer": int64(9007199254740993), "decimal": 1.25, "poc_owner": "owner"}
	)
	copied, err := relationshipCreationProperties("owner", properties)
	if err != nil || !reflect.DeepEqual(copied, expected) {
		t.Fatalf("copied properties %#v: %v", copied, err)
	}
	if _, found := properties["poc_owner"]; found {
		t.Fatal("creation mutated the query's property map")
	}
	if _, found := properties["remove"]; !found {
		t.Fatal("creation removed a key from the query's map")
	}
	if _, err := relationshipCreationProperties("", nil); err == nil {
		t.Fatal("created without owner")
	}
	if _, err := relationshipCreationProperties("owner", map[string]any{"poc_owner": "owner"}); err == nil {
		t.Fatal("accepted reserved ownership property")
	}
}

func TestRelationshipUpdateValidationRejectsUnsafePatch(t *testing.T) {
	var cases = []struct {
		name, owner string
		updates     []runner.RelationshipUpdate
	}{
		{"no owner", "", nil},
		{"negative ID", "owner", []runner.RelationshipUpdate{{ID: -1}}},
		{"duplicate ID", "owner", []runner.RelationshipUpdate{{ID: 1}, {ID: 1}}},
		{"change owner", "owner", []runner.RelationshipUpdate{{ID: 1, Properties: map[string]any{"poc_owner": "other"}}}},
		{"remove owner", "owner", []runner.RelationshipUpdate{{ID: 1, Properties: map[string]any{"poc_owner": nil}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateRelationshipUpdates(testCase.owner, testCase.updates); err == nil {
				t.Fatal("unsafe update accepted")
			}
		})
	}
	if err := validateRelationshipUpdates("owner", []runner.RelationshipUpdate{{ID: 0, Properties: map[string]any{"remove": nil}}, {ID: 2}}); err != nil {
		t.Fatalf("valid patch rejected: %v", err)
	}
}
