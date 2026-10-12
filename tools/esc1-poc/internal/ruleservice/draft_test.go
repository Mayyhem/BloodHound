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
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

// Both schemas refer to the other's collected node kind. Neither calculation
// needs the other's new answer, so there is no circular execution requirement.
func mutualSchemaFiles(name, other string) map[string]string {
	return map[string]string{
		name + ".schema.json": fmt.Sprintf(`{
            "schema":{"name":%q,"display_name":%q,"namespace":"PoC_%s","version":"v1"},
            "node_kinds":[{"name":"PoC_%s_Node"}],
            "relationship_kinds":[{"name":"PoC_%s_Link","compatible_kinds":[{"source_kind":"PoC_%s_Node","target_kind":"PoC_%s_Node"}]}]
        }`, name, name, name, name, name, name, other),
		name + ".cypher": fmt.Sprintf("// depends-on: node:PoC_%s_Node, node:PoC_%s_Node\nRETURN 'source' AS source, 'target' AS target, 'PoC_%s_Link' AS kind", name, other, name),
	}
}

func TestDraftMissingSchemaPersistsAndActivatesInEitherUploadOrder(t *testing.T) {
	for _, first := range []string{"first", "second"} {
		t.Run(first, func(t *testing.T) {
			var (
				directory  = t.TempDir()
				other      = "second"
				store, err = ruleservice.Open(directory)
			)
			if err != nil {
				t.Fatal(err)
			}
			if first == "second" {
				other = "first"
			}
			active, err := store.Update(files(t))
			if err != nil {
				t.Fatal(err)
			}
			draft, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, Files: mutualSchemaFiles(first, other)})
			if err != nil {
				t.Fatal(err)
			}
			if draft.Ready || !strings.Contains(draft.ValidationError, "PoC_"+other+"_Node") || !strings.Contains(draft.ValidationError, "include the schema") {
				t.Fatal("missing schema was not explained", draft.ValidationError)
			}
			if _, err := store.Apply(ruleservice.Change{BaseRevision: active.Revision, DraftRevision: draft.Revision, ConfirmRevision: "unreviewed"}); err == nil {
				t.Fatal("incomplete draft activated")
			}
			store, err = ruleservice.Open(directory)
			if err != nil {
				t.Fatal(err)
			}
			restored := store.Draft()
			if !reflect.DeepEqual(restored, draft) {
				t.Fatal("draft did not survive restart")
			}
			unchanged, err := store.Active()
			if err != nil || !reflect.DeepEqual(unchanged, active) {
				t.Fatal("draft changed active rules", err)
			}
			draft, err = store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, DraftRevision: draft.Revision, Files: mutualSchemaFiles(other, first)})
			if err != nil || !draft.Ready {
				t.Fatal("complete draft not ready", err, draft.ValidationError)
			}
			if current, _ := store.Active(); current.Revision != active.Revision {
				t.Fatal("saving completed draft activated it")
			}
			change := ruleservice.Change{BaseRevision: active.Revision, DraftRevision: draft.Revision}
			review, err := store.Preview(change)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Apply(change); !errors.Is(err, ruleservice.ErrConflict) {
				t.Fatal("activation skipped review confirmation", err)
			}
			change.ConfirmRevision = review.Revision
			applied, err := store.Apply(change)
			if err != nil {
				t.Fatal(err)
			}
			if applied.Revision != review.Revision || len(applied.Files) != len(active.Files)+4 {
				t.Fatal("activated a different set of files")
			}
			if err := store.DiscardDraft(draft.Revision); err != nil {
				t.Fatal(err)
			}
			store, err = ruleservice.Open(directory)
			if err != nil || store.Draft().Revision != "" {
				t.Fatal("discarded draft returned after restart", err)
			}
			if current, _ := store.Active(); current.Revision != applied.Revision {
				t.Fatal("discarding changed active rules")
			}
		})
	}
}

func TestDraftRequiresFreshRevisionsAndOverwriteChoices(t *testing.T) {
	var store = open(t)
	active, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{"account_users.cypher": active.Files["account_users.cypher"] + "\n// draft"}}
	if _, err := store.SaveDraft(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("replacement did not require an overwrite choice", err)
	}
	change.Overwrite = []string{"account_users.cypher"}
	draft, err := store.SaveDraft(change)
	if err != nil || !draft.Ready {
		t.Fatal(err, draft.ValidationError)
	}
	if _, err := store.SaveDraft(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("stale draft revision accepted", err)
	}
	if err := store.DiscardDraft("stale"); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("stale discard accepted", err)
	}
	copy := store.Draft()
	copy.Files["account_users.cypher"] = "changed"
	copy.Overwrite[0] = "changed"
	if !reflect.DeepEqual(store.Draft(), draft) {
		t.Fatal("caller changed stored draft")
	}
	if _, err := store.Update(map[string]string{"account_users.cypher": active.Files["account_users.cypher"] + "\n// independent active edit"}); err != nil {
		t.Fatal(err)
	}
	if store.Draft().Ready || !strings.Contains(store.Draft().ValidationError, "Active rules changed") {
		t.Fatal("stale base was not reported")
	}
	if _, err := store.Preview(ruleservice.Change{BaseRevision: store.View().Revision, DraftRevision: draft.Revision}); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("draft overwrote newer active rules", err)
	}
}

