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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestReportsSeparateCalculationAndSaving(t *testing.T) {
	for _, failure := range []string{"", "query", "commit", "uncertain"} {
		t.Run(failure, func(t *testing.T) {
			var database = fixtureDatabase()
			var selected runner.Database = database
			if failure == "query" {
				database.readErrors[relationshipQuery] = errors.New("division by zero; password=private-secret; objectid=private-object")
			}
			if failure == "commit" || failure == "uncertain" {
				selected = &reportCommitDatabase{Database: database, uncertain: failure == "uncertain"}
			}
			report, err := runner.Run(context.Background(), selected, fixturePlan(), completeBatch())
			execution := report.Execution
			if execution == nil || execution.Elapsed <= 0 || len(execution.Queries) != 2 {
				t.Fatalf("missing details: %+v", execution)
			}
			if execution.Queries[0].Stage != 1 || execution.Queries[1].Stage != 2 {
				t.Fatalf("wrong dependency stages: %+v", execution.Queries)
			}
			for _, query := range execution.Queries {
				if query.Elapsed <= 0 || query.Reason == "" {
					t.Fatalf("missing timing/reason: %+v", query)
				}
			}
			if failure == "" {
				if err != nil || execution.Calculation != "succeeded" || execution.Save != "succeeded" || execution.Saved == nil || execution.Queries[1].Saved == nil || *execution.Queries[1].Saved != 1 {
					t.Fatalf("success: %+v, %v", execution, err)
				}
			} else {
				if err == nil || report.Committed || execution.Saved != nil {
					t.Fatalf("false success: %+v, %v", execution, err)
				}
				for _, query := range execution.Queries {
					if query.Saved != nil {
						t.Fatal("failed run claims saved output")
					}
				}
				if failure == "query" {
					if execution.Failure.Query != "esc1.cypher" || execution.Failure.Stage != 2 || !strings.Contains(execution.Failure.Message, "divided by zero") {
						t.Fatalf("failure: %+v", execution.Failure)
					}
				} else {
					if execution.Calculation != "succeeded" {
						t.Fatal("lost successful calculation")
					}
					expected := "failed"
					if failure == "uncertain" {
						expected = "uncertain"
					}
					if execution.Save != expected {
						t.Fatalf("save state: %+v", execution)
					}
				}
			}
			contents, _ := json.Marshal(execution)
			if strings.Contains(string(contents), "private-secret") || strings.Contains(string(contents), "private-object") || strings.Contains(string(contents), "USER") {
				t.Fatal("stored report contains private values")
			}
		})
	}
}

type reportCommitDatabase struct {
	runner.Database
	uncertain bool
}
type reportCommitTransaction struct {
	runner.Transaction
	uncertain bool
}

func (s *reportCommitDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &reportCommitTransaction{Transaction: transaction, uncertain: s.uncertain}, nil
}
func (s *reportCommitTransaction) Commit(context.Context) error {
	if s.uncertain {
		return errors.New("lost confirmation; password=private-secret")
	}
	return runner.ErrCommitRolledBack
}

func TestReportsExplainSkippedAndDisabledQueries(t *testing.T) {
	var database = fixtureDatabase()
	var plan = fixturePlan()
	if _, err := runner.Run(context.Background(), database, plan, completeBatch()); err != nil {
		t.Fatal(err)
	}
	options := completeBatch()
	options.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"Computer"}}
	skipped, err := runner.Run(context.Background(), database, plan, options)
	if err != nil || !skipped.Skipped || skipped.Execution.Save != "unchanged" {
		t.Fatalf("skip: %+v, %v", skipped, err)
	}
	for _, query := range skipped.Execution.Queries {
		if query.State != "skipped" || query.Rows != nil || query.Reason == "" {
			t.Fatalf("skip explanation: %+v", query)
		}
	}
	plan.Queries[1].DisabledReason = "Disabled by the author."
	disabled, err := runner.Run(context.Background(), database, plan, completeBatch())
	if err != nil || disabled.Execution.Queries[1].State != "disabled" {
		t.Fatalf("disabled query: %+v, %v", disabled.Execution, err)
	}
}

func TestBroadInputsPreventUnsafeSkipping(t *testing.T) {
	var database = fixtureDatabase()
	var plan = fixturePlan()
	plan.Queries[1].BroadRead = true
	if _, err := runner.Run(t.Context(), database, plan, completeBatch()); err != nil {
		t.Fatal(err)
	}
	options := completeBatch()
	options.Changes = runner.Changes{CoverageKnown: true, Nodes: []string{"OtherKind"}}
	result, err := runner.Run(t.Context(), database, plan, options)
	if err != nil || result.Skipped || !strings.Contains(result.Execution.Queries[1].Reason, "conservative") {
		t.Fatalf("unsafe skip: %+v, %v", result.Execution, err)
	}
	options.Changes = runner.Changes{CoverageKnown: true}
	result, err = runner.Run(t.Context(), database, plan, options)
	if err != nil || !result.Skipped {
		t.Fatalf("known unchanged graph: %+v, %v", result, err)
	}
}

// Failures and a true skip must report only the steps that were attempted.
func TestPreparationTimesOnIdentityFailureAndSkip(t *testing.T) {
	var (
		database = fixtureDatabase()
		plan     = fixturePlan()
		settings = completeBatch()
	)
	if _, err := runner.Run(t.Context(), database, plan, settings); err != nil {
		t.Fatal(err)
	}
	settings.Changes = runner.Changes{CoverageKnown: true}
	skipped, err := runner.Run(t.Context(), database, plan, settings)
	if err != nil || !skipped.Skipped || skipped.Preparation.Selection <= 0 || skipped.Preparation.Identities != 0 {
		t.Fatalf("skip timings: %+v, %v", skipped, err)
	}
	settings.Changes = runner.Changes{}
	database.readErrors[identityQuery] = errors.New("identity read failed")
	failed, err := runner.Run(t.Context(), database, plan, settings)
	if err == nil || failed.Committed || failed.Preparation.Identities <= 0 || failed.CalculationElapsed != 0 || failed.SavingElapsed != 0 {
		t.Fatalf("failed identity timings: %+v, %v", failed, err)
	}
	for _, report := range []runner.Report{skipped, failed} {
		var sum time.Duration
		for _, elapsed := range []time.Duration{report.Preparation.Database, report.Preparation.Transaction, report.Preparation.Selection, report.Preparation.Identities} {
			sum += elapsed
		}
		if sum > report.PreparationElapsed {
			t.Fatalf("overlapping preparation times: %s > %s", sum, report.PreparationElapsed)
		}
	}
}
