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
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

type esc1Case struct {
	name    string
	change  string
	sources []string
}

func esc1Cases() []esc1Case {
	return []esc1Case{
		{"baseline", "", []string{"User"}},
		{"independent_template_permission_sets", "MATCH (template:CertTemplate), (ca:EnterpriseCA), (user:User)-[ca_right:Enroll]->(ca), (group:Group)-[template_right:Enroll]->(template) DELETE ca_right, template_right CREATE (second:Base:CertTemplate) SET second = properties(template), second.objectid = 'PoC_Test_SecondTemplate' CREATE (everyone:Base:Group {objectid:'PoC_Test_S-S-1-1-0',poc_fixture:true})-[:Enroll]->(template), (second)-[:PublishedTo]->(ca), (user)-[:Enroll]->(second), (group)-[:Enroll]->(ca)", []string{"Group", "User"}},
		{"independent_ca_permission_sets", "MATCH (ca:EnterpriseCA), (host:Computer), (root:RootCA), (store:NTAuthStore), (template:CertTemplate), (group:Group) CREATE (second:Base:EnterpriseCA) SET second = properties(ca), second.objectid = 'PoC_Test_SecondCA' CREATE (host)-[:HostsCAService]->(second), (second)-[:EnterpriseCAFor]->(root), (second)-[:TrustedForNTAuth]->(store), (template)-[:PublishedTo]->(second), (group)-[:Enroll]->(second)", []string{"Group", "User"}},
		{"approval_required", "MATCH (n:CertTemplate) SET n.requiresmanagerapproval = true", nil},
		{"missing_approval", "MATCH (n:CertTemplate) REMOVE n.requiresmanagerapproval", nil},
		{"missing_signatures_even_for_v1", "MATCH (n:CertTemplate) SET n.schemaversion = 1 REMOVE n.authorizedsignatures", nil},
		{"v1_ignores_signature_count", "MATCH (n:CertTemplate) SET n.schemaversion = 1, n.authorizedsignatures = 5", []string{"User"}},
		{"v2_requires_no_signatures", "MATCH (n:CertTemplate) SET n.authorizedsignatures = 1", nil},
		{"authentication_disabled", "MATCH (n:CertTemplate) SET n.authenticationenabled = false", nil},
		{"subject_cannot_be_supplied", "MATCH (n:CertTemplate) SET n.enrolleesuppliessubject = false", nil},
		{"disabled_host", "MATCH (n:Computer) SET n.enabled = false", nil},
		{"unknown_host_domain", "MATCH (n:Computer) SET n.domainsid = 'not-collected'", nil},
		{"unknown_ca_domain_falls_back", "MATCH (n:EnterpriseCA) REMOVE n.domainsid", []string{"User"}},
		{"host_in_other_forest", "MATCH (host:Computer) CREATE (:Base:Domain {objectid:'PoC_Test_Other', name:'OTHER', poc_fixture:true}) SET host.domainsid = 'PoC_Test_Other'", nil},
		{"host_in_same_forest", "MATCH (host:Computer), (domain:Domain) CREATE (:Base:Domain {objectid:'PoC_Test_Other', name:'OTHER', poc_fixture:true})-[:SameForestTrust]->(domain) SET host.domainsid = 'PoC_Test_Other'", []string{"User"}},
		{"forest_direction_matters", "MATCH (host:Computer), (domain:Domain) CREATE (domain)-[:SameForestTrust]->(:Base:Domain {objectid:'PoC_Test_Other', name:'OTHER', poc_fixture:true}) SET host.domainsid = 'PoC_Test_Other'", nil},
		{"missing_ntauth", "MATCH ()-[r:TrustedForNTAuth]->() DELETE r", nil},
		{"missing_root_trust", "MATCH ()-[r:RootCAFor]->() DELETE r", nil},
		{"ca_generic_all_is_not_enroll", "MATCH (u:User)-[r:Enroll]->(ca:EnterpriseCA) DELETE r CREATE (u)-[:GenericAll]->(ca)", nil},
		{"template_generic_all", "MATCH (g:Group)-[r:Enroll]->(ct:CertTemplate) DELETE r CREATE (g)-[:GenericAll]->(ct)", []string{"User"}},
		{"template_all_extended_rights", "MATCH (g:Group)-[r:Enroll]->(ct:CertTemplate) DELETE r CREATE (g)-[:AllExtendedRights]->(ct)", []string{"User"}},
		{"local_membership", "MATCH (u:User)-[r:MemberOf]->(g:Group) DELETE r CREATE (local:Base:LocalGroup {objectid:'PoC_Test_Local', poc_fixture:true}), (u)-[:MemberOfLocalGroup]->(local), (local)-[:MemberOf]->(g)", []string{"User"}},
		{"special_ca_group", "MATCH (u:User)-[r:Enroll]->(ca:EnterpriseCA) DELETE r CREATE (special:Base:Group {objectid:'PoC_Test_S-S-1-5-11',poc_fixture:true})-[:Enroll]->(ca)", []string{"Group"}},
		{"nested_special_ca_group", "MATCH (u:User)-[r:Enroll]->(ca:EnterpriseCA) DELETE r CREATE (special:Base:Group {objectid:'PoC_Test_S-S-1-5-11',poc_fixture:true})-[:MemberOf]->(outer:Base:Group {objectid:'PoC_Test_Outer',poc_fixture:true})-[:Enroll]->(ca)", []string{"Group"}},
		{"both_sides_special", "MATCH (u:User)-[ca_right:Enroll]->(ca:EnterpriseCA), (g:Group)-[template_right:Enroll]->(ct:CertTemplate) DELETE ca_right, template_right CREATE (everyone:Base:Group {objectid:'PoC_Test_S-S-1-1-0',poc_fixture:true})-[:Enroll]->(ct), (auth:Base:Group {objectid:'PoC_Test_S-S-1-5-11',poc_fixture:true})-[:Enroll]->(ca)", []string{"S-S-1-1-0", "S-S-1-5-11"}},
		{"nested_group_shortcut", "MATCH (u:User)-[member:MemberOf]->(g:Group), (u)-[right:Enroll]->(ca:EnterpriseCA) DELETE member, right CREATE (u)-[:MemberOf]->(inner:Base:Group {objectid:'PoC_Test_Inner',poc_fixture:true})-[:MemberOf]->(g), (inner)-[:Enroll]->(ca)", []string{"Inner"}},
	}
}

