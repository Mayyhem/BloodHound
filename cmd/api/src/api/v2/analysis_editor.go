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

package v2

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

// ManageAnalysisEditor executes the four editor operations. It rejects scoped
// users because the editor returns aggregate rows that cannot be filtered
// safely by environment after they are calculated.
func (s Resources) ManageAnalysisEditor(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	user, isUser := auth.GetUserFromAuthCtx(bhctx.FromRequest(request).AuthCtx)
	if !isUser || user.ID == uuid.Nil {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusUnauthorized, "The authenticated request has no user identity.", request), response)
		return
	}
	if s.DogTags != nil && ShouldFilterForETAC(s.DogTags, user) {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusForbidden, "Testing analysis files requires access to all environments.", request), response)
		return
	}
	if s.AnalysisEditor == nil || s.Config.PostProcessing.URL == "" {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusServiceUnavailable, "OpenGraph analysis is not configured.", request), response)
		return
	}
	owner := user.ID.String()
	var result any
	var err error
	switch request.URL.Path {
	case "/api/v2/analysis-workspace/editor/test", "/api/v2/analysis-workspace/editor/plan":
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		contents, readErr := io.ReadAll(http.MaxBytesReader(response, request.Body, postprocessing.MaxWorkspaceChangeBytes))
		if readErr != nil {
			editorInputError(request, response, readErr)
			return
		}
		editorRequest, decodeErr := postprocessing.DecodeEditorTestRequest(contents)
		if decodeErr != nil {
			editorInputError(request, response, decodeErr)
			return
		}
		if request.URL.Path == "/api/v2/analysis-workspace/editor/plan" {
			result, err = s.AnalysisEditor.PreviewAnalysisFile(request.Context(), owner, editorRequest)
		} else {
			result, err = s.AnalysisEditor.TestAnalysisFile(request.Context(), owner, editorRequest)
		}
	case "/api/v2/analysis-workspace/editor/review", "/api/v2/analysis-workspace/editor/activate":
		var payload struct {
			TestID          string `json:"test_id"`
			ConfirmRevision string `json:"confirm_revision"`
		}
		if request.URL.Path == "/api/v2/analysis-workspace/editor/review" && request.Method != http.MethodPost || request.URL.Path == "/api/v2/analysis-workspace/editor/activate" && request.Method != http.MethodPut {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if decodeEditorToken(request, response, &payload) {
			return
		}
		if request.URL.Path == "/api/v2/analysis-workspace/editor/review" {
			result, err = s.AnalysisEditor.ReviewAnalysisFile(request.Context(), owner, payload.TestID)
		} else {
			var revision string
			revision, err = s.AnalysisEditor.ActivateAnalysisFile(request.Context(), owner, payload.TestID, payload.ConfirmRevision)
			result = map[string]string{"revision": revision}
		}
	default:
		response.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		editorServiceError(request, response, err)
		return
	}
	api.WriteBasicResponse(request.Context(), result, http.StatusOK, response)
}

func decodeEditorToken(request *http.Request, response http.ResponseWriter, payload any) bool {
	contents, err := io.ReadAll(http.MaxBytesReader(response, request.Body, 4096))
	if err != nil {
		editorInputError(request, response, err)
		return true
	}
	action, err := postprocessing.DecodeEditorAction(contents)
	if err != nil {
		editorInputError(request, response, err)
		return true
	}
	encoded, _ := json.Marshal(action)
	_ = json.Unmarshal(encoded, payload)
	return false
}

func editorInputError(request *http.Request, response http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		status = http.StatusRequestEntityTooLarge
	}
	api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(status, "Provide one valid editor request within the upload limit.", request), response)
}

func editorServiceError(request *http.Request, response http.ResponseWriter, err error) {
	var editorError *postprocessing.EditorError
	if errors.As(err, &editorError) {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(editorError.Status, editorError.Message, request), response)
		return
	}
	api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadGateway, "Analysis editor results are unavailable. Refresh and try again.", request), response)
}
