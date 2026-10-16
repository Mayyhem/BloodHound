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

package postprocessing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
)

type serviceTestClient struct {
	bundle        ruleservice.Bundle
	failure       error
	fetches       int
	statuses      []ruleservice.Status
	reportFailure error
}

func (s *serviceTestClient) Fetch(context.Context) (ruleservice.Bundle, error) {
	s.fetches++
	return s.bundle, s.failure
}
func (s *serviceTestClient) Report(_ context.Context, status ruleservice.Status) error {
	if s.reportFailure != nil {
		return s.reportFailure
	}
	s.statuses = append(s.statuses, status)
	return nil
}

func testRuleBundle(t *testing.T) ruleservice.Bundle {
	t.Helper()
	var files = map[string]string{}
	entries, err := os.ReadDir("../extension")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "schema.json" && !strings.HasSuffix(entry.Name(), ".cypher") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join("../extension", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(contents)
	}
	store, err := ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Update(files)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func newServiceTest(t *testing.T) (*Service, *serviceTestClient, *time.Time, *int) {
	t.Helper()
	var (
		now     = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
		client  = &serviceTestClient{bundle: testRuleBundle(t)}
		calls   int
		service = &Service{gate: NewGate(), client: client, clock: func() time.Time { return now }, configuration: Configuration{Directory: t.TempDir()}}
	)
	installed := client.bundle.Schemas
	service.configuration.ReadSchemas = func(context.Context) ([]byte, error) {
		return json.Marshal(ruleservice.SchemaSnapshot{Definitions: installed})
	}
	service.execute = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		calls++
		if !options.IngestionComplete || !options.WritersPaused || options.BatchID == "" {
			t.Fatal("missing completion/write protection")
		}
		_, release, err := service.gate.EnterWrite(ctx)
		if err != nil {
			t.Fatal(err)
		}
		release()
		return runner.Report{Committed: true, Added: 1}, nil
	}
	return service, client, &now, &calls
}

func TestServiceOnlyRunsAfterSuccessfulAnalysis(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	ctx := context.Background()
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if client.fetches != 0 {
		t.Fatal("idle startup fetched rules")
	}
	if committed, err := service.AfterAnalysis(ctx); err != nil || !committed {
		t.Fatalf("first run %v %v", committed, err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatal("idle tick recalculated completed work")
	}
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || client.fetches != 2 {
		t.Fatal("next analysis did not fetch active rules")
	}
}

func TestServiceRecoversRegistryOutageWithoutDiscardingOldResults(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	ctx := context.Background()
	client.failure = errors.New("container down")
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("outage hidden")
	}
	if *calls != 0 {
		t.Fatal("outage ran calculation")
	}
	client.failure = nil
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatal("retry ignored backoff")
	}
	*now = now.Add(time.Minute)
	if committed, err := service.Retry(ctx); err != nil || !committed {
		t.Fatalf("retry %v %v", committed, err)
	}
	if *calls != 1 {
		t.Fatal("failed to recover")
	}
}

func TestServiceRetriesPinnedRulesAndWaitsForNewAnalysisAfterGraphChanges(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	ctx := context.Background()
	original := service.execute
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{}, errors.New("calculation failed")
	}
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	revision := service.pending.bundle.Revision
	client.bundle = ruleservice.Bundle{Revision: "invalid replacement"}
	service.execute = original
	*now = now.Add(time.Minute)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if client.fetches != 1 || *calls != 1 || client.statuses[len(client.statuses)-1].Revision != revision {
		t.Fatal("retry changed pinned rules")
	}

	client.bundle = testRuleBundle(t)
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{}, errors.New("failed again")
	}
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	_, release, err := service.gate.EnterWrite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	*now = now.Add(time.Minute)
	service.execute = original
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || service.pending != nil {
		t.Fatal("retried against changed evidence")
	}
	// New data is not a failure: the report says the next analysis will run the rules.
	if last := client.statuses[len(client.statuses)-1]; last.State != "waiting" || last.Message != waitingMessage {
		t.Fatalf("changed evidence reported as %q: %s", last.State, last.Message)
	}
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatal("new successful analysis failed to restore processing")
	}
}

func TestServiceUncertainCommitBlocksFurtherWrites(t *testing.T) {
	service, _, _, _ := newServiceTest(t)
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{}, runner.ErrCommitUncertain
	}
	if committed, err := service.AfterAnalysis(context.Background()); committed || !errors.Is(err, runner.ErrCommitUncertain) {
		t.Fatalf("uncertain commit %v %v", committed, err)
	}
	if _, _, err := service.gate.EnterWrite(context.Background()); err == nil {
		t.Fatal("allowed write after uncertain commit")
	}
}

func TestServiceNoRulesWaitsForNextNativeAnalysis(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	client.failure = ruleservice.ErrNoActive
	if committed, err := service.AfterAnalysis(context.Background()); committed || err != nil {
		t.Fatalf("no rules %v %v", committed, err)
	}
	client.failure = nil
	*now = now.Add(time.Minute)
	if _, err := service.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatal("upload triggered processing without a new native analysis")
	}
}

