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

package runner

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/dawgs/cypher/frontend"
)

type reachTransaction struct {
	seeds     []string
	nodes     map[string]int64
	edges     map[string][]string
	queries   []string
	tailError error
}

func (s *reachTransaction) Read(context.Context, string, map[string]any) (Rows, error) {
	return nil, fmt.Errorf("expected streaming")
}
func (s *reachTransaction) Write(context.Context, string, map[string]any) (Rows, error) {
	return nil, fmt.Errorf("reach must remain read-only")
}
func (s *reachTransaction) Commit(context.Context) error   { return nil }
func (s *reachTransaction) Rollback(context.Context) error { return nil }

func (s *reachTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	s.queries = append(s.queries, query)
	if err := ctx.Err(); err != nil {
		return err
	}
	if query == "seeds" {
		for _, id := range s.seeds {
			var graphID any = s.nodes[id]
			if _, exists := s.nodes[id]; !exists {
				graphID = nil
			}
			if err := consume(map[string]any{"id": id, "graph_id": graphID, "kinds": []string{"Group"}}); err != nil {
				return err
			}
		}
		return s.tailError
	}
	var ids, ok = parameters["_reach_frontier"].([]int64)
	if !ok {
		return fmt.Errorf("frontier must use graph IDs")
	}
	for _, graphID := range ids {
		var source string
		for id, candidate := range s.nodes {
			if candidate == graphID {
				source = id
			}
		}
		if source == "" {
			return fmt.Errorf("unexpected graph ID")
		}
		for _, target := range s.edges[source] {
			if err := consume(map[string]any{"source": source, "target": target, "graph_id": s.nodes[target], "kinds": []string{"Group"}}); err != nil {
				return err
			}
		}
	}
	return nil
}
func reachFixture() *reachTransaction {
	return &reachTransaction{seeds: []string{"A", "A"}, nodes: map[string]int64{"A": 1, "B": 2, "C": 3, "D": 4, "X": 5, "Y": 6}, edges: map[string][]string{"A": {"B", "B", "C"}, "B": {"D"}, "C": {"D"}, "X": {"Y"}, "Y": {"X"}}}
}
func reachQuery(config *extension.Reachability) extension.Query {
	return extension.Query{Text: "seeds", Reach: config}
}

