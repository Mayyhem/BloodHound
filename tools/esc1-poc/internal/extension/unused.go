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

package extension

import (
	"strings"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/querycheck"
)

// UnusedDependencies lists the declarations in a file's // depends-on lines
// that the file never uses. It is advice for the editor only: uploads and
// analysis runs accept unused declarations exactly as before.
//
// A declaration counts as used when the Cypher reads it (an object label, a
// relationship type, or a $parameter), or when an advanced instruction names
// it (reachability kinds, batch-on, or group-overlap). A query with an
// unlabeled read such as MATCH (n) may read any kind, so its object and
// relationship declarations always count as used.
func UnusedDependencies(query Query, checked querycheck.Result) []string {
	var unused []string
	for _, dependency := range query.Dependencies {
		if !dependencyUsed(query, checked, dependency) {
			unused = append(unused, dependencyLabel(dependency))
		}
	}
	return unused
}

func dependencyUsed(query Query, checked querycheck.Result, dependency Dependency) bool {
	// Same matching rule as the missing-dependency check in loadFiles.
	for _, input := range checked.Inputs {
		if input.Type == dependency.Type && (input.Name == dependency.Name || input.Type == "query" && dependency.AnswerParameter()+".cypher" == input.Name) {
			return true
		}
	}
	if dependency.Type == "query" {
		parameter := dependency.AnswerParameter()
		return query.BatchDependency == parameter || (query.Overlap != nil && query.Overlap.Memberships == parameter)
	}
	if checked.BroadRead {
		return true
	}
	if query.Reach != nil {
		kinds := query.Reach.NodeKinds
		if dependency.Type == "relationship" {
			kinds = query.Reach.Relationships
		}
		for _, kind := range kinds {
			if kind == dependency.Name {
				return true
			}
		}
	}
	return false
}

// dependencyLabel writes a declaration the way an author types it, for
// example node:Domain or query:PoC/users.
func dependencyLabel(dependency Dependency) string {
	return dependency.Type + ":" + strings.TrimSuffix(dependency.Name, ".cypher")
}