func TestServiceNativeAnalysisAndExtensionShareOneTurn(t *testing.T) {
	service, _, _, calls := newServiceTest(t)
	var ordinaryError error
	err := service.WithAnalysis(context.Background(), func(ctx context.Context) error {
		// Ordinary writes wait across the boundary between the native analysis
		// and rule fetch. A child using the supplied context can still write.
		waiting, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, _, ordinaryError = service.gate.EnterWrite(waiting)
		_, release, err := service.gate.EnterWrite(ctx)
		if err != nil {
			return err
		}
		release()
		_, err = service.AfterAnalysis(ctx)
		return err
	})
	if err != nil || !errors.Is(ordinaryError, context.DeadlineExceeded) || *calls != 1 {
		t.Fatalf("shared turn: %v, ordinary: %v, calls: %d", err, ordinaryError, *calls)
	}
	_, release, err := service.gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestServiceFailedNativeAnalysisInvalidatesPendingRetry(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	client.failure = errors.New("registry down")
	if _, err := service.AfterAnalysis(context.Background()); err == nil {
		t.Fatal("missing outage")
	}
	failure := errors.New("native analysis partially changed graph then failed")
	if err := service.WithAnalysis(context.Background(), func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	client.failure = nil
	*now = now.Add(time.Minute)
	if _, err := service.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 || service.pending != nil {
		t.Fatal("ran extension against failed native analysis")
	}
}

func TestServiceReadsInstalledSchemaBeforeEveryRun(t *testing.T) {
	service, _, now, calls := newServiceTest(t)
	var reads int
	original := service.configuration.ReadSchemas
	service.configuration.ReadSchemas = func(ctx context.Context) ([]byte, error) {
		reads++
		_, release, err := service.gate.EnterWrite(ctx)
		if err != nil {
			t.Fatal(err)
		}
		release()
		return original(ctx)
	}
	if _, err := service.AfterAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if _, err := service.AfterAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || *calls != 2 {
		t.Fatalf("schema reads %d runs %d", reads, *calls)
	}
	service.configuration.ReadSchemas = func(context.Context) ([]byte, error) { return nil, errors.New("schema read failed") }
	if _, err := service.AfterAnalysis(t.Context()); err == nil {
		t.Fatal("accepted failed schema read")
	}
	if *calls != 2 {
		t.Fatal("published without installed schema check")
	}
}

func TestServiceKnownRollbackKeepsOrdinaryWritesAvailable(t *testing.T) {
	service, _, _, _ := newServiceTest(t)
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{}, runner.ErrCommitRolledBack
	}
	if committed, err := service.AfterAnalysis(context.Background()); committed || !errors.Is(err, runner.ErrCommitRolledBack) {
		t.Fatalf("known rollback: %v %v", committed, err)
	}
	_, release, err := service.gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatalf("a confirmed rollback unnecessarily stopped ordinary writes: %v", err)
	}
	release()
}

func TestServiceRetriesReportDeliveryWithoutRepeatingSavedWork(t *testing.T) {
	service, client, _, calls := newServiceTest(t)
	client.reportFailure = errors.New("report service unavailable")
	if committed, err := service.AfterAnalysis(t.Context()); err != nil || !committed {
		t.Fatalf("graph save: %v %v", committed, err)
	}
	if len(client.statuses) != 0 || len(service.reports) != 2 {
		t.Fatal("lost unsent reports")
	}
	client.reportFailure = nil
	if _, err := service.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || len(client.statuses) != 2 || client.statuses[1].State != "success" || len(service.reports) != 0 {
		t.Fatal("report retry recalculated or lost success")
	}
}

func TestSchemaDeletionWaitsForRunningCalculationAndPreventsOldRetry(t *testing.T) {
	var (
		service, _, _, _ = newServiceTest(t)
		reading          = make(chan struct{})
		finish           = make(chan struct{})
		runDone          = make(chan error, 1)
		changeDone       = make(chan error, 1)
	)
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		close(reading)
		<-finish
		return runner.Report{Committed: true}, nil
	}
	go func() { _, err := service.AfterAnalysis(context.Background()); runDone <- err }()
	<-reading
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := service.WithSchemaChange(ctx, func(context.Context) error {
		t.Error("schema changed while the old calculation could still publish")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deletion did not wait: %v", err)
	}
	go func() {
		changeDone <- service.WithSchemaChange(context.Background(), func(context.Context) error { return nil })
	}()
	close(finish)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if err := <-changeDone; err != nil {
		t.Fatal(err)
	}
	if service.changes.Snapshot().CoverageKnown {
		t.Fatal("schema change did not require fresh calculation")
	}
}

