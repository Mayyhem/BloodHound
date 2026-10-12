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

package ruleservice_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func crossPlatformFiles(t *testing.T) map[string]string {
	t.Helper()
	var (
		directory = filepath.Join("..", "..", "examples", "cross-platform", "extension")
		result    = make(map[string]string)
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
		result[entry.Name()] = string(contents)
	}
	return result
}

func crossPlatformViews(view ruleservice.PlanView) map[string]ruleservice.QueryView {
	var result = make(map[string]ruleservice.QueryView)
	for _, thread := range view.Threads {
		for _, query := range thread.Queries {
			result[query.Name] = query
		}
	}
	return result
}

func TestCrossPlatformSchemasUseOneDependencyPlanAndKeepOwners(t *testing.T) {
	var store = open(t)
	bundle, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ruleservice.ValidateBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Definitions) != 2 || len(plan.Schema.Nodes) != 2 || len(plan.Schema.Relationships) != 1 || len(plan.Queries) != 3 {
		t.Fatal("schemas were not combined", plan)
	}
	for _, binding := range plan.Schema.Nodes {
		expectedOwner := "extension/cyberark"
		if binding.Kind == "PoC_AD_User" {
			expectedOwner = "extension/active_directory"
		}
		if binding.EffectiveOwner(plan.Schema.Owner) != expectedOwner {
			t.Fatal("node owner was flattened", binding)
		}
	}
	relationship := plan.Schema.Relationships[0]
	if relationship.Kind != "PoC_CyberArk_AccountFor" || relationship.EffectiveOwner(plan.Schema.Owner) != "extension/cyberark" {
		t.Fatal("relationship owner was flattened", relationship)
	}
	view := store.View()
	if len(view.Extensions) != 2 || view.Extensions[0].Name != "active_directory" || view.Extensions[0].DisplayName != "Active Directory" || view.Extensions[1].Name != "cyberark" || view.Extensions[1].DisplayName != "CyberArk" {
		t.Fatal("schema names absent from view", view.Extensions)
	}
	queries := crossPlatformViews(view)
	if queries["ad_users.cypher"].Stage != 1 || queries["cyberark_accounts.cypher"].Stage != 1 || queries["cyberark_links.cypher"].Stage != 2 || queries["ad_users.cypher"].Order >= queries["cyberark_links.cypher"].Order {
		t.Fatal("cross-schema prerequisite order incorrect", queries)
	}
	for name, query := range queries {
		expectedName := "CyberArk"
		expectedExtension := "cyberark"
		if name == "ad_users.cypher" {
			expectedName = "Active Directory"
			expectedExtension = "active_directory"
		}
		if query.Extension != expectedExtension || query.ExtensionName != expectedName || !query.Enabled || query.AddedAt == "" {
			t.Fatal("query metadata missing", query)
		}
	}
}

func TestAddingSecondSchemaPreservesActivationDates(t *testing.T) {
	var (
		store          = open(t)
		allFiles       = crossPlatformFiles(t)
		directoryFiles = map[string]string{"active_directory.schema.json": allFiles["active_directory.schema.json"], "ad_users.cypher": allFiles["ad_users.cypher"]}
		accountFiles   = map[string]string{"cyberark.schema.json": allFiles["cyberark.schema.json"], "cyberark_accounts.cypher": allFiles["cyberark_accounts.cypher"], "cyberark_links.cypher": allFiles["cyberark_links.cypher"]}
	)
	preview, err := store.Preview(ruleservice.Change{Files: directoryFiles})
	if err != nil {
		t.Fatal(err)
	}
	if query := crossPlatformViews(preview.Plan)["ad_users.cypher"]; query.AddedAt != "" {
		t.Fatal("preview fabricated an activation date", query)
	}
	initial := applyChange(t, store, ruleservice.Change{Files: directoryFiles})
	addedAt := initial.AddedAt["ad_users.cypher"]
	if addedAt.IsZero() {
		t.Fatal("activation date missing")
	}
	updated := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Files: accountFiles})
	if len(updated.Files) != 5 || !updated.AddedAt["ad_users.cypher"].Equal(addedAt) || updated.AddedAt["cyberark_accounts.cypher"].IsZero() || updated.AddedAt["cyberark_links.cypher"].IsZero() {
		t.Fatal("incremental activation lost dates or schemas", updated)
	}
	overwritten := applyChange(t, store, ruleservice.Change{BaseRevision: updated.Revision, Files: map[string]string{"ad_users.cypher": updated.Files["ad_users.cypher"] + "\n// updated"}, Overwrite: []string{"ad_users.cypher"}})
	if !reflect.DeepEqual(overwritten.AddedAt, updated.AddedAt) {
		t.Fatal("overwrite reset dates", overwritten.AddedAt)
	}
}

