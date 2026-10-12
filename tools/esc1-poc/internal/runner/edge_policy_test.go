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
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func edgePolicyPlan(policy extension.ExistingEdgePolicy) extension.Plan {
	var plan = fixturePlan()
	plan.Schema.Nodes = nil
	plan.Schema.Relationships[0].ExistingEdgePolicy = policy
	plan.Queries = plan.Queries[1:]
	plan.Queries[0].Dependencies = []extension.Dependency{{Type: "node", Name: "User"}}
	return plan
}

func edgePolicyDatabase() *fakeDatabase {
	var database = fixtureDatabase()
	database.graph.Edges = []fakeEdge{{ID: 10, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}, Properties: map[string]any{"name": "old", "keep": "unspecified", "remove": true}}}
	return database
}

func runEdgePolicy(t *testing.T, database *fakeDatabase, policy extension.ExistingEdgePolicy) runner.Report {
	t.Helper()
	var report, err = runner.Run(context.Background(), database, edgePolicyPlan(policy), completeBatch())
	if err != nil || !report.Committed {
		t.Fatalf("policy %q did not commit: report=%+v error=%v", policy, report, err)
	}
	return report
}

func TestEdgePolicyDefaultReplacesIdentityAndProperties(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting} {
		t.Run(string(policy), func(t *testing.T) {
			var database = edgePolicyDatabase()
			database.answers[relationshipQuery] = runner.Rows{{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "new", "remove": nil}}}
			var report = runEdgePolicy(t, database, policy)
			if report.Added != 1 || report.Removed != 1 || report.Updated != 0 || report.Unchanged != 0 || len(database.graph.Edges) != 1 {
				t.Fatalf("replacement counts: %+v edges=%+v", report, database.graph.Edges)
			}
			var edge = database.graph.Edges[0]
			if edge.ID == 10 || !reflect.DeepEqual(edge.Properties, map[string]any{"name": "new"}) {
				t.Fatalf("replacement retained old identity or properties: %+v", edge)
			}
			// Replacement remains intentional even when the newly supplied map is equal.
			var previousID = edge.ID
			report = runEdgePolicy(t, database, policy)
			if database.graph.Edges[0].ID == previousID || report.Added != 1 || report.Removed != 1 {
				t.Fatalf("equal replacement unexpectedly retained identity: %+v", report)
			}
		})
	}
}

func TestEdgePolicyMergePreservesExactValuesAndSkipsEqualPatches(t *testing.T) {
	var (
		database     = edgePolicyDatabase()
		exactInteger = int64(9_007_199_254_740_993)
	)
	database.answers[relationshipQuery] = runner.Rows{{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "new", "remove": nil, "large": exactInteger, "tags": []string{"one", "two"}}}}
	var report = runEdgePolicy(t, database, extension.MergeProperties)
	var expected = map[string]any{"name": "new", "keep": "unspecified", "large": exactInteger, "tags": []any{"one", "two"}}
	if database.graph.Edges[0].ID != 10 || !reflect.DeepEqual(database.graph.Edges[0].Properties, expected) || report.Updated != 1 || report.Added != 0 || report.Removed != 0 {
		t.Fatalf("merge changed identity, lost properties, or rounded integer: %+v edges=%+v", report, database.graph.Edges)
	}
	var writes = database.relationshipWrites
	report = runEdgePolicy(t, database, extension.MergeProperties)
	if report.Updated != 0 || report.Unchanged != 1 || database.relationshipWrites != writes || !reflect.DeepEqual(database.graph.Edges[0].Properties, expected) {
		t.Fatalf("equal patch wrote or changed a connection: %+v", report)
	}
	// Driver integer types and map/list order must not create a spurious update.
	database.graph.Edges[0].Properties["count"] = int(2)
	database.answers[relationshipQuery][0]["properties"].(map[string]any)["count"] = float64(2)
	report = runEdgePolicy(t, database, extension.MergeProperties)
	if report.Updated != 0 || report.Unchanged != 1 || database.relationshipWrites != writes {
		t.Fatalf("equivalent integer representation caused an update: %+v", report)
	}
}

func TestEdgePolicyKeepIgnoresExistingPatchButCreatesWithProperties(t *testing.T) {
	var database = edgePolicyDatabase()
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "SECOND", Kinds: []string{"Domain"}, Properties: map[string]any{}})
	var original = database.graph.Edges[0]
	database.answers[relationshipQuery] = runner.Rows{
		{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "ignored", "remove": nil}},
		{"source": "USER", "target": "SECOND", "properties": map[string]any{"name": "created", "count": int64(9_007_199_254_740_993), "absent": nil}},
	}
	var report = runEdgePolicy(t, database, extension.KeepExisting)
	if report.Added != 1 || report.Unchanged != 1 || report.Updated != 0 || report.Removed != 0 || len(database.graph.Edges) != 2 || !reflect.DeepEqual(database.graph.Edges[0], original) {
		t.Fatalf("keep policy changed an existing edge: %+v edges=%+v", report, database.graph.Edges)
	}
	if !reflect.DeepEqual(database.graph.Edges[1].Properties, map[string]any{"name": "created", "count": int64(9_007_199_254_740_993)}) {
		t.Fatalf("new edge lost supplied properties: %+v", database.graph.Edges[1])
	}
}