func TestReachabilitySharesDiamondAndIgnoresUnrelatedCycle(t *testing.T) {
	var transaction = reachFixture()
	var limits = DefaultLimits()
	limits.BatchRows = 2
	var space, err = newAnswerSpace(t.TempDir(), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer space.close()
	var answer *answerFile
	answer, err = calculateReach(context.Background(), transaction, reachQuery(&extension.Reachability{Direction: "incoming", Relationships: []string{"MemberOf"}, ExpandResults: true, RejectCycles: true}), nil, space)
	if err != nil {
		t.Fatal(err)
	}
	var rows Rows
	rows, _, err = answer.load(t.Context(), answer.space.limits.ParameterBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows %v", rows)
	}
	var expected = map[string][]string{"A": {"B", "C", "D"}, "B": {"D"}, "C": {"D"}, "D": {}}
	for _, row := range rows {
		var reach, _ = stringsFrom(row["reach"])
		if !slices.Equal(reach, expected[row["principal"].(string)]) {
			t.Fatalf("row %v", row)
		}
	}
	if len(transaction.queries) != 4 {
		t.Fatalf("diamond visited more than one frontier per node: %v", transaction.queries)
	}
	for _, query := range transaction.queries {
		if strings.Contains(query, "*1..") || strings.Contains(query, "path=") {
			t.Fatalf("enumerated routes: %s", query)
		}
	}
}

func TestReachabilityPermitsCyclicForestAndIncludesStart(t *testing.T) {
	var transaction = reachFixture()
	transaction.edges = map[string][]string{"A": {"B"}, "B": {"A", "C"}}
	var space, err = newAnswerSpace(t.TempDir(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer space.close()
	var answer *answerFile
	answer, err = calculateReach(context.Background(), transaction, reachQuery(&extension.Reachability{Direction: "outgoing", Relationships: []string{"SameForestTrust"}, IncludeStart: true}), nil, space)
	if err != nil {
		t.Fatal(err)
	}
	var rows Rows
	rows, _, err = answer.load(t.Context(), answer.space.limits.ParameterBytes)
	if err != nil {
		t.Fatal(err)
	}
	var reach, _ = stringsFrom(rows[0]["reach"])
	if len(rows) != 1 || !reflect.DeepEqual(reach, []string{"A", "B", "C"}) {
		t.Fatalf("rows %v", rows)
	}
}

func TestReachabilityFailsWithoutPartialAnswers(t *testing.T) {
	var cases = []struct {
		name   string
		edit   func(*reachTransaction, *Limits)
		cancel bool
	}{
		{"cycle", func(tx *reachTransaction, _ *Limits) { tx.edges["D"] = []string{"A"} }, false},
		{"self loop", func(tx *reachTransaction, _ *Limits) { tx.edges["A"] = []string{"A"} }, false},
		{"missing seed", func(tx *reachTransaction, _ *Limits) { tx.seeds = []string{"MISSING"} }, false},
		{"late database error", func(tx *reachTransaction, _ *Limits) { tx.tailError = fmt.Errorf("late stream failure") }, false},
		{"cache limit", func(_ *reachTransaction, limits *Limits) { limits.ParameterBytes = 100 }, false},
		{"row limit", func(_ *reachTransaction, limits *Limits) { limits.RowBytes = 20 }, false},
		{"canceled", func(_ *reachTransaction, _ *Limits) {}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var transaction = reachFixture()
			var limits = DefaultLimits()
			testCase.edit(transaction, &limits)
			var space, err = newAnswerSpace(t.TempDir(), limits)
			if err != nil {
				t.Fatal(err)
			}
			defer space.close()
			var ctx, cancel = context.WithCancel(context.Background())
			defer cancel()
			if testCase.cancel {
				cancel()
			}
			var answer *answerFile
			answer, err = calculateReach(ctx, transaction, reachQuery(&extension.Reachability{Direction: "incoming", Relationships: []string{"MemberOf"}, RejectCycles: true}), nil, space)
			if err == nil || answer != nil {
				t.Fatalf("answer %v error %v", answer, err)
			}
		})
	}
}

func TestReachabilityGeneratedQueriesRemainCypher(t *testing.T) {
	for _, query := range []string{reachTraversalQuery(&extension.Reachability{Direction: "incoming", Relationships: []string{"MemberOf", "MemberOfLocalGroup"}, NodeKinds: []string{"Group", "Domain"}}), reachTraversalQuery(&extension.Reachability{Direction: "outgoing", Relationships: []string{"SameForestTrust"}})} {
		if _, err := frontend.ParseCypher(frontend.NewContext(), query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
}

// An independent breadth-first oracle checks all directed three-node graphs,
// including self-loops and overlapping cyclic components.
func TestReachabilityCacheMatchesBreadthFirstSearch(t *testing.T) {
	for mask := 0; mask < 1<<9; mask++ {
		var graph = reachGraph{byID: map[string]uint32{}, graphIDs: map[int64]string{}, limit: 1 << 20}
		for index, id := range []string{"A", "B", "C"} {
			if _, _, err := graph.vertex(map[string]any{"principal": id, "graph_id": int64(index), "kinds": []string{"Domain"}}); err != nil {
				t.Fatal(err)
			}
		}
		for source := 0; source < 3; source++ {
			for target := 0; target < 3; target++ {
				if mask&(1<<(source*3+target)) != 0 {
					graph.vertices[source].neighbors.Add(uint32(target))
				}
			}
		}
		if err := graph.calculate(context.Background(), false); err != nil {
			t.Fatalf("graph %d: %v", mask, err)
		}
		for root := 0; root < 3; root++ {
			var seen = [3]bool{}
			var queue = []int{root}
			seen[root] = true
			for position := 0; position < len(queue); position++ {
				for target := 0; target < 3; target++ {
					if mask&(1<<(queue[position]*3+target)) != 0 && !seen[target] {
						seen[target] = true
						queue = append(queue, target)
					}
				}
			}
			seen[root] = false
			for target, reachable := range seen {
				if graph.vertices[root].reachable.Contains(uint32(target)) != reachable {
					t.Fatalf("graph %d root %d target %d closure disagrees with BFS", mask, root, target)
				}
			}
		}
	}
}
