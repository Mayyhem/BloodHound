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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestPublicationGuardsPreserveGraph(t *testing.T) {
	var cases = []struct {
		name          string
		change        string
		finalQuery    string
		errorContains string
	}{
		{
			name:          "duplicate_objectid",
			change:        "CREATE (:Base:User {objectid:'PoC_Test_User', poc_fixture:true})",
			errorContains: "duplicate objectids",
		},
		{
			name:          "helper_label_collision",
			change:        "MATCH (helper:PoC_DomainHelper) SET helper:Base",
			errorContains: "different node kind",
		},
		{
			name:          "incompatible_endpoint",
			finalQuery:    "RETURN 'PoC_Test_CA' AS source, 'PoC_Test_Domain' AS target",
			errorContains: "incompatible endpoint pair",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var (
				database, plan = setup(t)
				runOptions     = options()
			)
			first, err := runner.Run(t.Context(), database, plan, runOptions)
			if err != nil || !first.Committed || first.Added != 1 {
				t.Fatalf("initial publication: %+v, %v", first, err)
			}
			// The next run would update the helper before publishing relationships.
			write(t, database, "MATCH (domain:Domain) SET domain.name = 'CHANGED DOMAIN'", nil)
			if testCase.change != "" {
				write(t, database, testCase.change, nil)
			}
			if testCase.finalQuery != "" {
				replaceESC1Query(&plan, testCase.finalQuery)
			}
			// Keep deliberate fixture changes in the baseline so this comparison
			// detects only writes made by the failed run, including its saved state.
			before := guardGraphSnapshot(t, database)
			runOptions.BatchID = "rejected-fixture-batch"
			report, err := runner.Run(t.Context(), database, plan, runOptions)
			if err == nil || !strings.Contains(err.Error(), testCase.errorContains) {
				t.Fatalf("expected %q rejection, got %+v, %v", testCase.errorContains, report, err)
			}
			if report.Committed {
				t.Fatal("rejected publication reported a commit")
			}
			if testCase.name == "incompatible_endpoint" && report.HelpersChanged != 1 {
				t.Fatalf("expected helper update to be staged before rejection: %+v", report)
			}
			if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected publication changed graph: before %v, after %v", before, after)
			}
		})
	}
}

