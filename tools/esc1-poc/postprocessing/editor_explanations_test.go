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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
	"github.com/stretchr/testify/require"
)

func TestEditorPreviewExplainsAnUndeclaredGraphRead(t *testing.T) {
	service, _, _, request := newEditorServiceTest(t)
	request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nMATCH (d:Domain) RETURN d.objectid AS objectid, d.name AS name LIMIT 1"
	_, err := service.PreviewAnalysisFile(t.Context(), "owner", request)
	var editorError *EditorError
	require.ErrorAs(t, err, &editorError)
	require.Equal(t, http.StatusBadRequest, editorError.Status)
	require.Equal(t, "Line 4 reads node:Domain, but the file does not declare it. Add it to a // depends-on: line, for example // depends-on: node:Domain.", editorError.Message)
}

func TestEditorTestExplainsAWholeGraphObjectAnswer(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	// Any value that is not plain text, a number, a list, or a map stands in
	// for the node object the database driver returns for RETURN n.
	database.queryRows = runner.Rows{{"value": struct{ ID int64 }{ID: 1}}}
	_, err := service.TestAnalysisFile(t.Context(), "owner", request)
	var editorError *EditorError
	require.ErrorAs(t, err, &editorError)
	require.Equal(t, "Column value on row 1 holds a whole graph object (a node, relationship, or path). Return plain values instead, for example n.objectid AS objectid, n.name AS name.", editorError.Message)
	require.Empty(t, service.editor.receipts)
}

func TestEditorExplanationsNeverRepeatUnrecognizedFileText(t *testing.T) {
	for _, problem := range []error{
		errors.New("PoC/a.cypher: line 3: output: unexpected 'private literal'"),
		errors.New("PoC/a.cypher: line 3: missing dependency; add node:Domain 'private' to // depends-on"),
	} {
		require.Equal(t, genericEditorFileProblem, editorFileProblem(problem, "PoC/a.cypher"))
	}
	require.Equal(t,
		"The prerequisite PoC/b.cypher reads relationship:MemberOf on line 7 without declaring it. Fix that file first.",
		editorFileProblem(errors.New("PoC/b.cypher: line 7: missing dependency; add relationship:MemberOf to // depends-on"), "PoC/a.cypher"))
	require.Contains(t, editorFileProblem(fmt.Errorf("PoC/a.cypher: parameter $users needs a declared query prerequisite"), "PoC/a.cypher"), "$users has no matching query prerequisite")
	require.Contains(t, editorTestProblem(fmt.Errorf("query failed: %w", errors.New("database detail"))), "The test did not succeed")
}

func TestEditorTestExplainsAnEmptyHelperProperty(t *testing.T) {
	service, _, database, request := newEditorServiceTest(t)
	request.Name = "PoC/domain_helpers.cypher"
	request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: none\nRETURN 'PoC_DomainHelper_editor' AS id, ['PoC_DomainHelper'] AS kinds, {name: null} AS properties"
	// A Domain without a name produces this row in the real lab.
	database.queryRows = runner.Rows{{"id": "PoC_DomainHelper_editor", "kinds": []any{"PoC_DomainHelper"}, "properties": map[string]any{"name": nil}}}
	_, err := service.TestAnalysisFile(t.Context(), "owner", request)
	var editorError *EditorError
	require.ErrorAs(t, err, &editorError)
	require.Equal(t, "The name property is empty (null) for 1 helper object: PoC_DomainHelper_editor. Give it a fallback, for example name: coalesce(x.name, x.objectid), or leave those rows out with WHERE … IS NOT NULL.", editorError.Message)
	require.Zero(t, database.commits)
}

func TestEditorPreviewListsUnusedDeclarations(t *testing.T) {
	service, _, _, request := newEditorServiceTest(t)
	request.Text = "// namespace: PoC\n// version: v1.0.0\n// depends-on: node:Domain\n// depends-on: query:enabled_cas\nRETURN 1 AS value"
	result, err := service.PreviewAnalysisFile(t.Context(), "owner", request)
	require.NoError(t, err)
	require.Equal(t, []string{"node:Domain", "query:PoC/enabled_cas"}, result.Unused)
}

func TestEditorTestProblemsNameTheObjectsAtFault(t *testing.T) {
	property := &runner.HelperPropertyError{Property: "name", Count: 5, Examples: []string{"PoC_A", "PoC_B", "PoC_C"}, Cause: runner.ErrEmptyPropertyValue}
	require.Equal(t, "The name property is empty (null) for 5 helper objects: PoC_A, PoC_B, PoC_C, and 2 more. Give it a fallback, for example name: coalesce(x.name, x.objectid), or leave those rows out with WHERE … IS NOT NULL.", editorTestProblem(fmt.Errorf("helper answers from PoC/a.cypher: %w", property)))
	// Unusual property names are never repeated.
	odd := &runner.HelperPropertyError{Property: "bad name'", Count: 1, Examples: []string{"PoC_A"}, Cause: runner.ErrEmptyPropertyValue}
	require.True(t, strings.HasPrefix(editorTestProblem(odd), "A property is empty (null) for 1 helper object"))
	require.Equal(t, `The source of a PoC_Link connection ("S-1-5-21-9") matches no object in the graph. Return the object ID of an existing object.`, editorTestProblem(&runner.EndpointError{Kind: "PoC_Link", Side: "source", Object: "S-1-5-21-9"}))
	require.Equal(t, `The target of a PoC_Link connection ("S-1-5-21-9") matches 2 objects. Each object ID must belong to exactly one object.`, editorTestProblem(&runner.EndpointError{Kind: "PoC_Link", Side: "target", Object: "S-1-5-21-9", Found: 2}))
	// The shared status report keeps names and counts but leaves out object IDs.
	report := runner.FailureMessage(fmt.Errorf("helper answers from PoC/a.cypher: %w", property))
	require.Equal(t, "The name property is empty (null) for 5 helper objects. Give it a fallback value, for example with coalesce(), or leave out the rows without one.", report)
	require.NotContains(t, runner.FailureMessage(&runner.EndpointError{Kind: "PoC_Link", Side: "source", Object: "S-1-5-21-9"}), "S-1-5-21-9")
}

func TestEditorExplainsNamespaceProblems(t *testing.T) {
	require.Contains(t, editorFileProblem(errors.New("GH/domain_helpers.cypher: the selected extension and the file's namespace do not match"), "GH/domain_helpers.cypher"), "does not match the Installed extension")
	require.Contains(t, editorFileProblem(errors.New("PoC_X/a.cypher: install the schema for namespace PoC_X in OpenGraph Management"), "PoC_X/a.cypher"), "No installed extension uses")
}
