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

//go:build integration

package graphdb_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/testexamples"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

func TestTutorialStagesOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated test graphs are required")
	}
	stages, err := testexamples.Tutorial("../../DEVELOPERS.md")
	if err != nil {
		t.Fatal(err)
	}
	neoDatabase, _ := setup(t)
	// All three users have direct membership, but only one explicitly has enabled=true.
	write(t, neoDatabase, `MATCH (person:User), (group:Group)
 SET person.enabled = true
 CREATE (disabled:Base:User {objectid:'PoC_Test_Disabled',enabled:false,poc_fixture:true})-[:MemberOf]->(group),
 (missing:Base:User {objectid:'PoC_Test_Missing',poc_fixture:true})-[:MemberOf]->(group)`, nil)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			t.Cleanup(func() {
				if err := backend.native.WriteTransaction(context.Background(), func(transaction graph.Transaction) error {
					return transaction.Nodes().Filter(query.StringStartsWith(query.NodeProperty("objectid"), "PoC_Start_Note_")).Delete()
				}); err != nil {
					t.Error(err)
				}
			})
			var finalPlan extension.Plan
			var priorHelpers runner.Rows
			var priorEdge runner.Rows
			for index, files := range stages {
				directory := t.TempDir()
				for name, source := range files {
					if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				plan, err := extension.Load(directory)
				if err != nil {
					t.Fatal(err)
				}
				finalPlan = plan
				report, err := nativeIntegrationRun(t, backend, plan, options())
				if err != nil || !report.Committed || report.Execution.Save != "succeeded" {
					t.Fatalf("stage %d: %+v, %v", index+1, report, err)
				}
				expected := runner.Rows{{"source": "PoC_Test_User", "target": "PoC_Test_Group"}}
				if index == 0 {
					expected = runner.Rows{{"source": "PoC_Test_Disabled", "target": "PoC_Test_Group"}, {"source": "PoC_Test_Missing", "target": "PoC_Test_Group"}, {"source": "PoC_Test_User", "target": "PoC_Test_Group"}}
				}
				actual := read(t, backend.database, "MATCH (source)-[:PoC_Start_DirectMemberOf]->(target) RETURN source.objectid AS source, target.objectid AS target ORDER BY source", nil)
				sort.Slice(actual, func(first, second int) bool {
					return actual[first]["source"].(string) < actual[second]["source"].(string)
				})
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("stage %d: got %v, want %v", index+1, actual, expected)
				}
				helpers := read(t, backend.database, "MATCH (note) WHERE note.objectid STARTS WITH 'PoC_Start_Note_' RETURN note.objectid AS objectid, note.user_id AS user_id ORDER BY objectid", nil)
				if index < 2 && len(helpers) != 0 {
					t.Fatal("schema declaration created helpers")
				}
				if index >= 2 {
					expectedHelpers := runner.Rows{{"objectid": "PoC_Start_Note_PoC_Test_Disabled", "user_id": "PoC_Test_Disabled"}, {"objectid": "PoC_Start_Note_PoC_Test_Missing", "user_id": "PoC_Test_Missing"}, {"objectid": "PoC_Start_Note_PoC_Test_User", "user_id": "PoC_Test_User"}}
					if !reflect.DeepEqual(helpers, expectedHelpers) {
						t.Fatalf("stage %d helpers: %v", index+1, helpers)
					}
					if index == 2 {
						priorHelpers = helpers
						priorEdge = read(t, backend.database, "MATCH ()-[edge:PoC_Start_DirectMemberOf]->() RETURN id(edge) AS id", nil)
					}
				}
				if index == 3 {
					if !reflect.DeepEqual(priorHelpers, helpers) {
						t.Fatal("property addition changed helpers")
					}
					if actual := read(t, backend.database, "MATCH ()-[edge:PoC_Start_DirectMemberOf]->() RETURN id(edge) AS id", nil); !reflect.DeepEqual(actual, priorEdge) {
						t.Fatal("property update replaced the connection")
					}
					if actual := read(t, backend.database, "MATCH ()-[edge:PoC_Start_DirectMemberOf]->() RETURN edge.evidence AS evidence", nil); !reflect.DeepEqual(actual, runner.Rows{{"evidence": "direct membership of an enabled user"}}) {
						t.Fatalf("property update: %v", actual)
					}
				}
			}
			properties := graph.NewProperties()
			properties.Set("enabled", false)
			if err := backend.native.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
				return transaction.Nodes().Filter(query.Kind(query.Node(), graph.StringKind("User"))).Update(properties)
			}); err != nil {
				t.Fatal(err)
			}
			empty, err := nativeIntegrationRun(t, backend, finalPlan, options())
			if err != nil || !empty.Committed || empty.DesiredCounts["PoC_Start_DirectMemberOf"] != 0 || empty.Removed != 1 {
				t.Fatalf("complete empty answer: %+v, %v", empty, err)
			}
			if actual := read(t, backend.database, "MATCH ()-[edge:PoC_Start_DirectMemberOf]->() RETURN id(edge) AS id", nil); len(actual) != 0 {
				t.Fatal("empty answer retained an old connection")
			}
		})
	}
}

// The other shipped examples already have both-database tests for ESC1 and
// cross-extension helpers. This covers the actual Vault files on both drivers.
func TestShippedVaultOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated test graphs are required")
	}
	neoDatabase, plan := setupVault(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			report, err := nativeIntegrationRun(t, backend, plan, options())
			if err != nil || !report.Committed {
				t.Fatalf("vault: %+v, %v", report, err)
			}
			expected := runner.Rows{{"source": "VAULT-DEMO", "target": "PoC_Test_User"}}
			actual := read(t, backend.database, "MATCH (source)-[:PoC_Vault_HasAccountFor]->(target) RETURN source.objectid AS source, target.objectid AS target", nil)
			sort.Slice(actual, func(first, second int) bool {
				return actual[first]["source"].(string) < actual[second]["source"].(string)
			})
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("vault connections: %v", actual)
			}
			if rows := read(t, backend.database, "MATCH (helper:PoC_Vault_Helper) RETURN helper.objectid AS objectid", nil); len(rows) != 1 || rows[0]["objectid"] != "PoC_Vault_Helper_VAULT-DEMO" {
				t.Fatalf("vault helper: %v", rows)
			}
		})
	}
}
