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
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/stretchr/testify/require"
)

func TestWorkspacePreservesOriginalFilesAndChoice(t *testing.T) {
	var store = open(t)
	initial, err := store.Update(files(t))
	require.NoError(t, err)
	var change = ruleservice.Change{BaseRevision: initial.Revision, Disable: []string{"PoC_Vault/vault_helpers.cypher"}}
	preview, err := store.Preview(change)
	require.NoError(t, err)
	change.ConfirmRevision = preview.Revision
	_, err = store.Apply(change)
	require.NoError(t, err)
	view := store.Workspace()
	require.Len(t, view.Dependencies, 1)
	require.Empty(t, view.Plan.Threads)
	for _, query := range view.Plan.Queries {
		require.Equal(t, initial.Files[query.Name], query.Text)
		require.False(t, query.Enabled)
		require.Equal(t, query.Name != "PoC_Vault/vault_helpers.cypher", query.AuthorEnabled)
		require.NotEmpty(t, query.DisabledReason)
	}
	// Changing a returned picture cannot change the stored rules.
	view.Plan.Queries[0].Text = "not an uploaded query"
	require.NotEqual(t, view.Plan.Queries[0].Text, store.Workspace().Plan.Queries[0].Text)
}

func TestWorkspaceReadsRemainConsistentDuringActivation(t *testing.T) {
	var (
		store  = open(t)
		source = files(t)
		wait   sync.WaitGroup
	)
	initial, err := store.Update(source)
	require.NoError(t, err)
	source["PoC_Vault/account_users.cypher"] += "\n// revised explanation\n"
	second, err := store.Update(source)
	require.NoError(t, err)
	var originals = map[string]map[string]string{initial.Revision: initial.Files, second.Revision: second.Files}
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < 15; index++ {
			var selected = initial.Files
			if index%2 == 0 {
				selected = second.Files
			}
			if _, err := store.Update(selected); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for index := 0; index < 30; index++ {
		view := store.Workspace()
		for _, query := range view.Plan.Queries {
			require.Equal(t, originals[view.Plan.Revision][query.Name], query.Text)
		}
	}
	wait.Wait()
}

func TestWorkspaceHTTPOnlyAllowsAuthenticatedReads(t *testing.T) {
	var (
		store = open(t)
		token = strings.Repeat("t", 32)
	)
	handler, err := ruleservice.NewHandler(store, token)
	require.NoError(t, err)
	require.Equal(t, 401, exchange(handler, "GET", "/v1/workspace", "", nil).Code)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		require.Equal(t, 405, exchange(handler, method, "/v1/workspace", token, nil).Code)
	}
	client := localClient(handler)
	client.Token = token
	view, err := client.Workspace(context.Background())
	require.NoError(t, err)
	require.Empty(t, view.Plan.Queries)
	require.Equal(t, "unavailable", view.Status.State)
	require.False(t, view.ObservedAt.IsZero())
	require.Equal(t, "no-store", exchange(handler, http.MethodGet, "/v1/workspace", token, nil).Header().Get("Cache-Control"))
}

func TestWorkspaceEnvironmentIconMetadataUsesOneDistinctKind(t *testing.T) {
	for _, scenario := range []struct{ name, environments, expected string }{
		{"absent", "null", ""}, {"single", `[{"environment_kind":"Domain"}]`, "Domain"},
		{"repeated", `[{"environment_kind":"Domain"},{"environment_kind":"Domain"}]`, "Domain"},
		{"multiple", `[{"environment_kind":"Domain"},{"environment_kind":"Tenant"}]`, ""},
		{"invalid", `{}`, ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var plan = extension.Plan{Definitions: []extension.Definition{{Schema: extension.Metadata{Namespace: "PoC"}, Environments: json.RawMessage(scenario.environments)}}}
			view := ruleservice.BuildView(ruleservice.Bundle{}, plan)
			require.Len(t, view.Extensions, 1)
			require.Equal(t, scenario.expected, view.Extensions[0].EnvironmentKind)
		})
	}
}
