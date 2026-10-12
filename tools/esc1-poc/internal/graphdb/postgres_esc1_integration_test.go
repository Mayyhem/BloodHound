// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
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
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/drivers/pg"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
	"github.com/specterops/dawgs/util/size"
)

// Fixtures use Neo4j's existing setup queries, then copy input graph objects
// through the structured DAWGS API. The extension still calculates its own
// answers separately on both backends; no native ESC1 answers are copied.
func TestPostgresESC1Queries(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both dedicated Neo4j and PostgreSQL test addresses are required")
	}
	for _, testCase := range esc1Cases() {
		t.Run(testCase.name, func(t *testing.T) {
			neoDatabase, plan := setup(t)
			if testCase.change != "" {
				write(t, neoDatabase, testCase.change, nil)
			}
			postgresDatabase, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			before := postgresFixtureSnapshot(t, driver, true)
			neoBefore := nativeSnapshot(t, neoDatabase)
			for _, database := range []runner.Database{neoDatabase, postgresDatabase} {
				report, err := runner.Run(t.Context(), database, plan, options())
				if err != nil || !report.Committed {
					t.Fatalf("ESC1 publication: %+v: %v", report, err)
				}
				actual := read(t, database, "MATCH (s)-[:PoC_ADCSESC1]->(d) RETURN s.objectid AS source, d.objectid AS target ORDER BY source", nil)
				expected := runner.Rows{}
				for _, source := range testCase.sources {
					expected = append(expected, map[string]any{"source": "PoC_Test_" + source, "target": "PoC_Test_Domain"})
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("connections: got %v, want %v", actual, expected)
				}
			}
			if postgresFixtureSnapshot(t, driver, true) != before || nativeSnapshot(t, neoDatabase) != neoBefore {
				t.Fatal("publication changed input or native fixture data")
			}
		})
	}
}

func postgresESC1Database(t *testing.T) (*graphdb.PostgresDatabase, *pg.Driver) {
	t.Helper()
	var (
		address = os.Getenv("ESC1_POC_POSTGRES_TEST_URI")
		ctx     = t.Context()
		count   int64
	)
	if address != "postgresql://127.0.0.1:25432/esc1_poc_test" {
		t.Fatal("PostgreSQL parity tests require the dedicated port and database")
	}
	configuration, err := pgxpool.ParseConfig(address)
	if err != nil {
		t.Fatal(err)
	}
	configuration.ConnConfig.User, configuration.ConnConfig.Password = "bloodhound", "experiment-only"
	configuration.ConnConfig.Fallbacks, configuration.ConnConfig.TLSConfig = nil, nil
	configuration.ConnConfig.RuntimeParams = map[string]string{}
	configuration.AfterConnect = pg.AfterPooledConnectionEstablished
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	driver := pg.NewDriver(size.Size(32*1024*1024), pool)
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	defaultGraph := graph.Graph{Name: "default"}
	if err = driver.AssertSchema(ctx, graph.Schema{DefaultGraph: defaultGraph, Graphs: []graph.Graph{defaultGraph}}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM node) + (SELECT count(*) FROM edge)").Scan(&count); err != nil || count != 0 {
		t.Fatalf("test graph is not empty; refusing to modify it (count %d): %v", count, err)
	}
	t.Cleanup(func() {
		cleanupContext := context.Background()
		// Remove relationships incident to this test's marked inputs or owned outputs
		// before removing those nodes. No unrelated database objects are erased.
		predicate := "properties ->> 'poc_fixture' = 'true' OR properties ->> 'poc_owner' = 'esc1-poc/integration-test' OR properties ->> 'poc_owner' LIKE 'esc1-poc/integration-test/%'"
		if _, err := pool.Exec(cleanupContext, "DELETE FROM edge WHERE start_id IN (SELECT id FROM node WHERE "+predicate+") OR end_id IN (SELECT id FROM node WHERE "+predicate+")"); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(cleanupContext, "DELETE FROM node WHERE "+predicate); err != nil {
			t.Error(err)
		}
	})
	database, err := graphdb.OpenPostgres(ctx, address, "bloodhound", "experiment-only")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	return database, driver
}

func fixtureKinds(t *testing.T, value any) graph.Kinds {
	t.Helper()
	switch kinds := value.(type) {
	case []string:
		return graph.StringsToKinds(kinds)
	case []any:
		result := graph.Kinds{}
		for _, value := range kinds {
			kind, valid := value.(string)
			if !valid {
				t.Fatalf("fixture kind must be text: %T", value)
			}
			result = append(result, graph.StringKind(kind))
		}
		return result
	default:
		t.Fatalf("fixture kinds must be a list: %T", value)
		return nil
	}
}

