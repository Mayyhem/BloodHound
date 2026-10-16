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
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

// editorSnapshot includes every saved graph object, relationship, and property
// including PoC_Run. Unlike the older fixture snapshot, it deliberately does
// not omit derived kinds: an editor test must leave the whole saved graph alone.
func editorSnapshot(t *testing.T, database runner.Database) string {
	t.Helper()
	nodes := read(t, database, "MATCH (item) RETURN id(item) AS id, labels(item) AS kinds, properties(item) AS properties ORDER BY id", nil)
	edges := read(t, database, "MATCH (source)-[edge]->(target) RETURN id(edge) AS id, type(edge) AS kind, source.objectid AS source, target.objectid AS target, properties(edge) AS properties ORDER BY id", nil)
	return fmt.Sprintf("%v\n%v", nodes, edges)
}

// editorRelationshipPrerequisite makes a second complete producer whose output
// must be staged for the final query's declared relationship dependency.
func editorRelationshipPrerequisite(plan extension.Plan) (extension.Plan, string) {
	result := plan
	target := plan.Schema.Relationships[0]
	prerequisite := plan.Queries[0]
	for _, query := range plan.Queries {
		if query.Name == target.Query {
			prerequisite = query
			break
		}
	}
	prerequisite.Name = "editor_prerequisite.cypher"
	prerequisite.Parameter = "editor_prerequisite"
	result.Queries = append(append([]extension.Query(nil), plan.Queries...), prerequisite)
	result.Schema.Relationships = append([]extension.RelationshipBinding(nil), plan.Schema.Relationships...)
	result.Schema.Relationships = append(result.Schema.Relationships, extension.RelationshipBinding{Kind: "PoC_EditorPrerequisite", Query: prerequisite.Name, Endpoints: target.Endpoints})
	for index := range result.Queries {
		if result.Queries[index].Name == target.Query {
			result.Queries[index].Dependencies = append(result.Queries[index].Dependencies, extension.Dependency{Type: "relationship", Name: "PoC_EditorPrerequisite"})
		}
	}
	return result, target.Query
}

func nativeEditorTest(t *testing.T, backend nativeIntegrationBackend, plan extension.Plan, settings runner.Options) (runner.Report, error) {
	t.Helper()
	var report runner.Report
	err := backend.gate.Alone(t.Context(), 30*time.Second, func(ctx context.Context) error {
		var testErr error
		report, testErr = graphdb.TestNative(ctx, backend.native, backend.name, plan, settings)
		return testErr
	})
	return report, err
}

