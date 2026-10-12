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
package runner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

const (
	independentHelperQuery = "RETURN 'independent helper fixture' AS fixture"
	independentEdgeQuery   = "RETURN 'independent relationship fixture' AS fixture"
)

// This publisher keeps arbitrary kinds in the same private transaction graph.
// It allows skipped outputs to retain their IDs, ownership, and properties.
type selectionDatabase struct {
	*fakeDatabase
	failRunRecord bool
}

type selectionTransaction struct {
	*fakeTransaction
	owner *selectionDatabase
}

var _ runner.Publisher = (*selectionTransaction)(nil)

func (s *selectionDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &selectionTransaction{fakeTransaction: transaction.(*fakeTransaction), owner: s}, nil
}

func (s *selectionTransaction) Read(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	var rows runner.Rows
	query, scope := splitRelationshipScope(query)
	if strings.HasPrefix(query, "MATCH (source)-[edge:PoC_") {
		s.database.reads = append(s.database.reads, recordedRead{query: query, parameters: cloneProperties(parameters)})
		for _, edge := range s.staged.Edges {
			if strings.HasPrefix(query, "MATCH (source)-[edge:"+edge.Kind+"]->(target) RETURN id(edge)") {
				rows = append(rows, map[string]any{"id": edge.ID, "source": edge.Pair.Source, "target": edge.Pair.Target, "properties": cloneProperties(edge.Properties)})
			}
		}
		return s.filterScope(rows, scope), nil
	}
	return s.fakeTransaction.Read(ctx, query, parameters)
}

func (s *selectionTransaction) PutNode(_ context.Context, kind string, properties map[string]any) error {
	if kind == "PoC_Run" && s.owner.failRunRecord {
		return fmt.Errorf("injected late run record failure")
	}
	if kind != "PoC_Run" {
		s.database.helperWrites++
	}
	s.storeNode(fakeNode{ID: properties["objectid"].(string), Kinds: []string{kind}, Properties: cloneProperties(properties)})
	return nil
}

func (s *selectionTransaction) CreateRelationships(_ context.Context, kind string, relationships []runner.Relationship) (int, error) {
	var count int
	for _, relationship := range relationships {
		if len(s.matchingNodes(relationship.Source)) != 1 || len(s.matchingNodes(relationship.Target)) != 1 {
			continue
		}
		s.staged.NextID++
		s.database.relationshipWrites++
		s.staged.Edges = append(s.staged.Edges, fakeEdge{ID: s.staged.NextID, Kind: kind, Pair: runner.Pair{Source: relationship.Source, Target: relationship.Target}, Properties: cloneProperties(relationship.Properties)})
		count++
	}
	return count, nil
}

func (s *selectionTransaction) UpdateRelationships(_ context.Context, kind string, updates []runner.RelationshipUpdate) (int, error) {
	var count int
	for _, update := range updates {
		for index, edge := range s.staged.Edges {
			if edge.Kind == kind && edge.ID == update.ID {
				properties := cloneProperties(edge.Properties)
				for key, value := range update.Properties {
					if value == nil {
						delete(properties, key)
					} else {
						properties[key] = value
					}
				}
				s.staged.Edges[index].Properties = properties
				s.database.relationshipWrites++
				count++
			}
		}
	}
	return count, nil
}

func (s *selectionTransaction) DeleteRelationships(_ context.Context, kind string, identifiers []int64) (int, error) {
	var count int
	for _, identifier := range identifiers {
		for index, edge := range s.staged.Edges {
			if edge.Kind == kind && edge.ID == identifier {
				s.staged.Edges = append(s.staged.Edges[:index], s.staged.Edges[index+1:]...)
				s.database.relationshipWrites++
				count++
				break
			}
		}
	}
	return count, nil
}

