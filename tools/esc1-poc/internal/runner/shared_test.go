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

package runner_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func sharedPlan() extension.Plan {
	var plan = fixturePlan()
	plan.Schema.Nodes = nil
	plan.Schema.Relationships[0].Query = "a.cypher"
	plan.Schema.Relationships[0].ExistingEdgePolicy = extension.MergeProperties
	plan.Schema.Relationships = append(plan.Schema.Relationships, plan.Schema.Relationships[0])
	plan.Schema.Relationships[1].Query = "b.cypher"
	// Deliberately list B first. Property precedence follows filenames, not
	// the order queries happen to run or finish.
	plan.Queries = []extension.Query{
		{Name: "b.cypher", Parameter: "b", Text: "b"},
		{Name: "a.cypher", Parameter: "a", Text: "a"},
		{Name: "consumer.cypher", Parameter: "consumer", Text: "consumer", Dependencies: []extension.Dependency{{Type: "relationship", Name: "PoC_ADCSESC1"}}},
	}
	return plan
}

func sharedDatabase() *concurrentDatabase {
	var database = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
	database.answers["a"] = runner.Rows{{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "A", "from_a": true}}}
	database.answers["b"] = runner.Rows{{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "B", "from_b": true}}}
	return database
}

func TestSharedProducersCombineBeforeGraphConsumerReads(t *testing.T) {
	var (
		database = sharedDatabase()
		observed bool
	)
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		if query != "consumer" {
			return nil, false, nil
		}
		if len(transaction.staged.Edges) != 1 {
			return nil, true, fmt.Errorf("consumer did not see one combined edge")
		}
		if !reflect.DeepEqual(transaction.staged.Edges[0].Properties, map[string]any{"name": "B", "from_a": true, "from_b": true}) {
			return nil, true, fmt.Errorf("consumer saw incomplete or incorrectly ordered properties: %v", transaction.staged.Edges[0].Properties)
		}
		observed = true
		return nil, true, nil
	}
	report, err := runner.Run(t.Context(), database, sharedPlan(), parallelOptions(t))
	if err != nil || !observed || report.Added != 1 || report.DesiredCounts["PoC_ADCSESC1"] != 1 {
		t.Fatalf("shared publication failed: %+v %v", report, err)
	}
}

func TestFailureAfterSharedPublicationRestoresPreviousGraph(t *testing.T) {
	var (
		database = sharedDatabase()
		before   = cloneGraph(*database.graph)
	)
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		if query != "consumer" {
			return nil, false, nil
		}
		if len(transaction.staged.Edges) != 1 {
			return nil, true, fmt.Errorf("shared edge was not staged")
		}
		return nil, true, fmt.Errorf("injected downstream failure")
	}
	report, err := runner.Run(t.Context(), database, sharedPlan(), parallelOptions(t))
	if err == nil || report.Committed || database.relationshipWrites == 0 || !reflect.DeepEqual(before, *database.graph) {
		t.Fatalf("failed shared publication was not discarded: %+v %v", report, err)
	}
}

func TestSharedProducersCanReadTheirPreviousOutput(t *testing.T) {
	var (
		database = sharedDatabase()
		plan     = sharedPlan()
	)
	database.graph.Edges = []fakeEdge{{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}, Properties: map[string]any{"name": "previous"}}}
	for index := range plan.Queries {
		plan.Queries[index].Dependencies = []extension.Dependency{{Type: "relationship", Name: "PoC_ADCSESC1"}}
	}
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		switch query {
		case "a", "b":
			if len(transaction.staged.Edges) != 1 || transaction.staged.Edges[0].Properties["name"] != "previous" {
				return nil, true, fmt.Errorf("producer %s did not read previous output", query)
			}
			return database.answers[query], true, nil
		case "consumer":
			if len(transaction.staged.Edges) != 1 || transaction.staged.Edges[0].Properties["name"] != "B" {
				return nil, true, fmt.Errorf("consumer did not read combined output")
			}
			return nil, true, nil
		default:
			return nil, false, nil
		}
	}
	report, err := runner.Run(t.Context(), database, plan, parallelOptions(t))
	if err != nil || !report.Committed || report.Updated != 1 || len(report.Queries) != 3 {
		t.Fatalf("shared self-read failed: %+v %v", report, err)
	}
}