// Setup installs the editor, the ordinary analysis executor, and writer
// coordination. No database connection is needed: this test checks setup and
// the existing service execution path, while real database behavior is
// covered separately.
func TestNewServiceInstallsEditorAndOrdinaryAnalysis(t *testing.T) {
	var (
		database      = graph.NewDatabaseSwitch(t.Context(), nil)
		configuration = Configuration{URL: "http://127.0.0.1:18081", Token: strings.Repeat("x", 32), Directory: t.TempDir(), Backend: "neo4j"}
	)
	service, err := New(configuration, database)
	if err != nil || service == nil {
		t.Fatalf("ordinary setup failed: %v", err)
	}
	if service.editor == nil || service.execute == nil || service.executeTest == nil {
		t.Fatal("expected configured editor, ordinary executor, and editor test executor")
	}
	before := service.gate.Version()
	_, release, err := graph.EnterWriteAccess(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if service.gate.Version() == before {
		t.Fatal("ordinary writer coordination was not installed")
	}
	ordinary, client, _, _ := newServiceTest(t)
	service.client = client
	service.configuration.ReadSchemas = ordinary.configuration.ReadSchemas
	called := false
	service.execute = func(_ context.Context, _ extension.Plan, _ runner.Options) (runner.Report, error) {
		called = true
		return runner.Report{Committed: true}, nil
	}
	if _, err := service.AfterAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ordinary analysis did not execute")
	}
}

func TestServiceRetriesAFailedRunOnceThenStops(t *testing.T) {
	service, client, now, _ := newServiceTest(t)
	ctx := context.Background()
	attempts := 0
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		attempts++
		return runner.Report{}, errors.New("the same rule problem every time")
	}
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	if last := client.statuses[len(client.statuses)-1]; last.State != "failed" || !strings.HasSuffix(last.Message, "It will be tried once more in about a minute.") {
		t.Fatalf("first failure: %q %s", last.State, last.Message)
	}
	*now = now.Add(time.Minute)
	if _, err := service.Retry(ctx); err == nil {
		t.Fatal("retry failure hidden")
	}
	if last := client.statuses[len(client.statuses)-1]; !strings.Contains(last.Message, "Automatic retries have stopped") {
		t.Fatalf("second failure: %s", last.Message)
	}
	if service.pending != nil {
		t.Fatal("a third automatic attempt is still scheduled")
	}
	*now = now.Add(time.Hour)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected the first try and one retry, got %d attempts", attempts)
	}
	// A new BloodHound analysis starts again with a fresh retry allowance.
	if _, err := service.AfterAnalysis(ctx); err == nil || attempts != 3 {
		t.Fatalf("new analysis did not run the rules: %v, %d attempts", err, attempts)
	}
}

// One step fails. The steps tied to it are set aside, everything else is
// saved, and the run is reported as "partial". The one retry asks for no
// set-aside steps up front; the runner itself selects the unfinished ones.
func TestServiceReportsAPartlySavedRunAndRetriesOnce(t *testing.T) {
	service, client, now, _ := newServiceTest(t)
	ctx := context.Background()
	attempts := 0
	service.execute = func(_ context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		attempts++
		var (
			execution = &runner.Execution{Calculation: "failed", Save: "not-started"}
			setAside  = map[string]bool{}
		)
		for _, name := range options.SetAside {
			setAside[name] = true
		}
		for _, query := range plan.Queries {
			state := "succeeded"
			if setAside[query.Name] {
				state = "skipped"
			}
			execution.Queries = append(execution.Queries, runner.QueryReport{Name: query.Name, Stage: 1, State: state})
		}
		if len(options.SetAside) == 0 {
			// The first step of the run fails.
			execution.Failure = &runner.Failure{Query: plan.Queries[0].Name, Phase: "check-answers", Message: "The name property is empty."}
			execution.Queries[0].State = "failed"
			return runner.Report{Execution: execution}, errors.New("injected step failure")
		}
		execution.Calculation, execution.Save = "succeeded", "succeeded"
		return runner.Report{Committed: true, Added: 3, SetAside: options.SetAside, Execution: execution}, nil
	}
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	last := client.statuses[len(client.statuses)-1]
	if last.State != "partial" || !strings.HasPrefix(last.Message, "Finished with 1 failure: ") || !strings.Contains(last.Message, "kept their previous results. 3 connections added") || !strings.HasSuffix(last.Message, "The set-aside steps will be tried once more in about a minute.") {
		t.Fatalf("first run: %q %s", last.State, last.Message)
	}
	if last.Execution.Calculation != "partial" || service.pending == nil || attempts != 2 {
		t.Fatalf("expected a partial save with one retry pending: %+v attempts=%d", last.Execution, attempts)
	}
	*now = now.Add(time.Minute)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if last := client.statuses[len(client.statuses)-1]; last.State != "partial" || !strings.Contains(last.Message, "Automatic retries have stopped; the set-aside steps run again") {
		t.Fatalf("retry: %q %s", last.State, last.Message)
	}
	if service.pending != nil {
		t.Fatal("a third automatic attempt is still scheduled")
	}
}
