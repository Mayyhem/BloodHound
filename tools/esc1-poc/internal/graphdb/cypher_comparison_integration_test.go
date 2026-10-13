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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/specterops/dawgs/graph"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// Keep all five comparisons on the same engine. Only the selected authoring
// choice changes; in particular group selection retains the trusted-CA batches.
func cypherComparisonPlan(t *testing.T, variant string) extension.Plan {
	t.Helper()
	var (
		directory      = t.TempDir()
		ordinaryReach  = variant == "ordinary-reach" || variant == "ordinary-all"
		ordinaryGroups = variant == "ordinary-groups" || variant == "ordinary-all"
		noBatch        = variant == "no-batch" || variant == "ordinary-all"
		reachFiles     = map[string]bool{"membership_reach.cypher": true, "forest_reach.cypher": true, "ca_root_reach.cypher": true, "ca_auth_reach.cypher": true}
	)
	entries, err := os.ReadDir("../../extension")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".cypher" && filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		source := filepath.Join("../../extension", entry.Name())
		if ordinaryReach && reachFiles[entry.Name()] || ordinaryGroups && entry.Name() == "esc1.cypher" {
			source = filepath.Join("../../examples/esc1-cypher", entry.Name())
		}
		content, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		if entry.Name() == "esc1.cypher" {
			if ordinaryGroups && !noBatch {
				text = strings.Replace(text, "// depends-on:", "// batch-on: trusted_cas.cypher\n// depends-on:", 1)
			}
			if noBatch {
				text = strings.ReplaceAll(text, "// batch-on: trusted_cas.cypher\n", "")
			}
		}
		if err := os.WriteFile(filepath.Join(directory, entry.Name()), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := extension.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	// These fixtures supply native prerequisite relationships directly. Their
	// written expected answers are checked below; no native analysis was run.
	plan.Schema.CompareNativeESC1 = false
	return plan
}

// Existing written expectations cover special groups, missing properties,
// overlapping groups and trust choices. Run every choice against those answers,
// rather than using one implementation's answer as the only source of truth.
func TestCypherAlternatives(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated test databases are required")
	}
	for _, testCase := range cypherComparisonCases() {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				neoDatabase, _ = setup(t)
				expected       = runner.Rows{}
			)
			if testCase.change != "" {
				write(t, neoDatabase, testCase.change, nil)
			}
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			for _, source := range testCase.sources {
				expected = append(expected, map[string]any{"from_id": "PoC_Test_" + source, "to_id": "PoC_Test_Domain"})
			}
			for _, backend := range []struct {
				name     string
				database runner.Database
			}{{"neo4j", neoDatabase}, {"postgresql", postgresDatabase}} {
				t.Run(backend.name, func(t *testing.T) {
					var expectedAnswers map[string]map[string]bool
					var expectedHelpers runner.Rows
					var expectedProposals map[string]int
					var unchangedInputs = nativeSnapshot(t, backend.database)
					choices := []string{"advanced", "ordinary-reach", "no-batch", "ordinary-groups", "ordinary-all"}
					if testCase.name == "independent_ca_permission_sets" {
						choices = append(choices, "advanced/batch-1", "ordinary-groups/batch-1", "advanced/batch-2", "ordinary-groups/batch-2")
					}
					for _, choice := range choices {
						t.Run(choice, func(t *testing.T) {
							variant := strings.Split(choice, "/")[0]
							plan := cypherComparisonPlan(t, variant)
							settings := options()
							settings.Limits = runner.DefaultLimits()
							if strings.HasSuffix(choice, "batch-1") {
								settings.Limits.BatchRows = 1
							}
							if strings.HasSuffix(choice, "batch-2") {
								settings.Limits.BatchRows = 2
							}
							proposals := map[string]int{}
							settings.ObserveAnswer = func(name string, row map[string]any) error {
								if name != "esc1.cypher" {
									return nil
								}
								encoded, err := json.Marshal(row)
								if err != nil {
									return err
								}
								proposals[string(encoded)]++
								return nil
							}
							recorder := &comparisonDatabase{Database: backend.database, answers: map[string]map[string]bool{}}
							report, err := runner.Run(t.Context(), recorder, plan, settings)
							if err != nil || !report.Committed {
								t.Fatalf("%s did not save its complete answer: %v", variant, err)
							}
							actual := read(t, backend.database, "MATCH (source)-[:PoC_ADCSESC1]->(target) RETURN source.objectid AS from_id, target.objectid AS to_id", nil)
							sort.Slice(actual, func(left, right int) bool {
								return actual[left]["from_id"].(string) < actual[right]["from_id"].(string)
							})
							if !reflect.DeepEqual(actual, expected) {
								t.Fatalf("got %v, want %v", actual, expected)
							}
							helpers := read(t, backend.database, "MATCH (n:PoC_DomainHelper) RETURN properties(n) AS properties ORDER BY n.objectid", nil)
							if choice == "advanced" {
								expectedAnswers, expectedHelpers = recorder.answers, helpers
								expectedProposals = proposals
							} else if !reflect.DeepEqual(recorder.answers, expectedAnswers) || !reflect.DeepEqual(helpers, expectedHelpers) {
								t.Fatalf("intermediate answers or saved helpers differ: answers=%v expected=%v helpers=%v expected=%v", recorder.answers, expectedAnswers, helpers, expectedHelpers)
							}
							if !reflect.DeepEqual(proposals, expectedProposals) {
								t.Fatalf("supporting rows differ: got %v, want %v", proposals, expectedProposals)
							}
							if nativeSnapshot(t, backend.database) != unchangedInputs {
								t.Fatal("the calculation changed its input graph")
							}
							for _, row := range read(t, backend.database, "MATCH ()-[r:PoC_ADCSESC1]->() RETURN properties(r) AS properties", nil) {
								if len(row["properties"].(map[string]any)) != 0 {
									t.Fatalf("unexpected saved connection details: %v", row)
								}
							}
							t.Logf("elapsed=%s query_rows=%v", report.Elapsed, report.QueryRows)
						})
					}
				})
			}
		})
	}
}

