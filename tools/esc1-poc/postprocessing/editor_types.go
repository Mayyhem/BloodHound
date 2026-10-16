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

package postprocessing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

const MaxEditorPreviewRows = 500

// Browser input names a file and its displayed versions, never a database,
// file path, owner, or graph version. Every test recalculates the file and all
// of the earlier files it needs, so there is no choice to make here about
// using saved answers.
type EditorTestRequest struct {
	Name           string `json:"name"`
	Text           string `json:"text"`
	BaseRevision   string `json:"base_revision"`
	SchemaRevision string `json:"schema_revision"`
}

type EditorTestResult struct {
	TestID    string            `json:"test_id"`
	Rows      []map[string]any  `json:"rows"`
	TotalRows int64             `json:"total_rows"`
	Truncated bool              `json:"truncated"`
	Execution *runner.Execution `json:"execution"`
}

// EditorError is safe to send to the author. Underlying parser/database errors
// can contain complete Cypher or collected identifiers and stay out of it.
type EditorError struct {
	Status  int
	Message string
}

func (s *EditorError) Error() string { return s.Message }

var editorName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*/[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.cypher$`)
var editorRevision = regexp.MustCompile(`^[a-f0-9]{64}$`)

func DecodeEditorTestRequest(contents []byte) (EditorTestRequest, error) {
	var request EditorTestRequest
	if len(contents) > MaxWorkspaceChangeBytes || !utf8.Valid(contents) || rejectDuplicateEditorKeys(contents) != nil {
		return request, fmt.Errorf("invalid editor request")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return request, fmt.Errorf("invalid editor request")
	}
	if err := request.validate(); err != nil {
		return EditorTestRequest{}, err
	}
	return request, nil
}
func (s EditorTestRequest) validate() error {
	if !editorName.MatchString(s.Name) || len(s.Text) == 0 || len(s.Text) > 1<<20 || !utf8.ValidString(s.Text) || (s.BaseRevision != "" && !editorRevision.MatchString(s.BaseRevision)) || !editorRevision.MatchString(s.SchemaRevision) {
		return &EditorError{http.StatusBadRequest, "Choose an installed extension, a valid filename, and current versions."}
	}
	return nil
}

// Repeated keys must never silently choose a different file or revision.
func rejectDuplicateEditorKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	var readValue func() error
	readValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("repeated field")
				}
				seen[name] = true
				if err := readValue(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := readValue(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid value")
		}
		_, err = decoder.Token()
		return err
	}
	return readValue()
}

type EditorPlanPreview struct {
	// Declarations in the edited file's // depends-on lines that it never
	// uses, written as the author types them (node:Domain, query:PoC/users).
	Unused       []string                        `json:"unused"`
	Plan         ruleservice.PlanView            `json:"plan"`
	Dependencies []extension.ExecutionDependency `json:"dependencies"`
}

// EditorAction identifies a successful test and, for activation, the exact
// review the author saw. It cannot carry replacement file text.
type EditorAction struct {
	TestID          string `json:"test_id"`
	ConfirmRevision string `json:"confirm_revision,omitempty"`
}

func DecodeEditorAction(contents []byte) (EditorAction, error) {
	var action EditorAction
	if len(contents) > 4096 || rejectDuplicateEditorKeys(contents) != nil {
		return action, fmt.Errorf("invalid editor action")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&action) != nil || decoder.Decode(new(any)) != io.EOF || !editorRevision.MatchString(action.TestID) || (action.ConfirmRevision != "" && !editorRevision.MatchString(action.ConfirmRevision)) {
		return EditorAction{}, fmt.Errorf("invalid editor action")
	}
	return action, nil
}