func independentSelectionFixture() (*selectionDatabase, extension.Plan) {
	var (
		database = &selectionDatabase{fakeDatabase: fixtureDatabase()}
		plan     = fixturePlan()
	)
	// The omitted policy is replace: selecting another output must not recreate it.
	plan.Schema.Relationships[0].ExistingEdgePolicy = ""
	plan.Queries[0].Dependencies = []extension.Dependency{{Type: "node", Name: "InputA"}}
	plan.Queries[1].Dependencies = append(plan.Queries[1].Dependencies, extension.Dependency{Type: "relationship", Name: "Enroll"})
	plan.Schema.Nodes = append(plan.Schema.Nodes, extension.NodeBinding{Kind: "PoC_IndependentHelper", Query: "independent_helper.cypher", IDPrefix: "PoC_IndependentHelper_"})
	plan.Schema.Relationships = append(plan.Schema.Relationships, extension.RelationshipBinding{Kind: "PoC_Independent", Query: "independent.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}})
	plan.Queries = append(plan.Queries,
		extension.Query{Name: "independent_helper.cypher", Parameter: "independent_helper", Text: independentHelperQuery, Dependencies: []extension.Dependency{{Type: "node", Name: "InputB"}}},
		extension.Query{Name: "independent.cypher", Parameter: "independent", Text: independentEdgeQuery, Dependencies: []extension.Dependency{{Type: "query", Name: "independent_helper.cypher"}, {Type: "relationship", Name: "Owns"}}},
	)
	database.answers[independentHelperQuery] = runner.Rows{{"id": "PoC_IndependentHelper_example", "kinds": []string{"PoC_IndependentHelper"}, "properties": map[string]any{"name": "original independent helper"}}}
	database.answers[independentEdgeQuery] = runner.Rows{{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"evidence": "original"}}}
	return database, plan
}

func runSelection(t *testing.T, database *selectionDatabase, plan extension.Plan, changes runner.Changes) runner.Report {
	t.Helper()
	var options = completeBatch()
	options.Changes = changes
	options.TemporaryDirectory = t.TempDir()
	report, err := runner.Run(t.Context(), database, plan, options)
	if err != nil || !report.Committed {
		t.Fatalf("run failed: report=%+v error=%v", report, err)
	}
	return report
}

func selectionOutputBytes(t *testing.T, graph fakeGraph, kind string) string {
	t.Helper()
	var output struct {
		Nodes []fakeNode
		Edges []fakeEdge
	}
	for _, node := range graph.Nodes {
		if reflect.DeepEqual(node.Kinds, []string{kind}) {
			output.Nodes = append(output.Nodes, node)
		}
	}
	for _, edge := range graph.Edges {
		if edge.Kind == kind {
			output.Edges = append(output.Edges, edge)
		}
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestSelectiveReplacementPreservesSkippedOutputsAndFullPlanHash(t *testing.T) {
	var (
		database, plan = independentSelectionFixture()
		known          = runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}}
	)
	first := runSelection(t, database, plan, known)
	if len(first.Queries) != len(plan.Queries) {
		t.Fatal("first run did not establish a complete baseline")
	}
	var (
		skippedEdges   = selectionOutputBytes(t, *database.graph, "PoC_Independent")
		skippedHelpers = selectionOutputBytes(t, *database.graph, "PoC_IndependentHelper")
	)
	// Deliberately change skipped answers; selecting ESC1 must not publish them.
	database.answers[independentHelperQuery][0]["properties"] = map[string]any{"name": "changed independent helper"}
	database.answers[independentEdgeQuery] = nil
	second := runSelection(t, database, plan, known)
	if !reflect.DeepEqual(second.Queries, []string{"helper.cypher", "esc1.cypher"}) || second.Added != 1 || second.Removed != 1 {
		t.Fatalf("expected replacement of selected ESC1 only: %+v", second)
	}
	if selectionOutputBytes(t, *database.graph, "PoC_Independent") != skippedEdges || selectionOutputBytes(t, *database.graph, "PoC_IndependentHelper") != skippedHelpers {
		t.Fatal("skipped output changed identity, ownership, or properties")
	}
	var keptESC1 = selectionOutputBytes(t, *database.graph, "PoC_ADCSESC1")
	// A different selection must still recognize the original complete plan.
	third := runSelection(t, database, plan, runner.Changes{CoverageKnown: true, Relationships: []string{"Owns"}})
	if !reflect.DeepEqual(third.Queries, []string{"independent_helper.cypher", "independent.cypher"}) || third.Removed != 1 || third.HelpersChanged != 1 {
		t.Fatalf("subsequent selection lost the full-plan baseline: %+v", third)
	}
	if selectionOutputBytes(t, *database.graph, "PoC_ADCSESC1") != keptESC1 {
		t.Fatal("second selection changed skipped ESC1")
	}
}

func TestSelectiveEmptyResultRemovesOnlySelectedOwnedEdges(t *testing.T) {
	var database, plan = independentSelectionFixture()
	runSelection(t, database, plan, runner.Changes{})
	var foreign = fakeEdge{ID: 17, Kind: "PoC_Independent", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}, Properties: map[string]any{"note": "foreign"}}
	database.graph.Edges = append(database.graph.Edges, foreign)
	var skipped = selectionOutputBytes(t, *database.graph, "PoC_Independent")
	database.answers[relationshipQuery] = nil
	report := runSelection(t, database, plan, runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}})
	if report.Removed != 1 || report.Added != 0 || report.DesiredCounts["PoC_ADCSESC1"] != 0 || selectionOutputBytes(t, *database.graph, "PoC_Independent") != skipped {
		t.Fatalf("empty selected result changed unrelated ownership: %+v", report)
	}
	for _, edge := range database.graph.Edges {
		if edge.Kind == "PoC_ADCSESC1" {
			t.Fatal("selected owned edge survived complete empty result")
		}
	}
}