func TestDraftAcceptsQueryBeforeSchemaAndRejectsUnsafeFiles(t *testing.T) {
	var store = open(t)
	draft, err := store.SaveDraft(ruleservice.Change{Files: map[string]string{"future.cypher": "// depends-on: none\nRETURN 1"}})
	if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "schema") {
		t.Fatal(err, draft.ValidationError)
	}
	if _, err := store.Active(); !errors.Is(err, ruleservice.ErrNoActive) {
		t.Fatal("draft reached the worker", err)
	}
	for _, files := range []map[string]string{
		{"../outside.cypher": "RETURN 1"},
		{"large.cypher": strings.Repeat("x", ruleservice.MaxFileBytes+1)},
		{"broken.schema.json": `{"schema":{},"schema":{}}`},
	} {
		if _, err := store.SaveDraft(ruleservice.Change{DraftRevision: draft.Revision, Files: files}); err == nil {
			t.Fatal("unsafe file accepted")
		}
		if store.Draft().Revision != draft.Revision {
			t.Fatal("failed save replaced draft")
		}
	}
}

func TestDraftHTTPAndAuthentication(t *testing.T) {
	var (
		store        = open(t)
		token        = strings.Repeat("s", 32)
		handler, err = ruleservice.NewHandler(store, token)
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if response := exchange(handler, method, "/v1/draft", "", nil); response.Code != http.StatusUnauthorized {
			t.Fatal("draft access bypassed authentication", response.Code)
		}
	}
	contents, err := json.Marshal(ruleservice.Change{Files: mutualSchemaFiles("first", "second")})
	if err != nil {
		t.Fatal(err)
	}
	response := exchange(handler, "PUT", "/v1/draft", token, contents)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var draft ruleservice.DraftView
	if err := json.Unmarshal(response.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Ready || draft.Revision == "" {
		t.Fatal("incomplete draft status lost")
	}
	if response := exchange(handler, "GET", "/v1/rules", token, nil); response.Code != http.StatusNotFound {
		t.Fatal("worker received draft")
	}
	response = exchange(handler, "GET", "/v1/draft", token, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), draft.Revision) {
		t.Fatal("draft not readable")
	}
	contents, err = json.Marshal(ruleservice.Change{DraftRevision: draft.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if response := exchange(handler, "DELETE", "/v1/draft", token, contents); response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestDraftStorageFailurePreservesActiveAndPreviousDraft(t *testing.T) {
	var (
		directory  = t.TempDir()
		store, err = ruleservice.Open(directory)
	)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, Files: mutualSchemaFiles("first", "second")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "draft.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "draft.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, DraftRevision: draft.Revision, Files: mutualSchemaFiles("second", "first")}); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if store.Draft().Revision != draft.Revision {
		t.Fatal("failed persistence changed in-memory draft")
	}
	if current, _ := store.Active(); !reflect.DeepEqual(current, active) {
		t.Fatal("failed persistence changed active rules")
	}
}

func TestCompleteDraftWithCircularCalculationsCannotActivate(t *testing.T) {
	var (
		store = open(t)
		files = mutualSchemaFiles("first", "second")
	)
	for name, contents := range mutualSchemaFiles("second", "first") {
		files[name] = contents
	}
	files["first.cypher"] = strings.Replace(files["first.cypher"], "// depends-on:", "// depends-on: relationship:PoC_second_Link,", 1)
	files["second.cypher"] = strings.Replace(files["second.cypher"], "// depends-on:", "// depends-on: relationship:PoC_first_Link,", 1)
	draft, err := store.SaveDraft(ruleservice.Change{Files: files})
	if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "cycle") {
		t.Fatal("circular calculations were not kept inactive", err, draft.ValidationError)
	}
	if _, err := store.Apply(ruleservice.Change{DraftRevision: draft.Revision, ConfirmRevision: draft.Revision}); err == nil {
		t.Fatal("circular calculations activated")
	}
}

// Native endpoints do not hide a missing schema for a declared graph input.
// Collected relationships may be read in both directions across extensions;
// this is different from each producer waiting for the other's new answer.
func TestDraftWaitsForSchemaOfDeclaredGraphInput(t *testing.T) {
	for _, inputType := range []string{"node", "relationship"} {
		t.Run(inputType, func(t *testing.T) {
			var (
				store   = open(t)
				uploads = map[string]map[string]string{}
			)
			for name, other := range map[string]string{"first": "second", "second": "first"} {
				var (
					declared   = mutualSchemaFiles(name, other)
					definition map[string]any
				)
				if err := json.Unmarshal([]byte(declared[name+".schema.json"]), &definition); err != nil {
					t.Fatal(err)
				}
				definition["relationship_kinds"] = []any{
					map[string]any{"name": "PoC_" + name + "_Link", "compatible_kinds": []any{map[string]any{"source_kind": "User", "target_kind": "Domain"}}},
					map[string]any{"name": "PoC_" + name + "_Collected", "compatible_kinds": []any{map[string]any{"source_kind": "User", "target_kind": "Domain"}}},
				}
				encoded, err := json.Marshal(definition)
				if err != nil {
					t.Fatal(err)
				}
				declared[name+".schema.json"] = string(encoded)
				inputKind := "PoC_" + other + "_Node"
				if inputType == "relationship" {
					inputKind = "PoC_" + other + "_Collected"
				}
				declared[name+".cypher"] = "// depends-on: " + inputType + ":" + inputKind + "\nRETURN 'user' AS source, 'domain' AS target, 'PoC_" + name + "_Link' AS kind"
				uploads[name] = declared
			}
			draft, err := store.SaveDraft(ruleservice.Change{Files: uploads["first"]})
			if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "depends-on references unknown "+inputType+" kind") {
				t.Fatal(err, draft.ValidationError)
			}
			draft, err = store.SaveDraft(ruleservice.Change{DraftRevision: draft.Revision, Files: uploads["second"]})
			if err != nil || !draft.Ready {
				t.Fatal("mutual references to collected kinds failed", err, draft.ValidationError)
			}
		})
	}
}