func copyESC1Fixture(t *testing.T, source *graphdb.Database, destination *pg.Driver) {
	t.Helper()
	var (
		nodes       = read(t, source, "MATCH (n) WHERE n.poc_fixture = true RETURN id(n) AS id, labels(n) AS kinds, properties(n) AS properties ORDER BY id", nil)
		edges       = read(t, source, "MATCH (s)-[r]->(d) WHERE s.poc_fixture = true AND d.poc_fixture = true AND type(r) <> 'PoC_ADCSESC1' RETURN id(s) AS source, id(d) AS target, type(r) AS kind, properties(r) AS properties ORDER BY id(r)", nil)
		identifiers = map[int64]graph.ID{}
	)
	transaction, err := destination.BeginTransaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	// Creating nodes in ascending source ID order preserves the ordering used
	// by native group shortcuts without requiring identical IDs across databases.
	for _, row := range nodes {
		identifier, valid := row["id"].(int64)
		if !valid {
			t.Fatalf("fixture node ID is %T", row["id"])
		}
		properties, valid := row["properties"].(map[string]any)
		if !valid {
			t.Fatalf("fixture node properties are %T", row["properties"])
		}
		node, err := transaction.CreateNode(graph.AsProperties(properties), fixtureKinds(t, row["kinds"])...)
		if err != nil {
			t.Fatal(err)
		}
		identifiers[identifier] = node.ID
	}
	for _, row := range edges {
		sourceID, sourceOK := row["source"].(int64)
		targetID, targetOK := row["target"].(int64)
		kind, kindOK := row["kind"].(string)
		properties, propertiesOK := row["properties"].(map[string]any)
		start, startOK := identifiers[sourceID]
		end, endOK := identifiers[targetID]
		if !sourceOK || !targetOK || !kindOK || !propertiesOK || !startOK || !endOK {
			t.Fatal("fixture relationship is missing its endpoints or metadata")
		}
		if _, err := transaction.CreateRelationshipByIDs(start, end, graph.StringKind(kind), graph.AsProperties(properties)); err != nil {
			t.Fatal(err)
		}
	}
	if err = transaction.CommitContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// Check transactional rollback after helpers have been calculated and published
// results from a preceding successful run already exist.
func TestPostgresESC1Rollback(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both dedicated database test addresses are required")
	}
	neoDatabase, plan := setup(t)
	database, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	if _, err := runner.Run(t.Context(), database, plan, options()); err != nil {
		t.Fatal(err)
	}
	// A changed input forces a helper update before the final seed fails.
	updatePostgresFixtureProperty(t, driver, "PoC_Test_Domain", "name", "CHANGED DOMAIN")
	before := postgresFixtureSnapshot(t, driver, false)
	for index := range plan.Queries {
		if plan.Queries[index].Name == "esc1.cypher" {
			plan.Queries[index].Text = "RETURN 17 AS left, [] AS right, 'PoC_Test_Domain' AS target, 'PoC_ADCSESC1' AS kind"
		}
	}
	if _, err := runner.Run(t.Context(), database, plan, options()); err == nil {
		t.Fatal("invalid final seed should fail")
	}
	if before != postgresFixtureSnapshot(t, driver, false) {
		t.Fatal("failed calculation changed PostgreSQL graph")
	}
}

func TestPostgresESC1PublicationSupport(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both dedicated database test addresses are required")
	}
	neoDatabase, plan := setup(t)
	retainExistingConnections(&plan)
	write(t, neoDatabase, "MATCH (template:CertTemplate), (ca:EnterpriseCA), (group:Group) CREATE (second:Base:CertTemplate) SET second = properties(template), second.objectid = 'PoC_Test_Second' CREATE (second)-[:PublishedTo]->(ca), (group)-[:Enroll]->(second)", nil)
	database, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	first, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || first.Added != 1 {
		t.Fatalf("first publication: %+v: %v", first, err)
	}
	edges := read(t, database, "MATCH ()-[r:PoC_ADCSESC1]->() RETURN id(r) AS id", nil)
	second, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || second.Added != 0 || second.Removed != 0 || second.Unchanged != 1 {
		t.Fatalf("unchanged publication: %+v: %v", second, err)
	}
	if !reflect.DeepEqual(edges, read(t, database, "MATCH ()-[r:PoC_ADCSESC1]->() RETURN id(r) AS id", nil)) {
		t.Fatal("unchanged publication replaced relationship identity")
	}
	for index, identifier := range []string{"PoC_Test_Template", "PoC_Test_Second"} {
		updatePostgresFixtureProperty(t, driver, identifier, "requiresmanagerapproval", true)
		report, err := runner.Run(t.Context(), database, plan, options())
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 && (report.Unchanged != 1 || report.Removed != 0) {
			t.Fatalf("independent remaining support: %+v", report)
		}
		if index == 1 && report.Removed != 1 {
			t.Fatalf("all support removed: %+v", report)
		}
	}
}