// These tests exercise the actual Cypher and Neo4j transaction behavior. Their
// expected answers are written here by hand. The separate upload demonstration
// checks answers produced by native BloodHound; these tests do not replace it.
func TestESC1Queries(t *testing.T) {
	var cases = esc1Cases()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var database, plan = setup(t)
			if testCase.change != "" {
				write(t, database, testCase.change, nil)
			}
			report, err := runner.Run(t.Context(), database, plan, options())
			if err != nil {
				t.Fatal(err)
			}
			if !report.Committed {
				t.Fatal("run did not commit")
			}
			actual := read(t, database, "MATCH (s)-[:PoC_ADCSESC1]->(d) RETURN s.objectid AS source, d.objectid AS target ORDER BY source", nil)
			var expected = runner.Rows{}
			for _, source := range testCase.sources {
				expected = append(expected, map[string]any{"source": "PoC_Test_" + source, "target": "PoC_Test_Domain"})
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("connections: got %v, want %v", actual, expected)
			}
		})
	}
}

func TestPublicationAndRollback(t *testing.T) {
	var (
		database, plan = setup(t)
		ctx            = t.Context()
	)
	retainExistingConnections(&plan)
	// Two templates independently support exactly the same pair.
	write(t, database, "MATCH (template:CertTemplate), (ca:EnterpriseCA), (group:Group) CREATE (second:Base:CertTemplate) SET second = properties(template), second.objectid = 'PoC_Test_Second' CREATE (second)-[:PublishedTo]->(ca), (group)-[:Enroll]->(second)", nil)
	nativeBefore := nativeSnapshot(t, database)
	first, err := runner.Run(ctx, database, plan, options())
	if err != nil || first.Added != 1 {
		t.Fatalf("first publication: %+v, %v", first, err)
	}
	edges := read(t, database, "MATCH ()-[edge:PoC_ADCSESC1]->() RETURN id(edge) AS id, properties(edge) AS properties", nil)
	second, err := runner.Run(ctx, database, plan, options())
	if err != nil || second.Unchanged != 1 || second.Added != 0 || second.Removed != 0 || second.HelpersChanged != 0 {
		t.Fatalf("unchanged publication: %+v, %v", second, err)
	}
	if after := nativeSnapshot(t, database); !reflect.DeepEqual(nativeBefore, after) {
		t.Fatal("publication changed native fixture data")
	}
	if after := read(t, database, "MATCH ()-[edge:PoC_ADCSESC1]->() RETURN id(edge) AS id, properties(edge) AS properties", nil); !reflect.DeepEqual(edges, after) {
		t.Fatal("unchanged relationship was replaced or modified")
	}
	write(t, database, "MATCH (template:CertTemplate {objectid:'PoC_Test_Template'}) SET template.requiresmanagerapproval = true", nil)
	third, err := runner.Run(ctx, database, plan, options())
	if err != nil || third.Unchanged != 1 || third.Removed != 0 {
		t.Fatalf("remaining independent support: %+v, %v", third, err)
	}
	write(t, database, "MATCH (template:CertTemplate) SET template.requiresmanagerapproval = true", nil)
	fourth, err := runner.Run(ctx, database, plan, options())
	if err != nil || fourth.Removed != 1 {
		t.Fatalf("confirmed removal of all support: %+v, %v", fourth, err)
	}

	helpersBefore := read(t, database, "MATCH (helper:PoC_DomainHelper) RETURN properties(helper) AS properties", nil)
	write(t, database, "MATCH (domain:Domain) SET domain.name = 'CHANGED DOMAIN'", nil)
	replaceESC1Query(&plan, "RETURN 17 AS source, 'PoC_Test_Domain' AS target")
	failed, err := runner.Run(ctx, database, plan, options())
	if err == nil || !strings.Contains(err.Error(), "source and target must be nonempty objectid strings") || failed.Committed || failed.HelpersChanged != 1 {
		t.Fatalf("invalid final answer must reject after staging helper changes: %+v, %v", failed, err)
	}
	if after := read(t, database, "MATCH (helper:PoC_DomainHelper) RETURN properties(helper) AS properties", nil); !reflect.DeepEqual(helpersBefore, after) {
		t.Fatal("failed final query left helper changes behind")
	}
}

