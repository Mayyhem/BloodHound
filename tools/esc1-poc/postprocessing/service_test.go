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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

type serviceTestClient struct {
	bundle   ruleservice.Bundle
	failure  error
	fetches  int
	statuses []ruleservice.Status
}

func (s *serviceTestClient) Fetch(context.Context) (ruleservice.Bundle, error) {
	s.fetches++
	return s.bundle, s.failure
}
func (s *serviceTestClient) Report(_ context.Context, status ruleservice.Status) error {
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

func TestServiceRegistersStandardSchemaBeforePublishing(t *testing.T) {
	service, _, now, calls := newServiceTest(t)
	var registrations int
	service.configuration.RegisterSchema = func(ctx context.Context, payload []byte) error {
		registrations++
		if strings.Contains(string(payload), "post_processing") || strings.Contains(string(payload), "existing_edge_policy") || strings.Contains(string(payload), "endpoints") {
			t.Fatal("proposed fields leaked to native schema")
		}
		_, release, err := service.gate.EnterWrite(ctx)
		if err != nil {
			t.Fatal(err)
		}
		release()
		return nil
	}
	if _, err := service.AfterAnalysis(context.Background()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if _, err := service.AfterAnalysis(context.Background()); err != nil {
		t.Fatal(err)
	}
	if registrations != 1 || *calls != 2 {
		t.Fatalf("registration %d runs %d", registrations, *calls)
	}
	service.registeredSchema = nil
	service.configuration.RegisterSchema = func(context.Context, []byte) error { return errors.New("schema rejected") }
	if _, err := service.AfterAnalysis(context.Background()); err == nil {
		t.Fatal("accepted failed schema registration")
	}
	if *calls != 2 {
		t.Fatal("published before schema registration succeeded")
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