// Raw SQL snapshots are test-only inspection. They avoid adding Cypher
// properties() support solely to inspect the PostgreSQL fixture state.
func postgresFixtureSnapshot(t *testing.T, driver *pg.Driver, inputsOnly bool) string {
	t.Helper()
	transaction, err := driver.BeginTransaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	var (
		nodeFilter string
		edgeFilter string
		nodes      string
		edges      string
	)
	if inputsOnly {
		nodeFilter = " WHERE n.properties ->> 'poc_fixture' = 'true'"
		edgeFilter = " WHERE e.start_id IN (SELECT id FROM node WHERE properties ->> 'poc_fixture' = 'true') AND e.end_id IN (SELECT id FROM node WHERE properties ->> 'poc_fixture' = 'true') AND e.kind_id NOT IN (SELECT id FROM kind WHERE name = 'PoC_ADCSESC1')"
	}
	for _, statement := range []struct {
		sql    string
		output *string
	}{
		{"SELECT coalesce(jsonb_agg(to_jsonb(n) ORDER BY n.id), '[]'::jsonb)::text FROM node n" + nodeFilter, &nodes},
		{"SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY e.id), '[]'::jsonb)::text FROM edge e" + edgeFilter, &edges},
	} {
		result := transaction.Raw(statement.sql, nil)
		if !result.Next() {
			t.Fatalf("fixture snapshot returned no row: %v", result.Error())
		}
		if err := result.Scan(statement.output); err != nil {
			result.Close()
			t.Fatal(err)
		}
		result.Close()
	}
	return nodes + "\n" + edges
}

func updatePostgresFixtureProperty(t *testing.T, driver *pg.Driver, identifier, property string, value any) {
	t.Helper()
	transaction, err := driver.BeginTransaction(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	node, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), identifier)).First()
	if err != nil {
		t.Fatal(err)
	}
	node.Properties.Set(property, value)
	if err = transaction.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	if err = transaction.CommitContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresESC1IdentityGuard(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both dedicated database test addresses are required")
	}
	var cases = []struct {
		name       string
		properties map[string]any
		kinds      []string
		connected  bool
		allowed    bool
	}{
		{"missing ID", map[string]any{}, []string{"Base"}, false, false},
		{"explicit null ID", map[string]any{"objectid": nil}, []string{"Base"}, false, false},
		{"empty ID", map[string]any{"objectid": ""}, []string{"Base"}, false, false},
		{"whitespace ID", map[string]any{"objectid": " \t\n"}, []string{"Base"}, false, false},
		{"duplicate ID", map[string]any{"objectid": "PoC_Test_User"}, []string{"Base"}, false, false},
		{"isolated migration bookkeeping", map[string]any{}, []string{"MigrationData"}, false, true},
		{"connected migration bookkeeping", map[string]any{}, []string{"MigrationData"}, true, false},
		{"mixed-label migration bookkeeping", map[string]any{}, []string{"MigrationData", "Base"}, false, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			neoDatabase, plan := setup(t)
			database, driver := postgresESC1Database(t)
			copyESC1Fixture(t, neoDatabase, driver)
			if _, err := runner.Run(t.Context(), database, plan, options()); err != nil {
				t.Fatal(err)
			}
			transaction, err := driver.BeginTransaction(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback(context.Background())
			properties := graph.AsProperties(testCase.properties)
			properties.Set("poc_fixture", true)
			node, err := transaction.CreateNode(properties, graph.StringsToKinds(testCase.kinds)...)
			if err != nil {
				t.Fatal(err)
			}
			if testCase.connected {
				group, err := transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_Group")).First()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = transaction.CreateRelationshipByIDs(node.ID, group.ID, graph.StringKind("MemberOf"), graph.NewProperties()); err != nil {
					t.Fatal(err)
				}
			}
			if err = transaction.CommitContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := postgresFixtureSnapshot(t, driver, false)
			report, err := runner.Run(t.Context(), database, plan, options())
			if testCase.allowed {
				if err != nil || !report.Committed {
					t.Fatalf("isolated exact MigrationData exemption: %+v: %v", report, err)
				}
			} else {
				if err == nil || report.Committed {
					t.Fatalf("malformed identity was accepted: %+v: %v", report, err)
				}
				if before != postgresFixtureSnapshot(t, driver, false) {
					t.Fatal("identity rejection changed previous published results or input data")
				}
			}
		})
	}
}
