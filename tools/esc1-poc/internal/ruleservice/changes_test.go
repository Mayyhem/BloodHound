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
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func confirmedHTTPChange(t *testing.T, handler http.Handler, change ruleservice.Change) []byte {
	t.Helper()
	contents, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(handler, "POST", "/v1/preview", "private-token-012345678901234567890123456789", contents)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var preview ruleservice.Preview
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	change.ConfirmRevision = preview.Revision
	contents, err = json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func applyChange(t *testing.T, store *ruleservice.Store, change ruleservice.Change) ruleservice.Bundle {
	t.Helper()
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	change.ConfirmRevision = preview.Revision
	bundle, err := store.Apply(change)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestPreviewLeavesActiveAndDiskUntouched(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{Files: files(t)}
	preview, err := store.Preview(change)
	if err != nil || preview.Revision == "" {
		t.Fatal(err, preview)
	}
	if _, err := store.Active(); !errors.Is(err, ruleservice.ErrNoActive) {
		t.Fatal("preview activated rules", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("preview persisted files", entries, err)
	}
	initial := applyChange(t, store, change)
	before, err := os.ReadFile(filepath.Join(directory, "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	change = ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"vault_helpers.cypher"}}
	if _, err := store.Preview(change); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(directory, "active.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview changed disk", err)
	}
	active, err := store.Active()
	if err != nil || !reflect.DeepEqual(active, initial) {
		t.Fatal("preview changed active", err)
	}
}

func TestApplyRequiresExactReviewedCandidateAndCurrentBase(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{"account_users.cypher": initial.Files["account_users.cypher"] + "\n// reviewed"}}
	if _, err := store.Preview(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("replacement lacked explicit choice", err)
	}
	change.Overwrite = []string{"account_users.cypher"}
	preview, err := store.Preview(change)
	if err != nil || !reflect.DeepEqual(preview.Overwritten, change.Overwrite) {
		t.Fatal(err, preview)
	}
	if _, err := store.Apply(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("applied unconfirmed change", err)
	}
	change.ConfirmRevision = preview.Revision
	change.Files["account_users.cypher"] += "\n// edited after review"
	if _, err := store.Apply(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("applied edited candidate", err)
	}
	change.Files["account_users.cypher"] = initial.Files["account_users.cypher"] + "\n// reviewed"
	newer, err := store.Update(map[string]string{"vault_helpers.cypher": initial.Files["vault_helpers.cypher"] + "\n// another editor"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("stale editor overwrote current rules", err)
	}
	active, err := store.Active()
	if err != nil || active.Revision != newer.Revision {
		t.Fatal("lost newer work", err)
	}
}

func TestReplacementRebuildsDeclaredDependencies(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	query := "// depends-on: node:PoC_Vault_Container, node:User\nMATCH (vault:PoC_Vault_Container), (directory_user:User) RETURN vault.objectid AS source, directory_user.objectid AS target, 'PoC_Vault_HasAccountFor' AS kind"
	change := ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{"account_users.cypher": query}, Overwrite: []string{"account_users.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, thread := range preview.Plan.Threads {
		for _, rule := range thread.Queries {
			if rule.Name == "account_users.cypher" {
				found = true
				if len(rule.Prerequisites) != 0 || rule.Text != query || rule.Stage != 1 {
					t.Fatal("old prerequisite survived", rule)
				}
			}
		}
	}
	if !found {
		t.Fatal("replacement absent from plan")
	}
	updated := applyChange(t, store, change)
	if store.View().Revision != updated.Revision {
		t.Fatal("active plan not rebuilt")
	}
}

func TestRemovingPrerequisiteCascadesAndKeepsSchema(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"vault_helpers.cypher"}}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Removed, []string{"account_users.cypher", "vault_helpers.cypher"}) || len(preview.Plan.Threads) != 0 {
		t.Fatal("incomplete cascade", preview)
	}
	active, err := store.Active()
	if err != nil || active.Revision != initial.Revision {
		t.Fatal("cascade preview partially activated", err)
	}
	updated := applyChange(t, store, change)
	if len(updated.Files) != 1 || updated.Files["schema.json"] == "" {
		t.Fatal("schema lost or queries retained", updated.Files)
	}
	if !reflect.DeepEqual(updated.RetiredRelationships, []string{"PoC_Vault_HasAccountFor"}) {
		t.Fatal("removed output not retired", updated)
	}
	plan, err := ruleservice.ValidateBundle(updated)
	if err != nil || len(plan.Queries) != 0 || len(plan.Schema.Nodes) != 0 || len(plan.Schema.Relationships) != 0 {
		t.Fatal("removed declarations survived", err, plan)
	}
}

func TestQueryRenameKeepsOutputWithoutChangingSchema(t *testing.T) {
	var store = open(t)
	original := files(t)
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{
		"schema.json":          strings.ReplaceAll(original["schema.json"], "vault_helpers.cypher", "replacement.cypher"),
		"account_users.cypher": strings.ReplaceAll(original["account_users.cypher"], "vault_helpers", "replacement"),
		"replacement.cypher":   original["vault_helpers.cypher"],
	}, Overwrite: []string{"schema.json", "account_users.cypher"}, Remove: []string{"vault_helpers.cypher"}}
	preview, err := store.Preview(change)
	if err != nil || !reflect.DeepEqual(preview.Removed, []string{"vault_helpers.cypher"}) {
		t.Fatal("old query not pruned", err, preview)
	}
	updated := applyChange(t, store, change)
	if _, exists := updated.Files["vault_helpers.cypher"]; exists {
		t.Fatal("old query retained")
	}
	if len(updated.RetiredRelationships) != 0 {
		t.Fatal("unchanged output retired")
	}
}

