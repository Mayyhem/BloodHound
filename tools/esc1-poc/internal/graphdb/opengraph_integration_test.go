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
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// These checks use the real database to exercise the same publication rules
// with an OpenGraph source and an Active Directory destination. The upload
// demonstration separately proves that BloodHound imports the actual files.
func TestOpenGraphPublication(t *testing.T) {
	var database, plan = setupVault(t)
	retainExistingConnections(&plan)
	inputsBefore := vaultInputSnapshot(t, database)
	first, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || !first.Committed || first.Added != 1 || first.NativeComparison != nil || len(first.Queries) != 2 {
		t.Fatalf("first vault publication: %+v, %v", first, err)
	}
	expected := []runner.Pair{{Source: "VAULT-DEMO", Target: "PoC_Test_User"}}
	if !reflect.DeepEqual(first.Desired["PoC_Vault_HasAccountFor"], expected) {
		t.Fatalf("vault connections: %+v", first.Desired)
	}
	if after := vaultInputSnapshot(t, database); after != inputsBefore {
		t.Fatal("publication changed collected input, native answers, or another owner's connection")
	}
	edges := read(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN id(edge) AS id, properties(edge) AS properties", nil)
	second, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || second.Unchanged != 1 || second.HelpersChanged != 0 || second.Added != 0 || second.Removed != 0 {
		t.Fatalf("unchanged vault publication: %+v, %v", second, err)
	}
	if after := read(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN id(edge) AS id, properties(edge) AS properties", nil); !reflect.DeepEqual(edges, after) {
		t.Fatal("unchanged vault connection lost its identity or properties")
	}

	// Delete an actual containment edge in this isolated test database.
	// This is not collector omission or a new collector deletion feature.
	// Removing one of two supports must keep the existing connection intact.
	write(t, database, "MATCH ()-[edge:PoC_Vault_ContainsAccount]->(:PoC_Vault_Account {objectid:'VAULT-ACCOUNT-1'}) DELETE edge", nil)
	oneSupport, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || oneSupport.Unchanged != 1 || oneSupport.Removed != 0 {
		t.Fatalf("remaining vault support: %+v, %v", oneSupport, err)
	}
	if after := read(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN id(edge) AS id, properties(edge) AS properties", nil); !reflect.DeepEqual(edges, after) {
		t.Fatal("removing one support replaced the still-valid connection")
	}

	// A confirmed relationship removal must trigger recalculation and removal.
	write(t, database, "MATCH ()-[edge:PoC_Vault_ContainsAccount]->(:PoC_Vault_Account {objectid:'VAULT-ACCOUNT-2'}) DELETE edge", nil)
	changed := options()
	changed.Changes = runner.Changes{CoverageKnown: true, Relationships: []string{"PoC_Vault_ContainsAccount"}}
	removed, err := runner.Run(t.Context(), database, plan, changed)
	if err != nil || removed.Removed != 1 || removed.Skipped || len(removed.Queries) != 2 {
		t.Fatalf("confirmed OpenGraph relationship removal: %+v, %v", removed, err)
	}
	// Collector objects and the other owner's connection still belong to them.
	if rows := read(t, database, "MATCH (vault:PoC_Vault_Container)-[edge:PoC_OtherResult]->(:User) RETURN edge.poc_owner AS owner", nil); !reflect.DeepEqual(rows, runner.Rows{{"owner": "another-extension"}}) {
		t.Fatalf("another owner's result changed: %v", rows)
	}

	// Restore the containment edge deleted from this test database.
	write(t, database, "MATCH (vault:PoC_Vault_Container {objectid:'VAULT-DEMO'}), (account:PoC_Vault_Account {objectid:'VAULT-ACCOUNT-1'}) CREATE (vault)-[:PoC_Vault_ContainsAccount]->(account)", nil)
	restored, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || restored.Added != 1 {
		t.Fatalf("restore vault connection: %+v, %v", restored, err)
	}
	restoredEdges := read(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN id(edge) AS id, properties(edge) AS properties", nil)
	// Disabled users retain the same factual account identity association.
	write(t, database, "MATCH (principal:User) SET principal.enabled = false", nil)
	changed.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"User"}}
	disabled, err := runner.Run(t.Context(), database, plan, changed)
	if err != nil || disabled.Unchanged != 1 || disabled.Added != 0 || disabled.Removed != 0 || disabled.Skipped {
		t.Fatalf("disabled AD user: %+v, %v", disabled, err)
	}
	if after := read(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN id(edge) AS id, properties(edge) AS properties", nil); !reflect.DeepEqual(restoredEdges, after) {
		t.Fatal("disabling the user changed the factual account identity association")
	}
}