// TestNativeEditorDiscardOnBothDatabases proves a final editor result can use
// a temporary helper while its test leaves every saved node, relationship,
// property, and PoC_Run record exactly as it found them.
func TestNativeEditorDiscardOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated editor test databases are required")
	}
	neoDatabase, plan := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			before := editorSnapshot(t, backend.database)
			relationshipPlan, target := editorRelationshipPrerequisite(plan)
			settings := options()
			settings.TestTarget = target
			var relationshipVisible bool
			settings.InspectTestAnswer = func(ctx context.Context, transaction runner.Transaction, _ string, _ runner.CompleteAnswer) error {
				rows, err := transaction.Read(ctx, "MATCH ()-[:PoC_EditorPrerequisite]->() RETURN 1 AS visible", nil)
				relationshipVisible = err == nil && len(rows) > 0
				return err
			}
			report, err := nativeEditorTest(t, backend, relationshipPlan, settings)
			if err != nil || !report.Tested || !report.Discarded || report.Committed || report.HelpersChanged == 0 || report.Added == 0 || !relationshipVisible {
				t.Fatalf("editor test report=%+v error=%v", report, err)
			}
			if after := editorSnapshot(t, backend.database); after != before {
				t.Fatal("editor test changed saved graph")
			}
			// Cancel only after temporary helpers and relationships are visible.
			// This exercises rollback after real work, rather than a refused start.
			err = backend.gate.Alone(t.Context(), 30*time.Second, func(turnContext context.Context) error {
				testContext, cancel := context.WithCancel(turnContext)
				defer cancel()
				settings.InspectTestAnswer = func(ctx context.Context, transaction runner.Transaction, _ string, _ runner.CompleteAnswer) error {
					rows, err := transaction.Read(ctx, "MATCH ()-[:PoC_EditorPrerequisite]->() RETURN 1 AS visible", nil)
					if err != nil || len(rows) == 0 {
						return fmt.Errorf("temporary prerequisite unavailable before cancellation")
					}
					cancel()
					return testContext.Err()
				}
				var testError error
				report, testError = graphdb.TestNative(testContext, backend.native, backend.name, relationshipPlan, settings)
				return testError
			})
			if !errors.Is(err, context.Canceled) || report.Tested || report.Discarded {
				t.Fatalf("mid-flight canceled test=%+v error=%v", report, err)
			}
			if after := editorSnapshot(t, backend.database); after != before {
				t.Fatal("mid-flight cancellation changed saved graph")
			}
			// A bad final result must also undo prerequisites already staged.
			invalid := relationshipPlan
			invalid.Queries = append([]extension.Query(nil), relationshipPlan.Queries...)
			for index := range invalid.Queries {
				if invalid.Queries[index].Name == target {
					invalid.Queries[index].Text = "RETURN 'missing' AS source, 'missing' AS target, 'WrongKind' AS kind"
					invalid.Queries[index].OutputKind = "PoC_ADCSESC1"
				}
			}
			settings.InspectTestAnswer = nil
			report, err = nativeEditorTest(t, backend, invalid, settings)
			if err == nil || report.Tested || report.Discarded {
				t.Fatalf("invalid final result=%+v error=%v", report, err)
			}
			if after := editorSnapshot(t, backend.database); after != before {
				t.Fatal("failed final query changed saved graph")
			}

		})
	}
}

// TestNativeEditorFreshKindsAndFailures checks first-use metadata preparation
// and the failure paths. Snapshots intentionally exclude kind/index catalogs:
// those are declared metadata, while graph evidence must always be discarded.
func TestNativeEditorFreshKindsAndFailures(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated editor test databases are required")
	}
	neoDatabase, plan := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			before := editorSnapshot(t, backend.database)
			fresh, target := editorRelationshipPrerequisite(plan)
			fresh.Schema.Relationships = append([]extension.RelationshipBinding(nil), plan.Schema.Relationships...)
			fresh.Schema.Relationships = append(fresh.Schema.Relationships, extension.RelationshipBinding{Kind: "PoC_EditorFresh", Query: "editor_prerequisite.cypher", Endpoints: plan.Schema.Relationships[0].Endpoints})
			for index := range fresh.Schema.Relationships {
				if fresh.Schema.Relationships[index].Kind == "PoC_EditorPrerequisite" {
					fresh.Schema.Relationships[index].Kind = "PoC_EditorFresh"
				}
			}
			for index := range fresh.Queries {
				for dependency := range fresh.Queries[index].Dependencies {
					if fresh.Queries[index].Dependencies[dependency].Name == "PoC_EditorPrerequisite" {
						fresh.Queries[index].Dependencies[dependency].Name = "PoC_EditorFresh"
					}
				}
			}
			settings := options()
			settings.TestTarget = target
			if report, err := nativeEditorTest(t, backend, fresh, settings); err != nil || !report.Discarded {
				t.Fatalf("fresh kind test=%+v error=%v", report, err)
			}
			settings.Limits = runner.DefaultLimits()
			settings.Limits.ChangedObjects = 1
			if report, err := nativeEditorTest(t, backend, plan, settings); err == nil || report.Discarded {
				t.Fatalf("limit test=%+v error=%v", report, err)
			}
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			var report runner.Report
			err := backend.gate.Alone(t.Context(), 30*time.Second, func(context.Context) error {
				report, _ = graphdb.TestNative(cancelled, backend.native, backend.name, plan, settings)
				return nil
			})
			if err != nil || report.Discarded {
				t.Fatalf("canceled test=%+v error=%v", report, err)
			}
			after := editorSnapshot(t, backend.database)
			if after != before {
				t.Fatal("failed or canceled editor test changed saved graph")
			}
		})
	}
}

