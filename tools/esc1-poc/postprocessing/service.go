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
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
)

// Configuration is deliberately small. An empty native URL means the caller
// never constructs this service and never installs its write gate.
type Configuration struct {
	URL, Token, Directory, Backend string
	// Read installed definitions while holding the same turn as graph writes.
	// Stored analysis files never install or recreate an extension schema.
	ReadSchemas func(context.Context) ([]byte, error)
}

type ruleClient interface {
	Fetch(context.Context) (ruleservice.Bundle, error)
	Report(context.Context, ruleservice.Status) error
}

type pendingRun struct {
	event        string
	changeNumber uint64
	bundle       ruleservice.Bundle
	retryAt      time.Time
	attempt      int
}

// Service is called by the single native import loop, never by an HTTP request
// from the rule container. Its mutex also prevents accidental overlapping calls.
type Service struct {
	mutex         sync.Mutex
	gate          *Gate
	client        ruleClient
	configuration Configuration
	pending       *pendingRun
	reports       []ruleservice.Status
	clock         func() time.Time
	changes       changeTracker
	execute       func(context.Context, extension.Plan, runner.Options) (runner.Report, error)
	executeTest   func(context.Context, extension.Plan, runner.Options) (runner.Report, error)
	editor        *analysisEditor
}

func New(configuration Configuration, database *graph.DatabaseSwitch) (*Service, error) {
	if database == nil {
		return nil, fmt.Errorf("native graph database is required")
	}
	var parsed, err = url.Parse(configuration.URL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("post-processing URL must name the rule service without credentials or a path")
	}
	if len(strings.TrimSpace(configuration.Token)) < 32 {
		return nil, fmt.Errorf("post-processing token must contain at least 32 characters")
	}
	if configuration.Backend != "neo4j" && configuration.Backend != "pg" {
		return nil, fmt.Errorf("unsupported post-processing graph backend %q", configuration.Backend)
	}
	if configuration.Directory == "" {
		return nil, fmt.Errorf("post-processing work directory is required")
	}
	if err := os.MkdirAll(configuration.Directory, 0700); err != nil {
		return nil, err
	}
	var service = &Service{
		gate: NewGate(), configuration: configuration, clock: time.Now, editor: newAnalysisEditor(),
		client: ruleservice.Client{BaseURL: configuration.URL, Token: configuration.Token, HTTP: &http.Client{Timeout: 10 * time.Second}},
	}
	service.execute = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		return graphdb.RunNative(ctx, database, configuration.Backend, plan, options)
	}
	service.executeTest = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		return graphdb.TestNative(ctx, database, configuration.Backend, plan, options)
	}
	database.SetWriteGate(service.gate.EnterWrite)
	database.SetWriteObserver(graph.WriteObserver{Transaction: service.changes.WrapTransaction, Batch: service.changes.WrapBatch, Unknown: service.changes.Unknown})
	return service, nil
}

// WithAnalysis keeps native analysis and its following extension calculation
// together. Without this small boundary, a queued API write could change the
// evidence between those two steps. Reads continue throughout both steps.
func (s *Service) WithAnalysis(ctx context.Context, action func(context.Context) error) error {
	return s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		// The native writes inside this turn use its private context. Record
		// one change here so even a failed native analysis invalidates an old
		// extension retry; partially changed native data is not a fresh result.
		s.gate.mutex.Lock()
		s.gate.changeNumber++
		s.gate.advanceVersion()
		s.gate.mutex.Unlock()
		return action(turnContext)
	})
}

// WithSchemaChange uses the same turn as analysis, so a completed deletion
// cannot be followed by publication from a run that used the deleted schema.
func (s *Service) WithSchemaChange(ctx context.Context, action func(context.Context) error) error {
	return s.WithAnalysis(ctx, func(changeContext context.Context) error {
		s.changes.Unknown(changeContext)
		return action(changeContext)
	})
}

// AfterAnalysis records one successful native analysis, which may cover several
// uploads. A newer success replaces pending older work: only the newest complete
// graph matters. Uploading rules alone does not start a graph calculation.
func (s *Service) AfterAnalysis(ctx context.Context) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.pending = &pendingRun{event: s.clock().UTC().Format(time.RFC3339Nano), changeNumber: s.gate.ChangeNumber()}
	return s.run(ctx)
}

// Retry is called during an idle import tick. Retrying is safe only while no
// ordinary graph write has started since the successful native analysis.
// A restart triggers native startup analysis and reconstructs this opportunity;
// saved rule files do not depend on this in-memory retry record.
func (s *Service) Retry(ctx context.Context) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.flushReports(ctx)
	if s.pending == nil || s.clock().Before(s.pending.retryAt) {
		return false, nil
	}
	if s.pending.changeNumber != s.gate.ChangeNumber() {
		s.report(ctx, "failed", "Graph changed after the last successful analysis. Waiting for the next successful native analysis.")
		s.pending = nil
		return false, nil
	}
	return s.run(ctx)
}