func TestDisablingCrossSchemaPrerequisitePreservesRulesAndReenablesConsumer(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"ad_users.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	queries := crossPlatformViews(preview.Plan)
	if queries["ad_users.cypher"].Enabled || queries["cyberark_links.cypher"].Enabled || !queries["cyberark_accounts.cypher"].Enabled || queries["cyberark_links.cypher"].DisabledReason == "" {
		t.Fatal("disable did not propagate selectively", queries)
	}
	if len(queries["cyberark_links.cypher"].Outputs) != 1 || len(preview.Removed) != 0 || len(preview.RetiredRelationships) != 0 {
		t.Fatal("disable removed bindings or retired output", preview)
	}
	disabled := applyChange(t, store, change)
	if !reflect.DeepEqual(disabled.Files, initial.Files) || !reflect.DeepEqual(disabled.AddedAt, initial.AddedAt) || !reflect.DeepEqual(disabled.Disabled, []string{"ad_users.cypher"}) || len(disabled.RetiredRelationships) != 0 {
		t.Fatal("disable changed saved rule files or metadata", disabled)
	}
	plan, err := ruleservice.ValidateBundle(disabled)
	if err != nil || len(plan.Schema.Nodes) != 2 || len(plan.Schema.Relationships) != 1 {
		t.Fatal("disabled plan lost output contracts", err, plan)
	}
	enabled := applyChange(t, store, ruleservice.Change{BaseRevision: disabled.Revision, Enable: []string{"ad_users.cypher"}})
	if len(enabled.Disabled) != 0 || !reflect.DeepEqual(enabled.AddedAt, initial.AddedAt) {
		t.Fatal("reenable changed dates", enabled)
	}
	for _, query := range crossPlatformViews(store.View()) {
		if !query.Enabled || query.DisabledReason != "" {
			t.Fatal("consumer stayed paused", query)
		}
	}
}

func TestCrossSchemaRemovalCascadesAndRetainsRelationshipOwner(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"ad_users.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Removed, []string{"ad_users.cypher", "cyberark_links.cypher"}) {
		t.Fatal("cross-schema reader survived removal", preview.Removed)
	}
	removed := applyChange(t, store, change)
	if len(removed.Files) != 3 || removed.Files["active_directory.schema.json"] == "" || removed.Files["cyberark.schema.json"] == "" || removed.Files["cyberark_accounts.cypher"] == "" {
		t.Fatal("removal lost unrelated schema or account helper", removed.Files)
	}
	if !reflect.DeepEqual(removed.RetiredRelationships, []string{"PoC_CyberArk_AccountFor"}) || removed.RetiredOwners["PoC_CyberArk_AccountFor"] != "extension/cyberark" {
		t.Fatal("retirement lost owner", removed)
	}
	plan, err := ruleservice.ValidateBundle(removed)
	if err != nil || len(plan.Queries) != 1 || plan.Queries[0].Name != "cyberark_accounts.cypher" || plan.RetiredOwners["PoC_CyberArk_AccountFor"] != "extension/cyberark" {
		t.Fatal("worker plan lost retirement owner or independent rule", err, plan)
	}
}

func TestCrossSchemaMetadataSurvivesRestartAndCallerMutation(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	expected := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"ad_users.cypher"}})
	caller, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	caller.Disabled[0] = "cyberark_accounts.cypher"
	caller.AddedAt["ad_users.cypher"] = time.Time{}
	reopened, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reopened.Active()
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("saved metadata lost on restart", err, actual)
	}
	active, err := store.Active()
	if err != nil || !reflect.DeepEqual(active, expected) {
		t.Fatal("caller mutation changed active metadata", err, active)
	}
	queries := crossPlatformViews(reopened.View())
	if queries["cyberark_links.cypher"].Enabled || !queries["cyberark_accounts.cypher"].Enabled {
		t.Fatal("restart lost effective disable state", queries)
	}
	removed := applyChange(t, reopened, ruleservice.Change{BaseRevision: actual.Revision, Remove: []string{"ad_users.cypher"}})
	restored, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	actual, err = restored.Active()
	if err != nil || !reflect.DeepEqual(actual, removed) || actual.RetiredOwners["PoC_CyberArk_AccountFor"] != "extension/cyberark" {
		t.Fatal("restart lost retirement metadata", err, actual)
	}
}

func sameBundleJSON(t *testing.T, first, second ruleservice.Bundle) bool {
	t.Helper()
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(firstJSON, secondJSON)
}

