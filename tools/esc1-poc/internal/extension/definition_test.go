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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func modelDefinition() extension.Definition {
	return extension.Definition{
		Schema:            extension.Metadata{Name: "model_example", DisplayName: "Model example", Version: "v1.0.0", Namespace: "PoC"},
		NodeKinds:         []extension.NodeKind{{Name: "PoC_Collected"}},
		ExternalNodeKinds: []string{"User", "Group", "Domain", "Computer"},
		RelationshipKinds: []extension.RelationshipKind{{Name: "PoC_Access", Endpoints: []extension.Endpoint{
			{SourceKind: "User", TargetKind: "Domain"},
			{SourceKind: "Group", TargetKind: "Computer"},
		}}},
	}
}

func parseModel(t *testing.T, definition extension.Definition) (extension.Definition, error) {
	t.Helper()
	var contents, err = json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	return extension.ParseDefinition(contents)
}

func TestDefinitionKeepsExactPairs(t *testing.T) {
	var original = modelDefinition()
	definition, err := parseModel(t, original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definition.RelationshipKinds[0].Endpoints, original.RelationshipKinds[0].Endpoints) {
		t.Fatal("reading the model changed its allowed pairs")
	}
	// A self relationship is a real model entry, even when source and target
	// have the same kind. It must survive without being combined with others.
	original.RelationshipKinds[0].Endpoints = append(original.RelationshipKinds[0].Endpoints, extension.Endpoint{SourceKind: "User", TargetKind: "User"})
	definition, err = parseModel(t, original)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(definition.RelationshipKinds[0].Endpoints, original.RelationshipKinds[0].Endpoints) {
		t.Fatal("reading the model changed its self relationship")
	}
}

func TestDefinitionRejectsIncompleteOrAmbiguousPairs(t *testing.T) {
	var cases = []struct {
		name    string
		change  func(*extension.Definition)
		message string
	}{
		{"missing pairs", func(definition *extension.Definition) { definition.RelationshipKinds[0].Endpoints = nil }, "explicit endpoints"},
		{"duplicate pair", func(definition *extension.Definition) {
			definition.RelationshipKinds[0].Endpoints = append(definition.RelationshipKinds[0].Endpoints, definition.RelationshipKinds[0].Endpoints[0])
		}, "duplicate endpoint pair"},
		{"undeclared source", func(definition *extension.Definition) {
			definition.RelationshipKinds[0].Endpoints[0].SourceKind = "Unknown"
		}, "must be declared"},
		{"undeclared target", func(definition *extension.Definition) {
			definition.RelationshipKinds[0].Endpoints[0].TargetKind = "Unknown"
		}, "must be declared"},
		{"missing target", func(definition *extension.Definition) { definition.RelationshipKinds[0].Endpoints[0].TargetKind = "" }, "must be declared"},
		{"local and external collision", func(definition *extension.Definition) {
			definition.ExternalNodeKinds = append(definition.ExternalNodeKinds, "PoC_Collected")
		}, "duplicate local or external"},
		{"duplicate external declaration", func(definition *extension.Definition) {
			definition.ExternalNodeKinds = append(definition.ExternalNodeKinds, "User")
		}, "duplicate local or external"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var definition = modelDefinition()
			testCase.change(&definition)
			if _, err := parseModel(t, definition); err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error %v; expected %q", err, testCase.message)
			}
		})
	}
}

