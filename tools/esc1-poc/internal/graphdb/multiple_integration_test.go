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
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/query"
)

// Use the same AD and CyberArk files that someone can upload in the browser.
// The account is synthetic: its user_sid identifies the native fixture user.
func crossPlatformFiles(t *testing.T) map[string]string {
	t.Helper()
	var (
		directory = "../../examples/cross-platform/extension"
		files     = map[string]string{}
	)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".cypher") {
			name, err = extension.QueryFileKey(name, string(contents))
			if err != nil {
				t.Fatal(err)
			}
		}
		files[name] = string(contents)
	}
	return files
}

func crossPlatformSnapshot(t *testing.T, database runner.Database) []runner.Rows {
	t.Helper()
	return []runner.Rows{
		read(t, database, "MATCH (source)-[edge:PoC_CyberArk_AccountFor]->(target) RETURN id(edge) AS id, properties(edge) AS properties ORDER BY id", nil),
		read(t, database, "MATCH (item:PoC_AD_User) RETURN id(item) AS id, properties(item) AS properties ORDER BY id", nil),
		read(t, database, "MATCH (item:PoC_CyberArk_AccountSummary) RETURN id(item) AS id, properties(item) AS properties ORDER BY id", nil),
		read(t, database, "MATCH (item:PoC_Run) RETURN id(item) AS id, properties(item) AS properties ORDER BY id", nil),
	}
}

