// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package postprocessing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/stretchr/testify/require"
)

type editorTestDatabase struct {
	reads         map[string]int
	commits       int
	rollbacks     int
	rollbackError error
	queryRows     runner.Rows
}

func (s *editorTestDatabase) Begin(context.Context) (runner.Transaction, error) { return s, nil }
func (s *editorTestDatabase) Read(_ context.Context, text string, _ map[string]any) (runner.Rows, error) {
	s.reads[text]++
	if strings.Contains(text, "NOT (item:MigrationData") {
		return nil, nil
	}
	if s.queryRows != nil && !strings.HasPrefix(text, "MATCH (item)") {
		return s.queryRows, nil
	}
	if strings.HasSuffix(text, "RETURN 1 AS value") {
		return runner.Rows{{"value": int64(1)}}, nil
	}
	if strings.Contains(text, "UNWIND") {
		return runner.Rows{{"result": int64(1)}}, nil
	}
	return nil, nil
}
func (s *editorTestDatabase) Write(context.Context, string, map[string]any) (runner.Rows, error) {
	return nil, errors.New("unexpected graph write")
}
func (s *editorTestDatabase) Commit(context.Context) error   { s.commits++; return nil }
func (s *editorTestDatabase) Rollback(context.Context) error { s.rollbacks++; return s.rollbackError }

func newEditorServiceTest(t *testing.T) (*Service, *serviceTestClient, *editorTestDatabase, EditorTestRequest) {
	t.Helper()
	service, client, _, _ := newServiceTest(t)
	service.editor = newAnalysisEditor()
	database := &editorTestDatabase{reads: map[string]int{}}
	service.executeTest = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		return runner.Test(ctx, database, plan, options)
	}
	contents, err := service.configuration.ReadSchemas(t.Context())
	require.NoError(t, err)
	snapshot, err := ruleservice.ParseSchemas(contents)
	require.NoError(t, err)
	request := EditorTestRequest{Name: "PoC/editor_test.cypher", Text: "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nRETURN 1 AS value", BaseRevision: client.bundle.Revision, SchemaRevision: snapshot.Revision()}
	return service, client, database, request
}

func TestEditorSuccessShowsRowsAndDiscardsChanges(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	version := service.gate.Version()
	result, err := service.TestAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	require.Len(t, result.TestID, 64)
	require.EqualValues(t, 1, result.TotalRows)
	require.Len(t, result.Rows, 1)
	require.Equal(t, version, service.gate.Version())
	require.Equal(t, 1, database.rollbacks)
	require.Zero(t, database.commits)
	require.Equal(t, "discarded", result.Execution.Save)
	_, err = service.currentEditorReceipt(t.Context(), "other", result.TestID)
	require.Error(t, err)
}

func TestEditorReceiptRejectsChangedGraphRulesAndSchema(t *testing.T) {
	for _, change := range []string{"graph", "rules", "schema", "restart"} {
		t.Run(change, func(t *testing.T) {
			service, client, _, request := newEditorServiceTest(t)
			result, err := service.TestAnalysisFile(t.Context(), "owner", request)
			require.NoError(t, err)
			switch change {
			case "graph":
				_, release, err := service.gate.EnterWrite(t.Context())
				require.NoError(t, err)
				release()
			case "rules":
				client.bundle.Revision = strings.Repeat("b", 64)
			case "schema":
				service.configuration.ReadSchemas = func(context.Context) ([]byte, error) {
					return json.Marshal(ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}})
				}
			case "restart":
				service.gate = NewGate()
			}
			_, err = service.currentEditorReceipt(t.Context(), "owner", result.TestID)
			var editorError *EditorError
			require.ErrorAs(t, err, &editorError)
			require.Equal(t, http.StatusConflict, editorError.Status)
		})
	}
}

func TestEditorCannotIssueReceiptAfterUnconfirmedDiscard(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	database.rollbackError = errors.New("private database detail")
	_, err := service.TestAnalysisFile(t.Context(), "owner", request)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private database detail")
	require.Empty(t, service.editor.receipts)
	_, _, err = service.gate.EnterWrite(t.Context())
	require.Error(t, err)
}

func TestEditorStrictInputRejectsRepeatedUnknownAndMissingFields(t *testing.T) {
	_, _, _, request := newEditorServiceTest(t)
	contents, err := json.Marshal(request)
	require.NoError(t, err)
	_, err = DecodeEditorTestRequest(contents)
	require.NoError(t, err)
	// The editor always recalculates, so the old "mode" and "source_id"
	// choices are now unknown fields and must be rejected like any other.
	withField := func(field string) string { return strings.TrimSuffix(string(contents), "}") + "," + field + "}" }
	for _, invalid := range []string{`null`, `{}`, `{"name":"PoC/a.cypher","name":"PoC/b.cypher"}`, withField(`"owner":"other"`), withField(`"mode":"recalculate"`), withField(`"source_id":"` + strings.Repeat("a", 64) + `"`), string(contents) + ` {}`} {
		_, err := DecodeEditorTestRequest([]byte(invalid))
		require.Error(t, err)
	}
}

