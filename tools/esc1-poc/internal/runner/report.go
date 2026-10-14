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

// Execution contains only rule names, counts, timings, and fixed explanations.
// It can be stored in the rule service without copying any graph answers.
type Execution struct {
	CleanupPending bool          `json:"cleanup_pending,omitempty"`
	SchemaRevision string        `json:"schema_revision,omitempty"`
	Calculation    string        `json:"calculation"`
	Save           string        `json:"save"`
	Phase          string        `json:"phase"`
	Failure        *Failure      `json:"failure,omitempty"`
	Queries        []QueryReport `json:"queries"`
	Proposed       ChangesCount  `json:"proposed"`
	Saved          *ChangesCount `json:"saved,omitempty"`
	Elapsed        time.Duration `json:"elapsed_nanoseconds"`
}

type ChangesCount struct {
	Added     int `json:"relationships_added"`
	Removed   int `json:"relationships_removed"`
	Updated   int `json:"relationships_updated"`
	Unchanged int `json:"relationships_unchanged"`
	Helpers   int `json:"helpers_changed"`
}

// A missing count means unknown or not applicable; zero means a complete
// empty answer. Saved outputs describe this query's contribution, including
// connections that already existed, rather than the number of new writes.
type QueryReport struct {
	SchemaPaused  bool          `json:"schema_paused,omitempty"`
	Version       string        `json:"version,omitempty"`
	SchemaVersion string        `json:"schema_version,omitempty"`
	Name          string        `json:"name"`
	Stage         int           `json:"stage"`
	State         string        `json:"state"`
	Reason        string        `json:"reason"`
	Rows          *int64        `json:"rows,omitempty"`
	Elapsed       time.Duration `json:"elapsed_nanoseconds"`
	Proposed      *int64        `json:"proposed_outputs,omitempty"`
	Saved         *int64        `json:"saved_outputs,omitempty"`
}

