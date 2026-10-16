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
	"strings"
	"testing"

	v2 "github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/stretchr/testify/require"
)

func TestAnalysisManagementBoundsAndStrictChanges(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		require.Equal(t, "/v1/preview", request.URL.Path)
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"error":"secret Cypher and credentials"}`))
	}))
	defer server.Close()
	resources := v2.Resources{Config: config.Configuration{PostProcessing: config.PostProcessingConfiguration{URL: server.URL, Token: strings.Repeat("t", 32)}}}
	for _, body := range []string{`null`, `{} {}`, `{"base_revision":"one","base_revision":"two"}`, `{"url":"http://elsewhere"}`, `{"schema_revision":"bad"}`} {
		response := httptest.NewRecorder()
		resources.ManageAnalysisWorkspace(response, httptest.NewRequest("POST", "/api/v2/analysis-workspace/preview", strings.NewReader(body)))
		require.Equal(t, 400, response.Code)
	}
	response := httptest.NewRecorder()
	resources.ManageAnalysisWorkspace(response, httptest.NewRequest("POST", "/api/v2/analysis-workspace/preview", strings.NewReader(`{}`+strings.Repeat(" ", 4*1024*1024))))
	require.Equal(t, 413, response.Code)
	require.Zero(t, calls)
	response = httptest.NewRecorder()
	resources.ManageAnalysisWorkspace(response, httptest.NewRequest("POST", "/api/v2/analysis-workspace/preview", strings.NewReader(fmt.Sprintf(`{"base_revision":%q,"schema_revision":%q,"disable":["PoC/users.cypher"]}`, strings.Repeat("a", 64), strings.Repeat("b", 64)))))
	require.Equal(t, 409, response.Code)
	require.Equal(t, 1, calls)
	require.NotContains(t, response.Body.String(), "secret")
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}