func TestChangeCoverageControlsPublication(t *testing.T) {
	var (
		database, plan = setup(t)
		runOptions     = options()
	)
	retainExistingConnections(&plan)
	first, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !first.Committed || first.Added != 1 {
		t.Fatalf("initial publication: %+v, %v", first, err)
	}

	write(t, database, "CREATE (:Base:UnrelatedFixture {objectid:'PoC_Test_Unrelated', poc_fixture:true})", nil)
	// ESC1 contains unnamed graph reads. A newly seen kind cannot safely be
	// treated as unrelated, so recalculate its chain while keeping the separate
	// domain helper paused. Only the saved run record should change here.
	snapshotEvidence := func() []runner.Rows {
		result := guardGraphSnapshot(t, database)
		result[0] = read(t, database, "MATCH (node) WHERE NOT node:PoC_Run RETURN id(node) AS id, labels(node) AS kinds, properties(node) AS properties ORDER BY id", nil)
		return result
	}
	before := snapshotEvidence()
	runOptions.BatchID = "unrelated-fixture-batch"
	runOptions.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"UnrelatedFixture"}}
	conservative, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !conservative.Committed || conservative.Skipped || len(conservative.Queries) != len(plan.Queries)-1 || conservative.Unchanged != 1 || conservative.HelpersChanged != 0 {
		t.Fatalf("conservative graph read: %+v, %v", conservative, err)
	}
	if after := snapshotEvidence(); !reflect.DeepEqual(before, after) {
		t.Fatal("conservative recalculation changed existing graph evidence")
	}

	// Complete observation of no changes still permits a true skip.
	runOptions.Changes = runner.Changes{CoverageKnown: true}
	savedBefore := guardGraphSnapshot(t, database)
	skipped, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !skipped.Skipped || skipped.Committed {
		t.Fatalf("known empty changes: %+v, %v", skipped, err)
	}
	if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(savedBefore, after) {
		t.Fatal("skipped run changed the graph or run record")
	}

	// Empty change lists are inconclusive unless their coverage is known.
	write(t, database, "MATCH (domain:Domain) SET domain.name = 'UNKNOWN CHANGE'", nil)
	runOptions.BatchID = "unknown-fixture-batch"
	runOptions.Changes = runner.Changes{}
	full, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !full.Committed || full.Skipped || len(full.Queries) != len(plan.Queries) || full.HelpersChanged != 1 || full.Unchanged != 1 {
		t.Fatalf("unknown change coverage: %+v, %v", full, err)
	}
	if rows := read(t, database, "MATCH (run:PoC_Run) RETURN run.batch_id AS batch", nil); len(rows) != 1 || rows[0]["batch"] != runOptions.BatchID {
		t.Fatalf("full run did not save its batch: %v", rows)
	}

	// A confirmed deletion is a dependency change even when no nodes changed.
	// Only enrollment changed. The separate domain-name helper must stay
	// untouched while ESC1 and its required calculations refresh their answer.
	helpersBefore := read(t, database, "MATCH (helper:PoC_DomainHelper) RETURN id(helper) AS id, properties(helper) AS properties", nil)
	write(t, database, "MATCH ()-[right:Enroll]->(:CertTemplate) DELETE right", nil)
	runOptions.BatchID = "deleted-enrollment-fixture-batch"
	runOptions.Changes = runner.Changes{CoverageKnown: true, Relationships: []string{"Enroll"}}
	removed, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !removed.Committed || removed.Skipped || len(removed.Queries) != len(plan.Queries)-1 || removed.Removed != 1 || removed.Added != 0 || removed.HelpersChanged != 0 {
		t.Fatalf("confirmed enrollment deletion: %+v, %v", removed, err)
	}
	for _, name := range removed.Queries {
		if name == "PoC/domain_helpers.cypher" {
			t.Fatal("an enrollment deletion reran the unrelated domain-name helper")
		}
	}
	if helpersAfter := read(t, database, "MATCH (helper:PoC_DomainHelper) RETURN id(helper) AS id, properties(helper) AS properties", nil); !reflect.DeepEqual(helpersBefore, helpersAfter) {
		t.Fatal("selected enrollment cleanup changed the unrelated helper")
	}
	if rows := read(t, database, "MATCH ()-[edge:PoC_ADCSESC1]->() RETURN id(edge) AS id", nil); len(rows) != 0 {
		t.Fatalf("obsolete relationship survived confirmed deletion: %v", rows)
	}
}

func TestIsolatedMigrationMetadataIdentity(t *testing.T) {
	var (
		database, plan = setup(t)
		runOptions     = options()
	)
	// Native migration metadata has no AD object identity and contributes no
	// graph evidence while it remains isolated.
	write(t, database, "CREATE (:MigrationData {Major:1, Minor:0, Patch:0, poc_fixture:true})", nil)
	first, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !first.Committed || first.Added != 1 {
		t.Fatalf("isolated migration metadata: %+v, %v", first, err)
	}

	// The exemption must end when metadata becomes membership evidence.
	write(t, database, "MATCH (metadata:MigrationData), (group:Group {objectid:'PoC_Test_Group'}) CREATE (metadata)-[:MemberOf]->(group)", nil)
	before := guardGraphSnapshot(t, database)
	runOptions.BatchID = "connected-metadata-fixture-batch"
	report, err := runner.Run(t.Context(), database, plan, runOptions)
	if err == nil || !strings.Contains(err.Error(), "missing, empty, or duplicate objectids") {
		t.Fatalf("expected connected metadata identity rejection, got %+v, %v", report, err)
	}
	if report.Committed {
		t.Fatal("connected metadata rejection reported a commit")
	}
	if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(before, after) {
		t.Fatalf("metadata rejection changed graph: before %v, after %v", before, after)
	}
}

