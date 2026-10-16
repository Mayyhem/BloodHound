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

//go:build integration

package graphdb_test

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// partialPlan builds three rules. good.cypher is unrelated to the other two.
// bad.cypher either works or divides by zero, a real database error that ends
// the save transaction. reader.cypher reads bad.cypher's connections.
func partialPlan(t *testing.T, mark string, failing bool) extension.Plan {
	t.Helper()
	var score = "10 / 2"
	if failing {
		score = "10 / 0"
	}
	var files = map[string]string{
		"schema.json": `{
			"schema":{"name":"partial_test","display_name":"Partial runs","version":"v1.0.0","namespace":"PoC"},
			"node_kinds":[],
			"relationship_kinds":[
				{"name":"PoC_PartialGood","compatible_kinds":[{"source_kind":"User","target_kind":"Domain"}]},
				{"name":"PoC_PartialBad","compatible_kinds":[{"source_kind":"User","target_kind":"Domain"}]},
				{"name":"PoC_PartialReader","compatible_kinds":[{"source_kind":"User","target_kind":"Domain"}]}
			]}`,
		"PoC/good.cypher":   "// namespace: PoC\n// version: v1.0.0\n// depends-on: node:User\nMATCH (evidence:User) WHERE evidence.objectid = 'PoC_Test_User' RETURN evidence.objectid AS source, 'PoC_Test_Domain' AS target, 'PoC_PartialGood' AS kind, {mark:'" + mark + "'} AS properties",
		"PoC/bad.cypher":    "// namespace: PoC\n// version: v1.0.0\n// depends-on: node:User\nMATCH (evidence:User) WHERE evidence.objectid = 'PoC_Test_User' RETURN evidence.objectid AS source, 'PoC_Test_Domain' AS target, 'PoC_PartialBad' AS kind, {score:" + score + "} AS properties",
		"PoC/reader.cypher": "// namespace: PoC\n// version: v1.0.0\n// depends-on: relationship:PoC_PartialBad\nMATCH (source)-[:PoC_PartialBad]->(target) RETURN source.objectid AS source, target.objectid AS target, 'PoC_PartialReader' AS kind",
	}
	store, err := ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Update(files)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ruleservice.ValidateBundle(active)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func partialSnapshot(t *testing.T, database runner.Database) map[string]runner.Rows {
	t.Helper()
	var result = map[string]runner.Rows{}
	for _, kind := range []string{"PoC_PartialGood", "PoC_PartialBad", "PoC_PartialReader"} {
		result[kind] = read(t, database, "MATCH (source)-[edge:"+kind+"]->(target) RETURN source.objectid AS source, id(edge) AS id, properties(edge) AS properties", nil)
	}
	result["unfinished"] = read(t, database, "MATCH (run:PoC_Run {objectid: 'PoC_Run_partial_test'}) RETURN run.unfinished AS unfinished", nil)
	return result
}

// unfinishedIn reads the saved list, which each database may return as a
// list of text or a list of any values.
func unfinishedIn(snapshot map[string]runner.Rows) []string {
	var names []string
	if len(snapshot["unfinished"]) != 1 {
		return nil
	}
	switch list := snapshot["unfinished"][0]["unfinished"].(type) {
	case []string:
		names = list
	case []any:
		for _, item := range list {
			names = append(names, item.(string))
		}
	}
	return names
}

// One rule hits a database error. Through BloodHound's own database
// connection, as in the live service, the unrelated rule is still saved, the
// failing rule and its reader keep their previous connections, and the run
// record lists them as unfinished. A run with no new data then tries only
// those two. Fixing the rule clears the unfinished list.
func TestFailingRuleIsSetAsideAndUnrelatedRuleSavesOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, _ = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			// The live service holds one turn of the write gate around every
			// attempt, and runs each attempt through BloodHound's connection.
			run := func(plan extension.Plan, options runner.Options) (runner.Report, error) {
				var report runner.Report
				err := backend.gate.Alone(t.Context(), 30*time.Second, func(ctx context.Context) error {
					var err error
					report, err = runner.RunSettingAsideFailures(ctx, plan, options, func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
						return graphdb.RunNative(ctx, backend.native, backend.name, plan, options)
					})
					return err
				})
				return report, err
			}
			setAside := []string{"PoC/bad.cypher", "PoC/reader.cypher"}

			if report, err := run(partialPlan(t, "first", false), options()); err != nil || !report.Committed || len(report.SetAside) != 0 || report.Added != 3 {
				t.Fatalf("first run: %+v %v", report, err)
			}
			before := partialSnapshot(t, backend.database)

			failing := partialPlan(t, "second", true)
			report, err := run(failing, options())
			if err != nil || !report.Committed || !reflect.DeepEqual(report.SetAside, setAside) || report.Execution.Calculation != "partial" {
				var failures []runner.Failure
				if report.Execution != nil {
					failures = report.Execution.Failures
				}
				t.Fatalf("expected a partial save: set aside %v, failures %+v, error %v", report.SetAside, failures, err)
			}
			if failure := report.Execution.Failure; failure == nil || failure.Query != "PoC/bad.cypher" {
				t.Fatalf("the failure was not reported: %+v", report.Execution)
			}
			after := partialSnapshot(t, backend.database)
			if len(after["PoC_PartialGood"]) != 1 || after["PoC_PartialGood"][0]["properties"].(map[string]any)["mark"] != "second" {
				t.Fatalf("the unrelated rule's new result was not saved: %v", after["PoC_PartialGood"])
			}
			if !reflect.DeepEqual(before["PoC_PartialBad"], after["PoC_PartialBad"]) || !reflect.DeepEqual(before["PoC_PartialReader"], after["PoC_PartialReader"]) {
				t.Fatal("a set-aside rule's previous connections changed")
			}
			if names := unfinishedIn(after); !reflect.DeepEqual(names, setAside) {
				t.Fatalf("saved run record lists %v as unfinished", names)
			}

			// No new data: only the unfinished rules run. They still fail, so
			// nothing else is calculated and the list stays.
			retry := options()
			retry.Changes = runner.Changes{CoverageKnown: true}
			report, err = run(failing, retry)
			if err != nil || !report.Committed || len(report.Queries) != 0 || !reflect.DeepEqual(report.SetAside, setAside) {
				t.Fatalf("retry did not try only the unfinished rules: %+v %v", report, err)
			}
			for _, query := range report.Execution.Queries {
				if query.Name == "PoC/good.cypher" && query.State != "skipped" {
					t.Fatalf("the unrelated rule ran again on retry: %+v", query)
				}
			}
			if names := unfinishedIn(partialSnapshot(t, backend.database)); !reflect.DeepEqual(names, setAside) {
				t.Fatalf("retry changed the unfinished list to %v", names)
			}

			// Fixing the rule saves everything and clears the list.
			report, err = run(partialPlan(t, "second", false), options())
			if err != nil || !report.Committed || len(report.SetAside) != 0 {
				t.Fatalf("fixed run: %+v %v", report, err)
			}
			if names := unfinishedIn(partialSnapshot(t, backend.database)); len(names) != 0 {
				t.Fatalf("a complete run left %v unfinished", names)
			}
		})
	}
}
