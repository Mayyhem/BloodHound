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

package ruleservice_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestRecentReportsKeepConfirmedRevisionAcrossFailureAndRestart(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	if store.LatestStatus().State != "unavailable" {
		t.Fatal("missing report should be unavailable")
	}
	success := ruleservice.Status{State: "success", Event: "first", RunID: "first/1", Revision: initial.Revision, Execution: &runner.Execution{Calculation: "succeeded", Save: "succeeded", Phase: "complete", Saved: &runner.ChangesCount{Added: 1}}}
	if err := store.Report(success); err != nil {
		t.Fatal(err)
	}
	if err := store.Report(ruleservice.Status{State: "unavailable", Event: "fetch-failed", Message: "Could not fetch rules."}); err != nil {
		t.Fatal(err)
	}
	if !store.LatestStatus().ResultsStale {
		t.Fatal("unavailable attempt claims current results")
	}
	changed, err := store.Update(map[string]string{"account_users.cypher": initial.Files["account_users.cypher"] + "\n// changed\n"})
	if err != nil {
		t.Fatal(err)
	}
	if status := store.LatestStatus(); status.SavedRevision != initial.Revision || !status.ResultsStale {
		t.Fatalf("old revision: %+v", status)
	}
	for index := 0; index < ruleservice.MaxRecentRuns+3; index++ {
		status := ruleservice.Status{State: "running", Event: fmt.Sprint(index), RunID: fmt.Sprint(index), Revision: changed.Revision}
		if err := store.Report(status); err != nil {
			t.Fatal(err)
		}
		status.State = "failed"
		status.Execution = &runner.Execution{Calculation: "succeeded", Save: "failed", Phase: "commit", Failure: &runner.Failure{Phase: "commit", Message: "Saving failed."}}
		if err := store.Report(status); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	status := restored.LatestStatus()
	if len(status.History) != ruleservice.MaxRecentRuns || status.SavedRevision != initial.Revision || !status.ResultsStale || status.ResultsUncertain {
		t.Fatalf("restored report: %+v", status)
	}
	status.History[0].Execution.Save = "tampered"
	if restored.LatestStatus().History[0].Execution.Save == "tampered" {
		t.Fatal("caller changed stored history")
	}
	uncertain := ruleservice.Status{State: "uncertain", Event: "lost", Revision: changed.Revision, Execution: &runner.Execution{Calculation: "succeeded", Save: "uncertain", Phase: "commit"}}
	if err := restored.Report(uncertain); err != nil {
		t.Fatal(err)
	}
	if !restored.LatestStatus().ResultsUncertain || restored.LatestStatus().SavedRevision != initial.Revision {
		t.Fatal("uncertain save replaced confirmed revision")
	}
}

func TestReportsRejectInvalidCountsAndExpireMissingCompletion(t *testing.T) {
	var store = open(t)
	status := ruleservice.Status{State: "failed", Event: "bad", Revision: strings.Repeat("a", 64), Execution: &runner.Execution{Calculation: "failed", Save: "failed", Saved: &runner.ChangesCount{Added: 1}}}
	if err := store.Report(status); err == nil {
		t.Fatal("unconfirmed saved counts accepted")
	}
	status.Execution = &runner.Execution{Calculation: "failed", Save: "failed", Proposed: runner.ChangesCount{Added: -1}}
	if err := store.Report(status); err == nil {
		t.Fatal("negative counts accepted")
	}
	status.Execution = nil
	status.State = "running"
	status.UpdatedAt = time.Now().Add(-3 * time.Hour)
	if err := store.Report(status); err != nil {
		t.Fatal(err)
	}
	if latest := store.LatestStatus(); latest.State != "unavailable" || !latest.ResultsUncertain {
		t.Fatalf("lost completion: %+v", latest)
	}
}
