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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func files(t *testing.T) map[string]string {
	t.Helper()
	var result = map[string]string{}
	entries, err := os.ReadDir("../../examples/vault/extension")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "schema.json" || strings.HasSuffix(entry.Name(), ".cypher") {
			contents, err := os.ReadFile(filepath.Join("../../examples/vault/extension", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".cypher") {
				var keyError error
				name, keyError = extension.QueryFileKey(name, string(contents))
				if keyError != nil {
					t.Fatal(keyError)
				}
			}
			result[name] = string(contents)
		}
	}
	return result
}
func open(t *testing.T) *ruleservice.Store {
	t.Helper()
	store, err := ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSnapshotsAndPersistence(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Active(); !errors.Is(err, ruleservice.ErrNoActive) {
		t.Fatal(err)
	}
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ruleservice.ValidateBundle(initial); err != nil {
		t.Fatal(err)
	}
	name := "PoC_Vault/account_users.cypher"
	changed, err := store.Update(map[string]string{name: initial.Files[name] + "\n// replacement\n"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision == initial.Revision || strings.Contains(initial.Files[name], "replacement") {
		t.Fatal("snapshot changed")
	}
	changed.Files[name] = "tampered"
	saved, err := store.Active()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Files[name] == "tampered" {
		t.Fatal("caller mutated store")
	}
	if err := store.Report(ruleservice.Status{State: "success", Event: "analysis-completed", Revision: saved.Revision, Message: "Applied."}); err != nil {
		t.Fatal(err)
	}
	restored, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.Active()
	if err != nil || recovered.Revision != saved.Revision {
		t.Fatal(err, recovered)
	}
	if restored.LatestStatus().UpdatedAt.IsZero() || restored.LatestStatus().State != "success" {
		t.Fatal("status not restored")
	}
	for _, name := range []string{"active.json", "status.json"} {
		entry, err := os.Stat(filepath.Join(directory, name))
		if err != nil || entry.Mode().Perm() != 0600 {
			t.Fatal(err, entry)
		}
	}
	entry, err := os.Stat(directory)
	if err != nil || entry.Mode().Perm() != 0700 {
		t.Fatal(err, entry)
	}
}

func TestRejectedUpdatesPreserveActive(t *testing.T) {
	store := open(t)
	original := files(t)
	initial, err := store.Update(original)
	if err != nil {
		t.Fatal(err)
	}
	for name, update := range map[string]map[string]string{
		"path traversal":   {"../query.cypher": "RETURN 1"},
		"write":            {"PoC_Vault/account_users.cypher": "CREATE (item) RETURN item"},
		"oversize":         {"PoC_Vault/account_users.cypher": strings.Repeat("x", ruleservice.MaxFileBytes+1)},
		"duplicate schema": {"schema.json": `{"schema":{},"schema":{}}`},
		"renamed schema":   {"schema.json": strings.Replace(original["schema.json"], `"name": "vault_poc"`, `"name": "different"`, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Update(update); err == nil {
				t.Fatal("accepted invalid update")
			}
			active, err := store.Active()
			if err != nil || active.Revision != initial.Revision {
				t.Fatal("lost active snapshot", err)
			}
		})
	}

}

func TestFailedDiskReplacementAndConcurrentSnapshots(t *testing.T) {
	directory := t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for count := 0; count < 20; count++ {
				snapshot, err := store.Active()
				if err != nil {
					t.Error(err)
					return
				}
				snapshot.Files["schema.json"] = "caller copy"
			}
		}()
	}
	workers.Wait()
	if err := os.Remove(filepath.Join(directory, "active.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "active.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(map[string]string{"PoC_Vault/account_users.cypher": initial.Files["PoC_Vault/account_users.cypher"] + "\n// changed"}); err == nil {
		t.Fatal("expected disk failure")
	}
	snapshot, err := store.Active()
	if err != nil || snapshot.Revision != initial.Revision {
		t.Fatal("failed write activated", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("temporary validation files leaked", entries)
	}
}

func TestQueryOutputAndPolicyMayChange(t *testing.T) {
	var store = open(t)
	initialFiles := files(t)
	initial, err := store.Update(initialFiles)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(initialFiles["PoC_Vault/account_users.cypher"], "keep-existing", "merge-properties")
	updated, err := store.Update(map[string]string{"PoC_Vault/account_users.cypher": changed})
	if err != nil || updated.Revision == initial.Revision {
		t.Fatal("policy change rejected", err)
	}
	replacement := map[string]string{}
	for name, contents := range initialFiles {
		replacement[name] = strings.ReplaceAll(contents, "PoC_Vault_HasAccountFor", "PoC_Vault_Different")
	}
	updated, err = store.Update(replacement)
	if err != nil || updated.Revision == initial.Revision {
		t.Fatal("output change rejected", err)
	}
	if len(updated.RetiredRelationships) != 1 || updated.RetiredRelationships[0] != "PoC_Vault_HasAccountFor" {
		t.Fatal("previous output was not retired")
	}
	if endpoints := updated.RetiredEndpoints["PoC_Vault_HasAccountFor"]; len(endpoints) != 1 || endpoints[0].SourceKind != "PoC_Vault_Container" || endpoints[0].TargetKind != "User" {
		t.Fatal("renaming an output must retain the previous cleanup scope", endpoints)
	}
}
func TestCorruptPersistedStateFailsClosed(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "active.json"), []byte(`{"revision":"bad","files":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ruleservice.Open(directory); err == nil {
		t.Fatal("corrupt rules treated as absent")
	}
}

func TestInitialUploadRequiresCompleteBundle(t *testing.T) {
	store := open(t)
	initial := files(t)
	if _, err := store.Update(map[string]string{"schema.json": initial["schema.json"], "PoC_Vault/account_users.cypher": initial["PoC_Vault/account_users.cypher"]}); err == nil {
		t.Fatal("accepted missing queries")
	}
	if _, err := store.Update(map[string]string{"PoC_Vault/account_users.cypher": initial["PoC_Vault/account_users.cypher"]}); err == nil {
		t.Fatal("accepted missing schema")
	}
	if _, err := store.Active(); !errors.Is(err, ruleservice.ErrNoActive) {
		t.Fatal("partial bundle activated", err)
	}
}

func TestAddingProducerAndRemovingItButRejectingRename(t *testing.T) {
	store := open(t)
	initialFiles := files(t)
	if _, err := store.Update(initialFiles); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(initialFiles["schema.json"]), &schema); err != nil {
		t.Fatal(err)
	}
	relationships := schema["relationship_kinds"].([]any)
	newBinding := map[string]any{"name": "PoC_Vault_Additional", "compatible_kinds": relationships[1].(map[string]any)["compatible_kinds"], "description": "Additional calculated edge"}
	schema["relationship_kinds"] = append(relationships, newBinding)
	contents, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.Update(map[string]string{"schema.json": string(contents), "PoC_Vault/additional.cypher": "// namespace: PoC_Vault\n// version: v1.0.0\n// depends-on: none\nRETURN 'source' AS source, 'target' AS target, 'PoC_Vault_Additional' AS kind"})
	if err != nil {
		t.Fatal("new producer rejected", err)
	}
	removed := applyChange(t, store, ruleservice.Change{BaseRevision: added.Revision, Remove: []string{"PoC_Vault/additional.cypher"}})
	if _, exists := removed.Files["PoC_Vault/additional.cypher"]; exists {
		t.Fatal("unused producer remains")
	}
	if removed.Revision == added.Revision {
		t.Fatal("removal kept old revision")
	}
	schema["schema"].(map[string]any)["name"] = "renamed_schema"
	contents, err = json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(map[string]string{"schema.json": string(contents)}); err == nil {
		t.Fatal("schema name change accepted")
	}
	active, err := store.Active()
	if err != nil || active.Revision != removed.Revision {
		t.Fatal("rejected update changed active", err)
	}
}

// installSchemas represents separate successful OpenGraph Management uploads.
// The returned map contains only files accepted by the analysis upload page.
func installSchemas(t *testing.T, store *ruleservice.Store, files map[string]string) map[string]string {
	t.Helper()
	var (
		byNamespace = map[string]ruleservice.InstalledSchema{}
		queries     = map[string]string{}
	)
	if active, err := store.Active(); err == nil {
		for _, schema := range active.Schemas {
			byNamespace[schema.Namespace] = schema
		}
	}
	for name, contents := range files {
		if !extension.IsSchemaFile(name) {
			queries[name] = contents
			continue
		}
		definition, err := extension.ParseDefinition([]byte(contents))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(definition.Schema.Namespace))
		byNamespace[definition.Schema.Namespace] = ruleservice.InstalledSchema{
			ID: int32(binary.BigEndian.Uint32(digest[:4]) & 0x7fffffff), Name: definition.Schema.Name,
			Namespace: definition.Schema.Namespace, Version: definition.Schema.Version, Definition: json.RawMessage(contents),
		}
	}
	snapshot := ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}}
	for _, schema := range byNamespace {
		snapshot.Definitions = append(snapshot.Definitions, schema)
	}
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	return queries
}

func TestOldUnassociatedFilesRequireExplicitConversion(t *testing.T) {
	var (
		directory = t.TempDir()
		path      = filepath.Join(directory, "active.json")
		contents  = []byte(`{"revision":"old","files":{"users.cypher":"RETURN 1"}}`)
	)
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ruleservice.Open(directory); err == nil || !strings.Contains(err.Error(), "explicit extension associations") {
		t.Fatalf("expected conversion instructions, got %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(contents) {
		t.Fatal("opening old state must preserve its exact contents")
	}
}
