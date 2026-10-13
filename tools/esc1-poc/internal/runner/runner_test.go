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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

const (
	identityQuery           = "MATCH (item) WHERE NOT (labels(item) = ['MigrationData'] AND NOT (item)--()) WITH item.objectid AS objectid, count(item) AS matches WHERE objectid IS NULL OR trim(objectid) = '' OR matches <> 1 RETURN objectid LIMIT 1"
	helperQuery             = "RETURN 'helper fixture' AS fixture"
	relationshipQuery       = "RETURN 'relationship fixture' AS fixture"
	cycleQuery              = "MATCH path=(item)-[:MemberOf|MemberOfLocalGroup*1..]->(item) RETURN 1 AS cycle LIMIT 1"
	nativeQuery             = "MATCH (source)-[:ADCSESC1]->(target) RETURN DISTINCT source.objectid AS source, target.objectid AS target"
	endpointQuery           = "MATCH (item {objectid: $id}) RETURN labels(item) AS kinds"
	helperLookupQuery       = "MATCH (item) WHERE item.objectid STARTS WITH $prefix RETURN item.objectid AS id, labels(item) AS kinds, properties(item) AS properties"
	stateLookupQuery        = "MATCH (item {objectid: $id}) RETURN labels(item) AS kinds"
	previousRunQuery        = "MATCH (run:PoC_Run {objectid: $id}) RETURN run.plan_hash AS plan_hash"
	relationshipsQuery      = "MATCH (source)-[edge:PoC_ADCSESC1]->(target) RETURN id(edge) AS id, source.objectid AS source, target.objectid AS target"
	createRelationshipQuery = "MATCH (source {objectid: $source}), (target {objectid: $target}) CREATE (source)-[edge:`PoC_ADCSESC1`]->(target) SET edge = $properties RETURN id(edge) AS id"
	updateRelationshipQuery = "MATCH ()-[edge:`PoC_ADCSESC1`]->() WHERE id(edge) = $id SET edge += $properties RETURN id(edge) AS id"
	deleteRelationshipQuery = "MATCH ()-[edge:`PoC_ADCSESC1`]->() WHERE id(edge) = $id WITH edge, id(edge) AS old_id DELETE edge RETURN old_id"
	helperWriteQuery        = "MERGE (item:`PoC_Helper` {objectid: $id}) SET item = $properties"
	saveRunQuery            = "MERGE (run:PoC_Run {objectid: $id}) SET run.batch_id = $batch, run.plan_hash = $hash, run.completed_at = $completed, run.reference_commit = $reference"
)

type fakeNode struct {
	ID         string
	Kinds      []string
	Properties map[string]any
}

type fakeEdge struct {
	ID         int64
	Kind       string
	Pair       runner.Pair
	Properties map[string]any
}

type fakeGraph struct {
	Nodes  []fakeNode
	Edges  []fakeEdge
	NextID int64
}

// The persisted graph belongs to the database, not to one runner invocation.
// A second database client can use the same graph to model a process restart.
type recordedRead struct {
	query      string
	parameters map[string]any
}

type fakeDatabase struct {
	reads              []recordedRead
	graph              *fakeGraph
	answers            map[string]runner.Rows
	readErrors         map[string]error
	writeErrors        map[string]error
	begins             int
	commits            int
	rollbacks          int
	helperWrites       int
	relationshipWrites int
}

type fakeTransaction struct {
	database *fakeDatabase
	staged   fakeGraph
	finished bool
}

func cloneProperties(properties map[string]any) map[string]any {
	var copied = make(map[string]any, len(properties))
	for key, value := range properties {
		copied[key] = value
	}
	return copied
}

func cloneGraph(graph fakeGraph) fakeGraph {
	var copied = fakeGraph{NextID: graph.NextID}
	for _, node := range graph.Nodes {
		copied.Nodes = append(copied.Nodes, fakeNode{ID: node.ID, Kinds: append([]string{}, node.Kinds...), Properties: cloneProperties(node.Properties)})
	}
	for _, edge := range graph.Edges {
		copy := edge
		if edge.Properties != nil {
			copy.Properties = cloneProperties(edge.Properties)
		}
		copied.Edges = append(copied.Edges, copy)
	}
	return copied
}