// Literal fault queries return publication pairs directly. Keep their helper
// prerequisite explicit so rejection still proves that staged writes roll back.
func replaceESC1Query(plan *extension.Plan, text string) {
	for index := range plan.Queries {
		if plan.Queries[index].Name != "esc1.cypher" {
			continue
		}
		plan.Queries[index].Text = text + ", 'PoC_ADCSESC1' AS kind"
		plan.Queries[index].Overlap = nil
		plan.Queries[index].BatchDependency = ""
		for _, dependency := range plan.Queries[index].Dependencies {
			if dependency.Type == "query" && dependency.Name == "domain_helpers.cypher" {
				return
			}
		}
		plan.Queries[index].Dependencies = append(plan.Queries[index].Dependencies, extension.Dependency{Type: "query", Name: "domain_helpers.cypher"})
		return
	}
}

func TestDatabaseRejectsAnalysisWrites(t *testing.T) {
	var database, _ = setup(t)
	transaction, err := database.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Read(t.Context(), "CREATE (:PoC_TestForbidden {objectid:'forbidden'}) RETURN 1", nil); err == nil {
		t.Fatal("a write was accepted as an analysis query")
	}
	if rows := read(t, database, "MATCH (node:PoC_TestForbidden) RETURN node.objectid AS id", nil); len(rows) != 0 {
		t.Fatal("rejected analysis query wrote data")
	}
}

func TestCyclesPreservePreviousResults(t *testing.T) {
	var database, plan = setup(t)
	if _, err := runner.Run(t.Context(), database, plan, options()); err != nil {
		t.Fatal(err)
	}
	before := read(t, database, "MATCH ()-[edge:PoC_ADCSESC1]->() RETURN id(edge) AS id", nil)
	write(t, database, "MATCH (group:Group) CREATE (group)-[:MemberOf]->(group)", nil)
	if _, err := runner.Run(t.Context(), database, plan, options()); err == nil {
		t.Fatal("circular membership was accepted")
	}
	if after := read(t, database, "MATCH ()-[edge:PoC_ADCSESC1]->() RETURN id(edge) AS id", nil); !reflect.DeepEqual(before, after) {
		t.Fatal("cycle rejection changed published results")
	}
}

// These retention tests choose KeepExisting explicitly. Separate policy tests
// cover the default replacement and property merging behavior.
func retainExistingConnections(plan *extension.Plan) {
	for index := range plan.Schema.Relationships {
		plan.Schema.Relationships[index].ExistingEdgePolicy = extension.KeepExisting
	}
}

func options() runner.Options {
	// These fixtures deliberately supply native prerequisite relationships
	// directly. No claim of completed API ingestion or native parity is made.
	return runner.Options{BatchID: "fixture-only-test", IngestionComplete: true, WritersPaused: true}
}

