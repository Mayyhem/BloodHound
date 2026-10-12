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

package ruleservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// This handler is reached only after the registry has checked the caller's
// access. It can request normal BloodHound analysis, but cannot submit arbitrary
// BloodHound API calls or change how calculated relationships are saved.
func newAnalysisHandler(analysis AnalysisController) http.Handler {
	var starts = make(chan struct{}, 1)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodPost {
			fail(writer, http.StatusMethodNotAllowed, "GET or POST required")
			return
		}
		if analysis == nil {
			var message = "Run Analysis needs a BloodHound connection configured on the rule server."
			if request.Method == http.MethodGet {
				respond(writer, http.StatusOK, AnalysisState{Message: message})
			} else {
				fail(writer, http.StatusServiceUnavailable, message)
			}
			return
		}
		var ctx, cancel = context.WithTimeout(request.Context(), 25*time.Second)
		defer cancel()
		if request.Method == http.MethodGet {
			state, err := analysis.Status(ctx)
			if err != nil {
				fail(writer, http.StatusBadGateway, "Cannot read BloodHound analysis status. Check the server connection and API token permissions.")
				return
			}
			respond(writer, http.StatusOK, state)
			return
		}
		// One click needs only an empty JSON object. Never accept a path, query,
		// credentials, or options supplied by the browser.
		contents, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1024))
		if err != nil {
			fail(writer, http.StatusBadRequest, "Analysis requests must contain only {}.")
			return
		}
		var empty *struct{}
		if err := decodeJSON(contents, &empty); err != nil || empty == nil {
			fail(writer, http.StatusBadRequest, "Analysis requests must contain only {}.")
			return
		}
		// Avoid overlapping clicks from different browser tabs. BloodHound also
		// combines queued requests; its normal scheduler remains in charge.
		select {
		case starts <- struct{}{}:
			defer func() { <-starts }()
		default:
			fail(writer, http.StatusConflict, "An analysis request is already being sent. Refresh its status.")
			return
		}
		if err := analysis.Run(ctx); err != nil {
			if errors.Is(err, ErrAnalysisBusy) {
				fail(writer, http.StatusConflict, "BloodHound already has work queued or running. Refresh its status.")
			} else {
				// A lost response does not prove BloodHound rejected the request.
				// Let the user check status instead of automatically sending it again.
				fail(writer, http.StatusBadGateway, "Could not confirm the analysis request. It may have been queued; refresh status before trying again.")
			}
			return
		}
		respond(writer, http.StatusAccepted, AnalysisState{
			Available: true, Pending: true,
			Message: "Analysis requested. BloodHound will run its normal analysis, then check the enabled extension rules.",
		})
	})
}
