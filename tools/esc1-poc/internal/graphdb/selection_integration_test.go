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
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

// Fail after publication batches to check the real transaction rollback.
type selectionRecordFailureDatabase struct{ runner.Database }
type selectionRecordFailureTransaction struct {
	runner.Transaction
	publisher runner.Publisher
}

func (s *selectionRecordFailureDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &selectionRecordFailureTransaction{Transaction: transaction, publisher: transaction.(runner.Publisher)}, nil
}
func (s *selectionRecordFailureTransaction) PutNode(ctx context.Context, kind string, properties map[string]any) error {
	if kind == "PoC_Run" {
		return fmt.Errorf("injected selected run record failure")
	}
	return s.publisher.PutNode(ctx, kind, properties)
}
func (s *selectionRecordFailureTransaction) CreateRelationships(ctx context.Context, kind string, relationships []runner.Relationship) (int, error) {
	return s.publisher.CreateRelationships(ctx, kind, relationships)
}
func (s *selectionRecordFailureTransaction) UpdateRelationships(ctx context.Context, kind string, updates []runner.RelationshipUpdate) (int, error) {
	return s.publisher.UpdateRelationships(ctx, kind, updates)
}
func (s *selectionRecordFailureTransaction) DeleteRelationships(ctx context.Context, kind string, identifiers []int64) (int, error) {
	return s.publisher.DeleteRelationships(ctx, kind, identifiers)
}

func setSelectionEvidence(t *testing.T, database *graph.DatabaseSwitch, enabled bool, name string) {
	t.Helper()
	var properties = graph.NewProperties()
	// Set records a requested change. AsProperties describes values already
	// loaded from storage, so it would not ask the native driver to save them.
	properties.Set("selection_enabled", enabled)
	properties.Set("selection_version", name)
	if err := database.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
		return transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_User")).Update(properties)
	}); err != nil {
		t.Fatal(err)
	}
}

func selectionSnapshot(t *testing.T, database runner.Database) []runner.Rows {
	t.Helper()
	return []runner.Rows{
		read(t, database, "MATCH (source)-[edge:PoC_ADCSESC1]->(target) RETURN id(edge) AS id, properties(edge) AS properties", nil),
		read(t, database, "MATCH (source)-[edge:PoC_SelectionIndependent]->(target) RETURN id(edge) AS id, properties(edge) AS properties", nil),
		read(t, database, "MATCH (item:PoC_SelectionHelper) RETURN id(item) AS id, properties(item) AS properties", nil),
		read(t, database, "MATCH (item:PoC_SelectionIndependentHelper) RETURN id(item) AS id, properties(item) AS properties", nil),
		read(t, database, "MATCH (item:PoC_Run) RETURN id(item) AS id, properties(item) AS properties", nil),
	}
}

func TestSelectivePublicationAndRollbackOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, fixture = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			var (
				plan       = fixture
				runOptions = options()
			)
			plan = selectionIntegrationPlan(plan)
			setSelectionEvidence(t, backend.native, true, "initial")
			report, err := runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Committed || report.Added != 2 {
				t.Fatalf("complete baseline: %+v error=%v", report, err)
			}
			before := selectionSnapshot(t, backend.database)
			setSelectionEvidence(t, backend.native, true, "changed")
			runOptions.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"User", "Base"}}
			failed, err := runner.Run(t.Context(), &selectionRecordFailureDatabase{Database: backend.database}, plan, runOptions)
			if err == nil || failed.Committed || failed.HelpersChanged != 1 || failed.Added != 1 || failed.Removed != 1 {
				t.Fatalf("late selected failure: %+v error=%v", failed, err)
			}
			if after := selectionSnapshot(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatalf("selected rollback changed saved output: before=%v after=%v", before, after)
			}
			report, err = runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Committed || report.Added != 1 || report.Removed != 1 || len(report.Queries) != 2 {
				t.Fatalf("selected replacement: %+v error=%v", report, err)
			}
			after := selectionSnapshot(t, backend.database)
			if reflect.DeepEqual(before[0], after[0]) || !reflect.DeepEqual(before[1], after[1]) || !reflect.DeepEqual(before[3], after[3]) {
				t.Fatal("selected replacement changed skipped relationships or helpers")
			}
			setSelectionEvidence(t, backend.native, false, "empty")
			report, err = runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Committed || report.Removed != 1 || report.Added != 0 || len(report.Queries) != 2 {
				t.Fatalf("selected empty publication: %+v error=%v", report, err)
			}
			empty := selectionSnapshot(t, backend.database)
			if len(empty[0]) != 0 || !reflect.DeepEqual(after[1], empty[1]) || !reflect.DeepEqual(after[3], empty[3]) {
				t.Fatal("empty selected result changed skipped output")
			}
		})
	}
}