func setup(t *testing.T) (*graphdb.Database, extension.Plan) {
	t.Helper()
	var address = os.Getenv("NEO4J_TEST_URI")
	if address == "" {
		t.Skip("NEO4J_TEST_URI must point to the separate, empty test database")
	}
	database, err := graphdb.Open(t.Context(), address, "neo4j", "experiment-only", "neo4j")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if rows := read(t, database, "MATCH (node) RETURN count(node) AS count", nil); len(rows) != 1 || rows[0]["count"] != int64(0) {
		t.Fatal("test database is not empty; refusing to modify it")
	}
	plan, err := extension.Load("../../extension")
	if err != nil {
		t.Fatal(err)
	}
	// These small database fixtures use written expectations. Uploaded live
	// demonstrations separately check BloodHound's calculated ESC1 answers.
	plan.Schema.CompareNativeESC1 = false
	useIntegrationOwner(&plan)
	t.Cleanup(func() {
		// Delete only this test's marked input nodes and explicitly owned output.
		// Never use the repository's whole-graph erasure helper here.
		write(t, database, "MATCH (node) WHERE node.poc_fixture = true OR node.poc_owner = 'esc1-poc/integration-test' OR node.poc_owner STARTS WITH 'esc1-poc/integration-test/' DETACH DELETE node", nil)
	})
	write(t, database, `CREATE
		(domain:Base:Domain {objectid:'PoC_Test_Domain', name:'TEST DOMAIN', poc_fixture:true}),
		(host:Base:Computer {objectid:'PoC_Test_Host', domainsid:'PoC_Test_Domain', enabled:true, poc_fixture:true}),
		(ca:Base:EnterpriseCA {objectid:'PoC_Test_CA', domainsid:'PoC_Test_Domain', poc_fixture:true}),
		(root:Base:RootCA {objectid:'PoC_Test_Root', poc_fixture:true}),
		(store:Base:NTAuthStore {objectid:'PoC_Test_Store', poc_fixture:true}),
		(template:Base:CertTemplate {objectid:'PoC_Test_Template', requiresmanagerapproval:false, authenticationenabled:true, enrolleesuppliessubject:true, schemaversion:2, authorizedsignatures:0, poc_fixture:true}),
		(principal:Base:User {objectid:'PoC_Test_User', poc_fixture:true}),
		(group:Base:Group {objectid:'PoC_Test_Group', poc_fixture:true}),
		(host)-[:HostsCAService]->(ca), (ca)-[:EnterpriseCAFor]->(root),
		(root)-[:RootCAFor]->(domain), (ca)-[:TrustedForNTAuth]->(store),
		(store)-[:NTAuthStoreFor]->(domain), (template)-[:PublishedTo]->(ca),
		(group)-[:Enroll]->(template), (principal)-[:Enroll]->(ca),
		(principal)-[:MemberOf]->(group)`, nil)
	return database, plan
}

// Loaded plans carry each output's original extension owner. Tests that choose
// a cleanup owner must change both the run record and the output bindings.
func useIntegrationOwner(plan *extension.Plan) {
	plan.Schema.Owner = "esc1-poc/integration-test"
	for index := range plan.Schema.Nodes {
		plan.Schema.Nodes[index].Owner = plan.Schema.Owner
	}
	for index := range plan.Schema.Relationships {
		plan.Schema.Relationships[index].Owner = plan.Schema.Owner
	}
}

func read(t *testing.T, database runner.Database, query string, parameters map[string]any) runner.Rows {
	t.Helper()
	transaction, err := database.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	rows, err := transaction.Read(context.Background(), query, parameters)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func write(t *testing.T, database *graphdb.Database, query string, parameters map[string]any) {
	t.Helper()
	transaction, err := database.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Write(context.Background(), query, parameters); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func nativeSnapshot(t *testing.T, database runner.Database) string {
	t.Helper()
	var (
		nodes = read(t, database, "MATCH (n) WHERE n.poc_fixture = true RETURN id(n) AS id, labels(n) AS kinds, properties(n) AS properties ORDER BY id", nil)
		edges = read(t, database, "MATCH (source)-[edge]->(target) WHERE source.poc_fixture = true AND target.poc_fixture = true AND type(edge) <> 'PoC_ADCSESC1' RETURN id(edge) AS id, type(edge) AS kind, properties(edge) AS properties ORDER BY id", nil)
	)
	return fmt.Sprintf("%v\n%v", nodes, edges)
}
