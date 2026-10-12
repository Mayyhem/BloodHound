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
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// The shared fixture records reads and writes without locks. This adapter locks
// that state while allowing controlled read operations to overlap outside it.
type concurrentDatabase struct {
	*fakeDatabase
	mutex          sync.Mutex
	read           func(context.Context, *fakeTransaction, string, map[string]any) (runner.Rows, bool, error)
	beforeRollback func(*fakeTransaction)
}
type concurrentTransaction struct {
	transaction *fakeTransaction
	database    *concurrentDatabase
}

func (s *concurrentDatabase) SupportsParallelReads() bool { return true }
func (s *concurrentDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	transaction, err := s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &concurrentTransaction{transaction: transaction.(*fakeTransaction), database: s}, nil
}
func (s *concurrentTransaction) Read(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	if s.database.read != nil {
		rows, handled, err := s.database.read(ctx, s.transaction, query, parameters)
		if handled {
			return rows, err
		}
	}
	s.database.mutex.Lock()
	defer s.database.mutex.Unlock()
	return s.transaction.Read(ctx, query, parameters)
}
func (s *concurrentTransaction) Write(ctx context.Context, query string, parameters map[string]any) (runner.Rows, error) {
	s.database.mutex.Lock()
	defer s.database.mutex.Unlock()
	return s.transaction.Write(ctx, query, parameters)
}
func (s *concurrentTransaction) Commit(ctx context.Context) error {
	s.database.mutex.Lock()
	defer s.database.mutex.Unlock()
	return s.transaction.Commit(ctx)
}
func (s *concurrentTransaction) Rollback(ctx context.Context) error {
	if s.database.beforeRollback != nil {
		s.database.beforeRollback(s.transaction)
	}
	s.database.mutex.Lock()
	defer s.database.mutex.Unlock()
	return s.transaction.Rollback(ctx)
}