// This runs the public editor service, including its real result-identifier
// lookup, private archive, file review and one-file activation. The rule store
// and graph are isolated fixtures; the manual-test app is never contacted.
func TestNativeEditorServiceFlowOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated editor test databases are required")
	}
	neoDatabase, _ := setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			before := editorSnapshot(t, backend.database)
			files := map[string]string{}
			entries, err := os.ReadDir("../../extension")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "schema.json" && !strings.HasSuffix(entry.Name(), ".cypher") {
					continue
				}
				contents, err := os.ReadFile(filepath.Join("../../extension", entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				name := entry.Name()
				if strings.HasSuffix(name, ".cypher") {
					name, err = extension.QueryFileKey(name, string(contents))
					if err != nil {
						t.Fatal(err)
					}
				}
				files[name] = string(contents)
			}
			store, err := ruleservice.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			active, err := store.Update(files)
			if err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("t", 32)
			handler, err := ruleservice.NewHandler(store, token)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			snapshot := ruleservice.SchemaSnapshot{Definitions: active.Schemas}
			schemas, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			service, err := postprocessing.New(postprocessing.Configuration{URL: server.URL, Token: token, Directory: t.TempDir(), Backend: backend.name, ReadSchemas: func(context.Context) ([]byte, error) { return schemas, nil }}, backend.native)
			if err != nil {
				t.Fatal(err)
			}
			request := postprocessing.EditorTestRequest{Name: "PoC/editor_nodes.cypher", Text: "// namespace: PoC\n// version: v1.0.0\n// depends-on: node:Domain\nMATCH (domain:Domain) RETURN id(domain) AS graph_id, domain.objectid AS objectid, domain.name AS name", BaseRevision: active.Revision, SchemaRevision: snapshot.Revision()}
			result, err := service.TestAnalysisFile(t.Context(), "owner", request)
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalRows == 0 || len(result.Rows) == 0 {
				t.Fatalf("identifier results missing: rows=%d", result.TotalRows)
			}
			review, err := service.ReviewAnalysisFile(t.Context(), "owner", result.TestID)
			if err != nil {
				t.Fatal(err)
			}
			revision, err := service.ActivateAnalysisFile(t.Context(), "owner", result.TestID, review.Revision)
			if err != nil {
				t.Fatal(err)
			}
			// A file that reads internal graph IDs from the activated file works
			// because the editor always recalculates that earlier file as well.
			request.Name = "PoC/editor_consumer.cypher"
			request.BaseRevision = revision
			request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: query:editor_nodes\nUNWIND $editor_nodes AS row RETURN row.graph_id AS graph_id"
			consumer, err := service.TestAnalysisFile(t.Context(), "owner", request)
			if err != nil {
				t.Fatal(err)
			}
			if consumer.TotalRows != result.TotalRows {
				t.Fatalf("recalculated chain result mismatch: rows=%d", consumer.TotalRows)
			}
			request.Name = "PoC/editor_proposed.cypher"
			request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nRETURN 'PoC_EditorNew_test' AS id, ['PoC_EditorNew'] AS kinds, {name: 'New test helper'} AS properties"
			proposed, err := service.TestAnalysisFile(t.Context(), "owner", request)
			if err != nil {
				t.Fatal(err)
			}
			if proposed.TotalRows != 1 || len(proposed.Rows) != 1 {
				t.Fatal("new proposed helper was not shown as one result row")
			}
			if _, err := service.ReviewAnalysisFile(t.Context(), "owner", proposed.TestID); err == nil {
				t.Fatal("undeclared output kind passed activation review")
			}
			if after := editorSnapshot(t, backend.database); after != before {
				t.Fatal("editor service flow changed saved graph")
			}
		})
	}
}
