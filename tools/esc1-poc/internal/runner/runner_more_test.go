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
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestRunSharesIntermediateRowsWithoutLeakingOtherAnswers(t *testing.T) {
	var (
		database       = fixtureDatabase()
		plan           = fixturePlan()
		unrelatedQuery = "RETURN 'unrelated fixture' AS fixture"
		sharedQuery    = "RETURN 'shared fixture' AS fixture"
		unrelatedRows  = runner.Rows{{"private_value": "only shared may read this"}}
		sharedRows     = runner.Rows{{"id": "template-1", "count": int64(2), "kinds": []string{"CertTemplate"}, "properties": map[string]any{"eligible": true}}}
		sharedCalls    int
		dependentCalls int
	)
	database.answers[unrelatedQuery] = unrelatedRows
	database.answers[sharedQuery] = sharedRows
	plan.Queries[0].Dependencies = []extension.Dependency{{Type: "query", Name: "shared.cypher"}}
	plan.Queries[1].Dependencies = []extension.Dependency{{Type: "query", Name: "shared.cypher"}}
	plan.Queries = append([]extension.Query{
		{Name: "unrelated.cypher", Parameter: "unrelated", Text: unrelatedQuery, Dependencies: []extension.Dependency{{Type: "node", Name: "CertTemplate"}}},
		{Name: "shared.cypher", Parameter: "shared", Text: sharedQuery, Dependencies: []extension.Dependency{{Type: "query", Name: "unrelated.cypher"}}},
	}, plan.Queries...)
	report, err := runner.Run(context.Background(), database, plan, completeBatch())
	if err != nil || !report.Committed {
		t.Fatalf("run failed: report=%+v error=%v", report, err)
	}
	for _, call := range database.reads {
		switch call.query {
		case sharedQuery:
			sharedCalls++
			if !reflect.DeepEqual(call.parameters, map[string]any{"unrelated": unrelatedRows}) {
				t.Fatalf("shared parameters: %+v", call.parameters)
			}
		case helperQuery, relationshipQuery:
			dependentCalls++
			if !reflect.DeepEqual(call.parameters, map[string]any{"shared": sharedRows}) {
				t.Fatalf("dependent received altered rows or undeclared answers: %+v", call.parameters)
			}
		}
	}
	if sharedCalls != 1 || dependentCalls != 2 {
		t.Fatalf("shared executed %d times, dependents executed %d times", sharedCalls, dependentCalls)
	}
}

func TestRunSkipsOnlyPreviouslySuccessfulUnchangedPlanWithKnownUnrelatedChanges(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
		options  = completeBatch()
	)
	plan.Queries[1].Dependencies = append(plan.Queries[1].Dependencies, extension.Dependency{Type: "relationship", Name: "Enroll"})
	options.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"Computer"}}
	first, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !first.Committed || first.Skipped || len(first.Queries) != 2 {
		t.Fatalf("first run skipped without a successful baseline: %+v error=%v", first, err)
	}
	before := cloneGraph(*database.graph)
	readsBefore := len(database.reads)
	skipped, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !skipped.Skipped || skipped.Committed || len(skipped.Queries) != 0 {
		t.Fatalf("safe skip failed: %+v error=%v", skipped, err)
	}
	if !reflect.DeepEqual(*database.graph, before) {
		t.Fatal("skipped run changed persisted graph")
	}
	for _, read := range database.reads[readsBefore:] {
		if read.query == helperQuery || read.query == relationshipQuery {
			t.Fatal("skipped run recalculated query answers")
		}
	}
	// Even unrelated evidence cannot justify skipping a changed calculation.
	plan.Queries[0].Text += "\n// edited calculation"
	database.answers[plan.Queries[0].Text] = database.answers[helperQuery]
	edited, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !edited.Committed || edited.Skipped || len(edited.Queries) != 2 {
		t.Fatalf("edited plan skipped: %+v error=%v", edited, err)
	}
	options.Changes = runner.Changes{}
	unknown, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !unknown.Committed || unknown.Skipped || len(unknown.Queries) != 2 {
		t.Fatalf("unknown coverage skipped: %+v error=%v", unknown, err)
	}
	options.Changes = runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}}
	relationshipOnly, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !relationshipOnly.Committed || relationshipOnly.Skipped || len(relationshipOnly.Queries) != 2 {
		t.Fatalf("relationship-only changes skipped: %+v error=%v", relationshipOnly, err)
	}
}

func TestRunRejectsHelperCollisionsAndReservedFieldsWithoutMutatingNativeData(t *testing.T) {
	var cases = []struct {
		name     string
		existing *fakeNode
		row      map[string]any
		message  string
	}{
		{
			name:     "native node using helper-shaped objectid",
			existing: &fakeNode{ID: "PoC_Helper_example", Kinds: []string{"User"}, Properties: map[string]any{"name": "native user", "poc_owner": "native-owner"}},
			message:  "different node kind",
		},
		{
			name:    "reserved objectid property",
			row:     map[string]any{"id": "PoC_Helper_example", "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"objectid": "USER"}},
			message: "reserved",
		},
		{
			name:    "native kind in helper output",
			row:     map[string]any{"id": "PoC_Helper_example", "kinds": []string{"User"}, "properties": map[string]any{}},
			message: "only the approved kind",
		},
		{
			name:    "extra kind in helper output",
			row:     map[string]any{"id": "PoC_Helper_example", "kinds": []string{"PoC_Helper", "User"}, "properties": map[string]any{}},
			message: "only the approved kind",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var database = fixtureDatabase()
			database.graph.Edges = []fakeEdge{{ID: 8, Kind: "ADCSESC1", Pair: runner.Pair{Source: "USER", Target: "DOMAIN"}}}
			if testCase.existing != nil {
				database.graph.Nodes = append(database.graph.Nodes, *testCase.existing)
			}
			if testCase.row != nil {
				database.answers[helperQuery] = runner.Rows{testCase.row}
			}
			assertRollback(t, database, cloneGraph(*database.graph), fixturePlan(), completeBatch(), testCase.message)
			if database.helperWrites != 0 || database.relationshipWrites != 0 {
				t.Fatal("invalid helper answers attempted graph mutation")
			}
		})
	}
}