func TestHelperWaitsForIndependentReaderToClose(t *testing.T) {
	var (
		database        = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan            = fixturePlan()
		started         = make(chan string, 2)
		releaseRollback = make(chan struct{})
		done            = make(chan error, 1)
		readerMutex     sync.Mutex
		independent     *fakeTransaction
		readerClosed    bool
		options         = parallelOptions(t)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan.Queries = append([]extension.Query{{Name: "independent.cypher", Parameter: "independent", Text: "independent"}}, plan.Queries...)
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		switch query {
		case "independent":
			readerMutex.Lock()
			independent = transaction
			readerMutex.Unlock()
			return nil, true, nil
		case helperQuery:
			readerMutex.Lock()
			closed := readerClosed
			readerMutex.Unlock()
			if !closed {
				return nil, true, fmt.Errorf("helper started before the independent reader closed")
			}
			started <- "helper"
		}
		return nil, false, nil
	}
	database.beforeRollback = func(transaction *fakeTransaction) {
		readerMutex.Lock()
		isIndependent := transaction == independent
		readerMutex.Unlock()
		if !isIndependent {
			return
		}
		started <- "closing"
		// A query can finish returning rows before its database connection
		// closes. Keep that close unfinished until the test releases it.
		select {
		case <-releaseRollback:
		case <-ctx.Done():
		}
		readerMutex.Lock()
		readerClosed = true
		readerMutex.Unlock()
	}
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	if event := awaitRead(t, ctx, started); event != "closing" {
		t.Fatalf("first event = %q, want closing", event)
	}
	close(releaseRollback)
	if event := awaitRead(t, ctx, started); event != "helper" {
		t.Fatalf("next event = %q, want helper", event)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func parallelOptions(t *testing.T) runner.Options {
	options := completeBatch()
	options.Limits = runner.DefaultLimits()
	options.Limits.Workers = 2
	options.TemporaryDirectory = t.TempDir()
	return options
}
func awaitRead(t *testing.T, ctx context.Context, events <-chan string) string {
	t.Helper()
	select {
	case value := <-events:
		return value
	case <-ctx.Done():
		t.Fatal("reader did not reach the expected barrier")
		return ""
	}
}

func TestDefaultFiveReadersStartNextQueryAsSoonAsOneFinishes(t *testing.T) {
	var (
		database        = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan            = fixturePlan()
		started         = make(chan string, 6)
		releases        = map[string]chan struct{}{}
		done            = make(chan error, 1)
		mutex           sync.Mutex
		active, maximum int
		options         = parallelOptions(t)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	options.Limits = runner.DefaultLimits()
	if options.Limits.Workers != 5 {
		t.Fatalf("default reader limit = %d, want 5", options.Limits.Workers)
	}
	for index := range 6 {
		name := fmt.Sprintf("independent-%d", index)
		releases[name] = make(chan struct{})
		plan.Queries = append([]extension.Query{{Name: name + ".cypher", Parameter: name, Text: name}}, plan.Queries...)
	}
	database.read = func(ctx context.Context, _ *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		release, controlled := releases[query]
		if !controlled {
			return nil, false, nil
		}
		mutex.Lock()
		active++
		maximum = max(maximum, active)
		mutex.Unlock()
		started <- query
		select {
		case <-release:
		case <-ctx.Done():
			return nil, true, ctx.Err()
		}
		mutex.Lock()
		active--
		mutex.Unlock()
		return nil, true, nil
	}
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	first := awaitRead(t, ctx, started)
	for range 4 {
		awaitRead(t, ctx, started)
	}
	// Four readers remain blocked. The sixth query must use the freed place
	// without waiting for those four to finish.
	close(releases[first])
	awaitRead(t, ctx, started)
	for name, release := range releases {
		if name != first {
			close(release)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maximum != 5 {
		t.Fatalf("maximum simultaneous reads = %d, want 5", maximum)
	}
}

func TestWorkerLimitRejectsMoreThanFiveBeforeOpeningDatabase(t *testing.T) {
	var (
		database = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		options  = parallelOptions(t)
	)
	options.Limits.Workers = 6
	if _, err := runner.Run(t.Context(), database, fixturePlan(), options); err == nil {
		t.Fatal("accepted six simultaneous readers")
	}
	if database.begins != 0 {
		t.Fatalf("opened %d transactions before rejecting the reader limit", database.begins)
	}
}

func TestReadyDependentQueryDoesNotWaitForUnrelatedSlowQuery(t *testing.T) {
	var (
		database    = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan        = fixturePlan()
		started     = make(chan string, 3)
		fastRelease = make(chan struct{})
		slowRelease = make(chan struct{})
		done        = make(chan error, 1)
		options     = parallelOptions(t)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan.Queries = append([]extension.Query{
		{Name: "fast.cypher", Parameter: "fast", Text: "fast"},
		{Name: "slow.cypher", Parameter: "slow", Text: "slow"},
		{Name: "dependent.cypher", Parameter: "dependent", Text: "dependent", Dependencies: []extension.Dependency{{Type: "query", Name: "fast.cypher"}}},
	}, plan.Queries...)
	database.read = func(ctx context.Context, _ *fakeTransaction, query string, parameters map[string]any) (runner.Rows, bool, error) {
		var release <-chan struct{}
		switch query {
		case "fast":
			release = fastRelease
		case "slow":
			release = slowRelease
		case "dependent":
			if !reflect.DeepEqual(parameters["fast"], []map[string]any{{"value": "ready"}}) {
				return nil, true, fmt.Errorf("dependent query received incorrect prerequisite: %v", parameters)
			}
			started <- query
			return nil, true, nil
		default:
			return nil, false, nil
		}
		started <- query
		select {
		case <-release:
			return runner.Rows{{"value": "ready"}}, true, nil
		case <-ctx.Done():
			return nil, true, ctx.Err()
		}
	}
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	awaitRead(t, ctx, started)
	awaitRead(t, ctx, started)
	// Only the fast prerequisite finishes. Its dependent query can now run,
	// even though the unrelated slow query still occupies the other place.
	close(fastRelease)
	if query := awaitRead(t, ctx, started); query != "dependent" {
		t.Fatalf("next query = %q, want dependent", query)
	}
	close(slowRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIndependentReadsAreBoundedAndSharedPrerequisiteRunsOnce(t *testing.T) {
	var (
		database        = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan            = fixturePlan()
		started         = make(chan string, 8)
		release         = make(chan struct{})
		calls           = map[string]int{}
		active, maximum int
		mutex           sync.Mutex
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, name := range []string{"left", "right", "third"} {
		plan.Queries = append([]extension.Query{{Name: name + ".cypher", Parameter: name, Text: name, Dependencies: []extension.Dependency{{Type: "query", Name: "seed.cypher"}}}}, plan.Queries...)
	}
	plan.Queries = append([]extension.Query{{Name: "seed.cypher", Parameter: "seed", Text: "seed"}}, plan.Queries...)
	database.read = func(ctx context.Context, _ *fakeTransaction, query string, parameters map[string]any) (runner.Rows, bool, error) {
		if query != "seed" && query != "left" && query != "right" && query != "third" {
			return nil, false, nil
		}
		mutex.Lock()
		calls[query]++
		mutex.Unlock()
		if query == "seed" {
			return runner.Rows{{"value": "shared"}}, true, nil
		}
		if !reflect.DeepEqual(parameters["seed"], []map[string]any{{"value": "shared"}}) {
			return nil, true, fmt.Errorf("shared prerequisite missing: %v", parameters)
		}
		mutex.Lock()
		active++
		maximum = max(maximum, active)
		mutex.Unlock()
		started <- query
		select {
		case <-release:
		case <-ctx.Done():
			return nil, true, ctx.Err()
		}
		mutex.Lock()
		active--
		mutex.Unlock()
		return nil, true, nil
	}
	done := make(chan error, 1)
	options := parallelOptions(t)
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	awaitRead(t, ctx, started)
	awaitRead(t, ctx, started)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if maximum != 2 || calls["seed"] != 1 || calls["left"] != 1 || calls["right"] != 1 || calls["third"] != 1 {
		t.Fatalf("unexpected scheduling: maximum=%d calls=%v", maximum, calls)
	}
}

func TestFailedReadCancelsAndWaitsForOtherReadersBeforeRollback(t *testing.T) {
	var (
		database    = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan        = fixturePlan()
		peerStarted = make(chan struct{})
		cancelled   = make(chan string, 1)
		dependent   = make(chan string, 1)
		release     = make(chan struct{})
		done        = make(chan error, 1)
		before      = cloneGraph(*database.graph)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// Put the canceled peer first so its error cannot hide the later-listed
	// query's actual failure when the coordinator collects completed readers.
	plan.Queries = append([]extension.Query{
		{Name: "peer.cypher", Parameter: "peer", Text: "peer"},
		{Name: "fail.cypher", Parameter: "fail", Text: "fail"},
		{Name: "dependent.cypher", Parameter: "dependent", Text: "dependent", Dependencies: []extension.Dependency{{Type: "query", Name: "fail.cypher"}}},
	}, plan.Queries...)
	database.read = func(ctx context.Context, _ *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		switch query {
		case "fail":
			select {
			case <-peerStarted:
				return nil, true, fmt.Errorf("injected reader failure")
			case <-ctx.Done():
				return nil, true, ctx.Err()
			}
		case "peer":
			close(peerStarted)
			<-ctx.Done()
			cancelled <- "cancelled"
			<-release
			return nil, true, ctx.Err()
		case "dependent":
			dependent <- query
			return nil, true, nil
		default:
			return nil, false, nil
		}
	}
	options := parallelOptions(t)
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	awaitRead(t, ctx, cancelled)
	select {
	case err := <-done:
		t.Fatalf("run returned while reader was active: %v", err)
	default:
	}
	// Only the failed reader may have rolled back so far. The publication
	// transaction must stay open until its remaining reader has stopped.
	database.mutex.Lock()
	if database.rollbacks > 1 || database.helperWrites != 0 || database.relationshipWrites != 0 {
		t.Errorf("publication changed while reader was active: rollbacks=%d helpers=%d relationships=%d", database.rollbacks, database.helperWrites, database.relationshipWrites)
	}
	database.mutex.Unlock()
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "injected reader failure") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if database.commits != 0 || database.rollbacks != 3 || !reflect.DeepEqual(before, *database.graph) {
		t.Fatalf("failure did not roll back all three transactions: commits=%d rollbacks=%d", database.commits, database.rollbacks)
	}
	select {
	case query := <-dependent:
		t.Fatalf("started %s after its prerequisite failed", query)
	default:
	}
	if database.helperWrites != 0 || database.relationshipWrites != 0 {
		t.Fatalf("published results after failure: helpers=%d relationships=%d", database.helperWrites, database.relationshipWrites)
	}
	if files, err := os.ReadDir(options.TemporaryDirectory); err != nil || len(files) != 0 {
		t.Fatalf("temporary answers remain after readers stopped: files=%v err=%v", files, err)
	}
}

func TestLaterFailureCancelsAlreadyStartedDependentQuery(t *testing.T) {
	var (
		database         = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan             = fixturePlan()
		dependentStarted = make(chan struct{})
		cancelled        = make(chan string, 1)
		release          = make(chan struct{})
		done             = make(chan error, 1)
		options          = parallelOptions(t)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan.Queries = append([]extension.Query{
		{Name: "source.cypher", Parameter: "source", Text: "source"},
		{Name: "fail.cypher", Parameter: "fail", Text: "fail"},
		{Name: "dependent.cypher", Parameter: "dependent", Text: "dependent", Dependencies: []extension.Dependency{{Type: "query", Name: "source.cypher"}}},
	}, plan.Queries...)
	database.read = func(ctx context.Context, _ *fakeTransaction, query string, parameters map[string]any) (runner.Rows, bool, error) {
		switch query {
		case "source":
			return runner.Rows{{"value": "completed"}}, true, nil
		case "fail":
			select {
			case <-dependentStarted:
				return nil, true, fmt.Errorf("injected later failure")
			case <-ctx.Done():
				return nil, true, ctx.Err()
			}
		case "dependent":
			if !reflect.DeepEqual(parameters["source"], []map[string]any{{"value": "completed"}}) {
				return nil, true, fmt.Errorf("completed source missing: %v", parameters)
			}
			close(dependentStarted)
			<-ctx.Done()
			cancelled <- "dependent cancelled"
			<-release
			return nil, true, ctx.Err()
		default:
			return nil, false, nil
		}
	}
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	awaitRead(t, ctx, cancelled)
	// The first query has finished and its dependent has already started.
	// A later failure must still cancel and wait for that dependent reader.
	select {
	case err := <-done:
		t.Fatalf("returned before dependent reader stopped: %v", err)
	default:
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "injected later failure") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if database.commits != 0 || database.rollbacks != 4 || database.helperWrites != 0 || database.relationshipWrites != 0 {
		t.Fatalf("later failure was not fully discarded: commits=%d rollbacks=%d helpers=%d relationships=%d", database.commits, database.rollbacks, database.helperWrites, database.relationshipWrites)
	}
}

func TestFailedQueryCancelsPeersBeforeItsReaderCloses(t *testing.T) {
	var (
		database     = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		plan         = fixturePlan()
		peerStarted  = make(chan struct{})
		closing      = make(chan string, 1)
		cancelled    = make(chan string, 1)
		release      = make(chan struct{})
		done         = make(chan error, 1)
		readerMutex  sync.Mutex
		failedReader *fakeTransaction
		options      = parallelOptions(t)
	)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	plan.Queries = append([]extension.Query{
		{Name: "peer.cypher", Parameter: "peer", Text: "peer"},
		{Name: "fail.cypher", Parameter: "fail", Text: "fail"},
	}, plan.Queries...)
	database.read = func(ctx context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		switch query {
		case "peer":
			close(peerStarted)
			<-ctx.Done()
			cancelled <- "peer cancelled"
			return nil, true, ctx.Err()
		case "fail":
			readerMutex.Lock()
			failedReader = transaction
			readerMutex.Unlock()
			select {
			case <-peerStarted:
				return nil, true, fmt.Errorf("injected failure before reader close")
			case <-ctx.Done():
				return nil, true, ctx.Err()
			}
		default:
			return nil, false, nil
		}
	}
	database.beforeRollback = func(transaction *fakeTransaction) {
		readerMutex.Lock()
		failed := transaction == failedReader
		readerMutex.Unlock()
		if !failed {
			return
		}
		closing <- "failed reader closing"
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	go func() { _, err := runner.Run(ctx, database, plan, options); done <- err }()
	awaitRead(t, ctx, closing)
	// Closing a failed database connection may take time. Cancel its peers
	// as soon as the error is known, while still waiting for that close.
	awaitRead(t, ctx, cancelled)
	select {
	case err := <-done:
		t.Fatalf("returned while failed reader was still closing: %v", err)
	default:
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "injected failure before reader close") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if database.rollbacks != 3 || database.commits != 0 {
		t.Fatalf("failed readers were not discarded: rollbacks=%d commits=%d", database.rollbacks, database.commits)
	}
}

func TestDependentReadSeesNewHelperInPublicationTransaction(t *testing.T) {
	var (
		database = &concurrentDatabase{fakeDatabase: fixtureDatabase()}
		observed bool
	)
	database.read = func(_ context.Context, transaction *fakeTransaction, query string, _ map[string]any) (runner.Rows, bool, error) {
		if query != relationshipQuery {
			return nil, false, nil
		}
		for _, node := range transaction.staged.Nodes {
			if node.ID == "PoC_Helper_example" && node.Properties["name"] == "example" {
				observed = true
				return runner.Rows{{"source": "USER", "target": "DOMAIN"}}, true, nil
			}
		}
		return nil, true, fmt.Errorf("dependent read cannot see staged helper")
	}
	report, err := runner.Run(t.Context(), database, fixturePlan(), parallelOptions(t))
	if err != nil || !report.Committed || !observed || database.begins != 2 {
		t.Fatalf("helper visibility failed: report=%+v err=%v begins=%d", report, err, database.begins)
	}
}
