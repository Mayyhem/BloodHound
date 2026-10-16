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
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

const editorWorkingCopyToken = "private-token-012345678901234567890123456789"

func TestEditorWorkingCopySaveResumeAndDelete(t *testing.T) {
	var (
		directory = t.TempDir()
		owner     = "native-user-1"
		name      = "PoC_Vault/unfinished_query.cypher"
	)
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveEditorWorkingCopy(owner, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (", BaseRevision: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision == "" || saved.Text != "MATCH (" {
		t.Fatalf("saved unfinished copy: %+v", saved)
	}
	resumed, err := store.EditorWorkingCopy(owner, name)
	if err != nil || resumed != saved {
		t.Fatalf("resumed copy: %+v, %v", resumed, err)
	}
	copies, err := store.EditorWorkingCopies(owner)
	if err != nil || len(copies) != 1 || copies[0].Name != name || copies[0].Revision != saved.Revision {
		t.Fatalf("saved copy list: %+v, %v", copies, err)
	}
	updated, err := store.SaveEditorWorkingCopy(owner, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH () RETURN", BaseRevision: saved.BaseRevision, Revision: saved.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == saved.Revision {
		t.Fatal("saving changed text retained the old revision")
	}
	if _, err := store.SaveEditorWorkingCopy(owner, ruleservice.EditorWorkingCopy{Name: name, Text: "old tab", Revision: saved.Revision}); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatalf("stale save error: %v", err)
	}
	if err := store.DeleteEditorWorkingCopy(owner, name, saved.Revision); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatalf("stale delete error: %v", err)
	}
	if err := store.DeleteEditorWorkingCopy(owner, name, updated.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EditorWorkingCopy(owner, name); !errors.Is(err, ruleservice.ErrNoEditorWorkingCopy) {
		t.Fatalf("deleted copy error: %v", err)
	}
}

func TestEditorWorkingCopiesArePrivatePersistentAndSeparate(t *testing.T) {
	var (
		directory = t.TempDir()
		ownerOne  = "native-user-1"
		ownerTwo  = "native-user-2"
		name      = "PoC_Vault/account_users.cypher"
	)
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{name: active.Files[name]}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SaveEditorWorkingCopy(ownerOne, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (first", BaseRevision: active.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveEditorWorkingCopy(ownerTwo, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (second", BaseRevision: active.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EditorWorkingCopy(ownerTwo, name); err != nil {
		t.Fatal(err)
	}
	if copies, err := store.EditorWorkingCopies(ownerOne); err != nil || len(copies) != 1 || copies[0].Revision != first.Revision {
		t.Fatalf("owner one copies: %+v, %v", copies, err)
	}
	if restored, err := ruleservice.Open(directory); err != nil {
		t.Fatal(err)
	} else if resumed, err := restored.EditorWorkingCopy(ownerOne, name); err != nil || resumed.Text != "MATCH (first" {
		t.Fatalf("restored working copy: %+v, %v", resumed, err)
	} else if preservedDraft := restored.Draft(); preservedDraft.Revision != draft.Revision || preservedDraft.Files[name] != draft.Files[name] {
		t.Fatal("working copy changed the upload draft")
	} else if preservedActive, err := restored.Active(); err != nil || preservedActive.Revision != active.Revision {
		t.Fatalf("working copy changed active rules: %+v, %v", preservedActive, err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "editor-working-copies"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("working copy files: %+v, %v", entries, err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ownerOne) || strings.Contains(entry.Name(), name) {
			t.Fatal("working copy path exposed owner or filename")
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("working copy file permissions: %v, %v", info.Mode(), err)
		}
	}
}

func TestEditorWorkingCopyHTTP(t *testing.T) {
	store := open(t)
	handler, err := ruleservice.NewHandler(store, editorWorkingCopyToken)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]string{"owner": "native-user-1", "name": "PoC_Vault/unfinished_query.cypher", "text": "MATCH (", "base_revision": strings.Repeat("b", 64)}
	contents, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", editorWorkingCopyToken, contents); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	response := exchange(handler, http.MethodGet, "/v1/editor/working-copies?owner=native-user-1", editorWorkingCopyToken, nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "MATCH (") {
		t.Fatal("list did not hide editor text", response.Code, response.Body.String())
	}
	var copies []ruleservice.EditorWorkingCopySummary
	if err := json.Unmarshal(response.Body.Bytes(), &copies); err != nil || len(copies) != 1 {
		t.Fatalf("list response: %+v, %v", copies, err)
	}
	response = exchange(handler, http.MethodGet, "/v1/editor/working-copies?owner=native-user-1&name=PoC_Vault%2Funfinished_query.cypher", editorWorkingCopyToken, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "MATCH (") {
		t.Fatal("resume response", response.Code, response.Body.String())
	}
	if response := exchange(handler, http.MethodGet, "/v1/editor/working-copies?owner=native-user-1&owner=native-user-2", editorWorkingCopyToken, nil); response.Code != http.StatusBadRequest {
		t.Fatal("duplicate query owner", response.Code)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", editorWorkingCopyToken, []byte(`{"owner":"native-user-1","owner":"native-user-2"}`)); response.Code != http.StatusBadRequest {
		t.Fatal("duplicate JSON key", response.Code)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", editorWorkingCopyToken, []byte(`{"owner":"native-user-1","unexpected":true}`)); response.Code != http.StatusBadRequest {
		t.Fatal("unknown JSON key", response.Code)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", editorWorkingCopyToken, []byte(`{} {}`)); response.Code != http.StatusBadRequest {
		t.Fatal("trailing JSON", response.Code)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", "wrong-token", contents); response.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated request", response.Code)
	}
	if response := exchange(handler, http.MethodPut, "/v1/editor/working-copies", editorWorkingCopyToken, []byte(strings.Repeat("x", ruleservice.MaxBundleBytes+1))); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("large request", response.Code)
	}
}