func selectionIntegrationPlan(plan extension.Plan) extension.Plan {
	plan.Schema.CompareNativeESC1 = false
	plan.Schema.Nodes = []extension.NodeBinding{
		{Kind: "PoC_SelectionHelper", Query: "selected_helper.cypher", IDPrefix: "PoC_SelectionHelper_"},
		{Kind: "PoC_SelectionIndependentHelper", Query: "independent_helper.cypher", IDPrefix: "PoC_SelectionIndependentHelper_"},
	}
	plan.Schema.Relationships = []extension.RelationshipBinding{
		{Kind: "PoC_ADCSESC1", Query: "selected.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
		{Kind: "PoC_SelectionIndependent", Query: "independent.cypher", Endpoints: []extension.Endpoint{{SourceKind: "Group", TargetKind: "Domain"}}},
	}
	plan.Queries = []extension.Query{
		{Name: "selected_helper.cypher", Parameter: "selected_helper", Text: "MATCH (source:User {objectid: 'PoC_Test_User'}) RETURN 'PoC_SelectionHelper_example' AS id, ['PoC_SelectionHelper'] AS kinds, {name:source.selection_version} AS properties", Dependencies: []extension.Dependency{{Type: "node", Name: "User"}}},
		{Name: "selected.cypher", Parameter: "selected", Text: "MATCH (source:User {objectid: 'PoC_Test_User'}) WHERE source.selection_enabled = true RETURN source.objectid AS source, 'PoC_Test_Domain' AS target", Dependencies: []extension.Dependency{{Type: "node", Name: "User"}, {Type: "query", Name: "selected_helper.cypher"}}},
		{Name: "independent_helper.cypher", Parameter: "independent_helper", Text: "MATCH (source:Group {objectid: 'PoC_Test_Group'}) RETURN 'PoC_SelectionIndependentHelper_example' AS id, ['PoC_SelectionIndependentHelper'] AS kinds, {name:'independent'} AS properties", Dependencies: []extension.Dependency{{Type: "node", Name: "Group"}}},
		{Name: "independent.cypher", Parameter: "independent", Text: "MATCH (source:Group {objectid: 'PoC_Test_Group'}) RETURN source.objectid AS source, 'PoC_Test_Domain' AS target, {name:'independent'} AS properties", Dependencies: []extension.Dependency{{Type: "node", Name: "Group"}, {Type: "query", Name: "independent_helper.cypher"}}},
	}
	return plan
}

func selectionServiceFiles(t *testing.T, plan extension.Plan) map[string]string {
	t.Helper()
	var (
		files      = map[string]string{}
		definition = extension.Definition{
			Schema:         extension.Metadata{Name: plan.Schema.Name, DisplayName: "Observed selective processing", Version: "v1.0.0", Namespace: "PoC"},
			PostProcessing: &extension.ProcessingSettings{ReferenceCommit: plan.Schema.ReferenceCommit},
		}
	)
	for _, binding := range plan.Schema.Nodes {
		definition.NodeKinds = append(definition.NodeKinds, extension.NodeKind{Name: binding.Kind, PostProcessing: &extension.NodeProcessing{Query: binding.Query, IDPrefix: binding.IDPrefix}})
	}
	for _, binding := range plan.Schema.Relationships {
		definition.RelationshipKinds = append(definition.RelationshipKinds, extension.RelationshipKind{Name: binding.Kind, CompatibleKinds: binding.Endpoints, PostProcessing: &extension.RelationshipProcessing{Query: binding.Query}})
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	files["schema.json"] = string(encoded)
	for _, analysisQuery := range plan.Queries {
		var declarations []string
		var prerequisite string
		for _, dependency := range analysisQuery.Dependencies {
			declarations = append(declarations, dependency.Type+":"+dependency.Name)
			if dependency.Type == "query" {
				prerequisite = "WITH $" + strings.TrimSuffix(dependency.Name, ".cypher") + " AS prerequisite_rows\n"
			}
		}
		files[analysisQuery.Name] = "// depends-on: " + strings.Join(declarations, ", ") + "\n" + prerequisite + analysisQuery.Text
	}
	return files
}

func TestAutomaticObservedSelectionOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, fixture = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			var (
				plan  = selectionIntegrationPlan(fixture)
				token = "selection-integration-token-01234567890123456789"
			)
			// This rule reads Base evidence but publishes a Group -> Domain pair.
			// User's Base co-label must be observed even when Update filters User.
			plan.Schema.Relationships = append(plan.Schema.Relationships, extension.RelationshipBinding{Kind: "PoC_SelectionBase", Query: "base.cypher", Endpoints: []extension.Endpoint{{SourceKind: "Group", TargetKind: "Domain"}}})
			plan.Queries = append(plan.Queries, extension.Query{Name: "base.cypher", Parameter: "base", Text: "MATCH (evidence:Base {objectid:'PoC_Test_User'}) MATCH (source:Group {objectid:'PoC_Test_Group'}) RETURN source.objectid AS source, 'PoC_Test_Domain' AS target, {name:evidence.selection_version} AS properties", Dependencies: []extension.Dependency{{Type: "node", Name: "Base"}, {Type: "node", Name: "Group"}}})
			setSelectionEvidence(t, backend.native, true, "baseline")
			store, err := ruleservice.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Update(selectionServiceFiles(t, plan)); err != nil {
				t.Fatal(err)
			}
			handler, err := ruleservice.NewHandler(store, token)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			service, err := postprocessing.New(postprocessing.Configuration{URL: server.URL, Token: token, Directory: t.TempDir(), Backend: backend.name}, backend.native)
			if err != nil {
				t.Fatal(err)
			}
			finishAnalysis := func(ctx context.Context) error {
				committed, err := service.AfterAnalysis(ctx)
				if err != nil {
					return err
				}
				if !committed {
					return fmt.Errorf("observed analysis did not publish")
				}
				return nil
			}
			if err := service.WithAnalysis(t.Context(), finishAnalysis); err != nil {
				t.Fatal(err)
			}
			if status := store.LatestStatus(); status.State != "success" || !strings.Contains(status.Message, "Finished 5 of 5 queries") {
				t.Fatalf("full baseline status: %+v", status)
			}
			before := selectionSnapshot(t, backend.database)
			baseBefore := read(t, backend.database, "MATCH ()-[edge:PoC_SelectionBase]->() RETURN id(edge) AS id, properties(edge) AS properties", nil)
			if err := service.WithAnalysis(t.Context(), func(ctx context.Context) error {
				var properties = graph.NewProperties()
				properties.Set("selection_version", "observed change")
				if err := backend.native.WriteTransaction(ctx, func(transaction graph.Transaction) error {
					return transaction.Nodes().Filter(query.And(query.Kind(query.Node(), graph.StringKind("User")), query.Equals(query.NodeProperty("objectid"), "PoC_Test_User"))).Update(properties)
				}); err != nil {
					return err
				}
				return finishAnalysis(ctx)
			}); err != nil {
				t.Fatal(err)
			}
			if status := store.LatestStatus(); status.State != "success" || !strings.Contains(status.Message, "Finished 3 of 5 queries") {
				t.Fatalf("observed User and Base changes did not select exact dependency closure: %+v", status)
			}
			after := selectionSnapshot(t, backend.database)
			if reflect.DeepEqual(before[0], after[0]) || !reflect.DeepEqual(before[1], after[1]) || !reflect.DeepEqual(before[3], after[3]) {
				t.Fatal("automatic selection changed skipped Group outputs or failed to replace selected User output")
			}
			baseAfter := read(t, backend.database, "MATCH ()-[edge:PoC_SelectionBase]->() RETURN id(edge) AS id, properties(edge) AS properties", nil)
			if len(baseAfter) != 1 || reflect.DeepEqual(baseBefore, baseAfter) {
				t.Fatal("Base co-label dependency was not republished")
			}
		})
	}
}

func TestSelectionServiceBundleIsValid(t *testing.T) {
	var fixture, err = extension.Load("../../extension")
	if err != nil {
		t.Fatal(err)
	}
	store, err := ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(selectionServiceFiles(t, selectionIntegrationPlan(fixture))); err != nil {
		t.Fatal(err)
	}
}