func TestRemainingProducerRefreshesAndLastRemovalClearsBeforeConsumer(t *testing.T) {
	var (
		database = sharedDatabase()
		plan     = sharedPlan()
		expected = 1
	)
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		if query != "consumer" {
			return nil, false, nil
		}
		if len(transaction.staged.Edges) != expected {
			return nil, true, fmt.Errorf("consumer saw %d edges, expected %d", len(transaction.staged.Edges), expected)
		}
		return nil, true, nil
	}
	if _, err := runner.Run(t.Context(), database, plan, parallelOptions(t)); err != nil {
		t.Fatal(err)
	}
	plan.Queries = append(plan.Queries[:1], plan.Queries[2:]...)
	plan.Schema.Relationships = plan.Schema.Relationships[1:]
	if _, err := runner.Run(t.Context(), database, plan, parallelOptions(t)); err != nil {
		t.Fatal(err)
	}
	expected = 0
	plan.Queries = plan.Queries[1:]
	plan.Definitions = []extension.Definition{{RelationshipKinds: []extension.RelationshipKind{{Name: "PoC_ADCSESC1", CompatibleKinds: plan.Schema.Relationships[0].Endpoints}}}}
	plan.Schema.Relationships = nil
	plan.RetiredRelationships = []string{"PoC_ADCSESC1"}
	report, err := runner.Run(t.Context(), database, plan, parallelOptions(t))
	if err != nil || report.Removed != 1 || len(database.graph.Edges) != 0 {
		t.Fatalf("last producer cleanup failed: %+v %v", report, err)
	}
	database.graph.Edges = []fakeEdge{{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}}
	options := parallelOptions(t)
	options.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"User"}}
	report, err = runner.Run(t.Context(), database, plan, options)
	if err != nil || report.Removed != 1 || !reflect.DeepEqual(report.Queries, []string{"consumer.cypher"}) {
		t.Fatalf("retired endpoint change did not refresh its graph reader: %+v %v", report, err)
	}
}

func TestReconciliationLeavesOtherEndpointPairsUntouched(t *testing.T) {
	var (
		database = fixtureDatabase()
		outside  = fakeEdge{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "COMPUTER"}}
	)
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "COMPUTER", Kinds: []string{"Computer"}, Properties: map[string]any{}})
	database.graph.Edges = append(database.graph.Edges, outside)
	database.answers[relationshipQuery] = nil
	report := runSuccessfully(t, database)
	if report.Removed != 0 || !reflect.DeepEqual(database.graph.Edges, []fakeEdge{outside}) {
		t.Fatal("changed an edge outside the declared endpoint scope", report)
	}
}

func TestHelperWithLegacyMarkerCanBeUpdated(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
	)
	plan.Queries[0].OutputKind = "PoC_Helper"
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "PoC_Helper_example", Kinds: []string{"PoC_Helper"}, Properties: map[string]any{"name": "old", "poc_owner": "another-extension"}})
	if _, err := runner.Run(t.Context(), database, plan, completeBatch()); err != nil {
		t.Fatal(err)
	}
	for _, node := range database.graph.Nodes {
		if node.ID == "PoC_Helper_example" {
			if node.Properties["name"] != "example" {
				t.Fatal("helper was not updated", node)
			}
			if _, found := node.Properties["poc_owner"]; found {
				t.Fatal("runner retained an automatic ownership field")
			}
		}
	}
}

