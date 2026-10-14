// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package querycheck_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/querycheck"
)

func TestInputsComeFromQuerySyntax(t *testing.T) {
	result, err := querycheck.Check("// MATCH (:Computer)-[:AdminTo]->() $ignored\nUNWIND $people AS person\nMATCH (user:`User`)-[:`MemberOf`]->(group:Group)\nRETURN '$quoted :Computer' AS note")
	if err != nil {
		t.Fatal(err)
	}
	expected := []querycheck.Input{{Type: "query", Name: "people.cypher", Line: 2}, {Type: "node", Name: "User", Line: 3}, {Type: "relationship", Name: "MemberOf", Line: 3}, {Type: "node", Name: "Group", Line: 3}}
	if !reflect.DeepEqual(result.Inputs, expected) || result.BroadRead {
		t.Fatalf("inputs: %+v", result)
	}
	broad, err := querycheck.Check("MATCH (item)-[edge]->() RETURN labels(item), type(edge)")
	if err != nil || !broad.BroadRead {
		t.Fatalf("broad read: %+v %v", broad, err)
	}
}

func TestLanguageErrorsPreserveOriginalLines(t *testing.T) {
	_, err := querycheck.Check("\n\n// comment\nRETURN [n IN [1] | n] AS answer")
	if err == nil || !strings.Contains(err.Error(), "line 4:") || !strings.Contains(err.Error(), "language support") {
		t.Fatalf("language error: %v", err)
	}
	for _, source := range []string{"MACH (n) RETURN n", "MATCH (n RETURN n", "RETURN (", "RETURN 1; RETURN 2"} {
		if _, err := querycheck.Check(source); err == nil {
			t.Fatalf("accepted broken query %q", source)
		}
	}
}

func TestSharedColumnsKeepDescriptiveNamesAndRespectScope(t *testing.T) {
	for _, testCase := range []struct {
		source  string
		fields  []querycheck.SharedField
		known   bool
		columns []string
	}{
		{"UNWIND $directory_users AS person RETURN person.user_id AS identity, person.details AS display_name", []querycheck.SharedField{{Parameter: "directory_users", Column: "user_id", Line: 1}, {Parameter: "directory_users", Column: "details", Line: 1}}, true, []string{"identity", "display_name"}},
		{"UNWIND $people AS person WITH {different: 1} AS person RETURN person.different AS value", nil, true, []string{"value"}},
		{"UNWIND $people AS person UNWIND [{different: 1}] AS person RETURN person.different AS value", nil, true, []string{"value"}},
		{"RETURN null AS optional_value, [] AS memberships, {label:'x'} AS details", nil, true, []string{"optional_value", "memberships", "details"}},
		{"WITH 1 AS value RETURN *", nil, false, nil},
	} {
		result, err := querycheck.Check(testCase.source)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.SharedFields, testCase.fields) || result.ColumnsKnown != testCase.known || !reflect.DeepEqual(result.Columns, testCase.columns) {
			t.Fatalf("unexpected columns: %+v", result)
		}
	}
}
