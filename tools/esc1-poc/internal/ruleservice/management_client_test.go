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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func TestWorkspaceFirstUploadReviewAndStaleSchema(t *testing.T) {
	var store, err = ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uploaded := installSchemas(t, store, files(t))
	workspace := store.Workspace()
	if workspace.Plan.Revision != "" || workspace.Plan.SchemaRevision == "" {
		t.Fatal("first upload needs the installed schema revision")
	}
	draft, err := store.SaveDraft(ruleservice.Change{Files: uploaded})
	if err != nil || !draft.Ready {
		t.Fatal(err, draft.ValidationError)
	}
	change := ruleservice.Change{DraftRevision: draft.Revision, SchemaRevision: workspace.Plan.SchemaRevision}
	preview, err := store.Preview(change)
	if err != nil || len(preview.Plan.Queries) == 0 || len(preview.Dependencies) == 0 {
		t.Fatal("first review did not include its complete plan", err)
	}
	change.ConfirmRevision = preview.Revision
	change.SchemaRevision = strings.Repeat("f", 64)
	if _, err = store.Apply(change); !errors.Is(err, ruleservice.ErrConflict) {
		t.Fatal("stale schema was accepted", err)
	}
	change.SchemaRevision = workspace.Plan.SchemaRevision
	active, err := store.Apply(change)
	if err != nil || active.Revision != preview.Revision {
		t.Fatal("first activation failed", err)
	}
	if store.Draft().Revision != draft.Revision {
		t.Fatal("activation silently discarded the draft")
	}
}

func TestWorkspaceChangeRejectsAmbiguousJSON(t *testing.T) {
	for _, contents := range []string{`null`, `{} {}`, `{"base_revision":"one","base_revision":"two"}`, `{"files":{"PoC/users.cypher":"one","PoC/users.cypher":"two"}}`, `{"destination":"http://elsewhere"}`} {
		if _, err := ruleservice.DecodeChange([]byte(contents)); err == nil {
			t.Fatal("accepted ambiguous change")
		}
	}
}

func TestManagementClientUsesFixedRoutesAndSafeDraftErrors(t *testing.T) {
	var (
		secret = "never-expose-this-literal"
		token  = strings.Repeat("t", 32)
		ctx    = context.Background()
	)
	for _, diagnostic := range []string{
		`output kind "` + secret + `" is invalid`,
		"parser failed near RETURN '" + secret + "'\n" + strings.Repeat("x", 20000),
		"missing dependency " + secret,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/draft" || request.Header.Get("Authorization") != "Bearer "+token {
				t.Error("wrong destination or authentication")
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"files": map[string]string{}, "validation_error": diagnostic})
		}))
		client := ruleservice.Client{BaseURL: server.URL, Token: token}
		draft, err := client.ReadDraft(ctx)
		server.Close()
		if err != nil || draft.ValidationError == "" || strings.Contains(draft.ValidationError, secret) || len(draft.ValidationError) > 500 {
			t.Fatal("unsafe or absent draft guidance", err)
		}
	}
}

func TestManagementClientRejectsBadRepliesAndRedirects(t *testing.T) {
	var token = strings.Repeat("t", 32)
	for _, status := range []int{400, 401, 409, 413, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"error":"secret query text"}`))
		}))
		_, err := (ruleservice.Client{BaseURL: server.URL, Token: token}).Preview(context.Background(), ruleservice.Change{})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe management failure")
		}
	}
	for _, body := range []string{`null`, `{}`, `{"revision":"wrong"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) { _, _ = response.Write([]byte(body)) }))
		client := ruleservice.Client{BaseURL: server.URL, Token: token}
		if _, err := client.ReadDraft(context.Background()); err == nil {
			t.Error("accepted invalid draft response")
		}
		if _, err := client.Preview(context.Background(), ruleservice.Change{}); err == nil {
			t.Error("accepted invalid review response")
		}
		if _, err := client.Apply(context.Background(), ruleservice.Change{ConfirmRevision: "expected"}); err == nil {
			t.Error("accepted wrong activation response")
		}
		server.Close()
	}
	contacted := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { contacted = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, destination.URL, 307)
	}))
	defer redirect.Close()
	_, err := (ruleservice.Client{BaseURL: redirect.URL, Token: token}).ReadDraft(context.Background())
	if err == nil || contacted {
		t.Fatal("management followed a redirect")
	}
}

func TestManagementClientCompleteDraftAndActivationFlow(t *testing.T) {
	var (
		store, err = ruleservice.Open(t.TempDir())
		token      = strings.Repeat("t", 32)
		ctx        = context.Background()
	)
	if err != nil {
		t.Fatal(err)
	}
	uploaded := installSchemas(t, store, files(t))
	handler, err := ruleservice.NewHandler(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := ruleservice.Client{BaseURL: server.URL, Token: token}
	empty, err := client.ReadDraft(ctx)
	if err != nil || empty.Revision != "" {
		t.Fatal("expected empty draft", err)
	}
	draft, err := client.SaveDraft(ctx, ruleservice.Change{Files: uploaded})
	if err != nil || !draft.Ready {
		t.Fatal("draft upload failed", err)
	}
	change := ruleservice.Change{DraftRevision: draft.Revision, SchemaRevision: store.Workspace().Plan.SchemaRevision}
	preview, err := client.Preview(ctx, change)
	if err != nil || len(preview.Plan.Threads) != 0 || len(preview.Dependencies) == 0 {
		t.Fatal("native review is incomplete", err)
	}
	change.ConfirmRevision = preview.Revision
	revision, err := client.Apply(ctx, change)
	if err != nil || revision != preview.Revision {
		t.Fatal("activation was not confirmed", err)
	}
	if err := client.DiscardDraft(ctx, ruleservice.Change{DraftRevision: draft.Revision}); err != nil {
		t.Fatal(err)
	}
	after, err := client.ReadDraft(ctx)
	if err != nil || after.Revision != "" || store.Workspace().Plan.Revision != revision {
		t.Fatal("draft cleanup changed active rules", err)
	}
}