func TestRemovedTypeUsesSavedEndpointScope(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
		outside  = fakeEdge{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "COMPUTER"}}
	)
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "COMPUTER", Kinds: []string{"Computer"}, Properties: map[string]any{}})
	database.graph.Edges = []fakeEdge{outside, {ID: 11, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}}
	plan.RetiredEndpoints = map[string][]extension.Endpoint{"PoC_ADCSESC1": plan.Schema.Relationships[0].Endpoints}
	plan.RetiredRelationships = []string{"PoC_ADCSESC1"}
	plan.Schema.Relationships = nil
	plan.Schema.Nodes = nil
	plan.Queries = nil
	report, err := runner.Run(t.Context(), database, plan, completeBatch())
	if err != nil || report.Removed != 1 || !reflect.DeepEqual(database.graph.Edges, []fakeEdge{outside}) {
		t.Fatalf("removed type lost its previous endpoint scope: %+v %v", report, err)
	}
	options := completeBatch()
	options.Changes = runner.Changes{CoverageKnown: true}
	report, err = runner.Run(t.Context(), database, plan, options)
	if err != nil || !report.Skipped {
		t.Fatalf("unchanged removal did not skip: %+v %v", report, err)
	}
	options.Changes.Nodes = []string{"UnrelatedKind"}
	report, err = runner.Run(t.Context(), database, plan, options)
	if err != nil || !report.Skipped {
		t.Fatalf("unrelated change repeated removal: %+v %v", report, err)
	}
	database.graph.Edges = append(database.graph.Edges, fakeEdge{ID: 11, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}})
	options.Changes.Relationships = []string{"PoC_ADCSESC1"}
	report, err = runner.Run(t.Context(), database, plan, options)
	if err != nil || !report.Committed || report.Removed != 1 || !reflect.DeepEqual(database.graph.Edges, []fakeEdge{outside}) {
		t.Fatalf("changed retired type was not cleared: %+v %v", report, err)
	}
}

func TestSameFilenameAnswersStaySeparateAndSharedProviderRunsOnce(t *testing.T) {
	var (
		database  = fixtureDatabase()
		plan      = fixturePlan()
		directory = extension.Query{Name: "PoC_AD/users.cypher", Parameter: "users", Text: "directory answer", QueryMetadata: extension.QueryMetadata{Namespace: "PoC_AD", Version: "v1.0.3"}, SchemaVersion: "v1.1.0"}
		vault     = extension.Query{Name: "PoC_Vault/users.cypher", Parameter: "users", Text: "vault answer"}
	)
	plan.Schema.Nodes = nil
	plan.Schema.Relationships[0].Query = "PoC_Vault/links.cypher"
	plan.Schema.Relationships = append(plan.Schema.Relationships, plan.Schema.Relationships[0])
	plan.Schema.Relationships[1].Query = "PoC_Vault/other.cypher"
	plan.Queries = []extension.Query{directory, vault,
		{Name: "PoC_Vault/links.cypher", Text: relationshipQuery, Dependencies: []extension.Dependency{{Type: "query", Name: directory.Name, Parameter: "directory_users"}, {Type: "query", Name: vault.Name, Parameter: "vault_users"}}},
		{Name: "PoC_Vault/other.cypher", Text: "other answer", Dependencies: []extension.Dependency{{Type: "query", Name: directory.Name, Parameter: "people"}}},
	}
	database.answers[directory.Text] = runner.Rows{{"user_id": "directory", "memberships": []any{}, "optional": nil}}
	database.answers[vault.Text] = runner.Rows{{"user_id": "vault"}}
	database.answers["other answer"] = runner.Rows{}
	report, err := runner.Run(t.Context(), database, plan, completeBatch())
	if err != nil || !report.Committed {
		t.Fatalf("run: %v", err)
	}
	reads := 0
	for _, read := range database.reads {
		if read.query == directory.Text {
			reads++
		}
		if read.query == relationshipQuery {
			if !reflect.DeepEqual(read.parameters["directory_users"], runner.Rows{map[string]any{"user_id": "directory", "memberships": []any{}, "optional": nil}}) || !reflect.DeepEqual(read.parameters["vault_users"], runner.Rows{map[string]any{"user_id": "vault"}}) {
				t.Fatalf("answers collided: %+v", read.parameters)
			}
		}
		if read.query == "other answer" && len(read.parameters) != 1 {
			t.Fatal("consumer received another consumer's aliases")
		}
	}
	if reads != 1 {
		t.Fatalf("shared calculation ran %d times", reads)
	}
	if report.Execution.Queries[0].Version != "v1.0.3" || report.Execution.Queries[0].SchemaVersion != "v1.1.0" {
		t.Fatal("exact versions were not reported")
	}
}

func TestChangedReturnedColumnsRollBackBeforePublication(t *testing.T) {
	var database = fixtureDatabase()
	plan := fixturePlan()
	plan.Queries[0].ColumnsKnown = true
	plan.Queries[0].Columns = []string{"different"}
	before := cloneGraph(*database.graph)
	assertRollback(t, database, before, plan, completeBatch(), "column")
}
