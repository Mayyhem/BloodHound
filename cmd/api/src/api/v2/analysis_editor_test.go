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
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	v2 "github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/config"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
	"github.com/stretchr/testify/require"
)

type editorService struct {
	owner string
	err   error
}

func (s *editorService) PreviewAnalysisFile(context.Context, string, postprocessing.EditorTestRequest) (postprocessing.EditorPlanPreview, error) {
	return postprocessing.EditorPlanPreview{}, s.err
}
func (s *editorService) TestAnalysisFile(context.Context, string, postprocessing.EditorTestRequest) (postprocessing.EditorTestResult, error) {
	return postprocessing.EditorTestResult{}, s.err
}
func (s *editorService) ReviewAnalysisFile(_ context.Context, owner string, _ string) (postprocessing.WorkspacePreview, error) {
	s.owner = owner
	return postprocessing.WorkspacePreview{}, s.err
}
func (s *editorService) ActivateAnalysisFile(context.Context, string, string, string) (string, error) {
	return "", s.err
}

func TestAnalysisEditorUsesOwnerAndHidesServiceErrors(t *testing.T) {
	owner := uuid.Must(uuid.NewV4())
	service := &editorService{}
	resources := v2.Resources{Config: config.Configuration{PostProcessing: config.PostProcessingConfiguration{URL: "http://configured"}}, AnalysisEditor: service}
	review := `{"test_id":"` + strings.Repeat("a", 64) + `"}`
	response := httptest.NewRecorder()
	resources.ManageAnalysisEditor(response, editorWorkingCopyRequest(http.MethodPost, "/api/v2/analysis-workspace/editor/review", review, owner))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, owner.String(), service.owner)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	service.err = errors.New("MATCH (secret) returned sensitive-id")
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditor(response, editorWorkingCopyRequest(http.MethodPost, "/api/v2/analysis-workspace/editor/review", review, owner))
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.NotContains(t, response.Body.String(), "secret")
}

func TestAnalysisEditorRejectsUnsafeRequests(t *testing.T) {
	owner := uuid.Must(uuid.NewV4())
	resources := v2.Resources{Config: config.Configuration{PostProcessing: config.PostProcessingConfiguration{URL: "http://configured"}}, AnalysisEditor: &editorService{}}
	valid := `{"name":"PoC_Test/a.cypher","text":"RETURN 1","schema_revision":"` + strings.Repeat("a", 64) + `"`
	// The editor always recalculates, so the removed "mode" and "source_id"
	// fields are rejected as unknown input.
	for _, body := range []string{"{}", valid + `} {}`, valid + `,"mode":"recalculate"}`, valid + `,"source_id":"` + strings.Repeat("a", 64) + `"}`} {
		response := httptest.NewRecorder()
		resources.ManageAnalysisEditor(response, editorWorkingCopyRequest(http.MethodPost, "/api/v2/analysis-workspace/editor/test", body, owner))
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	resources.ManageAnalysisEditor(response, editorWorkingCopyRequest(http.MethodPost, "/api/v2/analysis-workspace/editor/test", valid+`}`, owner))
	require.Equal(t, http.StatusOK, response.Code)
	response = httptest.NewRecorder()
	resources.ManageAnalysisEditor(response, editorWorkingCopyRequest(http.MethodPost, "/api/v2/analysis-workspace/editor/test", strings.Repeat("x", 4*1024*1024+1), owner))
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}
