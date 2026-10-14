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
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func TestShippedExtensionsSelectNativeComparison(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		directory string
		compare   bool
	}{
		{"ESC1", filepath.Join("..", "..", "extension"), true},
		{"Vault", filepath.Join("..", "..", "examples", "vault", "extension"), false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var plan, err = extension.Load(testCase.directory)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Schema.CompareNativeESC1 != testCase.compare {
				t.Fatalf("native comparison = %t; expected %t", plan.Schema.CompareNativeESC1, testCase.compare)
			}
		})
	}
}

func TestNativeComparisonRequiresESC1RelationshipProducer(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		schema extension.Schema
		valid  bool
	}{
		{"disabled for another relationship", extension.Schema{Relationships: []extension.RelationshipBinding{{Kind: "PoC_Other", Query: "result.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}}}}, true},
		{"enabled for ESC1 relationship", extension.Schema{CompareNativeESC1: true, Relationships: []extension.RelationshipBinding{{Kind: "PoC_ADCSESC1", Query: "result.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}}}}, true},
		{"enabled for another relationship", extension.Schema{CompareNativeESC1: true, Relationships: []extension.RelationshipBinding{{Kind: "PoC_Other", Query: "result.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}}}}, false},
		{"ESC1 node is not a relationship", extension.Schema{CompareNativeESC1: true, Nodes: []extension.NodeBinding{{Kind: "PoC_ADCSESC1", Query: "result.cypher", IDPrefix: "PoC_Helper_"}}}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				schema  = testCase.schema
				queries = map[string]string{"result.cypher": "// depends-on: none\nRETURN 'user' AS source, 'domain' AS target"}
			)
			schema.Name = "native comparison test"
			schema.ReferenceCommit = strings.Repeat("a", 40)
			var _, err = extension.Load(writeExtension(t, schema, queries))
			if testCase.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "compare_native_esc1") || !strings.Contains(err.Error(), "PoC_ADCSESC1") {
				t.Fatalf("expected missing ESC1 producer error, got %v", err)
			}
		})
	}
}

func TestRenamingESC1QueryPreservesNativeComparison(t *testing.T) {
	var (
		schema  = schemaFor("renamed_result.cypher")
		queries = map[string]string{"renamed_result.cypher": "// depends-on: none\nRETURN 'user' AS source, 'domain' AS target"}
	)
	schema.CompareNativeESC1 = true
	schema.Relationships[0].Kind = "PoC_ADCSESC1"
	var plan, err = extension.Load(writeExtension(t, schema, queries))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Schema.CompareNativeESC1 || plan.Schema.Relationships[0].Query != "PoC/renamed_result.cypher" || plan.Schema.Relationships[0].Kind != "PoC_ADCSESC1" {
		t.Fatalf("renamed output changed native comparison configuration: %+v", plan.Schema)
	}
}
