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
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func schemaFor(query string) extension.Schema {
	return extension.Schema{Name: "ESC1 experiment", ReferenceCommit: strings.Repeat("a", 40), Relationships: []extension.RelationshipBinding{{Kind: "PoC_ESC1", Query: query, Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}, {SourceKind: "Group", TargetKind: "Domain"}}}}}
}

func writeExtension(t *testing.T, schema extension.Schema, queries map[string]string) string {
	t.Helper()
	var (
		directory     = t.TempDir()
		nodes         = []map[string]any{}
		relationships = []map[string]any{}
	)
	// These tests build the same single definition that an extension author
	// writes. The execution settings sit beside the ordinary display settings.
	for _, node := range schema.Nodes {
		nodes = append(nodes, map[string]any{"name": node.Kind, "post_processing": map[string]any{"query": node.Query, "id_prefix": node.IDPrefix}})
	}
	for _, relationship := range schema.Relationships {
		definition := map[string]any{"name": relationship.Kind, "compatible_kinds": relationship.Endpoints, "post_processing": map[string]any{"query": relationship.Query}}
		if relationship.ExistingEdgePolicy != "" {
			definition["existing_edge_policy"] = relationship.ExistingEdgePolicy
		}
		relationships = append(relationships, definition)
	}
	var (
		definition = map[string]any{
			"schema":             map[string]any{"name": schema.Name, "display_name": schema.Name, "version": "v1.0.0", "namespace": "PoC"},
			"node_kinds":         nodes,
			"relationship_kinds": relationships,
			"post_processing":    map[string]any{"reference_commit": schema.ReferenceCommit, "compare_native_esc1": schema.CompareNativeESC1},
		}
		contents, err = json.Marshal(definition)
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "schema.json"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	for name, query := range queries {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(query), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestLoadOrdersSharedPrerequisitesOnce(t *testing.T) {
	var queries = map[string]string{
		"shared.cypher": "// depends-on: node:Domain\nRETURN 1",
		"left.cypher":   "// depends-on: query:shared.cypher\nRETURN $shared",
		"right.cypher":  "// depends-on: query:shared.cypher\nRETURN $shared",
		"result.cypher": "// Read both sides.\n// depends-on: relationship:Enroll,\n// node:User, query:left.cypher\n// depends-on: query:right.cypher\n// Note: both sides use the shared rows.\nRETURN $left, $right, '$literal' // $example\n",
	}
	plan, err := extension.Load(writeExtension(t, schemaFor("result.cypher"), queries))
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, query := range plan.Queries {
		actual = append(actual, query.Name)
		if query.Parameter != strings.TrimSuffix(query.Name, ".cypher") {
			t.Fatalf("unexpected parameter %q", query.Parameter)
		}
	}
	expected := []string{"shared.cypher", "left.cypher", "right.cypher", "result.cypher"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("order %v; expected %v", actual, expected)
	}
}

func TestFilenameDoesNotAssignPublishedKind(t *testing.T) {
	plan, err := extension.Load(writeExtension(t, schemaFor("renamed_output.cypher"), map[string]string{"renamed_output.cypher": "// depends-on: none\nRETURN 1"}))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema.Relationships[0].Kind != "PoC_ESC1" {
		t.Fatal("query rename changed published kind")
	}
}

func TestLoadRejectsInvalidExtensions(t *testing.T) {
	var cases = []struct{ name, query, want string }{
		{"missing declaration", "RETURN 1", "missing"},
		{"empty declaration", "// depends-on:\nRETURN 1", "empty"},
		{"malformed dependency", "// depends-on: node\nRETURN 1", "malformed"},
		{"malformed continuation", "// depends-on: node:Domain\n// query missing.cypher\nRETURN 1", "malformed"},
		{"unknown dependency", "// depends-on: property:Name\nRETURN 1", "unknown"},
		{"invalid native label", "// depends-on: node:Bad-Label\nRETURN 1", "invalid"},
		{"invalid prerequisite filename", "// depends-on: query:BadFile.cypher\nRETURN 1", "invalid"},
		{"missing prerequisite", "// depends-on: query:missing.cypher\nRETURN 1", "missing prerequisite"},
		{"duplicate prerequisite", "// depends-on: node:Domain, node:Domain\nRETURN 1", "duplicate"},
		{"none with dependencies", "// depends-on: none\n// depends-on: node:Domain\nRETURN 1", "cannot be combined"},
		{"undeclared parameter", "// depends-on: node:Domain\nRETURN $missing", "add query:missing.cypher"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := extension.Load(writeExtension(t, schemaFor("result.cypher"), map[string]string{"result.cypher": testCase.query}))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %v; expected %q", err, testCase.want)
			}
		})
	}
}

func TestLoadRejectsCycleAndUnusedQueries(t *testing.T) {
	var cases = []struct {
		name    string
		queries map[string]string
		want    string
	}{
		{"cycle", map[string]string{"result.cypher": "// depends-on: query:intermediate.cypher\nRETURN 1", "intermediate.cypher": "// depends-on: query:result.cypher\nRETURN 1"}, "cycle"},
		{"unused", map[string]string{"result.cypher": "// depends-on: none\nRETURN 1", "unused.cypher": "// depends-on: none\nRETURN 1"}, "unused query"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := extension.Load(writeExtension(t, schemaFor("result.cypher"), testCase.queries))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %v; expected %q", err, testCase.want)
			}
		})
	}
}

