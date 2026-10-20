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
	"errors"
	"strings"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

// Execute runs one attempt of a plan and saves it all or not at all. Run is
// one; BloodHound's own database connection uses a version that borrows its
// save transaction.
type Execute func(context.Context, extension.Plan, Options) (Report, error)

// RunSettingAsideFailures lets steps unrelated to a failure still finish.
//
// Each attempt still saves everything or nothing, so the graph is never left
// half-updated. When an attempt fails at a step, nothing from it is saved.
// This then sets aside that step and every step tied to it (see
// Plan.SetAside) and runs the rest again as a new attempt. If another step
// fails, it sets that one aside too and tries again. Set-aside steps keep
// their previously saved results, and the saved run record lists them as
// unfinished, so the next run calculates them again.
//
// Why a new attempt instead of carrying on inside the failed one: a database
// error ends the save transaction on both PostgreSQL and Neo4j, and the failed
// step may already have staged helper objects or connections in it. Starting
// again is the only way to be sure none of the failed step's work is saved.
//
// It returns the failure and saves nothing when the failure belongs to no step
// or relationship type (for example, invalid object IDs in the graph, or the
// database could not confirm the save), or when the run was canceled or ran
// out of time. A run that finished with set-aside steps returns no error; its
// report lists them in SetAside, marks the calculation "partial", and lists
// every failure.
func RunSettingAsideFailures(ctx context.Context, plan extension.Plan, options Options, execute Execute) (Report, error) {
	var (
		started             = time.Now()
		failures            []Failure
		failedRuns          = map[string]QueryReport{}
		failedQueries       []string
		failedRelationships []string
		setAside            []string
		runnable            = runnablePlan(plan)
	)
	// All attempts together share one time limit, not one limit each.
	if limits, err := options.Limits.checked(); err == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.RunTimeout)
		defer cancel()
	}
	for {
		options.SetAside = setAside
		report, err := execute(ctx, plan, options)
		if err == nil {
			if len(failures) != 0 {
				markSetAside(&report, failures, failedRuns, setAside, started)
			}
			return report, nil
		}
		if report.Execution != nil && len(failures) != 0 {
			report.Execution.Failures = append(append([]Failure(nil), failures...), failureOf(report))
		}
		var failure = failureOf(report)
		if ctx.Err() != nil || report.Committed || errors.Is(err, ErrCommitUncertain) || errors.Is(err, ErrCommitRolledBack) || (failure.Query == "" && failure.Relationship == "") {
			return report, err
		}
		failures = append(failures, failure)
		if failure.Query != "" {
			failedQueries = append(failedQueries, failure.Query)
			// Keep how the step ran in the attempt where it failed (its time
			// and rows). The final attempt leaves it out, so it has neither.
			if detail := report.Execution.query(failure.Query); detail != nil {
				failedRuns[failure.Query] = *detail
			}
		}
		if failure.Relationship != "" {
			failedRelationships = append(failedRelationships, failure.Relationship)
		}
		next := runnable.SetAside(failedQueries, failedRelationships)
		// Every attempt sets aside at least one more step, so this always
		// ends. If it would not, stop rather than repeat the same attempt.
		if len(next) <= len(setAside) {
			return report, err
		}
		setAside = next
	}
}

func failureOf(report Report) Failure {
	if report.Execution == nil || report.Execution.Failure == nil {
		return Failure{}
	}
	return *report.Execution.Failure
}

// markSetAside explains the final, saved attempt in terms of the whole run:
// which steps failed, which were set aside because of them, and how long all
// attempts took together. failedRuns holds each failed step as it was in the
// attempt where it failed.
func markSetAside(report *Report, failures []Failure, failedRuns map[string]QueryReport, setAside []string, started time.Time) {
	var (
		detail = report.Execution
		failed = map[string]Failure{}
		names  []string
	)
	for _, failure := range failures {
		if failure.Query != "" {
			failed[failure.Query] = failure
			names = append(names, failure.Query)
		} else {
			names = append(names, "saving "+SafeName(failure.Relationship, "a relationship type"))
		}
	}
	report.SetAside = setAside
	report.Elapsed = time.Since(started)
	detail.Elapsed = report.Elapsed
	detail.Calculation = "partial"
	detail.Failure = &failures[0]
	detail.Failures = failures
	for _, name := range setAside {
		query := detail.query(name)
		if query == nil {
			continue
		}
		if _, ok := failed[name]; ok {
			// The failure message itself is in Failures. The reason says why
			// the step has no saved result from this run.
			query.State = "failed"
			query.Reason = "It ran in an earlier attempt of this run and failed, so it was set aside and kept its previous results."
			if earlier, ok := failedRuns[name]; ok {
				query.Elapsed, query.Rows = earlier.Elapsed, earlier.Rows
				query.StartedAt, query.FinishedAt = earlier.StartedAt, earlier.FinishedAt
			}
			continue
		}
		query.State = "set-aside"
		query.Reason = "Not run because it uses the results of, or saves the same type as, a step that failed (" + strings.Join(names, ", ") + "). Its previous results were kept."
	}
}