func TestBloodHoundExportPreservesStandardSectionsAndLocalModel(t *testing.T) {
	var (
		definition = modelDefinition()
		falseValue = false
	)
	definition.PostProcessing = &extension.ProcessingSettings{Owner: "example-owner", ReferenceCommit: strings.Repeat("a", 40)}
	definition.NodeKinds[0].PostProcessing = &extension.NodeProcessing{Query: "helper.cypher", IDPrefix: "PoC_Collected_"}
	definition.NodeKinds[0].DisplayName = "Collected item"
	definition.NodeKinds[0].Description = "An ordinary collected item."
	definition.NodeKinds[0].Color = "#abcdef"
	definition.NodeKinds[0].Icon = "box"
	definition.NodeKinds[0].IsDisplayKind = &falseValue
	definition.NodeKinds[0].Info = json.RawMessage(`{"panel":{"text":"Node details"}}`)
	definition.RelationshipKinds[0].PostProcessing = &extension.RelationshipProcessing{Query: "access.cypher"}
	definition.RelationshipKinds[0].IsTraversable = &falseValue
	definition.RelationshipKinds[0].ExistingEdgePolicy = extension.MergeProperties
	definition.RelationshipKinds[0].Info = json.RawMessage(`{"panel":{"text":"Edge details"}}`)
	definition.Environments = json.RawMessage(`[{"name":"environment_example"}]`)
	definition.RelationshipFindings = json.RawMessage(`[{"name":"finding_example"}]`)
	definition.Queries = json.RawMessage(`[{"name":"query_example","query":"RETURN 1"}]`)
	definition.PZRules = json.RawMessage(`[{"name":"rule_example"}]`)
	parsed, err := parseModel(t, definition)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := parsed.BloodHoundJSON()
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("export changed the local definition")
	}
	var (
		expected map[string]any
		actual   map[string]any
	)
	if err := json.Unmarshal(before, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(exported, &actual); err != nil {
		t.Fatal(err)
	}
	delete(expected, "post_processing")
	delete(expected, "external_node_kinds")
	for _, node := range expected["node_kinds"].([]any) {
		delete(node.(map[string]any), "post_processing")
	}
	for _, edge := range expected["relationship_kinds"].([]any) {
		delete(edge.(map[string]any), "post_processing")
		delete(edge.(map[string]any), "endpoints")
		delete(edge.(map[string]any), "existing_edge_policy")
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("export lost standard details or retained proposed fields:\n%s", exported)
	}
}

func TestCollectedKindsDoNotBecomeManagedOutput(t *testing.T) {
	var (
		definition = modelDefinition()
		directory  = t.TempDir()
	)
	definition.PostProcessing = &extension.ProcessingSettings{Owner: "example-owner", ReferenceCommit: strings.Repeat("a", 40)}
	definition.NodeKinds = append(definition.NodeKinds, extension.NodeKind{Name: "PoC_Helper", PostProcessing: &extension.NodeProcessing{Query: "helper.cypher", IDPrefix: "PoC_Helper_"}})
	definition.RelationshipKinds[0].PostProcessing = &extension.RelationshipProcessing{Query: "access.cypher"}
	definition.RelationshipKinds = append(definition.RelationshipKinds, extension.RelationshipKind{Name: "PoC_CollectedLink", Endpoints: []extension.Endpoint{{SourceKind: "PoC_Collected", TargetKind: "User"}}})
	contents, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "schema.json"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"helper.cypher", "access.cypher"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("// depends-on: none\nRETURN 1"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := extension.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Schema.Nodes) != 1 || plan.Schema.Nodes[0].Kind != "PoC_Helper" || len(plan.Schema.Relationships) != 1 || plan.Schema.Relationships[0].Kind != "PoC_Access" {
		t.Fatal("collected kinds were treated as managed output")
	}
}

func TestExistingEdgePolicyDefaultsAndValidation(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		t.Run(string(policy), func(t *testing.T) {
			var schema = schemaFor("result.cypher")
			schema.Relationships[0].ExistingEdgePolicy = policy
			directory := writeExtension(t, schema, map[string]string{"result.cypher": "// depends-on: none\nRETURN 'source' AS source, 'target' AS target"})
			plan, err := extension.Load(directory)
			if err != nil {
				t.Fatal(err)
			}
			expected := policy
			if expected == "" {
				expected = extension.ReplaceExisting
			}
			if plan.Schema.Relationships[0].EffectiveExistingEdgePolicy() != expected {
				t.Fatal("effective policy was lost")
			}
		})
	}
	for _, value := range []string{`null`, `""`, `false`, `12`, `[]`, `{}`, `"unknown"`} {
		t.Run(value, func(t *testing.T) {
			var definition = modelDefinition()
			contents, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			contents = bytes.Replace(contents, []byte(`"relationship_kinds":[{`), []byte(`"relationship_kinds":[{"existing_edge_policy":`+value+`,`), 1)
			if _, err := extension.ParseDefinition(contents); err == nil {
				t.Fatal("explicit invalid policy accepted")
			}
		})
	}
}
