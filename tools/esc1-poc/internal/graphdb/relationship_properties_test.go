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

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestRelationshipCreationPropertiesRemoveNullsWithoutChangingInput(t *testing.T) {
	var (
		properties = map[string]any{"integer": int64(9007199254740993), "decimal": 1.25, "remove": nil, "poc_owner": "ordinary property"}
		expected   = map[string]any{"integer": int64(9007199254740993), "decimal": 1.25, "poc_owner": "ordinary property"}
		copied     = relationshipCreationProperties(properties)
	)
	if !reflect.DeepEqual(copied, expected) {
		t.Fatalf("copied properties %#v", copied)
	}
	if _, found := properties["remove"]; !found {
		t.Fatal("creation removed a key from the query's map")
	}
	copied["integer"] = int64(1)
	if properties["integer"] != int64(9007199254740993) {
		t.Fatal("creation reused the query's property map")
	}
	if empty := relationshipCreationProperties(nil); len(empty) != 0 {
		t.Fatalf("creation invented properties: %v", empty)
	}
}

func TestRelationshipUpdateValidationRejectsInvalidIDs(t *testing.T) {
	var cases = []struct {
		name    string
		updates []runner.RelationshipUpdate
	}{
		{"negative ID", []runner.RelationshipUpdate{{ID: -1}}},
		{"duplicate ID", []runner.RelationshipUpdate{{ID: 1}, {ID: 1}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateRelationshipUpdates(testCase.updates); err == nil {
				t.Fatal("invalid update accepted")
			}
		})
	}
	if err := validateRelationshipUpdates([]runner.RelationshipUpdate{{ID: 0, Properties: map[string]any{"remove": nil, "poc_owner": "ordinary property"}}, {ID: 2, Properties: map[string]any{"poc_owner": nil}}}); err != nil {
		t.Fatalf("valid patch rejected: %v", err)
	}
}
