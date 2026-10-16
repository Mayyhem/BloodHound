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
	"mime"
	"net/http"
	"path"
	"regexp"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/tools/esc1-poc/postprocessing"
)

var workspaceRevision = regexp.MustCompile(`^[a-f0-9]{64}$`)
var workspaceQuery = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*/[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.cypher$`)

func (s Resources) workspaceReader() postprocessing.WorkspaceReader {
	return postprocessing.WorkspaceReader{URL: s.Config.PostProcessing.URL, Token: s.Config.PostProcessing.Token}
}

// GetAnalysisWorkspace returns definitions and reports only. It does not use a
// graph database connection or start analysis. Access is checked on the route.
func (s Resources) GetAnalysisWorkspace(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if s.Config.PostProcessing.URL == "" {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusServiceUnavailable, "OpenGraph analysis is not configured.", request), response)
		return
	}
	view, err := s.workspaceReader().Read(request.Context())
	if err != nil {
		// Neither a connection address, its secret, nor an upstream error body belongs
		// in a browser error. The author can retry using Refresh.
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadGateway, "The analysis workspace is unavailable. Check the rule service connection and refresh.", request), response)
		return
	}
	api.WriteBasicResponse(request.Context(), view, http.StatusOK, response)
}

func (s Resources) DownloadAnalysisQuery(response http.ResponseWriter, request *http.Request) {
	var (
		parameters     = request.URL.Query()
		name           = parameters.Get("query")
		revision       = parameters.Get("revision")
		schemaRevision = parameters.Get("schema_revision")
	)
	response.Header().Set("Cache-Control", "no-store")
	if len(parameters) != 3 || len(parameters["query"]) != 1 || len(parameters["revision"]) != 1 || len(parameters["schema_revision"]) != 1 ||
		!workspaceQuery.MatchString(name) || !workspaceRevision.MatchString(revision) || !workspaceRevision.MatchString(schemaRevision) {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusBadRequest, "Provide one query name and the displayed rule and schema revisions.", request), response)
		return
	}
	if s.Config.PostProcessing.URL == "" {
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(http.StatusServiceUnavailable, "OpenGraph analysis is not configured.", request), response)
		return
	}
	contents, err := s.workspaceReader().Download(request.Context(), name, revision, schemaRevision)
	if err != nil {
		var status, message = http.StatusBadGateway, "The query download is unavailable. Refresh and try again."
		if errors.Is(err, postprocessing.ErrWorkspaceChanged) {
			status, message = http.StatusConflict, err.Error()
		}
		if errors.Is(err, postprocessing.ErrWorkspaceQueryMissing) {
			status, message = http.StatusNotFound, err.Error()
		}
		api.WriteErrorResponse(request.Context(), api.BuildErrorResponse(status, message, request), response)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(contents))
}