func setAccountUser(t *testing.T, database *graph.DatabaseSwitch, userID string) {
	t.Helper()
	var properties = graph.NewProperties()
	properties.Set("user_sid", userID)
	if err := database.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
		return transaction.Nodes().Filter(query.Equals(query.NodeProperty("objectid"), "PoC_Test_CyberArkAccount")).Update(properties)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMultipleSchemasDisableAndRemovalOnBothDatabases(t *testing.T) {
	if os.Getenv("NEO4J_TEST_URI") == "" || os.Getenv("ESC1_POC_POSTGRES_TEST_URI") == "" {
		t.Skip("both isolated graph databases are required")
	}
	var neoDatabase, _ = setup(t)
	postgresDatabase, driver := postgresESC1Database(t)
	copyESC1Fixture(t, neoDatabase, driver)
	for _, backend := range nativeIntegrationBackends(t, neoDatabase, postgresDatabase, driver) {
		t.Run(backend.name, func(t *testing.T) {
			var (
				files = crossPlatformFiles(t)
				token = "multiple-integration-token-01234567890123456789"
			)
			// Remove only these examples' known helper objects and run records.
			t.Cleanup(func() {
				if err := backend.native.WriteTransaction(context.Background(), func(transaction graph.Transaction) error {
					return transaction.Nodes().Filter(query.Or(query.StringStartsWith(query.NodeProperty("objectid"), "PoC_AD_User_"), query.StringStartsWith(query.NodeProperty("objectid"), "PoC_CyberArk_AccountSummary_"), query.StringStartsWith(query.NodeProperty("objectid"), "PoC_Run_"))).Delete()
				}); err != nil {
					t.Error(err)
				}
			})
			store, err := ruleservice.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			active, err := store.Update(map[string]string{"active_directory.schema.json": files["active_directory.schema.json"], "PoC_AD/ad_users.cypher": files["PoC_AD/ad_users.cypher"]})
			if err != nil {
				t.Fatal(err)
			}
			handler, err := ruleservice.NewHandler(store, token)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			client := ruleservice.Client{BaseURL: server.URL, Token: token}
			// Fetch through HTTP after every change, just as the worker does.
			load := func() extension.Plan {
				bundle, err := client.Fetch(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				plan, err := ruleservice.ValidateBundle(bundle)
				if err != nil {
					t.Fatal(err)
				}
				return plan
			}
			change := func(request ruleservice.Change) extension.Plan {
				request.BaseRevision = active.Revision
				preview, err := store.Preview(request)
				if err != nil {
					t.Fatal(err)
				}
				request.ConfirmRevision = preview.Revision
				active, err = store.Apply(request)
				if err != nil {
					t.Fatal(err)
				}
				return load()
			}
			plan := load()
			if report, err := runner.Run(t.Context(), backend.database, plan, options()); err != nil || !report.Committed {
				t.Fatalf("AD alone: %+v %v", report, err)
			}
			adBefore := read(t, backend.database, "MATCH (item:PoC_AD_User) RETURN id(item) AS id, properties(item) AS properties ORDER BY id", nil)
			if err := backend.native.WriteTransaction(t.Context(), func(transaction graph.Transaction) error {
				_, err := transaction.CreateNode(graph.AsProperties(map[string]any{"objectid": "PoC_Test_CyberArkAccount", "user_sid": "PoC_Test_User", "poc_fixture": true}), graph.StringKind("PoC_CyberArk_Account"), graph.StringKind("PoC_CyberArk_Base"))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			active, err = store.Update(files)
			if err != nil {
				t.Fatal(err)
			}
			plan = load()
			report, err := runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || report.Added != 1 || len(report.Queries) != 3 {
				t.Fatalf("combined AD/CyberArk: %+v %v", report, err)
			}
			before := crossPlatformSnapshot(t, backend.database)
			if !reflect.DeepEqual(adBefore, before[1]) {
				t.Fatal("adding CyberArk changed existing AD helper IDs or properties")
			}
			connections := read(t, backend.database, "MATCH (source)-[edge:PoC_CyberArk_AccountFor]->(target) RETURN source.objectid AS source, target.objectid AS target", nil)
			if !reflect.DeepEqual(connections, runner.Rows{{"source": "PoC_Test_CyberArkAccount", "target": "PoC_Test_User"}}) {
				t.Fatalf("cross-platform connection has wrong endpoints: %v", connections)
			}
			plan = change(ruleservice.Change{Disable: []string{"PoC_AD/ad_users.cypher"}})
			setAccountUser(t, backend.native, "missing-user")
			report, err = runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || !reflect.DeepEqual(report.Queries, []string{"PoC_CyberArk/cyberark_accounts.cypher"}) || report.Removed != 0 {
				t.Fatalf("disabled AD with changed evidence: %+v %v", report, err)
			}
			after := crossPlatformSnapshot(t, backend.database)
			if !reflect.DeepEqual(before[0], after[0]) || !reflect.DeepEqual(before[1], after[1]) || reflect.DeepEqual(before[2], after[2]) {
				t.Fatal("disabled results changed, or the independent CyberArk rule did not run")
			}
			// No runnable query must not mean 'replace saved answers with none'.
			plan = change(ruleservice.Change{Disable: []string{"PoC_CyberArk/cyberark_accounts.cypher"}})
			report, err = runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || len(report.Queries) != 0 || report.Removed != 0 || report.HelpersChanged != 0 {
				t.Fatalf("all rules paused: %+v %v", report, err)
			}
			plan = change(ruleservice.Change{Enable: []string{"PoC_AD/ad_users.cypher", "PoC_CyberArk/cyberark_accounts.cypher"}})
			before = crossPlatformSnapshot(t, backend.database)
			report, err = runner.Run(t.Context(), &selectionRecordFailureDatabase{Database: backend.database}, plan, options())
			if err == nil || report.Committed || report.Removed != 1 {
				t.Fatalf("late failure after enabling: %+v %v", report, err)
			}
			if after := crossPlatformSnapshot(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatal("failed combined run changed previously saved results")
			}
			report, err = runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || report.Removed != 1 {
				t.Fatalf("enabled rule clears obsolete connection: %+v %v", report, err)
			}
			setAccountUser(t, backend.native, "PoC_Test_User")
			if report, err := runner.Run(t.Context(), backend.database, plan, options()); err != nil || report.Added != 1 {
				t.Fatalf("restored evidence: %+v %v", report, err)
			}
			plan = change(ruleservice.Change{Disable: []string{"PoC_AD/ad_users.cypher"}})
			before = crossPlatformSnapshot(t, backend.database)
			plan = change(ruleservice.Change{Remove: []string{"PoC_AD/ad_users.cypher"}})
			if len(plan.RetiredRelationships) != 1 || plan.RetiredRelationships[0] != "PoC_CyberArk_AccountFor" {
				t.Fatalf("removed output was not scheduled for cleanup: %v", plan.RetiredRelationships)
			}
			report, err = runner.Run(t.Context(), &selectionRecordFailureDatabase{Database: backend.database}, plan, options())
			if err == nil || report.Committed || report.Removed != 1 {
				t.Fatalf("failed deletion: %+v %v", report, err)
			}
			if after := crossPlatformSnapshot(t, backend.database); !reflect.DeepEqual(before, after) {
				t.Fatal("failed deletion lost saved data")
			}
			report, err = runner.Run(t.Context(), backend.database, plan, options())
			if err != nil || !report.Committed || report.Removed != 1 || !reflect.DeepEqual(report.Queries, []string{"PoC_CyberArk/cyberark_accounts.cypher"}) {
				t.Fatalf("delete paused cross-platform rule: %+v %v", report, err)
			}
			after = crossPlatformSnapshot(t, backend.database)
			if len(after[0]) != 0 || !reflect.DeepEqual(before[1], after[1]) || !reflect.DeepEqual(before[2], after[2]) {
				t.Fatal("deleting the cross-platform rule changed helper objects")
			}
		})
	}
}
