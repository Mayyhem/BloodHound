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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ManagementError carries a safe explanation, never the upstream response body.
// Validation details for inactive files are returned by the draft read instead.
type ManagementError struct {
	Status  int
	Message string
}

func (s *ManagementError) Error() string { return s.Message }

func (s Client) managementRequest(ctx context.Context, method, path string, change Change, result any) error {
	var (
		contents, err = json.Marshal(change)
		response      *http.Response
	)
	if err != nil || len(contents) > MaxBundleBytes {
		return &ManagementError{http.StatusRequestEntityTooLarge, "Analysis changes exceed the upload limit."}
	}
	response, err = s.request(ctx, method, path, contents)
	if err != nil {
		return fmt.Errorf("cannot contact the rule service")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && !(result == nil && response.StatusCode == http.StatusNoContent) {
		switch response.StatusCode {
		case http.StatusConflict:
			return &ManagementError{http.StatusConflict, "Rules, schemas, or the saved draft changed. Refresh and review again."}
		case http.StatusBadRequest:
			return &ManagementError{http.StatusBadRequest, "The change is invalid. Check file declarations, required queries, installed schemas, and your overwrite choices."}
		case http.StatusRequestEntityTooLarge:
			return &ManagementError{http.StatusRequestEntityTooLarge, "Analysis changes exceed the upload limit."}
		default:
			return fmt.Errorf("rule management is unavailable")
		}
	}
	if result == nil {
		return nil
	}
	contents, err = io.ReadAll(io.LimitReader(response.Body, MaxWorkspaceBytes+1))
	if err != nil || len(contents) > MaxWorkspaceBytes {
		return fmt.Errorf("rule management response is incomplete or exceeds its limit")
	}
	if json.Unmarshal(contents, result) != nil {
		return fmt.Errorf("invalid rule management response")
	}
	return nil
}

func (s Client) ReadDraft(ctx context.Context) (DraftView, error) {
	var result DraftView
	err := s.managementRequest(ctx, http.MethodGet, "/v1/draft", Change{}, &result)
	if err == nil && result.Files == nil {
		err = fmt.Errorf("invalid draft response")
	}
	result.ValidationError = safeDraftExplanation(result.ValidationError)
	return result, err
}
func (s Client) SaveDraft(ctx context.Context, change Change) (DraftView, error) {
	var result DraftView
	err := s.managementRequest(ctx, http.MethodPut, "/v1/draft", change, &result)
	if err == nil && result.Files == nil {
		err = fmt.Errorf("invalid draft response")
	}
	result.ValidationError = safeDraftExplanation(result.ValidationError)
	return result, err
}
func (s Client) DiscardDraft(ctx context.Context, change Change) error {
	return s.managementRequest(ctx, http.MethodDelete, "/v1/draft", change, nil)
}
func (s Client) Preview(ctx context.Context, change Change) (Preview, error) {
	var result Preview
	err := s.managementRequest(ctx, http.MethodPost, "/v1/preview", change, &result)
	if err == nil && (result.Revision == "" || result.BaseRevision != change.BaseRevision || result.Plan.Revision != result.Revision) {
		err = fmt.Errorf("invalid review response")
	}
	// The native preview uses complete dependencies, not the legacy broad groups.
	result.Plan.Threads = []ThreadView{}
	return result, err
}
func (s Client) Apply(ctx context.Context, change Change) (string, error) {
	var result struct {
		Revision string `json:"revision"`
	}
	err := s.managementRequest(ctx, http.MethodPut, "/v1/rules", change, &result)
	if err == nil && (result.Revision == "" || result.Revision != change.ConfirmRevision) {
		return "", errors.New("activation response did not confirm a revision")
	}
	return result.Revision, err
}

// Parser diagnostics can repeat literals from a file. The native workspace uses
// only fixed guidance; full file text is available through authorized inspection.
func safeDraftExplanation(message string) string {
	if message == "" {
		return ""
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "active rules changed"):
		return "Active rules changed. Download the draft files, discard the draft, and upload them again against the current rules."
	case strings.Contains(lower, "schema"), strings.Contains(lower, "namespace"), strings.Contains(lower, "version"):
		return "Check the namespace and version comments and install the matching schemas through OpenGraph Management. Then review this draft again."
	case strings.Contains(lower, "cycle"), strings.Contains(lower, "circular"):
		return "The declared prerequisites form a cycle. Adjust their dependencies, upload the corrected files, and review again."
	case strings.Contains(lower, "missing"), strings.Contains(lower, "not found"), strings.Contains(lower, "requires"):
		return "Check that every required analysis file and prerequisite is included. Upload the missing files and review again."
	default:
		return "These files could not be validated. Check their declarations, prerequisites, Cypher syntax, and final RETURN. The draft remains inactive."
	}
}

// DecodeChange preserves the same strict JSON checks at both HTTP boundaries.
// In particular, repeated revision/file keys must not silently choose a value.
func DecodeChange(contents []byte) (Change, error) {
	var change *Change
	if len(contents) > MaxBundleBytes {
		return Change{}, fmt.Errorf("change exceeds its byte limit")
	}
	if err := decodeJSON(contents, &change); err != nil {
		return Change{}, fmt.Errorf("invalid analysis change")
	}
	if change == nil {
		return Change{}, fmt.Errorf("analysis change must be an object")
	}
	return *change, nil
}
