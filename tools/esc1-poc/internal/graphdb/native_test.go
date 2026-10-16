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

package graphdb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
)

type nativeTestDatabase struct {
	graph.Database
	closeError  error
	closeCalls  int
	commitError error
	queryError  error
	commits     int
	rollbacks   int
	reads       int
	runs        int
	runsAtWrite int
	timeout     time.Duration
}
type nativeTestTransaction struct {
	graph.Transaction
	database *nativeTestDatabase
}
type nativeTestResult struct {
	graph.Result
	rows    runner.Rows
	index   int
	failure error
	keys    []string
}

func (s *nativeTestResult) Next() bool {
	if s.index >= len(s.rows) {
		return false
	}
	s.index++
	return true
}
func (s *nativeTestResult) Keys() []string { return s.keys }
func (s *nativeTestResult) Values() []any {
	var values = make([]any, len(s.keys))
	for index, key := range s.keys {
		values[index] = s.rows[s.index-1][key]
	}
	return values
}
func (s *nativeTestResult) Error() error      { return s.failure }
func (s *nativeTestResult) Close()            {}
func (s *nativeTestTransaction) Close() error { s.database.closeCalls++; return s.database.closeError }
func (s *nativeTestTransaction) Raw(source string, parameters map[string]any) graph.Result {
	if strings.HasPrefix(source, "MERGE") {
		return &nativeTestResult{rows: runner.Rows{{"id": int64(1)}}, keys: []string{"id"}}
	}
	if source == "RETURN 1 AS value" {
		return &nativeTestResult{rows: runner.Rows{{"value": int64(1)}}, keys: []string{"value"}, failure: s.database.queryError}
	}
	return &nativeTestResult{}
}
func (s *nativeTestDatabase) WriteTransaction(ctx context.Context, callback graph.TransactionDelegate, options ...graph.TransactionOption) error {
	s.runsAtWrite = s.runs
	var configuration graph.TransactionConfig
	for _, option := range options {
		option(&configuration)
	}
	s.timeout = configuration.Timeout
	if err := callback(&nativeTestTransaction{database: s}); err != nil {
		s.rollbacks++
		return err
	}
	s.commits++
	return s.commitError
}
func (s *nativeTestDatabase) ReadTransaction(ctx context.Context, callback graph.TransactionDelegate, options ...graph.TransactionOption) error {
	s.reads++
	return callback(&nativeTestTransaction{database: s})
}
func (s *nativeTestDatabase) Run(context.Context, string, map[string]any) error {
	s.runs++
	return nil
}

func nativeTestOptions(t *testing.T) runner.Options {
	return runner.Options{BatchID: "native-test", IngestionComplete: true, WritersPaused: true, TemporaryDirectory: t.TempDir()}
}
func nativeTestPlan() extension.Plan {
	return extension.Plan{Schema: extension.Schema{Name: "native"}}
}

func TestNativeCallbackReportsActualCommitFailure(t *testing.T) {
	var failure = errors.New("commit connection failed")
	for _, commitError := range []error{nil, failure} {
		database := &nativeTestDatabase{commitError: commitError}
		report, err := RunNative(t.Context(), database, "neo4j", nativeTestPlan(), nativeTestOptions(t))
		if commitError == nil {
			if err != nil || !report.Committed || database.commits != 1 {
				t.Fatalf("committed %+v, %v, calls %d", report, err, database.commits)
			}
		} else if report.Committed || report.GraphObjectsChanged != 0 || !errors.Is(err, runner.ErrCommitUncertain) || !errors.Is(err, failure) {
			t.Fatalf("failed commit %+v, %v", report, err)
		}
		if commitError == nil && (report.Execution.Save != "succeeded" || report.Execution.Saved == nil) {
			t.Fatal("callback success was not confirmed in report")
		}
		if commitError != nil && (report.Execution.Save != "uncertain" || report.Execution.Saved != nil || report.Execution.Failure.Phase != "commit") {
			t.Fatal("lost commit confirmation claims saved results")
		}
		if database.timeout <= 0 || database.timeout > runner.DefaultLimits().RunTimeout {
			t.Fatalf("server timeout %s", database.timeout)
		}
	}
}

func TestNativeReadProxyPropagatesLateCursorFailure(t *testing.T) {
	var (
		failure  = errors.New("late cursor failure")
		database = &nativeTestDatabase{queryError: failure}
		reader   = &nativeNeo4jReader{database: database}
		rows     int
	)
	err := reader.Stream(t.Context(), "RETURN 1 AS value", nil, func(row map[string]any) error { rows++; return nil })
	if !errors.Is(err, failure) || rows != 1 || database.reads != 1 {
		t.Fatalf("rows %d, reads %d, error %v", rows, database.reads, err)
	}
}

