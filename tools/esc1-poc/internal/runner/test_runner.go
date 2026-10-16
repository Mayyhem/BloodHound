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

package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Test runs the plan the caller already selected, then confirms that every
// temporary graph change was discarded. It intentionally does not prepare
// kinds or indexes, select an incremental subset, remove retired outputs, save
// a PoC_Run node, or commit. The selected test database must already have the
// schema needed to read its baseline safely.
func Test(ctx context.Context, database Database, plan extension.Plan, options Options) (Report, error) {
	var (
		started = time.Now()
		report  = Report{BatchID: options.BatchID, QueryRows: map[string]int{}, Desired: map[string][]Pair{}, DesiredCounts: map[string]int64{}, Execution: newExecution(plan)}
		err     error
	)
	var testPlan extension.Plan
	var validateOnlyNodes, withheldRelationships map[string]bool
	testPlan, validateOnlyNodes, withheldRelationships, err = testPlanForTarget(plan, options.TestTarget)
	if err == nil {
		err = test(ctx, database, testPlan, options, &report, validateOnlyNodes, withheldRelationships)
	}
	finishTestReport(&report, started, err)
	return report, err
}

// testPlan preserves all selected calculations but withholds the output the
// editor is inspecting. Helper outputs and relationship outputs needed by a
// later selected query keep their bindings so those later reads see them.
func testPlanForTarget(plan extension.Plan, target string) (extension.Plan, map[string]bool, map[string]bool, error) {
	if target == "" {
		target = plan.EditorTarget
	} else if plan.EditorTarget != "" && target != plan.EditorTarget {
		return extension.Plan{}, nil, nil, fmt.Errorf("test target %q does not match the editor target %q", target, plan.EditorTarget)
	}
	if target == "" {
		return plan, nil, nil, nil
	}
	var result = plan
	var validateOnlyNodes = map[string]bool{}
	var withheldRelationships = map[string]bool{}
	var found bool
	for _, query := range plan.Queries {
		found = found || query.Name == target
	}
	if !found {
		return extension.Plan{}, nil, nil, fmt.Errorf("test target %q is not in the selected plan", target)
	}
	for _, binding := range plan.Schema.Nodes {
		if binding.Query == target {
			validateOnlyNodes[binding.Kind] = true
		}
	}
	for _, binding := range plan.Schema.Relationships {
		if binding.Query == target {
			withheldRelationships[binding.Kind] = true
		}
	}
	for _, query := range plan.Queries {
		for _, dependency := range query.Dependencies {
			if dependency.Type == "relationship" && withheldRelationships[dependency.Name] && query.Name != target {
				return extension.Plan{}, nil, nil, fmt.Errorf("test target %q produces %q, needed by %s; select a final output that no later query reads", target, dependency.Name, query.Name)
			}
		}
	}
	return result, validateOnlyNodes, withheldRelationships, nil
}

