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

func TestEditorWorkingCopyClientUsesFixedRequestAndOwner(t *testing.T) {
	var (
		token = strings.Repeat("t", 32)
		owner = "native user / one"
		name  = "PoC_Vault/unfinished_query.cypher"
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/editor/working-copies" || request.Header.Get("Authorization") != "Bearer "+token {
			t.Error("client did not use its fixed request and bearer token")
		}
		if request.Method == http.MethodGet {
			if request.URL.RawQuery != "name=PoC_Vault%2Funfinished_query.cypher&owner=native+user+%2F+one" {
				t.Error("read did not encode the owner and name")
			}
			_ = json.NewEncoder(writer).Encode(ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (", BaseRevision: strings.Repeat("a", 64), Revision: strings.Repeat("b", 64)})
			return
		}
		var saved map[string]string
		if request.Method != http.MethodPut || json.NewDecoder(request.Body).Decode(&saved) != nil || saved["owner"] != owner {
			t.Error("save did not inject trusted owner")
		}
		_ = json.NewEncoder(writer).Encode(ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (", BaseRevision: strings.Repeat("a", 64), Revision: strings.Repeat("c", 64)})
	}))
	defer server.Close()
	client := ruleservice.Client{BaseURL: server.URL, Token: token}
	copy, err := client.ReadEditorWorkingCopy(context.Background(), owner, name)
	if err != nil || copy.Text != "MATCH (" {
		t.Fatal(copy, err)
	}
	copy, err = client.SaveEditorWorkingCopy(context.Background(), owner, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH (", BaseRevision: strings.Repeat("a", 64)})
	if err != nil || copy.Revision != strings.Repeat("c", 64) {
		t.Fatal(copy, err)
	}
}

func TestEditorWorkingCopyClientUsesServiceAndSafeErrors(t *testing.T) {
	var (
		store, err = ruleservice.Open(t.TempDir())
		token      = strings.Repeat("t", 32)
		owner      = "native-user"
		name       = "PoC_Vault/unfinished_query.cypher"
	)
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
	saved, err := client.SaveEditorWorkingCopy(context.Background(), owner, ruleservice.EditorWorkingCopy{Name: name, Text: "MATCH ("})
	if err != nil || saved.Revision == "" {
		t.Fatal(saved, err)
	}
	if list, err := client.ListEditorWorkingCopies(context.Background(), owner); err != nil || len(list) != 1 || list[0].Name != name {
		t.Fatal(list, err)
	}
	if resumed, err := client.ReadEditorWorkingCopy(context.Background(), owner, name); err != nil || resumed != saved {
		t.Fatal(resumed, err)
	}
	if err := client.DeleteEditorWorkingCopy(context.Background(), owner, name, strings.Repeat("a", 64)); err == nil {
		t.Fatal("stale delete succeeded")
	} else {
		var serviceError *ruleservice.EditorWorkingCopyError
		if !errors.As(err, &serviceError) || serviceError.Status != http.StatusConflict {
			t.Fatal(err)
		}
	}
	if _, err := client.ReadEditorWorkingCopy(context.Background(), owner, "PoC_Vault/missing_query.cypher"); err == nil {
		t.Fatal("missing copy succeeded")
	} else {
		var serviceError *ruleservice.EditorWorkingCopyError
		if !errors.As(err, &serviceError) || serviceError.Status != http.StatusNotFound {
			t.Fatal(err)
		}
	}
}

func TestEditorWorkingCopyClientRejectsUnsafeReplies(t *testing.T) {
	var token = strings.Repeat("t", 32)
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "raw service error", status: http.StatusBadRequest, body: `{"error":"secret unfinished query"}`},
		{name: "malformed response", status: http.StatusOK, body: `not JSON`},
		{name: "null list", status: http.StatusOK, body: `null`},
		{name: "duplicate response", status: http.StatusOK, body: `[{"name":"PoC_Vault/a.cypher","name":"PoC_Vault/b.cypher"}]`},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", ruleservice.MaxBundleBytes+1)},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(response.status)
				_, _ = writer.Write([]byte(response.body))
			}))
			defer server.Close()
			_, err := (ruleservice.Client{BaseURL: server.URL, Token: token}).ListEditorWorkingCopies(context.Background(), "native-user")
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe or missing error", err)
			}
		})
	}
	contacted := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { contacted = true }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := (ruleservice.Client{BaseURL: redirect.URL, Token: token}).ListEditorWorkingCopies(context.Background(), "native-user")
	if err == nil || contacted {
		t.Fatal("redirect was accepted or followed")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (ruleservice.Client{BaseURL: destination.URL, Token: token}).ListEditorWorkingCopies(canceled, "native-user")
	if err == nil || strings.Contains(err.Error(), "context canceled") {
		t.Fatal("cancellation leaked an unsafe error", err)
	}
}

func TestDecodeEditorWorkingCopyRejectsBrowserOwnerAndAmbiguousJSON(t *testing.T) {
	for _, contents := range []string{
		`null`,
		`{} {}`,
		`{"name":"PoC_Vault/a.cypher","name":"PoC_Vault/b.cypher"}`,
		`{"owner":"spoofed","name":"PoC_Vault/a.cypher","text":"MATCH ("}`,
		`{"name":"PoC_Vault/a.cypher","text":"MATCH (","unexpected":true}`,
	} {
		if _, err := ruleservice.DecodeEditorWorkingCopy([]byte(contents)); err == nil {
			t.Fatal("accepted unsafe browser request", contents)
		}
	}
	copy, err := ruleservice.DecodeEditorWorkingCopy([]byte(`{"name":"PoC_Vault/a.cypher","text":"MATCH ("}`))
	if err != nil || copy.Name != "PoC_Vault/a.cypher" {
		t.Fatal(copy, err)
	}
}
