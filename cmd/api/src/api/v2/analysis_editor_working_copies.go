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
	"net/url"

	"github.com/gofrs/uuid"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

const maxEditorWorkingCopyName = 256

// ManageAnalysisEditorWorkingCopies keeps unfinished editor work private to
// the authenticated BloodHound user. The browser cannot provide the owner.
func (s Resources) ManageAnalysisEditorWorkingCopies(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	user, isUser := auth.GetUserFromAuthCtx(bhctx.FromRequest(request).AuthCtx)
	if !isUser || user.ID == uuid.Nil {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusUnauthorized, "The authenticated request has no user identity.", request), response)
		return
	}
	if s.Config.PostProcessing.URL == "" {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusServiceUnavailable, "OpenGraph analysis is not configured.", request), response)
		return
	}
	manager := postprocessing.WorkspaceManager{WorkspaceReader: s.workspaceReader()}
	owner := user.ID.String()
	var result any
	var err error
	switch request.Method {
	case http.MethodGet:
		name, valid := editorWorkingCopyName(request)
		if !valid {
			api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadRequest, "Provide at most one analysis filename.", request), response)
			return
		}
		if name == "" {
			result, err = manager.ListEditorWorkingCopies(request.Context(), owner)
		} else {
			result, err = manager.ReadEditorWorkingCopy(request.Context(), owner, name)
		}
	case http.MethodPut:
		contents, readError := io.ReadAll(http.MaxBytesReader(response, request.Body, postprocessing.MaxWorkspaceChangeBytes))
		if readError != nil {
			editorWorkingCopyInputError(request, response, readError)
			return
		}
		copy, decodeError := postprocessing.DecodeEditorWorkingCopy(contents)
		if decodeError != nil {
			editorWorkingCopyInputError(request, response, decodeError)
			return
		}
		result, err = manager.SaveEditorWorkingCopy(request.Context(), owner, copy)
	case http.MethodDelete:
		contents, readError := io.ReadAll(http.MaxBytesReader(response, request.Body, postprocessing.MaxWorkspaceChangeBytes))
		if readError != nil {
			editorWorkingCopyInputError(request, response, readError)
			return
		}
		deletion, decodeError := postprocessing.DecodeEditorWorkingCopyDeletion(contents)
		if decodeError != nil {
			editorWorkingCopyInputError(request, response, decodeError)
			return
		}
		err = manager.DeleteEditorWorkingCopy(request.Context(), owner, deletion.Name, deletion.Revision)
	default:
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		editorWorkingCopyServiceError(request, response, err)
		return
	}
	if request.Method == http.MethodDelete {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	api.WriteBasicResponse(request.Context(), result, http.StatusOK, response)
}

func editorWorkingCopyName(request *http.Request) (string, bool) {
	parameters, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return "", false
	}
	if len(parameters) == 0 {
		return "", true
	}
	if len(parameters) != 1 || len(parameters["name"]) != 1 {
		return "", false
	}
	name := parameters.Get("name")
	return name, len(name) <= maxEditorWorkingCopyName && workspaceQuery.MatchString(name)
}

func editorWorkingCopyInputError(request *http.Request, response http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		status = http.StatusRequestEntityTooLarge
	}
	api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(status, "Provide one working copy within the upload limit.", request), response)
}

func editorWorkingCopyServiceError(request *http.Request, response http.ResponseWriter, err error) {
	var serviceError *postprocessing.EditorWorkingCopyError
	if errors.As(err, &serviceError) {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(serviceError.Status, serviceError.Message, request), response)
		return
	}
	api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadGateway, "Working copies are unavailable. Refresh and try again.", request), response)
}
