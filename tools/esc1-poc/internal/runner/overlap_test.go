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
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

func overlapRow(id string, graphID int64, group, special, specialMember bool, reach ...string) map[string]any {
	return map[string]any{"principal": id, "graph_id": graphID, "is_group": group, "is_special": special, "has_special_member": specialMember, "reach": reach, "reach_size": int64(len(reach))}
}

func prepareOverlap(t *testing.T, rows Rows) *groupOverlap {
	t.Helper()
	var (
		parameters  = map[string]any{"membership": rows, "other": Rows{{"value": "kept"}}}
		result, err = newGroupOverlap(t.Context(), extension.Query{Overlap: &extension.GroupOverlap{Memberships: "membership"}}, parameters, DefaultLimits())
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := parameters["membership"]; exists {
		t.Fatal("lookup table leaked into seed parameters")
	}
	if _, exists := parameters["other"]; !exists {
		t.Fatal("unrelated seed parameter removed")
	}
	return result
}

func overlapSources(t *testing.T, index *groupOverlap, left, right []string) []string {
	t.Helper()
	var sources = []string{}
	if err := index.emit(context.Background(), map[string]any{"left": left, "right": right, "target": "target"}, func(row map[string]any) error {
		if row["target"] != "target" {
			t.Fatal("target changed")
		}
		sources = append(sources, row["source"].(string))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return sources
}

func TestGroupOverlapShortcutRules(t *testing.T) {
	var testCases = []struct {
		name              string
		rows              Rows
		left, right, want []string
	}{
		{"direct matches stay selected", Rows{overlapRow("G", 1, true, false, false, "U"), overlapRow("U", 2, false, false, false)}, []string{"U", "G"}, []string{"G"}, []string{"G", "U"}},
		{"left special chooses original right", Rows{overlapRow("S", 1, true, true, false, "U"), overlapRow("R", 2, true, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"S"}, []string{"R"}, []string{"R"}},
		{"right nested special chooses original left", Rows{overlapRow("L", 1, true, false, false, "U"), overlapRow("R", 2, true, false, true, "U"), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"R"}, []string{"L"}},
		{"both special union original sets", Rows{overlapRow("L", 1, true, true, false), overlapRow("R", 2, true, false, true)}, []string{"L"}, []string{"R"}, []string{"L", "R"}},
		{"special flags require group", Rows{overlapRow("L", 1, false, true, true, "U"), overlapRow("R", 2, false, false, false), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"R"}, []string{}},
		{"right expands only group flag", Rows{overlapRow("L", 1, false, false, false, "U"), overlapRow("R", 2, false, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"R"}, []string{}},
		{"left reach ignores labels", Rows{overlapRow("L", 1, false, false, false, "U"), overlapRow("R", 2, true, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"R"}, []string{"U"}},
		{"matching nested group shortcuts its members", Rows{overlapRow("L", 1, true, false, false, "G", "U"), overlapRow("G", 2, true, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"G"}, []string{"G"}},
		{"unmatched nested group leaves members", Rows{overlapRow("L", 1, true, false, false, "G", "U"), overlapRow("G", 2, true, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"L"}, []string{"U"}, []string{"U"}},
		{"overlapping groups never readd shared member", Rows{overlapRow("L", 1, true, false, false, "G1", "G2", "U", "V", "W"), overlapRow("G1", 2, true, false, false, "U", "V"), overlapRow("G2", 3, true, false, false, "V", "W"), overlapRow("U", 4, false, false, false), overlapRow("V", 5, false, false, false), overlapRow("W", 6, false, false, false)}, []string{"L"}, []string{"G1", "G2"}, []string{"G1", "G2"}},
		{"larger shortcut preserves earlier selected group", Rows{overlapRow("L", 1, true, false, false, "G1", "G2", "U"), overlapRow("G1", 2, true, false, false, "U"), overlapRow("G2", 3, true, false, false, "G1", "U"), overlapRow("U", 4, false, false, false)}, []string{"L"}, []string{"G1", "G2"}, []string{"G1", "G2"}},
		{"native graph order beats input order and names", Rows{overlapRow("alpha", 20, false, false, false), overlapRow("zeta", 10, false, false, false)}, []string{"alpha", "zeta", "alpha"}, []string{"alpha", "zeta"}, []string{"zeta", "alpha"}},
		{"unmatched later direct root removes earlier expansion copy", Rows{overlapRow("L", 1, true, false, false, "G", "U"), overlapRow("G", 2, true, false, false, "U"), overlapRow("U", 3, false, false, false)}, []string{"G", "L"}, []string{"U"}, []string{"U"}},
		{"empty side", Rows{overlapRow("U", 1, false, false, false)}, []string{}, []string{"U"}, []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			index := prepareOverlap(t, testCase.rows)
			actual := overlapSources(t, index, testCase.left, testCase.right)
			if !reflect.DeepEqual(actual, testCase.want) {
				t.Fatalf("got %v, want %v", actual, testCase.want)
			}
			again := overlapSources(t, index, testCase.left, testCase.right)
			if !reflect.DeepEqual(again, actual) {
				t.Fatal("working sets mutated the shared cache")
			}
		})
	}
}

func TestGroupOverlapFailsClosed(t *testing.T) {
	var (
		rows                 = Rows{overlapRow("U", 1, false, false, false)}
		index                = prepareOverlap(t, rows)
		cancellation, cancel = context.WithCancel(context.Background())
		seed                 = map[string]any{"left": []string{"U"}, "right": []string{"U"}, "target": "target"}
		callbackError        = errors.New("disk full")
	)
	cancel()
	if err := index.emit(cancellation, seed, func(map[string]any) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	if err := index.emit(context.Background(), seed, func(map[string]any) error { return callbackError }); !errors.Is(err, callbackError) {
		t.Fatalf("writer failure was lost: %v", err)
	}
	seed["left"] = []string{"missing"}
	if err := index.emit(context.Background(), seed, func(map[string]any) error { return nil }); err == nil {
		t.Fatal("missing membership accepted")
	}
	seed["left"] = []string{"U"}
	seed["extra"] = true
	if err := index.emit(context.Background(), seed, func(map[string]any) error { return nil }); err == nil {
		t.Fatal("extra seed column accepted")
	}
	index.limits.RowBytes = 1
	delete(seed, "extra")
	if err := index.emit(context.Background(), seed, func(map[string]any) error { return nil }); err == nil {
		t.Fatal("oversized seed accepted")
	}
	var limits = DefaultLimits()
	limits.ParameterBytes = 1
	if _, err := newGroupOverlap(t.Context(), extension.Query{Overlap: &extension.GroupOverlap{Memberships: "membership"}}, map[string]any{"membership": rows}, limits); err == nil {
		t.Fatal("oversized cache accepted")
	}
	rows = Rows{overlapRow("U", 1, false, false, false, "missing")}
	if _, err := newGroupOverlap(t.Context(), extension.Query{Overlap: &extension.GroupOverlap{Memberships: "membership"}}, map[string]any{"membership": rows}, DefaultLimits()); err == nil {
		t.Fatal("incomplete reach table accepted")
	}
}

// Cancel after setup has made progress, without relying on goroutine timing.
// The embedded context still supplies the real cancellation signal and error.
type overlapSetupContext struct {
	context.Context
	cancel          context.CancelFunc
	remainingChecks int
}

func (s *overlapSetupContext) Err() error {
	s.remainingChecks--
	if s.remainingChecks <= 0 {
		s.cancel()
	}
	return s.Context.Err()
}

func TestGroupOverlapSetupCancellation(t *testing.T) {
	var (
		query                       = extension.Query{Overlap: &extension.GroupOverlap{Memberships: "membership"}}
		rows                        = Rows{}
		contextBefore, cancelBefore = context.WithCancel(t.Context())
	)
	cancelBefore()
	if _, err := newGroupOverlap(contextBefore, query, nil, DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled setup should stop before inspecting parameters: %v", err)
	}
	for index := 0; index < 1000; index++ {
		rows = append(rows, overlapRow(fmt.Sprintf("principal-%d", index), int64(index), false, false, false))
	}
	contextDuring, cancelDuring := context.WithCancel(t.Context())
	defer cancelDuring()
	progress := &overlapSetupContext{Context: contextDuring, cancel: cancelDuring, remainingChecks: 64}
	parameters := map[string]any{"membership": rows}
	if _, err := newGroupOverlap(progress, query, parameters, DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during setup should stop index construction: %v", err)
	}
	if _, retained := parameters["membership"]; !retained {
		t.Fatal("canceled setup consumed the membership parameter")
	}
}
