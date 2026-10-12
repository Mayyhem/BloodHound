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

package extension_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Exercise the public loader, rather than coupling tests to its private text
// scanner. Rejected files must fail before any database query can be executed.
func TestLoadRejectsWritesAndExternalReads(t *testing.T) {
	var cases = []struct {
		name  string
		query string
		want  string
	}{
		{"create", "CREATE (node:User) RETURN node.objectid", "CREATE"},
		{"merge", "MERGE (node:User {objectid: 'example'}) RETURN node.objectid", "MERGE"},
		{"delete", "MATCH ()-[edge]->() DELETE edge", "DELETE"},
		{"detach delete", "MATCH (node) DETACH DELETE node", "DETACH"},
		{"set", "MATCH (node) SET node.name = 'changed' RETURN node.name", "SET"},
		{"remove", "MATCH (node) REMOVE node.name RETURN node.objectid", "REMOVE"},
		{"foreach write", "FOREACH (value IN [1] | CREATE (:User))", "FOREACH"},
		{"write inside subquery", "CALL { CREATE (node:User) RETURN node.objectid AS source } RETURN source", "CREATE"},
		{"load csv", "LOAD CSV FROM 'file:///collection.csv' AS row RETURN row", "LOAD"},
		{"external procedure", "CALL external.fetch('http://example.invalid/data') YIELD value RETURN value", "procedures"},
		{"native read procedure", "CALL db.labels() YIELD label RETURN label", "procedures"},
		{"explain", "EXPLAIN MATCH (node) RETURN node.objectid", "EXPLAIN"},
		{"profile", "PROFILE MATCH (node) RETURN node.objectid", "PROFILE"},
		{"mixed case", "mAtCh (node) sEt node.name = 'changed' RETURN node.name", "SET"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				queries   = map[string]string{"result.cypher": "// depends-on: none\n" + testCase.query}
				directory = writeExtension(t, schemaFor("result.cypher"), queries)
			)
			_, err := extension.Load(directory)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %v; expected rejection containing %q", err, testCase.want)
			}
		})
	}
}

func TestLoadAllowsReadOnlySubqueriesAndLiteralKeywords(t *testing.T) {
	var cases = []struct {
		name  string
		query string
	}{
		{"subquery", "CALL { RETURN 'user-id' AS source } RETURN source, 'domain-id' AS target"},
		{"subquery with comment", "CALL /* CALL apoc.load.json('ignored'); CREATE */ { RETURN 1 AS value } RETURN value"},
		{"nested subquery", "CALL { CALL { RETURN 1 AS value } RETURN value } RETURN value"},
		{"literal keywords", "RETURN 'CREATE MERGE DELETE SET REMOVE LOAD CSV CALL EXPLAIN PROFILE' AS text"},
		{"double quoted literal", "RETURN \"CALL db.labels() CREATE SET\" AS text"},
		{"quoted identifiers", "MATCH (item) RETURN item.`SET` AS `CREATE`"},
		{"line comment", "RETURN 1 AS value // SET node.name = 'ignored'; CALL db.labels()\n"},
		{"block comment", "/* LOAD CSV FROM 'ignored'; PROFILE CREATE */ RETURN 1 AS value"},
		{"escaped quote", "RETURN 'a\\'b CREATE CALL' AS text"},
		{"doubled quote", "RETURN 'a''b CREATE CALL' AS text"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var queries = map[string]string{"result.cypher": "// depends-on: none\n" + testCase.query}
			if _, err := extension.Load(writeExtension(t, schemaFor("result.cypher"), queries)); err != nil {
				t.Fatalf("read-only query rejected: %v", err)
			}
		})
	}
}

// The shipped files must pass the same loader as user-authored extensions.
// In particular, their explanatory comments must not become executable tokens,
// and the helper must remain in the plan even though ESC1 does not use its rows.
func TestLoadDefaultExtension(t *testing.T) {
	var (
		directory     = filepath.Join("..", "..", "extension")
		expectedOrder = []string{
			"enabled_cas.cypher", "forest_reach.cypher", "usable_cas.cypher",
			"ca_auth_reach.cypher", "ca_root_reach.cypher", "domain_helpers.cypher",
			"eligible_templates.cypher", "trusted_cas.cypher", "enrollment_principals.cypher",
			"membership_reach.cypher", "special_groups.cypher", "membership.cypher", "esc1.cypher",
		}
		actualOrder []string
	)
	plan, err := extension.Load(directory)
	if err != nil {
		t.Fatalf("load shipped extension: %v", err)
	}
	for _, query := range plan.Queries {
		actualOrder = append(actualOrder, query.Name)
		if query.Parameter != strings.TrimSuffix(query.Name, ".cypher") {
			t.Fatalf("%s has unexpected parameter %q", query.Name, query.Parameter)
		}
	}
	if !reflect.DeepEqual(actualOrder, expectedOrder) {
		t.Fatalf("plan order %v; expected %v", actualOrder, expectedOrder)
	}
	if plan.Schema.Name != "esc1_poc" || plan.Schema.ReferenceCommit != "2cb6dbd37738ffe31e34e1729b3638f3887a93e9" {
		t.Fatalf("unexpected shipped schema identity: %+v", plan.Schema)
	}
	if len(plan.Schema.Nodes) != 1 || plan.Schema.Nodes[0].Kind != "PoC_DomainHelper" || plan.Schema.Nodes[0].Query != "domain_helpers.cypher" {
		t.Fatalf("unexpected helper binding: %+v", plan.Schema.Nodes)
	}
	if len(plan.Schema.Relationships) != 1 || plan.Schema.Relationships[0].Kind != "PoC_ADCSESC1" || plan.Schema.Relationships[0].Query != "esc1.cypher" {
		t.Fatalf("unexpected relationship binding: %+v", plan.Schema.Relationships)
	}
}
