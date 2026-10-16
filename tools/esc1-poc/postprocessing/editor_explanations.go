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
	"regexp"
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

// The checks that read an analysis file explain problems in detail, but those
// details can repeat text from the file. Like the upload review, the editor
// never passes them on directly. Instead it recognizes the most common
// problems and answers with fixed guidance. The only details it fills in are
// line numbers, file names, and kind or parameter names, and each pattern below
// accepts only plain names for those, never free text.
var (
	missingDependencyPattern   = regexp.MustCompile(`^(\S+\.cypher): line (\d+): missing dependency; add (node|relationship|query):([A-Za-z0-9_/]+) to // depends-on$`)
	undeclaredParameterPattern = regexp.MustCompile(`parameter \$([A-Za-z_][A-Za-z0-9_]*) needs a declared query prerequisite`)
	duplicateParameterPattern  = regexp.MustCompile(`duplicate parameter \$([A-Za-z_][A-Za-z0-9_]*);`)
)

const genericEditorFileProblem = "Check the file declarations, prerequisites, Cypher syntax, and final RETURN. Needed prerequisites must be enabled."

// editorFileProblem turns a failed file check into guidance for the author.
// `target` is the file being edited, so its own problems can say "this file".
func editorFileProblem(err error, target string) string {
	var guidance []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if match := missingDependencyPattern.FindStringSubmatch(line); match != nil {
			declaration := match[3] + ":" + match[4]
			if match[1] == target {
				guidance = append(guidance, fmt.Sprintf("Line %s reads %s, but the file does not declare it. Add it to a // depends-on: line, for example // depends-on: %s.", match[2], declaration, declaration))
			} else {
				guidance = append(guidance, fmt.Sprintf("The prerequisite %s reads %s on line %s without declaring it. Fix that file first.", match[1], declaration, match[2]))
			}
		}
	}
	if len(guidance) > 0 {
		return strings.Join(guidance, " ")
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "the selected extension and the file's namespace do not match"):
		return "The // namespace: line does not match the Installed extension chosen above. Choose the extension whose namespace the file uses, or change the namespace line."
	case strings.Contains(message, "install the schema for namespace"):
		return "No installed extension uses this file's // namespace: line. Install that extension's schema in OpenGraph Management, or change the namespace line."
	case strings.Contains(message, "add one leading // namespace: declaration"):
		return "Add one // namespace: line at the top of the file, before the Cypher."
	case strings.Contains(message, "depends-on: none cannot be combined with dependencies"):
		return "Remove // depends-on: none. A file that declares dependencies must not also say none."
	case undeclaredParameterPattern.MatchString(message):
		return fmt.Sprintf("$%s has no matching query prerequisite. Choose that query under Prerequisites, or change the parameter to one of the aliases listed there.", undeclaredParameterPattern.FindStringSubmatch(message)[1])
	case duplicateParameterPattern.MatchString(message):
		return fmt.Sprintf("Two prerequisites both use the alias %s. Give each one a different alias.", duplicateParameterPattern.FindStringSubmatch(message)[1])
	case strings.Contains(message, "a query file must contain one statement"):
		return "An analysis file must contain exactly one Cypher statement."
	case strings.Contains(message, "only the runner may write graph data"):
		return "Analysis files cannot change the graph directly (for example with CREATE, MERGE, SET, or DELETE). Return rows instead; the analysis saves them."
	case strings.Contains(message, "cannot call procedures"):
		return "Analysis files cannot use CALL to run procedures or contact other services."
	case strings.Contains(message, "a needed prerequisite is disabled or blocked"):
		return "A needed prerequisite is disabled or blocked. Enable it in the rules table, then try again."
	}
	return genericEditorFileProblem
}

// editorTestProblem explains a test that started but did not finish. Database
// messages can include parts of the query, so only recognized causes get
// specific guidance. Those name the column, property, or connection end at
// fault and give example object or helper IDs that the query itself returned;
// the author running the test can already read those objects.
func editorTestProblem(err error) string {
	var (
		helperProperty *runner.HelperPropertyError
		answerValue    *runner.AnswerValueError
		endpoint       *runner.EndpointError
	)
	switch {
	case errors.As(err, &helperProperty):
		property := runner.SafeName(helperProperty.Property, "")
		subject, example := "A property", "coalesce(x.name, x.objectid)"
		if property != "" {
			subject = "The " + property + " property"
			example = fmt.Sprintf("%s: coalesce(x.%s, x.objectid)", property, property)
		}
		problem := "is not text, a number, true/false, or a list of one of those"
		advice := "Return a supported value instead."
		if errors.Is(err, runner.ErrEmptyPropertyValue) {
			problem = "is empty (null)"
			advice = fmt.Sprintf("Give it a fallback, for example %s, or leave those rows out with WHERE … IS NOT NULL.", example)
		}
		return fmt.Sprintf("%s %s for %s: %s. %s", subject, problem, runner.HelperObjects(helperProperty.Count), runner.ExampleList(helperProperty.Examples, helperProperty.Count), advice)
	case errors.As(err, &answerValue):
		return fmt.Sprintf("Column %s on row %d holds a whole graph object (a node, relationship, or path). Return plain values instead, for example n.objectid AS objectid, n.name AS name.", runner.SafeName(answerValue.Column, "(unnamed)"), answerValue.Row)
	case errors.As(err, &endpoint):
		return runner.EndpointMessage(endpoint)
	case errors.Is(err, runner.ErrEmptyPropertyValue):
		return "A returned property is empty (null) for at least one object, and graph objects cannot store empty properties. Give it a fallback with coalesce(), or leave those rows out with WHERE … IS NOT NULL."
	}
	return "The test did not succeed. Check the query and returned values, or recalculate its prerequisites. No successful test was recorded."
}