func TestNativePairMismatchPreservesPublication(t *testing.T) {
	var (
		database, plan = setup(t)
		runOptions     = options()
	)
	first, err := runner.Run(t.Context(), database, plan, runOptions)
	if err != nil || !first.Committed || first.Added != 1 {
		t.Fatalf("initial publication: %+v, %v", first, err)
	}
	write(t, database, "MATCH (principal:User {objectid:'PoC_Test_User'}), (domain:Domain {objectid:'PoC_Test_Domain'}) CREATE (principal)-[:ADCSESC1]->(domain) SET domain.name = 'CHANGED DOMAIN'", nil)
	// Both answers contain one relationship, but their sources differ.
	replaceESC1Query(&plan, "RETURN 'PoC_Test_Group' AS source, 'PoC_Test_Domain' AS target")
	before := guardGraphSnapshot(t, database)
	runOptions.BatchID = "native-mismatch-fixture-batch"
	plan.Schema.CompareNativeESC1 = true
	report, err := runner.Run(t.Context(), database, plan, runOptions)
	if err == nil || !strings.Contains(err.Error(), "1 missing and 1 extra") || report.Committed {
		t.Fatalf("expected pair mismatch rejection: %+v, %v", report, err)
	}
	if report.HelpersChanged != 1 {
		t.Fatalf("expected helper update before native comparison: %+v", report)
	}
	if report.NativeComparison == nil {
		t.Fatal("native mismatch report omitted the comparison")
	}
	if expected := []runner.Pair{{Source: "PoC_Test_User", Target: "PoC_Test_Domain"}}; !reflect.DeepEqual(report.NativeComparison.Missing, expected) {
		t.Fatalf("missing native pairs: got %v, want %v", report.NativeComparison.Missing, expected)
	}
	if expected := []runner.Pair{{Source: "PoC_Test_Group", Target: "PoC_Test_Domain"}}; !reflect.DeepEqual(report.NativeComparison.Extra, expected) {
		t.Fatalf("extra experimental pairs: got %v, want %v", report.NativeComparison.Extra, expected)
	}
	if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(before, after) {
		t.Fatalf("native mismatch changed graph: before %v, after %v", before, after)
	}
}

func TestLostCommitAcknowledgementLeavesPublishedGraph(t *testing.T) {
	var (
		database, plan = setup(t)
		runOptions     = options()
		wrapped        = lostCommitAcknowledgementDatabase{Database: database}
	)
	runOptions.BatchID = "committed-with-lost-acknowledgement"
	report, err := runner.Run(t.Context(), wrapped, plan, runOptions)
	if err == nil || !strings.Contains(err.Error(), "commit result is uncertain") || !strings.Contains(err.Error(), "simulated lost commit acknowledgement") || report.Committed {
		t.Fatalf("expected uncertain commit report: %+v, %v", report, err)
	}
	// The real commit succeeded. The runner's deferred rollback cannot undo
	// the edge, helper, and saved batch even though it did not receive success.
	if rows := read(t, database, "MATCH (source)-[edge:PoC_ADCSESC1]->(target) RETURN source.objectid AS source, target.objectid AS target", nil); !reflect.DeepEqual(rows, runner.Rows{{"source": "PoC_Test_User", "target": "PoC_Test_Domain"}}) {
		t.Fatalf("committed relationship was not retained: %v", rows)
	}
	if rows := read(t, database, "MATCH (helper:PoC_DomainHelper) RETURN helper.name AS name, helper.domainid AS domain", nil); !reflect.DeepEqual(rows, runner.Rows{{"name": "TEST DOMAIN", "domain": "PoC_Test_Domain"}}) {
		t.Fatalf("committed helper was not retained: %v", rows)
	}
	if rows := read(t, database, "MATCH (run:PoC_Run) RETURN run.batch_id AS batch", nil); !reflect.DeepEqual(rows, runner.Rows{{"batch": runOptions.BatchID}}) {
		t.Fatalf("committed run state was not retained: %v", rows)
	}
}

// These wrappers simulate a lost acknowledgement at the application boundary,
// after an actual Neo4j commit. They do not inject a network fault.
type lostCommitAcknowledgementDatabase struct {
	runner.Database
}

func (s lostCommitAcknowledgementDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	var transaction, err = s.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return lostCommitAcknowledgementTransaction{Transaction: transaction}, nil
}

type lostCommitAcknowledgementTransaction struct {
	runner.Transaction
}

func (s lostCommitAcknowledgementTransaction) Commit(ctx context.Context) error {
	if err := s.Transaction.Commit(ctx); err != nil {
		return err
	}
	return errors.New("simulated lost commit acknowledgement")
}

func guardGraphSnapshot(t *testing.T, database runner.Database) []runner.Rows {
	t.Helper()
	var (
		nodes = read(t, database, "MATCH (item) RETURN id(item) AS id, labels(item) AS kinds, properties(item) AS properties ORDER BY id", nil)
		edges = read(t, database, "MATCH (source)-[edge]->(target) RETURN id(edge) AS id, id(source) AS source, id(target) AS target, type(edge) AS kind, properties(edge) AS properties ORDER BY id", nil)
	)
	return []runner.Rows{nodes, edges}
}