func TestRetirementHistorySurvivesRestartAndReintroduction(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	original := files(t)
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	removed := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"account_users.cypher"}})
	if len(removed.RetiredRelationships) != 1 {
		t.Fatal(removed)
	}
	removed.RetiredRelationships[0] = "caller mutation"
	removed.RetiredEndpoints["PoC_Vault_HasAccountFor"][0].TargetKind = "caller mutation"
	restored, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	active, err := restored.Active()
	if err != nil || !reflect.DeepEqual(active.RetiredRelationships, []string{"PoC_Vault_HasAccountFor"}) {
		t.Fatal("history lost or aliased", err, active)
	}
	if endpoints := active.RetiredEndpoints["PoC_Vault_HasAccountFor"]; len(endpoints) != 1 || endpoints[0].SourceKind != "PoC_Vault_Container" || endpoints[0].TargetKind != "User" {
		t.Fatal("saved cleanup endpoints were lost or changed by a caller", endpoints)
	}
	changed, err := restored.Update(map[string]string{"vault_helpers.cypher": active.Files["vault_helpers.cypher"] + "\n// update"})
	if err != nil || !reflect.DeepEqual(changed.RetiredRelationships, active.RetiredRelationships) {
		t.Fatal("update forgot retirement", err, changed)
	}
	reintroduced, err := restored.Update(original)
	if err != nil || len(reintroduced.RetiredRelationships) != 0 || len(reintroduced.RetiredEndpoints) != 0 {
		t.Fatal("active kind still retired", err, reintroduced)
	}
	tampered := changed
	tampered.RetiredRelationships = nil
	if _, err := ruleservice.ValidateBundle(tampered); err == nil {
		t.Fatal("history tampering preserved digest")
	}
	tampered = changed
	tampered.RetiredEndpoints["PoC_Vault_HasAccountFor"][0].TargetKind = "Computer"
	if _, err := ruleservice.ValidateBundle(tampered); err == nil {
		t.Fatal("changing cleanup endpoints must invalidate the digest")
	}
}