func (s *fakeDatabase) Begin(context.Context) (runner.Transaction, error) {
	s.begins++
	return &fakeTransaction{database: s, staged: cloneGraph(*s.graph)}, nil
}

func (s *fakeTransaction) matchingNodes(id any) []fakeNode {
	var matching []fakeNode
	for _, node := range s.staged.Nodes {
		if node.ID == id {
			matching = append(matching, node)
		}
	}
	return matching
}

// Recognize the endpoint restriction used by the real publisher. This lets
// tests verify that another endpoint combination is left alone.
func splitRelationshipScope(query string) (string, string) {
	var start = strings.Index(query, " WHERE (source:")
	if start < 0 {
		return query, ""
	}
	var end = strings.Index(query[start:], " RETURN ") + start
	return query[:start] + query[end:], query[start+7 : end]
}

func (s *fakeTransaction) filterScope(rows runner.Rows, scope string) runner.Rows {
	if scope == "" {
		return rows
	}
	var filtered runner.Rows
	for _, row := range rows {
		var matches bool
		for _, pair := range strings.Split(scope, " OR ") {
			var kinds = strings.Split(strings.Trim(pair, "()"), " AND target:")
			var sourceKind = strings.TrimPrefix(kinds[0], "source:")
			for _, source := range s.matchingNodes(row["source"]) {
				for _, target := range s.matchingNodes(row["target"]) {
					for _, left := range source.Kinds {
						for _, right := range target.Kinds {
							matches = matches || left == sourceKind && right == kinds[1]
						}
					}
				}
			}
		}
		if matches {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (s *fakeTransaction) Read(_ context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	var rows runner.Rows
	var scope string
	query, scope = splitRelationshipScope(query)
	s.database.reads = append(s.database.reads, recordedRead{query: query, parameters: cloneProperties(parameters)})
	if err := s.database.readErrors[query]; err != nil {
		return nil, err
	}
	if answers, exists := s.database.answers[query]; exists {
		return answers, nil
	}
	switch query {
	case identityQuery:
		counts := make(map[string]int)
		for _, node := range s.staged.Nodes {
			counts[node.ID]++
		}
		for _, node := range s.staged.Nodes {
			if strings.TrimSpace(node.ID) == "" || counts[node.ID] != 1 {
				return runner.Rows{{"objectid": node.ID}}, nil
			}
		}
	case cycleQuery:
		return nil, nil
	case nativeQuery, relationshipsQuery, relationshipsQuery + ", properties(edge) AS properties":
		for _, edge := range s.staged.Edges {
			if query == nativeQuery && edge.Kind == "ADCSESC1" {
				rows = append(rows, map[string]any{"source": edge.Pair.Source, "target": edge.Pair.Target})
			}
			if strings.HasPrefix(query, relationshipsQuery) && edge.Kind == "PoC_ADCSESC1" {
				row := map[string]any{"id": edge.ID, "source": edge.Pair.Source, "target": edge.Pair.Target}
				if query != relationshipsQuery {
					row["properties"] = cloneProperties(edge.Properties)
				}
				rows = append(rows, row)
			}
		}
	case helperLookupQuery:
		for _, node := range s.staged.Nodes {
			if strings.HasPrefix(node.ID, parameters["prefix"].(string)) {
				rows = append(rows, map[string]any{"id": node.ID, "kinds": node.Kinds, "properties": cloneProperties(node.Properties)})
			}
		}
	case endpointQuery, previousRunQuery:
		for _, node := range s.matchingNodes(parameters["id"]) {
			switch query {
			case endpointQuery:
				rows = append(rows, map[string]any{"kinds": node.Kinds})
			case previousRunQuery:
				if reflect.DeepEqual(node.Kinds, []string{"PoC_Run"}) {
					rows = append(rows, map[string]any{"plan_hash": node.Properties["plan_hash"]})
				}
			}
		}
	default:
		return nil, fmt.Errorf("fake does not recognize read: %s", query)
	}
	return s.filterScope(rows, scope), nil
}

func (s *fakeTransaction) storeNode(node fakeNode) {
	for index, existing := range s.staged.Nodes {
		if existing.ID == node.ID {
			s.staged.Nodes[index] = node
			return
		}
	}
	s.staged.Nodes = append(s.staged.Nodes, node)
}

func (s *fakeTransaction) Write(_ context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if err := s.database.writeErrors[query]; err != nil {
		return nil, err
	}
	switch query {
	case helperWriteQuery:
		s.database.helperWrites++
		s.storeNode(fakeNode{ID: parameters["id"].(string), Kinds: []string{"PoC_Helper"}, Properties: cloneProperties(parameters["properties"].(map[string]any))})
	case saveRunQuery:
		s.storeNode(fakeNode{ID: parameters["id"].(string), Kinds: []string{"PoC_Run"}, Properties: map[string]any{"batch_id": parameters["batch"], "plan_hash": parameters["hash"], "completed_at": parameters["completed"], "reference_commit": parameters["reference"]}})
	case createRelationshipQuery:
		s.database.relationshipWrites++
		if len(s.matchingNodes(parameters["source"])) != 1 || len(s.matchingNodes(parameters["target"])) != 1 {
			return nil, nil
		}
		s.staged.NextID++
		s.staged.Edges = append(s.staged.Edges, fakeEdge{ID: s.staged.NextID, Kind: "PoC_ADCSESC1", Properties: cloneProperties(parameters["properties"].(map[string]any)), Pair: runner.Pair{Source: parameters["source"].(string), Target: parameters["target"].(string)}})
		return runner.Rows{{"id": s.staged.NextID}}, nil
	case updateRelationshipQuery:
		s.database.relationshipWrites++
		for index, edge := range s.staged.Edges {
			if edge.Kind == "PoC_ADCSESC1" && edge.ID == parameters["id"] {
				properties := cloneProperties(edge.Properties)
				for key, value := range parameters["properties"].(map[string]any) {
					if value == nil {
						delete(properties, key)
					} else {
						properties[key] = value
					}
				}
				s.staged.Edges[index].Properties = properties
				return runner.Rows{{"id": edge.ID}}, nil
			}
		}
		return nil, nil
	case deleteRelationshipQuery:
		s.database.relationshipWrites++
		for index, edge := range s.staged.Edges {
			if edge.Kind == "PoC_ADCSESC1" && edge.ID == parameters["id"] {
				s.staged.Edges = append(s.staged.Edges[:index], s.staged.Edges[index+1:]...)
				return runner.Rows{{"old_id": edge.ID}}, nil
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("fake does not recognize write: %s", query)
	}
	return nil, nil
}

func (s *fakeTransaction) Commit(context.Context) error {
	if s.finished {
		return fmt.Errorf("transaction already finished")
	}
	*s.database.graph = cloneGraph(s.staged)
	s.database.commits++
	s.finished = true
	return nil
}

func (s *fakeTransaction) Rollback(context.Context) error {
	if !s.finished {
		s.database.rollbacks++
		s.finished = true
	}
	return nil
}

func fixturePlan() extension.Plan {
	return extension.Plan{
		Schema: extension.Schema{
			Name: "runner-test", ReferenceCommit: strings.Repeat("a", 40),
			Nodes:         []extension.NodeBinding{{Kind: "PoC_Helper", Query: "helper.cypher", IDPrefix: "PoC_Helper_"}},
			Relationships: []extension.RelationshipBinding{{Kind: "PoC_ADCSESC1", Query: "esc1.cypher", ExistingEdgePolicy: extension.KeepExisting, Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}}},
		},
		Queries: []extension.Query{
			{Name: "helper.cypher", Parameter: "helper", Text: helperQuery, Dependencies: []extension.Dependency{{Type: "node", Name: "User"}}},
			{Name: "esc1.cypher", Parameter: "esc1", Text: relationshipQuery, Dependencies: []extension.Dependency{{Type: "query", Name: "helper.cypher"}}},
		},
	}
}

func fixtureDatabase() *fakeDatabase {
	return &fakeDatabase{
		graph: &fakeGraph{NextID: 100, Nodes: []fakeNode{
			{ID: "USER", Kinds: []string{"User"}, Properties: map[string]any{}},
			{ID: "DOMAIN", Kinds: []string{"Domain"}, Properties: map[string]any{}},
		}},
		answers: map[string]runner.Rows{
			helperQuery:       {{"id": "PoC_Helper_example", "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"name": "example"}}},
			relationshipQuery: {{"source": "USER", "target": "DOMAIN"}},
		},
		readErrors: map[string]error{}, writeErrors: map[string]error{},
	}
}

func completeBatch() runner.Options {
	return runner.Options{BatchID: "batch-1", IngestionComplete: true, WritersPaused: true}
}

func runSuccessfully(t *testing.T, database *fakeDatabase) runner.Report {
	t.Helper()
	report, err := runner.Run(context.Background(), database, fixturePlan(), completeBatch())
	if err != nil || !report.Committed {
		t.Fatalf("run did not commit: report=%+v error=%v", report, err)
	}
	return report
}

func assertRollback(t *testing.T, database *fakeDatabase, before fakeGraph, plan extension.Plan, options runner.Options, message string) runner.Report {
	t.Helper()
	report, err := runner.Run(context.Background(), database, plan, options)
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("error %v; expected %q", err, message)
	}
	if report.Committed || database.commits != 0 || database.rollbacks != 1 {
		t.Fatalf("failed run was not rolled back: report=%+v commits=%d rollbacks=%d", report, database.commits, database.rollbacks)
	}
	if !reflect.DeepEqual(*database.graph, before) {
		t.Fatalf("failed run changed persisted graph: before=%+v after=%+v", before, *database.graph)
	}
	return report
}

func TestRunPreservesIdentityUntilFinalSupportIsRemoved(t *testing.T) {
	var (
		database   = fixtureDatabase()
		nativeEdge = fakeEdge{ID: 9, Kind: "ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}
	)
	database.graph.Edges = append(database.graph.Edges, nativeEdge)
	database.answers[relationshipQuery] = runner.Rows{{"source": "USER", "target": "DOMAIN"}, {"source": "USER", "target": "DOMAIN"}}
	first := runSuccessfully(t, database)
	if first.Added != 1 || first.HelpersChanged != 1 {
		t.Fatalf("first publication: %+v", first)
	}
	original := database.graph.Edges[1]
	writes := database.relationshipWrites
	second := runSuccessfully(t, database)
	if second.Added != 0 || second.Removed != 0 || second.Unchanged != 1 || second.HelpersChanged != 0 || database.relationshipWrites != writes {
		t.Fatalf("unchanged rerun wrote relationships: %+v", second)
	}
	if !reflect.DeepEqual(database.graph.Edges[1], original) {
		t.Fatal("rerun changed relationship identity or properties")
	}
	// Two independent evidence rows can support one connection. Losing one
	// supporting row does not authorize deleting that connection.
	database.answers[relationshipQuery] = runner.Rows{{"source": "USER", "target": "DOMAIN"}}
	remaining := runSuccessfully(t, database)
	if remaining.Unchanged != 1 || database.relationshipWrites != writes || !reflect.DeepEqual(database.graph.Edges[1], original) {
		t.Fatal("removing one support changed the connection")
	}
	database.answers[relationshipQuery] = nil
	removed := runSuccessfully(t, database)
	if removed.Removed != 1 || !reflect.DeepEqual(database.graph.Edges, []fakeEdge{nativeEdge}) {
		t.Fatalf("final support removal changed another relationship: %+v edges=%+v", removed, database.graph.Edges)
	}
}

func TestRunRollsBackAfterStagingHelpersAndRelationships(t *testing.T) {
	var cases = []struct {
		name               string
		afterRelationships bool
		message            string
	}{
		{"calculation failure after helper staging", false, "calculation failed"},
		{"run record failure after relationship staging", true, "run record failed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var database = fixtureDatabase()
			// Start with a previous publication. A failed replacement must
			// preserve its helper properties and its relationship identity.
			database.graph.Nodes = append(database.graph.Nodes,
				fakeNode{ID: "OLD_DOMAIN", Kinds: []string{"Domain"}, Properties: map[string]any{}},
				fakeNode{ID: "PoC_Helper_example", Kinds: []string{"PoC_Helper"}, Properties: map[string]any{"objectid": "PoC_Helper_example", "poc_owner": "test-owner/PoC_Helper", "name": "previous value"}},
			)
			database.graph.Edges = []fakeEdge{{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "OLD_DOMAIN"}}}
			var before = cloneGraph(*database.graph)
			if testCase.afterRelationships {
				database.writeErrors[saveRunQuery] = fmt.Errorf("run record failed")
			} else {
				database.readErrors[relationshipQuery] = fmt.Errorf("calculation failed")
			}
			assertRollback(t, database, before, fixturePlan(), completeBatch(), testCase.message)
			if database.helperWrites != 1 {
				t.Fatal("failure did not exercise staged helper changes")
			}
			if testCase.afterRelationships && database.relationshipWrites != 2 {
				t.Fatal("failure did not exercise staged relationship changes")
			}
		})
	}
}

func TestRunRejectsMissingWrongAndDuplicateEndpoints(t *testing.T) {
	var cases = []struct {
		name      string
		kinds     []string
		duplicate bool
		missing   bool
		message   string
	}{
		{"missing target", nil, false, true, "exactly one"},
		{"wrong target kind", []string{"Computer"}, false, false, "incompatible"},
		{"duplicate objectid", []string{"Domain"}, true, false, "duplicate objectids"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var database = fixtureDatabase()
			if testCase.missing {
				database.graph.Nodes = database.graph.Nodes[:1]
			} else {
				database.graph.Nodes[1].Kinds = testCase.kinds
				if testCase.duplicate {
					database.graph.Nodes = append(database.graph.Nodes, database.graph.Nodes[1])
				}
			}
			assertRollback(t, database, cloneGraph(*database.graph), fixturePlan(), completeBatch(), testCase.message)
			expectedHelperWrites := 1
			if testCase.duplicate {
				expectedHelperWrites = 0
			}
			if database.helperWrites != expectedHelperWrites || database.relationshipWrites != 0 {
				t.Fatal("endpoint failure must not publish graph changes")
			}
		})
	}
}

// Each allowed entry describes one complete pair. Allowing User -> Domain
// and Group -> Computer must not accidentally allow User -> Computer.
func TestRunChecksCompleteEndpointPairs(t *testing.T) {
	var cases = []struct {
		name        string
		sourceKinds []string
		targetKinds []string
		allowed     bool
	}{
		{"disallowed combination of otherwise valid kinds", []string{"User"}, []string{"Computer"}, false},
		{"first allowed pair", []string{"User"}, []string{"Domain"}, true},
		{"second allowed pair", []string{"Group"}, []string{"Computer"}, true},
		{"extra labels still contain an allowed pair", []string{"Base", "User", "Group"}, []string{"Base", "Computer"}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				database = fixtureDatabase()
				plan     = fixturePlan()
			)
			plan.Schema.Relationships[0].Endpoints = []extension.Endpoint{
				{SourceKind: "User", TargetKind: "Domain"},
				{SourceKind: "Group", TargetKind: "Computer"},
			}
			database.graph.Nodes[0].Kinds = testCase.sourceKinds
			database.graph.Nodes[1].Kinds = testCase.targetKinds
			// An invalid replacement must preserve an existing owned edge,
			// including its identity, as well as the staged helper changes.
			database.graph.Edges = []fakeEdge{{ID: 7, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}}
			if !testCase.allowed {
				assertRollback(t, database, cloneGraph(*database.graph), plan, completeBatch(), "incompatible")
				if database.relationshipWrites != 0 {
					t.Fatal("invalid endpoint combination changed a relationship")
				}
				return
			}
			if _, err := runner.Run(context.Background(), database, plan, completeBatch()); err != nil {
				t.Fatal(err)
			}
			if database.commits != 1 || len(database.graph.Edges) != 1 || database.graph.Edges[0].ID != 7 {
				t.Fatal("valid endpoint combination did not retain the existing relationship")
			}
		})
	}
}

func TestRunAcceptsExistingUnmarkedRelationships(t *testing.T) {
	var database = fixtureDatabase()
	database.graph.Edges = []fakeEdge{{ID: 7, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}}
	report := runSuccessfully(t, database)
	if report.Unchanged != 1 || database.graph.Edges[0].ID != 7 {
		t.Fatal("unmarked edge was not retained", report)
	}
}

func TestRunRejectsEqualCountsWithDifferentNativePairs(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
	)
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "OTHER", Kinds: []string{"Domain"}, Properties: map[string]any{}})
	database.graph.Edges = []fakeEdge{{ID: 8, Kind: "ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "OTHER"}}}
	plan := fixturePlan()
	plan.Schema.CompareNativeESC1 = true
	report := assertRollback(t, database, cloneGraph(*database.graph), plan, options, "1 missing and 1 extra")
	expected := runner.Comparison{MissingCount: 1, ExtraCount: 1, Missing: []runner.Pair{{Source: "USER", Target: "OTHER"}}, Extra: []runner.Pair{{Source: "USER", Target: "DOMAIN"}}}
	if report.NativeComparison == nil || !reflect.DeepEqual(*report.NativeComparison, expected) {
		t.Fatalf("comparison %+v; expected %+v", report.NativeComparison, expected)
	}
	if database.helperWrites != 1 || database.relationshipWrites != 0 {
		t.Fatal("native mismatch should discard helpers before publishing edges")
	}
}

func TestRunRetainsRelationshipsAcrossClientRestart(t *testing.T) {
	var database = fixtureDatabase()
	runSuccessfully(t, database)
	original := database.graph.Edges[0]
	restarted := fixtureDatabase()
	restarted.graph = database.graph
	report := runSuccessfully(t, restarted)
	if report.Unchanged != 1 || restarted.relationshipWrites != 0 || !reflect.DeepEqual(restarted.graph.Edges[0], original) {
		t.Fatalf("restart changed relationships: report=%+v edge=%+v", report, restarted.graph.Edges[0])
	}
}

func TestRunRequiresCompletedIngestionAndPausedWritersBeforeBeginning(t *testing.T) {
	var cases = []struct {
		name   string
		change func(*runner.Options)
	}{
		{"missing batch", func(options *runner.Options) { options.BatchID = "" }},
		{"incomplete ingestion", func(options *runner.Options) { options.IngestionComplete = false }},
		{"active writers", func(options *runner.Options) { options.WritersPaused = false }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				database = fixtureDatabase()
				options  = completeBatch()
			)
			testCase.change(&options)
			report, err := runner.Run(context.Background(), database, fixturePlan(), options)
			if err == nil || report.Committed || database.begins != 0 {
				t.Fatalf("unsafe batch began a transaction: report=%+v error=%v begins=%d", report, err, database.begins)
			}
		})
	}
}

