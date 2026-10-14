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
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func TestLegacyOutputSettingsCanBeReplacedByQueryDeclarations(t *testing.T) {
	var (
		directory  = t.TempDir()
		modern     = files(t)
		legacy     = files(t)
		definition extension.Definition
	)
	if err := json.Unmarshal([]byte(legacy["schema.json"]), &definition); err != nil {
		t.Fatal(err)
	}
	definition.PostProcessing = &extension.ProcessingSettings{ReferenceCommit: extension.BuildReferenceCommit}
	for index := range definition.NodeKinds {
		if definition.NodeKinds[index].Name == "PoC_Vault_Helper" {
			definition.NodeKinds[index].PostProcessing = &extension.NodeProcessing{Query: "PoC_Vault/vault_helpers.cypher", IDPrefix: "PoC_Vault_Helper_"}
		}
	}
	for index := range definition.RelationshipKinds {
		if definition.RelationshipKinds[index].Name == "PoC_Vault_HasAccountFor" {
			definition.RelationshipKinds[index].PostProcessing = &extension.RelationshipProcessing{Query: "PoC_Vault/account_users.cypher"}
			definition.RelationshipKinds[index].ExistingEdgePolicy = extension.KeepExisting
		}
	}
	contents, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	legacy["schema.json"] = string(contents)
	legacy["PoC_Vault/account_users.cypher"] = strings.ReplaceAll(legacy["PoC_Vault/account_users.cypher"], ", 'PoC_Vault_HasAccountFor' AS kind", "")
	legacy["PoC_Vault/account_users.cypher"] = strings.ReplaceAll(legacy["PoC_Vault/account_users.cypher"], "// existing-edge-policy: keep-existing\n", "")
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(legacy)
	if err != nil {
		t.Fatal(err)
	}
	store, err = ruleservice.Open(directory)
	if err != nil {
		t.Fatal("legacy snapshot cannot reopen", err)
	}
	migrated, err := store.Update(modern)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migrated.Schemas[0].Definition), "post_processing") {
		t.Fatal("replacement rewrote schema", migrated.Files["schema.json"])
	}
	store, err = ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ruleservice.ValidateBundle(active)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Schema.Nodes) != 1 || len(plan.Schema.Relationships) != 1 {
		t.Fatal("worker lost output declarations", plan.Schema)
	}
	if err := store.RefreshSchemas(ruleservice.SchemaSnapshot{Definitions: active.Schemas}); err != nil {
		t.Fatal(err)
	}
	removed := applyChange(t, store, ruleservice.Change{BaseRevision: active.Revision, Remove: []string{"PoC_Vault/account_users.cypher"}})
	if len(removed.RetiredRelationships) != 1 || removed.RetiredRelationships[0] != "PoC_Vault_HasAccountFor" {
		t.Fatal("last producer removal was not recorded", removed)
	}
}

func TestMissingOutputDeclarationDoesNotSilentlyRemoveRule(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	replacement := strings.ReplaceAll(initial.Files["PoC_Vault/account_users.cypher"], ", 'PoC_Vault_HasAccountFor' AS kind", "")
	replacement = strings.ReplaceAll(replacement, "// existing-edge-policy: keep-existing\n", "")
	if _, err := store.Update(map[string]string{"PoC_Vault/account_users.cypher": replacement}); err == nil {
		t.Fatal("missing kind silently retired output")
	}
	active, err := store.Active()
	if err != nil || active.Revision != initial.Revision {
		t.Fatal("invalid replacement changed active rules", err)
	}
}