func TestNativeRejectsWritesAndBackendMismatchBeforeOpeningCallback(t *testing.T) {
	var database = &nativeTestDatabase{}
	plan := nativeTestPlan()
	plan.Queries = []extension.Query{{Name: "bad.cypher", Text: "CREATE (item:User) RETURN item"}}
	report, err := RunNative(t.Context(), database, "neo4j", plan, nativeTestOptions(t))
	if err == nil {
		t.Fatal("accepted writing extension")
	}
	if report.Preparation.QueryValidation <= 0 || report.Preparation.Database != 0 || report.Execution.Elapsed != report.Elapsed {
		t.Fatalf("missing or misleading rejected-file timing: %+v", report)
	}
	for _, backend := range []string{"unknown", "pg"} {
		if _, err := RunNative(t.Context(), database, backend, nativeTestPlan(), nativeTestOptions(t)); err == nil {
			t.Fatalf("accepted %s", backend)
		}
	}
	if database.commits != 0 {
		t.Fatal("opened callback for rejected request")
	}
}

func TestNativeCalculationFailureRollsBackCallback(t *testing.T) {
	var (
		failure  = errors.New("late calculation failure")
		database = &nativeTestDatabase{queryError: failure}
		plan     = nativeTestPlan()
	)
	plan.Queries = []extension.Query{{Name: "result.cypher", Text: "RETURN 1 AS value"}}
	report, err := RunNative(t.Context(), database, "neo4j", plan, nativeTestOptions(t))
	if !errors.Is(err, failure) || report.Committed || database.rollbacks != 1 || database.commits != 0 {
		t.Fatalf("report %+v, error %v, commit/rollback %d/%d", report, err, database.commits, database.rollbacks)
	}
}

func TestNativeEditorTestReturnsTheDiscardSentinelToNeo4j(t *testing.T) {
	var database = &nativeTestDatabase{}
	report, err := TestNative(t.Context(), database, "neo4j", nativeTestPlan(), nativeTestOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.Committed || !report.Tested || !report.Discarded || database.commits != 0 || database.rollbacks != 1 {
		t.Fatalf("report=%+v commits=%d rollbacks=%d", report, database.commits, database.rollbacks)
	}
}

func TestNativeEditorTestPreparesEndpointsBeforeOpeningRollbackTransaction(t *testing.T) {
	var (
		database = &nativeTestDatabase{}
		plan     = nativeTestPlan()
	)
	plan.Schema.Relationships = []extension.RelationshipBinding{{Kind: "PoC_Test", Query: "result.cypher", Endpoints: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}}}
	report, err := TestNative(t.Context(), database, "neo4j", plan, nativeTestOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if database.runs < 2 || database.runsAtWrite != database.runs || report.Preparation.Database <= 0 || !report.Discarded {
		t.Fatalf("preparation runs=%d before write=%d report=%+v", database.runs, database.runsAtWrite, report)
	}
}

func TestNativeEditorTestCalculationFailureRollsBackCallback(t *testing.T) {
	var (
		failure  = errors.New("late calculation failure")
		database = &nativeTestDatabase{queryError: failure}
		plan     = nativeTestPlan()
	)
	plan.Queries = []extension.Query{{Name: "result.cypher", Text: "RETURN 1 AS value"}}
	report, err := TestNative(t.Context(), database, "neo4j", plan, nativeTestOptions(t))
	if !errors.Is(err, failure) || report.Tested || report.Discarded || database.commits != 0 || database.rollbacks != 1 {
		t.Fatalf("report=%+v error=%v commits=%d rollbacks=%d", report, err, database.commits, database.rollbacks)
	}
}

func TestNativeCancelledCommitIsKnownRollback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	transaction := &nativeNeo4jTransaction{}
	if err := transaction.Commit(ctx); !errors.Is(err, runner.ErrCommitRolledBack) || transaction.commitRequested {
		t.Fatalf("cancelled intent %v, requested %v", err, transaction.commitRequested)
	}
}

func TestNativeEditorDiscardFailureIsUncertain(t *testing.T) {
	database := &nativeTestDatabase{closeError: errors.New("close failed")}
	report, err := TestNative(t.Context(), database, "neo4j", nativeTestPlan(), nativeTestOptions(t))
	if !errors.Is(err, runner.ErrTestDiscardUncertain) || report.Tested || report.Discarded || database.commits != 0 || database.closeCalls != 1 {
		t.Fatalf("report=%+v error=%v", report, err)
	}
}