func TestHTTPRequiresPreviewAndReplacementChoice(t *testing.T) {
	var store = open(t)
	handler, err := ruleservice.NewHandler(store, "private-token-012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(ruleservice.Change{Files: files(t)})
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(handler, "PUT", "/v1/rules", "private-token-012345678901234567890123456789", contents)
	if response.Code != http.StatusConflict {
		t.Fatal("unreviewed upload activated", response.Code, response.Body.String())
	}
	response = exchange(handler, "PUT", "/v1/rules", "private-token-012345678901234567890123456789", confirmedHTTPChange(t, handler, ruleservice.Change{Files: files(t)}))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	initial, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{"account_users.cypher": initial.Files["account_users.cypher"] + "\n// replaced"}}
	contents, err = json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	response = exchange(handler, "POST", "/v1/preview", "private-token-012345678901234567890123456789", contents)
	if response.Code != http.StatusConflict {
		t.Fatal("silent filename replacement", response.Code)
	}
	change.Overwrite = []string{"account_users.cypher"}
	response = exchange(handler, "PUT", "/v1/rules", "private-token-012345678901234567890123456789", confirmedHTTPChange(t, handler, change))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestRemovalKeepsGraphReadersAndEndpointConsumers(t *testing.T) {
	var (
		store    = open(t)
		original = files(t)
		schema   map[string]any
	)
	if err := json.Unmarshal([]byte(original["schema.json"]), &schema); err != nil {
		t.Fatal(err)
	}
	relationships := schema["relationship_kinds"].([]any)
	relationships = append(relationships,
		map[string]any{"name": "PoC_Vault_HelperEdge", "compatible_kinds": []any{map[string]any{"source_kind": "PoC_Vault_Helper", "target_kind": "User"}}, "post_processing": map[string]any{"query": "endpoint.cypher"}},
		map[string]any{"name": "PoC_Vault_ReadHelper", "compatible_kinds": []any{map[string]any{"source_kind": "PoC_Vault_Container", "target_kind": "User"}}, "post_processing": map[string]any{"query": "node_reader.cypher"}},
		map[string]any{"name": "PoC_Vault_ReadEdge", "compatible_kinds": []any{map[string]any{"source_kind": "PoC_Vault_Container", "target_kind": "User"}}, "post_processing": map[string]any{"query": "edge_reader.cypher"}},
	)
	schema["relationship_kinds"] = relationships
	contents, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	original["schema.json"] = string(contents)
	original["endpoint.cypher"] = "// depends-on: none\nRETURN 'helper' AS source, 'user' AS target"
	original["node_reader.cypher"] = "// depends-on: node:PoC_Vault_Helper\nMATCH (helper:PoC_Vault_Helper) RETURN helper.objectid AS source, 'user' AS target"
	original["edge_reader.cypher"] = "// depends-on: relationship:PoC_Vault_HasAccountFor\nMATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN 'vault' AS source, 'user' AS target"
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.Preview(ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"vault_helpers.cypher"}})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"account_users.cypher", "vault_helpers.cypher"}
	if !reflect.DeepEqual(preview.Removed, expected) {
		t.Fatal("deletion should cascade only through required query rows", preview.Removed)
	}
	if !reflect.DeepEqual(preview.RetiredRelationships, []string{"PoC_Vault_HasAccountFor"}) {
		t.Fatal("retirements missing or unsorted", preview.RetiredRelationships)
	}
}

func TestRemovingLastProducerAllowsRemainingReader(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	updated := applyChange(t, store, ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"account_users.cypher"}})
	if updated.Files["schema.json"] != initial.Files["schema.json"] {
		t.Fatal("removal rewrote the display schema")
	}
	reader := "// depends-on: relationship:PoC_Vault_HasAccountFor\nMATCH ()-[edge:PoC_Vault_HasAccountFor]->() RETURN 'PoC_Vault_Helper_1' AS id, ['PoC_Vault_Helper'] AS kinds, {} AS properties"
	withReader, err := store.Update(map[string]string{"vault_helpers.cypher": reader})
	if err != nil {
		t.Fatal("reader should remain valid after the last producer is removed", err)
	}
	if !reflect.DeepEqual(withReader.RetiredRelationships, updated.RetiredRelationships) {
		t.Fatal("reader should not cancel cleanup", withReader)
	}
}

func TestRemovalPrunesUnusedScratchPrerequisites(t *testing.T) {
	var (
		store    = open(t)
		original = files(t)
	)
	original["scratch.cypher"] = "// depends-on: none\nRETURN 1 AS value"
	original["account_users.cypher"] = strings.Replace(original["account_users.cypher"], "query:vault_helpers.cypher,", "query:vault_helpers.cypher, query:scratch.cypher,", 1)
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.Preview(ruleservice.Change{BaseRevision: initial.Revision, Remove: []string{"account_users.cypher"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Removed, []string{"account_users.cypher", "scratch.cypher"}) {
		t.Fatal("unused prerequisite survived", preview.Removed)
	}
	if len(preview.Plan.Threads) != 1 || len(preview.Plan.Threads[0].Queries) != 1 || preview.Plan.Threads[0].Queries[0].Name != "vault_helpers.cypher" {
		t.Fatal("helper producer not preserved", preview.Plan)
	}
}

// The container only allows writes in its saved-rule directory. Reopening it
// must not depend on a separate writable system temporary folder.
func TestReopenValidatesInsideTheRuleDirectory(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", filepath.Join(directory, "unavailable-temporary-folder"))
	reopened, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	active, err := reopened.Active()
	if err != nil || !reflect.DeepEqual(active, initial) {
		t.Fatal("reopen changed the rules", err)
	}
}