func TestClientFetchValidatesDisabledAndRetiredCrossSchemaBundles(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	client := localClient(handler)
	disabled := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"ad_users.cypher"}})
	fetched, err := client.Fetch(context.Background())
	if err != nil || !sameBundleJSON(t, fetched, disabled) {
		t.Fatal("client rejected current disabled snapshot", err, fetched)
	}
	retired := applyChange(t, store, ruleservice.Change{BaseRevision: disabled.Revision, Remove: []string{"ad_users.cypher"}})
	fetched, err = client.Fetch(context.Background())
	if err != nil || !sameBundleJSON(t, fetched, retired) {
		t.Fatal("client rejected current retired snapshot", err, fetched)
	}
	for index, corrupted := range []ruleservice.Bundle{
		{Revision: disabled.Revision, Files: disabled.Files, AddedAt: disabled.AddedAt, Disabled: []string{"cyberark_accounts.cypher"}},
		{Revision: retired.Revision, Files: retired.Files, AddedAt: retired.AddedAt, RetiredRelationships: retired.RetiredRelationships, RetiredOwners: map[string]string{"PoC_CyberArk_AccountFor": "extension/active_directory"}},
		{Revision: disabled.Revision, Files: disabled.Files, AddedAt: map[string]time.Time{"missing.cypher": time.Now().UTC()}, Disabled: disabled.Disabled},
	} {
		contents, err := json.Marshal(corrupted)
		if err != nil {
			t.Fatal(err)
		}
		corruptClient := localClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write(contents) }))
		if _, err := corruptClient.Fetch(context.Background()); err == nil {
			t.Fatal("client accepted tampered metadata case", index)
		}
	}
}

func TestCrossSchemaDisableAlsoPausesManagedKindAndEndpointConsumers(t *testing.T) {
	var (
		store    = open(t)
		original = crossPlatformFiles(t)
		schema   map[string]any
	)
	if err := json.Unmarshal([]byte(original["cyberark.schema.json"]), &schema); err != nil {
		t.Fatal(err)
	}
	relationships := schema["relationship_kinds"].([]any)
	relationships = append(relationships,
		map[string]any{"name": "PoC_CyberArk_HelperFor", "endpoints": []any{map[string]any{"source_kind": "PoC_AD_User", "target_kind": "User"}}, "post_processing": map[string]any{"query": "helper_endpoint.cypher"}},
		map[string]any{"name": "PoC_CyberArk_ReadHelper", "endpoints": []any{map[string]any{"source_kind": "PoC_CyberArk_Account", "target_kind": "User"}}, "post_processing": map[string]any{"query": "helper_reader.cypher"}},
	)
	schema["relationship_kinds"] = relationships
	schema["external_node_kinds"] = []any{"User", "PoC_AD_User"}
	contents, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	original["cyberark.schema.json"] = string(contents)
	original["helper_endpoint.cypher"] = "// depends-on: none\nRETURN 'PoC_AD_User_example' AS source, 'example' AS target"
	original["helper_reader.cypher"] = "// depends-on: node:PoC_AD_User, node:PoC_CyberArk_Account, node:User\nMATCH (helper:PoC_AD_User), (account:PoC_CyberArk_Account), (user:User) RETURN account.objectid AS source, user.objectid AS target"
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	disabled := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"ad_users.cypher"}})
	queries := crossPlatformViews(store.View())
	for _, name := range []string{"ad_users.cypher", "cyberark_links.cypher", "helper_endpoint.cypher", "helper_reader.cypher"} {
		if queries[name].Enabled || strings.TrimSpace(queries[name].DisabledReason) == "" {
			t.Fatal("managed consumer remained enabled", name, queries[name])
		}
	}
	if !queries["cyberark_accounts.cypher"].Enabled || len(disabled.RetiredRelationships) != 0 || !reflect.DeepEqual(disabled.Files, initial.Files) {
		t.Fatal("disable pruned independent rules", disabled)
	}
}

func TestLegacyRulesKeepUnknownActivationDates(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(crossPlatformFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	initial.AddedAt = nil
	contents, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "active.json"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal("older snapshot without dates was rejected", err)
	}
	for _, query := range crossPlatformViews(reopened.View()) {
		if query.AddedAt != "" {
			t.Fatal("restart invented a date", query)
		}
	}
	updated := applyChange(t, reopened, ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{"ad_users.cypher": initial.Files["ad_users.cypher"] + "\n// edited legacy rule"}, Overwrite: []string{"ad_users.cypher"}})
	if len(updated.AddedAt) != 0 {
		t.Fatal("edit invented dates for older rules", updated.AddedAt)
	}
}