func cypherComparisonCases() []esc1Case {
	var (
		cases = esc1Cases()
		chain strings.Builder
	)
	chain.WriteString("MATCH (u:User)-[r:MemberOf]->(g:Group) DELETE r CREATE (u)")
	for index := 0; index < 20; index++ {
		fmt.Fprintf(&chain, "-[:MemberOf]->(:Base:Group {objectid:'PoC_Test_Deep%d', poc_fixture:true})", index)
	}
	chain.WriteString("-[:MemberOf]->(g)")
	return append(cases,
		esc1Case{"shared_authority_support", "MATCH (ca:EnterpriseCA), (host:Computer), (root:RootCA), (store:NTAuthStore), (template:CertTemplate), (u:User) CREATE (second:Base:EnterpriseCA) SET second = properties(ca), second.objectid = 'PoC_Test_SecondCA' CREATE (host)-[:HostsCAService]->(second), (second)-[:EnterpriseCAFor]->(root), (second)-[:TrustedForNTAuth]->(store), (template)-[:PublishedTo]->(second), (u)-[:Enroll]->(second)", []string{"User"}},
		esc1Case{"nested_groups_both_remain", "MATCH (u:User)-[r:Enroll]->(ca:EnterpriseCA), (g:Group) DELETE r CREATE (u)-[:MemberOf]->(inner:Base:Group {objectid:'PoC_Test_Inner',poc_fixture:true})-[:MemberOf]->(outer:Base:Group {objectid:'PoC_Test_Outer',poc_fixture:true})-[:MemberOf]->(g), (outer)-[:Enroll]->(ca)", []string{"Inner", "Outer"}},
		esc1Case{"equal_overlapping_groups_and_direct_user", "MATCH (u:User), (g:Group), (ca:EnterpriseCA), (template:CertTemplate) CREATE (a:Base:Group {objectid:'PoC_Test_EqualA',poc_fixture:true}), (b:Base:Group {objectid:'PoC_Test_EqualB',poc_fixture:true}), (u)-[:MemberOf]->(a)-[:MemberOf]->(g), (u)-[:MemberOf]->(b)-[:MemberOf]->(g), (a)-[:Enroll]->(ca), (b)-[:Enroll]->(ca), (u)-[:Enroll]->(template)", []string{"EqualA", "EqualB", "User"}},
		esc1Case{"equal_overlapping_groups_reverse_creation", "MATCH (u:User), (g:Group), (ca:EnterpriseCA), (template:CertTemplate) CREATE (b:Base:Group {objectid:'PoC_Test_EqualB',poc_fixture:true}), (a:Base:Group {objectid:'PoC_Test_EqualA',poc_fixture:true}), (u)-[:MemberOf]->(a)-[:MemberOf]->(g), (u)-[:MemberOf]->(b)-[:MemberOf]->(g), (a)-[:Enroll]->(ca), (b)-[:Enroll]->(ca), (u)-[:Enroll]->(template)", []string{"EqualA", "EqualB", "User"}},
		esc1Case{"no_template_permissions", "MATCH ()-[r:Enroll]->(:CertTemplate) DELETE r", nil},
		esc1Case{"empty_root_certificate_reach", "MATCH (:EnterpriseCA)-[r:EnterpriseCAFor]->() DELETE r", nil},
		esc1Case{"parallel_membership_links", "MATCH (u:User)-[:MemberOf]->(g:Group) CREATE (u)-[:MemberOfLocalGroup]->(g)", []string{"User"}},
		esc1Case{"forest_self_link", "MATCH (d:Domain) CREATE (d)-[:SameForestTrust]->(d)", []string{"User"}},
		esc1Case{"forest_path_through_non_domain", "MATCH (d:Domain), (host:Computer) CREATE (:Base:Domain {objectid:'PoC_Test_Other',name:'OTHER DOMAIN',poc_fixture:true})-[:SameForestTrust]->(:Base {objectid:'PoC_Test_NonDomain',poc_fixture:true})-[:SameForestTrust]->(d) SET host.domainsid = 'PoC_Test_Other'", nil},
		esc1Case{"membership_beyond_fifteen_links", chain.String(), []string{"User"}},
		esc1Case{"unlabelled_intermediate_member", "MATCH (u:User)-[r:MemberOf]->(g:Group) DELETE r CREATE (u)-[:MemberOf]->(:Base {objectid:'PoC_Test_Unlabelled', poc_fixture:true})-[:MemberOf]->(g)", []string{"User"}},
		esc1Case{"membership_diamond", "MATCH (u:User), (g:Group) CREATE (u)-[:MemberOf]->(:Base:Group {objectid:'PoC_Test_DiamondLeft',poc_fixture:true})-[:MemberOf]->(g), (u)-[:MemberOf]->(:Base:Group {objectid:'PoC_Test_DiamondRight',poc_fixture:true})-[:MemberOf]->(g)", []string{"User"}},
		esc1Case{"forest_loop", "MATCH (d:Domain) CREATE (d)-[:SameForestTrust]->(:Base:Domain {objectid:'PoC_Test_ForestLoop',name:'FOREST LOOP',poc_fixture:true})-[:SameForestTrust]->(d)", []string{"User"}},
		esc1Case{"unrelated_membership_loop", "CREATE (a:Base:Group {objectid:'PoC_Test_UnrelatedA',poc_fixture:true})-[:MemberOf]->(b:Base:Group {objectid:'PoC_Test_UnrelatedB',poc_fixture:true})-[:MemberOf]->(a)", []string{"User"}},
	)
}

