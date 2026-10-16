/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
//
// SPDX-License-Identifier: Apache-2.0
*/

package v2_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	v2 "github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/stretchr/testify/require"
)

func editorWorkingCopyRequest(method, path, body string, owner uuid.UUID) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	context := &bhctx.Context{AuthCtx: auth.Context{Owner: model.User{Unique: model.Unique{ID: owner}}}}
	return request.WithContext(context.ConstructGoContext())
}

func TestAnalysisEditorWorkingCopiesUseAuthenticatedOwner(t *testing.T) {
	var (
		token    = strings.Repeat("t", 32)
		ownerOne = uuid.Must(uuid.NewV4())
		ownerTwo = uuid.Must(uuid.NewV4())
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Error("native client did not authenticate to the rule service")
		}
		switch request.Method {
		case http.MethodPut:
			var saved map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&saved))
			require.Equal(t, ownerOne.String(), saved["owner"])
			_ = json.NewEncoder(writer).Encode(map[string]string{"name": saved["name"], "text": saved["text"], "revision": strings.Repeat("a", 64)})
		case http.MethodGet:
			if request.URL.Query().Get("owner") == ownerTwo.String() {
				_ = json.NewEncoder(writer).Encode([]any{})
			} else {
				_ = json.NewEncoder(writer).Encode(map[string]string{"name": "PoC_Vault/unfinished_query.cypher", "text": "MATCH (", "revision": strings.Repeat("a", 64)})
			}
		case http.MethodDelete:
			var deleted map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&deleted))
			require.Equal(t, ownerOne.String(), deleted["owner"])
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	resources := v2.Resources{Config: config.Configuration{PostProcessing: config.PostProcessingConfiguration{URL: server.URL, Token: token}}}
	path := "/api/v2/analysis-workspace/working-copies"
	response := httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodPut, path, `{"owner":"`+ownerTwo.String()+`","name":"PoC_Vault/unfinished_query.cypher","text":"MATCH ("}`, ownerOne))
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodPut, path, `{"name":"PoC_Vault/unfinished_query.cypher","text":"MATCH ("}`, ownerOne))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), ownerOne.String())
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodGet, path, "", ownerTwo))
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "unfinished_query")
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodGet, path+"?name=PoC_Vault%2Funfinished_query.cypher", "", ownerOne))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "MATCH (")
	var revisionResponse struct {
		Data struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &revisionResponse))
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodDelete, path, `{"name":"PoC_Vault/unfinished_query.cypher","revision":"`+revisionResponse.Data.Revision+`"}`, ownerOne))
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
}

func TestAnalysisEditorWorkingCopiesRejectUnsafeInputAndUnavailableService(t *testing.T) {
	owner := uuid.Must(uuid.NewV4())
	resources := v2.Resources{}
	response := httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodGet, "/api/v2/analysis-workspace/working-copies", "", owner))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":"secret unfinished query"}`))
	}))
	defer upstream.Close()
	resources.Config.PostProcessing = config.PostProcessingConfiguration{URL: upstream.URL, Token: strings.Repeat("t", 32)}
	for _, body := range []string{`null`, `{} {}`, `{"name":"PoC_Vault/a.cypher","name":"PoC_Vault/b.cypher"}`, `{"owner":"spoofed","name":"PoC_Vault/a.cypher","text":"MATCH ("}`} {
		response = httptest.NewRecorder()
		resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodPut, "/api/v2/analysis-workspace/working-copies", body, owner))
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodPut, "/api/v2/analysis-workspace/working-copies", `{"name":"PoC_Vault/a.cypher","text":"MATCH ("}`+strings.Repeat(" ", 4*1024*1024), owner))
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	require.Zero(t, calls)
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodGet, "/api/v2/analysis-workspace/working-copies?name=a&name=b", "", owner))
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditorWorkingCopies(response, editorWorkingCopyRequest(http.MethodGet, "/api/v2/analysis-workspace/working-copies", "", owner))
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.NotContains(t, response.Body.String(), "secret")
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v2/analysis-workspace/working-copies", nil)
	resources.ManageAnalysisEditorWorkingCopies(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, 1, calls)
}