func TestEdgePolicyAllModesRemoveStaleOwnedConnections(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		t.Run(string(policy), func(t *testing.T) {
			var database = edgePolicyDatabase()
			var native = fakeEdge{ID: 9, Kind: "ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}, Properties: map[string]any{"native": true}}
			database.graph.Edges = append(database.graph.Edges, native)
			database.answers[relationshipQuery] = nil
			var report = runEdgePolicy(t, database, policy)
			if report.Removed != 1 || report.Added != 0 || report.Updated != 0 || !reflect.DeepEqual(database.graph.Edges, []fakeEdge{native}) {
				t.Fatalf("stale reconciliation affected native evidence: %+v edges=%+v", report, database.graph.Edges)
			}
		})
	}
}

func TestEdgePoliciesAcceptExistingEdgesRegardlessOfLegacyProperties(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		for _, marker := range []string{"another-extension", ""} {
			t.Run(string(policy)+"/"+marker, func(t *testing.T) {
				var database = edgePolicyDatabase()
				if marker != "" {
					database.graph.Edges[0].Properties["poc_owner"] = marker
				}
				runEdgePolicy(t, database, policy)
			})
		}
	}
}

func TestEdgePolicyInvalidPropertiesRollbackInEveryMode(t *testing.T) {
	var cases = []struct {
		name    string
		row     map[string]any
		message string
	}{
		{"null map", map[string]any{"source": "USER", "target": "DOMAIN", "properties": nil}, "must be a map"},
		{"typed nil map", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any(nil)}, "a map value must not be nil"},
		{"non map", map[string]any{"source": "USER", "target": "DOMAIN", "properties": "invalid"}, "must be a map"},
		{"extra column", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{}, "extra": true}, "each relationship row"},
		{"wrong column", map[string]any{"source": "USER", "target": "DOMAIN", "unknown": true}, "each relationship row"},
		{"missing target", map[string]any{"source": "USER", "properties": map[string]any{}}, "target"},
		{"nested map", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": map[string]any{"nested": true}}}, "a property must be"},
		{"nested list", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": []any{[]any{"nested"}}}}, "simple values"},
		{"mixed list", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": []any{"text", true}}}, "same type"},
		{"null list item", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": []any{nil}}}, "simple values"},
		{"mixed whole and fractional list", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": []any{float64(2), float64(2.5)}}}, "same type"},
		{"whole number too large", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": -float64(math.MinInt64)}}, "signed 64-bit"},
		{"whole number too negative", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": math.Nextafter(float64(math.MinInt64), math.Inf(-1))}}, "signed 64-bit"},
		{"NaN", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": math.NaN()}}, "unsupported answer value"},
		{"positive infinity", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": math.Inf(1)}}, "unsupported answer value"},
		{"negative infinity", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"bad": math.Inf(-1)}}, "unsupported answer value"},
		{"empty name", map[string]any{"source": "USER", "target": "DOMAIN", "properties": map[string]any{" ": true}}, "empty name"},
	}
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		for _, testCase := range cases {
			t.Run(string(policy)+"/"+testCase.name, func(t *testing.T) {
				var database = edgePolicyDatabase()
				database.answers[relationshipQuery] = runner.Rows{testCase.row}
				var before = cloneGraph(*database.graph)
				var plan = fixturePlan()
				plan.Schema.Relationships[0].ExistingEdgePolicy = policy
				assertRollback(t, database, before, plan, completeBatch(), testCase.message)
				if database.helperWrites != 1 {
					t.Fatal("validation failure did not roll back staged helper changes")
				}
			})
		}
	}
}

func TestEdgePolicyConflictingSupportPropertiesRollback(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.ReplaceExisting, extension.MergeProperties, extension.KeepExisting} {
		t.Run(string(policy), func(t *testing.T) {
			var database = edgePolicyDatabase()
			database.answers[relationshipQuery] = runner.Rows{
				{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "one"}},
				{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "two"}},
			}
			var before = cloneGraph(*database.graph)
			assertRollback(t, database, before, edgePolicyPlan(policy), completeBatch(), "conflicting relationship properties")
		})
	}
}

func TestEdgePolicyEquivalentDuplicatePropertiesAgree(t *testing.T) {
	var database = edgePolicyDatabase()
	database.answers[relationshipQuery] = runner.Rows{
		{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"count": int(2), "tags": []string{"one"}}},
		{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"tags": []any{"one"}, "count": float64(2)}},
	}
	var report = runEdgePolicy(t, database, extension.MergeProperties)
	if report.Updated != 1 || report.DesiredCounts["PoC_ADCSESC1"] != 1 {
		t.Fatalf("equivalent supporting rows were not deduplicated: %+v", report)
	}
}

