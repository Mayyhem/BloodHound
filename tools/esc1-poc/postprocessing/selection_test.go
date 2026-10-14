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
	"reflect"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
)

func TestServiceKeepsChangesAcrossFailedAndSupersededRuns(t *testing.T) {
	var (
		service, client, now, _ = newServiceTest(t)
		ctx                     = context.Background()
		seen                    []runner.Changes
		fail                    bool
	)
	service.execute = func(ctx context.Context, _ extension.Plan, options runner.Options) (runner.Report, error) {
		seen = append(seen, options.Changes)
		// These are our own output writes. They must never become the reason
		// to run again after a later, unrelated upload.
		service.changes.add(ctx, false, graph.StringKind("PoC_Output"))
		if fail {
			return runner.Report{}, errors.New("calculation failed")
		}
		return runner.Report{Committed: true}, nil
	}
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	if seen[0].CoverageKnown {
		t.Fatal("restart claimed to know earlier graph changes")
	}
	service.changes.add(ctx, false, graph.StringKind("Enroll"))
	fail = true
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("failed calculation was hidden")
	}
	// The next successful native analysis replaces the old pending job. The
	// change list must still include the failed job's evidence.
	service.changes.add(ctx, true, graph.StringKind("User"))
	fail = false
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	expected := runner.Changes{CoverageKnown: true, Nodes: []string{"User"}, Relationships: []string{"Enroll"}}
	if !reflect.DeepEqual(seen[2], expected) {
		t.Fatalf("lost or invented changes: got %+v, want %+v", seen[2], expected)
	}
	if remaining := service.changes.Snapshot(); !remaining.CoverageKnown || len(remaining.Nodes)+len(remaining.Relationships) != 0 {
		t.Fatalf("successful publication left settled changes: %+v", remaining)
	}

	// Losing the rule container also must not consume the accumulated changes.
	service.changes.add(ctx, true, graph.StringKind("Domain"))
	client.failure = errors.New("rule container unavailable")
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("rule-container failure was hidden")
	}
	client.failure = nil
	*now = now.Add(time.Minute)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if last := seen[len(seen)-1]; !last.CoverageKnown || !reflect.DeepEqual(last.Nodes, []string{"Domain"}) {
		t.Fatalf("retry lost changes during the outage: %+v", last)
	}
}

func TestServiceKeepsChangesWhenRetryMustWaitForNewAnalysis(t *testing.T) {
	var (
		service, _, now, _ = newServiceTest(t)
		ctx                = context.Background()
		seen               runner.Changes
	)
	service.changes.Reset()
	service.changes.add(ctx, false, graph.StringKind("Enroll"))
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{}, errors.New("failed")
	}
	if _, err := service.AfterAnalysis(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	_, release, err := service.gate.EnterWrite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	service.changes.add(ctx, true, graph.StringKind("Domain"))
	release()
	*now = now.Add(time.Minute)
	if _, err := service.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	if service.pending != nil {
		t.Fatal("old retry was not canceled after a new graph write")
	}
	service.execute = func(_ context.Context, _ extension.Plan, options runner.Options) (runner.Report, error) {
		seen = options.Changes
		return runner.Report{Committed: true}, nil
	}
	if _, err := service.AfterAnalysis(ctx); err != nil {
		t.Fatal(err)
	}
	if !seen.CoverageKnown || !reflect.DeepEqual(seen.Nodes, []string{"Domain"}) || !reflect.DeepEqual(seen.Relationships, []string{"Enroll"}) {
		t.Fatalf("canceling an old retry lost changes: %+v", seen)
	}
}

func TestServiceSettlesSafeSkipAndExcludesSchemaPreparation(t *testing.T) {
	var (
		service, client, _, _ = newServiceTest(t)
		ctx                   = context.Background()
	)
	service.changes.Reset()
	service.changes.add(ctx, true, graph.StringKind("Unrelated"))
	original := service.configuration.ReadSchemas
	service.configuration.ReadSchemas = func(ctx context.Context) ([]byte, error) {
		service.changes.Unknown(ctx)
		return original(ctx)
	}
	service.execute = func(_ context.Context, _ extension.Plan, options runner.Options) (runner.Report, error) {
		if !options.Changes.CoverageKnown || !reflect.DeepEqual(options.Changes.Nodes, []string{"Unrelated"}) {
			t.Fatalf("schema preparation corrupted the change list: %+v", options.Changes)
		}
		return runner.Report{Skipped: true}, nil
	}
	if committed, err := service.AfterAnalysis(ctx); err != nil || committed {
		t.Fatalf("safe skip reported a graph change: committed=%v, error=%v", committed, err)
	}
	if remaining := service.changes.Snapshot(); !remaining.CoverageKnown || len(remaining.Nodes)+len(remaining.Relationships) != 0 {
		t.Fatalf("safe skip did not settle changes: %+v", remaining)
	}
	if len(client.statuses) == 0 || client.statuses[len(client.statuses)-1].Message != "No rules depend on the changed data. Existing extension results were kept." {
		t.Fatalf("safe skip was not explained: %+v", client.statuses)
	}
}