func (s *Service) run(ctx context.Context) (bool, error) {
	var pending = s.pending
	pending.attempt++
	pending.retryAt = s.clock().Add(time.Minute)
	if pending.bundle.Revision == "" {
		bundle, err := s.client.Fetch(ctx)
		if errors.Is(err, ruleservice.ErrNoActive) {
			s.report(ctx, "no-rules", "No analyses are active. Upload schemas in OpenGraph Management and analysis files on the rules page.")
			s.pending = nil
			return false, nil
		}
		if err != nil {
			s.report(ctx, "unavailable", "Could not fetch the active rules. No new calculation started; previous results were kept.")
			return false, fmt.Errorf("fetch extension rules; old results retained: %w", err)
		}
		pending.bundle = bundle
	}
	plan, err := ruleservice.ValidateBundle(pending.bundle)
	if err != nil {
		s.report(ctx, "failed", "The downloaded rules failed validation; existing results were retained.")
		return false, err
	}
	s.report(ctx, "running", "Calculating rules after successful native analysis. Graph queries remain available.")
	var report runner.Report
	err = s.gate.Alone(ctx, 30*time.Second, func(turnContext context.Context) error {
		if s.gate.ChangeNumber() != pending.changeNumber {
			return fmt.Errorf("graph changed while waiting; another successful native analysis is required")
		}
		// Keep every observed change until publication succeeds. In particular,
		// a failed rule followed by another upload must retry BOTH sets of changes.
		// Our own output writes must not become next time's input changes.
		changes := s.changes.Snapshot()
		turnContext = withoutChangeObservation(turnContext)
		if s.configuration.ReadSchemas == nil {
			return fmt.Errorf("installed schema reader is not configured")
		}
		contents, err := s.configuration.ReadSchemas(turnContext)
		if err != nil {
			return fmt.Errorf("cannot read installed schemas; previous results were kept")
		}
		snapshot, err := ruleservice.ParseSchemas(contents)
		if err != nil {
			return err
		}
		plan, err = ruleservice.ValidateInstalledBundle(pending.bundle, snapshot)
		if err != nil {
			return err
		}
		var runError error
		report, runError = s.execute(turnContext, plan, runner.Options{
			BatchID: pending.event, IngestionComplete: true, WritersPaused: true, Changes: changes,
			TemporaryDirectory: s.configuration.Directory, Limits: runner.DefaultLimits(),
		})
		// PostgreSQL publication borrows a transaction directly from its driver.
		// It is already inside this exclusive turn, but does not pass through the
		// ordinary writer hook. Record confirmed or uncertain publication here
		// before admitting another writer.
		if report.Committed || errors.Is(runError, runner.ErrCommitUncertain) {
			s.gate.mutex.Lock()
			s.gate.advanceVersion()
			s.gate.mutex.Unlock()
		}
		if errors.Is(runError, runner.ErrCommitUncertain) {
			// Do this before releasing the turn: no other writer may race a
			// transaction whose actual commit result is still unknown.
			s.gate.BlockWrites(runError)
		}
		if runError == nil && (report.Committed || report.Skipped) {
			// Clear this while other writers are still waiting at the gate.
			// A successful skip also settles the accumulated changes: none of
			// them can affect this already-published version of the rules.
			s.changes.Reset()
		}
		return runError
	})
	if err != nil {
		state := "failed"
		if errors.Is(err, runner.ErrCommitUncertain) {
			state = "uncertain"
		}
		if report.Execution == nil {
			report.Execution = &runner.Execution{Calculation: "failed", Save: "not-started", Phase: "preparation", Failure: &runner.Failure{Phase: "preparation", Message: runner.FailureMessage(err)}}
		}
		if state == "uncertain" {
			report.Execution.Save = "uncertain"
		}
		s.report(ctx, state, runner.FailureMessage(err), report.Execution)
		return false, err
	}
	if report.Skipped {
		s.report(ctx, "success", "No rules depend on the changed data. Existing extension results were kept.", report.Execution)
	} else {
		s.report(ctx, "success", fmt.Sprintf("Finished %d of %d queries: %d connections added, %d removed, %d updated.", len(report.Queries), len(plan.Queries), report.Added, report.Removed, report.Updated), report.Execution)
	}
	s.pending = nil
	return report.Committed, nil
}

func (s *Service) report(ctx context.Context, state, message string, execution ...*runner.Execution) {
	// Reports contain counts and rule identifiers, never query answers or lab
	// object names. A reporting outage cannot undo a committed graph change.
	var status = ruleservice.Status{State: state, Event: s.pending.event, Revision: s.pending.bundle.Revision, Message: message, UpdatedAt: s.clock().UTC(), Backend: s.configuration.Backend, RunID: fmt.Sprintf("%s/%d", s.pending.event, s.pending.attempt)}
	if len(execution) > 0 {
		status.Execution = execution[0]
	}
	s.reports = append(s.reports, status)
	if len(s.reports) > ruleservice.MaxRecentRuns {
		s.reports = s.reports[len(s.reports)-ruleservice.MaxRecentRuns:]
	}
	s.flushReports(ctx)
	slog.InfoContext(ctx, "Extension processing", "state", state, "event", status.Event, "revision", status.Revision, "message", message)
}

// Reporting can fail after the database has saved everything. Retry delivery
// on idle ticks without repeating a calculation or claiming that it rolled back.
func (s *Service) flushReports(ctx context.Context) {
	for len(s.reports) > 0 {
		if err := s.client.Report(ctx, s.reports[0]); err != nil {
			slog.WarnContext(ctx, "Could not save extension status in the rule container", "error", err)
			return
		}
		s.reports = s.reports[1:]
	}
}