func TestEdgePolicyChangeBudgetIncludesUpdatesAndReplacementBothSteps(t *testing.T) {
	var cases = []struct {
		name                    string
		policy                  extension.ExistingEdgePolicy
		tooSmall, enough        int64
		added, removed, updated int
	}{
		{"default replacement", "", 2, 3, 1, 1, 0},
		{"property update", extension.MergeProperties, 1, 2, 0, 0, 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var database = edgePolicyDatabase()
			database.answers[relationshipQuery][0]["properties"] = map[string]any{"name": "new"}
			var options = completeBatch()
			options.Limits = runner.DefaultLimits()
			options.Limits.ChangedObjects = testCase.tooSmall
			var before = cloneGraph(*database.graph)
			assertRollback(t, database, before, edgePolicyPlan(testCase.policy), options, "change transaction budget")
			if database.relationshipWrites != 0 {
				t.Fatal("budget failure reached publication")
			}
			options.Limits.ChangedObjects = testCase.enough
			var report, err = runner.Run(context.Background(), database, edgePolicyPlan(testCase.policy), options)
			if err != nil || !report.Committed || report.Added != testCase.added || report.Removed != testCase.removed || report.Updated != testCase.updated {
				t.Fatalf("exact sufficient budget failed: %+v error=%v", report, err)
			}
		})
	}
}

// Fail a later write so the transaction has already changed staged graph data.
type edgePolicyFailureDatabase struct {
	*fakeDatabase
	query string
	calls int
}

type edgePolicyFailureTransaction struct {
	runner.Transaction
	database *edgePolicyFailureDatabase
}

func (s *edgePolicyFailureDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &edgePolicyFailureTransaction{Transaction: transaction, database: s}, nil
}

func (s *edgePolicyFailureTransaction) Write(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if query == s.database.query {
		s.database.calls++
		if s.database.calls == 2 {
			return nil, fmt.Errorf("late property publication failure")
		}
	}
	return s.Transaction.Write(ctx, query, parameters)
}

func TestEdgePolicyLateWritesRestorePriorIdentitiesAndProperties(t *testing.T) {
	for _, policy := range []extension.ExistingEdgePolicy{"", extension.MergeProperties} {
		t.Run(string(policy), func(t *testing.T) {
			var database = edgePolicyDatabase()
			database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "SECOND", Kinds: []string{"Domain"}, Properties: map[string]any{}})
			database.graph.Edges = append(database.graph.Edges, fakeEdge{ID: 11, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "SECOND"}, Properties: map[string]any{"name": "second original"}})
			database.answers[relationshipQuery] = runner.Rows{
				{"source": "USER", "target": "DOMAIN", "properties": map[string]any{"name": "first changed"}},
				{"source": "USER", "target": "SECOND", "properties": map[string]any{"name": "second changed"}},
			}
			var query = createRelationshipQuery
			if policy == extension.MergeProperties {
				query = updateRelationshipQuery
				// A stale removal and the first patch precede the failing patch.
				database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "STALE", Kinds: []string{"Domain"}, Properties: map[string]any{}})
				database.graph.Edges = append(database.graph.Edges, fakeEdge{ID: 12, Kind: "PoC_ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "STALE"}, Properties: map[string]any{"name": "stale original"}})
			}
			var failing = &edgePolicyFailureDatabase{fakeDatabase: database, query: query}
			var before = cloneGraph(*database.graph)
			var report, err = runner.Run(context.Background(), failing, edgePolicyPlan(policy), completeBatch())
			if err == nil || !strings.Contains(err.Error(), "late property publication failure") || report.Committed || database.commits != 0 || database.rollbacks != 1 || !reflect.DeepEqual(*database.graph, before) {
				t.Fatalf("late failure did not restore prior graph: report=%+v error=%v before=%+v after=%+v", report, err, before, *database.graph)
			}
			var expectedWrites = 3 // Both replacements removed, first replacement created.
			if policy == extension.MergeProperties {
				expectedWrites = 2
			}
			if failing.calls != 2 || database.relationshipWrites != expectedWrites {
				t.Fatalf("failure did not follow staged writes: calls=%d writes=%d", failing.calls, database.relationshipWrites)
			}
		})
	}
}
