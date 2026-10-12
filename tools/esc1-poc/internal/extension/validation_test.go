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

package extension_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/testexamples"
)

func TestCompleteQueryValidation(t *testing.T) {
	for _, testCase := range []struct{ name, source, problem string }{
		{"misspelled match", "// depends-on: node:User\nMACH (person:User) RETURN person.objectid AS source", "line 2"},
		{"missing graph input", "// depends-on: node:User, node:Group\nMATCH (person:User)-[:MemberOf]->(group:Group) RETURN person.objectid AS source", "line 2: missing dependency; add relationship:MemberOf"},
		{"missing answer", "// depends-on: none\nUNWIND $people AS person RETURN person.id AS source", "line 2: missing dependency; add query:people.cypher"},
		{"unsupported subquery", "// depends-on: none\nCALL { RETURN 1 AS value } RETURN value", "file syntax: line 2"},
		{"unsupported expression", "// depends-on: none\nRETURN [n IN [1] | n] AS answer", "language support"},
		{"bad columns", "// depends-on: none\nRETURN 'x' AS source, 'PoC_ESC1' AS kind", "line 2: output: relationship output requires"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := extension.Load(writeExtension(t, schemaFor("result.cypher"), map[string]string{"result.cypher": testCase.source}))
			if err == nil || !strings.Contains(err.Error(), "result.cypher:") || !strings.Contains(err.Error(), testCase.problem) {
				t.Fatalf("got %v; expected %s", err, testCase.problem)
			}
		})
	}
}

func TestTutorialStagesAndShippedFiles(t *testing.T) {
	stages, err := testexamples.Tutorial("../../DEVELOPERS.md")
	if err != nil {
		t.Fatal(err)
	}
	for index, files := range stages {
		directory := t.TempDir()
		for name, source := range files {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
		}
		plan, err := extension.Load(directory)
		if err != nil {
			t.Fatalf("tutorial stage %d: %v", index+1, err)
		}
		expected := []int{1, 2, 3, 3}[index]
		if len(plan.Queries) != expected {
			t.Fatalf("stage %d has %d queries, expected %d", index+1, len(plan.Queries), expected)
		}
	}
	for _, directory := range []string{"../../extension", "../../examples/vault/extension", "../../examples/cross-platform/extension"} {
		if _, err := extension.Load(directory); err != nil {
			t.Fatalf("%s: %v", directory, err)
		}
	}
}
