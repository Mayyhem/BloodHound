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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func TestQueryOutputDeclarations(t *testing.T) {
	for _, testCase := range []struct {
		name, query, kind string
		node, invalid     bool
	}{
		{"relationship", "RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind", "PoC_Link", false, false},
		{"keyword property", "MATCH (u:User) RETURN u.return AS source, 'g' AS target, 'PoC_Link' AS kind", "PoC_Link", false, false},
		{"formatted literal", "RETURN DISTINCT 'u' AS source, 'g' AS target, /* kind */ \"PoC_Link\" AS `kind`; // end", "PoC_Link", false, false},
		{"zero rows", "MATCH (n:User) WHERE false RETURN n.objectid AS source, 'g' AS target, 'PoC_Link' AS kind", "PoC_Link", false, false},
		{"comment decoy", "// RETURN 'PoC_Wrong' AS kind\nRETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind", "PoC_Link", false, false},
		{"string decoy", "RETURN \"RETURN 'PoC_Wrong' AS kind\" AS example", "", false, false},
		{"nested return", "CALL { RETURN 'PoC_Wrong' AS kind } RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind", "PoC_Link", false, false},
		{"helper", "RETURN 'PoC_Helper_1' AS id, ['PoC_Helper'] AS kinds, {} AS properties", "PoC_Helper", true, false},
		{"prerequisite", "RETURN 'u' AS user_id", "", false, false},
		{"variable", "WITH 'PoC_Link' AS chosen RETURN 'u' AS source, 'g' AS target, chosen AS kind", "", false, true},
		{"parameter", "RETURN 'u' AS source, 'g' AS target, $chosen AS kind", "", false, true},
		{"expression", "RETURN 'u' AS source, 'g' AS target, 'PoC_' + 'Link' AS kind", "", false, true},
		{"native kind", "RETURN 'u' AS source, 'g' AS target, 'MemberOf' AS kind", "", false, true},
		{"duplicate column", "RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind, 'PoC_Other' AS kind", "", false, true},
		{"union", "RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind UNION RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind", "", false, true},
		{"trailing statement", "RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind; MATCH (n) RETURN n", "", false, true},
		{"missing endpoint", "RETURN 'u' AS source, 'PoC_Link' AS kind", "", false, true},
		{"dynamic helper kinds", "RETURN 'PoC_Helper_1' AS id, $labels AS kinds, {} AS properties", "", false, true},
		{"multiple helper kinds", "RETURN 'PoC_Helper_1' AS id, ['PoC_Helper','PoC_Other'] AS kinds, {} AS properties", "", false, true},
		{"invalid policy", "// existing-edge-policy: unknown\nRETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind", "", false, true},
		{"policy without output", "// existing-edge-policy: keep-existing\nRETURN 1", "", false, true},
		{"malformed projection", "RETURN 'u' AS source, 'g' AS target, 'PoC_Link' AS kind,", "", false, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			kind, node, err := extension.QueryOutputKind(testCase.query)
			if testCase.invalid {
				if err == nil {
					t.Fatal("invalid declaration accepted")
				}
				return
			}
			if err != nil || kind != testCase.kind || node != testCase.node {
				t.Fatalf("got %s node=%t error=%v", kind, node, err)
			}
		})
	}
}

func writeModernExtension(t *testing.T, queries map[string]string) string {
	t.Helper()
	var (
		schema     = schemaFor("result.cypher")
		directory  = writeExtension(t, schema, queries)
		path       = filepath.Join(directory, "schema.json")
		definition map[string]any
	)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, &definition); err != nil {
		t.Fatal(err)
	}
	delete(definition, "post_processing")
	for _, field := range []string{"node_kinds", "relationship_kinds"} {
		for _, raw := range definition[field].([]any) {
			delete(raw.(map[string]any), "post_processing")
		}
	}
	contents, err = json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestLoadBindsCypherOutputsWithoutSchemaProcessing(t *testing.T) {
	var query = "// depends-on: node:User\n// existing-edge-policy: keep-existing\nMATCH (n:User) WHERE false RETURN n.objectid AS source, 'd' AS target, 'PoC_ESC1' AS kind"
	directory := writeModernExtension(t, map[string]string{"renamed.cypher": query})
	plan, err := extension.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	binding := plan.Schema.Relationships[0]
	if binding.Query != "renamed.cypher" || binding.Kind != "PoC_ESC1" || binding.ExistingEdgePolicy != extension.KeepExisting || plan.Queries[0].OutputKind != "PoC_ESC1" {
		t.Fatalf("wrong inferred output: %+v", plan)
	}
	if plan.Schema.Owner != "extension/ESC1 experiment" || plan.Schema.ReferenceCommit != extension.BuildReferenceCommit {
		t.Fatalf("missing platform defaults: %+v", plan.Schema)
	}
	if err := os.WriteFile(filepath.Join(directory, "second.cypher"), []byte(query), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := extension.Load(directory); err == nil || !strings.Contains(err.Error(), "multiple producers") {
		t.Fatal("duplicate producer accepted", err)
	}
}

func TestLoadRejectsUndeclaredCypherOutput(t *testing.T) {
	var query = "// depends-on: none\nRETURN 'u' AS source, 'd' AS target, 'PoC_Unknown' AS kind"
	if _, err := extension.Load(writeModernExtension(t, map[string]string{"result.cypher": query})); err == nil || !strings.Contains(err.Error(), "must be declared") {
		t.Fatal("undeclared output accepted", err)
	}
}
