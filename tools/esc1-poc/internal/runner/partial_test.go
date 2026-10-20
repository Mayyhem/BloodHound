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
	"errors"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// runOn makes the attempt function RunSettingAsideFailures repeats.
func runOn(database runner.Database) runner.Execute {
	return func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		return runner.Run(ctx, database, plan, options)
	}
}

func stepStates(report runner.Report) map[string]string {
	var states = map[string]string{}
	for _, query := range report.Execution.Queries {
		states[query.Name] = query.State
	}
	return states
}

func savedUnfinished(t *testing.T, graph fakeGraph) []string {
	t.Helper()
	for _, node := range graph.Nodes {
		if reflect.DeepEqual(node.Kinds, []string{"PoC_Run"}) {
			names, _ := node.Properties["unfinished"].([]string)
			return names
		}
	}
	t.Fatal("no saved run record")
	return nil
}

// The independent chain fails. The ESC1 chain does not use it, so ESC1 is
// still calculated and saved, while the failed chain keeps its old results.
// The next run, with no new data, calculates only the unfinished chain.
func TestFailingStepIsSetAsideAndUnrelatedStepsAreSaved(t *testing.T) {
	var database, plan = independentSelectionFixture()
	runSelection(t, database, plan, runner.Changes{})
	var (
		keptEdges   = selectionOutputBytes(t, *database.graph, "PoC_Independent")
		keptHelpers = selectionOutputBytes(t, *database.graph, "PoC_IndependentHelper")
		options     = completeBatch()
		setAside    = []string{"independent_helper.cypher", "independent.cypher"}
	)
	options.TemporaryDirectory = t.TempDir()
	// New answers for both chains. Only the ESC1 chain's may be saved.
	database.answers[relationshipQuery] = nil
	database.answers[independentEdgeQuery] = nil
	database.readErrors[independentHelperQuery] = errors.New("injected independent failure")

	report, err := runner.RunSettingAsideFailures(t.Context(), plan, options, runOn(database))
	if err != nil || !report.Committed || !reflect.DeepEqual(report.SetAside, setAside) {
		t.Fatalf("expected a partial save: report=%+v error=%v", report, err)
	}
	if report.Removed != 1 || report.Execution.Calculation != "partial" || report.Execution.Save != "succeeded" {
		t.Fatalf("the unrelated ESC1 change was not saved: %+v", report.Execution)
	}
	if failure := report.Execution.Failure; failure == nil || failure.Query != "independent_helper.cypher" || len(report.Execution.Failures) != 1 {
		t.Fatalf("the failure was not reported: %+v", report.Execution)
	}
	expected := map[string]string{"helper.cypher": "succeeded", "esc1.cypher": "succeeded", "independent_helper.cypher": "failed", "independent.cypher": "set-aside"}
	if states := stepStates(report); !reflect.DeepEqual(states, expected) {
		t.Fatalf("step states %v; expected %v", states, expected)
	}
	for _, query := range report.Execution.Queries {
		if query.Name == "independent_helper.cypher" && (query.Reason == report.Execution.Failure.Message || query.Reason == "") {
			t.Fatalf("the failed step's reason repeats the failure instead of explaining it: %q", query.Reason)
		}
		// The failed step ran in the first attempt; the report keeps the
		// times from that attempt.
		if query.Name == "independent_helper.cypher" && (query.StartedAt == nil || query.FinishedAt == nil) {
			t.Fatalf("the failed step lost its start and finish times: %+v", query)
		}
	}
	if selectionOutputBytes(t, *database.graph, "PoC_Independent") != keptEdges || selectionOutputBytes(t, *database.graph, "PoC_IndependentHelper") != keptHelpers {
		t.Fatal("a set-aside step's previous results changed")
	}
	if names := savedUnfinished(t, *database.graph); !reflect.DeepEqual(names, setAside) {
		t.Fatalf("saved run record lists %v as unfinished", names)
	}

	// The retry: no new data, so only the unfinished chain runs, and it saves.
	delete(database.readErrors, independentHelperQuery)
	retry := runSelection(t, database, plan, runner.Changes{CoverageKnown: true})
	if !reflect.DeepEqual(retry.Queries, setAside) || retry.Removed != 1 {
		t.Fatalf("retry did not calculate only the unfinished steps: %+v", retry)
	}
	if names := savedUnfinished(t, *database.graph); len(names) != 0 {
		t.Fatalf("a complete run left %v unfinished", names)
	}
	options.Changes = runner.Changes{CoverageKnown: true}
	if again, err := runner.Run(t.Context(), database, plan, options); err != nil || !again.Skipped {
		t.Fatalf("nothing changed and nothing is unfinished, but the run calculated: %+v error=%v", again, err)
	}
}

// Saving a relationship type combines all of its producers, so a failure
// there names the type. Its producers are set aside; their inputs still save.
func TestFailureWhileSavingATypeSetsAsideItsProducers(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
	)
	options.TemporaryDirectory = t.TempDir()
	database.writeErrors[createRelationshipQuery] = errors.New("injected save failure")
	report, err := runner.RunSettingAsideFailures(t.Context(), fixturePlan(), options, runOn(database))
	if err != nil || !report.Committed || !reflect.DeepEqual(report.SetAside, []string{"esc1.cypher"}) {
		t.Fatalf("expected the helper step to save without ESC1: report=%+v error=%v", report, err)
	}
	if failure := report.Execution.Failure; failure.Relationship != "PoC_ADCSESC1" || failure.Query != "" {
		t.Fatalf("failure did not name the relationship type: %+v", failure)
	}
	if report.HelpersChanged != 1 || report.Added != 0 {
		t.Fatalf("unexpected saved changes: %+v", report)
	}
	for _, edge := range database.graph.Edges {
		if edge.Kind == "PoC_ADCSESC1" {
			t.Fatal("a connection from the failed save was kept")
		}
	}
}

// A failure that belongs to no step cannot be set aside. Nothing is saved,
// and no second attempt is made.
func TestFailureWithoutAStepSavesNothing(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
	)
	options.TemporaryDirectory = t.TempDir()
	database.readErrors[identityQuery] = errors.New("injected identity failure")
	report, err := runner.RunSettingAsideFailures(t.Context(), fixturePlan(), options, runOn(database))
	if err == nil || report.Committed || database.begins != 1 || database.commits != 0 {
		t.Fatalf("a failure without a step was retried or saved: report=%+v error=%v begins=%d", report, err, database.begins)
	}
}

// When every step is tied to the failure, the save still records them as
// unfinished, so the next run calculates them all again.
func TestEveryStepSetAsideSavesOnlyTheUnfinishedRecord(t *testing.T) {
	var (
		database = fixtureDatabase()
		options  = completeBatch()
	)
	options.TemporaryDirectory = t.TempDir()
	database.readErrors[helperQuery] = errors.New("injected helper failure")
	report, err := runner.RunSettingAsideFailures(t.Context(), fixturePlan(), options, runOn(database))
	if err != nil || !report.Committed || !reflect.DeepEqual(report.SetAside, []string{"helper.cypher", "esc1.cypher"}) {
		t.Fatalf("expected an empty save with both steps unfinished: report=%+v error=%v", report, err)
	}
	if report.HelpersChanged != 0 || report.Added != 0 {
		t.Fatalf("a set-aside step saved changes: %+v", report)
	}
	if names := savedUnfinished(t, *database.graph); !reflect.DeepEqual(names, report.SetAside) {
		t.Fatalf("saved run record lists %v as unfinished", names)
	}
}