func test(ctx context.Context, database Database, plan extension.Plan, options Options, report *Report, validateOnlyNodes, withheldRelationships map[string]bool) error {
	var (
		phaseStarted = time.Now()
		limits       Limits
		space        *answerSpace
		transaction  Transaction
		err          error
	)
	defer func() { report.PreparationElapsed += time.Since(phaseStarted) }()
	if options.BatchID == "" || !options.IngestionComplete {
		return fmt.Errorf("a successfully completed ingestion batch is required; the test did not change the graph")
	}
	if !options.WritersPaused {
		return fmt.Errorf("other graph writers must be paused before testing; the test did not change the graph")
	}
	if limits, err = options.Limits.checked(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, limits.RunTimeout)
	defer cancel()
	if space, err = newAnswerSpace(options.TemporaryDirectory, limits); err != nil {
		return err
	}
	defer space.close()
	space.observeAnswer = options.ObserveAnswer
	space.inspectTestAnswer = options.InspectTestAnswer
	space.testTarget = options.TestTarget
	if space.testTarget == "" {
		space.testTarget = plan.EditorTarget
	}

	transactionStarted := time.Now()
	if transaction, err = database.Begin(ctx); err != nil {
		report.Preparation.Transaction = time.Since(transactionStarted)
		return err
	}
	report.Preparation.Transaction = time.Since(transactionStarted)

	// A test uses the plan given by the editor. It never uses affectedPlan or
	// retired-output cleanup, because either would change what was requested.
	report.Execution.selectQueries(plan, "The editor selected this complete prerequisite chain for testing.")
	identityStarted := time.Now()
	invalidIdentities, identityErr := transaction.Read(ctx, "MATCH (item) WHERE NOT (item:MigrationData AND size(labels(item)) = 1 AND NOT (item)--()) WITH item.objectid AS objectid, count(*) AS matches WHERE objectid IS NULL OR trim(objectid) = '' OR matches <> 1 RETURN objectid LIMIT 1", nil)
	report.Preparation.Identities = time.Since(identityStarted)
	if identityErr != nil {
		err = fmt.Errorf("check graph identities: %w", identityErr)
	} else if len(invalidIdentities) != 0 {
		err = fmt.Errorf("graph contains missing, empty, or duplicate objectids; the test did not change the graph")
	}
	if err == nil {
		var staged = map[string]bool{}
		stage := func(binding extension.RelationshipBinding, desired *answerFile) error {
			if withheldRelationships[binding.Kind] {
				return fmt.Errorf("test target relationship %q cannot be temporarily published", binding.Kind)
			}
			if staged[binding.Kind] {
				return nil
			}
			if binding.Kind == "PoC_ADCSESC1" && plan.Schema.CompareNativeESC1 {
				comparison, comparisonErr := compareFiles(ctx, transaction, desired)
				report.NativeComparison = &comparison
				if comparisonErr != nil {
					return comparisonErr
				}
				if comparison.MissingCount != 0 || comparison.ExtraCount != 0 {
					return fmt.Errorf("experimental ESC1 differs from native ESC1: %d missing and %d extra connections; all test changes were discarded", comparison.MissingCount, comparison.ExtraCount)
				}
			}
			changes, stageErr := prepareChanges(ctx, transaction, binding, desired)
			if stageErr != nil {
				return stageErr
			}
			changed := int64(report.Added+report.Removed+report.Updated+report.HelpersChanged+1) + changes.additions.rows + changes.removals.rows + changes.updates.rows
			if changed > limits.ChangedObjects {
				return fmt.Errorf("combined graph changes exceed the %d-change transaction budget", limits.ChangedObjects)
			}
			if stageErr = applyChanges(ctx, transaction, changes); stageErr != nil {
				return stageErr
			}
			report.Added += int(changes.additions.rows)
			report.Removed += int(changes.removals.rows)
			report.Updated += int(changes.updates.rows)
			report.Unchanged += changes.unchanged
			staged[binding.Kind] = true
			for _, file := range []*answerFile{changes.additions, changes.removals, changes.updates} {
				if stageErr = file.remove(); stageErr != nil {
					return stageErr
				}
			}
			return nil
		}
		report.Execution.Phase, report.Execution.Calculation = "calculation", "running"
		report.PreparationElapsed += time.Since(phaseStarted)
		phaseStarted = time.Now()
		desiredByKind, calculationErr := calculate(ctx, database, transaction, plan, space, report, stage, validateOnlyNodes, withheldRelationships)
		err = calculationErr
		if err == nil && plan.Schema.CompareNativeESC1 {
			for _, binding := range plan.Schema.Relationships {
				if !withheldRelationships[binding.Kind] || binding.Kind != "PoC_ADCSESC1" {
					continue
				}
				comparison, comparisonErr := compareFiles(ctx, transaction, desiredByKind[binding.Kind])
				report.NativeComparison = &comparison
				if comparisonErr != nil {
					err = comparisonErr
					break
				}
				if comparison.MissingCount != 0 || comparison.ExtraCount != 0 {
					err = fmt.Errorf("experimental ESC1 differs from native ESC1: %d missing and %d extra connections; all test changes were discarded", comparison.MissingCount, comparison.ExtraCount)
					break
				}
			}
		}
		report.CalculationElapsed = time.Since(phaseStarted)
		if err == nil {
			report.Execution.Calculation = "succeeded"
			report.TemporaryPeakBytes = space.peak
		}
	}

	// Always use a new context. The calculation context can be canceled or past
	// its deadline, but the database still needs an opportunity to discard work.
	rollbackContext, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	rollbackErr := transaction.Rollback(rollbackContext)
	rollbackCancel()
	if rollbackErr != nil {
		if err != nil {
			return fmt.Errorf("%w: %w; discard test changes: %v", ErrTestDiscardUncertain, err, rollbackErr)
		}
		return fmt.Errorf("%w: %w", ErrTestDiscardUncertain, rollbackErr)
	}
	if err != nil {
		return err
	}
	report.Tested = true
	report.Discarded = true
	report.GraphObjectsChanged = 0
	return nil
}

func finishTestReport(report *Report, started time.Time, err error) {
	finishReport(report, started, err)
	if err == nil && report.Discarded {
		report.Execution.Calculation = "succeeded"
		report.Execution.Save = "discarded"
		report.Execution.Phase = "complete"
	}
}
