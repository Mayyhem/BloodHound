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
	"os"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

// Exercise the uploaded removal through both real database adapters. In
// particular, a failure after deletion must restore identities and properties,
// including existing connections carrying a legacy property.
func TestRuleRemovalAndRollbackOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, fixture = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			var initialPlan = selectionIntegrationPlan(fixture)
			store, err := ruleservice.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			initial, err := store.Update(selectionServiceFiles(t, initialPlan))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := ruleservice.ValidateBundle(initial)
			if err != nil {
				t.Fatal(err)
			}
			setSelectionEvidence(t, backend.native, true, "initial")
			report, err := runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || report.Added != 2 {
				t.Fatalf("baseline: %+v %v", report, err)
			}
			before := selectionSnapshot(t, backend.database)
			change := ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"selected_helper.cypher"}}
			preview, err := store.Preview(change)
			if err != nil {
				t.Fatal(err)
			}
			change.ConfirmRevision = preview.Revision
			removed, err := store.Apply(change)
			if err != nil {
				t.Fatal(err)
			}
			plan, err = ruleservice.ValidateBundle(removed)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(plan.RetiredRelationships, []string{"PoC_ADCSESC1"}) || len(plan.Queries) != 2 {
				t.Fatalf("removal plan: %+v", plan)
			}
			if after := selectionSnapshot(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatal("upload changed the graph")
			}
			// Complete change tracking must not skip the changed rule set.
			runOptions := options()
			runOptions.Changes = runner.Changes{CoverageKnown: true}
			report, err = runner.Run(t.Context(), &selectionRecordFailureDatabase{Database: backend.database}, plan, runOptions)
			if err == nil || report.Committed || report.Removed < 1 {
				t.Fatalf("late failure: %+v %v", report, err)
			}
			if after := selectionSnapshot(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatal("late failure lost previous graph data")
			}
			// A label left by an older version has no special meaning. Removing
			// this type clears matching connections regardless of its value.
			if err := backend.native.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
				var properties = graph.NewProperties()
				properties.Set("poc_owner", "someone-else")
				return transaction.Relationships().Filter(query.Kind(query.Relationship(), graph.StringKind("PoC_ADCSESC1"))).Update(properties)
			}); err != nil {
				t.Fatal(err)
			}
			report, err = runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Committed || len(report.Queries) != 2 {
				t.Fatalf("remove one branch: %+v %v", report, err)
			}
			after := selectionSnapshot(t, backend.database)
			if len(after[0]) != 0 || len(after[1]) != 1 || !reflect.DeepEqual(before[2], after[2]) {
				t.Fatal("removal lost surviving connections or the retired helper")
			}
			report, err = runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Skipped {
				t.Fatalf("unchanged removal revision: %+v %v", report, err)
			}
			change = ruleservice.Change{BaseRevision: removed.Revision, Remove: []string{"independent_helper.cypher"}}
			preview, err = store.Preview(change)
			if err != nil {
				t.Fatal(err)
			}
			change.ConfirmRevision = preview.Revision
			empty, err := store.Apply(change)
			if err != nil {
				t.Fatal(err)
			}
			plan, err = ruleservice.ValidateBundle(empty)
			if err != nil {
				t.Fatal(err)
			}
			report, err = runner.Run(t.Context(), backend.database, plan, runOptions)
			if err != nil || !report.Committed || len(report.Queries) != 0 || report.Removed != 1 {
				t.Fatalf("last rule removal: %+v %v", report, err)
			}
			after = selectionSnapshot(t, backend.database)
			if len(after[0])+len(after[1]) != 0 || len(after[2]) != 1 || len(after[3]) != 1 {
				t.Fatal("last removal did not preserve helper objects")
			}
		})
	}
}
