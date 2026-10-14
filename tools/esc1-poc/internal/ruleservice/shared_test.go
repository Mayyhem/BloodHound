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
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

// A and B share one output. The graph reader can keep working after A is
// deleted, while the direct reader needs A's returned rows and must be removed.
func sharedRelationshipFiles(t *testing.T) map[string]string {
	var (
		result = files(t)
		schema map[string]any
	)
	t.Helper()
	if err := json.Unmarshal([]byte(result["schema.json"]), &schema); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"PoC_Vault_ReadEdge", "PoC_Vault_DirectEdge", "PoC_Vault_Independent"} {
		schema["relationship_kinds"] = append(schema["relationship_kinds"].([]any), map[string]any{
			"name":             kind,
			"compatible_kinds": []any{map[string]any{"source_kind": "PoC_Vault_Container", "target_kind": "User"}},
		})
	}
	contents, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	result["schema.json"] = string(contents)
	result["PoC_Vault/second.cypher"] = "// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: none\n// existing-edge-policy: keep-existing\nRETURN 'vault' AS source, 'user' AS target, 'PoC_Vault_HasAccountFor' AS kind"
	result["PoC_Vault/graph_reader.cypher"] = "// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: relationship:PoC_Vault_HasAccountFor\nMATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN 'vault' AS source, 'user' AS target, 'PoC_Vault_ReadEdge' AS kind"
	result["PoC_Vault/direct_reader.cypher"] = "// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: query:account_users\nUNWIND $account_users AS row RETURN row.source AS source, row.target AS target, 'PoC_Vault_DirectEdge' AS kind"
	result["PoC_Vault/independent.cypher"] = "// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: none\nRETURN 'vault' AS source, 'user' AS target, 'PoC_Vault_Independent' AS kind"
	return result
}

func TestDisablingSharedProducerPausesPeersAndReaders(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(sharedRelationshipFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"PoC_Vault/account_users.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Paused, []string{"PoC_Vault/account_users.cypher", "PoC_Vault/direct_reader.cypher", "PoC_Vault/graph_reader.cypher", "PoC_Vault/second.cypher"}) {
		t.Fatal("shared producers and readers must pause together", preview.Paused)
	}
	queries := crossPlatformViews(preview.Plan)
	if !reflect.DeepEqual(queries["PoC_Vault/account_users.cypher"].Outputs[0].Producers, []string{"PoC_Vault/account_users.cypher", "PoC_Vault/second.cypher"}) {
		t.Fatal("the view must show shared producers in property precedence order", queries["PoC_Vault/account_users.cypher"].Outputs)
	}
	if queries["PoC_Vault/graph_reader.cypher"].Stage <= queries["PoC_Vault/second.cypher"].Stage {
		t.Fatal("the graph reader must wait for both producers", queries)
	}
	if !queries["PoC_Vault/independent.cypher"].Enabled || !queries["PoC_Vault/vault_helpers.cypher"].Enabled {
		t.Fatal("unrelated rules must continue", queries)
	}
	disabled := applyChange(t, store, change)
	if !reflect.DeepEqual(disabled.Disabled, []string{"PoC_Vault/account_users.cypher"}) || !reflect.DeepEqual(disabled.Files, initial.Files) || len(disabled.RetiredRelationships) != 0 {
		t.Fatal("only the explicit disable choice should be saved", disabled)
	}
	bothDisabled := applyChange(t, store, ruleservice.Change{BaseRevision: disabled.Revision, Disable: []string{"PoC_Vault/second.cypher"}})
	partlyEnabled := applyChange(t, store, ruleservice.Change{BaseRevision: bothDisabled.Revision, Enable: []string{"PoC_Vault/account_users.cypher"}})
	for _, name := range preview.Paused {
		if crossPlatformViews(store.View())[name].Enabled {
			t.Fatal("another disabled producer must keep shared results paused", name)
		}
	}
	applyChange(t, store, ruleservice.Change{BaseRevision: partlyEnabled.Revision, Enable: []string{"PoC_Vault/second.cypher"}})
	for _, query := range store.View().Queries {
		if !query.Enabled {
			t.Fatal("all rules should resume", query)
		}
	}
}

func TestDeletingSharedProducerKeepsPeersAndGraphReaders(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(sharedRelationshipFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"PoC_Vault/account_users.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Removed, []string{"PoC_Vault/account_users.cypher", "PoC_Vault/direct_reader.cypher"}) {
		t.Fatal("delete should cascade only to rules needing returned rows", preview.Removed)
	}
	if !reflect.DeepEqual(preview.RetiredRelationships, []string{"PoC_Vault_DirectEdge"}) {
		t.Fatal("shared output still has a producer", preview.RetiredRelationships)
	}
	if !reflect.DeepEqual(preview.RecalculatedRelationships, []string{"PoC_Vault_DirectEdge", "PoC_Vault_HasAccountFor", "PoC_Vault_ReadEdge"}) {
		t.Fatal("preview must distinguish recalculated types from deleted files", preview.RecalculatedRelationships)
	}
	removed := applyChange(t, store, change)
	for _, name := range []string{"PoC_Vault/second.cypher", "PoC_Vault/graph_reader.cypher", "PoC_Vault/independent.cypher"} {
		if removed.Files[name] == "" {
			t.Fatal("remaining rule was deleted", name)
		}
	}
	lastRemoved := applyChange(t, store, ruleservice.Change{BaseRevision: removed.Revision, Remove: []string{"PoC_Vault/second.cypher"}})
	if lastRemoved.Files["PoC_Vault/graph_reader.cypher"] == "" || lastRemoved.Files["schema.json"] != initial.Files["schema.json"] {
		t.Fatal("last-producer removal must keep graph readers and definitions", lastRemoved)
	}
	if !reflect.DeepEqual(lastRemoved.RetiredRelationships, []string{"PoC_Vault_DirectEdge", "PoC_Vault_HasAccountFor"}) {
		t.Fatal("last producer should schedule shared output cleanup", lastRemoved.RetiredRelationships)
	}
	plan, err := ruleservice.ValidateBundle(lastRemoved)
	if err != nil || len(plan.RetiredRelationships) != 2 {
		t.Fatal("worker must accept readers of empty retired output", err, plan)
	}
}

func TestSnapshotsAndViewsContainNoSavedOwnership(t *testing.T) {
	var store = open(t)
	bundle, err := store.Update(sharedRelationshipFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{bundle, store.View()} {
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{`"owners":`, `"owner":`, `"retired_owners":`} {
			if strings.Contains(string(contents), field) {
				t.Fatal("ownership should not be part of the saved or displayed contract", field)
			}
		}
	}
}

func TestDeletingDisabledSharedProducerResumesRemainingRules(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(sharedRelationshipFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	disabled := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"PoC_Vault/account_users.cypher"}})
	removed := applyChange(t, store, ruleservice.Change{BaseRevision: disabled.Revision, Remove: []string{"PoC_Vault/account_users.cypher"}})
	if len(removed.Disabled) != 0 {
		t.Fatal("deleted rule should no longer be saved as disabled", removed.Disabled)
	}
	for _, query := range store.View().Queries {
		if !query.Enabled {
			t.Fatal("removing the disabled producer should let remaining rules refresh", query)
		}
	}
}