type Failure struct {
	Query   string `json:"query,omitempty"`
	Stage   int    `json:"stage,omitempty"`
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

// Raw database errors may contain a password, query literal, or collected ID.
// Keep them in the returned error for existing private logging; stored reports
// use these explanations instead. Never try to redact an unknown error by guessing.
func FailureMessage(err error) string {
	switch {
	case errors.Is(err, ErrCommitUncertain):
		return "The database did not confirm whether results were saved. Inspect the saved run before retrying."
	case errors.Is(err, ErrCommitRolledBack):
		return "The database confirmed that saving was rolled back. Previous results were kept."
	case errors.Is(err, context.DeadlineExceeded):
		return "The time limit was reached. Reduce the query's work or review its limits."
	case errors.Is(err, context.Canceled):
		return "The run was canceled before completion."
	}
	var cause = err
	for errors.Unwrap(cause) != nil {
		cause = errors.Unwrap(cause)
	}
	var text = strings.ToLower(cause.Error())
	// Match only known categories, and never copy text from the error.
	for _, item := range [][2]string{
		{"conflicting relationship properties", "Rows disagree about properties for the same connection. Return one agreed property map per connection."},
		{"endpoint", "A proposed connection has a missing, ambiguous, or disallowed endpoint. Check compatible_kinds and the source and target columns."},
		{"missing, empty, or duplicate objectids", "Graph objects have missing or duplicate object IDs. Correct their identities before running again."},
		{"helper", "A helper answer has invalid properties or identity. Check its id, kinds, properties, and required ID prefix."},
		{"budget", "The run exceeded a configured resource limit. Reduce its result size or review the execution limits."},
		{"limit", "The run exceeded a configured limit. Review the query and execution limits."},
		{"circular", "The calculation found a circular membership or trust path it cannot process."},
		{"differs from native", "The calculated ESC1 connections differ from BloodHound's native answer. No results were saved."},
		{"each relationship row", "A relationship answer needs source, target, kind, and optionally properties. Check the final RETURN."},
		{"column", "The returned columns do not match the required answer shape. Check the final RETURN."},
		{"properties", "The returned properties do not match the supported property format."},
		{"kind", "A returned kind does not match the query's declared output."},
		{"division by zero", "The query divided by zero. Check its arithmetic and input values."},
		{"divide by zero", "The query divided by zero. Check its arithmetic and input values."},
		{"syntax", "The database rejected the query syntax. Check the query against the selected database's supported language."},
		{"not supported", "The query uses a feature that this execution path does not support."},
	} {
		if strings.Contains(text, item[0]) {
			return item[1]
		}
	}
	return "This step failed. Check its query and inputs; private application logs contain the database details."
}

func newExecution(plan extension.Plan) *Execution {
	var result = &Execution{CleanupPending: plan.CleanupPending, SchemaRevision: plan.SchemaRevision, Calculation: "not-started", Save: "not-started", Phase: "preparation", Queries: []QueryReport{}}
	var stages = map[string]int{}
	for _, query := range plan.Queries {
		stage := 1
		for _, name := range plan.Prerequisites(query) {
			stage = max(stage, stages[name]+1)
		}
		stages[query.Name] = stage
		detail := QueryReport{SchemaPaused: query.SchemaPaused, Version: query.Version, SchemaVersion: query.SchemaVersion, Name: query.Name, Stage: stage, State: "waiting", Reason: "Waiting for this run to reach the query."}
		if query.DisabledReason != "" {
			detail.State = "disabled"
			detail.Reason = query.DisabledReason
		}
		result.Queries = append(result.Queries, detail)
	}
	return result
}

func (s *Execution) query(name string) *QueryReport {
	for index := range s.Queries {
		if s.Queries[index].Name == name {
			return &s.Queries[index]
		}
	}
	return nil
}

// Selection happens before reading. Explain both the selected chain and any
// rules that are skipped, without claiming which goroutines ran together.
func (s *Execution) selectQueries(plan extension.Plan, reason string) {
	var (
		selected = map[string]bool{}
		broad    = map[string]bool{}
	)
	for _, query := range plan.Queries {
		selected[query.Name] = true
		broad[query.Name] = query.BroadRead
	}
	for index := range s.Queries {
		detail := &s.Queries[index]
		if detail.State == "disabled" {
			continue
		}
		if selected[detail.Name] {
			detail.Reason = reason
			if broad[detail.Name] {
				detail.Reason = "Graph reads without named kinds require conservative recalculation, along with their connected calculations."
			}
		} else {
			detail.State = "skipped"
			detail.Reason = "No declared input or connected calculation was affected by the confirmed changes."
		}
	}
}

func (s *Execution) fail(query, phase string, err error) {
	var failure = &Failure{Query: query, Phase: phase, Message: FailureMessage(err)}
	if detail := s.query(query); detail != nil {
		failure.Stage = detail.Stage
		detail.State = "failed"
	}
	s.Failure = failure
}

func finishReport(report *Report, started time.Time, err error) {
	var detail = report.Execution
	report.Elapsed = time.Since(started)
	detail.Elapsed = report.Elapsed
	detail.Proposed = ChangesCount{report.Added, report.Removed, report.Updated, report.Unchanged, report.HelpersChanged}
	if report.Committed {
		detail.Calculation, detail.Save, detail.Phase = "succeeded", "succeeded", "complete"
		saved := detail.Proposed
		detail.Saved = &saved
		for index := range detail.Queries {
			query := &detail.Queries[index]
			if query.Proposed != nil {
				count := *query.Proposed
				query.Saved = &count
			}
		}
	} else if report.Skipped {
		detail.Calculation, detail.Save, detail.Phase = "skipped", "unchanged", "complete"
	}
	if err != nil {
		if detail.Calculation != "succeeded" {
			detail.Calculation = "failed"
		}
		if detail.Save == "saving" {
			detail.Save = "failed"
		}
		if errors.Is(err, ErrCommitUncertain) {
			detail.Save = "uncertain"
		}
		if detail.Failure == nil {
			detail.fail("", detail.Phase, err)
		}
		for index := range detail.Queries {
			query := &detail.Queries[index]
			if query.State == "waiting" {
				query.State = "skipped"
				query.Reason = "The run stopped before this query could start."
			}
			if query.State == "running" {
				query.State = "canceled"
				query.Reason = "The run stopped before this query completed."
			}
		}
	}
}

// ConfirmNativeSave is used when BloodHound owns the surrounding transaction.
// Its callback finishing is not yet a database confirmation.
func ConfirmNativeSave(report *Report, err error) {
	if report.Execution == nil {
		return
	}
	report.Execution.Saved = nil
	for index := range report.Execution.Queries {
		report.Execution.Queries[index].Saved = nil
	}
	if err != nil {
		report.Execution.Save = "saving"
		report.Execution.Phase = "commit"
		report.Execution.Failure = nil
	}
	finishReport(report, time.Now().Add(-report.Elapsed), err)
}

// RejectedQueryReport identifies a file rejected before any database work.
func RejectedQueryReport(plan extension.Plan, options Options, name string, err error) Report {
	var report = Report{BatchID: options.BatchID, Execution: newExecution(plan)}
	report.Execution.Phase = "language-check"
	report.Execution.fail(name, "language-check", err)
	finishReport(&report, time.Now(), err)
	return report
}