func TestOpenGraphFailuresPreservePublishedGraph(t *testing.T) {
	for _, failure := range []string{"final_query", "wrong_endpoint", "foreign_owner", "incomplete_upload"} {
		t.Run(failure, func(t *testing.T) {
			var (
				database, plan = setupVault(t)
				runOptions     = options()
			)
			first, err := runner.Run(t.Context(), database, plan, runOptions)
			if err != nil || !first.Committed {
				t.Fatalf("initial vault publication: %+v, %v", first, err)
			}
			// A new name will stage a helper update before the final failure.
			write(t, database, "MATCH (vault:PoC_Vault_Container) SET vault.name = 'CHANGED VAULT'", nil)
			if failure == "foreign_owner" {
				write(t, database, "MATCH ()-[edge:PoC_Vault_HasAccountFor]->() SET edge.poc_owner = 'someone-else'", nil)
			}
			if failure == "incomplete_upload" {
				runOptions.IngestionComplete = false
			}
			for index := range plan.Queries {
				if plan.Queries[index].Name != "account_users.cypher" {
					continue
				}
				switch failure {
				case "final_query":
					plan.Queries[index].Text = "RETURN 1 / 0 AS source, 'PoC_Test_User' AS target, 'PoC_Vault_HasAccountFor' AS kind"
				case "wrong_endpoint":
					plan.Queries[index].Text = "RETURN 'VAULT-DEMO' AS source, 'PoC_Test_Domain' AS target, 'PoC_Vault_HasAccountFor' AS kind"
				}
			}
			before := guardGraphSnapshot(t, database)
			failed, err := runner.Run(t.Context(), database, plan, runOptions)
			if err == nil || failed.Committed {
				t.Fatalf("expected failure: %+v, %v", failed, err)
			}
			if failure != "incomplete_upload" && failed.HelpersChanged != 1 {
				t.Fatalf("failure did not follow a staged helper update: %+v", failed)
			}
			if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(after, before) {
				t.Fatal("failed vault run changed helpers, connections, input, or saved run state")
			}
		})
	}
}

func TestUnrelatedMembershipLoopDoesNotBlockVault(t *testing.T) {
	var database, plan = setupVault(t)
	write(t, database, "MATCH (group:Group) CREATE (group)-[:MemberOf]->(group)", nil)
	report, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || !report.Committed || report.Added != 1 {
		t.Fatalf("vault calculation does not read group membership: %+v, %v", report, err)
	}
	// The ESC1 plan still rejects precisely this unsupported membership case.
	esc1, err := extension.Load("../../extension")
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(t.Context(), database, esc1, options())
	if err == nil || !strings.Contains(err.Error(), "circular membership or trust") {
		t.Fatalf("ESC1 membership guard was lost: %v", err)
	}
}

func setupVault(t *testing.T) (*graphdb.Database, extension.Plan) {
	t.Helper()
	var database, _ = setup(t)
	plan, err := extension.Load("../../examples/vault/extension")
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the test's existing cleanup owner; production examples have distinct
	// owners and saved run identities. Every collected test node is marked too.
	useIntegrationOwner(&plan)
	write(t, database, `MATCH (principal:User), (domain:Domain)
        SET principal.enabled = true
        CREATE (principal)-[:ADCSESC1]->(domain),
        (vault:PoC_Vault_Container:PoC_Vault_Base {objectid:'VAULT-DEMO', name:'TEST VAULT', poc_fixture:true}),
        (first:PoC_Vault_Account:PoC_Vault_Base {objectid:'VAULT-ACCOUNT-1', user_sid:'PoC_Test_User', poc_fixture:true}),
        (second:PoC_Vault_Account:PoC_Vault_Base {objectid:'VAULT-ACCOUNT-2', user_sid:'PoC_Test_User', poc_fixture:true}),
        (vault)-[:PoC_Vault_ContainsAccount]->(first), (vault)-[:PoC_Vault_ContainsAccount]->(second),
        (vault)-[:PoC_OtherResult {poc_owner:'another-extension'}]->(principal)`, nil)
	return database, plan
}

func vaultInputSnapshot(t *testing.T, database *graphdb.Database) string {
	t.Helper()
	// Only the vault's derived output is excluded. The snapshot includes all
	// collected objects, native results, and the other extension's connection.
	var rows = read(t, database, `MATCH (node) WHERE node.poc_fixture = true
        RETURN 'node' AS item, id(node) AS id, labels(node) AS kinds, properties(node) AS properties
        UNION ALL
        MATCH (source)-[edge]->(target)
        WHERE source.poc_fixture = true AND target.poc_fixture = true AND type(edge) <> 'PoC_Vault_HasAccountFor'
        RETURN 'edge' AS item, id(edge) AS id, [type(edge)] AS kinds, properties(edge) AS properties
        ORDER BY item, id`, nil)
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