func TestEditorPreviewDisplaysOnlyNeededPrerequisites(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: query:enabled_cas\nUNWIND $enabled_cas AS ca RETURN ca.ca AS result"
	result, err := service.PreviewAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	require.Greater(t, len(result.Plan.Queries), 1)
	require.Less(t, len(result.Plan.Queries), 14)
	names := map[string]bool{}
	for _, query := range result.Plan.Queries {
		names[query.Name] = true
	}
	require.True(t, names[request.Name])
	require.False(t, names["PoC/esc1.cypher"])
	require.NotEmpty(t, result.Dependencies)
	require.Empty(t, database.reads)
}

// Testing a file that depends on another file must run that earlier file
// again every time, even when an earlier test already calculated it.
func TestEditorAlwaysRecalculatesNeededEarlierFiles(t *testing.T) {
	service, client, database, request := newEditorServiceTest(t)
	_, err := service.TestAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	client.bundle.Files[request.Name] = request.Text
	next := request
	next.Name = "PoC/consumer_edit.cypher"
	next.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: query:editor_test\nUNWIND $editor_test AS row RETURN row.value AS result"
	for attempt := 1; attempt <= 2; attempt++ {
		earlierReads := database.reads[request.Text]
		result, err := service.TestAnalysisFile(t.Context(), "owner", next)
		require.NoError(t, err)
		require.EqualValues(t, 1, result.TotalRows)
		require.Equal(t, earlierReads+1, database.reads[request.Text])
	}
}

func TestEditorHelperOutputAppearsAsRowsWithoutBeingSaved(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	request.Name = "PoC/domain_helpers.cypher"
	request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nRETURN 'PoC_DomainHelper_editor' AS id, ['PoC_DomainHelper'] AS kinds, {name: 'Proposed helper'} AS properties"
	database.queryRows = runner.Rows{{"id": "PoC_DomainHelper_editor", "kinds": []any{"PoC_DomainHelper"}, "properties": map[string]any{"name": "Proposed helper"}}}
	result, err := service.TestAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"id": "PoC_DomainHelper_editor", "kinds": []any{"PoC_DomainHelper"}, "properties": map[string]any{"name": "Proposed helper"}}}, result.Rows)
	require.Zero(t, database.commits)
}

func TestEditorReviewActivationPreservesPendingUploadDraft(t *testing.T) {
	service, client, _, request := newEditorServiceTest(t)
	store, err := ruleservice.Open(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.RefreshSchemas(ruleservice.SchemaSnapshot{Definitions: client.bundle.Schemas}))
	active, err := store.Update(client.bundle.Files)
	require.NoError(t, err)
	pendingName := "PoC/pending_edit.cypher"
	pendingText := "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nRETURN 2 AS value"
	pending, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{pendingName: pendingText}})
	require.NoError(t, err)
	token := strings.Repeat("t", 32)
	handler, err := ruleservice.NewHandler(store, token)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	service.configuration.URL, service.configuration.Token = server.URL, token
	service.client = ruleservice.Client{BaseURL: server.URL, Token: token}
	request.BaseRevision = active.Revision
	result, err := service.TestAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	receipt, receiptError := service.currentEditorReceipt(t.Context(), "owner", result.TestID)
	require.NoError(t, receiptError)
	_, directPreviewError := store.Preview(editorReceiptChange(receipt))
	require.NoError(t, directPreviewError)
	review, err := service.ReviewAnalysisFile(t.Context(), "owner", result.TestID)
	require.NoError(t, err)
	_, err = service.ActivateAnalysisFile(t.Context(), "owner", result.TestID, strings.Repeat("b", 64))
	require.Error(t, err)
	revision, err := service.ActivateAnalysisFile(t.Context(), "owner", result.TestID, review.Revision)
	require.NoError(t, err)
	require.Equal(t, review.Revision, revision)
	saved, err := store.Active()
	require.NoError(t, err)
	require.Equal(t, request.Text, saved.Files[request.Name])
	require.NotContains(t, saved.Files, pendingName)
	require.Len(t, saved.Files, len(active.Files)+1)
	draft := store.Draft()
	require.Equal(t, pending.Revision, draft.Revision)
	require.Equal(t, pending.BaseRevision, draft.BaseRevision)
	require.Equal(t, pending.Files, draft.Files)
	_, err = service.ActivateAnalysisFile(t.Context(), "owner", result.TestID, review.Revision)
	require.Error(t, err)
}
