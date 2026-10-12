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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestHelpersReadTheirPrefixOnceAndPreserveExistingNodes(t *testing.T) {
	var (
		database   = fixtureDatabase()
		desired    = runner.Rows{}
		oldHelpers = []fakeNode{
			{ID: "PoC_Helper_000_old", Kinds: []string{"User"}, Properties: map[string]any{"name": "unrelated prefix collision"}},
			{ID: "PoC_Helper_zzz_old", Kinds: []string{"PoC_Helper"}, Properties: map[string]any{"poc_owner": "another-owner", "name": "old helper"}},
		}
	)
	// Reverse input order exercises the merge rather than relying on query order.
	for index := 127; index >= 0; index-- {
		desired = append(desired, map[string]any{"id": fmt.Sprintf("PoC_Helper_%03d", index), "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"name": fmt.Sprintf("helper %d", index)}})
	}
	database.answers[helperQuery] = desired
	database.graph.Nodes = append(database.graph.Nodes, oldHelpers...)
	first := runSuccessfully(t, database)
	if first.HelpersChanged != len(desired) || database.helperWrites != len(desired) {
		t.Fatalf("first publication did not create every desired helper: report=%+v writes=%d", first, database.helperWrites)
	}
	assertHelperPrefixReads(t, database.reads, 1)
	for _, old := range oldHelpers {
		var retained bool
		for _, current := range database.graph.Nodes {
			if reflect.DeepEqual(current, old) {
				retained = true
			}
		}
		if !retained {
			t.Fatalf("unmatched old helper %q changed during initial publication", old.ID)
		}
	}
	var before = cloneGraph(*database.graph)
	database.reads = nil
	second := runSuccessfully(t, database)
	assertHelperPrefixReads(t, database.reads, 1)
	if second.HelpersChanged != 0 || database.helperWrites != len(desired) {
		t.Fatalf("unchanged helpers were rewritten: report=%+v writes=%d", second, database.helperWrites)
	}
	// No helper write on the second run means the existing node identities remain.
	for _, previous := range before.Nodes {
		if reflect.DeepEqual(previous.Kinds, []string{"PoC_Run"}) {
			continue
		}
		var matches []fakeNode
		for _, current := range database.graph.Nodes {
			if current.ID == previous.ID {
				matches = append(matches, current)
			}
		}
		if len(matches) != 1 || !reflect.DeepEqual(matches[0], previous) {
			t.Fatalf("existing node %q was removed, duplicated, or changed", previous.ID)
		}
	}
}

func TestHelperPrefixMatchingCollisionsRollBack(t *testing.T) {
	for _, collision := range []struct {
		name  string
		kinds []string
		owner string
	}{
		{name: "wrong kind", kinds: []string{"User"}, owner: "test-owner/PoC_Helper"},
		{name: "foreign owner", kinds: []string{"PoC_Helper"}, owner: "another-owner"},
	} {
		t.Run(collision.name, func(t *testing.T) {
			var database = fixtureDatabase()
			database.answers[helperQuery] = runner.Rows{
				{"id": "PoC_Helper_a", "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"name": "new helper"}},
				{"id": "PoC_Helper_z", "kinds": []string{"PoC_Helper"}, "properties": map[string]any{"name": "desired helper"}},
			}
			database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "PoC_Helper_z", Kinds: collision.kinds, Properties: map[string]any{"objectid": "PoC_Helper_z", "poc_owner": collision.owner, "name": "original"}})
			before := cloneGraph(*database.graph)
			assertRollback(t, database, before, fixturePlan(), completeBatch(), "belongs to a native or differently owned node")
			assertHelperPrefixReads(t, database.reads, 1)
			if database.helperWrites != 1 {
				t.Fatal("collision did not exercise rollback after an earlier helper write")
			}
		})
	}
}

func assertHelperPrefixReads(t *testing.T, reads []recordedRead, expected int) {
	t.Helper()
	var count int
	for _, read := range reads {
		if read.query == helperLookupQuery {
			count++
			if !reflect.DeepEqual(read.parameters, map[string]any{"prefix": "PoC_Helper_"}) {
				t.Fatalf("helper lookup must send only its prefix: %v", read.parameters)
			}
		} else if strings.Contains(read.query, "properties(item)") {
			t.Fatalf("unexpected helper lookup: %s", read.query)
		}
	}
	if count != expected {
		t.Fatalf("helper prefix was read %d times; expected %d", count, expected)
	}
}

// Wrap the existing transactional fake so a connection can fail after delivering
// a valid prefix row. Its staged state and rollback behavior remain unchanged.
type helperPrefixFailureDatabase struct{ *fakeDatabase }
type helperPrefixFailureTransaction struct{ runner.Transaction }

func (s *helperPrefixFailureDatabase) Begin(ctx context.Context) (runner.Transaction, error) {
	transaction, err := s.fakeDatabase.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &helperPrefixFailureTransaction{Transaction: transaction}, nil
}

func (s *helperPrefixFailureTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	rows, err := s.Transaction.Read(ctx, query, parameters)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := consume(row); err != nil {
			return err
		}
		if query == helperLookupQuery {
			return fmt.Errorf("helper prefix connection lost after a row")
		}
	}
	return nil
}

func TestHelperPrefixLateStreamFailureRollsBack(t *testing.T) {
	var database = fixtureDatabase()
	database.graph.Nodes = append(database.graph.Nodes, fakeNode{ID: "PoC_Helper_old", Kinds: []string{"PoC_Helper"}, Properties: map[string]any{"name": "preserve me"}})
	before := cloneGraph(*database.graph)
	report, err := runner.Run(t.Context(), &helperPrefixFailureDatabase{fakeDatabase: database}, fixturePlan(), completeBatch())
	if err == nil || !strings.Contains(err.Error(), "helper prefix connection lost after a row") {
		t.Fatalf("late prefix failure was accepted: %v", err)
	}
	if report.Committed || database.commits != 0 || database.rollbacks != 1 || !reflect.DeepEqual(*database.graph, before) {
		t.Fatalf("late prefix failure changed persisted graph: report=%+v commits=%d rollbacks=%d", report, database.commits, database.rollbacks)
	}
	assertHelperPrefixReads(t, database.reads, 1)
}