func TestSelectiveLateFailureRollsBackHelpersEdgesAndRunRecord(t *testing.T) {
	var database, plan = independentSelectionFixture()
	runSelection(t, database, plan, runner.Changes{})
	database.graph.Edges = append(database.graph.Edges, fakeEdge{ID: 18, Kind: "PoC_Independent", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}, Properties: map[string]any{"note": "foreign"}})
	var (
		before       = cloneGraph(*database.graph)
		helperWrites = database.helperWrites
		edgeWrites   = database.relationshipWrites
		options      = completeBatch()
	)
	database.answers[helperQuery][0]["properties"] = map[string]any{"name": "staged change"}
	database.failRunRecord = true
	options.Changes = runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}}
	report, err := runner.Run(t.Context(), database, plan, options)
	if err == nil || !strings.Contains(err.Error(), "late run record failure") || report.Committed || !reflect.DeepEqual(before, *database.graph) {
		t.Fatalf("late selected failure did not restore all graph objects: %+v error=%v", report, err)
	}
	if database.helperWrites != helperWrites+1 || database.relationshipWrites != edgeWrites+2 || database.rollbacks != 1 {
		t.Fatal("failure did not exercise staged helper and replacement edge writes")
	}
}

func TestSelectionFallsBackToFullPlanForUnknownCoverageOrEditedPlan(t *testing.T) {
	for _, scenario := range []string{"unknown coverage", "edited skipped query", "edited skipped binding"} {
		t.Run(scenario, func(t *testing.T) {
			var (
				database, plan = independentSelectionFixture()
				changes        = runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}}
			)
			runSelection(t, database, plan, changes)
			switch scenario {
			case "unknown coverage":
				changes.CoverageKnown = false
			case "edited skipped query":
				plan.Queries[2].Text += " // edited"
				database.answers[plan.Queries[2].Text] = database.answers[independentHelperQuery]
			case "edited skipped binding":
				plan.Schema.Relationships[1].ExistingEdgePolicy = extension.KeepExisting
			}
			report := runSelection(t, database, plan, changes)
			if len(report.Queries) != len(plan.Queries) {
				t.Fatalf("unsafe partial run after %s: %+v", scenario, report)
			}
		})
	}
}

func TestSelectiveRunComparesNativeOnlyWhenESC1IsSelected(t *testing.T) {
	var database, plan = independentSelectionFixture()
	plan.Schema.CompareNativeESC1 = true
	database.graph.Edges = append(database.graph.Edges, fakeEdge{ID: 19, Kind: "ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}})
	runSelection(t, database, plan, runner.Changes{})
	database.readErrors[nativeQuery] = fmt.Errorf("native comparison should be skipped")
	report := runSelection(t, database, plan, runner.Changes{CoverageKnown: true, Relationships: []string{"Owns"}})
	if report.NativeComparison != nil {
		t.Fatal("unselected ESC1 was compared to an empty desired answer")
	}
	delete(database.readErrors, nativeQuery)
	report = runSelection(t, database, plan, runner.Changes{CoverageKnown: true, Relationships: []string{"ADCSESC1"}})
	if report.NativeComparison == nil || !reflect.DeepEqual(report.Queries, []string{"helper.cypher", "esc1.cypher"}) {
		t.Fatalf("native ESC1 change did not select its producer and comparison: %+v", report)
	}
}

func TestSelectiveRunExecutesDownstreamAndSharedPrerequisitesOnce(t *testing.T) {
	var (
		database, plan = independentSelectionFixture()
		shared         = extension.Query{Name: "shared.cypher", Parameter: "shared", Text: "RETURN 'shared selection fixture' AS fixture"}
		consumer       = extension.Query{Name: "consumer.cypher", Parameter: "consumer", Text: "RETURN 'consumer selection fixture' AS fixture", Dependencies: []extension.Dependency{{Type: "query", Name: "esc1.cypher"}, {Type: "query", Name: "shared.cypher"}}}
	)
	plan.Queries[1].Dependencies = append(plan.Queries[1].Dependencies, extension.Dependency{Type: "query", Name: shared.Name})
	plan.Queries = append([]extension.Query{shared}, plan.Queries...)
	plan.Queries = append(plan.Queries, consumer)
	plan.Schema.Relationships = append(plan.Schema.Relationships, extension.RelationshipBinding{Kind: "PoC_Consumer", Query: consumer.Name, Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}})
	database.answers[shared.Text] = runner.Rows{{"value": "shared"}}
	database.answers[consumer.Text] = runner.Rows{{"source": "USER", "target": "DOMAIN"}}
	runSelection(t, database, plan, runner.Changes{})
	database.reads = nil
	report := runSelection(t, database, plan, runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}})
	var counts = map[string]int{}
	for _, read := range database.reads {
		counts[read.query]++
	}
	for _, query := range []extension.Query{shared, plan.Queries[1], plan.Queries[2], consumer} {
		if counts[query.Text] != 1 {
			t.Fatalf("selected prerequisite or consumer %s ran %d times", query.Name, counts[query.Text])
		}
	}
	if len(report.Queries) != 4 || counts[independentEdgeQuery] != 0 || counts[independentHelperQuery] != 0 {
		t.Fatalf("dependency closure included independent queries: %+v", report)
	}
}
