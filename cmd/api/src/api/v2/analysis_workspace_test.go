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

package v2_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	v2 "github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/stretchr/testify/require"
)

func TestAnalysisWorkspaceReadsAndRevisionCheckedDownloads(t *testing.T) {
	var (
		revision       = strings.Repeat("a", 64)
		schemaRevision = strings.Repeat("b", 64)
		calls          int
		token          = strings.Repeat("t", 32)
		upstream       = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			calls++
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "/v1/workspace", request.URL.RequestURI())
			require.Equal(t, "Bearer "+token, request.Header.Get("Authorization"))
			_, _ = fmt.Fprintf(response, `{"plan":{"revision":%q,"schema_revision":%q,"queries":[{"name":"PoC_A/users.cypher","text":"// original comment\nRETURN 1"}]},"status":{"state":"unavailable"},"observed_at":"2026-10-14T12:00:00Z"}`, revision, schemaRevision)
		}))
	)
	defer upstream.Close()
	// No graph/database services are supplied: these handlers cannot calculate or
	// create graph objects, even when the author inspects and downloads queries.
	var resources = v2.Resources{Config: config.Configuration{PostProcessing: config.PostProcessingConfiguration{URL: upstream.URL, Token: token}}}
	response := httptest.NewRecorder()
	resources.GetAnalysisWorkspace(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace", nil))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), "original comment")
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.NotContains(t, response.Body.String(), token)
	var parameters = url.Values{"query": {"PoC_A/users.cypher"}, "revision": {revision}, "schema_revision": {schemaRevision}}
	response = httptest.NewRecorder()
	resources.DownloadAnalysisQuery(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace/query?"+parameters.Encode(), nil))
	require.Equal(t, 200, response.Code)
	require.Equal(t, "// original comment\nRETURN 1", response.Body.String())
	require.Contains(t, response.Header().Get("Content-Disposition"), "users.cypher")
	parameters.Set("revision", strings.Repeat("c", 64))
	response = httptest.NewRecorder()
	resources.DownloadAnalysisQuery(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace/query?"+parameters.Encode(), nil))
	require.Equal(t, 409, response.Code)
	parameters.Set("query", "../../elsewhere")
	response = httptest.NewRecorder()
	resources.DownloadAnalysisQuery(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace/query?"+parameters.Encode(), nil))
	require.Equal(t, 400, response.Code)
	require.Equal(t, 3, calls)
}

func TestAnalysisWorkspaceUnavailableMessagesDoNotExposeConnection(t *testing.T) {
	var resources = v2.Resources{}
	response := httptest.NewRecorder()
	resources.GetAnalysisWorkspace(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace", nil))
	require.Equal(t, 503, response.Code)
	resources.Config.PostProcessing.URL = "http://private:secret@rules"
	response = httptest.NewRecorder()
	resources.GetAnalysisWorkspace(response, httptest.NewRequest("GET", "/api/v2/analysis-workspace", nil))
	require.Equal(t, 502, response.Code)
	require.NotContains(t, response.Body.String(), "secret")
	require.NotContains(t, response.Body.String(), "private")
}
