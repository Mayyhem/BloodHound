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

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func sharedProducerSnapshot(t *testing.T, database runner.Database) []runner.Rows {
	t.Helper()
	return []runner.Rows{
		read(t, database, "MATCH (source)-[edge:PoC_Shared]->(target) RETURN source.objectid AS source, id(edge) AS id, properties(edge) AS properties ORDER BY source", nil),
		read(t, database, "MATCH (source)-[edge:PoC_SharedConsumer]->(target) RETURN source.objectid AS source, id(edge) AS id, properties(edge) AS properties ORDER BY source", nil),
		read(t, database, "MATCH (run:PoC_Run) RETURN properties(run) AS properties", nil),
	}
}

// Two independent rules overlap on one pair. Their graph-reading consumer must
// see the complete combined answer in the same transaction, including after a
// disable or deletion changes which calculations can run.
func TestSharedProducersDisableDeleteAndRollbackOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, _ = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			var files = map[string]string{
				"schema.json": `{
					"schema":{"name":"shared_test","display_name":"Shared outputs","version":"v1.0.0","namespace":"PoC"},
					"node_kinds":[],
					"relationship_kinds":[
						{"name":"PoC_Shared","existing_edge_policy":"merge-properties","compatible_kinds":[{"source_kind":"User","target_kind":"Domain"},{"source_kind":"Group","target_kind":"Domain"}]},
						{"name":"PoC_SharedConsumer","existing_edge_policy":"merge-properties","compatible_kinds":[{"source_kind":"User","target_kind":"Domain"},{"source_kind":"Group","target_kind":"Domain"}]}
					]}`,
				"a.cypher":        "// depends-on: node:Group\nMATCH (evidence:Group) UNWIND ['PoC_Test_User','PoC_Test_Group'] AS identifier RETURN identifier AS source, 'PoC_Test_Domain' AS target, 'PoC_Shared' AS kind, {name:'a',from_a:true} AS properties",
				"b.cypher":        "// depends-on: node:User\nMATCH (evidence:User) RETURN evidence.objectid AS source, 'PoC_Test_Domain' AS target, 'PoC_Shared' AS kind, {name:'b',from_b:true} AS properties",
				"consumer.cypher": "// depends-on: relationship:PoC_Shared\nMATCH (source)-[:PoC_Shared]->(target) RETURN source.objectid AS source, target.objectid AS target, 'PoC_SharedConsumer' AS kind",
			}
			store, err := ruleservice.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			active, err := store.Update(files)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := ruleservice.ValidateBundle(active)
			if err != nil {
				t.Fatal(err)
			}
			change := func(request ruleservice.Change) extension.Plan {
				request.BaseRevision = active.Revision
				preview, err := store.Preview(request)
				if err != nil {
					t.Fatal(err)
				}
				request.ConfirmRevision = preview.Revision
				active, err = store.Apply(request)
				if err != nil {
					t.Fatal(err)
				}
				updated, err := ruleservice.ValidateBundle(active)
				if err != nil {
					t.Fatal(err)
				}
				return updated
			}
			report, err := nativeIntegrationRun(t, backend, plan, options())
			if err != nil || !report.Committed || report.Added != 4 {
				t.Fatalf("initial shared publication: %+v %v", report, err)
			}
			before := sharedProducerSnapshot(t, backend.database)
			if len(before[0]) != 2 || len(before[1]) != 2 {
				t.Fatalf("consumer did not read the complete shared result: %v", before)
			}
			for _, row := range before[0] {
				if row["source"] != "PoC_Test_User" {
					continue
				}
				properties := row["properties"].(map[string]any)
				if properties["name"] != "b" || properties["from_a"] != true || properties["from_b"] != true {
					t.Fatalf("overlapping producers did not merge in stable rule order: %v", properties)
				}
			}
			// Only A reads Group, but refreshing its output also needs B's answer.
			selected := options()
			selected.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"Group"}}
			if report, err := nativeIntegrationRun(t, backend, plan, selected); err != nil || !report.Committed || len(report.Queries) != 3 {
				t.Fatalf("selected shared recalculation: %+v %v", report, err)
			}
			plan = change(ruleservice.Change{Disable: []string{"a.cypher"}})
			if report, err := nativeIntegrationRun(t, backend, plan, options()); err != nil || !report.Committed || len(report.Queries) != 0 {
				t.Fatalf("disable did not pause shared output and its consumer: %+v %v", report, err)
			}
			paused := sharedProducerSnapshot(t, backend.database)
			if !reflect.DeepEqual(before[:2], paused[:2]) {
				t.Fatal("disabling a producer changed saved connections")
			}
			plan = change(ruleservice.Change{Remove: []string{"a.cypher"}})
			if len(plan.Queries) != 2 || len(plan.RetiredRelationships) != 0 {
				t.Fatalf("removing one producer removed a surviving producer or consumer: %+v", plan)
			}
			report, err = runner.Run(t.Context(), &selectionRecordFailureDatabase{Database: backend.database}, plan, options())
			if err == nil || report.Committed || report.Removed != 2 {
				t.Fatalf("late failure did not follow both staged removals: %+v %v", report, err)
			}
			if after := sharedProducerSnapshot(t, backend.database); !reflect.DeepEqual(paused, after) {
				t.Fatal("failed shared recalculation changed committed results")
			}
			if report, err := nativeIntegrationRun(t, backend, plan, options()); err != nil || !report.Committed || report.Removed != 2 {
				t.Fatalf("remaining producer refresh: %+v %v", report, err)
			}
			after := sharedProducerSnapshot(t, backend.database)
			if len(after[0]) != 1 || len(after[1]) != 1 || after[0][0]["source"] != "PoC_Test_User" || after[1][0]["source"] != "PoC_Test_User" {
				t.Fatalf("remaining support and consumer disagree: %v", after)
			}
			plan = change(ruleservice.Change{Remove: []string{"b.cypher"}})
			if len(plan.Queries) != 1 || plan.Queries[0].Name != "consumer.cypher" || !reflect.DeepEqual(plan.RetiredRelationships, []string{"PoC_Shared"}) {
				t.Fatalf("last producer removal should preserve the graph consumer: %+v", plan)
			}
			if report, err := nativeIntegrationRun(t, backend, plan, options()); err != nil || !report.Committed || report.Removed != 2 {
				t.Fatalf("last producer and consumer cleanup: %+v %v", report, err)
			}
			if empty := sharedProducerSnapshot(t, backend.database); len(empty[0]) != 0 || len(empty[1]) != 0 {
				t.Fatalf("consumer did not observe retired output being cleared: %v", empty)
			}
		})
	}
}