func TestCompareUsesDistinctSortedPairs(t *testing.T) {
	var (
		expected   = []runner.Pair{{Source: "Z", Target: "D"}, {Source: "A", Target: "D"}, {Source: "A", Target: "D"}}
		actual     = []runner.Pair{{Source: "C", Target: "D"}, {Source: "B", Target: "D"}, {Source: "B", Target: "D"}}
		comparison = runner.Compare(expected, actual)
		want       = runner.Comparison{MissingCount: 2, ExtraCount: 2, Missing: []runner.Pair{{Source: "A", Target: "D"}, {Source: "Z", Target: "D"}}, Extra: []runner.Pair{{Source: "B", Target: "D"}, {Source: "C", Target: "D"}}}
	)
	if !reflect.DeepEqual(comparison, want) {
		t.Fatalf("comparison %+v; expected %+v", comparison, want)
	}
}

func TestAnswerObserverSeesCompleteRowsAndCannotPublishOnFailure(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
		observed = map[string]int{}
	)
	options.ObserveAnswer = func(name string, row map[string]any) error {
		observed[name]++
		return nil
	}
	report, err := runner.Run(context.Background(), database, fixturePlan(), options)
	if err != nil || !report.Committed || !reflect.DeepEqual(observed, report.QueryRows) {
		t.Fatalf("observed rows %v differ from complete answers %v: %v", observed, report.QueryRows, err)
	}
	database = fixtureDatabase()
	before := *database.graph
	options.ObserveAnswer = func(string, map[string]any) error { return errors.New("cannot retain evidence") }
	assertRollback(t, database, before, fixturePlan(), options, "cannot retain evidence")
}
