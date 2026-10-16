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
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// EditorWorkingCopyError gives native handlers a fixed explanation. It never
// includes a response body, unfinished Cypher, owner, or service URL.
type EditorWorkingCopyError struct {
	Status  int
	Message string
}

func (s *EditorWorkingCopyError) Error() string { return s.Message }

func (s Client) ListEditorWorkingCopies(ctx context.Context, owner string) ([]EditorWorkingCopySummary, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil {
		return nil, fmt.Errorf("invalid editor working copy request")
	}
	values := url.Values{"owner": []string{owner}}
	var result []EditorWorkingCopySummary
	if err := s.editorWorkingCopyRequest(ctx, http.MethodGet, values, nil, &result); err != nil {
		return nil, err
	}
	if result == nil || len(result) > maxEditorWorkingCopies {
		return nil, fmt.Errorf("invalid editor working copy response")
	}
	for _, summary := range result {
		if err := validateEditorWorkingCopy(EditorWorkingCopy{Name: summary.Name, BaseRevision: summary.BaseRevision, Revision: summary.Revision}); err != nil {
			return nil, fmt.Errorf("invalid editor working copy response")
		}
	}
	return result, nil
}

func (s Client) ReadEditorWorkingCopy(ctx context.Context, owner, name string) (EditorWorkingCopy, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil || !queryName.MatchString(name) || len(name) > maxEditorWorkingCopyName {
		return EditorWorkingCopy{}, fmt.Errorf("invalid editor working copy request")
	}
	values := url.Values{"owner": []string{owner}, "name": []string{name}}
	var result EditorWorkingCopy
	if err := s.editorWorkingCopyRequest(ctx, http.MethodGet, values, nil, &result); err != nil {
		return EditorWorkingCopy{}, err
	}
	if err := validateEditorWorkingCopy(result); err != nil {
		return EditorWorkingCopy{}, fmt.Errorf("invalid editor working copy response")
	}
	return result, nil
}

func (s Client) SaveEditorWorkingCopy(ctx context.Context, owner string, copy EditorWorkingCopy) (EditorWorkingCopy, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil || validateEditorWorkingCopy(copy) != nil {
		return EditorWorkingCopy{}, fmt.Errorf("invalid editor working copy request")
	}
	contents, err := json.Marshal(editorWorkingCopyRequest{Owner: owner, Name: copy.Name, Text: copy.Text, BaseRevision: copy.BaseRevision, Revision: copy.Revision})
	if err != nil || len(contents) > MaxBundleBytes {
		return EditorWorkingCopy{}, fmt.Errorf("editor working copy exceeds its byte limit")
	}
	var result EditorWorkingCopy
	if err := s.editorWorkingCopyRequest(ctx, http.MethodPut, nil, contents, &result); err != nil {
		return EditorWorkingCopy{}, err
	}
	if err := validateEditorWorkingCopy(result); err != nil || result.Name != copy.Name || result.Text != copy.Text || result.BaseRevision != copy.BaseRevision || result.Revision == "" {
		return EditorWorkingCopy{}, fmt.Errorf("invalid editor working copy response")
	}
	return result, nil
}

func (s Client) DeleteEditorWorkingCopy(ctx context.Context, owner, name, revision string) error {
	if err := validateEditorWorkingCopyOwner(owner); err != nil || !queryName.MatchString(name) || len(name) > maxEditorWorkingCopyName || !validWorkingCopyRevision(revision) || revision == "" {
		return fmt.Errorf("invalid editor working copy request")
	}
	contents, err := json.Marshal(deleteEditorWorkingCopyRequest{Owner: owner, Name: name, Revision: revision})
	if err != nil || len(contents) > MaxBundleBytes {
		return fmt.Errorf("editor working copy exceeds its byte limit")
	}
	return s.editorWorkingCopyRequest(ctx, http.MethodDelete, nil, contents, nil)
}

func (s Client) editorWorkingCopyRequest(ctx context.Context, method string, values url.Values, contents []byte, result any) error {
	path := "/v1/editor/working-copies"
	if len(values) != 0 {
		path += "?" + values.Encode()
	}
	response, err := s.request(ctx, method, path, contents)
	if err != nil {
		return fmt.Errorf("cannot contact the rule service")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && !(result == nil && response.StatusCode == http.StatusNoContent) {
		return editorWorkingCopyHTTPError(response.StatusCode)
	}
	if result == nil {
		return nil
	}
	contents, err = io.ReadAll(io.LimitReader(response.Body, maxEditorWorkingCopyStorage+1))
	if err != nil || len(contents) > maxEditorWorkingCopyStorage {
		return fmt.Errorf("editor working copy response is incomplete or exceeds its limit")
	}
	if err := decodeJSON(contents, result); err != nil {
		return fmt.Errorf("invalid editor working copy response")
	}
	return nil
}

func editorWorkingCopyHTTPError(status int) error {
	switch status {
	case http.StatusNotFound:
		return &EditorWorkingCopyError{Status: status, Message: "The saved working copy was not found."}
	case http.StatusConflict:
		return &EditorWorkingCopyError{Status: status, Message: "The working copy changed. Resume it and try again."}
	case http.StatusBadRequest:
		return &EditorWorkingCopyError{Status: status, Message: "The working copy request was rejected."}
	case http.StatusRequestEntityTooLarge:
		return &EditorWorkingCopyError{Status: status, Message: "The working copy exceeds its size limit."}
	default:
		return fmt.Errorf("editor working copies are unavailable")
	}
}
