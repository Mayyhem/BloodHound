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

package v2

import (
	"errors"
	"io"
	"net/http"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

// Routes check both reading and editing permission before this handler runs.
// These five operations reuse the rule service's draft and review checks.
// There is deliberately no arbitrary URL, status write, or analysis-start proxy.
func (s Resources) ManageAnalysisWorkspace(response http.ResponseWriter, request *http.Request) {
	var (
		manager = postprocessing.WorkspaceManager{WorkspaceReader: s.workspaceReader()}
		change  postprocessing.WorkspaceChange
		result  any
		err     error
	)
	response.Header().Set("Cache-Control", "no-store")
	if s.Config.PostProcessing.URL == "" {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusServiceUnavailable, "OpenGraph analysis is not configured.", request), response)
		return
	}
	if request.Method != http.MethodGet {
		var contents []byte
		contents, err = io.ReadAll(http.MaxBytesReader(response, request.Body, postprocessing.MaxWorkspaceChangeBytes))
		if err == nil {
			change, err = postprocessing.DecodeWorkspaceChange(contents)
		}
		if err != nil {
			var oversized *http.MaxBytesError
			status := http.StatusBadRequest
			if errors.As(err, &oversized) {
				status = http.StatusRequestEntityTooLarge
			}
			api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(status, "Provide one analysis change within the upload limit.", request), response)
			return
		}
		if (change.BaseRevision != "" && !workspaceRevision.MatchString(change.BaseRevision)) ||
			(change.DraftRevision != "" && !workspaceRevision.MatchString(change.DraftRevision)) ||
			(change.ConfirmRevision != "" && !workspaceRevision.MatchString(change.ConfirmRevision)) ||
			(request.URL.Path != "/api/v2/analysis-workspace/draft" && !workspaceRevision.MatchString(change.SchemaRevision)) {
			api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadRequest, "Provide the displayed rule, schema, and draft revisions.", request), response)
			return
		}
	}
	switch {
	case request.URL.Path == "/api/v2/analysis-workspace/draft" && request.Method == http.MethodGet:
		result, err = manager.Draft(request.Context())
	case request.URL.Path == "/api/v2/analysis-workspace/draft" && request.Method == http.MethodPut:
		result, err = manager.SaveDraft(request.Context(), change)
	case request.URL.Path == "/api/v2/analysis-workspace/draft" && request.Method == http.MethodDelete:
		err = manager.DiscardDraft(request.Context(), change)
	case request.URL.Path == "/api/v2/analysis-workspace/preview" && request.Method == http.MethodPost:
		result, err = manager.Preview(request.Context(), change)
	case request.URL.Path == "/api/v2/analysis-workspace/rules" && request.Method == http.MethodPut:
		var revision string
		revision, err = manager.Apply(request.Context(), change)
		result = map[string]string{"revision": revision}
	default:
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		var (
			status          = http.StatusBadGateway
			message         = "Rule management could not be confirmed. Refresh before trying another change."
			managementError *postprocessing.WorkspaceManagementError
		)
		if errors.As(err, &managementError) {
			status, message = managementError.Status, managementError.Message
		}
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(status, message, request), response)
		return
	}
	if request.Method == http.MethodDelete {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.WriteBasicResponse(request.Context(), result, http.StatusOK, response)
}