// Every failed alternative must preserve the previous connections, helper
// details and successful-run record. The graph changes here are malformed input,
// not new expected answers that a partial calculation may publish.
func TestCypherAlternativesKeepSavedResults(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated test databases are required")
	}
	for _, testCase := range []struct{ name, change string }{
		{"membership_self_link", "MATCH (g:Group) CREATE (g)-[:MemberOf]->(g)"},
		{"root_certificate_self_link", "MATCH (root:RootCA) CREATE (root)-[:RootCAFor]->(root)"},
		{"authentication_self_link", "MATCH (store:NTAuthStore) CREATE (store)-[:TrustedForNTAuth]->(store)"},
		{"authentication_loop", "MATCH (ca:EnterpriseCA), (store:NTAuthStore) CREATE (store)-[:TrustedForNTAuth]->(ca)"},
		{"certificate_loop_below_start", "MATCH (root:RootCA) CREATE (root)-[:IssuedSignedBy]->(a:Base {objectid:'PoC_Test_LoopA',poc_fixture:true})-[:IssuedSignedBy]->(b:Base {objectid:'PoC_Test_LoopB',poc_fixture:true})-[:IssuedSignedBy]->(a)"},
		{"membership_loop", "MATCH (u:User)-[:MemberOf]->(g:Group) CREATE (g)-[:MemberOf]->(u)"},
		{"certificate_loop", "MATCH (ca:EnterpriseCA)-[:EnterpriseCAFor]->(root:RootCA) CREATE (root)-[:EnterpriseCAFor]->(ca)"},
		{"missing_identifier", "MATCH (u:User) REMOVE u.objectid"},
		{"blank_identifier", "MATCH (u:User) SET u.objectid = ' '"},
		{"duplicate_identifier", "MATCH (u:User), (g:Group) SET u.objectid = g.objectid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var neoDatabase, _ = setup(t)
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			for _, backend := range []struct {
				name     string
				database runner.Database
			}{{"neo4j", neoDatabase}, {"postgresql", postgresDatabase}} {
				t.Run(backend.name, func(t *testing.T) {
					if report, err := runner.Run(t.Context(), backend.database, cypherComparisonPlan(t, "advanced"), options()); err != nil || !report.Committed {
						t.Fatalf("initial save: %v", err)
					}
					before := cypherSavedOutputs(t, backend.database)
					if backend.name == "neo4j" {
						write(t, neoDatabase, testCase.change, nil)
					} else if err := driver.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
						result := transaction.Query(testCase.change, nil)
						defer result.Close()
						for result.Next() {
						}
						return result.Error()
					}); err != nil {
						t.Fatal(err)
					}
					// Identifier changes alter endpoint properties, so snapshot saved
					// objects by their internal IDs, independently of those properties.
					for _, variant := range []string{"advanced", "ordinary-reach", "no-batch", "ordinary-groups", "ordinary-all"} {
						t.Run(variant, func(t *testing.T) {
							report, err := runner.Run(t.Context(), backend.database, cypherComparisonPlan(t, variant), options())
							if err == nil || report.Committed {
								t.Fatalf("invalid evidence was accepted: %+v", report)
							}
							message := strings.ToLower(err.Error())
							if strings.Contains(testCase.name, "identifier") {
								if !strings.Contains(message, "missing, empty, or duplicate objectids") {
									t.Fatalf("expected identity rejection, got %v", err)
								}
							} else {
								cause := "circular"
								if variant == "ordinary-reach" || variant == "ordinary-all" {
									cause = "zero"
								}
								if !strings.Contains(message, cause) || report.Execution.Failure == nil || !strings.HasSuffix(report.Execution.Failure.Query, "_reach.cypher") {
									t.Fatalf("expected %s rejection in a reach query, got %v", cause, err)
								}
							}
							if after := cypherSavedOutputs(t, backend.database); !reflect.DeepEqual(before, after) {
								t.Fatalf("failed calculation changed saved results: before=%v after=%v", before, after)
							}
						})
					}
				})
			}
		})
	}
}

func cypherSavedOutputs(t *testing.T, database runner.Database) []runner.Rows {
	t.Helper()
	return []runner.Rows{
		read(t, database, "MATCH (s)-[r:PoC_ADCSESC1]->(d) RETURN id(r) AS edge_id, id(s) AS from_id, id(d) AS to_id, properties(r) AS properties ORDER BY id(r)", nil),
		read(t, database, "MATCH (n) WHERE n:PoC_DomainHelper OR n:PoC_Run RETURN id(n) AS node_id, labels(n) AS kinds, properties(n) AS properties ORDER BY id(n)", nil),
	}
}
