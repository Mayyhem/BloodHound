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
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestCypherKindPublishesAndEmptyAnswerRemovesOwnedEdges(t *testing.T) {
	var (
		plan     = fixturePlan()
		database = fixtureDatabase()
	)
	plan.Queries[1].OutputKind = "PoC_ADCSESC1"
	database.answers[relationshipQuery][0]["kind"] = "PoC_ADCSESC1"
	database.answers[relationshipQuery][0]["properties"] = map[string]any{"evidence": "fixture"}
	for attempt := 0; attempt < 2; attempt++ {
		report, err := runner.Run(context.Background(), database, plan, completeBatch())
		if err != nil || !report.Committed || len(database.graph.Edges) != 1 {
			t.Fatal(report, err)
		}
		edge := database.graph.Edges[0]
		if edge.Kind != "PoC_ADCSESC1" || edge.Properties["evidence"] != "fixture" {
			t.Fatal("wrong publication", edge)
		}
		if _, exists := edge.Properties["kind"]; exists {
			t.Fatal("routing column was persisted as a property")
		}
	}
	database.answers[relationshipQuery] = nil
	report, err := runner.Run(context.Background(), database, plan, completeBatch())
	if err != nil || !report.Committed || report.Removed != 1 || len(database.graph.Edges) != 0 {
		t.Fatal("empty result did not clean up owned output", report, err)
	}
}

func TestCypherKindMismatchRollsBack(t *testing.T) {
	for _, value := range []any{nil, "PoC_Other", 1, []string{"PoC_ADCSESC1"}} {
		var (
			plan     = fixturePlan()
			database = fixtureDatabase()
		)
		runSuccessfully(t, database)
		before := cloneGraph(*database.graph)
		database = fixtureDatabase()
		persisted := cloneGraph(before)
		database.graph = &persisted
		plan.Queries[1].OutputKind = "PoC_ADCSESC1"
		database.answers[relationshipQuery][0]["kind"] = value
		assertRollback(t, database, before, plan, completeBatch(), "declared literal")
	}
}
