// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package graphdb_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// Checking sources and targets separately would permit User -> Computer here.
// The real database must reject that invented combination and keep every old
// result, including a helper update staged before the rejection.
func TestExactEndpointPairsPreservePreviousResults(t *testing.T) {
	var database, plan = setup(t)
	first, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || !first.Committed || first.Added != 1 {
		t.Fatalf("initial run: %+v, %v", first, err)
	}
	write(t, database, "MATCH (domain:Domain) SET domain.name = 'STAGED HELPER CHANGE'", nil)
	plan.Schema.Relationships[0].Endpoints = []extension.Endpoint{
		{SourceKind: "User", TargetKind: "Domain"},
		{SourceKind: "Group", TargetKind: "Computer"},
	}
	replaceESC1Query(&plan, "MATCH (source:User), (target:Computer) RETURN source.objectid AS source, target.objectid AS target")
	before := guardGraphSnapshot(t, database)
	rejected, err := runner.Run(t.Context(), database, plan, options())
	if err == nil || !strings.Contains(err.Error(), "incompatible endpoint pair") || rejected.Committed || rejected.HelpersChanged != 1 {
		t.Fatalf("expected rejection after staged helper change: %+v, %v", rejected, err)
	}
	if after := guardGraphSnapshot(t, database); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected pair changed the graph or saved run state")
	}

	// A node with several labels is allowed when one COMPLETE pair matches.
	// User now also has Group, so Group -> Computer genuinely applies.
	write(t, database, "MATCH (source:User) SET source:Group", nil)
	accepted, err := runner.Run(t.Context(), database, plan, options())
	if err != nil || !accepted.Committed || accepted.Added != 1 || accepted.Removed != 1 {
		t.Fatalf("allowed multi-label pair: %+v, %v", accepted, err)
	}
}