func TestLoadRejectsInvalidSchema(t *testing.T) {
	var cases = []struct {
		name   string
		change func(*extension.Schema)
		want   string
	}{
		{"reference", func(schema *extension.Schema) { schema.ReferenceCommit = "main" }, "40 hexadecimal"},
		{"published kind", func(schema *extension.Schema) { schema.Relationships[0].Kind = "ESC1" }, "namespace"},
		{"endpoint", func(schema *extension.Schema) { schema.Relationships[0].Endpoints[0].TargetKind = "Bad-Label" }, "invalid"},
		{"missing binding", func(schema *extension.Schema) { schema.Relationships[0].Query = "missing.cypher" }, "missing query"},
		{"duplicate kind", func(schema *extension.Schema) {
			schema.Relationships = append(schema.Relationships, schema.Relationships[0])
		}, "duplicate"},
		{"both output roles", func(schema *extension.Schema) {
			schema.Nodes = []extension.NodeBinding{{Kind: "PoC_Helper", Query: "result.cypher", IDPrefix: "PoC_Helper_"}}
		}, "multiple output"},
		{"helper prefix", func(schema *extension.Schema) {
			schema.Relationships = nil
			schema.Nodes = []extension.NodeBinding{{Kind: "PoC_Helper", Query: "result.cypher", IDPrefix: "native_"}}
		}, "ID prefix"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var schema = schemaFor("result.cypher")
			testCase.change(&schema)
			_, err := extension.Load(writeExtension(t, schema, map[string]string{"result.cypher": "// depends-on: none\nRETURN 1"}))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %v; expected %q", err, testCase.want)
			}
		})
	}
}

func TestLoadRejectsUnknownJSONFieldsAndTrailingJSON(t *testing.T) {
	for _, suffix := range []string{`, "typo": true}`, `} {}`} {
		t.Run(suffix, func(t *testing.T) {
			var (
				directory     = writeExtension(t, schemaFor("result.cypher"), map[string]string{"result.cypher": "// depends-on: none\nRETURN 1"})
				filename      = filepath.Join(directory, "schema.json")
				contents, err = os.ReadFile(filename)
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filename, append(contents[:len(contents)-1], suffix...), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := extension.Load(directory); err == nil {
				t.Fatal("invalid schema JSON accepted")
			}
		})
	}
}

func TestAffectedEvidenceAndPrerequisites(t *testing.T) {
	var (
		schema  = schemaFor("result.cypher")
		queries = map[string]string{
			"constant.cypher":  "// depends-on: none\nRETURN 1",
			"evidence.cypher":  "// depends-on: relationship:Enroll, query:constant.cypher\nRETURN $constant",
			"result.cypher":    "// depends-on: query:evidence.cypher\nRETURN $evidence",
			"unrelated.cypher": "// depends-on: node:Computer\nRETURN 1",
		}
	)
	schema.Nodes = []extension.NodeBinding{{Kind: "PoC_Unrelated", Query: "unrelated.cypher", IDPrefix: "PoC_Unrelated_"}}
	plan, err := extension.Load(writeExtension(t, schema, queries))
	if err != nil {
		t.Fatal(err)
	}
	var cases = []struct {
		name                 string
		nodes, relationships []string
		known                bool
		expected             []string
	}{
		{"unknown coverage", nil, nil, false, []string{"constant.cypher", "evidence.cypher", "result.cypher", "unrelated.cypher"}},
		{"relationship only including removal", nil, []string{"Enroll"}, true, []string{"constant.cypher", "evidence.cypher", "result.cypher"}},
		{"endpoint property change", []string{"Domain"}, nil, true, []string{"constant.cypher", "evidence.cypher", "result.cypher"}},
		{"helper change", []string{"PoC_Unrelated"}, nil, true, []string{"unrelated.cypher"}},
		{"unrelated evidence", []string{"Unknown"}, []string{"Unknown"}, true, nil},
		{"known no changes", nil, nil, true, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			actual := plan.Affected(testCase.nodes, testCase.relationships, testCase.known)
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("affected %v; expected %v", actual, testCase.expected)
			}
		})
	}
}
